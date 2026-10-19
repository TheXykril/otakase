package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func inboxItems(t *testing.T, b *bot, token, query string) []inboxItem {
	t.Helper()
	w := apiDo(t, b, http.MethodGet, "/v1/inbox"+query, nil, map[string]string{"Authorization": "Bearer " + token})
	if w.Code != http.StatusOK {
		t.Fatalf("inbox: %d %s", w.Code, w.Body.String())
	}
	var r struct {
		Items []inboxItem `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil || r.Items == nil {
		t.Fatalf("inbox body %q: %v", w.Body.String(), err)
	}
	return r.Items
}

func TestInboxFanOutAndAck(t *testing.T) {
	b := appTestBot(t)
	inboxLimit, inboxAckLimit = newRateLimit(30, time.Minute), newRateLimit(30, time.Minute)
	pc, laptop, other := testToken(b, "u1"), testToken(b, "u1"), testToken(b, "u2")

	if n := b.sendToApps("u1", inboxItem{Kind: "queue", AniListID: 5, Title: "Frieren"}); n != 2 {
		t.Fatalf("sent to %d apps, want 2", n)
	}
	if got := inboxItems(t, b, other, ""); len(got) != 0 {
		t.Errorf("another member's app got %+v", got)
	}
	got := inboxItems(t, b, pc, "")
	if len(got) != 1 || got[0].Kind != "queue" || got[0].AniListID != 5 || got[0].ID == "" || got[0].Expires.IsZero() {
		t.Fatalf("pc inbox %+v", got)
	}
	if w := apiJSON(t, b, "/v1/inbox/ack", `{"ids":["`+got[0].ID+`"]}`, pc); w.Code != http.StatusOK {
		t.Fatalf("ack: %d", w.Code)
	}
	if left := inboxItems(t, b, pc, ""); len(left) != 0 {
		t.Errorf("acked item still there: %+v", left)
	}
	if left := inboxItems(t, b, laptop, ""); len(left) != 1 {
		t.Errorf("the other app keeps its copy: %+v", left)
	}
	if w := apiDo(t, b, http.MethodGet, "/v1/inbox", nil, nil); w.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d", w.Code)
	}
}

func TestInboxExpiresAndCaps(t *testing.T) {
	b := appTestBot(t)
	inboxLimit = newRateLimit(30, time.Minute)
	tok := testToken(b, "u1")
	b.sendToApps("u1", inboxItem{Kind: "remote", Key: "pause", Expires: appNow().Add(-time.Second)})
	if got := inboxItems(t, b, tok, ""); len(got) != 0 {
		t.Errorf("expired item delivered: %+v", got)
	}
	for k := 0; k < maxInboxItems+5; k++ {
		b.sendToApps("u1", inboxItem{Kind: "notice", Episode: k})
	}
	got := inboxItems(t, b, tok, "")
	if len(got) != maxInboxItems || got[len(got)-1].Episode != maxInboxItems+4 {
		t.Errorf("kept %d, newest %+v", len(got), got[len(got)-1])
	}
}

func TestInboxLongPollWakes(t *testing.T) {
	b := appTestBot(t)
	inboxLimit = newRateLimit(30, time.Minute)
	tok := testToken(b, "u1")
	go func() {
		time.Sleep(100 * time.Millisecond)
		b.sendToApps("u1", inboxItem{Kind: "notice", Title: "Hi"})
	}()
	start := time.Now()
	got := inboxItems(t, b, tok, "?wait=10")
	if len(got) != 1 || got[0].Title != "Hi" {
		t.Fatalf("got %+v", got)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("long poll did not wake: %v", time.Since(start))
	}
}

func TestHeartbeatInboxCountAndHooks(t *testing.T) {
	b := appTestBot(t)
	tok := testToken(b, "u1")
	var seen []heartbeatEvent
	old := heartbeatHooks
	heartbeatHooks = []func(*bot, heartbeatEvent){func(_ *bot, ev heartbeatEvent) { seen = append(seen, ev) }}
	t.Cleanup(func() { heartbeatHooks = old })
	b.sendToApps("u1", inboxItem{Kind: "notice"})
	w := apiJSON(t, b, "/v1/heartbeat", `{"version":"26.8.0","share_watching":false,"watching":{"anilist_id":5,"title":"X","episode":2},"watched_seconds":60}`, tok)
	if w.Code != http.StatusOK || decode(t, w)["inbox"] != float64(1) {
		t.Fatalf("heartbeat: %d %s", w.Code, w.Body.String())
	}
	if len(seen) != 1 || seen[0].User != "u1" || seen[0].Seconds != 60 || seen[0].Show != nil || seen[0].Version != "26.8.0" {
		t.Errorf("hook saw %+v (show must be nil when not shared)", seen)
	}
}

func TestQueueButtons(t *testing.T) {
	rows := withQueueButton([]discordgo.MessageComponent{discordgo.ActionsRow{Components: make([]discordgo.MessageComponent, 5)}}, 7)
	if len(rows) != 2 || !strings.HasSuffix(rows[1].(discordgo.ActionsRow).Components[0].(discordgo.Button).CustomID, ":7") {
		t.Errorf("full row: %+v", rows)
	}
	rows = withQueueButton([]discordgo.MessageComponent{discordgo.ActionsRow{}}, 7)
	if len(rows) != 1 || len(rows[0].(discordgo.ActionsRow).Components) != 1 {
		t.Errorf("room in the row: %+v", rows)
	}
	if rows := withQueueButton(nil, 0); len(rows) != 0 {
		t.Errorf("no show: %+v", rows)
	}
}
