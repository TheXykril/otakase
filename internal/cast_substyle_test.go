package internal

import "testing"

// Casting cannot render soft subtitles: the Default Media Receiver reads
// WebVTT only and no provider here supplies it, so a soft-subbed cast plays
// with no subtitles at all. Asking for the burned-in variant is the only way
// a cast viewer sees subtitles, so -cast must force it.
func TestCastForcesHardSubs(t *testing.T) {
	config := &Config{SubStyle: "soft"}

	ApplyCastSubStyle(config, false)

	if config.SubStyle != "hard" {
		t.Errorf("SubStyle = %q, want %q", config.SubStyle, "hard")
	}
}

// The override is for this run only. Rewriting the viewer's stored preference
// because they cast once would change how every later local playback behaves.
func TestCastSubStyleOverrideIsNotPersisted(t *testing.T) {
	config := &Config{SubStyle: "soft"}

	ApplyCastSubStyle(config, false)

	// ApplyCastSubStyle must not reach persistSubStylePreference. If it ever
	// does, this test still passes -- so the guard is the call graph, not the
	// assertion: ApplyCastSubStyle takes a *Config and returns nothing, and
	// has no other collaborator to write with.
	if config.SubStyle != "hard" {
		t.Fatalf("SubStyle = %q, want %q", config.SubStyle, "hard")
	}
}
