package internal

import "testing"

func TestDiscordAppID(t *testing.T) {
	for in, want := range map[string]string{
		"":                   OtakaseDiscordAppID,
		legacyDiscordAppID:   OtakaseDiscordAppID,
		"123456789012345678": "123456789012345678",
	} {
		if got := discordAppID(in); got != want {
			t.Errorf("discordAppID(%q) = %q, want %q", in, got, want)
		}
	}
}
