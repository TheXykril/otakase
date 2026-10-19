package substyle_test

import (
	"testing"

	"github.com/thexykril/otakase/internal/providerhost"
	"github.com/thexykril/otakase/internal/providers/substyle"
)

// host swaps the provider host hooks for the length of a test. answer is the
// key the prompt returns; prompts and messages count what the user was shown.
type host struct {
	answer   string
	saved    string
	prompts  int
	messages int
}

func stubHost(t *testing.T, answer string) *host {
	t.Helper()
	h := &host{answer: answer}
	prompt, out := providerhost.PromptSelect, providerhost.Out
	persist, current := providerhost.PersistSubStylePreference, providerhost.CurrentSubStyle
	t.Cleanup(func() {
		providerhost.PromptSelect, providerhost.Out = prompt, out
		providerhost.PersistSubStylePreference, providerhost.CurrentSubStyle = persist, current
		substyle.ResetForTest()
	})
	substyle.ResetForTest()
	providerhost.PromptSelect = func([]providerhost.PromptOption) (providerhost.PromptOption, error) {
		h.prompts++
		return providerhost.PromptOption{Key: h.answer}, nil
	}
	providerhost.Out = func(string) { h.messages++ }
	providerhost.PersistSubStylePreference = func(style string) error {
		h.saved = style
		return nil
	}
	providerhost.CurrentSubStyle = func() string { return "" }
	return h
}

func TestChoose(t *testing.T) {
	cases := []struct {
		name             string
		hasSoft, hasHard bool
		configured       string
		answer           string
		want             string
		wantErr          bool
		wantPrompts      int
	}{
		{name: "soft wanted and there", hasSoft: true, hasHard: true, configured: "soft", want: "soft"},
		{name: "soft wanted, only hard", hasHard: true, configured: "soft", want: "hard"},
		{name: "hard wanted and there", hasSoft: true, hasHard: true, configured: "HARD ", want: "hard"},
		{name: "hard wanted, soft accepted", hasSoft: true, configured: "hard", answer: "soft", want: "soft", wantPrompts: 1},
		{name: "hard wanted, soft declined", hasSoft: true, configured: "hard", answer: "cancel", wantErr: true, wantPrompts: 1},
		{name: "ask with both", hasSoft: true, hasHard: true, configured: "ask", answer: "hard", want: "hard", wantPrompts: 1},
		{name: "ask with soft only", hasSoft: true, configured: "", want: "soft"},
		{name: "ask with hard only", hasHard: true, configured: "ask", want: "hard"},
		{name: "nothing at all", configured: "soft", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := stubHost(t, tc.answer)
			got, err := substyle.Choose(tc.hasSoft, tc.hasHard, tc.configured)
			if tc.wantErr != (err != nil) {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("style = %q, want %q", got, tc.want)
			}
			if h.prompts != tc.wantPrompts {
				t.Fatalf("prompted %d times, want %d", h.prompts, tc.wantPrompts)
			}
		})
	}
}

func TestChooseSoftOnlySaysSo(t *testing.T) {
	h := stubHost(t, "")
	if _, err := substyle.Choose(true, false, "ask"); err != nil {
		t.Fatal(err)
	}
	if h.messages != 1 || h.saved != "soft" {
		t.Fatalf("messages = %d, saved = %q; want one message and soft saved", h.messages, h.saved)
	}
}

func TestChooseAsksOncePerRun(t *testing.T) {
	h := stubHost(t, "hard")
	for i := 0; i < 3; i++ {
		got, err := substyle.Choose(true, true, "ask")
		if err != nil || got != "hard" {
			t.Fatalf("round %d: %q, %v", i, got, err)
		}
	}
	if h.prompts != 1 {
		t.Fatalf("prompted %d times, want 1", h.prompts)
	}
}

func TestChooseLiveSettingWins(t *testing.T) {
	stubHost(t, "")
	providerhost.CurrentSubStyle = func() string { return "hard" }
	got, err := substyle.Choose(true, true, "soft")
	if err != nil || got != "hard" {
		t.Fatalf("got %q, %v; want the live hard setting", got, err)
	}
}

func TestChooseWithoutPromptPrefersSoft(t *testing.T) {
	stubHost(t, "")
	providerhost.PromptSelect = nil
	got, err := substyle.Choose(true, true, "ask")
	if err != nil || got != "soft" {
		t.Fatalf("got %q, %v; want soft", got, err)
	}
}
