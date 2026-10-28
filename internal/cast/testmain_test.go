package cast

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
)

type localCastTransport struct{ base http.RoundTripper }

func (t localCastTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	u := *r.URL
	clone.URL = &u
	if u.Hostname() == "192.0.2.1" {
		u.Host = net.JoinHostPort("127.0.0.1", u.Port())
	}
	ip := net.ParseIP(u.Hostname())
	if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("tests require mocked HTTP for %s", u.Hostname())
	}
	return t.base.RoundTrip(clone)
}

func TestMain(m *testing.M) {
	// Advertise a test LAN address while every fixture listens on loopback.
	outboundIP = func() (net.IP, error) { return net.IPv4(192, 0, 2, 1), nil }
	serverListen = func(network, address string) (net.Listener, error) {
		_, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		return net.Listen(network, net.JoinHostPort("127.0.0.1", port))
	}
	http.DefaultTransport = localCastTransport{base: http.DefaultTransport}
	os.Exit(m.Run())
}
