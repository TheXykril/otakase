package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaskSecrets(t *testing.T) {
	secrets := []string{"supersecretvalue123"}
	cases := map[string]string{
		"[LOG] 2026/01/02 using supersecretvalue123 for MAL":     "[LOG] 2026/01/02 using **** for MAL",
		"MyAnimeListClientSecret=abcdef123456":                   "MyAnimeListClientSecret=****",
		`{"access_token":"eyJhbGciOi.abc.def","token_type":"x"}`: `{"access_token":"****","token_type":"x"}`,
		"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload":     "Authorization: Bearer ****",
		"KodiPassword: hunter22":                                 "KodiPassword: ****",
		"GET https://x.test/cb?code=1&client_secret=s3cr3tvalue": "GET https://x.test/cb?code=1&client_secret=****",
		"[LOG] 2026/01/02 Failed to start mpv":                   "[LOG] 2026/01/02 Failed to start mpv",
		"Player=mpv":                                             "Player=mpv",
	}
	for in, want := range cases {
		if got := maskSecrets(in, secrets); got != want {
			t.Errorf("maskSecrets(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConfigSecrets(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "otakase.conf")
	os.WriteFile(conf, []byte("Player=mpv\nVimKeys=true\nTraktClientSecret=trakt-secret-value\nKodiPassword=abc\nMyAnimeListClientID=public-id\n"), 0o600)
	got := configSecrets(conf)
	if len(got) != 1 || got[0] != "trakt-secret-value" {
		t.Fatalf("configSecrets = %q", got)
	}
}

func TestDiagnosticsBlock(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "otakase.conf")
	os.WriteFile(conf, []byte("TraktClientSecret=trakt-secret-value\n"), 0o600)
	logPath := filepath.Join(dir, LogFileName)
	var log strings.Builder
	for i := 0; i < 40; i++ {
		log.WriteString("[LOG] 2026/01/02 line\n")
	}
	log.WriteString("[LOG] 2026/01/02 sent trakt-secret-value\n")
	os.WriteFile(logPath, []byte(log.String()), 0o600)

	out := Diagnostics("26.5.0", conf, logPath)
	if !strings.HasPrefix(out, "```\notakase diagnostics\n") || !strings.HasSuffix(out, "```\n") {
		t.Fatalf("not a code block with the header:\n%s", out)
	}
	if !strings.Contains(out, DisplayName+" version: 26.5.0") || !strings.Contains(out, "--- last 30 log lines ---") {
		t.Fatalf("missing version or log header:\n%s", out)
	}
	if strings.Contains(out, "trakt-secret-value") || !strings.Contains(out, "sent ****") {
		t.Fatalf("secret not masked:\n%s", out)
	}
}

func TestWantsDiagnostics(t *testing.T) {
	if !WantsDiagnostics([]string{"-diag"}) || !WantsDiagnostics([]string{"-rofi", "--diag"}) {
		t.Fatal("flag not seen")
	}
	if WantsDiagnostics([]string{"-v"}) || WantsDiagnostics([]string{"--", "-diag"}) {
		t.Fatal("false positive")
	}
}
