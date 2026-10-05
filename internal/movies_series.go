package internal

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/thexykril/otakase/internal/icons"
	"github.com/thexykril/otakase/internal/movies"
)

// Series are played an episode at a time from their own menu: the episode
// up next first, then the list of episodes, then what a film's menu has.

const (
	movieEpisodesKey = "MOVIE:EPISODES"
	movieEpisodeKey  = "MOVIE_EPISODE:"
	movieSeasonKey   = "MOVIE_SEASON:"
)

// nextEpisode is where a series carries on from: the episode stopped part
// way, else the one after the last finished, else the first. It reports
// the index in episodes and the second to start at.
func nextEpisode(entry movies.Entry, episodes []movies.Episode) (index, start int) {
	if entry.Watched || entry.Episode == 0 {
		return 0, 0
	}
	for i, episode := range episodes {
		if episode.Season != entry.Season || episode.Number != entry.Episode {
			continue
		}
		if !entry.EpisodeDone {
			return i, entry.Position
		}
		if i+1 < len(episodes) {
			return i + 1, 0
		}
		return i, 0
	}
	return 0, 0
}

// episodeSeen reports whether an episode comes before where the series was
// left, or is the episode it was left at and that one was finished.
func episodeSeen(entry movies.Entry, episode movies.Episode) bool {
	if entry.Watched {
		return true
	}
	if entry.Episode == 0 {
		return false
	}
	if episode.Season != entry.Season {
		return episode.Season < entry.Season
	}
	if episode.Number == entry.Episode {
		return entry.EpisodeDone
	}
	return episode.Number < entry.Episode
}

// openSeries is a series' menu.
func openSeries(config *Config, store *movies.Store, lib *movieLibrary, show movies.Movie) {
	for {
		entry, known := store.Get(show.Key())
		episodes, err := lib.episodes(show)
		options := []SelectionOption{}
		message := []string{show.Label()}
		if err != nil {
			Log(fmt.Sprintf("movies: %s: episodes: %v", show.Key(), err))
			message = append(message, "Could not list the episodes: "+err.Error())
		} else {
			index, start := nextEpisode(entry, episodes)
			label := "Play " + episodes[index].Label()
			switch {
			case start > 0:
				label = fmt.Sprintf("Continue %s from %s", episodes[index].Label(), formatClock(start))
			case entry.Watched:
				label = "Watch again from " + episodes[0].Label()
			case entry.Episode > 0:
				label = "Play next: " + episodes[index].Label()
			}
			options = append(options,
				SelectionOption{Key: moviePlayKey, Label: label, Icon: icons.Play},
				SelectionOption{Key: movieEpisodesKey, Label: fmt.Sprintf("Episodes (%d)", len(episodes)), Icon: icons.TV})
		}
		if entry.Started() || entry.Watched {
			options = append(options, SelectionOption{Key: movieUnwatchedKey, Label: "Start over (mark as not watched)", Icon: icons.Undo})
		}
		rateLabel := "Rate"
		if entry.Rating > 0 {
			rateLabel = fmt.Sprintf("Rating: %d/10", entry.Rating)
		}
		options = append(options, SelectionOption{Key: movieRateKey, Label: rateLabel, Icon: icons.Star})
		if err == nil {
			options = append(options, SelectionOption{Key: movieDownloadKey, Label: "Download an episode", Icon: icons.Download})
		}
		if show.Provider == movies.VidsrcName {
			options = append(options, SelectionOption{Key: movieSubsKey, Label: "Subtitles: " + titleCase(movieSubtitleLanguage(config, store, show)), Icon: icons.Audio})
		}
		if entry.Watchlist {
			options = append(options, SelectionOption{Key: movieListOffKey, Label: "Remove from watchlist", Icon: icons.No})
		} else if !entry.Watched {
			options = append(options, SelectionOption{Key: movieListOnKey, Label: "Add to watchlist", Icon: icons.Add})
		}
		if known {
			options = append(options, SelectionOption{Key: movieForgetKey, Label: "Remove from history", Icon: icons.Dropped})
		}

		ClearScreen()
		if details, found := movieDetails(lib, store, show); found {
			message = append(message, movieDetailsText(details)...)
		}
		picked, ok := pickMovieOptionWithMessage(config, options, strings.Join(message, "\n"))
		if !ok {
			return
		}
		switch picked.Key {
		case moviePlayKey:
			index, start := nextEpisode(entry, episodes)
			playEpisodes(config, store, lib, show, episodes, index, start)
		case movieEpisodesKey:
			if index, ok := pickEpisode(config, entry, episodes); ok {
				start := 0
				if episodes[index].Season == entry.Season && episodes[index].Number == entry.Episode && !entry.EpisodeDone {
					start = entry.Position
				}
				playEpisodes(config, store, lib, show, episodes, index, start)
			}
		case movieDownloadKey:
			if index, ok := pickEpisode(config, entry, episodes); ok {
				downloadEpisode(config, lib, show, episodes[index])
			}
		case movieUnwatchedKey:
			if err := store.SetWatched(show, false); err != nil {
				Log(fmt.Sprintf("movies: could not save: %v", err))
			}
		case movieRateKey:
			if rating, ok := pickMovieRating(entry.Rating); ok {
				if err := store.SetRating(show, rating); err != nil {
					Log(fmt.Sprintf("movies: could not save the rating: %v", err))
				}
			}
		case movieSubsKey:
			if language, ok := pickMovieSubtitleLanguage(config, entry.SubtitleLanguage); ok {
				if err := store.SetSubtitleLanguage(show, language); err != nil {
					Log(fmt.Sprintf("movies: could not save the subtitle language: %v", err))
				}
			}
		case movieListOnKey, movieListOffKey:
			if err := store.SetWatchlist(show, picked.Key == movieListOnKey); err != nil {
				Log(fmt.Sprintf("movies: could not save the watchlist: %v", err))
			}
		case movieForgetKey:
			if err := store.Remove(show.Key()); err != nil {
				Log(fmt.Sprintf("movies: could not forget %s: %v", show.Key(), err))
			}
			return
		}
	}
}

// pickEpisode asks for an episode: the season first when there are several,
// the one the series was left in marked. It reports the index in episodes.
func pickEpisode(config *Config, entry movies.Entry, episodes []movies.Episode) (int, bool) {
	seasons := []int{}
	for _, episode := range episodes {
		if len(seasons) == 0 || seasons[len(seasons)-1] != episode.Season {
			seasons = append(seasons, episode.Season)
		}
	}
	season := seasons[0]
	for {
		if len(seasons) > 1 {
			options := make([]SelectionOption, 0, len(seasons))
			for _, n := range seasons {
				icon := icons.TV
				if n == entry.Season && !entry.Watched {
					icon = icons.Play
				}
				options = append(options, SelectionOption{Key: movieSeasonKey + strconv.Itoa(n), Label: fmt.Sprintf("Season %d", n), Icon: icon})
			}
			picked, ok := pickMovieOption(options)
			if !ok {
				return 0, false
			}
			season, _ = strconv.Atoi(strings.TrimPrefix(picked.Key, movieSeasonKey))
		}
		options := []SelectionOption{}
		for i, episode := range episodes {
			if episode.Season != season {
				continue
			}
			label, icon := episode.Label(), icons.TV
			switch {
			case episode.Season == entry.Season && episode.Number == entry.Episode && !entry.EpisodeDone && entry.Position > 0 && !entry.Watched:
				label += " · stopped at " + formatClock(entry.Position)
				icon = icons.Play
			case episodeSeen(entry, episode):
				icon = icons.Completed
			}
			options = append(options, SelectionOption{Key: movieEpisodeKey + strconv.Itoa(i), Label: label, Icon: icon})
		}
		picked, ok := pickMovieOption(options)
		if ok {
			index, err := strconv.Atoi(strings.TrimPrefix(picked.Key, movieEpisodeKey))
			return index, err == nil && index >= 0 && index < len(episodes)
		}
		if len(seasons) == 1 {
			return 0, false
		}
		// Back from the episodes is back to the seasons.
	}
}
