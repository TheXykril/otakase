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

// A torrent episode is a Matroska file served from a local port. Given the HLS
// demuxer's options, ffmpeg refused to open it ("Option extension_picky not
// found"), so no nyaa episode could be cast.
func TestInputArgsLeaveWholeFilesAlone(t *testing.T) {
	torrent := "http://127.0.0.1:42611/14ff93355d5119f16697b1ccde32cfa9ac0a9689"
	for _, stream := range []string{torrent, "https://cdn.example/episode.mkv"} {
		if got := InputArgs(stream); len(got) != 0 {
			t.Errorf("InputArgs(%q) = %v, want none", stream, got)
		}
	}
	for _, stream := range []string{
		"https://cdn.example/master.m3u8?token=abc",
		"http://127.0.0.1:40000/stream/abc/master.m3u8", // anineko's local proxy
		"https://cdn.example/playlist",
	} {
		if got := InputArgs(stream); len(got) == 0 {
			t.Errorf("InputArgs(%q) gave no HLS options", stream)
		}
	}

	for name, args := range map[string][]string{
		"remux": BuildRemuxArgsFrom(torrent, "", t.TempDir(), 115, nil, nil),
		"burn":  BuildBurnArgsFrom(torrent, "", "", t.TempDir(), Encoder{}, 115, nil, nil),
		"probe": ProbeStreamArgs(torrent, "", nil),
	} {
		if joined := strings.Join(args, " "); strings.Contains(joined, "allowed_extensions") {
			t.Errorf("%s args for a torrent carry HLS options: %s", name, joined)
		}
	}
}
