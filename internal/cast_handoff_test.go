package internal

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

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

// Moving an untracked cast on must drop everything that belonged to the
// episode just played, or the next one is cast from the old stream.
func TestAdvanceUntrackedCastClearsTheFinishedEpisode(t *testing.T) {
	anime := testCastAnime()
	anime.Untracked = true
	anime.Ep.Links = []string{"https://example.test/ep5.m3u8"}
	anime.Ep.SubtitleURL = "https://example.test/ep5.vtt"
	anime.Ep.Duration = 1421
	anime.Ep.SkipTimes = SkipTimes{Ed: Skip{Start: 1327, End: 1416}}
	anime.Ep.Player.PlaybackTime = 1420

	advanceUntrackedCast(anime)

	if anime.Ep.Number != 6 {
		t.Errorf("episode = %d, want 6", anime.Ep.Number)
	}
	if len(anime.Ep.Links) != 0 || anime.Ep.SubtitleURL != "" || anime.Ep.Duration != 0 ||
		anime.Ep.SkipTimes != (SkipTimes{}) || anime.Ep.Player.PlaybackTime != 0 {
		t.Errorf("the finished episode's state was carried over: %+v", anime.Ep)
	}
	if !anime.Untracked {
		t.Error("the show stopped being untracked")
	}
}

// A provider and AniList routinely punctuate one title differently; the skip
// lookup for an untracked show must still see them as the same title, and
// must not see a sequel as it.
func TestUntrackedTitleKeyIgnoresPunctuationOnly(t *testing.T) {
	if untrackedTitleKey("Frieren: Beyond Journey's End") != untrackedTitleKey("Frieren: Beyond Journey’s End") {
		t.Error("a curly apostrophe broke the match")
	}
	if untrackedTitleKey("Demon King Daimao") == untrackedTitleKey("Demon King Daimao 2") {
		t.Error("a sequel matched the original")
	}
}
