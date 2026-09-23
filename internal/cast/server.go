package cast

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Server hands the remuxed stream to the cast device over the LAN.
//
// The device fetches over the network, so this binds to the machine's routable
// LAN address rather than to localhost: the Chromecast is a different machine,
// and serving on loopback would be serving to nobody. It binds to that one
// address rather than to every interface, because what it serves is a
// directory listing of the scratch directory for the length of an episode.
type Server struct {
	dir      string
	listener net.Listener
	server   *http.Server
	baseURL  string
	mu       sync.Mutex
	err      error
	fetched  bool
}

// NewServer starts serving dir on a free port and returns immediately.
func NewServer(dir string) (*Server, error) {
	return NewServerOnPort(dir, 0)
}

// NewServerOnPort starts serving dir on the given port, or on a free one when
// port is 0.
//
// A fixed port exists for firewalls. A host that drops inbound connections by
// default -- ufw's shipped policy, among others -- blocks the device from
// fetching anything, and the failure is invisible from here: the device simply
// never connects, which looks exactly like a device that never got the load.
// A random port cannot be allowed through without opening the whole ephemeral
// range or the whole subnet; one port can be allowed with one rule.
func NewServerOnPort(dir string, port int) (*Server, error) {
	addr, err := outboundIP()
	if err != nil {
		return nil, err
	}

	// Bound to the one address the device needs, not every interface: this is
	// a directory listing of the scratch dir for the length of the episode,
	// and outboundIP already picked the address the device reaches it on.
	listener, err := net.Listen("tcp", net.JoinHostPort(addr.String(), strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("cast: could not listen: %w", err)
	}

	bound := listener.Addr().(*net.TCPAddr).Port
	server := &Server{
		dir:      dir,
		listener: listener,
		baseURL:  fmt.Sprintf("http://%s:%d", addr, bound),
	}

	// http.FileServer resolves ".." itself before touching the filesystem, so a
	// path climbing out of dir is answered 404 rather than served.
	files := http.FileServer(http.Dir(dir))

	// Every fetch is logged, because whether the device reached us at all is
	// the first thing worth knowing when a cast does not start: a device that
	// never appears here failed to reach this machine, and one that fetches
	// the playlist and then stops rejected what it was given. Without this the
	// two look identical from the outside.
	server.server = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Log(fmt.Sprintf("cast: %s requested %s", r.RemoteAddr, r.URL.Path))
			server.mu.Lock()
			server.fetched = true
			server.mu.Unlock()

			// The receiver plays adaptive media through a web player, which
			// fetches the manifest and every segment by XHR. Those fetches are
			// cross-origin, so without these headers the response arrives and
			// the player is then forbidden to read it: the request succeeds,
			// nothing plays, and the device asks again until it gives up.
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Range")
			w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			// Stated rather than inferred: mime.TypeByExtension reads the
			// system database, where .ts is a TypeScript source or a Qt
			// Linguist catalogue long before it is an MPEG transport stream,
			// and .m3u8 comes back as audio -- which is not what this is.
			if ctype := castContentType(r.URL.Path); ctype != "" {
				w.Header().Set("Content-Type", ctype)
			}

			files.ServeHTTP(w, r)
		}),
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

// Fetched reports whether anything has ever asked this server for a file.
//
// It is how a caller tells a device that refused what it was served from one
// that never reached this machine at all -- the second is almost always a host
// firewall dropping inbound connections, and from the device's own status the
// two are identical.
func (s *Server) Fetched() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fetched
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

// castContentType is the media type to serve a cast stream's file as, or "" to
// let net/http decide.
//
// The system mime database cannot be trusted for either of these extensions,
// and a receiver that is told a transport stream is a text file will not play
// it.
func castContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".ts":
		return "video/mp2t"
	default:
		return ""
	}
}
