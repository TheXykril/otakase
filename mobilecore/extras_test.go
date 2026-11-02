package mobilecore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tr1xem/go-discordrpc/client"
)

type fakeDiscord struct {
	shown  []client.Activity
	fail   int
	closed int
}

func (f *fakeDiscord) SetActivity(a client.Activity) error {
	if f.fail > 0 {
		f.fail--
		return errors.New("broken pipe")
	}
	f.shown = append(f.shown, a)
	return nil
}

func (f *fakeDiscord) Logout() error { f.closed++; return nil }

func TestPresenceShowsTheEpisodeAndRedialsAfterDiscordRestarts(t *testing.T) {
	fake := &fakeDiscord{fail: 1}
	dials := 0
	c := NewCore()
	c.presence.open = func() (presenceClient, error) { dials++; return fake, nil }
	out := c.execute(context.Background(), `{"version":1,"operation":"presence_set","presence":{"title":"Frieren","state":"Episode 3","cover":"https://img.example/c.jpg","position":60,"duration":1440,"buttons":[{"label":"View on AniList","url":"https://anilist.co/anime/154587"}]}}`)
	if !strings.Contains(out, `"shown":true`) {
		t.Fatalf("set: %s", out)
	}
	if dials != 2 || len(fake.shown) != 1 {
		t.Fatalf("dials %d shown %d", dials, len(fake.shown))
	}
	a := fake.shown[0]
	if a.Details != "Frieren" || a.State != "Episode 3" || a.LargeImage != "https://img.example/c.jpg" || a.Timestamps.End == nil || len(a.Buttons) != 1 {
		t.Errorf("activity %+v", a)
	}
	c.execute(context.Background(), `{"version":1,"operation":"presence_clear"}`)
	if fake.closed < 2 {
		t.Errorf("clear did not close the socket")
	}
}

func TestPresenceRefusesUnsafeButtons(t *testing.T) {
	for _, b := range []string{`{"label":"x","url":"http://plain.example"}`, `{"label":"","url":"https://a.example"}`, `{"label":"x","url":"javascript:alert(1)"}`} {
		if _, err := presenceActivity(presenceRequest{Title: "t", Buttons: []presenceButton{decodeButton(t, b)}}, time.Now()); err == nil {
			t.Errorf("%s accepted", b)
		}
	}
	a, err := presenceActivity(presenceRequest{Title: "t", Cover: "file:///etc/passwd", Paused: true}, time.Now())
	if err != nil || a.LargeImage != discordIconImage || a.SmallText != "Paused" || a.Timestamps.End != nil {
		t.Errorf("paused activity %+v %v", a, err)
	}
}

func decodeButton(t *testing.T, s string) presenceButton {
	var b presenceButton
	if err := json.Unmarshal([]byte(s), &b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSkipTimesLookupNeedsAnEpisode(t *testing.T) {
	c := NewCore()
	out := c.execute(context.Background(), `{"version":1,"operation":"skiptimes_lookup","mal_id":1}`)
	if !strings.Contains(out, "invalid_request") {
		t.Errorf("lookup without an episode: %s", out)
	}
	out = c.execute(context.Background(), `{"version":1,"operation":"skiptimes_submit","mal_id":1,"episode":1,"type":"op","start":10,"end":11,"length":1400,"submitter":"x"}`)
	if !strings.Contains(out, "rejected") {
		t.Errorf("a two-second opening was sent: %s", out)
	}
}
