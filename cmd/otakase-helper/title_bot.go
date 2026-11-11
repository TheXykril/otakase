package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/thexykril/otakase/internal"
	"github.com/thexykril/otakase/mobilecore"
)

// -title-stream-info finds a film or a series episode for the watch room,
// the way the app plays one: vidsrc by IMDb id, with English subtitles.
// The subtitles come back inline as data: addresses, because the helper's
// temporary folder is gone by the time the bot serves them.

var titleArg = regexp.MustCompile(`^(tt\d{5,10})(?::(\d{1,4}):(\d{1,5}))?$`)

// parseTitleArg reads tt0903747 or tt0903747:1:2 (season, episode).
func parseTitleArg(arg string) (imdb string, season, episode int, err error) {
	m := titleArg.FindStringSubmatch(strings.TrimSpace(arg))
	if m == nil {
		return "", 0, 0, fmt.Errorf("-title-stream-info wants tt<id> or tt<id>:<season>:<episode>, got %q", arg)
	}
	if m[2] != "" {
		season, _ = strconv.Atoi(m[2])
		episode, _ = strconv.Atoi(m[3])
		if season < 0 || episode < 1 {
			return "", 0, 0, fmt.Errorf("-title-stream-info wants a season and an episode from 1, got %q", arg)
		}
	}
	return m[1], season, episode, nil
}

// titleResolve asks the app's core, a variable for tests.
var titleResolve = func(request string) string {
	core := mobilecore.NewCore()
	defer core.Close()
	dir, err := os.MkdirTemp("", "otakase-title")
	if err == nil {
		defer os.RemoveAll(dir)
		core.SetCacheDir(dir)
	}
	return core.Result(core.Start(request))
}

func titleStreamInfo(arg string) (internal.StreamInfo, error) {
	imdb, season, episode, err := parseTitleArg(arg)
	if err != nil {
		return internal.StreamInfo{}, err
	}
	series := episode > 0
	request, _ := json.Marshal(map[string]any{
		"version":   1,
		"operation": "movie_resolve",
		"movie":     map[string]any{"provider": "vidsrc", "path": imdb, "imdb": imdb, "series": series},
		"season":    season,
		"episode":   episode,
		"subtitles": "english",
	})
	var answer struct {
		Data struct {
			Stream struct {
				URL       string            `json:"url"`
				Headers   map[string]string `json:"headers"`
				Referrer  string            `json:"referrer"`
				HLS       bool              `json:"hls"`
				Subtitles []struct {
					URL      string `json:"url"`
					Language string `json:"language"`
					Label    string `json:"label"`
				} `json:"subtitles"`
			} `json:"stream"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(titleResolve(string(request))), &answer); err != nil {
		return internal.StreamInfo{}, err
	}
	if answer.Error != nil {
		return internal.StreamInfo{}, errors.New(strings.TrimSuffix(answer.Error.Message, "."))
	}
	s := answer.Data.Stream
	if s.URL == "" {
		return internal.StreamInfo{}, errors.New("no server played this title")
	}
	link := internal.StreamLink{URL: s.URL, Referrer: s.Referrer, Headers: s.Headers, HLS: s.HLS}
	for _, sub := range s.Subtitles {
		inline := inlineSubtitle(sub.URL)
		if inline == "" {
			continue
		}
		link.Subtitles = append(link.Subtitles, internal.StreamSubtitle{URL: inline, Language: languageCode(sub.Language), Label: sub.Label})
	}
	return internal.StreamInfo{Episode: max(episode, 1), Provider: "vidsrc", ProviderID: imdb, Mode: "sub", Streams: []internal.StreamLink{link}}, nil
}

// inlineSubtitle turns a saved subtitle file into a data: address. Web
// addresses pass as they are.
func inlineSubtitle(path string) string {
	if strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "http://") {
		return path
	}
	body, err := os.ReadFile(path)
	if err != nil || len(body) == 0 || len(body) > 2<<20 {
		return ""
	}
	return "data:text/plain;base64," + base64.StdEncoding.EncodeToString(body)
}

func languageCode(name string) string {
	switch strings.ToLower(name) {
	case "english":
		return "en"
	case "":
		return ""
	}
	return strings.ToLower(name)
}
