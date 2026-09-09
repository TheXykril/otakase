package kickassanime

import "testing"

// Captured from a real player page. The props attribute is HTML-escaped in the
// markup and each value is Astro-tagged, so both layers have to come off before
// anything is readable.
const playerPageFixture = `<!DOCTYPE html><html><body>
<astro-island uid="Z1Xyuk6" component-url="/_astro/VidstackPlayer.js" props="{&quot;manifest&quot;:[0,&quot;//bl.krussdomi.com/playlist/6a9cc983/master.m3u8&quot;],&quot;subtitles&quot;:[1,[[0,{&quot;format&quot;:[0,&quot;srt&quot;],&quot;language&quot;:[0,&quot;en&quot;],&quot;name&quot;:[0,&quot;English&quot;],&quot;src&quot;:[0,&quot;https:///subbl.krussdomi.com/6a9cc983/en.srt&quot;]}],[0,{&quot;format&quot;:[0,&quot;srt&quot;],&quot;language&quot;:[0,&quot;th&quot;],&quot;name&quot;:[0,&quot;Thai&quot;],&quot;src&quot;:[0,&quot;https:///subbl.krussdomi.com/6a9cc983/th.srt&quot;]}]]]}"></astro-island>
</body></html>`

func TestParsePlayerSources(t *testing.T) {
	got, err := parsePlayerSources([]byte(playerPageFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The manifest is protocol-relative in the page and unusable as-is.
	want := "https://bl.krussdomi.com/playlist/6a9cc983/master.m3u8"
	if got.Manifest != want {
		t.Errorf("manifest: got %q want %q", got.Manifest, want)
	}
	if len(got.Subtitles) != 2 {
		t.Fatalf("expected 2 subtitle tracks, got %d", len(got.Subtitles))
	}

	// The host emits subtitle links with an empty authority; left alone they
	// resolve nowhere.
	wantSub := "https://subbl.krussdomi.com/6a9cc983/en.srt"
	if url := englishSubtitle(got.Subtitles); url != wantSub {
		t.Errorf("english subtitle: got %q want %q", url, wantSub)
	}
}

func TestParsePlayerSourcesRejectsPageWithoutProps(t *testing.T) {
	if _, err := parsePlayerSources([]byte(`<html><body>no island here</body></html>`)); err == nil {
		t.Fatal("expected an error when the page carries no player")
	}
}

// A page whose props parse but carry no manifest is a failure, not an episode
// with an empty URL -- MPV would be handed nothing and spin.
func TestParsePlayerSourcesRequiresAManifest(t *testing.T) {
	page := `<astro-island props="{&quot;subtitles&quot;:[1,[]]}"></astro-island>`
	if _, err := parsePlayerSources([]byte(page)); err == nil {
		t.Fatal("expected an error when no manifest is present")
	}
}

func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"//host/path.m3u8":       "https://host/path.m3u8",
		"https:///host/sub.srt":  "https://host/sub.srt",
		"http:///host/sub.srt":   "http://host/sub.srt",
		"https://host/fine.m3u8": "https://host/fine.m3u8",
		"":                       "",
	}
	for in, want := range cases {
		if got := normalizeURL(in); got != want {
			t.Errorf("normalizeURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// Only the English track should be chosen, whichever position it holds.
func TestEnglishSubtitleSelection(t *testing.T) {
	tracks := []SubtitleTrack{
		{Language: "th", URL: "th.srt"},
		{Language: "en", URL: "en.srt"},
	}
	if got := englishSubtitle(tracks); got != "en.srt" {
		t.Errorf("got %q", got)
	}
	if got := englishSubtitle([]SubtitleTrack{{Language: "th", URL: "th.srt"}}); got != "" {
		t.Errorf("expected no track, got %q", got)
	}
}
