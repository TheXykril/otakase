package internal

import (
	"strings"
	"testing"
)

// Raw mode turns off the translation that makes \n also return the cursor, so
// every line printed while the cast controls are live has to end \r\n. Without
// this each message starts where the last one ended and the output staircases
// across the screen -- which is what dragged the control panel's first frame
// into the middle of an unrelated line.
func TestOutUsesRawModeLineEndings(t *testing.T) {
	// The global config is shared with every other test in the package, and
	// leaving an empty one behind changes how unrelated code reads its
	// settings -- it broke a provider title-fallback test that runs after this.
	previous := GetGlobalConfig()
	SetGlobalConfig(&Config{})
	castSetRawMode(true)
	t.Cleanup(func() {
		castSetRawMode(false)
		SetGlobalConfig(previous)
	})

	out := captureStdout(t, func() { Out("first"); Out("second") })

	if strings.Contains(strings.ReplaceAll(out, "\r\n", ""), "\n") {
		t.Errorf("a bare newline survived raw mode: %q", out)
	}
	if !strings.Contains(out, "first\r\nsecond\r\n") {
		t.Errorf("messages are not separated by \\r\\n: %q", out)
	}
}
