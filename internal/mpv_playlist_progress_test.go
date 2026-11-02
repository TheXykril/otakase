package internal

import "testing"

// Finishing an episode of a show that has no dub cost the user the episode.
//
// The episode played as sub (AutoAudioFallback), MPV reached the end and landed
// on the first playlist placeholder, and the controller read that as "the user
// picked episode 1". The switch then asked every provider for a dub -- the audio
// the show does not have -- and failed. What made it expensive is that the
// switch had already zeroed the playback state it needed to put back, so the
// finished episode looked unwatched and the tracker was never updated.

// The playlist has to follow the audio that is playing, not the preference. A
// dub-configured session watching a sub-only show would otherwise build a
// playlist whose every entry fails.
func TestPlaylistFollowsTheAudioActuallyPlaying(t *testing.T) {
	config := &Config{SubOrDub: "dub", MpvEpisodePlaylist: true}

	// AutoAudioFallback resolved sub for a show with no dub; the playlist must
	// follow that, not the preference it overrode.
	anime := &Anime{TotalEpisodes: 12}
	anime.Ep.Number = 10
	anime.Ep.Mode = "sub"
	if got := playlistAudioMode(anime, config); got != "sub" {
		t.Errorf("expected the playing audio, got %q", got)
	}

	// Nothing recorded yet: the configured preference stands. Testing the raw
	// value matters here -- normalizeTranslationType("") answers "sub", so a
	// check made after normalising would silently override a dub preference.
	anime.Ep.Mode = ""
	if got := playlistAudioMode(anime, config); got != "dub" {
		t.Errorf("expected the configured preference, got %q", got)
	}
	anime.Ep.Mode = "   "
	if got := playlistAudioMode(anime, config); got != "dub" {
		t.Errorf("blank mode should fall back to the preference, got %q", got)
	}

	// A dub that really is playing is followed too.
	anime.Ep.Mode = "dub"
	if got := playlistAudioMode(anime, &Config{SubOrDub: "sub"}); got != "dub" {
		t.Errorf("expected dub, got %q", got)
	}
}
