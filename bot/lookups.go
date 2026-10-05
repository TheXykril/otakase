package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

func getJSON(req *http.Request, out any) error {
	req.Header.Set("User-Agent", "otakase-bot (https://github.com/TheXykril/otakase)")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", req.URL.Host, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func githubRequest(method, url, token string) *http.Request {
	req, _ := http.NewRequest(method, url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func latestRelease(token string) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	var r struct {
		TagName     string    `json:"tag_name"`
		HTMLURL     string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
	}
	if err := getJSON(githubRequest("GET", "https://api.github.com/repos/TheXykril/otakase/releases/latest", token), &r); err != nil {
		return errorEmbed("Couldn't reach GitHub right now. Releases: %s/releases", repoURL), nil
	}
	v := strings.TrimPrefix(r.TagName, "v")
	return &discordgo.MessageEmbed{
			Title:       "Otakase " + v,
			URL:         r.HTMLURL,
			Description: fmt.Sprintf("Released <t:%d:D>. Update with `otakase -u`, or see `/install`.", r.PublishedAt.Unix()),
			Color:       shu,
		}, linkButtons("Download", r.HTMLURL, "Changelog",
			fmt.Sprintf("%s/blob/%s/CHANGELOG.md", repoURL, r.TagName))
}

const anilistQuery = `query ($q: String) {
  Media(search: $q, type: ANIME, isAdult: false) {
    siteUrl episodes status averageScore genres seasonYear format
    title { romaji english }
    coverImage { large color }
    description(asHtml: false)
  }
}`

var tagRe = regexp.MustCompile(`<[^>]+>`)

var statusNames = map[string]string{
	"FINISHED": "Finished", "RELEASING": "Airing", "NOT_YET_RELEASED": "Not yet aired",
	"CANCELLED": "Cancelled", "HIATUS": "On hiatus",
}

func searchAnime(q string) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	body, _ := json.Marshal(map[string]any{"query": anilistQuery, "variables": map[string]string{"q": q}})
	req, _ := http.NewRequest("POST", "https://graphql.anilist.co", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	var r struct {
		Data struct {
			Media *struct {
				SiteURL      string   `json:"siteUrl"`
				Episodes     *int     `json:"episodes"`
				Status       string   `json:"status"`
				AverageScore *int     `json:"averageScore"`
				Genres       []string `json:"genres"`
				SeasonYear   *int     `json:"seasonYear"`
				Format       string   `json:"format"`
				Title        struct{ Romaji, English string }
				CoverImage   struct{ Large, Color string } `json:"coverImage"`
				Description  string
			} `json:"Media"`
		} `json:"data"`
	}
	if err := getJSON(req, &r); err != nil || r.Data.Media == nil {
		return errorEmbed("Nothing found on AniList for **%s**.", q), nil
	}
	m := r.Data.Media
	title := m.Title.English
	if title == "" {
		title = m.Title.Romaji
	}
	desc := html.UnescapeString(tagRe.ReplaceAllString(m.Description, ""))
	if len([]rune(desc)) > 350 {
		desc = string([]rune(desc)[:347]) + "…"
	}
	var fields []*discordgo.MessageEmbedField
	add := func(name, value string) {
		if value != "" {
			fields = append(fields, &discordgo.MessageEmbedField{Name: name, Value: value, Inline: true})
		}
	}
	if m.Episodes != nil {
		add("Episodes", fmt.Sprint(*m.Episodes))
	}
	add("Status", statusNames[m.Status])
	if m.AverageScore != nil {
		add("Score", fmt.Sprintf("%d%%", *m.AverageScore))
	}
	if m.SeasonYear != nil {
		add("Year", fmt.Sprint(*m.SeasonYear))
	}
	if len(m.Genres) > 0 {
		add("Genres", strings.Join(m.Genres, ", "))
	}
	e := &discordgo.MessageEmbed{
		Title: title, URL: m.SiteURL, Description: desc, Color: shu, Fields: fields,
		Thumbnail: &discordgo.MessageEmbedThumbnail{URL: m.CoverImage.Large},
		Footer:    &discordgo.MessageEmbedFooter{Text: "Watch it: otakase, then search \"" + title + "\""},
	}
	return e, linkButtons("Open on AniList", m.SiteURL)
}
