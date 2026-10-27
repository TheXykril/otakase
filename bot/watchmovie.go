package main

import (
	"context"
	"fmt"
	"strings"
)

// "Otakase: The Movie" in the watch room. The app hides the film behind a
// search for "otakase" when adding a show; the room's picker does the same.
// It is not on AniList and has no episodes on the providers: the CLI draws it
// (`otakase -movie-serve`), about the host when their AniList is linked with
// access, and serves it the way the torrent helper serves an episode.

// watchMovieID stands in for an AniList id, which are all positive.
const watchMovieID = -1

// watchMovieShow is the film as the picker lists it.
func watchMovieShow() watchShow {
	return watchShow{ID: watchMovieID, Title: "Otakase: The Movie", Episodes: 1, Year: 2026, Format: "MOVIE"}
}

// isWatchMovieQuery reports whether a picker search is the one that finds
// the film.
func isWatchMovieQuery(q string) bool {
	return strings.EqualFold(strings.TrimSpace(q), "otakase")
}

// isMovie reports whether the show is the film.
func (s watchShow) isMovie() bool {
	return s.ID == watchMovieID
}

// tracked reports whether the show is on AniList, so watching it can move
// lists on and save positions.
func (s watchShow) tracked() bool {
	return s.ID > 0
}

// watchStartMovie starts the helper that draws the film, a variable for tests.
var watchStartMovie = func(ctx context.Context, cli, presenter, token string, progress func(string)) (*watchTorrent, string, error) {
	var env []string
	if token != "" {
		env = append(env, "OTAKASE_MOVIE_ANILIST_TOKEN="+token)
	}
	return startWatchHelper(ctx, cli, []string{"-movie-serve", presenter}, env, progress)
}

// loadWatchMovie draws the film for the room and starts it, as loadEpisode
// does an episode. It runs on its own; cancel stops the helper.
func (b *bot) loadWatchMovie(ctx context.Context, cancel context.CancelFunc, r *watchRoom, gen int, cli, host, hostName string, step func(string)) {
	var token string
	b.store.view(func(d *storeData) { token = d.AniList[host].Token })
	step("Developing the film")
	helper, link, err := watchStartMovie(ctx, cli, hostName, token, step)

	b.watch.mu.Lock()
	defer b.watch.mu.Unlock()
	if !b.watch.open(r) || r.gen != gen {
		cancel()
		if helper != nil {
			go helper.stop()
		}
		return
	}
	if err != nil {
		cancel()
		code := recordWatchFailure(watchMovieShow().Title, 1, r.loadStep, err)
		r.phase, r.errText = "error", fmt.Sprintf("The film could not be developed. Report code %s to the moderators.", code)
		r.changed()
		return
	}
	r.streams = []streamLink{{URL: link}}
	r.dub = false
	r.torrentCancel = nil
	if !b.startStream(r, 0) {
		cancel()
		go helper.stop()
		return
	}
	r.torrent = helper
	r.torrentCancel = cancel
}
