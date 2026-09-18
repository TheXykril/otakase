package substyle_test

import (
	"testing"

	"github.com/thexykril/otakase/internal/providerhost"
	"github.com/thexykril/otakase/internal/providers/substyle"
)

func TestChooseHardPromptsForSoftFallback(t *testing.T) {
	substyle.ResetForTest()
	prompts := 0
	previousPrompt := providerhost.PromptSelect
	providerhost.Out = func(string) {}
	providerhost.PromptSelect = func(options []providerhost.PromptOption) (providerhost.PromptOption, error) {
		prompts++
		return providerhost.PromptOption{Key: "soft", Label: options[0].Label}, nil
	}
	t.Cleanup(func() {
		providerhost.PromptSelect = previousPrompt
		substyle.ResetForTest()
	})

	style, err := substyle.Choose(true, false, "hard")
	if err != nil || style != "soft" {
		t.Fatalf("expected soft after prompt, got style=%q err=%v", style, err)
	}
	if prompts != 1 {
		t.Fatalf("expected 1 prompt, got %d", prompts)
	}
}

func TestChooseHardDeclinedSoftFallback(t *testing.T) {
	substyle.ResetForTest()
	previousPrompt := providerhost.PromptSelect
	providerhost.PromptSelect = func([]providerhost.PromptOption) (providerhost.PromptOption, error) {
		return providerhost.PromptOption{Key: "cancel", Label: "Cancel"}, nil
	}
	t.Cleanup(func() {
		providerhost.PromptSelect = previousPrompt
		substyle.ResetForTest()
	})

	_, err := substyle.Choose(true, false, "hard")
	if err == nil {
		t.Fatal("expected error when soft fallback is declined")
	}
}

func TestChooseSoftFallsBackToHard(t *testing.T) {
	style, err := substyle.Choose(false, true, "soft")
	if err != nil || style != "hard" {
		t.Fatalf("expected hard fallback, got style=%q err=%v", style, err)
	}
}

func TestChooseAskAutoUsesSoftOnly(t *testing.T) {
	substyle.ResetForTest()
	messages := 0
	previousOut := providerhost.Out
	providerhost.Out = func(string) { messages++ }
	t.Cleanup(func() {
		providerhost.Out = previousOut
		substyle.ResetForTest()
	})

	style, err := substyle.Choose(true, false, "ask")
	if err != nil || style != "soft" {
		t.Fatalf("expected soft, got style=%q err=%v", style, err)
	}
	if messages != 1 {
		t.Fatalf("expected status message, got %d", messages)
	}
}

func TestChooseAskDeclinedSoftOnly(t *testing.T) {
	t.Skip("ask mode auto-accepts soft-only streams")
}
