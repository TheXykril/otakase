package cast

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

func TestDiscoverAllMergesKindsInOrder(t *testing.T) {
	from := []backend{
		{kind: KindChromecast, discover: func(context.Context) ([]Device, error) {
			return []Device{{Name: "Living Room", Kind: KindChromecast}}, nil
		}},
		{kind: "dlna", discover: func(context.Context) ([]Device, error) {
			return []Device{{Name: "Bedroom TV", Kind: "dlna"}}, nil
		}},
	}
	devices, err := discoverAll(context.Background(), from)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 || devices[0].Name != "Living Room" || devices[1].Name != "Bedroom TV" {
		t.Fatalf("devices = %v", devices)
	}
}

func TestDiscoverAllSurvivesOneKindFailing(t *testing.T) {
	from := []backend{
		{kind: KindChromecast, discover: func(context.Context) ([]Device, error) {
			return nil, errors.New("multicast blocked")
		}},
		{kind: "dlna", discover: func(context.Context) ([]Device, error) {
			return []Device{{Name: "Bedroom TV", Kind: "dlna"}}, nil
		}},
	}
	devices, err := discoverAll(context.Background(), from)
	if err != nil || len(devices) != 1 {
		t.Fatalf("devices = %v, err = %v", devices, err)
	}
}

func TestDiscoverAllFailsOnlyWhenEveryKindFails(t *testing.T) {
	from := []backend{
		{kind: KindChromecast, discover: func(context.Context) ([]Device, error) { return nil, errors.New("a") }},
		{kind: "dlna", discover: func(context.Context) ([]Device, error) { return nil, errors.New("b") }},
	}
	if _, err := discoverAll(context.Background(), from); err == nil || !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), "b") {
		t.Fatalf("err = %v", err)
	}
}

func TestDeviceStringNamesOtherKinds(t *testing.T) {
	d := Device{Name: "Bedroom TV", Addr: net.ParseIP("192.168.1.20"), Port: 1400, Kind: "dlna"}
	if got := d.String(); got != "Bedroom TV (192.168.1.20:1400) · DLNA" {
		t.Fatalf("got %q", got)
	}
}

func TestConnectRefusesAnUnknownKind(t *testing.T) {
	if _, err := Connect(Device{Name: "Toaster", Kind: "toaster"}); err == nil {
		t.Fatal("expected an error")
	}
}
