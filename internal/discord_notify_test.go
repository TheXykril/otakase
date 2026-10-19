package internal

import (
	"errors"
	"reflect"
	"strings"
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

	keepAiringItem(config, discordInboxItem{ID: "a", Kind: "airing", Title: "Frieren", AnilistID: 154587, Episode: 4, URL: "https://otakase.xyverion.com/open/?anilist=154587&ep=4"})
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
	if len(shown) != 3 || shown[0] != "Episode 4 of Frieren is out | https://otakase.xyverion.com/open/?anilist=154587&ep=4" {
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

func TestApplyDiscordRemote(t *testing.T) {
	var sent [][]interface{}
	previous := mpvRemoteSend
	mpvRemoteSend = func(socket string, command []interface{}) (interface{}, error) {
		if socket != "/tmp/mpv-test" {
			return nil, errors.New("wrong socket " + socket)
		}
		sent = append(sent, command)
		return nil, nil
	}
	t.Cleanup(func() { mpvRemoteSend = previous; untrackRemotePlayer("/tmp/mpv-test") })

	applyDiscordRemote(nil, discordInboxItem{Kind: "remote", Key: "pause"})
	if len(sent) != 0 {
		t.Fatal("sent while nothing plays")
	}
	trackRemotePlayer("/tmp/mpv-test")
	applyDiscordRemote(nil, discordInboxItem{Kind: "remote", Key: "pause"})
	applyDiscordRemote(nil, discordInboxItem{Kind: "remote", Key: "stop"})
	applyDiscordRemote(nil, discordInboxItem{Kind: "remote", Key: "eject"})
	if len(sent) != 3 || sent[0][0] != "show-text" || !strings.Contains(sent[0][1].(string), "Paused") ||
		!reflect.DeepEqual(sent[1], []interface{}{"set_property", "pause", true}) || !reflect.DeepEqual(sent[2], []interface{}{"quit"}) {
		t.Errorf("sent %v", sent)
	}

	// A playback that ended does not forget the one after it.
	trackRemotePlayer("/tmp/mpv-next")
	untrackRemotePlayer("/tmp/mpv-test")
	if currentRemotePlayer() != "/tmp/mpv-next" {
		t.Error("the newer playback was forgotten")
	}
	untrackRemotePlayer("/tmp/mpv-next")
	if currentRemotePlayer() != "" {
		t.Error("ended playback still tracked")
	}
}
