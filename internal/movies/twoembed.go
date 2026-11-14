package movies

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// TwoEmbed is 2embed's own player, vidsrc.buzz: films and series by IMDb id,
// in English, on a few servers.
//
// The embed page (/embed/movie/<imdb>, /embed/tv/<imdb>/<season>/<episode>)
// is rendered with a token and its servers, each with a ref. The player asks
// /pl/api.php?a=play for a server's ref and token, and is given the address
// of an HLS playlist on the site. Both the token and the playlist are tied
// to the address that asked, so the page is fetched right before playing.
// The site answers only home connections; from data centres every call says
// "unavailable".
//
// Its own subtitle list needs more than the page gives, so subtitles come
// from OpenSubtitles by IMDb id, as for vidsrc.
type TwoEmbed struct {
	Base   string
	Client *http.Client
	// Subtitles picks the subtitle language; nil is English.
	Subtitles func(Movie) SubtitleLanguage
}

// NewTwoEmbed returns vidsrc.buzz.
func NewTwoEmbed() *TwoEmbed {
	return &TwoEmbed{Base: "https://vidsrc.buzz", Client: &http.Client{Timeout: 25 * time.Second}}
}

func (*TwoEmbed) Name() string  { return TwoEmbedName }
func (*TwoEmbed) Label() string { return "2Embed (English)" }

// Search is not offered: titles are played by IMDb id, which the app has
// from Trakt.
func (*TwoEmbed) Search(string) ([]Movie, error) {
	return nil, fmt.Errorf("2embed plays titles by IMDb id and has no search")
}

func (t *TwoEmbed) Open(movie Movie) (Movie, []Source, error) {
	movie.Provider = TwoEmbedName
	if movie.IMDb == "" {
		movie.IMDb = movie.Path
	}
	sources, err := t.servers(movie, Episode{})
	return movie, sources, err
}

func (t *TwoEmbed) Episodes(Movie) ([]Episode, error) {
	return nil, fmt.Errorf("2embed has no episode list")
}

func (t *TwoEmbed) OpenEpisode(show Movie, episode Episode) ([]Source, error) {
	if show.IMDb == "" {
		show.IMDb = show.Path
	}
	return t.servers(show, episode)
}

var twoEmbedPagePattern = regexp.MustCompile(`var Q = (\{.*?\});\n`)

// twoEmbedPage is the part of the embed page's Q the player uses.
type twoEmbedPage struct {
	Token string `json:"t"`
	SSR   struct {
		Servers []struct {
			Ref  string `json:"ref"`
			Name string `json:"name"`
		} `json:"servers"`
	} `json:"ssr"`
}

type twoEmbedPlay struct {
	URL   string `json:"url"`
	Type  string `json:"type"`
	Error string `json:"error"`
}

func (t *TwoEmbed) embedURL(imdb string, episode Episode) string {
	if episode.Number > 0 {
		return fmt.Sprintf("%s/embed/tv/%s/%d/%d", t.Base, url.PathEscape(imdb), max(episode.Season, 1), episode.Number)
	}
	return t.Base + "/embed/movie/" + url.PathEscape(imdb)
}

func parseTwoEmbedPage(body string) (twoEmbedPage, error) {
	var page twoEmbedPage
	match := twoEmbedPagePattern.FindStringSubmatch(body)
	if match == nil {
		return page, fmt.Errorf("2embed: no player data on the page")
	}
	if err := json.Unmarshal([]byte(match[1]), &page); err != nil {
		return page, fmt.Errorf("2embed: %w", err)
	}
	if page.Token == "" || len(page.SSR.Servers) == 0 {
		return page, fmt.Errorf("2embed has no servers for this title")
	}
	return page, nil
}

func (t *TwoEmbed) servers(movie Movie, episode Episode) ([]Source, error) {
	if !strings.HasPrefix(movie.IMDb, "tt") {
		return nil, fmt.Errorf("2embed needs an IMDb id")
	}
	embed := t.embedURL(movie.IMDb, episode)
	body, err := fetchPage(t.Client, embed, "")
	if err != nil {
		return nil, err
	}
	page, err := parseTwoEmbedPage(body)
	if err != nil {
		return nil, err
	}
	name := movie.IMDb
	if episode.Number > 0 {
		name = fmt.Sprintf("%s-s%de%d", movie.IMDb, episode.Season, episode.Number)
	}
	var subtitlesOnce sync.Once
	var found []Subtitle
	subtitles := func() []Subtitle {
		subtitlesOnce.Do(func() {
			language := English
			if t.Subtitles != nil {
				language = t.Subtitles(movie)
			}
			found = openSubtitles(t.Client, openSubtitlesQuery(movie.IMDb, episode), "2embed-"+name, "", language)
		})
		return found
	}
	sources := []Source{}
	for i, server := range page.SSR.Servers {
		label := fmt.Sprintf("2Embed %d", i+1)
		ref := server.Ref
		sources = append(sources, Source{Server: label, Resolve: func() (Stream, error) {
			stream, err := t.play(embed, ref, page.Token)
			if err != nil {
				return Stream{}, err
			}
			stream.Server = label
			stream.Subtitles = subtitles()
			return stream, nil
		}})
	}
	return sources, nil
}

// play asks for one server's playlist, as the player does: from its /pl/
// folder, with the embed page as referrer.
func (t *TwoEmbed) play(embed, ref, token string) (Stream, error) {
	api := t.Base + "/pl/api.php?a=play&ref=" + url.QueryEscape(ref) + "&t=" + url.QueryEscape(token)
	body, err := fetchPage(t.Client, api, embed)
	if err != nil {
		return Stream{}, err
	}
	var answer twoEmbedPlay
	if err := json.Unmarshal([]byte(body), &answer); err != nil {
		return Stream{}, fmt.Errorf("2embed: %w", err)
	}
	if answer.URL == "" {
		return Stream{}, fmt.Errorf("2embed server has no stream (%s)", answer.Error)
	}
	base, _ := url.Parse(t.Base + "/pl/")
	link, err := base.Parse(answer.URL)
	if err != nil {
		return Stream{}, err
	}
	hls := answer.Type == "" || answer.Type == "hls"
	return Stream{URL: link.String(), Referrer: embed, HLS: hls}, nil
}
