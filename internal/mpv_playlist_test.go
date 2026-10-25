package internal

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func playlistTestController(anime *Anime, mode string) *PlaylistController {
	return &PlaylistController{
		anime:     anime,
		baseAudio: mode,
		nowAudio:  mode,
		nowEp:     anime.Ep.Number,
	}
}

func TestPlaylistRowLabels(t *testing.T) {
	pc := playlistTestController(&Anime{
		Title:          AnimeTitle{English: "Show"},
		FillerEpisodes: []int{3},
		Ep:             Episode{Number: 2},
	}, "sub")

	for _, tc := range []struct {
		ep   int
		mode string
		want string
	}{
		{2, "sub", "Show - Episode 2  ◀"},
		{3, "sub", "Show - Episode 3 [Filler]"},
		{2, "dub", "Show - Episode 2 (DUB)"},
		{4, "sub", "Show - Episode 4"},
	} {
		if got := pc.rowTitle(tc.ep, tc.mode); got != tc.want {
			t.Errorf("episode %d %s: got %q, want %q", tc.ep, tc.mode, got, tc.want)
		}
	}

	// Rows in the audio playing carry no audio tag, dub included.
	pc.nowAudio = "dub"
	if got := pc.rowTitle(1, "dub"); strings.Contains(got, "DUB") {
		t.Errorf("a dub row in a dub session is tagged: %q", got)
	}
}

func TestPlaylistRowTitlesReadBack(t *testing.T) {
	pc := playlistTestController(&Anime{Title: AnimeTitle{English: "Show"}, Ep: Episode{Number: 5}}, "sub")
	for _, tc := range []struct {
		ep   int
		mode string
	}{{5, "sub"}, {5, "dub"}, {12, "sub"}} {
		ep, mode, ok := parsePlaylistEpisodeTitle(pc.rowTitle(tc.ep, tc.mode), "sub")
		if !ok || ep != tc.ep || mode != tc.mode {
			t.Errorf("label for %d %s read back as %d %s (ok=%v)", tc.ep, tc.mode, ep, mode, ok)
		}
	}
	if _, _, ok := parsePlaylistEpisodeTitle("av://lavfi:color", "sub"); ok {
		t.Error("a placeholder path is not a row title")
	}
}

func TestEpisodeIndexPrefersTheAudioAsked(t *testing.T) {
	pc := &PlaylistController{rows: []playlistRow{
		{Ep: 1, Audio: "sub"}, {Ep: 2, Audio: "sub"}, {Ep: 2, Audio: "dub"}, {Ep: 3, Audio: "sub"},
	}}
	for _, tc := range []struct {
		ep   int
		mode string
		want int
	}{{2, "dub", 2}, {2, "sub", 1}, {3, "dub", 3}, {9, "sub", 8}} {
		if got := pc.rowIndexLocked(tc.ep, tc.mode); got != tc.want {
			t.Errorf("episode %d %s: row %d, want %d", tc.ep, tc.mode, got, tc.want)
		}
	}
}

func TestParseEpisodeNumberList(t *testing.T) {
	got := episodeNumbersFrom([]string{"3", "1", "2", "2", " 12.0 ", "12.5", "bad", "0", "0.5", "-1"})
	if want := []int{1, 2, 3, 12}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := episodeNumbersFrom([]string{"none"}); got != nil {
		t.Fatalf("expected nil for no numbers, got %v", got)
	}
}

func TestPlaceholderM3UTitlesEveryRow(t *testing.T) {
	pc := &PlaylistController{config: &Config{StoragePath: t.TempDir()}}
	m3u, werr := pc.writeRowsM3U([]playlistRow{
		{Ep: 1, Title: "Show - Episode 1"},
		{Ep: 2, Title: "Show - Episode 2\n[Filler]"},
	})
	if werr != nil {
		t.Fatalf("write: %v", werr)
	}
	t.Cleanup(func() { _ = os.Remove(m3u) })

	body, rerr := os.ReadFile(m3u)
	if rerr != nil {
		t.Fatalf("read back: %v", rerr)
	}
	want := strings.Join([]string{
		"#EXTM3U",
		"#EXTINF:-1,Show - Episode 1", placeholderFor(1),
		"#EXTINF:-1,Show - Episode 2 [Filler]", placeholderFor(2),
	}, "\n") + "\n"
	if string(body) != want {
		t.Fatalf("m3u body:\n%s\nwant:\n%s", body, want)
	}
}

func TestM3UTitle(t *testing.T) {
	for in, want := range map[string]string{
		" \r\n ":   "Episode",
		"":         "Episode",
		"a\r\nb":   "a  b",
		"  Ep 3  ": "Ep 3",
	} {
		if got := m3uTitle(in); got != want {
			t.Errorf("m3uTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlaceholderPathsAreUniqueAndReadBack(t *testing.T) {
	seen := map[string]bool{}
	for _, ep := range []int{1, 39, 40, 199, 200, 366, 1000} {
		url := placeholderFor(ep)
		if seen[url] {
			t.Fatalf("episode %d shares a path: %s", ep, url)
		}
		seen[url] = true
		if strings.Contains(url, "#") || !strings.Contains(url, "d=86400") || !isPlaceholderPath(url) {
			t.Fatalf("episode %d: unusable placeholder %q", ep, url)
		}
		if got, ok := placeholderEpisode(url); !ok || got != ep {
			t.Fatalf("episode %d read back as %d (ok=%v)", ep, got, ok)
		}
	}
	if _, ok := placeholderEpisode("https://cdn.example.test/1.m3u8"); ok {
		t.Fatal("a stream is not a placeholder")
	}
}

func TestPlaylistSwitchFlagHoldsOffThePlaybackMonitor(t *testing.T) {
	t.Cleanup(markPlaylistSwitchDone)

	markPlaylistSwitchStart()
	switching := PlaylistSwitchInProgress()
	action := ClassifyPlaybackLoss("", true, 11, 85)
	markPlaylistSwitchDone()
	if !switching || action != PlaybackLossWait {
		t.Fatalf("mid-switch: switching=%v action=%d, want true and wait", switching, action)
	}
	if PlaylistSwitchInProgress() {
		t.Fatal("flag survived markPlaylistSwitchDone")
	}

	cases := []struct {
		started bool
		watched float64
		want    PlaybackLossAction
	}{
		{started: true, watched: 11, want: PlaybackLossExit},
		{started: true, watched: 90, want: PlaybackLossComplete},
		{started: false, watched: 0, want: PlaybackLossWait},
	}
	for _, c := range cases {
		if got := ClassifyPlaybackLoss("", c.started, c.watched, 85); got != c.want {
			t.Errorf("started=%v watched=%v: got %d, want %d", c.started, c.watched, got, c.want)
		}
	}
}

func TestMarkLeftEpisodeOnlyWhenMovingOnFromAWatchedOne(t *testing.T) {
	for _, tc := range []struct {
		from, to int
		watched  float64
		want     bool
	}{
		{40, 41, 90, true},
		{40, 41, 11, false},
		{40, 39, 90, false}, // going back never lowers progress
		{40, 1, 0, false},
		{40, 40, 90, false}, // the other audio of the same episode
		{40, 45, 90, false}, // skipping ahead is left to the end of the session
	} {
		if got := markLeftEpisodeOnSwitch(tc.from, tc.to, tc.watched, 85); got != tc.want {
			t.Errorf("%d → %d at %.0f%%: got %v, want %v", tc.from, tc.to, tc.watched, got, tc.want)
		}
	}
}

func TestMediaURLKeyIgnoresTokens(t *testing.T) {
	if mediaURLKey("https://cdn.example.test/x/a.m3u8?token=1") != mediaURLKey("https://cdn.example.test/x/a.m3u8?token=2#t") {
		t.Fatal("the same file with another token should match")
	}
	if mediaURLKey("https://cdn.example.test/a/p.m3u8") == mediaURLKey("https://cdn.example.test/b/p.m3u8") {
		t.Fatal("different files should differ")
	}
}

func TestEscapeMPVOptionValue(t *testing.T) {
	if got, want := escapeMPVOptionValue(`Show "A", Ep=1 \ x`), `"Show \"A\"\, Ep\=1 \\ x"`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestFinalizePlaylistSwitchRecordsTheNewEpisode(t *testing.T) {
	store := t.TempDir()
	show := &Anime{
		AnilistId:      269,
		ProviderId:     "269",
		ProviderName:   "anizone",
		Title:          AnimeTitle{English: "Show"},
		FillerEpisodes: []int{33, 41},
		Ep:             Episode{Number: 40},
	}
	show.Ep.Player.PlaybackTime = 900
	pc := &PlaylistController{
		config:    &Config{StoragePath: store, SubOrDub: "sub"},
		anime:     show,
		baseAudio: "sub",
	}

	pc.finalizePlaylistEpisodeChange(40, 39, "sub", false)
	pc.WaitForPrefetch()
	if ep := show.Ep; ep.Number != 39 || ep.IsFiller || ep.Player.PlaybackTime != 0 {
		t.Fatalf("after 40 -> 39: %+v", ep)
	}
	last, err := os.ReadFile(LastAnimeFile(store))
	if err != nil || strings.TrimSpace(string(last)) != "269" {
		t.Fatalf("last-anime file = %q, err %v", last, err)
	}
	saved := LocalFindAnime(LocalGetAllAnime(HistoryFile(store)), 269, "269")
	if saved == nil || saved.Ep.Number != 39 || saved.Ep.Player.PlaybackTime != 0 {
		t.Fatalf("history row: %+v", saved)
	}

	pc.finalizePlaylistEpisodeChange(39, 41, "sub", false)
	pc.WaitForPrefetch()
	if !show.Ep.IsFiller {
		t.Fatal("41 should be flagged filler")
	}
}

func TestHistoryFileLocation(t *testing.T) {
	if p := localHistoryPath(" "); p != "" {
		t.Fatalf("blank storage path gave %q", p)
	}
	if p := localHistoryPath("/tmp/otakase-test-store"); !strings.HasSuffix(p, historyFileName) {
		t.Fatalf("history path %q does not end in %s", p, historyFileName)
	}
}
