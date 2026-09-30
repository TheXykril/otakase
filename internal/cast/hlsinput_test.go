package cast

import (
	"strings"
	"sync"
	"testing"
)

func TestHLSInputArgsFollowsFFmpeg(t *testing.T) {
	previous := hlsExtensionPicky
	t.Cleanup(func() { hlsExtensionPicky = previous })

	hlsExtensionPicky = func() bool { return true }
	if got := strings.Join(HLSInputArgs(), " "); got != "-allowed_extensions ALL -extension_picky 0" {
		t.Fatalf("current ffmpeg: got %q", got)
	}

	// ffmpeg before 7.1 refuses to start when given an option it lacks.
	hlsExtensionPicky = func() bool { return false }
	if got := strings.Join(HLSInputArgs(), " "); got != "-allowed_extensions ALL" {
		t.Fatalf("older ffmpeg: got %q", got)
	}
}

// Whatever ffmpeg is installed, asking it must not fail or hang the caller.
func TestHLSExtensionPickyProbe(t *testing.T) {
	probe := sync.OnceValue(probeHLSExtensionPicky)
	_ = probe()
}
