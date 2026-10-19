package providers

import "strings"

// NormalizeTranslationType reads anything but "dub" as sub, the audio every
// host carries when it carries only one.
func NormalizeTranslationType(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), "dub") {
		return "dub"
	}
	return "sub"
}

// AlternateTranslationType is the audio to fall back to from mode.
func AlternateTranslationType(mode string) string {
	if NormalizeTranslationType(mode) == "sub" {
		return "dub"
	}
	return "sub"
}
