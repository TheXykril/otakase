package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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

func TestDownRe(t *testing.T) {
	for _, text := range []string{
		"is otakase down?", "are the sources down rn", "is anikoto broken", "providers not working today",
		"down for everyone or just me?", "anyone else getting no results?",
	} {
		if !downRe.MatchString(text) {
			t.Errorf("%q should count as a down question", text)
		}
	}
	for _, text := range []string{"I'm feeling down today", "scroll down to the bottom", "the episode was great"} {
		if downRe.MatchString(text) {
			t.Errorf("%q should not count", text)
		}
	}
}

func TestSemanticIntents(t *testing.T) {
	for intent := range intentExamples {
		if _, ok := faqByKey(intent); !ok && intent != "down" {
			t.Errorf("intent %q has no FAQ entry", intent)
		}
	}
	ex := []semanticExample{{"mpv", []float64{1, 0}}, {"down", []float64{0, 1}}}
	if got, ok := bestIntent([]float64{0.9, 0.1}, ex, 0.6); !ok || got != "mpv" {
		t.Errorf("bestIntent = %q %v", got, ok)
	}
	if _, ok := bestIntent([]float64{1, 1}, ex, 0.8); ok {
		t.Error("a vector halfway between should stay under the threshold")
	}
	if math.Abs(cosine([]float64{1, 2}, []float64{2, 4})-1) > 1e-9 || cosine(nil, nil) != 0 {
		t.Error("cosine wrong")
	}
	for text, want := range map[string]bool{
		"my player won't open": true, "how do I cast?": true, "lol nice episode": false, "good morning everyone": false,
	} {
		if questionRe.MatchString(text) != want {
			t.Errorf("questionRe(%q) = %v", text, !want)
		}
	}
}

// fakeOllama answers /api/pull and /api/embed, embedding texts that mention
// "player" along one axis and everything else along the other.
func fakeOllama(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/pull":
			w.Write([]byte(`{"status":"success"}`))
		case "/api/embed":
			var req struct {
				Model string   `json:"model"`
				Input []string `json:"input"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			var out [][]float64
			for _, s := range req.Input {
				if strings.Contains(s, "player") || strings.Contains(s, "mpv") {
					out = append(out, []float64{1, 0})
				} else {
					out = append(out, []float64{0, 1})
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"embeddings": out})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
}

func TestSemanticWithOllama(t *testing.T) {
	srv := fakeOllama(t)
	defer srv.Close()
	sm := &semantic{url: srv.URL, model: "all-minilm", threshold: 0.9}
	if err := sm.load(); err != nil {
		t.Fatal(err)
	}
	if got, ok := sm.match("my player won't open, why?"); !ok || got != "mpv" {
		t.Errorf("match = %q %v, want mpv", got, ok)
	}
	if _, ok := sm.match("good morning everyone"); ok {
		t.Error("plain chat should not be matched")
	}
}

func TestPartyHost(t *testing.T) {
	ev := &discordgo.GuildScheduledEvent{Description: "Watch party hosted by <@123456789>. Join the voice channel.\nhttps://anilist.co/anime/1"}
	if got := partyHost(ev); got != "123456789" {
		t.Errorf("partyHost = %q", got)
	}
	if partyHost(&discordgo.GuildScheduledEvent{Description: "some other event"}) != "" {
		t.Error("an event the bot didn't make has no host")
	}
}

func TestScamReason(t *testing.T) {
	scams := []string{
		"FREE NITRO for everyone https://dlscord-gift.com/claim",
		"free discord nitro 3 months https://example.xyz/n",
		"I'm leaving cs2, giving away my skins https://stearncommunity.ru/trade",
		"steam gift for you bro https://steam-gift.shop/abc",
		"check this https://steamcommunlty.com/tradeoffer/new",
		"@everyone new airdrop https://claim-now.top",
		"https://discord-nitro.gift/xyz",
	}
	for _, s := range scams {
		if scamReason(s) == "" {
			t.Errorf("missed scam: %q", s)
		}
	}
	fine := []string{
		"join us at https://discord.gg/AfrDAeWbcW",
		"my steam profile https://steamcommunity.com/id/someone",
		"free nitro would be nice lol",
		"discord.js docs https://discord.js.org/docs",
		"is there a free version? https://otakase.xyverion.com",
		"https://store.steampowered.com/app/123 is free this week",
		"@everyone the release is out",
	}
	for _, s := range fine {
		if r := scamReason(s); r != "" {
			t.Errorf("false positive %q: %s", s, r)
		}
	}
}

func TestRaidGuard(t *testing.T) {
	var g raidGuard
	now := time.Now()
	for n := 0; n < raidJoins-1; n++ {
		if g.join(now.Add(time.Duration(n) * time.Second)) {
			t.Fatal("alert too early")
		}
	}
	if !g.join(now.Add(20 * time.Second)) {
		t.Fatal("no alert at the limit")
	}
	if g.join(now.Add(21 * time.Second)) {
		t.Fatal("alerted twice")
	}
	var slow raidGuard
	for n := 0; n < 30; n++ {
		if slow.join(now.Add(time.Duration(n) * 10 * time.Second)) {
			t.Fatal("slow joins alerted")
		}
	}
}

func TestNewcomerLink(t *testing.T) {
	now := time.Now()
	fresh := snowflakeAt(now.Add(-2 * 24 * time.Hour))
	old := snowflakeAt(now.Add(-400 * 24 * time.Hour))
	msg := func(author string, joined time.Time, text string) *discordgo.MessageCreate {
		return &discordgo.MessageCreate{Message: &discordgo.Message{Content: text, Author: &discordgo.User{ID: author},
			Member: &discordgo.Member{JoinedAt: joined}}}
	}
	if !newcomerLink(msg(fresh, now.Add(-time.Hour), "look https://x.io"), now) {
		t.Error("new account link not caught")
	}
	if newcomerLink(msg(fresh, now.Add(-time.Hour), "hello there"), now) {
		t.Error("plain text caught")
	}
	if newcomerLink(msg(old, now.Add(-time.Hour), "look https://x.io"), now) {
		t.Error("old account caught")
	}
	if newcomerLink(msg(fresh, now.Add(-30*time.Hour), "look https://x.io"), now) {
		t.Error("caught after the first day")
	}
}

func TestPurgeIDs(t *testing.T) {
	now := time.Now()
	msgs := []*discordgo.Message{
		{ID: "1", Author: &discordgo.User{ID: "a"}, Timestamp: now},
		{ID: "2", Author: &discordgo.User{ID: "b"}, Timestamp: now},
		{ID: "3", Author: &discordgo.User{ID: "a"}, Timestamp: now.Add(-15 * 24 * time.Hour)},
	}
	if got := purgeIDs(msgs, "", now); len(got) != 2 {
		t.Errorf("all: %v", got)
	}
	if got := purgeIDs(msgs, "a", now); len(got) != 1 || got[0] != "1" {
		t.Errorf("only a: %v", got)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	st := openStore(path)
	st.update(func(d *storeData) {
		d.Warnings["u"] = append(d.Warnings["u"], warning{Reason: "spam"})
		d.Rooms["c"] = room{Owner: "u"}
	})
	again := openStore(path)
	if len(again.data.Warnings["u"]) != 1 || again.data.Rooms["c"].Owner != "u" {
		t.Errorf("reloaded: %+v", again.data)
	}
	if e := warningsEmbed("u", again.data.Warnings["u"]); !strings.Contains(e.Description, "spam") {
		t.Errorf("embed: %s", e.Description)
	}
}

func TestRoomBits(t *testing.T) {
	for name, want := range map[string]bool{"➕ Create room": true, "声・create-room": true, "Join to Create": true, "lounge": false} {
		if isCreateChannel(name) != want {
			t.Errorf("%q", name)
		}
	}
	if o := roomOverwrites("g", "bot", "u", []string{"mod"}, false); len(o) != 1 {
		t.Errorf("public: %d overwrites", len(o))
	}
	o := roomOverwrites("g", "bot", "u", []string{"mod"}, true)
	if len(o) != 4 || o[1].ID != "g" || o[1].Deny&discordgo.PermissionViewChannel == 0 {
		t.Errorf("private: %+v", o)
	}
}

func snowflakeAt(t time.Time) string {
	return strconv.FormatInt((t.UnixMilli()-1420070400000)<<22, 10)
}

func TestChannelBase(t *testing.T) {
	for in, want := range map[string]string{"助・support": "support", "守・mod-chat": "mod-chat", "lounge": "lounge"} {
		if got := channelBase(in); got != want {
			t.Errorf("%q: got %q", in, got)
		}
	}
}
