package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
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
		if a.Type != discordgo.ActivityTypeCustom || a.State == "" {
			t.Errorf("not a custom status with text: %+v", a)
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

func TestBoardShows(t *testing.T) {
	var all []airing
	for k := 0; k < 30; k++ {
		all = append(all, airing{ID: k, At: int64(100000 - k*60), Episode: 3, Title: fmt.Sprint("Show ", k), Popularity: k})
	}
	shows := boardShows(all)
	if len(shows) != boardSize {
		t.Fatalf("got %d shows, want the %d most popular", len(shows), boardSize)
	}
	if shows[0].ID != 29 || shows[len(shows)-1].ID != 10 {
		t.Errorf("want the popular ones by time, got first %d last %d", shows[0].ID, shows[len(shows)-1].ID)
	}
}

func TestBoardMessage(t *testing.T) {
	day := time.Date(2026, 10, 17, 6, 0, 0, 0, time.UTC).Unix()
	at := func(h, m int) int64 { return day + int64(h*3600+m*60) }
	shows := []airing{
		{ID: 1, At: at(1, 30), Episode: 1, Title: "Early"},
		{ID: 2, At: at(2, 0), Episode: 12, Episodes: 12, Title: "Done_Soon"},
		{ID: 3, At: at(2, 45), Episode: 3, Title: "Same hour"},
		{ID: 4, At: at(4, 0), Episode: 5, Title: "Next"},
		{ID: 5, At: at(6, 0), Episode: 7, Title: "Later"},
	}
	now := time.Unix(at(2, 20), 0)
	comps := boardMessage(shows, now, "attachment://airing.png", boardMarks{Live: "<:otk_an1:1><:otk_an2:1>", Next: "<:otk_un1:2>", Aired: "<:otk_aired:3>"})
	box, ok := comps[0].(discordgo.Container)
	if len(comps) != 1 || !ok || box.AccentColor == nil || *box.AccentColor != shu {
		t.Fatalf("want one Shu card, got %#v", comps)
	}
	if _, ok := box.Components[0].(discordgo.MediaGallery); !ok {
		t.Fatalf("picture should come first: %#v", box.Components[0])
	}
	var texts []string
	for _, c := range box.Components {
		if td, ok := c.(discordgo.TextDisplay); ok {
			texts = append(texts, td.Content)
		}
	}
	if len(texts) != 4 {
		t.Fatalf("want 3 hours + aired, got %q", texts)
	}
	if !strings.Contains(texts[0], "\n<:otk_an1:1><:otk_an2:1>\n") || !strings.Contains(texts[0], `Done\_Soon *ep 12 · finale*`) || !strings.Contains(texts[0], "Same hour") {
		t.Errorf("current hour: %q", texts[0])
	}
	if !strings.Contains(texts[1], "\n<:otk_un1:2> · <t:") || strings.Contains(texts[2], "otk_un1") {
		t.Errorf("only the next hour is up next: %q / %q", texts[1], texts[2])
	}
	if !strings.HasPrefix(texts[3], "-# <:otk_aired:3> **AIRED TODAY**\n-# <t:") || !strings.Contains(texts[3], "Early ep 1 · new") || strings.Contains(texts[3], "~~") {
		t.Errorf("aired hours should be small lines at the bottom, not crossed out: %q", texts[3])
	}
	plain := boardMessage(shows, now, "", boardMarks{})[0].(discordgo.Container)
	if td := plain.Components[0].(discordgo.TextDisplay); !strings.Contains(td.Content, "\n**AIRING NOW**") {
		t.Errorf("without marks the label stands alone: %q", td.Content)
	}
	if boardState(shows, now) == boardState(shows, now.Add(time.Hour+45*time.Minute)) {
		t.Error("state should change when the next hour starts")
	}
	if boardState(shows, now) != boardState(shows, now.Add(-10*time.Minute)) {
		t.Error("state should hold within an hour")
	}
}

func TestBoardMessageFitsComponentLimit(t *testing.T) {
	var shows []airing
	for k := range boardSize {
		shows = append(shows, airing{ID: k, At: int64(10_000_000 + k*3600), Episode: 1, Title: "S"})
	}
	n := 0
	var count func(c discordgo.MessageComponent)
	count = func(c discordgo.MessageComponent) {
		n++
		switch v := c.(type) {
		case discordgo.Container:
			for _, x := range v.Components {
				count(x)
			}
		case discordgo.ActionsRow:
			for _, x := range v.Components {
				count(x)
			}
		}
	}
	for _, c := range boardMessage(shows, time.Unix(0, 0), "x", boardMarks{}) {
		count(c)
	}
	if n > 40 {
		t.Errorf("%d components, Discord allows 40", n)
	}
}

func TestMyAiringEmbed(t *testing.T) {
	all := []airing{{ID: 1, At: 200, Episode: 2, Title: "B", URL: "u"}, {ID: 2, At: 100, Episode: 1, Title: "A", URL: "u"}, {ID: 3, At: 50, Title: "C"}}
	e := myAiringEmbed(all, map[int]bool{1: true, 2: true})
	lines := strings.Split(e.Description, "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "[A]") {
		t.Errorf("want A then B: %q", e.Description)
	}
	if e := myAiringEmbed(all, nil); !strings.Contains(e.Description, "Nothing") {
		t.Errorf("empty: %q", e.Description)
	}
}

func TestSynopsis(t *testing.T) {
	got := synopsis("A <i>witch</i> &amp; her crush.<br><br>\n(Source: X)", 100)
	if got != "A witch & her crush.\n(Source: X)" {
		t.Errorf("got %q", got)
	}
}

func TestRenderAiringBoard(t *testing.T) {
	for _, n := range []int{0, 1, 4, 7} {
		var shows []boardShow
		for k := range n {
			shows = append(shows, boardShow{Title: fmt.Sprint("A rather long show title number ", k), Episode: k + 1})
		}
		png, err := renderAiringBoard("Saturday 17 Oct", n, shows)
		if err != nil || len(png) < 1000 {
			t.Fatalf("%d shows: %v (%d bytes)", n, err, len(png))
		}
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
	if e := warningsEmbed("u", again.data.Warnings["u"], time.Now(), 0); !strings.Contains(e.Description, "spam") {
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

func TestPartyOverwrites(t *testing.T) {
	everyone := func(mode string) *discordgo.PermissionOverwrite {
		for _, o := range mergeOverwrites(roomOverwrites("g", "bot", "u", nil, false), partyOverwrites("g", "u", mode)) {
			if o.ID == "g" {
				return o
			}
		}
		return nil
	}
	if e := everyone(partyTalk); e.Allow != discordgo.PermissionVoiceSpeak || e.Deny != discordgo.PermissionVoiceStreamVideo {
		t.Errorf("talk: %+v", e)
	}
	if e := everyone(partyShare); e.Allow != partyVoice || e.Deny != 0 {
		t.Errorf("share: %+v", e)
	}
	if e := everyone(partyHostOnly); e.Allow != 0 || e.Deny != partyVoice {
		t.Errorf("host: %+v", e)
	}
	o := mergeOverwrites(roomOverwrites("g", "bot", "u", nil, false), partyOverwrites("g", "u", partyHostOnly))
	if len(o) != 2 || o[0].ID != "u" || o[0].Allow&partyVoice != partyVoice || o[0].Allow&discordgo.PermissionManageChannels == 0 {
		t.Errorf("host overwrite: %+v", o)
	}
}

func TestIsPartyCategory(t *testing.T) {
	cat, voice := discordgo.ChannelTypeGuildCategory, discordgo.ChannelTypeGuildVoice
	for _, tc := range []struct {
		c    discordgo.Channel
		want bool
	}{
		{discordgo.Channel{Name: "WatchParty", Type: cat}, true},
		{discordgo.Channel{Name: "映・watch-party", Type: cat}, true},
		{discordgo.Channel{Name: "Watch Parties", Type: cat}, true},
		{discordgo.Channel{Name: "voice", Type: cat}, false},
		{discordgo.Channel{Name: "WatchParty", Type: voice}, false},
	} {
		if isPartyCategory(&tc.c) != tc.want {
			t.Errorf("%q %v", tc.c.Name, tc.c.Type)
		}
	}
}

func TestCategoriesFirst(t *testing.T) {
	cat := discordgo.ChannelTypeGuildCategory
	o := categoriesFirst([]*discordgo.Channel{
		{ID: "b", Type: cat, Position: 2}, {ID: "v", Type: discordgo.ChannelTypeGuildVoice}, {ID: "a", Type: cat, Position: 0},
	}, "new")
	var got []string
	for n, c := range o {
		if c.Position != n {
			t.Errorf("%s at %d", c.ID, c.Position)
		}
		got = append(got, c.ID)
	}
	if strings.Join(got, ",") != "new,a,b" {
		t.Errorf("order %v", got)
	}
}

func TestTitleLabel(t *testing.T) {
	m := titleMatch{ID: 1, Format: "TV_SHORT", SeasonYear: 2024}
	m.Title.English, m.Title.Romaji = "Frieren", "Sousou no Frieren"
	if got := titleLabel(m); got != "Frieren (Sousou no Frieren) · TV SHORT, 2024" {
		t.Errorf("%q", got)
	}
	m.Title.English, m.Title.Romaji = "", strings.Repeat("a", 200)
	if got := titleLabel(m); len([]rune(got)) > 100 {
		t.Errorf("too long: %d", len([]rune(got)))
	}
	if !pickedShowRe.MatchString("anilist:154587") || pickedShowRe.MatchString("frieren") {
		t.Error("picked value")
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

func TestQuestionFilter(t *testing.T) {
	for _, s := range []string{"my list doesnt save what i watched", "anilist not updating after episodes", "player wont open"} {
		if !questionRe.MatchString(s) {
			t.Errorf("skipped %q", s)
		}
	}
	if questionRe.MatchString("just finished frieren, so good") {
		t.Error("plain chat matched")
	}
}

func TestSuggestionVote(t *testing.T) {
	s := &suggestion{}
	s.vote("a", true)
	s.vote("b", false)
	if len(s.Up) != 1 || len(s.Down) != 1 {
		t.Fatalf("up %v down %v", s.Up, s.Down)
	}
	s.vote("a", true) // same button again takes the vote back
	if len(s.Up) != 0 {
		t.Errorf("vote not taken back: %v", s.Up)
	}
	s.vote("b", true) // other button moves it
	if len(s.Up) != 1 || len(s.Down) != 0 {
		t.Errorf("vote not moved: up %v down %v", s.Up, s.Down)
	}
	s.vote("b", true)
	s.vote("b", true)
	if len(s.Up) != 1 {
		t.Errorf("double vote: %v", s.Up)
	}
}

func TestSuggestionClosed(t *testing.T) {
	s := &suggestion{Status: "done", Up: []string{"a", "b"}}
	row := s.components()[0].(discordgo.ActionsRow)
	up := row.Components[0].(discordgo.Button)
	if !up.Disabled || up.Label != "2" {
		t.Errorf("done suggestion button: %+v", up)
	}
	if s.embed().Color != statusByKey("done").Color {
		t.Error("status colour not used")
	}
	if (&suggestion{}).components()[0].(discordgo.ActionsRow).Components[1].(discordgo.Button).Disabled {
		t.Error("open suggestion closed")
	}
}

func TestSuggestionWait(t *testing.T) {
	b := &bot{store: openStore(filepath.Join(t.TempDir(), "s.json"))}
	now := time.Now()
	if b.suggestionWait("a", now) != 0 {
		t.Error("wait with no suggestions")
	}
	b.store.update(func(d *storeData) { d.Suggestions["m"] = suggestion{Author: "a", Created: now.Add(-time.Minute)} })
	if w := b.suggestionWait("a", now); w != 4*time.Minute {
		t.Errorf("wait %v", w)
	}
	if b.suggestionWait("b", now) != 0 {
		t.Error("other member waits")
	}
}

func TestWelcomePayload(t *testing.T) {
	chans := []*discordgo.Channel{
		{ID: "1", Name: "則・rules", Type: discordgo.ChannelTypeGuildText},
		{ID: "2", Name: "案・suggestions", Type: discordgo.ChannelTypeGuildText},
		{ID: "3", Name: "➕ Create room", Type: discordgo.ChannelTypeGuildVoice},
		{ID: "4", Name: "General", Type: discordgo.ChannelTypeGuildCategory},
	}
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode(welcomePayload(chans))
	s := buf.String()
	for _, want := range []string{"<#1>  Read before posting", "<#2>  Suggest features", "<#3>  Join to get your own voice room", "attachment://welcome-header.png"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(s, "<#4>") || strings.Contains(s, "faq") {
		t.Error("linked a category or a missing channel")
	}
	if len(welcomeHeader) == 0 {
		t.Error("header image not embedded")
	}
}

func TestHealthAlerts(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	h := newHealth()
	h.now = func() time.Time { return now }
	if h.ok("airing") {
		t.Fatal("recovery note for a job that never failed")
	}
	if !h.fail("airing") {
		t.Fatal("first failure not alerted")
	}
	if h.fail("airing") {
		t.Fatal("repeat failure alerted inside the window")
	}
	if !h.fail("stale") {
		t.Fatal("other job muted by airing")
	}
	now = now.Add(alertEvery)
	if !h.fail("airing") {
		t.Fatal("failure not alerted after the window")
	}
	if !h.ok("airing") || h.ok("airing") {
		t.Fatal("want exactly one recovery note")
	}
}

func TestRunGuarded(t *testing.T) {
	b := &bot{health: newHealth()}
	if b.runGuarded("job", func() {}) {
		t.Fatal("clean return reported as panic")
	}
	if !b.runGuarded("job", func() { panic("boom") }) {
		t.Fatal("panic not caught")
	}
	if !b.health.failing["job"] {
		t.Fatal("panic not recorded as a failure")
	}
}

func TestCrashWatch(t *testing.T) {
	dir := t.TempDir()
	c := newCrashWatch(dir)
	c.start()
	if c.embed() != nil {
		t.Fatal("first run reported as crash")
	}
	c.stop()

	c = newCrashWatch(dir)
	c.start()
	if c.embed() != nil {
		t.Fatal("clean shutdown reported as crash")
	}
	// Killed: the marker stays.
	c = newCrashWatch(dir)
	c.start()
	e := c.embed()
	if e == nil || strings.Contains(e.Description, "```") {
		t.Fatalf("kill without panic output: %+v", e)
	}
	// Panicked: Go wrote the crash file.
	os.WriteFile(filepath.Join(dir, "otakase-bot.crash"), []byte("panic: boom\n\ngoroutine 1 [running]:\n"), 0o600)
	c.stop()
	c = newCrashWatch(dir)
	c.start()
	if e := c.embed(); e == nil || !strings.Contains(e.Description, "panic: boom") {
		t.Fatalf("panic output missing: %+v", e)
	}
	c.stop()
	c = newCrashWatch(dir)
	c.start()
	if c.embed() != nil {
		t.Fatal("old crash reported twice")
	}
	c.stop()
}

const testSidebar = "**[Home](Home)**\n\n**Using otakase**\n\n- [Casting Problems](Casting-Problems)\n- [Troubleshooting](Troubleshooting)\n" +
	"- [Discord server](https://otakase.xyverion.com/discord/)\n\n**Developer**\n\n- [Overview](Developer)\n"

var testWiki = map[string]string{
	"Casting-Problems": "# Casting Problems\n\n## No devices found / the TV isn't in the list\n\nYour Chromecast or TV must be on the same network.\n\n" +
		"```bash\n# not a heading\notakase -cast-setup\n```\n\n## No subtitles appear\n\nSome TVs ignore **subtitle** tracks.\n\n## No subtitles appear\n\nSecond one.\n",
	"Troubleshooting": "# Troubleshooting\n\nStart here.\n\n## Playback never starts (local, not casting)\n\nThe mpv player must be installed and on your PATH.\n\n" +
		"## The rofi menus look wrong\n\nUse a [Nerd Font](https://www.nerdfonts.com) in rofi.\n",
}

func TestSidebarPages(t *testing.T) {
	got := strings.Join(sidebarPages(testSidebar), ",")
	if got != "Casting-Problems,Troubleshooting" {
		t.Errorf("sidebarPages = %q", got)
	}
}

func TestSplitWiki(t *testing.T) {
	secs := splitWiki("Casting-Problems", testWiki["Casting-Problems"])
	var got []string
	for _, s := range secs {
		got = append(got, s.label()+" "+s.url())
	}
	want := []string{
		"Casting Problems › No devices found / the TV isn't in the list " + wikiURL + "/Casting-Problems#no-devices-found--the-tv-isnt-in-the-list",
		"Casting Problems › No subtitles appear " + wikiURL + "/Casting-Problems#no-subtitles-appear",
		"Casting Problems › No subtitles appear " + wikiURL + "/Casting-Problems#no-subtitles-appear-1",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("sections:\n%s", strings.Join(got, "\n"))
	}
	if !strings.Contains(secs[0].Text, "otakase -cast-setup") || strings.Contains(secs[1].Text, "**") {
		t.Errorf("text: %q / %q", secs[0].Text, secs[1].Text)
	}
	tr := splitWiki("Troubleshooting", testWiki["Troubleshooting"])
	if tr[0].Heading != "" || tr[0].url() != wikiURL+"/Troubleshooting" || tr[0].Text != "Start here." {
		t.Errorf("intro section = %+v", tr[0])
	}
	if tr[2].Text != "Use a Nerd Font in rofi." {
		t.Errorf("links not flattened: %q", tr[2].Text)
	}
}

func TestWikiKeywordMatch(t *testing.T) {
	var w wikiIndex
	if !w.update(testWiki, nil) {
		t.Fatal("update without Ollama should be complete")
	}
	cases := map[string]string{
		"chromecast not found\nmy TV isn't in the devices list":    "no-devices-found--the-tv-isnt-in-the-list",
		"Rofi menu looks wrong\nthe menus look broken in rofi":     "the-rofi-menus-look-wrong",
		"Watch party tonight?\nwhat time does the party start lol": "",
	}
	for q, want := range cases {
		hits := w.match(q, nil)
		got := ""
		if len(hits) > 0 {
			got = hits[0].Anchor
		}
		if got != want {
			t.Errorf("%q: got %q, want %q", q, got, want)
		}
	}
}

func TestWikiSemanticMatch(t *testing.T) {
	srv := fakeOllama(t)
	defer srv.Close()
	sm := &semantic{url: srv.URL, model: "all-minilm"}
	var w wikiIndex
	if !w.update(testWiki, sm) {
		t.Fatal("every section should have vectors")
	}
	hits := w.match("my player won't open at all", sm)
	if len(hits) == 0 || hits[0].Page != "Troubleshooting" || hits[0].Anchor != "playback-never-starts-local-not-casting" {
		t.Errorf("hits = %+v", hits)
	}
	// Vectors are reused on the next refresh.
	n := len(w.cache)
	w.update(testWiki, &semantic{url: "http://127.0.0.1:1", model: "all-minilm"})
	if len(w.cache) != n || len(w.sections[0].vecs) != 2 {
		t.Error("refresh should reuse cached vectors without asking Ollama")
	}
}

func TestFetchWiki(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".md")
		if name == "_Sidebar" {
			w.Write([]byte(testSidebar))
		} else if md, ok := testWiki[name]; ok {
			w.Write([]byte(md))
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	pages, err := fetchWiki(srv.Client(), srv.URL+"/")
	if err != nil || len(pages) != 2 || pages["Troubleshooting"] != testWiki["Troubleshooting"] {
		t.Errorf("fetchWiki = %d pages, %v", len(pages), err)
	}
}

func TestWikiEmbed(t *testing.T) {
	b := &bot{}
	if b.wikiEmbed("hi", "anyone here?") != nil {
		t.Error("no card before the wiki is loaded")
	}
	b.wiki.update(testWiki, nil)
	e := b.wikiEmbed("Rofi menus look wrong", "the rofi menus look broken")
	if e == nil || !strings.Contains(e.Description, "(https://github.com/TheXykril/otakase/wiki/Troubleshooting#the-rofi-menus-look-wrong)") {
		t.Errorf("embed = %+v", e)
	}
}

func TestBoardGoneReposts(t *testing.T) {
	dir := t.TempDir()
	b := &bot{store: openStore(dir + "/s.json")}
	b.store.update(func(d *storeData) { d.Airing = &airingBoard{Message: "m1", Day: time.Unix(0, 0)} })
	b.boardGone("other")
	b.boardGone("m1")
	b.store.view(func(d *storeData) {
		if d.Airing.Message != "" {
			t.Errorf("deleting the board should clear it, got %q", d.Airing.Message)
		}
	})
}

func TestWelcomeDMMessage(t *testing.T) {
	m := welcomeDMMessage(welcomeDMText, welcomeDMSteps)
	b, _ := json.Marshal(m.Components)
	if m.Flags != discordgo.MessageFlagsIsComponentsV2 || len(m.Files) != 1 || !strings.Contains(string(b), "attachment://welcome-header.png") || !strings.Contains(string(b), "/follow") {
		t.Errorf("welcome DM: %s", b)
	}
}

func TestBoardMarkPNG(t *testing.T) {
	names := map[string]bool{}
	for _, art := range boardMarkArts() {
		data, err := art.png()
		if err != nil {
			t.Fatalf("%s: %v", art.name, err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil || len(data) > 256<<10 {
			t.Fatalf("%s: %v (%d bytes)", art.name, err, len(data))
		}
		if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
			t.Errorf("%s: corner should be clear", art.name)
		}
		names[art.name] = true
	}
	for _, want := range []string{"otk_an1", "otk_an5", "otk_un4", "otk_aired"} {
		if !names[want] {
			t.Errorf("missing %s in %v", want, names)
		}
	}
	if names["otk_an6"] || names["otk_un5"] {
		t.Errorf("too many pieces: %v", names)
	}
}
