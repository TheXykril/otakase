package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestAwardHelper(t *testing.T) {
	d := &storeData{}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, ok := awardHelper(d, "poster", "t1", "poster", now); ok {
		t.Fatal("no point for helping yourself")
	}
	if n, ok := awardHelper(d, "a", "t1", "poster", now); !ok || n != 1 {
		t.Fatalf("first point: %d %v", n, ok)
	}
	if n, ok := awardHelper(d, "a", "t1", "poster", now); ok || n != 1 {
		t.Fatalf("second point on the same post: %d %v", n, ok)
	}
	if n, ok := awardHelper(d, "a", "t2", "other", now); !ok || n != 2 {
		t.Fatalf("point on another post: %d %v", n, ok)
	}
	if !d.Helpers["a"].Last.Equal(now) {
		t.Fatal("last awarded not set")
	}
}

func TestAwardHelperKeepsThreadsSmall(t *testing.T) {
	d := &storeData{}
	for k := 0; k < helperThreadsKept+10; k++ {
		awardHelper(d, "a", fmt.Sprint("t", k), "p", time.Now())
	}
	if h := d.Helpers["a"]; h.Points != helperThreadsKept+10 || len(h.Threads) != helperThreadsKept {
		t.Fatalf("points %d threads %d", h.Points, len(h.Threads))
	}
}

func TestRankHelpers(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h := map[string]helperStat{
		"a":    {Points: 3, Last: t0.Add(time.Hour)},
		"b":    {Points: 5, Last: t0},
		"c":    {Points: 3, Last: t0},
		"zero": {Points: 0},
	}
	r := rankHelpers(h)
	got := []string{}
	for _, x := range r {
		got = append(got, x.ID)
	}
	if strings.Join(got, ",") != "b,c,a" {
		t.Fatalf("order %v", got)
	}
	if rankOf(r, "a") != 3 || rankOf(r, "zero") != 0 {
		t.Fatal("rankOf")
	}
	e := helpersEmbed(h, "a", 5)
	if !strings.Contains(e.Description, "**1.** <@b> · 5 helper points") || !strings.Contains(e.Description, "You're **#3** with 3 helper points") {
		t.Fatalf("embed:\n%s", e.Description)
	}
	if e := helpersEmbed(nil, "a", 5); !strings.Contains(e.Description, "Nobody has helper points yet") {
		t.Fatalf("empty board:\n%s", e.Description)
	}
}

func TestRepliers(t *testing.T) {
	u := func(id string, bot bool) *discordgo.User { return &discordgo.User{ID: id, Username: id, Bot: bot} }
	// Newest first, like ChannelMessages.
	msgs := []*discordgo.Message{
		{Author: u("b", false)}, {Author: u("bot", true)}, {Author: u("poster", false)},
		{Author: u("a", false)}, {Author: u("b", false)}, {Author: nil},
	}
	got := repliers(msgs, "poster")
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "a" {
		t.Fatalf("repliers %v", got)
	}
	var many []*discordgo.Message
	for k := 0; k < 40; k++ {
		many = append(many, &discordgo.Message{Author: u(fmt.Sprint(k), false)})
	}
	if n := len(repliers(many)); n != 25 {
		t.Fatalf("capped at %d", n)
	}
}

func TestWhoHelpedPrompt(t *testing.T) {
	p := whoHelpedPrompt("th", []*discordgo.User{{ID: "1", Username: "one"}}, map[string]string{"1": "One"})
	menu := p.Components[0].(discordgo.ActionsRow).Components[0].(discordgo.SelectMenu)
	if menu.MenuType != discordgo.StringSelectMenu || menu.CustomID != "helped:th" || menu.Options[0].Label != "One" || menu.MaxValues != 1 {
		t.Fatalf("menu %+v", menu)
	}
	skip := p.Components[1].(discordgo.ActionsRow).Components[0].(discordgo.Button)
	if skip.CustomID != "helped-skip:th" || p.Flags != discordgo.MessageFlagsEphemeral {
		t.Fatalf("skip %+v", skip)
	}
	p = whoHelpedPrompt("th", nil, nil)
	if menu := p.Components[0].(discordgo.ActionsRow).Components[0].(discordgo.SelectMenu); menu.MenuType != discordgo.UserSelectMenu {
		t.Fatal("no repliers should give a member picker")
	}
}

func TestJoinNames(t *testing.T) {
	if joinNames([]string{"a"}) != "a" || joinNames([]string{"a", "b"}) != "a and b" || joinNames([]string{"a", "b", "c"}) != "a, b and c" {
		t.Fatal("joinNames")
	}
}

func TestAnswerVotes(t *testing.T) {
	d := &storeData{Answers: map[string]answerMsg{"m": {Keys: []string{"log:mpv", "log:network"}}}}
	if _, r := recordAnswerVote(d, "nope", "u", true); r != voteUnknown {
		t.Fatal("unknown message")
	}
	if _, r := recordAnswerVote(d, "m", "u", false); r != voteCounted {
		t.Fatal("first vote")
	}
	if _, r := recordAnswerVote(d, "m", "u", true); r != voteAgain {
		t.Fatal("one vote per person")
	}
	recordAnswerVote(d, "m", "v", true)
	if s := d.AnswerStats["log:mpv"]; s.Helped != 1 || s.NotHelped != 1 {
		t.Fatalf("tally %+v", s)
	}
	e := answerStatsEmbed(map[string]answerTally{"faq:mpv": {Helped: 9, NotHelped: 1}, "version": {NotHelped: 4}})
	if !strings.HasPrefix(e.Description, "**Update reminder** · didn't help 4") {
		t.Fatalf("stats:\n%s", e.Description)
	}
}

func TestPruneAnswers(t *testing.T) {
	now := time.Now()
	a := map[string]answerMsg{"old": {At: now.Add(-answerKeep - time.Hour)}, "new": {At: now}}
	pruneAnswers(a, now)
	if _, ok := a["old"]; ok || len(a) != 1 {
		t.Fatalf("old kept: %v", a)
	}
	for k := 0; k < answersKept+5; k++ {
		a[fmt.Sprint(k)] = answerMsg{At: now.Add(time.Duration(k) * time.Second)}
	}
	pruneAnswers(a, now)
	if len(a) != answersKept {
		t.Fatalf("kept %d", len(a))
	}
	if _, ok := a["new"]; ok {
		t.Fatal("oldest should go first")
	}
}

func TestAnswerLabels(t *testing.T) {
	cases := map[string]string{
		"faq:mpv":                      "FAQ: Playback never starts",
		"log:mpv":                      "Log fix: mpv is missing",
		"wiki:Troubleshooting#mpv-fix": "Wiki: Troubleshooting › mpv fix",
		"version":                      "Update reminder",
	}
	for k, want := range cases {
		if got := answerLabel(k); got != want {
			t.Errorf("%s: %q, want %q", k, got, want)
		}
	}
	e := &discordgo.MessageEmbed{Description: "**[Casting › Firewall](" + wikiURL + "/Casting-Problems#firewall)**\nx\n\n**[Tracking](" + wikiURL + "/Tracking)**"}
	if got := wikiAnswerKeys(e); strings.Join(got, ",") != "wiki:Casting-Problems#firewall,wiki:Tracking" {
		t.Fatalf("wiki keys %v", got)
	}
}

func TestAnswerButtonsOnReplies(t *testing.T) {
	r := logReply([]logHint{logHints[0]})
	if len(r.Components) != 2 {
		t.Fatalf("log reply rows: %d", len(r.Components))
	}
	row := r.Components[1].(discordgo.ActionsRow)
	if row.Components[0].(discordgo.Button).CustomID != "ans-up" || row.Components[1].(discordgo.Button).CustomID != "ans-down" {
		t.Fatal("answer buttons")
	}
	if answerRows(false) != nil || len(answerRows(true)) != 1 {
		t.Fatal("answerRows")
	}
}

func TestOSTags(t *testing.T) {
	cases := map[string]string{
		"Arch Linux":        "linux",
		"ubuntu 24.04":      "linux",
		"Windows 11":        "windows",
		"win11 + WSL":       "windows",
		"macOS Sequoia":     "macos",
		"MacBook, Sonoma":   "macos",
		"Steam Deck":        "linux",
		"my phone, i guess": "",
	}
	for in, want := range cases {
		got := ""
		if n := osTagNames(in); len(n) > 0 {
			got = n[0]
		}
		if got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	forum := &discordgo.Channel{AvailableTags: []discordgo.ForumTag{{ID: "1", Name: "Linux"}, {ID: "2", Name: "Mac"}, {ID: "3", Name: "Solved"}}}
	if got := osTags(forum, "macOS 15"); len(got) != 1 || got[0] != "2" {
		t.Fatalf("mac tag %v", got)
	}
	if got := osTags(forum, "Windows"); len(got) != 0 {
		t.Fatalf("no windows tag on this forum: %v", got)
	}
}

func TestSupportPostText(t *testing.T) {
	if supportTitle("  mpv won't start\nmore text") != "mpv won't start" || supportTitle("ok") != "Help needed" {
		t.Fatal("title")
	}
	if got := supportTitle(strings.Repeat("x", 300)); len([]rune(got)) != 100 {
		t.Fatalf("title length %d", len([]rune(got)))
	}
	body := supportBody("42", map[string]string{"system": "Arch\nLinux", "version": "26.5.0", "install": "AUR",
		"what": "It plays nothing", "steps": "[LOG] 2026/01/02 15:04:05 No episode links found"})
	for _, want := range []string{"<@42> needs help", "**System:** Arch Linux", "**Otakase version:** 26.5.0", "**Installed with:** AUR",
		"**What happened**\nIt plays nothing", "```\n[LOG] 2026/01/02"} {
		if !strings.Contains(body, want) {
			t.Errorf("body misses %q:\n%s", want, body)
		}
	}
	if strings.Contains(supportBody("1", map[string]string{"what": "x"}), "Steps or error") {
		t.Error("empty steps shown")
	}
	long := supportBody("1", map[string]string{"what": strings.Repeat("a", 900), "steps": strings.Repeat("b", 900), "system": strings.Repeat("c", 80)})
	if len([]rune(long)) > 2000 {
		t.Errorf("body too long: %d", len([]rune(long)))
	}
}

const diagBlock = "```\n" +
	"otakase diagnostics\n" +
	"Otakase version: 26.3.1\n" +
	"OS: linux/amd64\n" +
	"mpv: not found\n" +
	"Config: /home/me/.config/otakase/otakase.conf\n" +
	"Log: /home/me/.local/share/otakase/otakase-debug.log\n" +
	"--- last 2 log lines ---\n" +
	"[LOG] 2026/01/02 15:04:05 Starting\n" +
	"[LOG] 2026/01/02 15:04:06 No episode links found\n" +
	"```\n"

func TestDiagnosticsBlockIsRead(t *testing.T) {
	if !looksLikeLog("otakase diagnostics\nOtakase version: 26.3.1") {
		t.Error("diagnostics header not seen as a log")
	}
	if v, ok := mentionedVersion(diagBlock); !ok || v != "26.3.1" {
		t.Errorf("version %q %v", v, ok)
	}
	var keys []string
	for _, h := range diagnoseLog(diagBlock) {
		keys = append(keys, h.Key)
	}
	if strings.Join(keys, ",") != "mpv,no-stream" {
		t.Errorf("hints %v", keys)
	}
	if len(diagnoseLog("mpv: not found here, can someone help")) != 0 {
		t.Error("mpv line only counts on its own")
	}
}

func TestDiagnoseEmbed(t *testing.T) {
	e := diagnoseEmbed()
	if len(e.Fields) != 3 || !strings.Contains(e.Fields[2].Value, "otakase -diag | clip") {
		t.Fatalf("%+v", e.Fields)
	}
}
