package nyaa

import "testing"

// Release titles are free-form, and the two conventions in wide use put the
// episode number in different places. Getting this wrong is not a near miss: a
// resolution read as an episode number sends the user to episode 1080.
func TestParseReleaseEpisodeNumber(t *testing.T) {
	cases := []struct {
		title   string
		episode int
		quality int
		group   string
	}{
		{"[SubsPlease] Saijo no Osewa - 10 (1080p) [A87EB168].mkv", 10, 1080, "SubsPlease"},
		{"[Erai-raws] Saijo no Osewa - 10 [1080p CR WEB-DL AVC AAC][MultiSub][78ABD684]", 10, 1080, "Erai-raws"},
		{"[SubsPlease] Saijo no Osewa - 9 (720p) [E1F3DA9A].mkv", 9, 720, "SubsPlease"},
		{"[BlackRose] Rich Girl Caretaker - S01E10 (WEB 1080p HEVC 10-bit EAC-3)", 10, 1080, "BlackRose"},
		{"[DKB] Saijo no Osewa - S01E10 [1080p][HEVC x265 10bit][Multi-Subs][weekly]", 10, 1080, "DKB"},
		{"[ASW] Saijo no Osewa - 10 [1080p HEVC x265 10Bit][AAC]", 10, 1080, "ASW"},
		{"Rich Girl Caretaker S01E09 1080p BILI WEB-DL AAC2.0 H.264-VARYG", 9, 1080, ""},
		{"[Cattleya] Rich Girl Caretaker - S01E10 - The Plan to Free Narika (CR WEB-DL 1080p AAC x264) [67784878]", 10, 1080, "Cattleya"},
		{"[SubsPlease] Saijo no Osewa - 10 (480p) [F702A4A9].mkv", 10, 480, "SubsPlease"},
		// A version suffix must not swallow the number.
		{"[Group] Some Show - 07v2 (1080p).mkv", 7, 1080, "Group"},
	}
	for _, tc := range cases {
		got := parseRelease(tc.title)
		if got.Episode != tc.episode {
			t.Errorf("episode: got %d want %d for %q", got.Episode, tc.episode, tc.title)
		}
		if got.Quality != tc.quality {
			t.Errorf("quality: got %d want %d for %q", got.Quality, tc.quality, tc.title)
		}
		if got.Group != tc.group {
			t.Errorf("group: got %q want %q for %q", got.Group, tc.group, tc.title)
		}
	}
}

// A batch has no single episode, and treating one as episode 1 would make Curd
// download a whole season to play one episode.
func TestBatchesAreNotGivenAnEpisodeNumber(t *testing.T) {
	batches := []string{
		"[Erai-raws] Saijo no Osewa - 01 ~ 12 [1080p][Multiple Subtitle][Batch]",
		"[Judas] Some Show (Season 1) [Batch] [1080p]",
		"[Group] Another Show Complete Series [1080p]",
		"[Group] Show - 01-12 [1080p]",
	}
	for _, title := range batches {
		if got := parseRelease(title); got.Episode != 0 {
			t.Errorf("expected no episode for batch %q, got %d", title, got.Episode)
		}
	}
}

// The resolution and the CRC32 checksum both contain digits next to a dash or
// bracket; neither is an episode number.
func TestQualityAndChecksumAreNotEpisodes(t *testing.T) {
	for _, title := range []string{
		"[Group] Show Name - 1080p WEB-DL.mkv",
		"[Group] Show Name [A87EB168].mkv",
	} {
		if got := parseRelease(title); got.Episode > 100 {
			t.Errorf("%q parsed an implausible episode %d", title, got.Episode)
		}
	}
}

// Releases of one show from different groups must collapse to one entry, or the
// picker shows the same series a dozen times.
func TestSeriesKeyGroupsReleasesOfOneShow(t *testing.T) {
	same := []string{
		"[SubsPlease] Saijo no Osewa - 10 (1080p) [A87EB168].mkv",
		"[SubsPlease] Saijo no Osewa - 09 (720p) [E1F3DA9A].mkv",
		"[ASW] Saijo no Osewa - 10 [1080p HEVC x265 10Bit][AAC]",
		"[Erai-raws] Saijo no Osewa - 10 [1080p CR WEB-DL AVC AAC][MultiSub][78ABD684]",
	}
	first := seriesKey(same[0])
	if first == "" {
		t.Fatal("series key should not be empty")
	}
	for _, title := range same[1:] {
		if got := seriesKey(title); got != first {
			t.Errorf("expected %q to group as %q, got %q", title, first, got)
		}
	}
	if first != "Saijo no Osewa" {
		t.Errorf("unexpected series key %q", first)
	}
}
