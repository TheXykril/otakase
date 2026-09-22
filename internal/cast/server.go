package cast

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// Server hands the remuxed stream to the cast device over the LAN.
//
// The device fetches over the network, so this binds to every interface and
// advertises a routable address. Serving on localhost would be serving to
// nobody: the Chromecast is a different machine.
type Server struct {
	dir      string
	listener net.Listener
	server   *http.Server
	baseURL  string
	mu       sync.Mutex
	err      error
}

// NewServer starts serving dir on a free port and returns immediately.
func NewServer(dir string) (*Server, error) {
	addr, err := outboundIP()
	if err != nil {
		return nil, err
	}

	// Bound to the one address the device needs, not every interface: this is
	// a directory listing of the scratch dir for the length of the episode,
	// and outboundIP already picked the address the device reaches it on.
	listener, err := net.Listen("tcp", net.JoinHostPort(addr.String(), "0"))
	if err != nil {
		return nil, fmt.Errorf("cast: could not listen: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	server := &Server{
		dir:      dir,
		listener: listener,
		baseURL:  fmt.Sprintf("http://%s:%d", addr, port),
	}

	// http.FileServer resolves ".." itself before touching the filesystem, so a
	// path climbing out of dir is answered 404 rather than served.
	server.server = &http.Server{
		Handler:           http.FileServer(http.Dir(dir)),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		err := server.server.Serve(listener)
		// Close is how this normally ends; only an unasked-for stop is a fault.
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			server.mu.Lock()
			server.err = fmt.Errorf("cast: the stream server stopped: %w", err)
			server.mu.Unlock()
		}
	}()

	return server, nil
}

// URL is where the cast device should fetch name from.
func (s *Server) URL(name string) string {
	return s.baseURL + "/" + name
}

// Err reports why the server stopped serving, or nil while it is still up or
// if it was closed deliberately. The device gives no sign that its source has
// died -- it just stops playing -- so the caller polls this to tell a dead
// server from a finished episode.
func (s *Server) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Close stops serving. A leaked server holds its port for the life of the
// process, and every episode starts another one.
func (s *Server) Close() error {
	return s.server.Close()
}

// outboundIP finds the address this machine is reachable at from the LAN.
//
// It dials a UDP socket rather than scanning interfaces: no packet is sent,
// but the kernel picks the source address it would route from, which is the
// one the cast device can reach back on. Scanning interfaces means guessing
// between a VPN, a container bridge and the real network.
func outboundIP() (net.IP, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return nil, fmt.Errorf("cast: could not determine this machine's LAN address: %w", err)
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP, nil
}
