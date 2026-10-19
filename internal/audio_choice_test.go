package internal

import (
	"errors"
	"slices"
	"testing"
)

func TestAudioChoicePreferredModeOnly(t *testing.T) {
	p := &scriptedProvider{links: map[string][]string{"sub": {"s.m3u8"}, "dub": {"d.m3u8"}}}
	useProvider(t, p)

	links, mode, err := GetEpisodeURLForPlayback(Config{SubOrDub: "sub"}, "show", 3)
	if err != nil {
		t.Fatal(err)
	}
	if mode != "sub" || !slices.Equal(links, []string{"s.m3u8"}) {
		t.Errorf("got %q %v", mode, links)
	}
	if !slices.Equal(p.linkCalls, []string{"sub"}) {
		t.Errorf("lookups = %v, dub should not be touched", p.linkCalls)
	}
}

func TestAudioChoiceFallbackNeedsConsent(t *testing.T) {
	for _, tc := range []struct {
		answer    string
		wantMode  string
		wantLinks []string
		wantErr   bool
	}{
		{answer: "play", wantMode: "dub", wantLinks: []string{"d.m3u8"}},
		{answer: "cancel", wantMode: "sub", wantErr: true},
	} {
		t.Run(tc.answer, func(t *testing.T) {
			p := &scriptedProvider{
				links:    map[string][]string{"dub": {"d.m3u8"}},
				linkErrs: map[string]error{"sub": errors.New("no sub")},
			}
			useProvider(t, p)
			asked := false
			withPromptSelect(t, func(opts []SelectionOption) (SelectionOption, error) {
				asked = true
				if len(opts) != 2 || opts[0].Key != "play" {
					t.Errorf("fallback menu: %+v", opts)
				}
				return SelectionOption{Key: tc.answer}, nil
			})

			links, mode, err := GetEpisodeURLForPlayback(Config{SubOrDub: "sub"}, "show", 3)
			if !asked {
				t.Fatal("switched audio without asking")
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
			if mode != tc.wantMode || !slices.Equal(links, tc.wantLinks) {
				t.Errorf("got %q %v", mode, links)
			}
			if tc.answer == "play" && !slices.Equal(p.linkCalls, []string{"sub", "dub"}) {
				t.Errorf("lookups = %v", p.linkCalls)
			}
		})
	}
}

func TestAudioChoiceProviderNameFromConfig(t *testing.T) {
	// whatever heads the stack is the fallback; don't pin a name here
	head := defaultEnabledProviderStack()[0]
	for _, name := range []string{" Anikoto ", " AnimePahe "} {
		useProvider(t, nil)
		useConfig(t, &Config{Provider: name})
		if got := GetProvider().Name(); got != head {
			t.Errorf("Provider %q -> %q, want %q", name, got, head)
		}
	}
}
