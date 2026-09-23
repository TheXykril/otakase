package internal

import (
	"strings"
	"testing"
)

// Every key a viewer can press, decoded from the bytes a terminal in raw mode
// actually delivers.
func TestDecodeCastKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []byte
		want castCommand
		used int
	}{
		{"space pauses", []byte(" "), castCmdPauseToggle, 1},
		{"q stops", []byte("q"), castCmdStop, 1},
		{"ctrl-c stops", []byte{0x03}, castCmdStop, 1},
		{"s skips a span", []byte("s"), castCmdSkipSpan, 1},
		{"right seeks forward", []byte{0x1b, '[', 'C'}, castCmdSeekForward, 3},
		{"left seeks back", []byte{0x1b, '[', 'D'}, castCmdSeekBack, 3},
		{"up raises volume", []byte{0x1b, '[', 'A'}, castCmdVolumeUp, 3},
		{"down lowers volume", []byte{0x1b, '[', 'B'}, castCmdVolumeDown, 3},
		{"an unknown key is consumed, not acted on", []byte("z"), castCmdNone, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, used := decodeCastKey(tc.in)
			if got != tc.want || used != tc.used {
				t.Errorf("decodeCastKey(%q) = (%v, %d), want (%v, %d)", tc.in, got, used, tc.want, tc.used)
			}
		})
	}
}

// Review Focus 2. A terminal may deliver an arrow key's three bytes across
// separate reads. Consuming a partial sequence turns one arrow into a stray
// "[" and a wrong command, so an incomplete escape must consume nothing and
// wait for the rest.
func TestDecodeCastKeyWaitsForASplitEscapeSequence(t *testing.T) {
	for _, partial := range [][]byte{{0x1b}, {0x1b, '['}} {
		got, used := decodeCastKey(partial)
		if got != castCmdNone || used != 0 {
			t.Errorf("decodeCastKey(%v) = (%v, %d), want (castCmdNone, 0)", partial, got, used)
		}
	}

	if got, used := decodeCastKey([]byte{0x1b, '[', 'C'}); got != castCmdSeekForward || used != 3 {
		t.Errorf("completed sequence = (%v, %d), want (castCmdSeekForward, 3)", got, used)
	}
}

// The status line is the only thing on screen for twenty minutes, so it shows
// where the episode is, whether it is moving, and what the volume is.
func TestCastStatusLine(t *testing.T) {
	line := castStatusLine(724, 1451, "PLAYING", 0.6)

	for _, want := range []string{"12:04", "24:11", "PLAYING", "60%"} {
		if !strings.Contains(line, want) {
			t.Errorf("status line %q does not contain %q", line, want)
		}
	}
}

// A device that has not reported a duration yet must not render as "/ 0:00" or
// divide by zero building the bar.
func TestCastStatusLineToleratesAnUnknownDuration(t *testing.T) {
	line := castStatusLine(12, 0, "BUFFERING", 0.5)

	if !strings.Contains(line, "BUFFERING") {
		t.Errorf("status line %q lost the player state", line)
	}
	if strings.Contains(line, "NaN") || strings.Contains(line, "Inf") {
		t.Errorf("status line %q has a number built by dividing by zero", line)
	}
}
