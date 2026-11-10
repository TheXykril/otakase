package internal

import (
	"reflect"
	"testing"
)

func TestAiringNotification(t *testing.T) {
	if got := airingNotification(discordInboxItem{Title: "Frieren", Episode: 5}); got != "Episode 5 of Frieren is out" {
		t.Errorf("got %q", got)
	}
	if got := airingNotification(discordInboxItem{}); got != "A new episode of a show you follow is out" {
		t.Errorf("no title or episode: %q", got)
	}
}

func TestKeepAiringItemQueuesTheEpisode(t *testing.T) {
	config := newDiscordTestConfig(t, "http://bot.invalid")
	var shown []string
	previous := notifyAiring
	notifyAiring = func(message, url string) { shown = append(shown, message+" | "+url) }
	t.Cleanup(func() { notifyAiring = previous })

	keepAiringItem(config, discordInboxItem{ID: "a", Kind: "airing", Title: "Frieren", AnilistID: 154587, Episode: 4, URL: "https://otakase.com/open/?anilist=154587&ep=4"})
	keepAiringItem(config, discordInboxItem{ID: "b", Kind: "airing", Title: "Frieren", AnilistID: 154587, Episode: 5})
	keepAiringItem(config, discordInboxItem{ID: "c", Kind: "airing", Title: "Dandadan", AnilistID: 171018, Episode: 1})

	items := loadKeptInbox(config)
	if len(items) != 2 || items[0].ID != "c" || items[1].ID != "b" {
		t.Fatalf("newest first, one entry per show: %+v", items)
	}
	if items[1].Kind != "queue" || items[1].Episode != 5 {
		t.Errorf("kept as a queued show at its episode: %+v", items[1])
	}
	if got := inboxOption(items[1]).Label; got != "Play Frieren · episode 5" {
		t.Errorf("menu label %q", got)
	}
	if len(shown) != 3 || shown[0] != "Episode 4 of Frieren is out | https://otakase.com/open/?anilist=154587&ep=4" {
		t.Errorf("notifications: %q", shown)
	}
}

func TestRemoteCommand(t *testing.T) {
	cases := map[string][]interface{}{
		"pause":    {"set_property", "pause", true},
		"play":     {"set_property", "pause", false},
		"skip":     {"seek", 85, "relative"},
		"next":     {"playlist-next", "force"},
		"previous": {"playlist-prev", "force"},
		"stop":     {"quit"},
	}
	for action, want := range cases {
		got, _, ok := remoteCommand(action)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v, want %v", action, got, want)
		}
	}
	if _, _, ok := remoteCommand("loadfile"); ok {
		t.Error("an unknown action was accepted")
	}
}
