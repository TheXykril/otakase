package internal

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// The referrer is the point of this file. Episode.StreamReferrer,
// SubtitleURL, StreamHeaders and Mode are all tagged json:"-", so marshalling
// an Anime drops them silently -- and without the referrer the remux cannot
// fetch the stream from the provider at all. A cast handed off without it
// fails with no obvious cause.
func TestCastSessionRoundTripKeepsTheReferrer(t *testing.T) {
	config := testCastConfig(t)
	anime := testCastAnime()
	anime.Ep.StreamReferrer = "https://megaplay.buzz/"
	anime.Ep.StreamHeaders = map[string]string{"Origin": "https://megaplay.buzz"}
	anime.Ep.Links = []string{"https://example.test/master.m3u8?token=abc"}

	path, err := writeCastSession(config, anime, "Office TV")
	if err != nil {
		t.Fatalf("writeCastSession: %v", err)
	}

	session, err := readCastSession(path)
	if err != nil {
		t.Fatalf("readCastSession: %v", err)
	}

	if session.StreamReferrer != "https://megaplay.buzz/" {
		t.Errorf("referrer = %q, want %q", session.StreamReferrer, "https://megaplay.buzz/")
	}
	if session.StreamHeaders["Origin"] != "https://megaplay.buzz" {
		t.Errorf("headers = %v, lost Origin", session.StreamHeaders)
	}
	if len(session.Links) != 1 || session.Links[0] != anime.Ep.Links[0] {
		t.Errorf("links = %v, want %v", session.Links, anime.Ep.Links)
	}
	if session.Device != "Office TV" {
		t.Errorf("device = %q, want %q", session.Device, "Office TV")
	}
}

// The file holds a stream URL with an authentication token, so it is not
// readable by other users and does not outlive the handoff.
func TestCastSessionFileIsPrivateAndDeletedOnRead(t *testing.T) {
	config := testCastConfig(t)

	path, err := writeCastSession(config, testCastAnime(), "Office TV")
	if err != nil {
		t.Fatalf("writeCastSession: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %o, want 600: this file contains a tokened URL", mode)
	}

	if _, err := readCastSession(path); err != nil {
		t.Fatalf("readCastSession: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the session file outlived the read that consumed it")
	}
}

// A child from a different build must refuse a file it does not understand
// rather than misreading a tokened URL out of it.
func TestReadCastSessionRejectsAnUnknownVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")
	if err := os.WriteFile(path, []byte(`{"version":9999}`), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}

	if _, err := readCastSession(path); err == nil {
		t.Error("a session file from an unknown version was accepted")
	}
}

// Review Focus 5. If the spawn succeeds but the terminal dies before reading,
// nothing deletes the file -- and it holds a tokened URL. The sweep is what
// stops those accumulating.
func TestSweepCastSessionsRemovesStaleFiles(t *testing.T) {
	config := testCastConfig(t)

	stale, err := writeCastSession(config, testCastAnime(), "Office TV")
	if err != nil {
		t.Fatalf("writeCastSession: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	fresh, err := writeCastSession(config, testCastAnime(), "Office TV")
	if err != nil {
		t.Fatalf("writeCastSession: %v", err)
	}

	sweepCastSessions(config, time.Hour)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("a two-hour-old session file survived the sweep")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("the sweep deleted a session file that had just been written")
	}
}

// Review Focus 1. $TERMINAL and CastTerminal routinely carry arguments --
// "flatpak run org.x.Term", "kitty --single-instance". exec.Command on the
// whole string looks for a binary with spaces in its name and fails, which is
// the same trap internal/editor.go documents for EDITOR.
func TestResolveCastTerminalSplitsArguments(t *testing.T) {
	config := &Config{CastTerminal: "flatpak run org.x.Term"}
	found := func(name string) (string, error) { return "/usr/bin/" + name, nil }

	command, err := resolveCastTerminal(config, found)
	if err != nil {
		t.Fatalf("resolveCastTerminal: %v", err)
	}
	want := []string{"/usr/bin/flatpak", "run", "org.x.Term"}
	if len(command) != len(want) {
		t.Fatalf("command = %v, want %v", command, want)
	}
	for i := range want {
		if command[i] != want[i] {
			t.Fatalf("command = %v, want %v", command, want)
		}
	}
}

// Configuration beats the environment, which beats whatever is installed.
func TestResolveCastTerminalPrefersConfigThenEnvThenPath(t *testing.T) {
	onlyFoot := func(name string) (string, error) {
		if name == "foot" || name == "myterm" || name == "envterm" {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}

	t.Setenv("TERMINAL", "envterm")

	command, err := resolveCastTerminal(&Config{CastTerminal: "myterm"}, onlyFoot)
	if err != nil || command[0] != "/usr/bin/myterm" {
		t.Errorf("with CastTerminal set, got %v (%v), want myterm", command, err)
	}

	command, err = resolveCastTerminal(&Config{}, onlyFoot)
	if err != nil || command[0] != "/usr/bin/envterm" {
		t.Errorf("with only $TERMINAL set, got %v (%v), want envterm", command, err)
	}

	t.Setenv("TERMINAL", "")
	command, err = resolveCastTerminal(&Config{}, onlyFoot)
	if err != nil || command[0] != "/usr/bin/foot" {
		t.Errorf("with neither set, got %v (%v), want foot from PATH", command, err)
	}
}

// A machine with no terminal emulator must say so, so the caller can fall back
// to casting in place rather than losing the episode.
func TestResolveCastTerminalReportsWhenNoneIsInstalled(t *testing.T) {
	t.Setenv("TERMINAL", "")
	none := func(string) (string, error) { return "", exec.ErrNotFound }

	if _, err := resolveCastTerminal(&Config{}, none); err == nil {
		t.Error("no terminal is installed, but resolveCastTerminal found one")
	}
}
