package internal

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProviderReportDue(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	st := &providerReportState{}
	steps := []struct {
		ok    bool
		after time.Duration
		want  bool
	}{
		{true, 0, true},                 // first word on a source
		{true, 10 * time.Minute, false}, // works again: not before an hour
		{false, 0, true},                // fails: sent at once
		{false, time.Minute, false},     // fails again: once is enough
		{true, time.Minute, true},       // works after failing: sent
		{false, time.Minute, true},      // fails again after working
		{true, time.Minute, true},
		{false, time.Minute, true}, // the third failure this run
		{true, time.Minute, true},
		{false, time.Minute, false}, // no fourth
		{true, 30 * time.Minute, false},
		{true, 31 * time.Minute, true}, // an hour since the last success
		{true, 59 * time.Minute, false},
		{true, 2 * time.Minute, true},
		{false, 24 * time.Hour, false}, // still capped for the run
	}
	for i, s := range steps {
		now = now.Add(s.after)
		if got := providerReportDue(st, s.ok, now); got != s.want {
			t.Fatalf("step %d (ok=%t): due=%t, want %t", i, s.ok, got, s.want)
		}
	}
}

func TestCleanProviderError(t *testing.T) {
	err := errors.New(`no results for "Frieren" from https://example.org/api?q=frieren: Post 'x': EOF`)
	got := cleanProviderError(err)
	if strings.Contains(got, "Frieren") || strings.Contains(got, "example.org") {
		t.Fatalf("leaks the show or the host: %q", got)
	}
	if got != "no results for … from <url> Post …: EOF" {
		t.Fatalf("cleanProviderError = %q", got)
	}
	if len([]rune(cleanProviderError(errors.New(strings.Repeat("x", 500))))) != providerErrorMax {
		t.Fatal("not clipped")
	}
}

func TestNoteProviderResultOnlyWhenLinked(t *testing.T) {
	config := newDiscordTestConfig(t, "http://bot.invalid")
	previousConfig := GetGlobalConfig()
	SetGlobalConfig(config)
	sent := make(chan providerReportBody, 10)
	previousSend := sendProviderReport
	sendProviderReport = func(_ *Config, token string, body providerReportBody) {
		if token != "tok" {
			t.Errorf("token %q", token)
		}
		sent <- body
	}
	providerReports.m = map[string]*providerReportState{}
	t.Cleanup(func() {
		SetGlobalConfig(previousConfig)
		sendProviderReport = previousSend
		providerReports.m = map[string]*providerReportState{}
	})

	noteProviderResult("anizone", "search", errors.New("connection refused"))
	select {
	case body := <-sent:
		t.Fatalf("sent while not linked: %+v", body)
	case <-time.After(50 * time.Millisecond):
	}

	if err := saveDiscordLink(config, discordLink{Token: "tok"}); err != nil {
		t.Fatal(err)
	}
	noteProviderResult("AniZone", "search", errors.New(`Get "https://x.example/s": connection refused`))
	noteProviderResult("anizone", "episode", errors.New("connection reset"))  // a second failure: not again
	noteProviderResult("anikoto", "search", errors.New(`no results for "x"`)) // answered: it works
	noteProviderResult("kickassanime", "search", &errProviderCooling{provider: "kickassanime"})
	var got []providerReportBody
	for len(got) < 2 {
		select {
		case body := <-sent:
			got = append(got, body)
		case <-time.After(time.Second):
			t.Fatalf("only %d reports: %+v", len(got), got)
		}
	}
	select {
	case body := <-sent:
		t.Fatalf("extra report: %+v", body)
	case <-time.After(50 * time.Millisecond):
	}
	byName := map[string]providerReportBody{}
	for _, b := range got {
		byName[b.Provider] = b
	}
	if b := byName["anizone"]; b.OK || b.Kind != "search" || b.Error != "Get …: connection refused" {
		t.Fatalf("anizone: %+v", b)
	}
	if b := byName["anikoto"]; !b.OK || b.Error != "" {
		t.Fatalf("anikoto: %+v", b)
	}
}

func TestBackupText(t *testing.T) {
	raw := "AnimeNameLanguage=english\r\nKodiPassword=hunter2\nTraktClientSecret=s\nMyAnimeListClientID=id\nAnimeSkipClientID=a\nStoragePath=/home/me/.local/share/otakase\n# note\n\nSkipOp = true\nnot a line\n"
	if got := backupText(raw); got != "AnimeNameLanguage=english\nSkipOp = true" {
		t.Fatalf("backupText = %q", got)
	}
	current := map[string]string{"SkipOp": "false", "KodiPassword": "mine", "StoragePath": "/here", "Theme": "auto"}
	merged, n := mergeBackup(current, "SkipOp=true\nKodiPassword=theirs\nStoragePath=/there\nQuality=720")
	if n != 2 || merged["SkipOp"] != "true" || merged["Quality"] != "720" || merged["KodiPassword"] != "mine" || merged["StoragePath"] != "/here" || merged["Theme"] != "auto" {
		t.Fatalf("merge: %d %v", n, merged)
	}
	if current["SkipOp"] != "false" {
		t.Fatal("merge changed the current map")
	}
}

func TestSettingsBackupRoundTrip(t *testing.T) {
	var stored string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/backup" || r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, `{"error":"no"}`, http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodPost:
			var body struct{ Config string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			stored = body.Config
			w.Write([]byte(`{"at":"2026-10-14T10:00:00Z"}`))
		case http.MethodGet:
			if stored == "" {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"error":"no settings backup yet"}`))
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"config": stored, "at": "2026-10-14T10:00:00Z", "from_os": "linux"})
		}
	}))
	defer srv.Close()
	config := newDiscordTestConfig(t, srv.URL)
	if err := saveDiscordLink(config, discordLink{Token: "tok"}); err != nil {
		t.Fatal(err)
	}
	previousPath := ConfigFilePath
	ConfigFilePath = filepath.Join(t.TempDir(), "otakase.conf")
	t.Cleanup(func() { ConfigFilePath = previousPath })

	if _, err := fetchSettingsBackup(config); !errors.Is(err, errNoSettingsBackup) {
		t.Fatalf("before a backup: %v", err)
	}
	os.WriteFile(ConfigFilePath, []byte("SkipOp=false\nKodiPassword=secret\nQuality=720\n"), 0o600)
	if n, err := saveSettingsBackup(config); err != nil || n != 2 {
		t.Fatalf("save: %d %v", n, err)
	}
	if strings.Contains(stored, "secret") {
		t.Fatalf("a password left the computer: %q", stored)
	}

	// The other computer.
	os.WriteFile(ConfigFilePath, []byte("SkipOp=true\nKodiPassword=other\n"), 0o600)
	bk, err := fetchSettingsBackup(config)
	if err != nil || bk.FromOS != "linux" || bk.At.IsZero() {
		t.Fatalf("fetch: %+v %v", bk, err)
	}
	n, keep, err := restoreSettingsBackup(bk)
	if err != nil || n != 2 {
		t.Fatalf("restore: %d %v", n, err)
	}
	got, _ := LoadConfigFromFile(ConfigFilePath)
	if got["SkipOp"] != "false" || got["Quality"] != "720" || got["KodiPassword"] != "other" {
		t.Fatalf("restored: %v", got)
	}
	if old, _ := os.ReadFile(keep); string(old) != "SkipOp=true\nKodiPassword=other\n" {
		t.Fatalf("copy of the old settings: %q", old)
	}
}

func TestCollectCrashes(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	marker := func(pid int, started time.Time) {
		data, _ := json.Marshal(runMarker{PID: pid, Started: started, Version: "26.8.0"})
		os.WriteFile(markerPath(dir, pid), data, 0o600)
	}
	marker(100, now.Add(-time.Hour))       // still running
	marker(200, now.Add(-3*time.Hour))     // gone, crashed with a stack
	marker(300, now.Add(-2*time.Hour))     // gone later, killed: no stack, newest
	marker(400, now.Add(-30*24*time.Hour)) // its id was reused long after
	marker(500, now)                       // this run
	os.WriteFile(crashOutputPath(dir, 200), []byte("panic: boom\n\ngoroutine 1"), 0o600)
	previous := crashProcessAlive
	crashProcessAlive = func(pid int) bool { return pid == 100 || pid == 400 || pid == 500 }
	t.Cleanup(func() { crashProcessAlive = previous })

	collectCrashes(dir, 500, now)
	for pid, want := range map[int]bool{100: true, 200: false, 300: false, 400: false, 500: true} {
		if _, err := os.Stat(markerPath(dir, pid)); (err == nil) != want {
			t.Errorf("marker %d kept=%t, want %t", pid, err == nil, want)
		}
	}
	if _, err := os.Stat(crashOutputPath(dir, 200)); err == nil {
		t.Error("crash output not cleared")
	}
	c, ok := takePendingCrash(dir, now)
	if !ok || !c.At.Equal(now.Add(-2*time.Hour)) || c.Stack != "" || c.Version != "26.8.0" {
		t.Fatalf("pending: %+v %t", c, ok)
	}
	if _, ok := takePendingCrash(dir, now); ok {
		t.Fatal("a crash is asked about once")
	}

	marker(200, now.Add(-3*time.Hour))
	os.WriteFile(crashOutputPath(dir, 200), []byte("panic: boom\n\ngoroutine 1"), 0o600)
	collectCrashes(dir, 500, now)
	if c, ok := takePendingCrash(dir, now.Add(31*24*time.Hour)); ok {
		t.Fatalf("an old crash is not asked about: %+v", c)
	}
}

func TestCrashReportText(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	title, desc := crashReportText(pendingCrash{At: at, Version: "26.8.0", Stack: "panic: runtime error: index out of range\n\ngoroutine 1 [running]:\nmain.main()"}, "picking an episode")
	if title != "Otakase closed unexpectedly: panic: runtime error: index out of range" {
		t.Fatalf("title %q", title)
	}
	for _, want := range []string{"26.8.0", "What I was doing: picking an episode", "```\npanic: runtime error", "main.main()"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description lacks %q:\n%s", want, desc)
		}
	}
	title, desc = crashReportText(pendingCrash{At: at}, "")
	if title != "Otakase closed unexpectedly" || !strings.Contains(desc, "No crash message was saved") || strings.Contains(desc, "What I was doing") {
		t.Fatalf("no stack: %q %q", title, desc)
	}
}

func TestCrashWatchEndsCleanly(t *testing.T) {
	config := &Config{StoragePath: t.TempDir()}
	StartCrashWatch(config)
	dir := crashDir(config)
	if _, err := os.Stat(markerPath(dir, os.Getpid())); err != nil {
		t.Fatalf("no marker: %v", err)
	}
	EndCrashWatch()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("left behind: %v", entries)
	}
}

func TestAskServerOption(t *testing.T) {
	askedServer.m = map[string]bool{}
	t.Cleanup(func() { askedServer.m = map[string]bool{} })
	options := []SelectionOption{{Key: "custom"}, {Key: "back"}}
	if got := withAskServerOption(&Config{}, options, "Frieren"); len(got) != 2 {
		t.Fatal("offered with Discord off")
	}
	config := &Config{DiscordBotURL: "http://bot.invalid"}
	got := withAskServerOption(config, options, "Frieren")
	if len(got) != 3 || got[1].Key != askServerKey || got[2].Key != "back" || options[1].Key != "back" {
		t.Fatalf("options: %+v", got)
	}
	askedServer.m["frieren"] = true
	if got := withAskServerOption(config, options, " FRIEREN "); len(got) != 2 {
		t.Fatal("offered twice for one show")
	}
	title, desc := askServerText("Frieren", 154587, "Tried: anikoto, kickassanime")
	if title != "Where to watch Frieren?" || !strings.Contains(desc, "https://anilist.co/anime/154587") || !strings.Contains(desc, "Tried: anikoto, kickassanime") {
		t.Fatalf("text: %q %q", title, desc)
	}
}

func TestRatingShare(t *testing.T) {
	config := newDiscordTestConfig(t, "http://bot.invalid")
	anime := &Anime{AnilistId: 154587}
	if ratingShareWanted(config, anime, 8) {
		t.Fatal("offered without a link")
	}
	var got ratingBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rating" || r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"url":"https://discord.com/channels/1/2/3"}`))
	}))
	defer srv.Close()
	config.DiscordBotURL = srv.URL
	saveDiscordLink(config, discordLink{Token: "tok"})
	if !ratingShareWanted(config, anime, 8) {
		t.Fatal("not offered when linked")
	}
	if ratingShareWanted(config, &Anime{AnilistId: 1, IsAdult: true}, 8) {
		t.Fatal("offered for an adult title")
	}
	off := false
	if err := saveDiscordNews(config, discordNews{ScoreSharing: &off, FetchedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if ratingShareWanted(config, anime, 8) {
		t.Fatal("offered with score sharing off on the server")
	}
	on := true
	saveDiscordNews(config, discordNews{ScoreSharing: &on, FetchedAt: time.Now()})
	if !ratingShareWanted(config, anime, 8) {
		t.Fatal("not offered with score sharing on")
	}
	config.DiscordShareRatings = "never"
	if ratingShareWanted(config, anime, 8) {
		t.Fatal("offered with DiscordShareRatings=never")
	}
	url, err := shareRating(config, ratingBody{AnilistID: 154587, Score: 8.5, Text: "great"})
	if err != nil || url != "https://discord.com/channels/1/2/3" || got.AnilistID != 154587 || got.Score != 8.5 || got.Text != "great" {
		t.Fatalf("share: %q %v %+v", url, err, got)
	}
}
