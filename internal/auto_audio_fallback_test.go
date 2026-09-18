package internal

import (
	"testing"
)

// dubOnlyStack is a provider stack that carries only the dub of a show, the
// mirror of the real case: a sub-only show watched with SubOrDub=dub.
func dubOnlyStack(t *testing.T) (CurdConfig, *Anime) {
	t.Helper()
	provider := &stackStubProvider{
		name: "anipub",
		episodeResults: map[string]map[string][]string{
			"anipub-id": {"dub": {"anipub-dub"}},
		},
		searchResults: map[string][]SelectionOption{
			"dub": {{Title: "Example", Key: "anipub-id"}},
			"sub": {{Title: "Example", Key: "anipub-id"}},
		},
	}
	withProviderFactories(t, provider)

	config := CurdConfig{Provider: `["anipub"]`, SubOrDub: "sub", AutoAudioFallback: true}
	anime := &Anime{
		Title:        AnimeTitle{Romaji: "Example"},
		ProviderName: "anipub",
		ProviderId:   "anipub-id",
	}
	return config, anime
}

// With AutoAudioFallback on, a show that exists only in the other language plays
// straight away. It used to take two menus to get there -- a recovery menu, then
// a confirmation -- for a choice with exactly one useful answer.
func TestAutoAudioFallbackPlaysWithoutPrompting(t *testing.T) {
	config, anime := dubOnlyStack(t)

	withPromptSelect(t, func(options []SelectionOption) (SelectionOption, error) {
		t.Fatalf("no prompt should be shown; got %v", options)
		return SelectionOption{}, nil
	})

	result, err := ResolveEpisodeURLAlternateModeAuto(config, anime, 1, nil)
	if err != nil {
		t.Fatalf("expected the alternate mode to play automatically, got %v", err)
	}
	if result.Mode != "dub" || len(result.Links) == 0 || result.Links[0] != "anipub-dub" {
		t.Fatalf("expected the dub stream, got %#v", result)
	}
}

// The whole recovery path must reach that stream on its own, without the user
// answering anything.
func TestRecoveryAutoFallsBackBeforeAskingAnything(t *testing.T) {
	config, anime := dubOnlyStack(t)

	withPromptSelect(t, func(options []SelectionOption) (SelectionOption, error) {
		t.Fatalf("recovery should not have needed a prompt; got %v", options)
		return SelectionOption{}, nil
	})

	anime.Ep.Number = 1
	result, ok := resolveEpisodeLinksWithRecovery(&config, anime, nil)
	if !ok {
		t.Fatal("expected recovery to resolve via the alternate audio")
	}
	if result.Mode != "dub" || len(result.Links) == 0 {
		t.Fatalf("expected the dub stream, got %#v", result)
	}
	// The mapping the fallback actually used must be what gets recorded.
	if anime.ProviderName != result.ProviderName {
		t.Fatalf("anime should carry the provider that served it: %q vs %q",
			anime.ProviderName, result.ProviderName)
	}
}

// Opting out restores the ask-first behaviour.
func TestAutoAudioFallbackCanBeDisabled(t *testing.T) {
	config, anime := dubOnlyStack(t)
	config.AutoAudioFallback = false

	asked := 0
	withPromptSelect(t, func(options []SelectionOption) (SelectionOption, error) {
		asked++
		return SelectionOption{Key: "-2", Label: "Back"}, nil
	})

	anime.Ep.Number = 1
	if _, ok := resolveEpisodeLinksWithRecovery(&config, anime, nil); ok {
		t.Fatal("expected backing out of the recovery menu to give up")
	}
	if asked == 0 {
		t.Fatal("expected the recovery menu to be shown when the option is off")
	}
}

// The default must be on: a show that only exists in one language should play.
func TestAutoAudioFallbackDefaultsOn(t *testing.T) {
	if defaultConfigMap()["AutoAudioFallback"] != "true" {
		t.Fatalf("expected AutoAudioFallback to default on, got %q",
			defaultConfigMap()["AutoAudioFallback"])
	}
}
