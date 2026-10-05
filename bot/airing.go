package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Each day at AiringHour (UTC) the bot posts the episodes airing in the next
// 24 hours to the anime channel, most popular first. Times are Discord
// timestamps, so everyone sees their own time zone.

const airingQuery = `query ($from: Int, $to: Int, $page: Int) {
  Page(page: $page, perPage: 50) {
    pageInfo { hasNextPage }
    airingSchedules(airingAt_greater: $from, airingAt_lesser: $to, sort: TIME) {
      airingAt episode
      media { siteUrl isAdult popularity format countryOfOrigin title { romaji english } episodes }
    }
  }
}`

type airing struct {
	At         int64
	Episode    int
	Episodes   int
	Title      string
	URL        string
	Popularity int
}

// airingTitle marks the date the post covers, so a restart doesn't post twice.
func airingTitle(day time.Time) string {
	return "Airing today · " + day.Format("Mon 2 Jan")
}

func fetchAiring(from, to time.Time) ([]airing, error) {
	var list []airing
	for page := 1; page <= 4; page++ {
		body, _ := json.Marshal(map[string]any{"query": airingQuery,
			"variables": map[string]any{"from": from.Unix(), "to": to.Unix(), "page": page}})
		req, _ := http.NewRequest("POST", "https://graphql.anilist.co", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		var r struct {
			Data struct {
				Page struct {
					PageInfo struct {
						HasNextPage bool `json:"hasNextPage"`
					} `json:"pageInfo"`
					AiringSchedules []struct {
						AiringAt int64 `json:"airingAt"`
						Episode  int   `json:"episode"`
						Media    struct {
							SiteURL         string `json:"siteUrl"`
							IsAdult         bool   `json:"isAdult"`
							Popularity      int    `json:"popularity"`
							Format          string `json:"format"`
							CountryOfOrigin string `json:"countryOfOrigin"`
							Episodes        *int   `json:"episodes"`
							Title           struct{ Romaji, English string }
						} `json:"media"`
					} `json:"airingSchedules"`
				} `json:"Page"`
			} `json:"data"`
		}
		if err := getJSON(req, &r); err != nil {
			return nil, err
		}
		for _, a := range r.Data.Page.AiringSchedules {
			m := a.Media
			if m.IsAdult || m.CountryOfOrigin != "JP" {
				continue
			}
			title := m.Title.English
			if title == "" {
				title = m.Title.Romaji
			}
			eps := 0
			if m.Episodes != nil {
				eps = *m.Episodes
			}
			list = append(list, airing{At: a.AiringAt, Episode: a.Episode, Episodes: eps, Title: title, URL: m.SiteURL, Popularity: m.Popularity})
		}
		if !r.Data.Page.PageInfo.HasNextPage {
			break
		}
	}
	return list, nil
}

// airingEmbed keeps the most popular shows and lists them by time.
func airingEmbed(day time.Time, list []airing) *discordgo.MessageEmbed {
	sort.Slice(list, func(i, j int) bool { return list[i].Popularity > list[j].Popularity })
	if len(list) > 20 {
		list = list[:20]
	}
	sort.Slice(list, func(i, j int) bool { return list[i].At < list[j].At })
	var lines []string
	for _, a := range list {
		ep := fmt.Sprintf("Ep %d", a.Episode)
		if a.Episodes > 0 && a.Episode == a.Episodes {
			ep += " · finale"
		}
		lines = append(lines, fmt.Sprintf("<t:%d:t> [%s](%s) · %s", a.At, clip(a.Title, 60), a.URL, ep))
	}
	desc := strings.Join(lines, "\n")
	if desc == "" {
		desc = "Nothing airing today."
	}
	return &discordgo.MessageEmbed{
		Title:       airingTitle(day),
		Description: clip(desc, 4000),
		Color:       shu,
		Footer:      &discordgo.MessageEmbedFooter{Text: "Times are in your time zone · watch with otakase"},
	}
}

// postedAiring reports whether today's post is already in the channel.
func (b *bot) postedAiring(title string) bool {
	msgs, err := b.s.ChannelMessages(b.cfg.AiringChannel, 30, "", "", "")
	if err != nil {
		return false
	}
	for _, m := range msgs {
		if m.Author != nil && m.Author.ID == b.s.State.User.ID {
			for _, e := range m.Embeds {
				if e.Title == title {
					return true
				}
			}
		}
	}
	return false
}

func (b *bot) postAiring(now time.Time) {
	day := time.Date(now.Year(), now.Month(), now.Day(), b.cfg.AiringHour, 0, 0, 0, time.UTC)
	title := airingTitle(day)
	if b.postedAiring(title) {
		return
	}
	list, err := fetchAiring(day, day.Add(24*time.Hour))
	if err != nil {
		log.Printf("airing: %v", err)
		return
	}
	if _, err := b.s.ChannelMessageSendComplex(b.cfg.AiringChannel, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{airingEmbed(day, list)},
	}); err != nil {
		log.Printf("airing post: %v", err)
	}
}

func (b *bot) airingLoop() {
	if !b.cfg.DailyAiring || b.cfg.AiringChannel == "" {
		return
	}
	posted := ""
	for {
		now := time.Now().UTC()
		if today := now.Format("2006-01-02"); now.Hour() >= b.cfg.AiringHour && posted != today {
			b.postAiring(now)
			posted = today
		}
		time.Sleep(5 * time.Minute)
	}
}
