package internal

import (
	"strings"
	"testing"
)

// A referrer is not always enough to fetch a stream. KickassAnime's manifest
// wants Referer: kaa.lt, but its video segments live on another host that
// answers 403 unless Origin names the player's domain -- and MPV's --referrer
// cannot set Origin. The symptom is the worst kind: the manifest opens, MPV
// reports a playable file, and then every segment fails.
func TestStreamHeaderArgsRendersMPVArguments(t *testing.T) {
	args := streamHeaderArgs(map[string]string{"Origin": "https://krussdomi.com"})
	if len(args) != 1 {
		t.Fatalf("expected one argument, got %v", args)
	}
	want := "--http-header-fields-append=Origin: https://krussdomi.com"
	if args[0] != want {
		t.Fatalf("got %q, want %q", args[0], want)
	}
}

// Append rather than replace, so headers do not silently discard whatever the
// user put in MpvArgs.
func TestStreamHeaderArgsAppendsRatherThanReplaces(t *testing.T) {
	for _, arg := range streamHeaderArgs(map[string]string{"Origin": "https://example.test"}) {
		if strings.HasPrefix(arg, "--http-header-fields=") {
			t.Fatalf("must not replace configured header fields: %q", arg)
		}
	}
}

func TestStreamHeaderArgsIsStableAndSkipsEmpties(t *testing.T) {
	headers := map[string]string{
		"Origin": "https://example.test",
		"Accept": "*/*",
		"Empty":  "",
		"  ":     "ignored",
	}
	first := streamHeaderArgs(headers)
	if len(first) != 2 {
		t.Fatalf("expected blank names and values to be dropped, got %v", first)
	}
	// Sorted, so two runs of the same episode launch MPV identically.
	if !strings.Contains(first[0], "Accept") || !strings.Contains(first[1], "Origin") {
		t.Fatalf("expected header names in sorted order, got %v", first)
	}
	for i := 0; i < 5; i++ {
		again := streamHeaderArgs(headers)
		for j := range first {
			if again[j] != first[j] {
				t.Fatalf("ordering is not stable: %v vs %v", first, again)
			}
		}
	}
}

func TestStreamHeaderArgsEmptyWhenNothingToSend(t *testing.T) {
	if args := streamHeaderArgs(nil); args != nil {
		t.Fatalf("expected no arguments, got %v", args)
	}
	if args := streamHeaderArgs(map[string]string{}); args != nil {
		t.Fatalf("expected no arguments, got %v", args)
	}
}

// The hint has to survive the provider bridge, or the headers never reach MPV.
func TestApplyStreamPlaybackHintsCarriesHeaders(t *testing.T) {
	anime := &Anime{}
	link := "https://cdn.test/master.m3u8"
	applyStreamPlaybackHints(anime, []string{link}, map[string]StreamPlaybackHint{
		link: {
			Referrer: "https://site.test/",
			Subtitle: "https://cdn.test/en.vtt",
			Headers:  map[string]string{"Origin": "https://player.test"},
		},
	})

	if anime.Ep.StreamReferrer != "https://site.test/" {
		t.Errorf("referrer: got %q", anime.Ep.StreamReferrer)
	}
	if anime.Ep.SubtitleURL != "https://cdn.test/en.vtt" {
		t.Errorf("subtitle: got %q", anime.Ep.SubtitleURL)
	}
	if anime.Ep.StreamHeaders["Origin"] != "https://player.test" {
		t.Errorf("headers: got %v", anime.Ep.StreamHeaders)
	}
}

// A link with no hint must clear stale headers, or the next episode inherits
// the previous provider's Origin and fails in a way that looks like the CDN.
func TestApplyStreamPlaybackHintsClearsStaleHeaders(t *testing.T) {
	anime := &Anime{}
	anime.Ep.StreamHeaders = map[string]string{"Origin": "https://old.test"}
	applyStreamPlaybackHints(anime, []string{"https://cdn.test/a.m3u8"}, nil)
	if anime.Ep.StreamHeaders != nil {
		t.Fatalf("expected headers to be cleared, got %v", anime.Ep.StreamHeaders)
	}
}
