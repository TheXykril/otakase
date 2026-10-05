package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestAutoRules(t *testing.T) {
	cases := map[string]string{
		"it says mpv not found when I play":                 "mpv",
		"the menu icons are boxes for me":                   "icons",
		"nothing found when I search for frieren":           "nothing-found",
		"my anilist progress doesn't update after watching": "sync",
		"chromecast not showing up at all":                  "cast",
		"how do i update otakase on windows":                "install",
		"just watched the new episode, great stuff":         "",
	}
	for text, want := range cases {
		a := newAutoReplier()
		f, ok := a.match("c", text)
		if got := map[bool]string{true: f.Key, false: ""}[ok]; got != want {
			t.Errorf("%q: got %q, want %q", text, got, want)
		}
	}
}

func TestAutoReplyCooldown(t *testing.T) {
	a := newAutoReplier()
	if _, ok := a.match("c", "it says mpv not found"); !ok {
		t.Fatal("first match missing")
	}
	if _, ok := a.match("c", "still mpv not found"); ok {
		t.Fatal("second match within cooldown")
	}
	if _, ok := a.match("other", "mpv not found here too"); !ok {
		t.Fatal("other channel should still match")
	}
}

func TestRulesPointAtFAQ(t *testing.T) {
	for _, r := range autoRules {
		if _, ok := faqByKey(r.faq); !ok {
			t.Errorf("rule points at missing FAQ %q", r.faq)
		}
	}
	for _, k := range []string{"linux", "arch", "windows", "mac"} {
		if installGuides[k].Body == "" {
			t.Errorf("no install guide for %s", k)
		}
	}
}

func TestLoadEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(path, []byte("# comment\nOTK_A=\"one\"\nOTK_B = two\nbad line\n"), 0o600)
	t.Setenv("OTK_B", "kept")
	t.Setenv("OTK_A", "")
	os.Unsetenv("OTK_A")
	loadEnvFile(path)
	if got := os.Getenv("OTK_A"); got != "one" {
		t.Errorf("OTK_A = %q", got)
	}
	if got := os.Getenv("OTK_B"); got != "kept" {
		t.Errorf("OTK_B = %q, want existing value kept", got)
	}
}

func TestLoadHAOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "options.json")
	os.WriteFile(path, []byte(`{"token":"abc","guild_id":"123","welcome_dm":false}`), 0o600)
	for _, env := range haOptions {
		t.Setenv(env, "")
		os.Unsetenv(env)
	}
	loadHAOptions(path)
	for env, want := range map[string]string{"DISCORD_BOT_TOKEN": "abc", "OTAKASE_GUILD_ID": "123", "OTAKASE_WELCOME_DM": "false"} {
		if got := os.Getenv(env); got != want {
			t.Errorf("%s = %q, want %q", env, got, want)
		}
	}
	if _, set := os.LookupEnv("GITHUB_TOKEN"); set {
		t.Error("GITHUB_TOKEN set although the option is missing")
	}
}

func TestPresenceActivities(t *testing.T) {
	p := &presence{}
	without := len(p.activities())
	p.version = "26.6.0"
	list := p.activities()
	if len(list) != without+1 {
		t.Fatalf("version activity not added: %d vs %d", len(list), without)
	}
	for _, a := range list {
		if a.Name == "" {
			t.Error("activity without a name")
		}
		if a.Type == discordgo.ActivityTypeCustom && a.State == "" {
			t.Error("custom status without text")
		}
	}
}

func TestIssueRefs(t *testing.T) {
	cases := map[string][]int{
		"fixed in #82, see also #81 and #82":             {82, 81},
		"channel <#1556589777287913492> is not an issue": nil,
		"url https://x.y/#12 and `#5` in code":           nil,
		"(#7) #8 #9 #10":                                 {7, 8, 9},
		"#0 is not one":                                  nil,
	}
	for text, want := range cases {
		got := issueRefs(text)
		if fmt.Sprint(got) != fmt.Sprint(want) && !(len(got) == 0 && len(want) == 0) {
			t.Errorf("issueRefs(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestIssueStatus(t *testing.T) {
	merged := time.Now()
	pr := ghIssue{State: "closed", PullRequest: &struct {
		MergedAt *time.Time `json:"merged_at"`
	}{&merged}}
	if s, _ := issueStatus(pr); s != "Pull request · merged" {
		t.Errorf("merged PR: %q", s)
	}
	if s, _ := issueStatus(ghIssue{State: "open"}); s != "Issue · open" {
		t.Errorf("open issue: %q", s)
	}
}

const sampleStatus = `Provider status (search query: "one piece")

  ✗ anidb       [disabled]
      FAILED after 483ms: anidb.app is under maintenance
      reason: anidb.app is serving a maintenance page; add "anidb" to Provider to try it anyway

  ✓ anikoto     [enabled, in stack]
      20 result(s) in 1.595s

  ✗ sukebei     [disabled]
      FAILED after 905ms: no results for "one piece"
      reason: sukebei only serves adult titles; set AdultContent=true to use it

2 of 3 providers returned results.
`

func TestParseProviderStatus(t *testing.T) {
	list := parseProviderStatus(sampleStatus)
	if len(list) != 2 {
		t.Fatalf("got %d providers, want 2 (adult one hidden): %+v", len(list), list)
	}
	if list[0].Name != "anidb" || list[0].OK || list[0].Enabled {
		t.Errorf("anidb parsed wrong: %+v", list[0])
	}
	if list[1].Name != "anikoto" || !list[1].OK || !strings.Contains(providerLine(list[1]), "1.595s") {
		t.Errorf("anikoto parsed wrong: %+v / %s", list[1], providerLine(list[1]))
	}
	if !strings.Contains(providerLine(list[0]), "under maintenance") {
		t.Errorf("failure detail missing: %s", providerLine(list[0]))
	}
}

func TestAiringEmbed(t *testing.T) {
	var list []airing
	for k := 0; k < 30; k++ {
		list = append(list, airing{At: int64(1000 - k), Episode: 3, Episodes: 3, Title: fmt.Sprint("Show ", k), URL: "u", Popularity: k})
	}
	e := airingEmbed(time.Date(2026, 10, 5, 6, 0, 0, 0, time.UTC), list)
	lines := strings.Split(e.Description, "\n")
	if len(lines) != 20 {
		t.Fatalf("got %d lines, want the 20 most popular", len(lines))
	}
	if !strings.Contains(lines[0], "Show 29") || !strings.Contains(lines[0], "finale") {
		t.Errorf("first line should be the earliest of the popular ones: %s", lines[0])
	}
	if e.Title != "Airing today · Mon 5 Oct" {
		t.Errorf("title %q", e.Title)
	}
}

func TestBugReportURL(t *testing.T) {
	u := bugReportURL("Playback never starts", "mpv opens then closes", "https://discord.com/channels/1/2")
	for _, want := range []string{"/issues/new?", "title=Playback+never+starts", "labels=bug", "mpv+opens+then+closes"} {
		if !strings.Contains(u, want) {
			t.Errorf("%s missing %q", u, want)
		}
	}
}

func TestIsNudge(t *testing.T) {
	m := &discordgo.Message{Author: &discordgo.User{ID: "bot"}, Embeds: []*discordgo.MessageEmbed{{Title: nudgeTitle}}}
	if !isNudge(m, "bot") || isNudge(m, "someone") {
		t.Error("isNudge wrong")
	}
}
