package internal

import (
	"testing"
	"time"
)

func TestParseOpenLink(t *testing.T) {
	good := []struct {
		raw    string
		id, ep int
	}{
		{"otakase://play/anilist/154587", 154587, 0},
		{"otakase://play/anilist/154587?ep=3", 154587, 3},
		{"otakase://play/anilist/21/?ep=1100", 21, 1100},
		{"OTAKASE://Play/AniList/21?ep=2", 21, 2},
		{"  otakase://play/anilist/21?ep=2&from=discord  ", 21, 2},
	}
	for _, c := range good {
		link, err := ParseOpenLink(c.raw)
		if err != nil {
			t.Errorf("%q: %v", c.raw, err)
			continue
		}
		if link.AnilistID != c.id || link.Episode != c.ep {
			t.Errorf("%q = %+v, want id %d ep %d", c.raw, link, c.id, c.ep)
		}
	}

	bad := []string{
		"",
		"https://otakase.xyverion.com/open/?anilist=21",
		"otakase:play/anilist/21",
		"otakase://watch/anilist/21",
		"otakase://play/mal/21",
		"otakase://play/anilist",
		"otakase://play/anilist/21/extra",
		"otakase://play/anilist/0",
		"otakase://play/anilist/021",
		"otakase://play/anilist/-21",
		"otakase://play/anilist/+21",
		"otakase://play/anilist/21abc",
		"otakase://play/anilist/1e3",
		"otakase://play/anilist/9999999999",
		"otakase://play/anilist/%32%31",
		"otakase://play/anilist/21?ep=",
		"otakase://play/anilist/21?ep=0",
		"otakase://play/anilist/21?ep=-1",
		"otakase://play/anilist/21?ep=2;rm",
		"otakase://play/anilist/21?ep=2%20",
		"otakase://play/anilist/21?ep=100000",
		"otakase://play/anilist/21?ep=1&ep=2",
		"otakase://user@play/anilist/21",
		"otakase://play/anilist/21\" -e",
	}
	for _, raw := range bad {
		if link, err := ParseOpenLink(raw); err == nil {
			t.Errorf("%q accepted as %+v", raw, link)
		}
	}
}

func TestIsOpenLinkArg(t *testing.T) {
	for arg, want := range map[string]bool{
		"otakase://play/anilist/1": true,
		"Otakase:anything":         true,
		"-open":                    false,
		"otakase":                  false,
		"https://x":                false,
	} {
		if got := IsOpenLinkArg(arg); got != want {
			t.Errorf("IsOpenLinkArg(%q) = %t", arg, got)
		}
	}
}

func TestTakeOpenLinkOnce(t *testing.T) {
	SetOpenLink(OpenLink{AnilistID: 5, Episode: 2})
	if link, ok := takeOpenLink(); !ok || link.AnilistID != 5 || link.Episode != 2 {
		t.Fatalf("got %+v %t", link, ok)
	}
	if _, ok := takeOpenLink(); ok {
		t.Fatal("link handed over twice")
	}
}

func TestApplyLinkEpisode(t *testing.T) {
	defer func(prev time.Duration) { openLinkNoticePause = prev }(openLinkNoticePause)
	openLinkNoticePause = 0
	config := &Config{TrackingRemote: TrackingRemoteAniList}

	// A later episode than the one the menu would pick starts from its start.
	a := &Anime{TotalEpisodes: 12}
	a.Ep.Number, a.Ep.Resume, a.Ep.Player.PlaybackTime = 4, true, 300
	applyLinkEpisode(config, a, 7, 4)
	if a.Ep.Number != 7 || a.Ep.Resume || a.Ep.Player.PlaybackTime != 0 || a.SkipRemoteSync {
		t.Errorf("later episode: %+v", a.Ep)
	}

	// The episode already chosen keeps its resume position.
	a = &Anime{TotalEpisodes: 12}
	a.Ep.Number, a.Ep.Resume, a.Ep.Player.PlaybackTime = 4, true, 300
	applyLinkEpisode(config, a, 4, 4)
	if a.Ep.Number != 4 || !a.Ep.Resume || a.Ep.Player.PlaybackTime != 300 {
		t.Errorf("same episode: %+v", a.Ep)
	}

	// Past the known total, the link is wrong and the usual episode stands.
	a = &Anime{TotalEpisodes: 12}
	a.Ep.Number = 4
	applyLinkEpisode(config, a, 13, 4)
	if a.Ep.Number != 4 {
		t.Errorf("past total: %+v", a.Ep)
	}

	// An unknown total takes the link at its word.
	a = &Anime{}
	a.Ep.Number = 1
	applyLinkEpisode(config, a, 30, 0)
	if a.Ep.Number != 30 {
		t.Errorf("unknown total: %+v", a.Ep)
	}

	// An episode already watched does not set the tracker back.
	a = &Anime{TotalEpisodes: 12}
	a.Ep.Number = 9
	applyLinkEpisode(config, a, 2, 9)
	if a.Ep.Number != 2 || !a.SkipRemoteSync {
		t.Errorf("watched episode: %+v skip=%t", a.Ep, a.SkipRemoteSync)
	}

	// No episode in the link changes nothing.
	a = &Anime{TotalEpisodes: 12}
	a.Ep.Number = 9
	applyLinkEpisode(config, a, 0, 9)
	if a.Ep.Number != 9 || a.SkipRemoteSync {
		t.Errorf("no episode: %+v", a.Ep)
	}
}

func TestCommandExecutable(t *testing.T) {
	exe := `C:\Users\me\AppData\Roaming\Otakase\otakase.exe`
	if got := commandExecutable(urlSchemeCommand(exe)); got != exe {
		t.Errorf("round trip: %q", got)
	}
	if got := urlSchemeCommand(exe); got != `"`+exe+`" "%1"` {
		t.Errorf("command: %q", got)
	}
	for command, want := range map[string]string{
		`C:\otakase.exe "%1"`: `C:\otakase.exe`,
		`C:\otakase.exe`:      `C:\otakase.exe`,
		`"C:\broken`:          "",
		"":                    "",
	} {
		if got := commandExecutable(command); got != want {
			t.Errorf("commandExecutable(%q) = %q, want %q", command, got, want)
		}
	}
}

func TestIsPartyLink(t *testing.T) {
	if !IsPartyLink("otakase://party/1234567890123456789") || IsPartyLink("otakase://play/anilist/1") {
		t.Fatal("IsPartyLink")
	}
	if id, ok := parsePartyID("otakase://party/1234567890123456789"); !ok || id != "1234567890123456789" {
		t.Fatalf("parsePartyID = %q %v", id, ok)
	}
}
