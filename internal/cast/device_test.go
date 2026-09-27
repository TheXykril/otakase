package cast

import (
	"net"
	"testing"

	"github.com/vishen/go-chromecast/dns"
)

// Discovery answers on every interface, so the same device arrives more than
// once. Offering it twice in a menu is a bug the user has to think about.
func TestDevicesFromEntriesDedupesByUUID(t *testing.T) {
	entries := []dns.CastEntry{
		{UUID: "abc", DeviceName: "Living Room", AddrV4: net.ParseIP("192.168.1.10"), Port: 8009},
		{UUID: "abc", DeviceName: "Living Room", AddrV4: net.ParseIP("192.168.1.10"), Port: 8009},
		{UUID: "def", DeviceName: "Bedroom", AddrV4: net.ParseIP("192.168.1.11"), Port: 8009},
	}

	got := devicesFromEntries(entries)
	if len(got) != 2 {
		t.Fatalf("got %d devices, want 2: %+v", len(got), got)
	}
}

// A device with no friendly name still has to be selectable, or it cannot be
// told apart from the next unnamed one.
func TestDevicesFromEntriesFallsBackToTheAddress(t *testing.T) {
	entries := []dns.CastEntry{
		{UUID: "abc", DeviceName: "", AddrV4: net.ParseIP("192.168.1.10"), Port: 8009},
	}

	got := devicesFromEntries(entries)
	if len(got) != 1 {
		t.Fatalf("got %d devices, want 1", len(got))
	}
	if got[0].Name != "192.168.1.10" {
		t.Errorf("unnamed device is called %q", got[0].Name)
	}
}

// An entry with no usable address cannot be connected to, so it must not be
// offered at all.
func TestDevicesFromEntriesSkipsAddresslessEntries(t *testing.T) {
	entries := []dns.CastEntry{
		{UUID: "abc", DeviceName: "Ghost", Port: 8009},
		{UUID: "def", DeviceName: "Real", AddrV4: net.ParseIP("192.168.1.11"), Port: 8009},
	}

	got := devicesFromEntries(entries)
	if len(got) != 1 || got[0].Name != "Real" {
		t.Fatalf("addressless entry was offered: %+v", got)
	}
}

func TestDeviceStringNamesTheDeviceAndAddress(t *testing.T) {
	device := Device{Name: "Living Room", Addr: net.ParseIP("192.168.1.10"), Port: 8009}
	if got := device.String(); got != "Living Room (192.168.1.10:8009)" {
		t.Errorf("String() = %q", got)
	}
}
