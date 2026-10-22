package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestCapsuleTextReason(t *testing.T) {
	ok := []string{
		"Happy birthday Otakase! Hope everyone finished their backlog.",
		"I watched 1,240 episodes in 2026, see you in 2027",
		"Frieren season 2 better be out by now",
		"as long as you keep watching, we're good",
		"Greetings from Niigata, the therapist says I watch too much anime",
		"Class of 2026, assemble! Dickens would have loved Monogatari.",
		"Hi!!! Great app.",
		"I hope I'm not still stuck on episode 400 of One Piece",
	}
	for _, text := range ok {
		if r := capsuleTextReason(text); r != "" {
			t.Errorf("%q blocked: %s", text, r)
		}
	}
	blocked := []string{
		"hi",
		"what the fuck",
		"this is sh1t",
		"f u c k this",
		"f.u.c.k",
		"fuuuuck",
		"you're an a$$hole",
		"kys loser",
		"go kill yourself",
		"aaass",
		"join discord.gg/abcdef",
		"watch at https://example.com",
		"visit mysite.xyz now",
		"mail me at someone@example.com",
		"mail me at someone (at) example (dot) com",
		"call +370 612 34567",
		"my card 4111111111111111",
		"hey @someone remember me",
		"hey <@123456789012345678>",
		"I live at 12 Baker Street",
		"snapchat: coolname",
		"add me kitty#1234",
		"eik nachui",
	}
	for _, text := range blocked {
		if r := capsuleTextReason(text); r == "" {
			t.Errorf("%q passed", text)
		}
	}
	long := make([]rune, capsuleMaxRunes+1)
	for i := range long {
		long[i] = 'a'
	}
	if capsuleTextReason(string(long)) == "" {
		t.Error("long message passed")
	}
}

func TestCapsuleDates(t *testing.T) {
	cases := []struct {
		at    time.Time
		opens int
		open  bool
	}{
		{time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), 2027, false},
		{time.Date(2027, 4, 11, 9, 0, 0, 0, time.UTC), 2027, false},
		// April 12 has started in UTC+14.
		{time.Date(2027, 4, 11, 10, 0, 0, 0, time.UTC), 2028, true},
		{time.Date(2027, 4, 12, 20, 0, 0, 0, time.UTC), 2028, true},
		// Still April 12 in UTC-12.
		{time.Date(2027, 4, 13, 11, 59, 0, 0, time.UTC), 2028, true},
		{time.Date(2027, 4, 13, 12, 0, 0, 0, time.UTC), 2028, false},
	}
	for _, c := range cases {
		if got := capsuleOpens(c.at); got != c.opens {
			t.Errorf("%v opens %d, want %d", c.at, got, c.opens)
		}
		year, open := capsuleOpenYear(c.at)
		if open != c.open || (open && year != c.at.Year()) {
			t.Errorf("%v open %v %d, want %v", c.at, open, year, c.open)
		}
	}
	if capsuleAge(2027) != "Otakase turns 1 today" {
		t.Error(capsuleAge(2027))
	}
}

func TestCapsuleJudged(t *testing.T) {
	if !capsuleJudged(0.7, 0.5, 0.6) {
		t.Error("close to a bad example should block")
	}
	if capsuleJudged(0.7, 0.75, 0.6) {
		t.Error("closer to a good example should pass")
	}
	if capsuleJudged(0.5, 0.2, 0.6) {
		t.Error("below the threshold should pass")
	}
}

func TestCapsuleSealReplaceReport(t *testing.T) {
	b := appTestBot(t)
	capsuleSealLimit = newRateLimit(5, time.Hour)
	now := time.Date(2026, 11, 3, 18, 0, 0, 0, time.UTC)
	if _, _, reason := b.sealCapsule("u1", "what the fuck", now); reason == "" {
		t.Fatal("swearing sealed")
	}
	first, replaced, reason := b.sealCapsule("u1", "Hello from November!", now)
	if reason != "" || replaced || first.Opens != 2027 || !first.Checked {
		t.Fatalf("first seal: %+v %v %q", first, replaced, reason)
	}
	second, replaced, _ := b.sealCapsule("u1", "Hello from the past, future people", now)
	if !replaced {
		t.Fatal("second seal should replace the first")
	}
	var count int
	b.store.view(func(d *storeData) { count = len(d.Capsules) })
	if count != 1 {
		t.Fatalf("%d messages kept, want 1", count)
	}
	// The writer's own report and repeats don't count.
	if _, counted, _ := b.reportCapsule("u1", second.ID); counted {
		t.Error("own report counted")
	}
	for i, u := range []string{"a", "a", "b", "c"} {
		hidden, _, found := b.reportCapsule(u, second.ID)
		if !found || hidden != (i == 3) {
			t.Fatalf("report %d by %s: hidden %v", i, u, hidden)
		}
	}
	var notes map[string]capsuleNote
	b.store.view(func(d *storeData) { notes = d.Capsules })
	if _, ok := pickCapsule(notes, 2027, ""); ok {
		t.Error("hidden message still picked")
	}
}

func TestCapsuleMemberReason(t *testing.T) {
	now := time.Date(2026, 11, 3, 18, 0, 0, 0, time.UTC)
	if capsuleMemberReason(nil, now) == "" {
		t.Error("non-member allowed")
	}
	if capsuleMemberReason(&discordgo.Member{JoinedAt: now.Add(-2 * 24 * time.Hour)}, now) == "" {
		t.Error("two-day member allowed")
	}
	if r := capsuleMemberReason(&discordgo.Member{JoinedAt: now.Add(-30 * 24 * time.Hour)}, now); r != "" {
		t.Error(r)
	}
}

func TestCapsuleAPI(t *testing.T) {
	b := appTestBot(t)
	capsuleSealLimit = newRateLimit(5, time.Hour)
	capsuleOpenLimit = newRateLimit(30, time.Hour)
	capsuleReportLimit = newRateLimit(10, time.Hour)
	appNow = func() time.Time { return time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC) }
	tok := testToken(b, "u1")

	if w := apiJSON(t, b, "/v1/capsule", `{"text":"visit example.com"}`, tok); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("link sealed: %d %s", w.Code, w.Body)
	}
	w := apiJSON(t, b, "/v1/capsule", `{"text":"See you next spring, everyone"}`, tok)
	if w.Code != http.StatusOK || decode(t, w)["opens"] != float64(2027) {
		t.Fatalf("seal: %d %s", w.Code, w.Body)
	}
	mine := apiDo(t, b, http.MethodGet, "/v1/capsule/mine", nil, map[string]string{"Authorization": "Bearer " + tok})
	if mine.Code != http.StatusOK || decode(t, mine)["text"] != "See you next spring, everyone" {
		t.Fatalf("mine: %d %s", mine.Code, mine.Body)
	}
	if w := apiDo(t, b, http.MethodGet, "/v1/capsule", nil, nil); w.Code != http.StatusNotFound {
		t.Fatalf("open before the birthday: %d", w.Code)
	}

	appNow = func() time.Time { return time.Date(2027, 4, 12, 8, 0, 0, 0, time.UTC) }
	open := apiDo(t, b, http.MethodGet, "/v1/capsule", nil, nil)
	got := decode(t, open)
	if open.Code != http.StatusOK || got["text"] != "See you next spring, everyone" || got["written"] != "December 2026" {
		t.Fatalf("open: %d %s", open.Code, open.Body)
	}
	if _, leaks := got["user"]; leaks {
		t.Error("the writer's id is sent to apps")
	}
	report := apiJSON(t, b, "/v1/capsule/report", `{"id":"`+got["id"].(string)+`"}`, testToken(b, "u2"))
	if report.Code != http.StatusOK {
		t.Fatalf("report: %d %s", report.Code, report.Body)
	}

	appNow = func() time.Time { return time.Date(2026, 12, 2, 12, 0, 0, 0, time.UTC) }
	if w := apiDo(t, b, http.MethodDelete, "/v1/capsule", nil, map[string]string{"Authorization": "Bearer " + tok}); w.Code != http.StatusOK {
		t.Fatalf("unseal: %d", w.Code)
	}
}
