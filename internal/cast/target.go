package cast

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Kind is the protocol a cast device speaks.
type Kind string

const (
	KindChromecast Kind = "chromecast"
)

// Label is how a kind is named to the viewer.
func (k Kind) Label() string {
	switch k {
	case KindChromecast:
		return "Chromecast"
	case KindDLNA:
		return "DLNA"
	case KindKodi:
		return "Kodi"
	default:
		return strings.ToUpper(string(k))
	}
}

// Player is a connected device playing one stream. Everything past device
// choice -- the watch loop, skips, seeking by rebuilding the stream, the
// panel, tracking -- talks to this and nothing more specific, so a new kind of
// device is a new Player and a way to find it, not a change to any of that.
type Player interface {
	// Play loads a URL served from this machine and starts it, returning as
	// soon as the device has been told: the caller's watch loop polls it.
	Play(url string) error
	Progress() (Progress, error)
	SeekToTime(seconds float64) error
	Pause() error
	Unpause() error
	// SetVolume and Volume use a 0..1 scale.
	SetVolume(level float64) error
	Volume() float64
	// Stop ends playback and lets the device go.
	Stop() error
}

// backend is how one kind of device is found and connected to.
type backend struct {
	kind     Kind
	discover func(ctx context.Context) ([]Device, error)
	connect  func(Device) (Player, error)
}

// backends is every kind of device otakase can cast to, in the order their
// devices are listed.
var backends = []backend{
	{
		kind:     KindChromecast,
		discover: discoverChromecasts,
		connect: func(d Device) (Player, error) {
			// Not returned directly: a nil *Session inside a Player is not a
			// nil Player, and teardown checks for nil.
			s, err := connectChromecast(d)
			if err != nil {
				return nil, err
			}
			return s, nil
		},
	},
	{
		kind:     KindDLNA,
		discover: discoverDLNA,
		connect: func(d Device) (Player, error) {
			p, err := connectDLNA(d)
			if err != nil {
				return nil, err
			}
			return p, nil
		},
	},
	{
		kind:     KindKodi,
		discover: discoverKodi,
		connect: func(d Device) (Player, error) {
			p, err := connectKodi(d)
			if err != nil {
				return nil, err
			}
			return p, nil
		},
	},
}

// Discover lists the cast devices on the local network, of every kind at once.
//
// Each kind listens for its own announcements in parallel, so adding a kind
// does not add to the wait. One kind failing -- a blocked multicast port, say
// -- does not hide the devices another found; only every kind failing is an
// error.
func Discover(ctx context.Context, timeout time.Duration) ([]Device, error) {
	if timeout <= 0 {
		timeout = DefaultDiscoveryTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return discoverAll(ctx, backends)
}

func discoverAll(ctx context.Context, from []backend) ([]Device, error) {
	found := make([][]Device, len(from))
	errs := make([]error, len(from))
	var wg sync.WaitGroup
	for i, b := range from {
		wg.Add(1)
		go func(i int, b backend) {
			defer wg.Done()
			found[i], errs[i] = b.discover(ctx)
		}(i, b)
	}
	wg.Wait()

	var devices []Device
	var failures []string
	for i := range from {
		if errs[i] != nil {
			Log(fmt.Sprintf("cast: %s discovery failed: %v", from[i].kind.Label(), errs[i]))
			failures = append(failures, errs[i].Error())
			continue
		}
		devices = append(devices, found[i]...)
	}
	if len(failures) == len(from) && len(from) > 0 {
		if len(errs) == 1 {
			return nil, errs[0]
		}
		return nil, fmt.Errorf("cast: discovery failed: %s", strings.Join(failures, "; "))
	}
	return devices, nil
}

// Connect opens a connection to a device of any kind.
func Connect(d Device) (Player, error) {
	for _, b := range backends {
		if b.kind == d.kind() {
			return b.connect(d)
		}
	}
	return nil, fmt.Errorf("cast: %s is a %s, which otakase cannot cast to", d.Name, d.kind().Label())
}
