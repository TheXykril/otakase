package internal

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func optionKeys(options []SelectionOption) []string {
	keys := make([]string, len(options))
	for i, option := range options {
		keys[i] = option.Key
	}
	return keys
}

// promptKind names a menu by its first entry: the audio fallback question
// starts with "play", the recovery menu with "remap".
func promptKind(t *testing.T, options []SelectionOption) string {
	t.Helper()
	switch {
	case len(options) > 0 && options[0].Key == "play":
		return "alternate-audio"
	case len(options) > 0 && options[0].Key == "remap":
		return "recovery"
	}
	t.Fatalf("unexpected menu %v", optionKeys(options))
	return ""
}

// anikotoExample is a show mapped to anikoto under "anikoto-id", on episode.
func anikotoExample(episode int) *Anime {
	return &Anime{
		Title:        AnimeTitle{Romaji: "Example"},
		ProviderName: "anikoto",
		ProviderId:   "anikoto-id",
		Ep:           Episode{Number: episode},
	}
}

func TestEpisodeLinkFailureMenuReadsWell(t *testing.T) {
	menu := episodeLinkFailureRecoveryOptions("sub", true)
	if keys := optionKeys(menu); !slices.Equal(keys, []string{"remap", "audio", "episode"}) {
		t.Fatalf("entries %v", keys)
	}
	if audio := menu[1].Label; !strings.Contains(audio, "dub") {
		t.Fatalf("the audio entry does not name the other audio: %q", audio)
	}
	if episode := strings.ToLower(menu[2].Label); !strings.Contains(episode, "wrong") {
		t.Fatalf("the episode entry does not say it is for a wrong number: %q", episode)
	}
	for _, entry := range menu {
		if strings.HasPrefix(entry.Label, "1.") || strings.HasPrefix(entry.Label, "2.") {
			t.Fatalf("entries are numbered: %q", entry.Label)
		}
	}

	if keys := optionKeys(episodeLinkFailureRecoveryOptions("dub", false)); !slices.Equal(keys, []string{"remap", "episode"}) {
		t.Fatalf("without audio: %v", keys)
	}
}

func TestEpisodeLinkFailureDiagnosisNamesEpisodeAudioAndProviders(t *testing.T) {
	withAllProvidersEnabledForTest(t)
	cfg := &Config{Provider: `["anizone","anikoto"]`, SubOrDub: "sub"}
	frieren := &Anime{Title: AnimeTitle{English: "Frieren: Beyond Journey's End"}, Ep: Episode{Number: 12}, ProviderName: "anizone"}

	msg := episodeLinkFailureDiagnosis(cfg, frieren, errors.New("no sub episode links found across providers"))
	want := "Couldn't find Episode 12 (sub) for \"Frieren: Beyond Journey's End\"\n" +
		"Tried: anizone, anikoto\n" +
		"Reason: no sub episode links found across providers"
	if msg != want {
		t.Fatalf("diagnosis:\n%s\nwant:\n%s", msg, want)
	}
}

func TestEpisodeLinkFailureDiagnosisShortensTheReason(t *testing.T) {
	msg := episodeLinkFailureDiagnosis(nil, nil, errors.New(strings.Repeat("x", 300)))
	reason := msg[strings.LastIndex(msg, "Reason: ")+len("Reason: "):]
	if len(reason) != 160 || !strings.HasSuffix(reason, "...") {
		t.Fatalf("reason is %d characters: %q", len(reason), reason)
	}
	if !strings.HasPrefix(msg, `Couldn't find Episode 0 (sub) for "this anime"`) {
		t.Fatalf("diagnosis without an anime: %q", msg)
	}
}

func TestEpisodeRecoveryMenuFoldsItsExitsIntoBackAndQuit(t *testing.T) {
	cfg := &Config{SubOrDub: "sub", Provider: `["anikoto"]`}
	anime := anikotoExample(1)

	for key, want := range map[string]string{"-2": "back", "-1": "quit", "": "back", "unknown": "back", "audio": "audio"} {
		withPromptSelect(t, func([]SelectionOption) (SelectionOption, error) {
			return SelectionOption{Key: key}, nil
		})
		if got := promptEpisodeLinkFailureRecovery(cfg, anime, errors.New("fail"), true); got != want {
			t.Errorf("%q: got %q, want %q", key, got, want)
		}
	}
	withPromptSelect(t, func([]SelectionOption) (SelectionOption, error) {
		return SelectionOption{}, errors.New("no terminal")
	})
	if got := promptEpisodeLinkFailureRecovery(cfg, anime, nil, false); got != "back" {
		t.Errorf("a broken menu: got %q, want back", got)
	}
}

func TestEpisodeThatResolvesShowsNoMenu(t *testing.T) {
	stub := &stackStubProvider{
		name:           "anikoto",
		episodeResults: map[string]map[string][]string{"anikoto-id": {"sub": {"sub-url"}}},
	}
	withProviderFactories(t, stub)
	withPromptSelect(t, func(options []SelectionOption) (SelectionOption, error) {
		t.Fatalf("a menu opened though the episode resolved: %v", optionKeys(options))
		return SelectionOption{}, nil
	})

	cfg := &Config{Provider: `["anikoto"]`, SubOrDub: "sub"}
	got, ok := resolveEpisodeLinksWithRecovery(cfg, anikotoExample(1), nil)
	if !ok || got.Links[0] != "sub-url" || got.Mode != "sub" {
		t.Fatalf("got ok=%v %#v", ok, got)
	}
	if _, ok := resolveEpisodeLinksWithRecovery(nil, anikotoExample(1), nil); ok {
		t.Fatal("resolved without a config")
	}
}

// The automatic switch to the other audio is tried once. When it finds
// nothing the menu stops offering that audio too.
func TestEpisodeRecoveryAutoAudioRunsOnce(t *testing.T) {
	withProviderFactories(t, &stackStubProvider{
		name:          "anikoto",
		episodeErrors: map[string]map[string]error{"anikoto-id": {"sub": errors.New("no sub"), "dub": errors.New("no dub")}},
	})

	off := &episodeRecovery{config: &Config{Provider: `["anikoto"]`, SubOrDub: "sub"}, anime: anikotoExample(1), offerAudio: true}
	if _, ok := off.autoAudio(); ok || off.triedAutoAudio || !off.offerAudio {
		t.Fatalf("ran with AutoAudioFallback off: %+v", off)
	}

	cfg := &Config{Provider: `["anikoto"]`, SubOrDub: "sub", AutoAudioFallback: true}
	rec := &episodeRecovery{config: cfg, anime: anikotoExample(1), offerAudio: true}
	if _, ok := rec.autoAudio(); ok {
		t.Fatal("found a dub that is not there")
	}
	if rec.offerAudio || !rec.triedAutoAudio {
		t.Fatalf("after a fruitless fallback: %+v", rec)
	}
	rec.offerAudio = true
	if _, ok := rec.autoAudio(); ok || !rec.offerAudio {
		t.Fatal("the automatic fallback ran a second time")
	}
}

// With nothing in either audio there is nothing to ask about, so the
// recovery menu comes straight after the preferred audio failed.
func TestResolveEpisodeLinksWithRecoverySkipsTheAudioQuestionWithNoOtherAudio(t *testing.T) {
	stub := &stackStubProvider{
		name:          "anikoto",
		episodeErrors: map[string]map[string]error{"anikoto-id": {"sub": errors.New("no sub"), "dub": errors.New("no dub")}},
	}
	withProviderFactories(t, stub)

	var shown []string
	withPromptSelect(t, func(options []SelectionOption) (SelectionOption, error) {
		kind := promptKind(t, options)
		shown = append(shown, kind)
		if kind != "recovery" {
			return SelectionOption{Key: "cancel"}, nil
		}
		if keys := optionKeys(options); !slices.Equal(keys, []string{"remap", "audio", "episode"}) {
			t.Fatalf("recovery menu entries: %v", keys)
		}
		return SelectionOption{Key: "back"}, nil
	})

	cfg := &Config{Provider: `["anikoto","no-kickassanime"]`, SubOrDub: "sub"}
	if got, ok := resolveEpisodeLinksWithRecovery(cfg, anikotoExample(3), nil); ok || len(got.Links) > 0 {
		t.Fatalf("expected a back-out, got ok=%v %#v", ok, got)
	}
	if !slices.Equal(shown, []string{"recovery"}) {
		t.Fatalf("menus shown: %v", shown)
	}
	if len(stub.calls) == 0 || stub.calls[0] != "anikoto:sub" {
		t.Fatalf("expected the preferred sub first, got %v", stub.calls)
	}
}

func TestResolveEpisodeLinksWithRecoveryAsksAboutOtherAudioFirst(t *testing.T) {
	withProviderFactories(t, &stackStubProvider{
		name:           "anikoto",
		episodeResults: map[string]map[string][]string{"anikoto-id": {"dub": {"dub-url"}}},
		episodeErrors:  map[string]map[string]error{"anikoto-id": {"sub": errors.New("no sub")}},
	})

	var shown []string
	withPromptSelect(t, func(options []SelectionOption) (SelectionOption, error) {
		kind := promptKind(t, options)
		shown = append(shown, kind)
		answer := map[string]string{"recovery": "back", "alternate-audio": "cancel"}[kind]
		return SelectionOption{Key: answer}, nil
	})

	cfg := &Config{Provider: `["anikoto"]`, SubOrDub: "sub"}
	if _, ok := resolveEpisodeLinksWithRecovery(cfg, anikotoExample(4), nil); ok {
		t.Fatal("expected a back-out after declining the dub")
	}
	if !slices.Equal(shown, []string{"alternate-audio", "recovery"}) {
		t.Fatalf("menus shown: %v", shown)
	}
}

// Declining the dub when it is first offered does not rule it out: the
// recovery menu offers it again, and accepting there plays it.
func TestResolveEpisodeLinksWithRecoveryOffersAudioAgain(t *testing.T) {
	withProviderFactories(t, &stackStubProvider{
		name:           "anikoto",
		episodeResults: map[string]map[string][]string{"anikoto-id": {"dub": {"dub-url"}}},
		episodeErrors:  map[string]map[string]error{"anikoto-id": {"sub": errors.New("no sub")}},
	})

	answers := []struct{ kind, key string }{
		{"alternate-audio", "cancel"},
		{"recovery", "audio"},
		{"alternate-audio", "play"},
	}
	step := 0
	withPromptSelect(t, func(options []SelectionOption) (SelectionOption, error) {
		if step >= len(answers) {
			t.Fatalf("unexpected extra menu %v", optionKeys(options))
		}
		want := answers[step]
		step++
		if kind := promptKind(t, options); kind != want.kind {
			t.Fatalf("menu %d is %s, want %s", step, kind, want.kind)
		}
		return SelectionOption{Key: want.key}, nil
	})

	cfg := &Config{Provider: `["anikoto"]`, SubOrDub: "sub"}
	got, ok := resolveEpisodeLinksWithRecovery(cfg, anikotoExample(1), nil)
	if !ok || got.Mode != "dub" || got.Links[0] != "dub-url" {
		t.Fatalf("got ok=%v %#v, want the dub", ok, got)
	}
}
