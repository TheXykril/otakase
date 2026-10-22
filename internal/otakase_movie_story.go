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
		facts.Binged, facts.BingedCount = statsMostBinged(stats.Activities)
		_, facts.Streak = statsStreaks(stats.Activities, now)
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
// does, written from the viewer's own numbers.
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
	if facts.Minutes > 0 {
		say(1,
			formatWatchMinutes(facts.Minutes)+" in other worlds. That is a long way from home.",
			"They have spent "+formatWatchMinutes(facts.Minutes)+" in stories that were not theirs.",
		)
		say(0, "They were not lost there. They were looking for something.", "Not lost. Visiting.")
	}
	if len(facts.Genres) > 0 {
		say(1, "In "+strings.ToLower(facts.Genres[0])+" stories, mostly.", "Mostly "+strings.ToLower(facts.Genres[0])+". It says a lot about them.")
	}
	if facts.Binged != "" && facts.BingedCount > 1 {
		say(0,
			fmt.Sprintf("And in %s. %d episodes, almost without stopping.", facts.Binged, facts.BingedCount),
			fmt.Sprintf("Lately, %s. They would not let it end.", facts.Binged),
		)
	}
	say(1, "They skipped the openings.", "Sometimes they skip the openings.")
	say(0, "Not all of them. The good ones, they let play.", "Only the ones they already knew by heart.")
	if voice := facts.Extras.VoiceActor; voice != "" {
		say(1, "They have heard "+voice+" more than some of their friends.", "One voice keeps coming back. "+voice+".")
	}
	if since := facts.Extras.MemberSince; !since.IsZero() {
		say(0, "Since "+since.Format("January 2006")+", they keep coming back.", "They started keeping count in "+since.Format("2006")+".")
	}
	say(1, "Do they know the stories end?", "Do they know every season has a finale?")
	say(0, "They know. That is why they start the next one.", "They know. They watch anyway. That is the point.")
	say(1, "Then tell them.")
	say(0, name+". The story you were watching was never only on the screen.", name+". You were in every one of them, a little.")
	say(1, "Stretch. Drink some water. Look at the sky.", "Rest your eyes. Open a window.")
	say(0, "And when you are ready...")
	say(1, "...see you next episode.")
	return lines
}

// movieCrew is the closing crew roll: who made what the viewer watches, and
// who really made the film.
func movieCrew(facts movieFacts, rng *rand.Rand) []creditLine {
	crew := []creditLine{{Role: "Directed by", Name: facts.Viewer}}
	for i, studio := range facts.Extras.Studios {
		role := ""
		if i == 0 {
			role = "Animation by"
		}
		crew = append(crew, creditLine{Role: role, Name: studio})
	}
	if facts.Extras.VoiceActor != "" {
		crew = append(crew, creditLine{Role: "Voice you hear most", Name: facts.Extras.VoiceActor})
	}
	if len(facts.Extras.Tags) > 0 {
		crew = append(crew, creditLine{Role: "Recurring themes", Name: strings.Join(facts.Extras.Tags, " · ")})
	}
	if facts.Extras.StartYear > 0 {
		crew = append(crew, creditLine{Role: "Favourite year", Name: strconv.Itoa(facts.Extras.StartYear)})
	}
	if score := facts.MeanScore; score > 0 {
		// AniList reports the mean on the viewer's own scale, out of 10 or
		// out of 100.
		if score > 10 {
			score /= 10
		}
		critic := "A critic with a soft heart"
		if score < 7 {
			critic = "Hard to please"
		}
		crew = append(crew, creditLine{Role: "Critic", Name: critic})
	}
	jokes := []creditLine{
		{Role: "Filmed on location", Name: pick(rng, "Your couch", "Your bed, mostly", "Under a blanket")},
		{Role: "Catering", Name: pick(rng, "Whatever was in the fridge", "Instant ramen", "Snacks you said you'd save")},
		{Role: "Openings skipped", Name: "Too many to count"},
		{Role: "Sleep schedule", Name: pick(rng, "Lost in production", "Cancelled after season one")},
		{Role: "Special thanks", Name: "Every fansubber, ever"},
	}
	return append(crew, jokes...)
}

// movieFarewell is the line under the closing seal.
func movieFarewell(rng *rand.Rand) string {
	return pick(rng, "Mata ne.", "Otsukare.", "See you next episode.")
}
