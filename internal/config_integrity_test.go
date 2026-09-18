package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestKnownConfigKeysCoversTheStruct(t *testing.T) {
	keys := KnownConfigKeys()
	if len(keys) < 20 {
		t.Fatalf("expected the full config surface, got %d keys", len(keys))
	}
	for _, want := range []string{"Player", "Provider", "SkipOp", "MyAnimeListClientID", "Theme", "DownloadDir"} {
		found := false
		for _, key := range keys {
			if key == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %q among known keys", want)
		}
	}
}

// The exact corruption found in a real config: "MyAnimeListClientID" with its
// first three characters gone.
func TestUnknownConfigKeysFlagsRealCorruption(t *testing.T) {
	unknown := UnknownConfigKeys(map[string]string{
		"MyAnimeListClientID": "abc",
		"nimeListClientID":    "abc",
		"Player":              "mpv",
	})

	suggestion, ok := unknown["nimeListClientID"]
	if !ok {
		t.Fatalf("expected the corrupted key to be flagged, got %v", unknown)
	}
	if suggestion != "MyAnimeListClientID" {
		t.Fatalf("suggestion = %q, want MyAnimeListClientID", suggestion)
	}
	if _, flagged := unknown["Player"]; flagged {
		t.Fatal("a valid key must not be flagged")
	}
	if len(unknown) != 1 {
		t.Fatalf("expected exactly one unknown key, got %v", unknown)
	}
}

// A typo'd setting silently doing nothing is the failure this prevents.
func TestUnknownConfigKeysSuggestsForCaseTypos(t *testing.T) {
	unknown := UnknownConfigKeys(map[string]string{"SkipOP": "true"})
	if unknown["SkipOP"] != "SkipOp" {
		t.Fatalf("expected a SkipOp suggestion, got %q", unknown["SkipOP"])
	}
}

// Something wholly unrelated gets flagged but not mis-suggested.
func TestUnknownConfigKeysDoesNotInventSuggestions(t *testing.T) {
	unknown := UnknownConfigKeys(map[string]string{"CompletelyUnrelatedSetting": "1"})
	suggestion, ok := unknown["CompletelyUnrelatedSetting"]
	if !ok {
		t.Fatal("expected the key to be flagged")
	}
	if suggestion != "" {
		t.Fatalf("expected no suggestion for an unrelated key, got %q", suggestion)
	}
}

func TestSaveConfigToFileRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "otakase.conf")
	in := map[string]string{"Player": "mpv", "SkipOp": "true", "Provider": "stacked"}

	if err := SaveConfigToFile(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := LoadConfigFromFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for key, want := range in {
		if out[key] != want {
			t.Fatalf("%s = %q, want %q", key, out[key], want)
		}
	}
}

// The write must leave no temporary files behind.
func TestSaveConfigToFileLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "otakase.conf")
	if err := SaveConfigToFile(path, map[string]string{"Player": "mpv"}); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
	if len(entries) != 1 {
		t.Fatalf("expected only the config file, got %d entries", len(entries))
	}
}

// The corruption this replaces came from a non-atomic truncate-then-write with
// no locking. Concurrent writers must never leave a partial file: every read
// must see one complete, valid config.
func TestSaveConfigToFileIsAtomicUnderConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "otakase.conf")

	// A long value makes a torn write obvious.
	long := strings.Repeat("x", 4096)
	writers := map[string]string{
		"a": "MyAnimeListClientID=" + long,
		"b": "MyAnimeListClientID=" + strings.Repeat("y", 8192),
	}
	_ = writers

	if err := SaveConfigToFile(path, map[string]string{"MyAnimeListClientID": long}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Readers check the file stays complete and parseable throughout.
	var readerErr error
	var readerMu sync.Mutex
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				got, err := LoadConfigFromFile(path)
				if err != nil {
					readerMu.Lock()
					readerErr = err
					readerMu.Unlock()
					return
				}
				value := got["MyAnimeListClientID"]
				// Any read must see one writer's complete value, never a mixture
				// or a truncation.
				if value != strings.Repeat("x", 4096) && value != strings.Repeat("y", 8192) {
					readerMu.Lock()
					readerErr = fmt.Errorf("torn read: value length %d", len(value))
					readerMu.Unlock()
					return
				}
			}
		}()
	}

	for i := 0; i < 40; i++ {
		fill := "x"
		size := 4096
		if i%2 == 1 {
			fill, size = "y", 8192
		}
		if err := SaveConfigToFile(path, map[string]string{"MyAnimeListClientID": strings.Repeat(fill, size)}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()

	readerMu.Lock()
	defer readerMu.Unlock()
	if readerErr != nil {
		t.Fatal(readerErr)
	}
}
