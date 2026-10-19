package internal

import (
	"reflect"
	"slices"
	"testing"
)

func TestNormalizeReferrerArgsFillsInSchemeAndPath(t *testing.T) {
	got := normalizeReferrerArgs([]string{
		"--referrer=example.test",
		"--mpv-referrer", "https://cdn.example.test",
		"--title=x=y",
		"--referrer",
	})
	want := []string{
		"--referrer=https://example.test/",
		"--mpv-referrer", "https://cdn.example.test/",
		"--title=x=y",
		"--referrer",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHasMPVSubtitleArg(t *testing.T) {
	for _, args := range [][]string{
		{"--sub-file=a.srt"},
		{"--SUB-FILE", "a.srt"},
		{"--sub-files", "a.srt:b.srt"},
		{"--sub-files=a.srt"},
	} {
		if !hasMPVSubtitleArg(args) {
			t.Errorf("%q loads subtitles", args)
		}
	}
	for _, args := range [][]string{{"--sub-files"}, {"--sub-auto=fuzzy"}, nil} {
		if hasMPVSubtitleArg(args) {
			t.Errorf("%q loads no subtitles", args)
		}
	}
}

func TestTranslateMPVArgsForIINA(t *testing.T) {
	got := translateMPVArgsForIINA([]string{"--fs", "--mpv-volume=50", "https://cdn.example.test/a.m3u8"})
	want := []string{"--mpv-fs", "--mpv-volume=50", "https://cdn.example.test/a.m3u8"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if !isIINAPlayer("mpv", "/Applications/IINA.app/Contents/MacOS/IINA") || isIINAPlayer("mpv", "/usr/bin/mpv") {
		t.Fatal("IINA is told apart by name or binary")
	}
}

func TestBuildPlaybackArgsLeavesTheUsersReferrerAndSubtitles(t *testing.T) {
	anime := &Anime{}
	anime.Ep.StreamReferrer = "provider.example.test"
	anime.Ep.SubtitleURL = "https://subs.example.test/1.vtt"

	p := buildPlaybackArgs(&Config{}, anime, "https://cdn.example.test/1.m3u8", nil)
	if p.referrer != "provider.example.test" || p.subtitleURL == "" || p.ownSubtitles {
		t.Fatalf("expected the provider's referrer and subtitles, got %+v", p)
	}
	if !slices.Contains(p.args, "--referrer=https://provider.example.test/") || !slices.Contains(p.args, "--sub-file=https://subs.example.test/1.vtt") {
		t.Fatalf("missing provider arguments in %q", p.args)
	}

	own := buildPlaybackArgs(&Config{MpvArgs: []string{"--referrer=mine.example.test", "--sub-file=mine.srt"}}, anime, "https://cdn.example.test/1.m3u8", nil)
	if own.referrer != "" || own.subtitleURL != "" || !own.ownSubtitles {
		t.Fatalf("expected the user's own referrer and subtitles to win, got %+v", own)
	}
}
