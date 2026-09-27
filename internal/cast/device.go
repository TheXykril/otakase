// Package cast plays a stream on a Chromecast on the local network.
//
// It deliberately knows nothing about otakase's types: internal imports this
// package for the bridge, so importing internal back would be a cycle. Values
// in, values out -- the same discipline internal/providerhost follows.
package cast

import (
	"fmt"
	"net"

	"github.com/vishen/go-chromecast/dns"
)

// Device is one Chromecast found on the network.
type Device struct {
	Name string
	UUID string
	Addr net.IP
	Port int
}

// String names a device the way a menu should show it: the friendly name, and
// the address to tell two rooms with the same name apart.
func (d Device) String() string {
	return fmt.Sprintf("%s (%s:%d)", d.Name, d.Addr, d.Port)
}

// devicesFromEntries turns discovery results into devices worth offering.
//
// Discovery answers per interface, so the same device arrives repeatedly; and
// an entry without an address cannot be connected to at all.
func devicesFromEntries(entries []dns.CastEntry) []Device {
	devices := make([]Device, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))

	for _, entry := range entries {
		addr := entry.AddrV4
		if addr == nil {
			addr = entry.AddrV6
		}
		if addr == nil {
			continue
		}
		if _, already := seen[entry.UUID]; already && entry.UUID != "" {
			continue
		}
		if entry.UUID != "" {
			seen[entry.UUID] = struct{}{}
		}

		name := entry.DeviceName
		if name == "" {
			name = addr.String()
		}

		devices = append(devices, Device{
			Name: name,
			UUID: entry.UUID,
			Addr: addr,
			Port: entry.Port,
		})
	}
	return devices
}
