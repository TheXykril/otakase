package internal

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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

func TestBuildPlaybackArgsEvensOutVolumeUnlessTheUserSetFilters(t *testing.T) {
	link := "https://cdn.example.test/1.m3u8"
	on := buildPlaybackArgs(&Config{NormalizeVolume: true}, &Anime{}, link, nil)
	if !slices.Contains(on.args, "--af-append="+volumeLevellerFilter) {
		t.Fatalf("expected the volume leveller in %q", on.args)
	}
	off := buildPlaybackArgs(&Config{}, &Anime{}, link, nil)
	if slices.ContainsFunc(off.args, func(a string) bool { return strings.HasPrefix(a, "--af") }) {
		t.Fatalf("leveller added while off: %q", off.args)
	}
	for _, own := range []string{"--af=lavfi=[loudnorm]", "--af-add=scaletempo2"} {
		p := buildPlaybackArgs(&Config{NormalizeVolume: true, MpvArgs: []string{own}}, &Anime{}, link, nil)
		if slices.Contains(p.args, "--af-append="+volumeLevellerFilter) {
			t.Fatalf("leveller added on top of the user's %s: %q", own, p.args)
		}
	}
}

func TestVolumeLevellerMatchesTheApp(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "app", "lib", "data", "store.dart"))
	if err != nil {
		t.Skipf("app source not found: %v", err)
	}
	if !strings.Contains(string(src), "'"+volumeLevellerFilter+"'") {
		t.Fatalf("app volumeLeveller differs from %q", volumeLevellerFilter)
	}
}

func TestVolumeLevellerIsAValidFilterGraph(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	graph := strings.TrimSuffix(strings.TrimPrefix(volumeLevellerFilter, "lavfi=["), "]")
	for _, layout := range []string{"mono", "stereo", "5.1", "7.1"} {
		out, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error",
			"-f", "lavfi", "-i", "anoisesrc=d=1:a=0.1,aformat=channel_layouts="+layout,
			"-af", graph, "-f", "null", "-").CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", layout, err, out)
		}
	}
}
