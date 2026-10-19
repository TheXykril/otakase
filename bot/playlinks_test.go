package main

import (
	"net/url"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestPlayURL(t *testing.T) {
	cases := []struct {
		id, ep int
		want   string
	}{
		{154587, 0, "https://otakase.xyverion.com/open/?anilist=154587"},
		{154587, 12, "https://otakase.xyverion.com/open/?anilist=154587&ep=12"},
		{1, -3, "https://otakase.xyverion.com/open/?anilist=1"},
		{0, 4, ""},
		{-5, 0, ""},
	}
	for _, c := range cases {
		if got := playURL(c.id, c.ep); got != c.want {
			t.Errorf("playURL(%d, %d) = %q, want %q", c.id, c.ep, got, c.want)
		}
	}
	u, err := url.Parse(playURL(21, 1100))
	if err != nil || u.Scheme != "https" || u.Query().Get("anilist") != "21" || u.Query().Get("ep") != "1100" {
		t.Errorf("does not parse back: %v %v", u, err)
	}
}

func TestWithPlayButton(t *testing.T) {
	row := []discordgo.MessageComponent{discordgo.Button{Label: "AniList", Style: discordgo.LinkButton, URL: "https://anilist.co/anime/2"}}
	got := withPlayButton(row, 2, 5)
	if len(got) != 3 {
		t.Fatalf("want 3 buttons, got %d", len(got))
	}
	if q, ok := got[2].(discordgo.Button); !ok || q.CustomID != "app-queue:2" {
		t.Errorf("last button: %+v", got[2])
	}
	b, ok := got[0].(discordgo.Button)
	if !ok || b.Label != "Play in Otakase" || b.Style != discordgo.LinkButton || b.URL != "https://otakase.xyverion.com/open/?anilist=2&ep=5" {
		t.Errorf("first button: %+v", got[0])
	}
	if got := withPlayButton(row, 0, 5); len(got) != 1 {
		t.Errorf("no id should add nothing: %d", len(got))
	}
	full := make([]discordgo.MessageComponent, 5)
	if got := withPlayButton(full, 2, 0); len(got) != 5 {
		t.Errorf("a full row stays full: %d", len(got))
	}
}
