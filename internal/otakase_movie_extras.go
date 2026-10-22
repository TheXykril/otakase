package internal

import (
	"fmt"
	"time"
)

// movieExtras is what the film knows beyond the stats page: who the viewer
// is, what they love, and who made it. All of it comes from AniList, in one
// request; MyAnimeList has no equivalent, and the film does without.
type movieExtras struct {
	Name        string
	MemberSince time.Time
	Studios     []string
	VoiceActor  string
	Tags        []string
	StartYear   int
	// Favourites are the shows the wall shows, the viewer's own favourites
	// first and their top scored after.
	Favourites []movieFavourite
	// Watching is the show the viewer last moved on, and NextEpisode the
	// episode they are up to in it, for the "next time" card.
	Watching    string
	NextEpisode int
}

type movieFavourite struct {
	Title string
	Cover string
	// Path is the cover once downloaded, empty until then.
	Path string
}

// movieWallSize is how many covers the favourites wall holds.
const movieWallSize = 5

const aniListMovieExtrasQuery = `
query {
	Viewer {
		id
		name
		createdAt
		favourites { anime(perPage: 5) { nodes { title { romaji english } coverImage { extraLarge } } } }
		statistics {
			anime {
				studios(limit: 3, sort: COUNT_DESC) { studio { name } }
				voiceActors(limit: 1, sort: COUNT_DESC) { voiceActor { name { full } } }
				tags(limit: 3, sort: COUNT_DESC) { tag { name } }
				startYears(limit: 1, sort: COUNT_DESC) { startYear }
			}
		}
	}
}`

const aniListMovieListsQuery = `
query ($userId: Int) {
	top: Page(perPage: 5) {
		mediaList(userId: $userId, type: ANIME, sort: [SCORE_DESC, PROGRESS_DESC]) {
			media { title { romaji english } coverImage { extraLarge } }
		}
	}
	current: Page(perPage: 1) {
		mediaList(userId: $userId, type: ANIME, status: CURRENT, sort: UPDATED_TIME_DESC) {
			progress
			media { title { romaji english } coverImage { extraLarge } episodes }
		}
	}
}`

type movieMediaNode struct {
	Title struct {
		Romaji  string `json:"romaji"`
		English string `json:"english"`
	} `json:"title"`
	CoverImage struct {
		ExtraLarge string `json:"extraLarge"`
	} `json:"coverImage"`
}

func (node movieMediaNode) favourite(config *Config) movieFavourite {
	media := Media{Title: AnimeTitle{Romaji: node.Title.Romaji, English: node.Title.English}}
	return movieFavourite{Title: mediaDisplayTitle(media, config), Cover: node.CoverImage.ExtraLarge}
}

// fetchMovieExtras reads the extras and downloads the wall's covers.
func fetchMovieExtras(config *Config) (movieExtras, error) {
	key := statsTrackerKey(config)
	if key != TrackingRemoteAniList && key != TrackingRemoteBoth {
		return movieExtras{}, fmt.Errorf("extras need AniList")
	}
	token, err := GetTokenFromFile(anilistTokenPath(config))
	if err != nil {
		return movieExtras{}, fmt.Errorf("no AniList sign-in: %w", err)
	}

	var answer struct {
		Data struct {
			Viewer struct {
				ID         int    `json:"id"`
				Name       string `json:"name"`
				CreatedAt  int64  `json:"createdAt"`
				Favourites struct {
					Anime struct {
						Nodes []movieMediaNode `json:"nodes"`
					} `json:"anime"`
				} `json:"favourites"`
				Statistics struct {
					Anime struct {
						Studios []struct {
							Studio struct {
								Name string `json:"name"`
							} `json:"studio"`
						} `json:"studios"`
						VoiceActors []struct {
							VoiceActor struct {
								Name struct {
									Full string `json:"full"`
								} `json:"name"`
							} `json:"voiceActor"`
						} `json:"voiceActors"`
						Tags []struct {
							Tag struct {
								Name string `json:"name"`
							} `json:"tag"`
						} `json:"tags"`
						StartYears []struct {
							StartYear int `json:"startYear"`
						} `json:"startYears"`
					} `json:"anime"`
				} `json:"statistics"`
			} `json:"Viewer"`
		} `json:"data"`
	}
	if err := aniListStatsRequest(token, aniListMovieExtrasQuery, nil, &answer); err != nil {
		return movieExtras{}, err
	}
	viewer := answer.Data.Viewer
	anime := viewer.Statistics.Anime

	extras := movieExtras{Name: viewer.Name}
	if viewer.CreatedAt > 0 {
		extras.MemberSince = time.Unix(viewer.CreatedAt, 0)
	}
	for _, studio := range anime.Studios {
		extras.Studios = append(extras.Studios, studio.Studio.Name)
	}
	if len(anime.VoiceActors) > 0 {
		extras.VoiceActor = anime.VoiceActors[0].VoiceActor.Name.Full
	}
	for _, tag := range anime.Tags {
		extras.Tags = append(extras.Tags, tag.Tag.Name)
	}
	if len(anime.StartYears) > 0 {
		extras.StartYear = anime.StartYears[0].StartYear
	}

	seen := map[string]bool{}
	add := func(node movieMediaNode) {
		favourite := node.favourite(config)
		if favourite.Cover == "" || seen[favourite.Cover] || len(extras.Favourites) == movieWallSize {
			return
		}
		seen[favourite.Cover] = true
		extras.Favourites = append(extras.Favourites, favourite)
	}
	for _, node := range viewer.Favourites.Anime.Nodes {
		add(node)
	}
	if viewer.ID != 0 {
		var lists struct {
			Data struct {
				Top struct {
					MediaList []struct {
						Media movieMediaNode `json:"media"`
					} `json:"mediaList"`
				} `json:"top"`
				Current struct {
					MediaList []struct {
						Progress int `json:"progress"`
						Media    struct {
							movieMediaNode
							Episodes int `json:"episodes"`
						} `json:"media"`
					} `json:"mediaList"`
				} `json:"current"`
			} `json:"data"`
		}
		if err := aniListStatsRequest(token, aniListMovieListsQuery, map[string]interface{}{"userId": viewer.ID}, &lists); err != nil {
			Log(fmt.Sprintf("Otakase movie: lists: %v", err))
		}
		for _, entry := range lists.Data.Top.MediaList {
			add(entry.Media)
		}
		if current := lists.Data.Current.MediaList; len(current) > 0 {
			entry := current[0]
			if episodes := entry.Media.Episodes; episodes == 0 || entry.Progress < episodes {
				extras.Watching = entry.Media.favourite(config).Title
				extras.NextEpisode = entry.Progress + 1
			}
		}
	}

	// A cover that will not download leaves the wall, rather than a hole in it.
	kept := extras.Favourites[:0]
	for _, favourite := range extras.Favourites {
		path, err := downloadToCache(favourite.Cover)
		if err != nil {
			Log(fmt.Sprintf("Otakase movie: cover: %v", err))
			continue
		}
		favourite.Path = path
		kept = append(kept, favourite)
	}
	extras.Favourites = kept
	return extras, nil
}
