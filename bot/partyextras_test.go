package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

// testRoom is a hub with a party ev and connections for users, the first
// one hosting.
func testRoom(t *testing.T, users ...string) (*partyHub, map[string]*partyConn, *time.Time) {
	t.Helper()
	h := newPartyHub()
	now := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
	h.now = func() time.Time { return now }
	conns := map[string]*partyConn{}
	for k, u := range users {
		c := &partyConn{user: u, host: k == 0, out: make(chan []byte, 1000)}
		if !h.join("ev", watchParty{Host: users[0], Sync: true}, c) {
			t.Fatalf("%s refused", u)
		}
		conns[u] = c
	}
	return h, conns, &now
}

// frames empties c's queue and returns the frames of type kind.
func frames(c *partyConn, kind string) []map[string]any {
	var out []map[string]any
	for len(c.out) > 0 {
		var m map[string]any
		json.Unmarshal(<-c.out, &m)
		if m["type"] == kind {
			out = append(out, m)
		}
	}
	return out
}

func TestPartyVotes(t *testing.T) {
	h, c, now := testRoom(t, "h", "a", "b", "d")
	for _, x := range c {
		frames(x, "")
	}
	h.handle("ev", c["a"], []byte(`{"type":"vote","what":"skip"}`))
	h.handle("ev", c["a"], []byte(`{"type":"vote","what":"skip"}`)) // the same member twice is one vote
	h.handle("ev", c["b"], []byte(`{"type":"vote","what":"dance"}`))
	v := frames(c["h"], "votes")
	if len(v) != 2 || v[1]["votes"] != 1.0 || v[1]["needed"] != 3.0 || v[1]["what"] != "skip" {
		t.Fatalf("tally of 4 members: %v", v)
	}
	h.handle("ev", c["b"], []byte(`{"type":"vote","what":"skip"}`))
	h.handle("ev", c["h"], []byte(`{"type":"vote","what":"skip"}`))
	if p := frames(c["h"], "voted"); len(p) != 1 || p[0]["what"] != "skip" {
		t.Fatalf("three of four should pass: %v", p)
	}
	if frames(c["d"], "voted") == nil {
		t.Error("everyone hears it passed")
	}

	// Old votes run out.
	h.handle("ev", c["a"], []byte(`{"type":"vote","what":"next"}`))
	*now = now.Add(partyVoteWindow + time.Second)
	h.handle("ev", c["b"], []byte(`{"type":"vote","what":"next"}`))
	if v := frames(c["h"], "votes"); v[len(v)-1]["votes"] != 1.0 {
		t.Errorf("an old vote still counted: %v", v)
	}

}

func TestPartyVotesWhenMembersLeave(t *testing.T) {
	// 2 of 4 is not more than half, 2 of 3 is.
	h, c, _ := testRoom(t, "h", "a", "b", "d")
	h.handle("ev", c["a"], []byte(`{"type":"vote","what":"next"}`))
	h.handle("ev", c["b"], []byte(`{"type":"vote","what":"next"}`))
	if p := frames(c["h"], "voted"); len(p) != 0 {
		t.Fatalf("passed too early: %v", p)
	}
	h.leave("ev", c["d"])
	if p := frames(c["h"], "voted"); len(p) != 1 {
		t.Errorf("should pass once d left: %v", p)
	}
}

func TestPartyVotesClearOnEpisode(t *testing.T) {
	h, c, now := testRoom(t, "h", "a", "b")
	state := func(ep int) []byte {
		b, _ := json.Marshal(map[string]any{"type": "state", "anilist_id": 1, "episode": ep, "playing": true, "position": 1, "sent_at": now.UnixMilli()})
		return b
	}
	h.handle("ev", c["h"], state(1))
	h.handle("ev", c["a"], []byte(`{"type":"vote","what":"next"}`))
	h.handle("ev", c["h"], state(2))
	frames(c["b"], "")
	h.handle("ev", c["b"], []byte(`{"type":"vote","what":"next"}`))
	if v := frames(c["b"], "votes"); len(v) != 1 || v[0]["votes"] != 1.0 {
		t.Errorf("votes should start over on a new episode: %v", v)
	}
}

func TestPartyRelay(t *testing.T) {
	h, c, now := testRoom(t, "h", "a")
	frames(c["a"], "")
	for k := 0; k < partyRelayRate+3; k++ {
		h.relay("ev", mustJSON(map[string]string{"type": "react", "emoji": "🔥", "name": "x"}))
	}
	if n := len(frames(c["a"], "react")); n != partyRelayRate {
		t.Errorf("relayed in one second: %d", n)
	}
	*now = now.Add(time.Second)
	if !h.relay("ev", []byte(`{"type":"chat"}`)) || h.relay("nope", []byte(`{}`)) {
		t.Error("relay after a second, and not to a party nobody is in")
	}
	if !h.hostOn("ev") || h.hostOn("nope") {
		t.Error("hostOn")
	}
}

func TestEmojiOnly(t *testing.T) {
	for s, want := range map[string]bool{
		"🔥": true, "🔥 😂": true, "👍🏽": true, "❤️": true, "👨‍👩‍👧": true, ":pepe:": true, "🔥:pepe:": true,
		"lol 🔥": false, "": false, "  ": false, "ok": false, ":)": false, "1️⃣": false,
	} {
		if got := emojiOnly(s); got != want {
			t.Errorf("emojiOnly(%q) = %v", s, got)
		}
	}
}

func TestPartyChatText(t *testing.T) {
	m := &discordgo.Message{Content: "hi <@12> look <:pepe:99> <#5> <@&7> <@!13>\nnext line",
		Mentions: []*discordgo.User{{ID: "12", Username: "ann", GlobalName: "Ann"}}}
	if got := partyChatText(m); got != "hi @Ann look :pepe: #channel @role @someone next line" {
		t.Errorf("got %q", got)
	}
	long := &discordgo.Message{Content: strings.Repeat("a", 500)}
	if n := len([]rune(partyChatText(long))); n > partyChatMax {
		t.Errorf("not clipped: %d", n)
	}
	if got := partyChatText(&discordgo.Message{Attachments: []*discordgo.MessageAttachment{{}}}); got != "sent an attachment" {
		t.Errorf("attachment: %q", got)
	}
}

func resetLiveParties() {
	liveParties.Lock()
	liveParties.byID, liveParties.byHost = map[string]*liveParty{}, map[string]string{}
	liveParties.Unlock()
	livePartyLimit = newRateLimit(20, time.Hour)
}

func TestPartyStartAPI(t *testing.T) {
	b := appTestBot(t)
	resetLiveParties()
	tok := testToken(b, "u1")
	if w := apiJSON(t, b, "/v1/party/start", `{"anilist_id":154587,"episode":3,"title":"Frieren"}`, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d", w.Code)
	}
	if w := apiJSON(t, b, "/v1/party/start", `{"title":"Frieren"}`, tok); w.Code != http.StatusBadRequest {
		t.Errorf("no show: %d", w.Code)
	}
	var first, again struct {
		ID      string `json:"id"`
		JoinURL string `json:"join_url"`
	}
	w := apiJSON(t, b, "/v1/party/start", `{"anilist_id":154587,"episode":3,"title":"Frieren"}`, tok)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &first) != nil || len(first.ID) != 16 ||
		first.JoinURL != "https://otakase.xyverion.com/open/?party="+first.ID {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	w = apiJSON(t, b, "/v1/party/start", `{"anilist_id":154587,"episode":4,"title":"Frieren"}`, tok)
	json.Unmarshal(w.Body.Bytes(), &again)
	if again.ID != first.ID {
		t.Errorf("a member has one party: %s, %s", first.ID, again.ID)
	}
	p, ok := liveSyncParty(first.ID)
	if !ok || p.Host != "u1" || !p.Sync || p.Show != "Frieren" {
		t.Errorf("hub's view: %+v %v", p, ok)
	}
	if lp, ok := livePartyOf("u1"); !ok || lp.Episode != 4 {
		t.Errorf("livePartyOf: %+v", lp)
	}
}

func TestLivePartyEnds(t *testing.T) {
	resetLiveParties()
	now := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
	a, _ := startLiveParty("a", appShow{AniListID: 1, Episode: 1}, now)
	b, _ := startLiveParty("b", appShow{AniListID: 1, Episode: 1}, now)
	if gone := expireLiveParties(map[string]bool{a.ID: true}, now.Add(liveGrace+time.Second)); len(gone) != 1 || gone[0] != b.ID {
		t.Fatalf("only the party whose host left goes: %v", gone)
	}
	if _, ok := livePartyOf("b"); ok {
		t.Error("b's party should be gone")
	}

	// The host saying end ends it for everyone, at once.
	h := newPartyHub()
	host := &partyConn{user: "a", host: true, out: make(chan []byte, 100)}
	member := &partyConn{user: "m", out: make(chan []byte, 100)}
	p, _ := liveSyncParty(a.ID)
	h.join(a.ID, p, host)
	h.join(a.ID, p, member)
	if h.handle(a.ID, member, []byte(`{"type":"end"}`)); !h.hostOn(a.ID) {
		t.Fatal("a member can't end it")
	}
	if h.handle(a.ID, host, []byte(`{"type":"end"}`)) {
		t.Error("the host's connection should close")
	}
	if frames(member, "ended") == nil {
		t.Error("members hear it ended")
	}
	if _, ok := livePartyOf("a"); ok {
		t.Error("a's party should be gone")
	}
}

func TestAppPartyList(t *testing.T) {
	now := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
	parties := map[string]watchParty{
		"2": {Show: "Later", Start: now.Add(time.Hour), Host: "h", Going: []string{"h"}, Link: "https://discord.com/events/g/2"},
		"1": {Show: "Frieren", Start: now.Add(10 * time.Minute), Host: "h", Going: []string{"h", "me"}, AniListID: 154587, Sync: true},
	}
	list := appPartyList(parties, "me", now, func(id string) string { return "name-" + id }, func(id string) bool { return id == "1" })
	if len(list) != 2 || list[0].ID != "1" || list[1].ID != "2" {
		t.Fatalf("soonest first: %+v", list)
	}
	f := list[0]
	if !f.RSVP || f.Going != 2 || !f.Live || f.Host != "name-h" || f.JoinURL != partyJoinURL("1") {
		t.Errorf("synced party: %+v", f)
	}
	if l := list[1]; l.RSVP || l.Live || l.JoinURL != "" || l.EventURL == "" {
		t.Errorf("plain party: %+v", l)
	}
}

func TestPartyRSVPAPI(t *testing.T) {
	b := appTestBot(t)
	rsvpLimit = newRateLimit(20, time.Minute)
	partiesLimit = newRateLimit(30, time.Minute)
	tok := testToken(b, "me")
	b.store.update(func(d *storeData) {
		d.Parties["ev"] = watchParty{Show: "Frieren", Host: "h", Going: []string{"h"}, Out: []string{"me"}, Start: appNow().Add(time.Hour)}
	})
	for _, c := range []struct {
		going bool
		count int
	}{{true, 2}, {true, 2}, {false, 1}, {false, 1}} {
		body, _ := json.Marshal(map[string]bool{"going": c.going})
		w := apiJSON(t, b, "/v1/party/ev/rsvp", string(body), tok)
		var got struct {
			Going bool
			Count int
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Going != c.going || got.Count != c.count {
			t.Errorf("going=%v: %d %s", c.going, w.Code, w.Body)
		}
	}
	var p watchParty
	b.store.view(func(d *storeData) { p = d.Parties["ev"] })
	if len(p.Out) != 0 {
		t.Errorf("saying I'm in takes them off can't make it: %+v", p)
	}
	if w := apiJSON(t, b, "/v1/party/gone/rsvp", `{"going":true}`, tok); w.Code != http.StatusNotFound {
		t.Errorf("unknown party: %d", w.Code)
	}
	w := apiDo(t, b, http.MethodGet, "/v1/parties", nil, map[string]string{"Authorization": "Bearer " + tok})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"title":"Frieren"`) {
		t.Errorf("list: %d %s", w.Code, w.Body)
	}
}

func TestDueAppReminders(t *testing.T) {
	now := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
	d := &storeData{Parties: map[string]watchParty{
		"soon":  {Show: "Soon", Created: now.Add(-time.Hour), Start: now.Add(4 * time.Minute), Going: []string{"a"}, Sync: true},
		"later": {Show: "Later", Created: now.Add(-time.Hour), Start: now.Add(time.Hour)},
		"stale": {Show: "Stale", Created: now.Add(-3 * time.Hour), Start: now.Add(-time.Hour)},
	}}
	due := dueAppReminders(d, now)
	if len(due) != 1 || due[0].ID != "soon" {
		t.Fatalf("due: %+v", due)
	}
	if !d.Parties["stale"].AppReminded || d.Parties["later"].AppReminded {
		t.Error("a missed reminder is marked, a later one isn't")
	}
	if again := dueAppReminders(d, now); len(again) != 0 {
		t.Errorf("sent twice: %+v", again)
	}
	it := partyReminderItem("soon", due[0].P, now)
	if it.Kind != "notice" || it.URL != partyJoinURL("soon") || !strings.Contains(it.Text, "Soon starts in 4 minutes") {
		t.Errorf("item: %+v", it)
	}
	if it := partyReminderItem("x", watchParty{Show: "Now", Start: now, Link: "https://e"}, now); it.Title != "Watch party starting now" || it.URL != "https://e" {
		t.Errorf("starting now: %+v", it)
	}
}

func TestWhosWatchingRows(t *testing.T) {
	list := []nowWatching{
		{User: "1", Show: appShow{AniListID: 10, Episode: 3}},
		{User: "2", Show: appShow{AniListID: 66, Episode: 1}},
		{User: "3", Show: appShow{Title: "Local"}},
	}
	for k := 4; k < 12; k++ {
		list = append(list, nowWatching{User: string(rune('0' + k)), Show: appShow{AniListID: 10}})
	}
	lookup := func(id int) (airingShowInfo, bool) {
		var info airingShowInfo
		info.IsAdult = id == 66
		return info, true
	}
	rows := whosWatchingRows(list, lookup, func(id string) string { return "M" + id })
	if len(rows) != whosWatchingButtons {
		t.Fatalf("rows: %d", len(rows))
	}
	first := rows[0].(discordgo.ActionsRow).Components
	open, ask := first[0].(discordgo.Button), first[1].(discordgo.Button)
	if open.Label != "Open ep 3 · M1" || open.URL != playURL(10, 3) || ask.CustomID != "party-ask:1" {
		t.Errorf("first row: %+v %+v", open, ask)
	}
	for _, r := range rows {
		if strings.Contains(r.(discordgo.ActionsRow).Components[1].(discordgo.Button).CustomID, ":2") ||
			strings.Contains(r.(discordgo.ActionsRow).Components[1].(discordgo.Button).CustomID, ":3") {
			t.Error("adult titles and shows not on AniList get no buttons")
		}
	}
	if rows := whosWatchingRows(nil, lookup, func(string) string { return "" }); len(rows) != 0 {
		t.Error("no rows for nobody")
	}
}
