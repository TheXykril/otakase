package internal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoteSyncWriteGate(t *testing.T) {
	anilist := &Config{TrackingRemote: TrackingRemoteAniList}
	local := &Config{TrackingRemote: TrackingRemoteNone}

	for _, tc := range []struct {
		name  string
		cfg   *Config
		anime *Anime
		want  bool
	}{
		{"remote", anilist, &Anime{}, true},
		{"skipped this session", anilist, &Anime{SkipRemoteSync: true}, false},
		{"local only", local, &Anime{}, false},
	} {
		if got := ShouldWriteRemoteTracking(tc.cfg, tc.anime); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRemoteSyncDecliningImportOnlyDismisses(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "otakase.conf")
	if err := createDefaultConfig(confPath); err != nil {
		t.Fatal(err)
	}
	savedPath := ConfigFilePath
	ConfigFilePath = confPath
	t.Cleanup(func() { ConfigFilePath = savedPath })

	if err := os.WriteFile(filepath.Join(dir, "anilist_token.json"), []byte("tok"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		StoragePath:        dir,
		TrackingRemote:     TrackingRemoteMyAnimeList,
		TrackingConfigured: true,
	}
	withPromptSelect(t, func([]SelectionOption) (SelectionOption, error) {
		return SelectionOption{Key: "no"}, nil
	})

	if err := maybeImportAniListToMyAnimeList(cfg, &User{}); err != nil {
		t.Fatal(err)
	}
	if cfg.MyAnimeListImported {
		t.Error("import marked done after saying no")
	}
	if !cfg.MyAnimeListImportDismissed {
		t.Error("prompt not marked dismissed")
	}
}

func TestRemoteSyncBackupHoldsBothLists(t *testing.T) {
	cfg := &Config{StoragePath: t.TempDir()}
	al := AnimeList{Watching: []Entry{{Media: Media{ID: 5, MalID: 50}, Status: "CURRENT"}}}
	mal := AnimeList{Dropped: []Entry{{Media: Media{ID: 6, MalID: 60}, Status: "DROPPED"}}}

	path, err := writeTrackingBackup(cfg, "before merge", al, mal)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("backup is not JSON: %v", err)
	}
	if doc["action"] != "before merge" {
		t.Errorf("action = %v", doc["action"])
	}
	seen := map[float64]bool{}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if id, ok := x["id"].(float64); ok {
				seen[id] = true
			}
			for _, c := range x {
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(doc)
	if !seen[5] || !seen[6] {
		t.Errorf("backup ids %v, want 5 and 6", seen)
	}
}

// the date cases myanimelist_test.go doesn't already cover
func TestRemoteSyncMALDateOutOfRange(t *testing.T) {
	for _, in := range []string{"bad", "2026-13-01", "2026-01-00"} {
		if got := parseMyAnimeListDate(in); got != (FuzzyDate{}) {
			t.Errorf("parseMyAnimeListDate(%q) = %+v, want zero", in, got)
		}
	}
}

func TestRemoteSyncEntryMapDropsUnknownIDs(t *testing.T) {
	byID := mapEntriesByMediaID(AnimeList{Paused: []Entry{
		{Media: Media{ID: 0}, Progress: 4},
		{Media: Media{ID: 42}, Progress: 9},
	}})
	if len(byID) != 1 || byID[42].Progress != 9 {
		t.Fatalf("got %#v", byID)
	}
}
