package internal

import (
	"testing"
)

// The default must be on: a show that only exists in one language should play.
func TestAutoAudioFallbackDefaultsOn(t *testing.T) {
	if defaultConfigMap()["AutoAudioFallback"] != "true" {
		t.Fatalf("expected AutoAudioFallback to default on, got %q",
			defaultConfigMap()["AutoAudioFallback"])
	}
}
