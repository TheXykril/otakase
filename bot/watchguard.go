package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"syscall"
	"time"
)

// The watch relay fetches whatever a provider's playlist names. The bot runs
// next to Home Assistant and other devices on a home network, so a playlist
// that names a LAN or loopback address must not make the bot fetch it and
// play the answer to the room. The check runs when a connection is dialled,
// after DNS, so redirects and hostnames that resolve to a private address
// are refused too.
//
// One loopback address is allowed per episode: the one the episode itself
// starts from, when that is the torrent helper's or the app's own relay on
// 127.0.0.1. Addresses a remote playlist names never get that allowance.
//
// A proxy set through HTTPS_PROXY/HTTP_PROXY on a private address would be
// refused as well; the add-on doesn't use one.

var errPrivateUpstream = errors.New("refusing to fetch a private network address")

type watchLocalKey struct{}

// withWatchLocal allows dialling the loopback address of link, if link is a
// plain-http loopback address. Anything else leaves ctx as it is.
func withWatchLocal(ctx context.Context, link string) context.Context {
	if addr := watchLocalAddr(link); addr != "" {
		return context.WithValue(ctx, watchLocalKey{}, addr)
	}
	return ctx
}

// watchLocalAddr is the host:port of a loopback http link, or "".
func watchLocalAddr(link string) string {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "http" || u.Port() == "" {
		return ""
	}
	ip, err := netip.ParseAddr(u.Hostname())
	if err != nil || !ip.IsLoopback() {
		return ""
	}
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil {
		return ""
	}
	return netip.AddrPortFrom(ip.Unmap(), uint16(port)).String()
}

// publicUpstreamIP reports whether ip is safe to fetch from: not loopback, private,
// link-local, carrier-grade NAT, multicast or unspecified.
func publicUpstreamIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() ||
		ip.IsUnspecified() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT, also Tailscale
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), // NAT64 can wrap a private IPv4 address
}

// guardedDial refuses any address that isn't public, except the one loopback
// address ctx allows.
func guardedDial(ctx context.Context, network, addr string) (net.Conn, error) {
	allowed, _ := ctx.Value(watchLocalKey{}).(string)
	d := net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return errPrivateUpstream
			}
			if publicUpstreamIP(ap.Addr()) {
				return nil
			}
			if allowed != "" && netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port()).String() == allowed {
				return nil
			}
			return fmt.Errorf("%w (%s)", errPrivateUpstream, ap.Addr())
		},
	}
	return d.DialContext(ctx, network, addr)
}

func newWatchUpstream() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = guardedDial
	return &http.Client{Timeout: 2 * time.Minute, Transport: t}
}
