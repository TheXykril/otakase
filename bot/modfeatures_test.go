package main

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestWarningDays(t *testing.T) {
	for in, want := range map[string]int{"": 90, "30": 30, "0": 0, "-1": 90, "x": 90, " 7 ": 7} {
		if got := warningDays(in); got != want {
			t.Errorf("warningDays(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestSplitWarnings(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	list := []warning{
		{At: now.AddDate(0, 0, -100), Reason: "old"},
		{At: now.AddDate(0, 0, -89), Reason: "recent"},
		{At: now, Reason: "new"},
	}
	active, expired := splitWarnings(list, now, 90)
	if len(active) != 2 || len(expired) != 1 || expired[0].Reason != "old" {
		t.Errorf("active %v expired %v", active, expired)
	}
	if active, _ := splitWarnings(list, now, 0); len(active) != 3 {
		t.Errorf("0 days should never expire: %v", active)
	}
	e := warningsEmbed("u", list, now, 90)
	if !strings.Contains(e.Description, "~~") || !strings.Contains(e.Description, "expired") || !strings.HasPrefix(e.Title, "2 active") {
		t.Errorf("embed: %s / %s", e.Title, e.Description)
	}
	if e := warningsEmbed("u", list[:1], now, 90); !strings.Contains(e.Description, "No active warnings") {
		t.Errorf("all expired: %s", e.Description)
	}
}

func TestMsgCache(t *testing.T) {
	now := time.Now()
	c := newMsgCache()
	c.size = 3
	for n := range 5 {
		c.put(cachedMsg{ID: fmt.Sprint(n), Content: "m", At: now}, now)
	}
	if _, ok := c.get("1", now); ok {
		t.Error("oldest should be dropped past the size")
	}
	if m, ok := c.get("4", now); !ok || m.Content != "m" {
		t.Error("newest missing")
	}
	if _, ok := c.take("4", now); !ok {
		t.Error("take")
	}
	if _, ok := c.get("4", now); ok {
		t.Error("take should forget")
	}
	if _, ok := c.get("3", now.Add(25*time.Hour)); ok {
		t.Error("messages older than a day should be gone")
	}
	c.markRemoved("scam filter", "a", "b")
	if r, ok := c.removedBy("a"); !ok || r != "scam filter" {
		t.Errorf("removedBy: %q %v", r, ok)
	}
	if _, ok := c.removedBy("a"); ok {
		t.Error("a mark should be used once")
	}
	var nilCache *msgCache
	nilCache.markRemoved("x", "y")
	if _, ok := nilCache.get("y", now); ok {
		t.Error("nil cache")
	}
}

func TestEditChanged(t *testing.T) {
	edited := time.Now()
	old := cachedMsg{Content: "hi", Attachments: []attachmentRef{{"a.png", "u"}}}
	cases := []struct {
		m     discordgo.Message
		known bool
		want  bool
	}{
		{discordgo.Message{Content: "hi"}, true, false}, // link preview loaded
		{discordgo.Message{Content: "hi", EditedTimestamp: &edited, Attachments: []*discordgo.MessageAttachment{{}}}, true, false},
		{discordgo.Message{Content: "hello", EditedTimestamp: &edited, Attachments: []*discordgo.MessageAttachment{{}}}, true, true},
		{discordgo.Message{Content: "hi", EditedTimestamp: &edited}, true, true}, // attachment removed
		{discordgo.Message{Content: "x", EditedTimestamp: &edited}, false, true},
	}
	for n, c := range cases {
		if got := editChanged(old, c.known, &c.m); got != c.want {
			t.Errorf("case %d: got %v", n, got)
		}
	}
}

func TestLogEmbeds(t *testing.T) {
	now := time.Now()
	c := cachedMsg{ID: "m", Channel: "c", Author: "u", Name: "Kai", Content: "bye", At: now,
		Attachments: []attachmentRef{{"cat [1].png", "https://cdn/x"}}}
	e := deletedEmbed("g", c, now)
	if e.Description != "bye" || len(e.Fields) != 4 || !strings.Contains(e.Fields[3].Value, "(https://cdn/x)") {
		t.Errorf("deleted: %+v", e)
	}
	ed := editedEmbed("g", cachedMsg{}, false, c, now)
	if !strings.Contains(ed.Fields[0].Value, "before the bot") || ed.Fields[1].Value != "bye" {
		t.Errorf("edited: %+v", ed.Fields)
	}
	b := bulkEmbed("c", 5, []cachedMsg{c, c}, "/purge by <@mod>", now)
	if !strings.HasPrefix(b.Title, "5 ") || !strings.Contains(b.Description, "removed by the bot: /purge") || !strings.Contains(b.Description, "3 more") {
		t.Errorf("bulk: %s", b.Description)
	}
	if one := bulkEmbed("c", 1, nil, "/purge by <@mod>", now); one.Title != "1 message deleted" {
		t.Errorf("bulk one: %s", one.Title)
	}
}

func TestAppealHelpers(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 5, time.UTC)
	list := []warning{{At: at.Add(-time.Hour)}, {At: at}, {At: at.Add(time.Hour)}}
	got, ok := removeWarning(list, at)
	if !ok || len(got) != 2 || got[1].At != at.Add(time.Hour) || len(list) != 3 || list[1].At != at {
		t.Errorf("removeWarning: %v %v (original %v)", got, ok, list)
	}
	if _, ok := removeWarning(list, at.Add(time.Minute)); ok {
		t.Error("removed a warning that isn't there")
	}
	labels := func(a appeal) string {
		var l []string
		for _, c := range appealButtons("x", a)[0].(discordgo.ActionsRow).Components {
			l = append(l, c.(discordgo.Button).Label)
		}
		return strings.Join(l, ",")
	}
	if got := labels(appeal{WarnAt: at}); got != "Remove warning,Reject" {
		t.Errorf("warn: %s", got)
	}
	if got := labels(appeal{WarnAt: at, Timeout: true}); got != "Remove warning,End timeout,Reject" {
		t.Errorf("warn+timeout: %s", got)
	}
	if got := labels(appeal{Timeout: true}); got != "End timeout,Reject" {
		t.Errorf("timeout: %s", got)
	}
	m := map[string]appeal{"old": {At: at}, "new": {At: at.Add(59 * 24 * time.Hour)}}
	pruneAppeals(m, at.Add(61*24*time.Hour))
	if _, ok := m["old"]; ok || len(m) != 1 {
		t.Errorf("prune: %v", m)
	}
}

func TestVerifyActive(t *testing.T) {
	now := time.Now()
	cases := []struct {
		v    verifyData
		want bool
	}{
		{verifyData{Mode: "on"}, true},
		{verifyData{Mode: "off", Raid: now}, false},
		{verifyData{}, false},
		{verifyData{Raid: now.Add(-59 * time.Minute)}, true},
		{verifyData{Raid: now.Add(-61 * time.Minute)}, false},
	}
	for n, c := range cases {
		if got := verifyActive(&c.v, now); got != c.want {
			t.Errorf("case %d: got %v", n, got)
		}
	}
}

func TestVerifyJoinBurst(t *testing.T) {
	var r verifyRuntime
	now := time.Now()
	for n := range raidJoins - 1 {
		if r.join(fmt.Sprint(n), now.Add(time.Duration(n)*time.Second)) {
			t.Fatalf("busy after %d joins", n+1)
		}
	}
	if !r.join("last", now.Add(20*time.Second)) {
		t.Error("should be busy at the raid limit")
	}
	if got := r.recent(now.Add(20*time.Second), 2*raidWindow); len(got) != raidJoins {
		t.Errorf("recent: %v", got)
	}
	var slow verifyRuntime
	for n := range raidJoins * 2 {
		if slow.join(fmt.Sprint(n), now.Add(time.Duration(n)*10*time.Second)) {
			t.Fatal("slow joins counted as a burst")
		}
	}
}

func TestChallenge(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for range 50 {
		answer, choices := newChallenge(r)
		seen := map[string]bool{}
		for _, c := range choices {
			if seen[c] || verifyEmoji(c) == "" {
				t.Fatalf("bad choices %v", choices)
			}
			seen[c] = true
		}
		if len(choices) != 4 || !seen[answer] {
			t.Fatalf("answer %q not in %v", answer, choices)
		}
		e, comps := challengeMessage(answer, choices, "")
		if !strings.Contains(e.Description, answer) || len(comps[0].(discordgo.ActionsRow).Components) != 4 {
			t.Fatal("challenge message")
		}
	}
}

func TestVerifyExpiredAndDeny(t *testing.T) {
	now := time.Now()
	got := verifyExpired(map[string]verifyPending{"a": {Joined: now.Add(-31 * time.Minute)}, "b": {Joined: now.Add(-5 * time.Minute)}}, now)
	if len(got) != 1 || got[0] != "a" {
		t.Errorf("expired: %v", got)
	}
	deny := &discordgo.PermissionOverwrite{ID: "r", Deny: discordgo.PermissionViewChannel}
	if needsVerifyDeny(&discordgo.Channel{ID: "v"}, "r", "v") {
		t.Error("the verify channel stays visible")
	}
	if needsVerifyDeny(&discordgo.Channel{ID: "c", PermissionOverwrites: []*discordgo.PermissionOverwrite{deny}}, "r", "v") {
		t.Error("already hidden")
	}
	if !needsVerifyDeny(&discordgo.Channel{ID: "c"}, "r", "v") {
		t.Error("should be hidden")
	}
	o := verifyChannelOverwrites("g", "bot", "r", []string{"mod"})
	if o[0].ID != "g" || o[0].Deny&discordgo.PermissionViewChannel == 0 || o[1].Allow&discordgo.PermissionViewChannel == 0 || o[1].Deny&discordgo.PermissionSendMessages == 0 {
		t.Errorf("verify channel overwrites: %+v", o)
	}
	if s := verifySummary(2, []string{"a"}, nil); !strings.Contains(s.Description, "2 passed") || !strings.Contains(s.Description, "<@a>") {
		t.Errorf("summary: %s", s.Description)
	}
}
