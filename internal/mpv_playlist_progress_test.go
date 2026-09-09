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

// A switch that fails must leave the episode exactly as it found it, or a
// watched episode is silently downgraded to an abandoned one.
func TestFailedPlaylistSwitchRestoresWatchProgress(t *testing.T) {
	anime := &Anime{TotalEpisodes: 12}
	anime.Ep.Number = 10
	anime.Ep.Player.PlaybackTime = 1400
	anime.Ep.Duration = 1440
	anime.Ep.Resume = true
	anime.Ep.Links = []string{"https://cdn.test/ep10.m3u8"}
	anime.Ep.StreamReferrer = "https://site.test/"
	anime.Ep.SubtitleURL = "https://cdn.test/en.vtt"
	anime.Ep.StreamHeaders = map[string]string{"Origin": "https://player.test"}

	before := anime.Ep

	// A provider stack that carries nothing: every switch attempt fails.
	withAllProvidersEnabledForTest(t)
	config := CurdConfig{Provider: `["anipub"]`, SubOrDub: "dub", PercentageToMarkComplete: 85}
	c := &MPVPlaylistController{
		config:         &config,
		anime:          anime,
		socket:         "",
		preferredMode:  "dub",
		currentMode:    "dub",
		currentPlaying: 10,
	}

	err := c.playSlot(playlistSlot{Episode: 1, Mode: "dub"})
	if err == nil {
		t.Fatal("expected the switch to fail with no provider carrying the show")
	}

	if anime.Ep.Number != before.Number {
		t.Errorf("episode number: got %d, want %d", anime.Ep.Number, before.Number)
	}
	if anime.Ep.Player.PlaybackTime != before.Player.PlaybackTime {
		t.Errorf("playback time was lost: got %d, want %d",
			anime.Ep.Player.PlaybackTime, before.Player.PlaybackTime)
	}
	if anime.Ep.Duration != before.Duration {
		t.Errorf("duration was lost: got %d, want %d — the completion check divides by this",
			anime.Ep.Duration, before.Duration)
	}
	if anime.Ep.Resume != before.Resume {
		t.Errorf("resume flag: got %v, want %v", anime.Ep.Resume, before.Resume)
	}
	if len(anime.Ep.Links) != len(before.Links) {
		t.Errorf("links were lost: got %v", anime.Ep.Links)
	}
	if anime.Ep.StreamReferrer != before.StreamReferrer {
		t.Errorf("referrer was lost: got %q", anime.Ep.StreamReferrer)
	}
	if anime.Ep.StreamHeaders["Origin"] != before.StreamHeaders["Origin"] {
		t.Errorf("stream headers were lost: got %v", anime.Ep.StreamHeaders)
	}

	// The whole point: after a failed switch the episode still reads as watched.
	if pct := PercentageWatched(anime.Ep.Player.PlaybackTime, anime.Ep.Duration); int(pct) < 85 {
		t.Fatalf("episode no longer counts as watched (%.0f%%), so progress would not be recorded", pct)
	}
}

// Once the episode is watched, playlist movement is end-of-file, not a choice.
func TestFinishedEpisodeIsNotHijackedByPlaylistMovement(t *testing.T) {
	anime := &Anime{TotalEpisodes: 12}
	anime.Ep.Number = 10
	anime.Ep.Player.PlaybackTime = 1400
	anime.Ep.Duration = 1440 // ~97%

	config := CurdConfig{PercentageToMarkComplete: 85}
	c := &MPVPlaylistController{config: &config, anime: anime, currentPlaying: 10}

	if !c.episodeFinished() {
		t.Fatal("97% watched should count as finished")
	}

	// Partway through, the user really may be picking another episode.
	anime.Ep.Player.PlaybackTime = 300
	if c.episodeFinished() {
		t.Error("20% watched must not count as finished")
	}

	// Unknown duration cannot be judged, so it must not suppress a real pick.
	anime.Ep.Player.PlaybackTime = 1400
	anime.Ep.Duration = 0
	if c.episodeFinished() {
		t.Error("an unknown duration must not read as finished")
	}
}

// The playlist has to follow the audio that is playing, not the preference. A
// dub-configured session watching a sub-only show would otherwise build a
// playlist whose every entry fails.
func TestPlaylistFollowsTheAudioActuallyPlaying(t *testing.T) {
	config := &CurdConfig{SubOrDub: "dub", MpvEpisodePlaylist: true}

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
	if got := playlistAudioMode(anime, &CurdConfig{SubOrDub: "sub"}); got != "dub" {
		t.Errorf("expected dub, got %q", got)
	}
}

// Refusing to chase MPV off the end of the episode is only half the job. The
// real stream is one row in a playlist of placeholders, so when it finishes MPV
// moves to the next row -- a black clip that runs for a day. Declining to switch
// left MPV sitting on that blackness with "Loading episode 1…" on screen, and
// because playback never ended the episode was never marked watched.
//
// The distinguishing signal is the path: empty means MPV is idle between files,
// which is what running off the end looks like. A row the user actually picked
// has media loaded and reports a path.
func TestFinishedEpisodeEndsTheSessionOnlyWhenMPVRanOffTheEnd(t *testing.T) {
	finished := func() *MPVPlaylistController {
		anime := &Anime{TotalEpisodes: 12}
		anime.Ep.Number = 10
		anime.Ep.Player.PlaybackTime = 1400
		anime.Ep.Duration = 1440 // ~97%
		return &MPVPlaylistController{
			config:         &CurdConfig{PercentageToMarkComplete: 85},
			anime:          anime,
			socket:         "", // no MPV: property reads answer empty, as when idle
			currentPlaying: 10,
		}
	}

	// Finished, and MPV reports no path: this is end-of-file, so end the session
	// and let the main loop record the episode.
	if !finished().endSessionIfEpisodeOver(1) {
		t.Error("expected a finished episode with no media loaded to end the session")
	}

	// Partway through, the same movement is a genuine selection.
	partway := finished()
	partway.anime.Ep.Player.PlaybackTime = 300 // ~20%
	if partway.endSessionIfEpisodeOver(1) {
		t.Error("an unfinished episode must never have its session ended")
	}

	// Duration unknown: nothing can be concluded, so do not end anything.
	unknown := finished()
	unknown.anime.Ep.Duration = 0
	if unknown.endSessionIfEpisodeOver(1) {
		t.Error("an unknown duration must not end the session")
	}
}
