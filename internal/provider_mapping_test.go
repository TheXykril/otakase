package internal

import (
	"errors"
	"testing"

	_ "github.com/thexykril/otakase/internal/loadproviders"
)

func TestProviderMappingSearchStateNextProvider(t *testing.T) {
	t.Run("single provider hides next option", func(t *testing.T) {
		state := &providerMappingSearchState{allProviders: []string{"anineko"}}
		if got := state.nextProviderLabel(); got != "" {
			t.Fatalf("nextProviderLabel() = %q, want empty", got)
		}
		if state.advanceToNextProvider() {
			t.Fatal("advanceToNextProvider() = true, want false")
		}
	})

	t.Run("stacked search offers first provider", func(t *testing.T) {
		state := &providerMappingSearchState{allProviders: []string{"senshi", "anineko", "anipub"}}
		if got := state.nextProviderLabel(); got != "senshi" {
			t.Fatalf("nextProviderLabel() = %q, want senshi", got)
		}
	})

	t.Run("sequential advance walks stack", func(t *testing.T) {
		state := &providerMappingSearchState{allProviders: []string{"senshi", "anineko", "anipub"}}
		if !state.advanceToNextProvider() {
			t.Fatal("expected first advance to succeed")
		}
		if !state.sequential || state.providerIndex != 0 {
			t.Fatalf("expected sequential at index 0, got sequential=%v index=%d", state.sequential, state.providerIndex)
		}
		if got := state.nextProviderLabel(); got != "anineko" {
			t.Fatalf("nextProviderLabel() = %q, want anineko", got)
		}
		if !state.advanceToNextProvider() || state.providerIndex != 1 {
			t.Fatalf("expected index 1 after second advance, got %d", state.providerIndex)
		}
		if got := state.nextProviderLabel(); got != "anipub" {
			t.Fatalf("nextProviderLabel() = %q, want anipub", got)
		}
		if !state.advanceToNextProvider() || state.providerIndex != 2 {
			t.Fatalf("expected index 2 after third advance, got %d", state.providerIndex)
		}
		if got := state.nextProviderLabel(); got != "" {
			t.Fatalf("nextProviderLabel() = %q, want empty at end", got)
		}
		if state.advanceToNextProvider() {
			t.Fatal("expected final advance to fail")
		}
	})
}

func TestProviderNameFromSelectionUsesSequentialProvider(t *testing.T) {
	withAllProvidersEnabledForTest(t)
	config := &CurdConfig{Provider: `["senshi","anineko"]`}
	state := &providerMappingSearchState{
		allProviders:  []string{"senshi", "anineko"},
		sequential:    true,
		providerIndex: 1,
	}

	selected := SelectionOption{
		Key:   "frieren-beyond-journeys-end",
		Label: "Frieren: Beyond Journey's End",
		Title: "Frieren: Beyond Journey's End",
	}
	if got := providerNameFromSelection(config, state, selected); got != "anineko" {
		t.Fatalf("providerNameFromSelection() = %q, want anineko", got)
	}

	var anime Anime
	applySelectedProviderMapping(config, state, &anime, selected)
	if anime.ProviderName != "anineko" || anime.ProviderId != "frieren-beyond-journeys-end" {
		t.Fatalf("unexpected mapping %+v", anime)
	}
}

func TestProviderNameFromSelectionUsesQualifiedKey(t *testing.T) {
	withAllProvidersEnabledForTest(t)
	config := &CurdConfig{Provider: `["senshi","anineko"]`}
	state := &providerMappingSearchState{allProviders: []string{"senshi", "anineko"}}

	selected := SelectionOption{
		Key:   "anineko::frieren-beyond-journeys-end",
		Label: "Frieren: Beyond Journey's End [anineko]",
	}
	if got := providerNameFromSelection(config, state, selected); got != "anineko" {
		t.Fatalf("providerNameFromSelection() = %q, want anineko", got)
	}
}

func TestApplyMatchedProviderMappingUsesSequentialProvider(t *testing.T) {
	withAllProvidersEnabledForTest(t)
	config := &CurdConfig{Provider: `["senshi","anineko"]`}
	state := &providerMappingSearchState{
		allProviders:  []string{"senshi", "anineko"},
		sequential:    true,
		providerIndex: 1,
	}

	anime := Anime{ProviderId: "frieren-beyond-journeys-end"}
	applyMatchedProviderMapping(config, state, &anime)
	if anime.ProviderName != "anineko" || anime.ProviderId != "frieren-beyond-journeys-end" {
		t.Fatalf("unexpected mapping %+v", anime)
	}
}

// Backing out of "search under a different name" leaves the search as it was.
// The question is one keypress from a list of results, and changing your mind
// about it should not clear the name that got you there.
func TestCustomProviderQueryCancelKeepsTheCurrentQuery(t *testing.T) {
	query, cancelled, err := customProviderQueryFromAnswer("frieren", func() (string, bool, error) {
		return "", true, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cancelled {
		t.Error("cancelling the prompt should be reported as cancelled")
	}
	if query != "frieren" {
		t.Errorf("cancelling changed the query to %q", query)
	}
}

func TestCustomProviderQueryUsesTheAnswer(t *testing.T) {
	query, cancelled, err := customProviderQueryFromAnswer("frieren", func() (string, bool, error) {
		return "sousou no frieren", false, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cancelled {
		t.Error("an answered prompt should not be reported as cancelled")
	}
	if query != "sousou no frieren" {
		t.Errorf("the answer was not used, got %q", query)
	}
}

// A prompt that fails is not a prompt that was answered with nothing: the
// caller reports the failure, and the query it already had survives it.
func TestCustomProviderQueryKeepsTheQueryOnError(t *testing.T) {
	failed := errors.New("no terminal")
	query, cancelled, err := customProviderQueryFromAnswer("frieren", func() (string, bool, error) {
		return "", false, failed
	})
	if !errors.Is(err, failed) {
		t.Fatalf("expected the prompt error back, got %v", err)
	}
	if cancelled {
		t.Error("an error is not a cancellation")
	}
	if query != "frieren" {
		t.Errorf("the query was lost, got %q", query)
	}
}
