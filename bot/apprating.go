package main

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Shared scores: when a member finishes a show in the app and scores it, the
// app offers to share the score, and the bot posts a card in the reviews
// channel with the cover, the score, the member (mentioned, not pinged) and
// Play in Otakase and Queue in Otakase buttons. Adult titles are never
// posted.
//
//	POST /v1/rating {"anilist_id":154587,"score":8.5,"text":"…"} (auth) → {"url":"…"}
//
// score is out of 10; one above 10 is taken as out of 100.

const ratingTextMax = 1000

var (
	ratingLimit = newRateLimit(5, time.Hour)
	// ratingsShared stops the same show being shared twice in a day by one
	// member, by "user/id".
	ratingsShared = struct {
		sync.Mutex
		m map[string]time.Time
	}{m: map[string]time.Time{}}
)

func init() {
	apiHandle("POST /v1/rating", (*bot).apiRating)
}

// normalScore is a score out of 10, rounded to a half, and whether it was
// one at all.
func normalScore(s float64) (float64, bool) {
	if math.IsNaN(s) || s < 0 || s > 100 {
		return 0, false
	}
	if s > 10 {
		s /= 10
	}
	return math.Round(s*2) / 2, true
}

// scoreStars draws a score out of 10 as five stars.
func scoreStars(s float64) string {
	full := int(math.Round(s / 2))
	return strings.Repeat("★", full) + strings.Repeat("☆", 5-full)
}

func (b *bot) apiRating(w http.ResponseWriter, r *http.Request) {
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	var req struct {
		AniListID int     `json:"anilist_id"`
		Score     float64 `json:"score"`
		Text      string  `json:"text"`
	}
	if !readJSON(w, r, 8<<10, &req) {
		return
	}
	score, ok := normalScore(req.Score)
	if req.AniListID <= 0 || req.AniListID > 10_000_000 || !ok {
		apiError(w, http.StatusBadRequest, "bad show or score")
		return
	}
	text := clip(cleanText(req.Text, true), ratingTextMax)
	key := fmt.Sprintf("%s/%d", user, req.AniListID)
	now := appNow()
	ratingsShared.Lock()
	last, dup := ratingsShared.m[key]
	ratingsShared.Unlock()
	if dup && now.Sub(last) < 24*time.Hour {
		apiError(w, http.StatusConflict, "you already shared a score for this show today")
		return
	}
	if !ratingLimit.allow(user, now) {
		apiError(w, http.StatusTooManyRequests, "you can share 5 scores an hour, try again later")
		return
	}
	info, found := appShowInfo(req.AniListID)
	if !found {
		apiError(w, http.StatusNotFound, "couldn't find that show on AniList")
		return
	}
	info.ID = req.AniListID
	if info.IsAdult {
		apiError(w, http.StatusForbidden, "scores for this show aren't shared")
		return
	}
	if !b.settingBool("score_sharing") {
		apiError(w, http.StatusServiceUnavailable, "score sharing is turned off on the server")
		return
	}
	if b.s == nil {
		apiError(w, http.StatusServiceUnavailable, "the server isn't reachable right now")
		return
	}
	channel := b.reviewsChannel()
	if channel == "" {
		apiError(w, http.StatusServiceUnavailable, "the server has no reviews channel")
		return
	}
	m, err := b.s.ChannelMessageSendComplex(channel, ratingMessage(user, info, score, text))
	if err != nil {
		log.Printf("rating card: %v", err)
		apiError(w, http.StatusBadGateway, "couldn't post the score, try again later")
		return
	}
	ratingsShared.Lock()
	if len(ratingsShared.m) > 5000 {
		ratingsShared.m = map[string]time.Time{}
	}
	ratingsShared.m[key] = now
	ratingsShared.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"url": fmt.Sprintf("https://discord.com/channels/%s/%s/%s", b.cfg.GuildID, channel, m.ID)})
}

// reviewsChannel is a text channel named reviews (or review), "" when the
// server has none.
func (b *bot) reviewsChannel() string {
	chans, err := b.s.GuildChannels(b.cfg.GuildID)
	if err != nil {
		log.Printf("listing channels: %v", err)
		return ""
	}
	var review string
	for _, c := range chans {
		if c.Type != discordgo.ChannelTypeGuildText {
			continue
		}
		switch strings.ToLower(channelBase(c.Name)) {
		case "reviews":
			return c.ID
		case "review", "ratings":
			review = c.ID
		}
	}
	return review
}

// ratingMessage is the card for a shared score.
func ratingMessage(user string, info airingShowInfo, score float64, text string) *discordgo.MessageSend {
	title := info.Title.English
	if title == "" {
		title = info.Title.Romaji
	}
	desc := fmt.Sprintf("<@%s> finished it in Otakase and scored it\n# %s/10\n%s", user, scoreText(score), scoreStars(score))
	if text != "" {
		desc += "\n\n>>> " + quiet(text)
	}
	e := &discordgo.MessageEmbed{
		Title:       clip(title, 250),
		URL:         info.SiteURL,
		Description: desc,
		Color:       shu,
		Footer:      &discordgo.MessageEmbedFooter{Text: "Shared from the Otakase app"},
	}
	if info.CoverImage.ExtraLarge != "" {
		e.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: info.CoverImage.ExtraLarge}
	}
	msg := &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{e}, AllowedMentions: &discordgo.MessageAllowedMentions{}}
	if row := withPlayButton(nil, info.ID, 0); len(row) > 0 {
		msg.Components = []discordgo.MessageComponent{discordgo.ActionsRow{Components: row}}
	}
	return msg
}

// scoreText is 8 or 8.5.
func scoreText(s float64) string {
	if s == math.Trunc(s) {
		return fmt.Sprintf("%d", int(s))
	}
	return fmt.Sprintf("%.1f", s)
}
