package anidb

import (
	"errors"
	"strings"
	"testing"
)

// anidb.app returns its maintenance placeholder with HTTP 200 for every path,
// including the JSON API routes, so it has to be detected by body rather than
// status or it surfaces as an unintelligible parse error.
func TestIsMaintenancePage(t *testing.T) {
	page := `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">
	<title>Under Maintenance</title></head><body><h1>Under Maintenance</h1></body></html>`
	if !isMaintenancePage(page) {
		t.Fatal("expected the maintenance placeholder to be detected")
	}
	if isMaintenancePage(`{"episodes":[{"id":1,"number":1}]}`) {
		t.Fatal("a JSON payload is not a maintenance page")
	}
}

func TestIsCloudflareChallenge(t *testing.T) {
	if !isCloudflareChallenge("<title>Just a moment...</title>") {
		t.Fatal("expected the Cloudflare interstitial to be detected")
	}
	if isCloudflareChallenge("<html>real content</html>") {
		t.Fatal("real content is not a challenge")
	}
}

func TestShowIDFromSlug(t *testing.T) {
	cases := map[string]string{
		"one-piece-21":          "21",
		"rich-girl-caretaker-8": "8",
		"12345":                 "12345",
	}
	for slug, want := range cases {
		if got := showIDFromSlug(slug); got != want {
			t.Fatalf("showIDFromSlug(%q) = %q, want %q", slug, got, want)
		}
	}
}

func TestSearchAnchorParsing(t *testing.T) {
	body := `
	<a href="/anime/one-piece-21" class="card"><img alt="One Piece" src="/img/op.webp"></a>
	<a href="/anime/one-piece-film-red-999" class="card"><img alt="One Piece Film: Red" src="/img/red.webp"></a>
	<a href="/browse?q=x">next page</a>
	`
	chunks := strings.Split(body, "<a href")
	var slugs []string
	for _, chunk := range chunks {
		match := searchAnchorRE.FindStringSubmatch("<a href" + chunk)
		if len(match) < 3 {
			continue
		}
		slugs = append(slugs, match[1]+"|"+match[2])
	}

	want := []string{"one-piece-21|One Piece", "one-piece-film-red-999|One Piece Film: Red"}
	if len(slugs) != len(want) {
		t.Fatalf("got %v, want %v", slugs, want)
	}
	for i := range want {
		if slugs[i] != want[i] {
			t.Fatalf("got %v, want %v", slugs, want)
		}
	}
}

// The episodes endpoint has been seen returning both a bare array and a wrapped
// object, so decoding accepts either.
func TestDecodeEpisodesAcceptsBothShapes(t *testing.T) {
	for name, raw := range map[string]string{
		"bare array": `[{"id":101,"number":1},{"id":102,"number":2}]`,
		"wrapped":    `{"episodes":[{"id":101,"number":1},{"id":102,"number":2}]}`,
		"data key":   `{"data":[{"id":101,"number":1},{"id":102,"number":2}]}`,
	} {
		episodes, err := decodeEpisodes([]byte(raw))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(episodes) != 2 || episodeNumber(episodes[1]) != 2 {
			t.Fatalf("%s: got %v", name, episodes)
		}
	}

	if _, err := decodeEpisodes([]byte("  ")); err == nil {
		t.Fatal("expected an error for an empty body")
	}
}

func TestLanguageTagAndEmbedSelection(t *testing.T) {
	if languageTag("dub") != "eng" {
		t.Fatal("dub maps to eng")
	}
	if languageTag("sub") != "jpn" || languageTag("") != "jpn" {
		t.Fatal("sub maps to jpn and is the default")
	}

	languages := []language{
		{Language: "eng", Name: "English Dub", EmbedURL: "https://example.test/dub"},
		{Language: "jpn", Name: "Japanese", EmbedURL: "https://example.test/sub"},
	}
	if got := embedURLForMode(languages, "sub"); got != "https://example.test/sub" {
		t.Fatalf("sub embed = %q", got)
	}
	if got := embedURLForMode(languages, "dub"); got != "https://example.test/dub" {
		t.Fatalf("dub embed = %q", got)
	}
	if got := embedURLForMode(nil, "sub"); got != "" {
		t.Fatalf("expected no embed, got %q", got)
	}
}

func TestEmbedFileExtraction(t *testing.T) {
	page := `<script>var player = new Plyr({ file: 'https://cdn.example.test/master.m3u8', type: 'hls' });</script>`
	match := embedFileRE.FindStringSubmatch(page)
	if len(match) < 2 || match[1] != "https://cdn.example.test/master.m3u8" {
		t.Fatalf("got %v", match)
	}
}

func TestResolveRelative(t *testing.T) {
	master := "https://cdn.example.test/a/b/master.m3u8"
	if got := resolveRelative(master, "index-f1.m3u8"); got != "https://cdn.example.test/a/b/index-f1.m3u8" {
		t.Fatalf("got %q", got)
	}
	if got := resolveRelative(master, "https://other.test/x.m3u8"); got != "https://other.test/x.m3u8" {
		t.Fatalf("absolute URLs pass through, got %q", got)
	}
}

func TestMaintenanceErrorIsDistinct(t *testing.T) {
	if !errors.Is(errMaintenance, errMaintenance) {
		t.Fatal("sentinel must compare equal to itself")
	}
	if !strings.Contains(errMaintenance.Error(), "maintenance") {
		t.Fatalf("unhelpful maintenance error: %v", errMaintenance)
	}
}
