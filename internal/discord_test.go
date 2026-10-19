package internal

import (
	"testing"
	"time"
)

func TestDiscordAppID(t *testing.T) {
	for in, want := range map[string]string{
		"":                   OtakaseDiscordAppID,
		oldDiscordAppID:      OtakaseDiscordAppID,
		"123456789012345678": "123456789012345678",
	} {
		if got := discordAppID(in); got != want {
			t.Errorf("discordAppID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPresenceActivity(t *testing.T) {
	now := time.Unix(1000, 0)
	anime := Anime{AnilistId: 1, MalId: 2}

	playing := presenceActivity(anime, presenceShown{title: "Show", episode: 3}, 100, 1400, now)
	if playing.State != "Episode 3" || playing.SmallImage != discordMarkImage || playing.LargeImage != discordIconImage {
		t.Errorf("playing activity = %+v", playing)
	}
	if playing.Timestamps.End == nil || !playing.Timestamps.End.Equal(now.Add(1300*time.Second)) {
		t.Errorf("end = %v, want 1300s from now", playing.Timestamps.End)
	}
	if len(playing.Buttons) != 2 || playing.Buttons[1].Label != "View on MAL" {
		t.Errorf("buttons = %+v", playing.Buttons)
	}

	paused := presenceActivity(anime, presenceShown{title: "Show", paused: true, joinURL: "https://x"}, 100, 1400, now)
	if paused.Timestamps.End != nil || paused.SmallText != "Paused" {
		t.Errorf("paused activity = %+v", paused)
	}
	if paused.Buttons[0].Label != "Watch with me" || paused.Buttons[1].Label != "View on AniList" {
		t.Errorf("buttons with Watch with me = %+v", paused.Buttons)
	}

	unknown := presenceActivity(anime, presenceShown{}, 10, 0, now)
	if unknown.Timestamps.End != nil {
		t.Error("an unknown length must not set an end time")
	}
}
