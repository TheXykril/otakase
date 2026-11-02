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
