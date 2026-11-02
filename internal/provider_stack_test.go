package internal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Sources are always picked automatically: the old settings that chose,
// turned off or hand-searched them are dropped from the file and ignored.
func TestLoadConfigRetiresTheSourceSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "otakase.conf")
	old := "Provider=anikoto\nDisabledProviders=[\"nyaa\"]\nManualProviderSearch=true\nMovieProvider=vidsrc\nAddMissingOptions=true\n"
	if err := os.WriteFile(path, []byte(old), 0644); err != nil {
		t.Fatalf("seeding the config: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if loaded.Provider != "stacked" || loaded.DisabledProviders != "[]" || loaded.ManualProviderSearch || loaded.MovieProvider != "all" {
		t.Fatalf("got Provider=%q DisabledProviders=%q ManualProviderSearch=%v MovieProvider=%q, want automatic",
			loaded.Provider, loaded.DisabledProviders, loaded.ManualProviderSearch, loaded.MovieProvider)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the config back: %v", err)
	}
	for _, key := range []string{"Provider=", "DisabledProviders=", "ManualProviderSearch=", "MovieProvider="} {
		if strings.Contains(string(saved), key) {
			t.Fatalf("%s left in the file:\n%s", key, saved)
		}
	}
}

func TestProviderQualifiedIDsRoundTrip(t *testing.T) {
	if got := QualifyProviderID("AniKoto", "21"); got != "anikoto::21" {
		t.Fatalf("QualifyProviderID = %q", got)
	}
	if got := QualifyProviderID("anikoto", "anikoto::21"); got != "anikoto::21" {
		t.Fatalf("an id already qualified for its provider was qualified again: %q", got)
	}
	if got := QualifyProviderID("nonsense", "21"); got != "21" {
		t.Fatalf("an unknown provider qualified the id: %q", got)
	}
	if got := QualifyProviderID("anikoto", ""); got != "" {
		t.Fatalf("an empty id was qualified: %q", got)
	}

	for _, tc := range []struct {
		id, name, raw string
		ok            bool
	}{
		{id: "kickassanime::frieren::s2", name: "kickassanime", raw: "frieren::s2", ok: true},
		{id: "frieren", raw: "frieren"},
		{id: "nonsense::21", raw: "nonsense::21"},
		{id: "kickassanime::", raw: "kickassanime::"},
	} {
		name, raw, ok := ParseProviderQualifiedID(tc.id)
		if name != tc.name || raw != tc.raw || ok != tc.ok {
			t.Errorf("%q: got (%q, %q, %v)", tc.id, name, raw, ok)
		}
	}
}

func TestProviderIDForAnimePrefersTheQualifiedID(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	anime := &Anime{ProviderName: "kickassanime", ProviderId: "anizone::slug"}
	if name, id := AnimeProviderID(anime); name != "anizone" || id != "slug" {
		t.Fatalf("got (%q, %q), want the provider the id names", name, id)
	}
	anime = &Anime{ProviderName: "KickAssAnime", ProviderId: "42"}
	if name, id := AnimeProviderID(anime); name != "kickassanime" || id != "42" {
		t.Fatalf("got (%q, %q)", name, id)
	}
}

func TestQualifySelectionOptionTagsTheLabelOnce(t *testing.T) {
	withAllProvidersEnabledForTest(t)
	tag := sourceTag("kickassanime")
	if !strings.HasPrefix(tag, "[source ") || strings.Contains(tag, "kickass") {
		t.Fatalf("tag %q names the site or has no number", tag)
	}
	got := qualifySelectionOption("kickassanime", SelectionOption{Key: "7", Label: "Show"})
	if got.Key != "kickassanime::7" || got.Label != "Show "+tag {
		t.Fatalf("got %+v", got)
	}
	again := qualifySelectionOption("kickassanime", got)
	if again.Key != "kickassanime::7" || again.Label != "Show "+tag {
		t.Fatalf("qualifying twice changed the option: %+v", again)
	}
}

func TestProviderNamesForAnimeAppendsTheStoredProvider(t *testing.T) {
	withAllProvidersEnabledForTest(t)

	cfg := &Config{Provider: `["anizone","kickassanime"]`}
	got := providerNamesForAnime(cfg, &Anime{ProviderName: "nyaa", ProviderId: "x"})
	if want := []string{"anizone", "kickassanime", "nyaa"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	got = providerNamesForAnime(cfg, &Anime{ProviderName: "kickassanime", ProviderId: "x"})
	if want := []string{"anizone", "kickassanime"}; !slices.Equal(got, want) {
		t.Fatalf("a stored provider already in the stack was added again: %v", got)
	}
}

func TestFilterExcludedProviders(t *testing.T) {
	kept := filterExcludedProviders([]string{"anizone", "anikoto", "kickassanime"}, []string{"anizone", "ANIZONE"})
	if want := []string{"anikoto", "kickassanime"}; !slices.Equal(kept, want) {
		t.Fatalf("kept %v, want %v", kept, want)
	}
}

// The recovery menu stops offering the other audio on this error, so it has
// to survive wrapping and keep the text it has always had.
func TestNoStreamsErrorIsRecognisedWhenWrapped(t *testing.T) {
	err := fmt.Errorf("resolve: %w", noStreamsError{mode: "dub"})
	if !isNoStreamsError(err) {
		t.Fatal("a wrapped noStreamsError was not recognised")
	}
	if !strings.HasSuffix(err.Error(), "no dub streams available") {
		t.Fatalf("unexpected text %q", err.Error())
	}
	if isNoStreamsError(errors.New("no dub streams available")) {
		t.Fatal("a plain error with the same text is not a noStreamsError")
	}
}
