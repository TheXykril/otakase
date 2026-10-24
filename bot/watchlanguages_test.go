package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWatchValidLanguage(t *testing.T) {
	cases := []struct {
		value string
		subtitles bool
		valid bool
	}{
		{"auto", false, true},
		{"auto", true, true},
		{"off", true, true},
		{"off", false, false},
		{"en", false, true},
		{"eng", false, true},
		{"pt-br", true, true},
		{"track-12", false, true},
		{"hls-0", false, true},
		{"track-abc", false, false},
		{"hls-1234", false, false},
		{"", false, false},
		{"<script>", true, false},
		{"en/../../etc", false, false},
	}
	for _, tc := range cases {
		if got := watchValidLanguage(tc.value, tc.subtitles); got != tc.valid {
			t.Errorf("watchValidLanguage(%q, %v) = %v, want %v", tc.value, tc.subtitles, got, tc.valid)
		}
	}
}

func TestWatchPreferredLanguage(t *testing.T) {
	for _, code := range []string{"auto", "en", "ja", "pt-br"} {
		if !watchPreferredLanguage(code, false) { t.Errorf("audio %q rejected", code) }
	}
	for _, code := range []string{"off", "track-1", "hls-2", "invalid/code", ""} {
		if watchPreferredLanguage(code, false) { t.Errorf("audio %q accepted", code) }
	}
	if !watchPreferredLanguage("off", true) {
		t.Fatal("subtitles off was rejected")
	}
	got := watchNormalizePreferences(watchPlaybackPreferences{Audio: "en", BackupAudio: "en", Subtitles: "off"})
	if got.Audio != "en" || got.BackupAudio != "auto" || got.Subtitles != "off" {
		t.Fatalf("duplicate backup not normalized: %+v", got)
	}
}

func TestWatchPreferencesAccountDefaults(t *testing.T) {
	b := testGamesBot(t)
	// Existing per-player language overrides are reset when new personal
	// Automatic preferences are explicitly saved.
	b.store.update(func(d *storeData) {
		d.WatchLanguages = map[string]watchLanguagePreference{
			"a": {Audio: "ja", Subtitles: "off"},
		}
	})
	var saved watchPlaybackPreferences
	in := watchPlaybackPreferences{Audio: "fr", BackupAudio: "en", Subtitles: "ja"}
	if code := togetherPost(b, (*bot).watchPreferencesSet, "a", in, &saved); code != 200 {
		t.Fatalf("save returned %d", code)
	}
	if saved != in { t.Fatalf("saved %+v, want %+v", saved, in) }
	var other watchPlaybackPreferences
	r := httptest.NewRequest("GET", "/activity/api/watch/preferences", nil)
	r.Header.Set("Authorization", "Bearer "+b.acts.add("b"))
	w := httptest.NewRecorder()
	b.watchPreferencesGet(w, r)
	if err := json.Unmarshal(w.Body.Bytes(), &other); err != nil { t.Fatal(err) }
	if other != watchDefaultPreferences() { t.Fatalf("other member inherited preferences: %+v", other) }
	b.store.view(func(d *storeData) {
		if d.WatchPreferences["a"] != in { t.Errorf("stored %+v", d.WatchPreferences["a"]) }
		if got := d.WatchLanguages["a"]; got.Audio != "auto" || got.Subtitles != "auto" {
			t.Errorf("manual choice not reset: %+v", got)
		}
	})
	if code := togetherPost(b, (*bot).watchPreferencesSet, "a",
		watchPlaybackPreferences{Audio: "off", BackupAudio: "ja", Subtitles: "auto"}, nil); code != 400 {
		t.Errorf("invalid audio preference accepted, status %d", code)
	}
}
