package internal

import (
	"errors"
	"testing"
)

// Enter has to exist as a command at all. The panel's other keys are playback
// controls -- pause, seek, volume, stop -- and a viewer being asked to rate
// something needs a key that means yes and is not one of those.
func TestEnterDecodesAsSelect(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []byte
		want castCommand
	}{
		{"carriage return, which is what Enter sends in raw mode", []byte{'\r'}, castCmdSelect},
		{"newline", []byte{'\n'}, castCmdSelect},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, used := decodeCastKey(tc.in)
			if got != tc.want || used != 1 {
				t.Errorf("decodeCastKey(%q) = %v, %d; want %v, 1", tc.in, got, used, tc.want)
			}
		})
	}
}

// The bug this whole change exists beside. RateAnime returned nil when the score
// prompt was cancelled, so every caller took the success branch and announced a
// rating that was never written -- "Anime rated successfully!", and a summary
// reading "rating saved". Escaping the prompt in a terminal did it too, and a
// cast window did it every time.
//
// Driven through the real function rather than a simulation of it, because the
// failure was precisely that nil and a declined rating looked like a success.
func TestRateAnimeReportsADeclineRatherThanSuccess(t *testing.T) {
	// Every configuration, not just dual. ErrRatingDeclined was reachable only
	// for dual-tracking viewers: the single-tracker arm delegated the asking to
	// RateAnimeWithScore, which swallowed the cancel and returned nil, so
	// escaping the score prompt on an AniList-only setup -- the commonest one --
	// still printed "Anime rated successfully!" with nothing written. Testing
	// only the dual case is how that survived being "fixed".
	for _, tracking := range []string{"anilist", "myanimelist", "anilist+myanimelist"} {
		t.Run(tracking, func(t *testing.T) {
			previous := GetGlobalConfig()
			previousAnime := GetGlobalAnime()
			SetGlobalConfig(&Config{TrackingRemote: tracking, CastNonInteractive: true})
			SetGlobalAnime(&Anime{AnilistId: 1})
			t.Cleanup(func() {
				SetGlobalConfig(previous)
				SetGlobalAnime(previousAnime)
			})

			err := RateAnime("token", 1)

			if !errors.Is(err, ErrRatingDeclined) {
				t.Fatalf("RateAnime error = %v, want ErrRatingDeclined: a nil here is what "+
					"made a declined rating report itself as saved", err)
			}
		})
	}
}

// Where a score goes, for each configuration.
//
// The defect this pins: the AniList and MyAnimeList arms of RateAnimeWithScore
// used to call RateAniListAnime and rateMyAnimeListAnime, which take no score
// and prompt for one. So the score a cast viewer picked with the arrows was
// discarded, the prompt was auto-cancelled by CastNonInteractive, nil came back,
// and the panel reported a rating that was never written.
//
// Asserted on the route rather than on the write because this package has no
// HTTP seam to intercept the write with: a test that called through would need
// the network. The route is the part that was wrong.
func TestRatingRouteFollowsTheConfiguredTracker(t *testing.T) {
	anime := &Anime{AnilistId: 1}
	for _, c := range []struct {
		tracking string
		want     ratingRoute
	}{
		{"anilist", ratingRouteAniList},
		{"myanimelist", ratingRouteMyAnimeList},
		{"anilist+myanimelist", ratingRouteDual},
		{"none", ratingRouteNone},
	} {
		if got := ratingRouteFor(&Config{TrackingRemote: c.tracking}, anime); got != c.want {
			t.Errorf("route for %q = %d, want %d", c.tracking, got, c.want)
		}
	}
}
