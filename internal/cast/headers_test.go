package cast

import (
	"strings"
	"testing"
)

// One host 403s its video segments unless Origin names the player's domain, and
// a Referer -- even the right one -- does not do. mpv was always sent it; the
// cast never was, so that provider's casts could not fetch a segment.
func TestInputHeadersCarryTheProvidersHeaders(t *testing.T) {
	headers := map[string]string{"Origin": "https://player.test", "Referer": "https://ignored.test/"}
	for name, args := range map[string][]string{
		"remux": BuildRemuxArgsFrom("https://host/master.m3u8", "https://kaa.test/", "/tmp/out", 0, nil, headers),
		"burn":  BuildBurnArgsFrom("https://host/master.m3u8", "https://kaa.test/", "/tmp/s.vtt", "/tmp/out", Encoder{Name: "libx264"}, 0, nil, headers),
		"probe": ProbeStreamArgs("https://host/master.m3u8", "https://kaa.test/", headers),
	} {
		joined := strings.Join(args, " ")
		want := "-headers Referer: https://kaa.test/\r\nOrigin: https://player.test\r\n "
		at := strings.Index(joined, want)
		if at < 0 || at > strings.Index(joined, "-i https://host/") {
			t.Errorf("%s: headers missing, doubled, or after the input:\n%q", name, joined)
		}
	}
}

// With no referrer of its own, a Referer among the headers is still sent.
func TestInputHeadersUseAHeaderReferer(t *testing.T) {
	got := strings.Join(inputHeaderArgs("", map[string]string{"Referer": "https://r.test/"}), " ")
	if got != "-headers Referer: https://r.test/\r\n" {
		t.Errorf("got %q", got)
	}
}

func TestInputHeadersEmpty(t *testing.T) {
	if got := inputHeaderArgs("  ", nil); len(got) != 0 {
		t.Errorf("sent headers with nothing to send: %q", got)
	}
}
