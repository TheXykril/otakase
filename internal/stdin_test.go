package internal

import (
	"os"
	"path/filepath"
	"testing"
)

func stdinFromString(t *testing.T, contents string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing fake stdin: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening fake stdin: %v", err)
	}
	original := os.Stdin
	os.Stdin = file
	t.Cleanup(func() {
		os.Stdin = original
		file.Close()
	})
}

// A stdin that has ended answers instantly and forever. The waits that use this
// sit in loops that start the next episode each time round, so reading that as
// "enter was pressed" spins them, opening a player per turn.
func TestAwaitEnterReportsAnEndedStdin(t *testing.T) {
	stdinFromString(t, "")
	if AwaitEnter() {
		t.Error("an ended stdin was reported as enter being pressed")
	}
}

// The reader is kept between calls: one built per read would throw away the
// second line along with everything else it had buffered.
func TestAwaitEnterKeepsWhatItBuffered(t *testing.T) {
	stdinFromString(t, "\n\n")

	if !AwaitEnter() {
		t.Fatal("the first enter was not seen")
	}
	if !AwaitEnter() {
		t.Error("the second enter was lost with the first read's buffer")
	}
	if AwaitEnter() {
		t.Error("a third enter was reported from an exhausted stdin")
	}
}
