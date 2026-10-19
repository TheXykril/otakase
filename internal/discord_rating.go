package internal

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/thexykril/otakase/internal/icons"
)

// Sharing a score: after scoring a show on finishing it, a linked app
// offers to share the score in the Otakase Discord, where the bot posts a
// card in the reviews channel. Asked every time unless DiscordShareRatings
// is "never" (the menu's "Don't ask again" sets that). Adult titles are
// never offered.
//
//	POST /v1/rating {"anilist_id":154587,"score":8.5,"text":"…"} → {"url"}

const ratingNoteMax = 300

// ratingShareWanted reports whether to offer sharing this score.
func ratingShareWanted(config *Config, anime *Anime, score float64) bool {
	if config == nil || anime == nil || anime.IsAdult || anime.AnilistId <= 0 {
		return false
	}
	if score < 0 || score > 10 || math.IsNaN(score) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(config.DiscordShareRatings), "never") {
		return false
	}
	if news, ok := cachedDiscordNews(config); ok && news.ScoreSharing != nil && !*news.ScoreSharing {
		return false // turned off on the server
	}
	return !castWindowNonInteractive() && discordLinked(config)
}

type ratingBody struct {
	AnilistID int     `json:"anilist_id"`
	Score     float64 `json:"score"`
	Text      string  `json:"text,omitempty"`
}

// shareRating posts the score and returns the card's link.
func shareRating(config *Config, body ratingBody) (string, error) {
	var answer struct {
		URL string `json:"url"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), discordRequestTimeout)
	defer cancel()
	if _, err := discordPostJSON(ctx, config, "/v1/rating", loadDiscordLink(config).Token, body, &answer); err != nil {
		return "", err
	}
	return answer.URL, nil
}

// offerRatingShare asks whether to share a score just given to a finished
// show, and shares it.
func offerRatingShare(config *Config, anime *Anime, score float64) {
	if !ratingShareWanted(config, anime, score) {
		return
	}
	selected, err := selectWithOptionalMessage(config, []SelectionOption{
		{Key: "share", Label: "Share my score in the Otakase Discord", Icon: icons.Star},
		{Key: "note", Label: "Share it with a few words", Icon: icons.Edit},
		{Key: "no", Label: "Not this time", Icon: icons.No},
		{Key: "never", Label: "Don't ask again", Icon: icons.BellOff},
	}, "Share", fmt.Sprintf("Share your score for %s in the Otakase Discord?", GetAnimeName(*anime)))
	if err != nil {
		Log(fmt.Sprintf("Rating share: %v", err))
		return
	}
	selected = NormalizeSelectionKey(selected)
	body := ratingBody{AnilistID: anime.AnilistId, Score: score}
	switch selected.Key {
	case "share":
	case "note":
		note, cancelled, err := promptCancelable(config, "Share", "A few words about it", "Optional · shown under your score")
		if err != nil || cancelled {
			return
		}
		body.Text = clipRunes(strings.TrimSpace(note), ratingNoteMax)
	case "never":
		config.DiscordShareRatings = "never"
		saveConfigValue(config, "DiscordShareRatings", "never")
		Out("Won't ask again. Set DiscordShareRatings=ask (otakase -e) to be asked again.")
		return
	default:
		return
	}
	if _, err := shareRating(config, body); err != nil {
		Log(fmt.Sprintf("Rating share: %v", err))
		Out("Could not share your score: " + err.Error())
		return
	}
	Out("Shared your score in the Otakase Discord.")
}
