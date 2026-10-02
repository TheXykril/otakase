package cast

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/vishen/go-chromecast/dns"
)

// DefaultDiscoveryTimeout is how long to listen for devices announcing
// themselves. Discovery is multicast and answers trickle in, so this is a
// deadline rather than a duration anything waits out in full.
const DefaultDiscoveryTimeout = 3 * time.Second

// DefaultDiscoveryPort is the UDP port SSDP searches are sent from, so a
// firewall needs one rule to let the answers back in.
const DefaultDiscoveryPort = 8011

var (
	discoveryPortMu sync.Mutex
	discoveryPort   = DefaultDiscoveryPort
)

// SetDiscoveryPort sets the UDP port discovery listens for answers on; 0 means
// a random free one. Call it before Discover.
//
// Only SSDP (DLNA) uses it. mDNS (Chromecast, Kodi) already listens on 5353,
// where its answers are multicast, so that port needs no setting of its own.
func SetDiscoveryPort(port int) {
	if port < 0 || port > 65535 {
		port = 0
	}
	discoveryPortMu.Lock()
	discoveryPort = port
	discoveryPortMu.Unlock()
}

func currentDiscoveryPort() int {
	discoveryPortMu.Lock()
	defer discoveryPortMu.Unlock()
	return discoveryPort
}

// discoverChromecasts lists the Chromecasts on the local network.
func discoverChromecasts(ctx context.Context) ([]Device, error) {
	found, err := dns.DiscoverCastDNSEntries(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("cast: discovery failed: %w", err)
	}

	entries := []dns.CastEntry{}
	for entry := range found {
		entries = append(entries, entry)
	}
	return devicesFromEntries(entries), nil
}
