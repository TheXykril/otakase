package nyaa

import "testing"

func TestReleaseSeason(t *testing.T) {
	cases := map[string]int{
		"[SubsPlease] Dandadan - 24 (1080p) [AD3DEA4E].mkv":                                  0,
		"[DKB] Dandadan - S02E12 [1080p][END][HEVC x265 10bit][Multi-Subs][weekly]":          2,
		"[Raze] Dandadan S2 - 11 (Dual-Audio) x265 10bit 1080p 143.8561fps.mkv":              2,
		"[Ironclad] Sousou no Frieren 2nd Season - S02E01 [WEB.1080p.AV1]":                   2,
		"[EMBER] Dandadan S02E10 [1080p] [Dual Audio HEVC WEBRip DDP] (Dan Da Dan Season 2)": 2,
		"[Anime Time] Mushoku Tensei - Jobless Reincarnation II - 05 [1080p]":                2,
		"[MTBB] Mushoku Tensei S3 - 14 (WEB 1080p) | Jobless Reincarnation":                  3,
		"[LostYears] SPY x FAMILY - S01E02 (WEB 1080p HEVC AAC E-AC-3) [Dual-Audio]":         1,
		"[Group] Hii no Sekai - 03 (1080p)":                                                  0,
	}
	for title, want := range cases {
		if got := releaseSeason(title); got != want {
			t.Errorf("releaseSeason(%q) = %d, want %d", title, got, want)
		}
	}
}

func TestSplitSeason(t *testing.T) {
	cases := []struct {
		name   string
		base   string
		season int
	}{
		{"Dandadan", "Dandadan", 1},
		{"Dandadan S2", "Dandadan", 2},
		{"Sousou no Frieren 2nd Season", "Sousou no Frieren", 2},
		{"Oshi no Ko 3rd Season", "Oshi no Ko", 3},
		{"SPY x FAMILY Season 3", "SPY x FAMILY", 3},
	}
	for _, tc := range cases {
		if base, season := splitSeason(tc.name); base != tc.base || season != tc.season {
			t.Errorf("splitSeason(%q) = %q, %d, want %q, %d", tc.name, base, season, tc.base, tc.season)
		}
	}
}

// A weekly release that names its season is one episode, not a season pack.
func TestSeasonNamedEpisodeIsNotABatch(t *testing.T) {
	r := parseRelease("[EMBER] Dandadan S02E10 [1080p] [Dual Audio HEVC WEBRip DDP] (Dan Da Dan Season 2)")
	if r.Episode != 10 {
		t.Fatalf("episode = %d, want 10", r.Episode)
	}
}

// Tags a group leaves outside brackets after the episode number are not part
// of the show's name.
func TestSeriesKeyDropsTagsAfterEpisode(t *testing.T) {
	if got := seriesKey("[Raze] Dandadan S2 - 11 (Dual-Audio) x265 10bit 1080p 143.8561fps.mkv"); got != "Dandadan S2" {
		t.Fatalf("seriesKey = %q", got)
	}
}

// Asking for the first season must never play the second's episode of the
// same number, and the other way round.
func TestShowReleasesKeepsOneSeason(t *testing.T) {
	releases := []Release{
		parseSeeded("[SubsPlease] Dandadan - 05 (1080p) [00000000].mkv", 100),
		parseSeeded("[DKB] Dandadan - S02E05 [1080p][HEVC x265 10bit][Multi-Subs][weekly]", 50),
		parseSeeded("[Raze] Dandadan S2 - 05 (Dual-Audio) x265 10bit 1080p.mkv", 5),
	}
	first := showReleases("Dandadan", releases)
	if len(first) != 1 || releaseSeason(first[0].Title) != 0 {
		t.Fatalf("first season kept %+v", first)
	}
	second := showReleases("Dandadan S2", releases)
	if len(second) != 2 {
		t.Fatalf("second season kept %d releases, want 2", len(second))
	}
	for _, release := range second {
		if releaseSeason(release.Title) != 2 {
			t.Errorf("second season kept %q", release.Title)
		}
	}
}

// A search for a show also returns its spin-offs; their episode 3 is not the
// show's. Its other spellings are the show, though.
func TestShowReleasesDropsSpinOffsKeepsSpellings(t *testing.T) {
	releases := []Release{
		parseSeeded("[SubsPlease] Kaijuu 8-gou - Narumi no Heijitsu - 03 (1080p) [44506493].mkv", 140),
		parseSeeded("Kaiju No 8 S01E03 Revenge Match 1080p CR WEB-DL AAC2.0 H 264-VARYG (Kaijuu 8-gou, Dual-Audio, Multi-Subs)", 4),
		parseSeeded("[SubsPlease] Kaijuu 8-gou - 03 (1080p) [5A757FA8].mkv", 90),
		parseSeeded("[SubsPlease] Sousou no Frieren - Marumaru no Mahou - 01 (1080p).mkv", 30),
		parseSeeded("[Group] Juuou Mujin Dandivine - 03 (1080p).mkv", 30),
	}
	kept := showReleases("Kaiju No 8", releases)
	if len(kept) != 2 {
		t.Fatalf("kept %d releases, want the two of the show: %+v", len(kept), kept)
	}
	for _, release := range kept {
		if containsName(release.Title, "Narumi no Heijitsu") {
			t.Errorf("kept the spin-off %q", release.Title)
		}
	}
	if kept := showReleases("Sousou no Frieren", releases); len(kept) != 0 {
		t.Errorf("Frieren kept %+v", kept)
	}
}

// sukebei files every language under one category; a release with neither
// English audio nor English subtitles is of no use.
func TestNotEnglish(t *testing.T) {
	cases := map[string]bool{
		"[ZigZag] Overflow S01E02 [1080p AO WEB-DL] [Doblaje Latino] [Sub. Español]":                         true,
		"[JapanCore] Overflow - 01 (2020) [ESP - LAT] [DUB] [BD HEVC 1080p] [BADC3269]":                      true,
		"[AormeSubs] Overflow - 08 [720p][Spanish Sub/Sub Español]":                                          true,
		"[Group] Show - 03 (RAW 1080p)":                                                                      true,
		"[HentaiHub] Overflow S01E02 1080p UNCENSORED OV WEB-DL DUAL FLAC2 0 H.264 (Dual-Audio, Eng-Sub)":    false,
		"[Erai-raws] One Piece - 1100 [1080p][HEVC][Multiple Subtitle] [ENG][POR-BR][SPA-LA][SPA][ARA][FRE]": false,
		"[SubsPlease] Dandadan - 03 (1080p) [569BAA9C].mkv":                                                  false,
		"[Erai-raws] Saijo no Osewa - 10 [1080p CR WEB-DL AVC AAC][MultiSub][78ABD684]":                      false,
	}
	for title, want := range cases {
		if got := notEnglish(parseRelease(title)); got != want {
			t.Errorf("notEnglish(%q) = %v, want %v", title, got, want)
		}
	}
}

func TestShahidDualAudioIsNotAnEnglishDub(t *testing.T) {
	r := parseSeeded("[ToonsHub] Dandadan S02E03 1080p SHAHID WEB-DL AAC2.0 H.264 (Dual-Audio, Multi-Subs)", 100)
	if matchesMode(r, "dub") {
		t.Fatal("an Arabic dual-audio rip was taken for an English dub")
	}
	if !matchesMode(r, "sub") {
		t.Fatal("it still has the Japanese audio")
	}
}

func TestSearchName(t *testing.T) {
	cases := map[string]string{
		"Frieren- Beyond Journey's End": "Frieren Beyond Journey's End",
		"Kaijuu 8-gou":                  "Kaijuu 8-gou",
		"Show - Subtitle":               "Show Subtitle",
	}
	for in, want := range cases {
		if got := searchName(in); got != want {
			t.Errorf("searchName(%q) = %q, want %q", in, got, want)
		}
	}
}

// A search grouped by season gives each season its own row, keyed so the
// episode lookup knows which season it is.
func TestSearchGroupsBySeason(t *testing.T) {
	p := New()
	seedFeed(t, p.feed, "Dandadan", []Release{
		parseSeeded("[SubsPlease] Dandadan - 05 (1080p) [00000000].mkv", 100),
		parseSeeded("[DKB] Dandadan - S02E05 [1080p][HEVC x265 10bit][Multi-Subs][weekly]", 50),
		parseSeeded("[Raze] Dandadan S2 - 06 (Dual-Audio) x265 10bit 1080p.mkv", 5),
	})
	options, err := p.SearchAnime("Dandadan", "sub")
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, option := range options {
		keys[option.Key] = true
	}
	if len(options) != 2 || !keys["Dandadan"] || !keys["Dandadan S2"] {
		t.Fatalf("options = %+v", options)
	}
}
