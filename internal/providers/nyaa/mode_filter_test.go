package nyaa

import "testing"

// A season of a sub-only show: no release carries an English audio track.
func subOnlySeason() []Release {
	return []Release{
		parseSeeded("[SubsPlease] Saijo no Osewa - 10 (1080p) [A87EB168].mkv", 390),
		parseSeeded("[SubsPlease] Saijo no Osewa - 09 (1080p) [E1F3DA9A].mkv", 210),
		parseSeeded("[Erai-raws] Saijo no Osewa - 10 [1080p CR WEB-DL AVC AAC][MultiSub]", 220),
	}
}

func parseSeeded(title string, seeders int) Release {
	r := parseRelease(title)
	r.Seeders = seeders
	r.InfoHash = "deadbeef"
	return r
}

// Filtering by audio during search made a sub-only show disappear entirely from
// a dub-configured setup: every title variant answered "no results", so otakase
// could not map the show at all, and the automatic audio fallback -- which only
// runs once a provider is mapped -- never got the chance to switch to sub.
//
// Finding a show and choosing its audio are separate questions.
func TestSearchIsNotFilteredByAudioMode(t *testing.T) {
	releases := subOnlySeason()

	if got := playableReleases(releases); len(got) != len(releases) {
		t.Fatalf("search should keep every seeded episode regardless of audio, got %d of %d",
			len(got), len(releases))
	}

	// The same set, asked for as dub, is correctly empty at episode level.
	if got := usableReleases(releases, "dub"); len(got) != 0 {
		t.Fatalf("expected no dub-capable releases, got %d", len(got))
	}
	if got := usableReleases(releases, "sub"); len(got) != len(releases) {
		t.Fatalf("expected every release to satisfy sub, got %d", len(got))
	}
}

// Dual-audio releases must still satisfy a dub request.
func TestDualAudioSatisfiesDub(t *testing.T) {
	releases := []Release{
		parseSeeded("[Judas] Some Show - 03 (1080p) [Dual-Audio]", 50),
		parseSeeded("[Group] Some Show - 04 (1080p)", 50),
	}
	dub := usableReleases(releases, "dub")
	if len(dub) != 1 || dub[0].Episode != 3 {
		t.Fatalf("expected only the dual-audio release for dub, got %+v", dub)
	}
}

// Releases nobody seeds cannot be played, so they must not reach either path.
func TestUnseededReleasesAreDroppedEverywhere(t *testing.T) {
	releases := []Release{parseSeeded("[Group] Some Show - 01 (1080p)", 0)}
	if got := playableReleases(releases); len(got) != 0 {
		t.Fatalf("expected unseeded release to be dropped from search, got %+v", got)
	}
	if got := usableReleases(releases, "sub"); len(got) != 0 {
		t.Fatalf("expected unseeded release to be dropped from playback, got %+v", got)
	}
}
