package internal

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/thexykril/otakase/internal/movies"
	"github.com/thexykril/otakase/internal/providers"
)

// moviePlay is one film, or one episode of a series, being played.
type moviePlay struct {
	movie movies.Movie
	// episode is the series' episode; zero for a film.
	episode movies.Episode
	// last is the series' last episode: finishing it finishes the series.
	last bool
	// start is where to start, in seconds.
	start int
}

// title is how the play reads in the player and in messages.
func (p moviePlay) title() string {
	if p.episode.Number > 0 {
		return p.movie.Title + " · " + p.episode.Label()
	}
	return p.movie.Label()
}

// save records where playback stopped.
func (p moviePlay) save(store *movies.Store, position, duration int, watched bool) {
	if store == nil {
		return
	}
	var err error
	if p.episode.Number > 0 {
		err = store.SetEpisodeProgress(p.movie, p.episode, position, duration, watched, p.last)
	} else {
		err = store.SetProgress(p.movie, position, duration, watched)
	}
	if err != nil {
		Log(fmt.Sprintf("movies: could not save progress: %v", err))
	}
}

// playMovie plays a film from start seconds, in mpv or on the cast device
// when casting is on, trying its servers in turn until one plays, and records
// where it stopped.
func playMovie(config *Config, store *movies.Store, lib *movieLibrary, movie movies.Movie, start int) {
	movie, sources, err := lib.open(config, movie)
	if err != nil {
		Out(fmt.Sprintf("Could not open %s: %v", movie.Label(), err))
		awaitEnterNotice()
		return
	}
	play := moviePlay{movie: movie, start: start}
	if !playSources(config, store, lib, play, sources) {
		Out(fmt.Sprintf("None of the servers for %s could be played. The movie may have been taken down; try again later.", play.title()))
		awaitEnterNotice()
	}
}

// playEpisodes plays a series from episodes[index], start seconds in, and
// goes on to the next episode each time one is watched to the end, as anime
// do: straight away, or after asking when NextEpisodePrompt is on.
func playEpisodes(config *Config, store *movies.Store, lib *movieLibrary, show movies.Movie, episodes []movies.Episode, index, start int) {
	for index < len(episodes) {
		episode := episodes[index]
		play := moviePlay{movie: show, episode: episode, last: index == len(episodes)-1, start: start}
		sources, err := lib.openEpisode(show, episode)
		if err != nil {
			Out(fmt.Sprintf("Could not open %s: %v", play.title(), err))
			awaitEnterNotice()
			return
		}
		if !playSources(config, store, lib, play, sources) {
			Out(fmt.Sprintf("None of the servers for %s could be played; try again later.", play.title()))
			awaitEnterNotice()
			return
		}
		entry, _ := store.Get(show.Key())
		if !entry.EpisodeDone || entry.At() != (movies.Episode{Season: episode.Season, Number: episode.Number}) || play.last {
			return
		}
		index++
		start = 0
		if config.NextEpisodePrompt && !askPlayNext(episodes[index]) {
			return
		}
	}
}

// askPlayNext asks whether to go on to the next episode.
func askPlayNext(next movies.Episode) bool {
	ClearScreen()
	picked, ok := pickMovieOption([]SelectionOption{
		{Key: "NEXT", Label: "Play " + next.Label()},
		{Key: "STOP", Label: "Stop here"},
	})
	return ok && picked.Key == "NEXT"
}

// playSources plays the first of sources that plays, reporting whether one
// did, then deals with what follows: the server is remembered for next
// time, Trakt is told where it stopped, a film or series stopped part way
// goes on the watchlist, and one just finished is offered a rating.
func playSources(config *Config, store *movies.Store, lib *movieLibrary, play moviePlay, sources []movies.Source) bool {
	movie := play.movie
	title := play.title()
	Out(fmt.Sprintf("Loading %s…", title))
	before, known := store.Get(movie.Key())
	if known {
		sources = movies.PreferServer(sources, before.Server)
	}
	played := func(server string) {
		if err := store.SetServer(movie, server); err != nil {
			Log(fmt.Sprintf("movies: could not save the server: %v", err))
		}
		traktPaused(lib, store, movie)
		after, _ := store.Get(movie.Key())
		askRating, keep := afterMoviePlay(before, after, config.ScoreOnCompletion)
		if keep {
			if err := store.SetWatchlist(movie, true); err != nil {
				Log(fmt.Sprintf("movies: could not save the watchlist: %v", err))
			}
		}
		if askRating {
			ClearScreen()
			Out(fmt.Sprintf("You've finished %s! Would you like to rate it?", movie.Label()))
			if rating, ok := pickMovieRating(0); ok {
				if err := store.SetRating(movie, rating); err != nil {
					Log(fmt.Sprintf("movies: could not save the rating: %v", err))
				}
			}
		}
	}

	for _, source := range sources {
		stream, err := source.Resolve()
		if err != nil {
			Log(fmt.Sprintf("movies: %s: %s: %v", movie.Key(), source.Server, err))
			continue
		}
		Log(fmt.Sprintf("movies: %s: playing %s from %s", movie.Key(), title, stream.Server))
		anime := movieAnime(config, store, play, stream)
		started := func() { traktStarted(lib, store, play) }
		if config.CastToDevice {
			started()
			if castMovie(config, &anime) {
				played(source.Server)
				return true
			}
			continue
		}
		if playMovieInMPV(config, store, play, stream, &anime, started) {
			played(source.Server)
			return true
		}
	}
	return false
}

// afterMoviePlay decides what follows a play: a film or series finished
// just now and not yet rated is offered a rating when ScoreOnCompletion is
// on, and one started but not finished goes on the watchlist so it is not
// lost.
func afterMoviePlay(before, after movies.Entry, scoreOnCompletion bool) (askRating, addToWatchlist bool) {
	if after.Watched {
		return scoreOnCompletion && !before.Watched && after.Rating == 0, false
	}
	return false, after.Started() && !after.Watchlist
}

// movieAnime dresses a film or an episode as the one-episode, untracked show
// the player and the cast code know how to play.
func movieAnime(config *Config, store *movies.Store, play moviePlay, stream movies.Stream) Anime {
	title := play.title()
	anime := Anime{
		Title:         AnimeTitle{English: title, Romaji: title},
		CoverImage:    play.movie.Poster,
		TotalEpisodes: 1,
		Untracked:     true,
		// Nothing about a movie goes to AniList or MyAnimeList.
		SkipRemoteSync: true,
	}
	anime.Ep.Number = 1
	anime.Ep.Links = []string{stream.URL}
	anime.Ep.StreamReferrer = stream.Referrer
	for _, subtitle := range stream.Subtitles {
		anime.Ep.SubtitleTracks = append(anime.Ep.SubtitleTracks, SubtitleTrack{URL: subtitle.URL, Language: subtitle.Language, Label: cmp.Or(subtitle.Label, subtitle.Language)})
	}
	if len(anime.Ep.SubtitleTracks) > 0 {
		// The provider lists the best match first.
		anime.Ep.SubtitleURL = providers.PickSubtitle(anime.Ep.SubtitleTracks, movieSubtitleLanguage(config, store, play.movie), anime.Ep.SubtitleTracks[0].URL)
	}
	anime.Movie = &MoviePlayback{
		Start: float64(play.start),
		Progress: func(position float64, duration int, watched bool) {
			play.save(store, int(position), duration, watched)
		},
	}
	return anime
}

// playMovieInMPV plays one server's stream in mpv, reporting whether it
// played. started is called once the player is up.
func playMovieInMPV(config *Config, store *movies.Store, play moviePlay, stream movies.Stream, anime *Anime, started func()) bool {
	args := movieMPVArgs(stream)
	if play.start > 0 {
		// A few seconds back, so the line it stopped on is heard again.
		args = append(args, fmt.Sprintf("--start=%d", max(play.start-5, 0)))
	}
	Log(fmt.Sprintf("movies: %s: mpv %v %s (referrer %q)", stream.Server, args, stream.URL, stream.Referrer))
	socket, err := StartVideo(stream.URL, args, play.title(), anime)
	if err != nil {
		Log(fmt.Sprintf("movies: %s: could not start the player: %v", stream.Server, err))
		return false
	}
	anime.Ep.Player.SocketPath = socket
	if socket == "android-intent" {
		Out("Opened it in mpv. Press Enter when you have finished watching...")
		AwaitEnter()
		return true
	}
	if !WaitForMPVPlaybackStart(socket, MpvPlaybackStartTimeoutDuration(config)) {
		Log(fmt.Sprintf("movies: %s did not start playing", stream.Server))
		if IsMPVRunning(socket) {
			ExitMPV(socket)
		}
		anime.Ep.Player.SocketPath = ""
		return false
	}
	started()
	addAlternateSubtitles(MPVSendCommand, socket, anime.Ep.SubtitleURL, anime.Ep.SubtitleTracks)
	watchMoviePlayback(config, store, play, socket)
	return true
}

// watchMoviePlayback follows playback until the player closes or the video
// ends, saving the position as it goes so a crash loses little.
func watchMoviePlayback(config *Config, store *movies.Store, play moviePlay, socket string) {
	position, duration := 0, 0
	lastSaved := time.Now()
	save := func() {
		watched := PercentageWatched(position, duration) >= float64(config.PercentageToMarkComplete)
		play.save(store, position, duration, watched)
	}
	for {
		value, err := MPVSendCommand(socket, []interface{}{"get_property", "time-pos"})
		if err != nil {
			if isMPVConnectionGoneError(err) {
				save()
				return
			}
		} else if seconds, ok := value.(float64); ok {
			position = int(seconds + 0.5)
		}
		if duration == 0 {
			if value, err := MPVSendCommand(socket, []interface{}{"get_property", "duration"}); err == nil {
				if seconds, ok := value.(float64); ok {
					duration = int(seconds + 0.5)
				}
			}
		}
		// The player stays open at the end (--idle), so the end is it going
		// idle rather than it closing. Stopping without closing it idles it
		// too, so how far it got still decides whether it was watched.
		if idle, err := MPVSendCommand(socket, []interface{}{"get_property", "idle-active"}); err == nil {
			if done, ok := idle.(bool); ok && done {
				save()
				ExitMPV(socket)
				return
			}
		}
		if time.Since(lastSaved) > 15*time.Second && position > 0 {
			save()
			lastSaved = time.Now()
		}
		time.Sleep(time.Second)
	}
}

// downloadMovie saves a film to the download folder from the first server
// that hands over a file.
func downloadMovie(config *Config, lib *movieLibrary, movie movies.Movie) {
	movie, sources, err := lib.open(config, movie)
	if err != nil {
		Out(fmt.Sprintf("Could not open %s: %v", movie.Label(), err))
		awaitEnterNotice()
		return
	}
	downloadSources(config, lib, moviePlay{movie: movie}, sources)
}

// downloadEpisode saves one episode of a series.
func downloadEpisode(config *Config, lib *movieLibrary, show movies.Movie, episode movies.Episode) {
	play := moviePlay{movie: show, episode: episode}
	sources, err := lib.openEpisode(show, episode)
	if err != nil {
		Out(fmt.Sprintf("Could not open %s: %v", play.title(), err))
		awaitEnterNotice()
		return
	}
	downloadSources(config, lib, play, sources)
}

// downloadSources downloads from the first of sources that hands over a
// file.
func downloadSources(config *Config, lib *movieLibrary, play moviePlay, sources []movies.Source) {
	binary, err := ffmpegPath()
	if err != nil {
		Out("Downloading needs ffmpeg, which was not found.")
		awaitEnterNotice()
		return
	}
	dir := ResolveDownloadDir(config)
	if err := os.MkdirAll(dir, 0755); err != nil {
		Out(fmt.Sprintf("Could not create %s: %v", dir, err))
		awaitEnterNotice()
		return
	}
	output := movieDownloadPath(dir, play, DownloadFormat(config))
	if info, err := os.Stat(output); err == nil && info.Size() > 0 {
		Out("Already downloaded: " + output)
		awaitEnterNotice()
		return
	}
	title := play.title()
	for _, source := range sources {
		stream, err := source.Resolve()
		if err != nil {
			Log(fmt.Sprintf("movies: %s: %s: %v", play.movie.Key(), source.Server, err))
			continue
		}
		anime := movieAnime(config, lib.store, play, stream)
		job := ffmpegJob{
			Stream:   stream.URL,
			Referrer: stream.Referrer,
			Subtitle: anime.Ep.SubtitleURL,
			Output:   output,
			File:     !stream.HLS,
		}
		Out(fmt.Sprintf("Downloading %s from %s…", title, stream.Server))
		lastReport := time.Now()
		onProgress := func(elapsed time.Duration) {
			if time.Since(lastReport) < 5*time.Second {
				return
			}
			lastReport = time.Now()
			Out(fmt.Sprintf("  %s downloaded", elapsed.Round(time.Second)))
		}
		err = runFFmpeg(binary, job.args(), onProgress)
		if err != nil && job.Subtitle != "" {
			Log(fmt.Sprintf("movies: download with subtitles failed (%v); retrying without them", err))
			job.Subtitle = ""
			err = runFFmpeg(binary, job.args(), onProgress)
		}
		if err != nil {
			os.Remove(output)
			Log(fmt.Sprintf("movies: %s: download from %s failed: %v", play.movie.Key(), stream.Server, err))
			continue
		}
		Out("Saved " + output)
		awaitEnterNotice()
		return
	}
	Out(fmt.Sprintf("None of the servers for %s could be downloaded.", title))
	awaitEnterNotice()
}

// movieDownloadPath names a downloaded film "Title (Year).mkv" and an
// episode "Title - S01E03.mkv".
func movieDownloadPath(dir string, play moviePlay, format string) string {
	name := play.movie.Title
	if play.episode.Number > 0 {
		name += fmt.Sprintf(" - S%02dE%02d", play.episode.Season, play.episode.Number)
	} else if play.movie.Year != "" {
		name += " (" + play.movie.Year + ")"
	}
	if format != DownloadFormatMP4 {
		format = DownloadFormatMKV
	}
	return filepath.Join(dir, SanitizeFilename(name)+"."+format)
}
