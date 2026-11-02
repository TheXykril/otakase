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
