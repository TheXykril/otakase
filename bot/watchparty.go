package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"
)

// /watchparty creates a server event in a voice channel for watching a show
// together, with the show's AniList banner as the event image.

const watchpartyQuery = `query ($q: String) {
  Media(search: $q, type: ANIME, isAdult: false) {
    siteUrl bannerImage coverImage { extraLarge }
    title { romaji english }
  }
}`

type partyShow struct {
	Title string
	URL   string
	Image string
}

func findPartyShow(q string) (partyShow, error) {
	body, _ := json.Marshal(map[string]any{"query": watchpartyQuery, "variables": map[string]string{"q": q}})
	req, _ := http.NewRequest("POST", "https://graphql.anilist.co", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	var r struct {
		Data struct {
			Media *struct {
				SiteURL     string `json:"siteUrl"`
				BannerImage string `json:"bannerImage"`
				CoverImage  struct {
					ExtraLarge string `json:"extraLarge"`
				} `json:"coverImage"`
				Title struct{ Romaji, English string }
			} `json:"Media"`
		} `json:"data"`
	}
	if err := getJSON(req, &r); err != nil {
		return partyShow{}, err
	}
	m := r.Data.Media
	if m == nil {
		return partyShow{Title: q}, nil
	}
	p := partyShow{Title: m.Title.English, URL: m.SiteURL, Image: m.BannerImage}
	if p.Title == "" {
		p.Title = m.Title.Romaji
	}
	if p.Image == "" {
		p.Image = m.CoverImage.ExtraLarge
	}
	return p, nil
}

// imageDataURI downloads an image for the event cover. Failures just leave
// the event without one.
func imageDataURI(url string) string {
	if url == "" {
		return ""
	}
	req, _ := http.NewRequest("GET", url, nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil || resp.StatusCode != 200 {
		return ""
	}
	return "data:" + http.DetectContentType(data) + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func (b *bot) watchparty(i *discordgo.InteractionCreate) {
	d := i.ApplicationCommandData()
	title, minutes, channel := "", int64(15), b.cfg.LoungeChannel
	for _, o := range d.Options {
		switch o.Name {
		case "anime":
			title = o.StringValue()
		case "starts_in":
			minutes = o.IntValue()
		case "voice":
			channel = o.ChannelValue(nil).ID
		}
	}
	if channel == "" {
		b.respond(i, true, errorEmbed("Pick a voice channel for the watch party."))
		return
	}
	b.deferThen(i, false, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		show, err := findPartyShow(title)
		if err != nil {
			show = partyShow{Title: title}
		}
		start := time.Now().Add(time.Duration(minutes) * time.Minute)
		if minutes < 1 {
			start = time.Now().Add(time.Minute) // events must start in the future
		}
		host := i.Member.User
		desc := fmt.Sprintf("Watch party hosted by %s. Join the voice channel and watch along with otakase.", host.GlobalName)
		if host.GlobalName == "" {
			desc = fmt.Sprintf("Watch party hosted by %s. Join the voice channel and watch along with otakase.", host.Username)
		}
		if show.URL != "" {
			desc += "\n" + show.URL
		}
		ev, err := b.s.GuildScheduledEventCreate(b.cfg.GuildID, &discordgo.GuildScheduledEventParams{
			ChannelID:          channel,
			Name:               clip("Watch party: "+show.Title, 100),
			Description:        desc,
			ScheduledStartTime: &start,
			PrivacyLevel:       discordgo.GuildScheduledEventPrivacyLevelGuildOnly,
			EntityType:         discordgo.GuildScheduledEventEntityTypeVoice,
			Image:              imageDataURI(show.Image),
		})
		if err != nil {
			log.Printf("watchparty: %v", err)
			return errorEmbed("Couldn't create the event. The bot needs the **Create Events** permission."), nil
		}
		link := fmt.Sprintf("https://discord.com/events/%s/%s", b.cfg.GuildID, ev.ID)
		e := &discordgo.MessageEmbed{
			Title:       "Watch party: " + show.Title,
			URL:         link,
			Description: fmt.Sprintf("<@%s> is hosting in <#%s>, starting <t:%d:R>. Press **Interested** on the event to get a reminder.", host.ID, channel, start.Unix()),
			Color:       shu,
		}
		if show.Image != "" {
			e.Image = &discordgo.MessageEmbedImage{URL: show.Image}
		}
		return e, linkButtons("Open event", link)
	})
}
