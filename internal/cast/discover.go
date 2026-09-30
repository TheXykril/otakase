package cast

import (
	"context"
	"fmt"
	"time"

	"github.com/vishen/go-chromecast/dns"
)

// DefaultDiscoveryTimeout is how long to listen for devices announcing
// themselves. Discovery is multicast and answers trickle in, so this is a
// deadline rather than a duration anything waits out in full.
const DefaultDiscoveryTimeout = 3 * time.Second

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
