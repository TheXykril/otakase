package internal

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"
)

// What the film says: the numbers it counts up, the conversation it has about
// the viewer, and the crew it credits. Every line has a few wordings and the
// film picks among them each time it plays, so a second viewing is not a
// rerun of the first.

// movieFacts is everything the story is written from.
type movieFacts struct {
	Viewer    string
	Milestone int

	Episodes    int
	Minutes     int
	Count       int
	Completed   int
	MeanScore   float64
	Genres      []string
	Binged      string
	BingedCount int
	Streak      int
	// GenreCount is how many genres the viewer has watched at all, and
	// NightShare the part of their recent episodes watched between midnight
	// and five in the morning.
	GenreCount int
	NightShare float64

	Extras movieExtras
}

// gatherMovieFacts reads the facts out of whatever the trackers answered.
func gatherMovieFacts(stats watchStats, extras movieExtras, viewer string, milestone int, now time.Time) movieFacts {
	if strings.TrimSpace(viewer) == "" {
		viewer = "You"
	}
	facts := movieFacts{Viewer: viewer, Milestone: milestone, Extras: extras}
	if totals := stats.AniList; totals != nil {
		facts.Episodes = totals.EpisodesWatched
		facts.Minutes = totals.MinutesWatched
		facts.Count = totals.Count
		facts.MeanScore = totals.MeanScore
		for _, status := range totals.Statuses {
			if status.Name == "COMPLETED" {
				facts.Completed = status.Count
			}
		}
		for _, genre := range totals.Genres {
			if len(facts.Genres) < 3 {
				facts.Genres = append(facts.Genres, genre.Name)
			}
		}
		facts.GenreCount = len(totals.Genres)
		facts.Binged, facts.BingedCount = statsMostBinged(stats.Activities)
		_, facts.Streak = statsStreaks(stats.Activities, now)
		facts.NightShare = nightShare(stats.Activities, now.Location())
	}
	if totals := stats.MyAnimeList; totals != nil {
		facts.Episodes = totals.Episodes
		facts.Minutes = int(math.Round(totals.DaysWatched * 24 * 60))
		facts.Count = totals.Items
		facts.Completed = totals.Completed
		facts.MeanScore = totals.MeanScore
	}
	if facts.Milestone > facts.Episodes {
		facts.Episodes = facts.Milestone
	}
	return facts
}

// nightShare is the part of the episodes in activities that were watched
// between midnight and five, local time.
func nightShare(activities []statsActivity, location *time.Location) float64 {
	var night, all int
	for _, activity := range activities {
		if activity.Episodes <= 0 {
			continue
		}
		all += activity.Episodes
		if activity.At.In(location).Hour() < 5 {
			night += activity.Episodes
		}
	}
	if all < 20 {
		return 0
	}
	return float64(night) / float64(all)
}

// pick is one of the wordings, chosen by rng.
func pick(rng *rand.Rand, options ...string) string {
	return options[rng.IntN(len(options))]
}

// statCard is one of the numbers chapter's cards: a label, a number that
// counts up from nothing, what it counts, and a line about it.
type statCard struct {
	Label string
	Value int
	Unit  string
	Quip  string
}

// movieStatCards picks the cards worth showing, at most four.
func movieStatCards(facts movieFacts, rng *rand.Rand) []statCard {
	var cards []statCard
	if facts.Episodes > 0 {
		quip := pick(rng,
			"Every one of them started with \"okay, last one\".",
			"That's a lot of \"just one more\".",
			"Previously, on "+facts.Viewer+"'s life...",
		)
		if facts.Milestone > 0 {
			quip = pick(rng,
				"A milestone. The credits were always going to roll.",
				"Some people count sheep. You count episodes.",
			)
		}
		cards = append(cards, statCard{"Episodes watched", facts.Episodes, "episodes", quip})
	}
	if hours := facts.Minutes / 60; hours > 0 {
		days := float64(facts.Minutes) / (24 * 60)
		cards = append(cards, statCard{"Time in other worlds", hours, "hours", pick(rng,
			fmt.Sprintf("About %.0f days. Most people call that a vacation.", math.Round(days)),
			fmt.Sprintf("%.0f days, if you watched without blinking.", math.Round(days)),
			fmt.Sprintf("As a full-time job, %d work weeks. No overtime pay.", max(1, facts.Minutes/2400)),
		)})
	}
	if facts.Completed > 0 {
		cards = append(cards, statCard{"Stories finished", facts.Completed, "series", pick(rng,
			"Seen to the very last episode. Respect.",
			strconv.Itoa(facts.Completed)+" finales, and not one of them was enough.",
			"Every ending you stayed for.",
		)})
	}
	if facts.BingedCount > 1 && facts.Binged != "" {
		cards = append(cards, statCard{"Most binged lately", facts.BingedCount, "episodes of " + ellipsize(facts.Binged, 40), pick(rng,
			"Sleep was optional.",
			"The autoplay button never stood a chance.",
		)})
	} else if facts.Streak > 1 {
		cards = append(cards, statCard{"Longest streak", facts.Streak, "days in a row", pick(rng,
			"Discipline, or something like it.",
			"Not a single day off.",
		)})
	}
	if len(cards) > 4 {
		cards = cards[:4]
	}
	return cards
}

// poemLine is one line of the conversation and which of the two voices says
// it.
type poemLine struct {
	Voice int
	Text  string
}

// moviePoem is the conversation the film closes on: two voices talking
// about the viewer as if they could not hear, the way the end of a long game
// does, written from the viewer's own numbers. The opening and the goodbye
// always play; between them the film picks some of the passages it has data
// for, in a new order each time, so no two viewings say the same things.
func moviePoem(facts movieFacts, rng *rand.Rand) []poemLine {
	var lines []poemLine
	say := func(voice int, options ...string) {
		lines = append(lines, poemLine{voice, pick(rng, options...)})
	}
	name := facts.Viewer

	say(0, "I see the viewer you meant.", "There. The one still watching.", "Look. The screen is still on.")
	say(1, name+"?")
	if facts.Episodes > 0 {
		say(0,
			fmt.Sprintf("Yes. %s episodes, and still going.", formatStatsCount(facts.Episodes)),
			fmt.Sprintf("Yes. %s episodes so far. They lost count. We did not.", formatStatsCount(facts.Episodes)),
		)
	} else {
		say(0, "Yes. Every episode, one at a time.")
	}
	say(1, "Do they know we are talking about them?", "Can they hear us?")
	say(0,
		"Sometimes. In the quiet between episodes, they almost do.",
		"No. They think this is just the ending song.",
	)

	var passages []func()
	if facts.Minutes > 0 {
		passages = append(passages, func() {
			say(1,
				formatWatchMinutes(facts.Minutes)+" in other worlds. That is a long way from home.",
				"They have spent "+formatWatchMinutes(facts.Minutes)+" in stories that were not theirs.",
			)
			say(0, "They were not lost there. They were looking for something.", "Not lost. Visiting.")
			say(1, "And did they find it?", "What did they bring back?")
			say(0, "Pieces of it. A line here, a feeling there.", "More than they know.")
		})
	}
	if found := poemGenres(facts.Genres); len(found) > 0 {
		passages = append(passages, func() {
			say(1, "What do they look for, in all those stories?", "Why those stories?")
			for i, line := range found {
				say(i%2, line)
			}
		})
	}
	if facts.Completed > 0 {
		passages = append(passages, func() {
			say(1, fmt.Sprintf("They finished %s stories.", formatStatsCount(facts.Completed)))
			say(0, "And missed every one, for a day or two after.", "Each ending left a small, good ache.")
			if waiting := facts.Count - facts.Completed; waiting > 5 {
				say(1, "And the others?")
				say(0, fmt.Sprintf("Waiting. Stories are patient. %s of them are holding their place.", formatStatsCount(waiting)))
			}
		})
	}
	if facts.Binged != "" && facts.BingedCount > 1 {
		passages = append(passages, func() {
			say(0,
				fmt.Sprintf("Lately, %s. %d episodes, almost without stopping.", facts.Binged, facts.BingedCount),
				fmt.Sprintf("Lately, %s. They would not let it end.", facts.Binged),
			)
			say(1, "\"Just one more,\" they said. Many times.", "The night got shorter every episode.")
		})
	}
	if facts.Streak > 1 {
		passages = append(passages, func() {
			say(1, fmt.Sprintf("%d days in a row, they came back.", facts.Streak))
			say(0, "Like a habit.", "Like a promise.")
		})
	}
	if score := facts.MeanScore; score > 0 {
		if score > 10 {
			score /= 10
		}
		passages = append(passages, func() {
			say(1, fmt.Sprintf("They give scores, you know. %.1f, on average.", score))
			if score >= 7.5 {
				say(0, "Generous. They want to love what they watch.", "Kind. They see the best in a story.")
			} else {
				say(0, "Strict. They are waiting for something to earn a ten.", "Honest. A ten from them would mean something.")
			}
		})
	}
	if len(facts.Extras.Studios) > 0 {
		passages = append(passages, func() {
			say(0, "They trust "+facts.Extras.Studios[0]+".", "When "+facts.Extras.Studios[0]+" makes something, they notice.")
			say(1, "With good reason.", "That trust has been rewarded.")
		})
	}
	if voice := facts.Extras.VoiceActor; voice != "" {
		passages = append(passages, func() {
			say(1, "They have heard "+voice+" more than some of their friends.", "One voice keeps coming back. "+voice+".")
			say(0, "A familiar voice is a kind of home.", "They would know it anywhere.")
		})
	}
	if len(facts.Extras.Tags) > 0 {
		passages = append(passages, func() {
			say(1, "The same things keep finding them. "+strings.Join(facts.Extras.Tags, ", ")+".")
			say(0, "Or they keep finding those things. From here, it is hard to tell.")
		})
	}
	if favourites := facts.Extras.Favourites; len(favourites) > 0 {
		passages = append(passages, func() {
			say(0, "Some stories, they keep.", "Some stories never really end for them.")
			say(1, favourites[0].Title+". They would watch it for the first time again, if they could.")
			if len(favourites) > 2 {
				say(0, "And "+favourites[1].Title+". And "+favourites[2].Title+".")
			}
		})
	}
	if year := facts.Extras.StartYear; year > 0 {
		passages = append(passages, func() {
			say(1, fmt.Sprintf("Their favourite year was %d.", year))
			say(0, "A good year. Its stories still feel warm.", "They go back to it the way you go back to an old street.")
		})
	}
	if since := facts.Extras.MemberSince; !since.IsZero() {
		passages = append(passages, func() {
			say(0, "Since "+since.Format("January 2006")+", they keep coming back.", "They started keeping count in "+since.Format("2006")+".")
			say(1, "Long enough to forget the first one.", "Long enough to become a different person.")
		})
	}
	passages = append(passages,
		func() {
			say(1, "They skip the openings.", "Sometimes they skip the openings.")
			say(0, "Not all of them. The good ones, they let play.", "Only the ones they already know by heart.")
		},
		func() {
			say(0, "They watch when the house is quiet.", "They watch long after everyone else is asleep.")
			say(1, "Some nights, the sky turned light before they stopped.")
		},
	)
	rng.Shuffle(len(passages), func(i, j int) { passages[i], passages[j] = passages[j], passages[i] })
	for _, passage := range passages[:min(len(passages), 8)] {
		passage()
	}

	if facts.Milestone > 0 {
		say(1, fmt.Sprintf("And today, %s episodes.", formatStatsCount(facts.Milestone)))
		say(0, "A number is only a number. But this one, we wanted to mark.")
	}
	say(1, "Do they know the stories end?", "Do they know every season has a finale?")
	say(0, "They know. That is why they start the next one.", "They know. They watch anyway. That is the point.")
	say(1, "Then why do they come back?")
	say(0, "Because every first episode is a door.", "Because somewhere, a character they have not met yet is waiting for them.")
	say(1, "Then tell them.")
	say(0, name+". The story you were watching was never only on the screen.", name+". You were in every one of them, a little.")
	say(1, "The worlds were real while you were in them. So were you.")
	say(0, "Every hero you cheered for was showing you how.", "You carried a piece of each one back out with you.")
	say(1, "Stretch. Drink some water. Look at the sky.", "Rest your eyes. Open a window.")
	say(0, "And when you are ready...")
	say(1, "...see you next episode.")
	return lines
}

// poemGenres says what the viewer finds in their top genres, one line each.
// A genre without a line is left out.
func poemGenres(genres []string) []string {
	found := map[string]string{
		"Action":        "a reason to stand back up",
		"Adventure":     "that the road matters more than where it ends",
		"Comedy":        "a reason to laugh at three in the morning",
		"Drama":         "the courage to feel things all the way",
		"Fantasy":       "worlds bigger than the one outside the window",
		"Horror":        "that fear can be survived",
		"Mahou Shoujo":  "that kindness can be a kind of strength",
		"Mecha":         "that the pilot matters more than the machine",
		"Music":         "a rhythm to keep going to",
		"Mystery":       "that every question has an answer, eventually",
		"Psychological": "that minds are the strangest worlds of all",
		"Romance":       "that people can be brave for each other",
		"Sci-Fi":        "futures worth hoping for",
		"Slice of Life": "that ordinary days are worth watching",
		"Sports":        "that trying counts, even when you lose",
		"Supernatural":  "that the world holds more than it shows",
		"Thriller":      "how loud a heartbeat can get",
	}
	var lines []string
	for _, genre := range genres {
		if line, ok := found[genre]; ok {
			lines = append(lines, "In "+strings.ToLower(genre)+", "+line+".")
		}
	}
	return lines
}

// achievement is one badge of the achievements chapter. A rare one is
// drawn in gold and gets a bigger sound.
type achievement struct {
	Name   string
	Detail string
	Rare   bool
}

// movieAchievements are the badges the viewer has earned, common first and
// rare last so the chapter builds, at most eight. The last is always the
// one for watching the film itself.
func movieAchievements(facts movieFacts, rng *rand.Rand) []achievement {
	var earned []achievement
	add := func(ok bool, rare bool, name, detail string) {
		if ok {
			earned = append(earned, achievement{name, detail, rare})
		}
	}
	add(facts.Episodes >= 1000, facts.Episodes >= 5000, "Marathoner", formatStatsCount(facts.Episodes)+" episodes watched")
	add(facts.Completed >= 50, facts.Completed >= 200, "Finisher", formatStatsCount(facts.Completed)+" series seen to the end")
	add(facts.Count >= 100, facts.Count >= 500, "Collector", formatStatsCount(facts.Count)+" shows on the list")
	add(facts.Minutes >= 30*24*60, facts.Minutes >= 100*24*60, "Time Traveller", formatWatchMinutes(facts.Minutes)+" in other worlds")
	add(facts.NightShare >= 0.25, facts.NightShare >= 0.5, "Night Owl", fmt.Sprintf("%.0f%% of episodes after midnight", facts.NightShare*100))
	add(facts.BingedCount >= 12, facts.BingedCount >= 24, "Binge Mode", fmt.Sprintf("%d episodes of %s", facts.BingedCount, ellipsize(facts.Binged, 24)))
	add(facts.Streak >= 7, facts.Streak >= 30, "Iron Will", fmt.Sprintf("%d days in a row", facts.Streak))
	add(facts.GenreCount >= 12, facts.GenreCount >= 17, "Genre Hopper", fmt.Sprintf("%d genres explored", facts.GenreCount))
	if score := facts.MeanScore; score > 0 {
		if score > 10 {
			score /= 10
		}
		add(score >= 8, false, "Soft Heart", fmt.Sprintf("Scores %.1f on average", score))
		add(score < 6.5, false, "Tough Critic", fmt.Sprintf("Scores %.1f on average", score))
	}
	if since := facts.Extras.MemberSince; !since.IsZero() {
		years := int(time.Since(since).Hours() / (24 * 365))
		add(years >= 3, years >= 6, "Veteran", "Keeping count since "+since.Format("2006"))
	}
	if len(facts.Extras.Studios) > 0 {
		add(true, false, "Studio Loyalist", "Most watched: "+facts.Extras.Studios[0])
	}
	add(facts.Extras.VoiceActor != "", false, "Familiar Voice", "Knows "+facts.Extras.VoiceActor+" anywhere")
	add(facts.Milestone > 0, true, "Milestone", formatStatsCount(facts.Milestone)+" episodes")

	// Keep the rare ones, then as many common ones as fit, in a new mix
	// each viewing.
	var common, rare []achievement
	for _, badge := range earned {
		if badge.Rare {
			rare = append(rare, badge)
		} else {
			common = append(common, badge)
		}
	}
	rng.Shuffle(len(common), func(i, j int) { common[i], common[j] = common[j], common[i] })
	rare = rare[:min(len(rare), 4)]
	common = common[:min(len(common), 7-len(rare))]
	return append(append(common, rare...), achievement{
		Name:   "Stayed for the Credits",
		Detail: pick(rng, "Found the secret film", "Watched to the very end", "Didn't skip the ending"),
		Rare:   true,
	})
}

// movieNextTime is the anime-style preview card the film ends on: the show
// the viewer is in the middle of, the episode they are up to, and a teaser.
func movieNextTime(facts movieFacts, rng *rand.Rand) (title, episode, teaser string) {
	title, episode = "Something new", "Episode 1"
	if facts.Extras.Watching != "" {
		title = facts.Extras.Watching
		episode = fmt.Sprintf("Episode %d", facts.Extras.NextEpisode)
	}
	teaser = pick(rng,
		"Will "+facts.Viewer+" finally go to sleep?",
		"A new episode. A new chance to say \"just one more\".",
		"Don't miss it!",
		"The story continues.",
		facts.Viewer+" returns. The snacks do not.",
	)
	return title, episode, teaser
}

// movieFarewell is the line under the closing seal.
func movieFarewell(rng *rand.Rand) string {
	return pick(rng, "Mata ne.", "Otsukare.", "See you next episode.")
}
