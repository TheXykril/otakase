package internal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newsServer(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/news" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("news sent a token; it needs none")
		}
		hits.Add(1)
		w.Write([]byte(`{"announcement":{"title":"26.8.0 is out","text":"Watch parties","url":"https://discord.com/channels/1/2/3","at":"2026-10-03T12:00:00Z"},"online":12,"members":85,"invite":"https://otakase.xyverion.com/discord/"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscordNewsIsCachedAndRefreshedEverySixHours(t *testing.T) {
	var hits atomic.Int32
	srv := newsServer(t, &hits)
	config := newDiscordTestConfig(t, srv.URL)

	if _, ok := cachedDiscordNews(config); ok {
		t.Fatal("news shown before any fetch")
	}
	refreshDiscordNewsIfStale(config)
	refreshDiscordNewsIfStale(config)
	if hits.Load() != 1 {
		t.Fatalf("fetched %d times, want once within the refresh window", hits.Load())
	}
	news, ok := cachedDiscordNews(config)
	if !ok {
		t.Fatal("no news after a fetch")
	}
	if news.headline() != "26.8.0 is out · "+time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC).Local().Format("2 Jan 2006") {
		t.Errorf("headline = %q", news.headline())
	}
	if news.countsLine() != "12 online · 85 members" {
		t.Errorf("counts = %q", news.countsLine())
	}

	// An old copy is fetched again.
	news.FetchedAt = time.Now().Add(-7 * time.Hour)
	saveDiscordNews(config, news)
	refreshDiscordNewsIfStale(config)
	if hits.Load() != 2 {
		t.Fatalf("stale copy not refreshed: %d fetches", hits.Load())
	}
}

func TestDiscordNewsHiddenWhenOffline(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	config := newDiscordTestConfig(t, url)
	refreshDiscordNewsIfStale(config)
	if _, ok := cachedDiscordNews(config); ok {
		t.Fatal("news shown with the bot unreachable and nothing cached")
	}
	// A copy from long ago is not shown either.
	saveDiscordNews(config, discordNews{Online: 3, FetchedAt: time.Now().Add(-48 * time.Hour)})
	if _, ok := cachedDiscordNews(config); ok {
		t.Fatal("two-day-old news shown")
	}
	config.DiscordBotURL = ""
	saveDiscordNews(config, discordNews{Online: 3, FetchedAt: time.Now()})
	if _, ok := cachedDiscordNews(config); ok {
		t.Fatal("news shown with the features off")
	}
}

func TestCommunityOptions(t *testing.T) {
	config := newDiscordTestConfig(t, DefaultDiscordBotURL)
	news := discordNews{Announcement: &discordAnnouncement{Title: "Hello", URL: "https://discord.com/channels/1"}, Online: 4, Members: 9}

	keys := func(options []SelectionOption) string {
		var out []string
		for _, option := range options {
			out = append(out, option.Key)
		}
		return strings.Join(out, ",")
	}

	options := communityOptions(config, news, true)
	if got := keys(options); got != "news,join,link,report" {
		t.Errorf("not linked: %s", got)
	}
	if !strings.Contains(options[1].Label, "4 online · 9 members") {
		t.Errorf("join row = %q", options[1].Label)
	}

	saveDiscordLink(config, discordLink{Token: "t", DiscordName: "Xykril"})
	options = communityOptions(config, news, false)
	if got := keys(options); got != "join,unlink,share,party,backup:save,backup:restore,parties,wwm,report" {
		t.Errorf("linked, no news: %s", got)
	}
	if !strings.Contains(options[1].Label, "Xykril") || !strings.Contains(options[2].Label, "Off") {
		t.Errorf("linked rows = %q / %q", options[1].Label, options[2].Label)
	}
	config.DiscordShareWatching = true
	if label := communityOptions(config, news, false)[2].Label; !strings.Contains(label, "On") {
		t.Errorf("share row with sharing on = %q", label)
	}

	forgetDiscordLink(config, true)
	if label := communityOptions(config, news, false)[1].Label; !strings.Contains(label, "removed") {
		t.Errorf("revoked link row = %q", label)
	}
}

func TestUpdatePromptOffersDiscordWithNews(t *testing.T) {
	previous := GetGlobalConfig()
	t.Cleanup(func() { SetGlobalConfig(previous) })
	config := newDiscordTestConfig(t, DefaultDiscordBotURL)
	SetGlobalConfig(config)

	state := pendingUpdate{LatestVersion: "26.9.0"}
	for _, option := range updateActionOptions(state) {
		if option.Key == "discord" {
			t.Fatal("Discord offered without news")
		}
	}
	if discordNewsLine() != "" {
		t.Fatal("news line without news")
	}

	saveDiscordNews(config, discordNews{Announcement: &discordAnnouncement{Title: "Hello"}, Online: 2, FetchedAt: time.Now()})
	found := false
	for _, option := range updateActionOptions(state) {
		if option.Key == "discord" {
			found = true
			if !strings.Contains(option.Label, "2 online") {
				t.Errorf("label = %q", option.Label)
			}
		}
	}
	if !found {
		t.Fatal("no Join Discord row with news cached")
	}
	if line := discordNewsLine(); !strings.HasPrefix(line, "Discord: Hello") {
		t.Errorf("news line = %q", line)
	}
	opened := stubDiscordOpen(t)
	if err := openDiscordInvite(); err != nil || len(*opened) != 1 || (*opened)[0] != DiscordInviteURL {
		t.Errorf("opened %v, err %v", *opened, err)
	}
}

func TestDiscordNewsDecodesProtocol(t *testing.T) {
	var news discordNews
	if err := json.Unmarshal([]byte(`{"announcement":null,"online":0,"members":5,"invite":""}`), &news); err != nil {
		t.Fatal(err)
	}
	if news.headline() != "" || news.countsLine() != "5 members" || news.invite() != DiscordInviteURL {
		t.Errorf("news = %+v", news)
	}
}
