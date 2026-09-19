package internal

import (
	"path/filepath"
	"testing"
)

// The persisted id is what SetupAnimeSkipClientID writes after the prompt --
// this is the half of the flow that runs without a terminal or a browser, and
// so the only half worth pinning with a test.
func TestPersistAnimeSkipClientIDWritesIntoTheExistingConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "otakase.conf")
	if err := createDefaultConfig(configPath); err != nil {
		t.Fatalf("create config: %v", err)
	}

	previousConfigPath := GlobalConfigPath
	GlobalConfigPath = configPath
	t.Cleanup(func() { GlobalConfigPath = previousConfigPath })

	if err := persistAnimeSkipClientID("a-real-client-id"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	saved, err := LoadConfigFromFile(configPath)
	if err != nil {
		t.Fatalf("read config back: %v", err)
	}
	if saved["AnimeSkipClientID"] != "a-real-client-id" {
		t.Errorf("AnimeSkipClientID = %q", saved["AnimeSkipClientID"])
	}
	// Everything else that was already in the file must survive: this writes
	// one setting, not the whole config.
	if _, stillPresent := saved["StoragePath"]; !stillPresent {
		t.Error("an unrelated setting was lost")
	}
}

// A personal id pasted in has to win over whatever was there before -- "auto"
// included -- since replacing it is the whole point of running setup.
func TestPersistAnimeSkipClientIDOverwritesWhatWasThereBefore(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "otakase.conf")
	if err := createDefaultConfig(configPath); err != nil {
		t.Fatalf("create config: %v", err)
	}

	previousConfigPath := GlobalConfigPath
	GlobalConfigPath = configPath
	t.Cleanup(func() { GlobalConfigPath = previousConfigPath })

	if err := persistAnimeSkipClientID("auto"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := persistAnimeSkipClientID("a-personal-id"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	saved, err := LoadConfigFromFile(configPath)
	if err != nil {
		t.Fatalf("read config back: %v", err)
	}
	if saved["AnimeSkipClientID"] != "a-personal-id" {
		t.Errorf("AnimeSkipClientID = %q, want the later value to win", saved["AnimeSkipClientID"])
	}
}

// Without GlobalConfigPath set there is nowhere to write, and that must not be
// an error: it is the same "nothing to persist to yet" case persistTrackingConfig
// already treats as a no-op.
func TestPersistAnimeSkipClientIDDoesNothingWithoutAConfigPath(t *testing.T) {
	previousConfigPath := GlobalConfigPath
	GlobalConfigPath = ""
	t.Cleanup(func() { GlobalConfigPath = previousConfigPath })

	if err := persistAnimeSkipClientID("a-personal-id"); err != nil {
		t.Errorf("unexpected error with no config path: %v", err)
	}
}

func TestSetupAnimeSkipClientIDRejectsAMissingConfig(t *testing.T) {
	if err := SetupAnimeSkipClientID(nil); err == nil {
		t.Error("a nil config should be refused before anything opens a browser")
	}
}
