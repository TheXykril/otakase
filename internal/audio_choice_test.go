package internal

import (
	"testing"
)

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
