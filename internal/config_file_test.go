package internal

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseStringArray(t *testing.T) {
	for in, want := range map[string][]string{
		`["--fs", "--volume=50"]`: {"--fs", "--volume=50"},
		`[--fs, "--mute"]`:        {"--fs", "--mute"},
		`[]`:                      nil,
		``:                        nil,
		`[ , a ]`:                 {"a"},
	} {
		if got := parseStringArray(in); !reflect.DeepEqual(got, want) {
			t.Errorf("parseStringArray(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadConfigCreatesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "otakase.conf")
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Player != "mpv" || !config.SkipOp || config.PercentageToMarkComplete != 85 {
		t.Errorf("defaults not applied: %+v", config)
	}
	onDisk, err := LoadConfigFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(onDisk) != len(defaultConfigMap()) {
		t.Errorf("created file has %d keys, want %d", len(onDisk), len(defaultConfigMap()))
	}
}

func TestLoadConfigFillsGapsWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "otakase.conf")
	body := "# comment\nSkipOp = false\nPercentageToMarkComplete=150\nMpvArgs=[\"--fs\"]\nMpvPlaybackStartTimeout=-3\n"
	os.WriteFile(path, []byte(body), 0o644)

	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.SkipOp || !config.SkipEd {
		t.Errorf("SkipOp=%v SkipEd=%v", config.SkipOp, config.SkipEd)
	}
	if config.PercentageToMarkComplete != 100 || config.MpvPlaybackStartTimeout != MpvStartWaitSeconds {
		t.Errorf("bounds not applied: %d %d", config.PercentageToMarkComplete, config.MpvPlaybackStartTimeout)
	}
	if !reflect.DeepEqual(config.MpvArgs, []string{"--fs"}) {
		t.Errorf("MpvArgs = %q", config.MpvArgs)
	}
	if got, _ := os.ReadFile(path); string(got) != body {
		t.Errorf("an ordinary load rewrote the file:\n%s", got)
	}
}

func TestSaveConfigToFileSorted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.conf")
	if err := SaveConfigToFile(path, map[string]string{"b": "2", "a": "1"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "a=1\nb=2\n" {
		t.Errorf("file = %q", got)
	}
}
