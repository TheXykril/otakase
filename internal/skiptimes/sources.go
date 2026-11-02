package skiptimes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/thexykril/otakase/internal/aniskip"
)

const (
	aniSkipBase       = "https://api.aniskip.com/v2/skip-times"
	introDBBase       = "https://api.theintrodb.org/v3/media"
	animeSkipEndpoint = "https://api.anime-skip.com/graphql"
)

// aniSkip asks AniSkip, keyed on the MyAnimeList id.
func (s *Service) aniSkip(client *http.Client, ref Ref) ([]Span, error) {
	c := aniskip.New(client)
	c.V2 = s.aniSkipBase
	c.V1 = strings.Replace(s.aniSkipBase, "/v2/", "/v1/", 1)
	entries, err := c.Lookup(ref.MalID, ref.Episode)
	if err != nil {
		return nil, err
	}
	var spans []Span
	for _, e := range aniskip.Fit(entries, ref.Length) {
		kind := strings.TrimPrefix(e.Kind, "mixed-")
		if kind != "op" && kind != "ed" && kind != "recap" {
			continue
		}
		spans = append(spans, Span{Type: kind, Start: e.Start, End: e.End, ID: e.ID})
	}
	return spans, nil
}

// introDB asks TheIntroDB, which files episodes under TMDB show and season
// numbers, so the AniList id is looked up in the anime-lists table first.
func (s *Service) introDB(ctx context.Context, client *http.Client, ref Ref) ([]Span, error) {
	show, ok := s.mapping.Show(ctx, ref.AniListID, wait)
	if !ok {
		return nil, nil
	}
	url := fmt.Sprintf("%s?tmdb_id=%d&season=%d&episode=%d", s.introDBBase, show.ID, show.Season, ref.Episode)
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("theintrodb: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("theintrodb: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return introDBSpans(body, ref.Length), nil
}

type introDBSpan struct {
	StartMS *float64 `json:"start_ms"`
	EndMS   *float64 `json:"end_ms"`
}

// introDBSpans reads TheIntroDB's answer. An opening with no start begins
// with the episode; credits with no end run to its end, which is only known
// when the player said how long the episode is.
func introDBSpans(body []byte, length float64) []Span {
	var answer struct {
		Intro   []introDBSpan `json:"intro"`
		Recap   []introDBSpan `json:"recap"`
		Credits []introDBSpan `json:"credits"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return nil
	}
	var spans []Span
	add := func(kind string, list []introDBSpan, openStart bool) {
		if len(list) == 0 {
			return
		}
		v := list[0]
		start, end := -1.0, -1.0
		switch {
		case v.StartMS != nil:
			start = *v.StartMS / 1000
		case openStart:
			start = 0
		}
		switch {
		case v.EndMS != nil:
			end = *v.EndMS / 1000
		case !openStart && length > 0:
			end = length
		}
		if span := (Span{Type: kind, Start: start, End: end}); span.Usable() {
			spans = append(spans, span)
		}
	}
	add("op", answer.Intro, true)
	add("recap", answer.Recap, true)
	add("ed", answer.Credits, false)
	return spans
}

// animeSkip asks Anime-Skip's GraphQL API.
func (s *Service) animeSkip(client *http.Client, ref Ref, configured string) ([]Span, error) {
	auto := strings.EqualFold(configured, AutoClientID)
	clientID := configured
	if auto {
		var err error
		if clientID, err = s.ids.ClientID(client); err != nil {
			return nil, err
		}
	}
	spans, err := s.animeSkipOnce(client, ref, clientID)
	if auto && isRefusedClientID(err) {
		// A refused id is what a rotated one looks like: fetch the new one
		// and try once more.
		s.ids.Invalidate()
		if clientID, err = s.ids.ClientID(client); err != nil {
			return nil, err
		}
		return s.animeSkipOnce(client, ref, clientID)
	}
	return spans, err
}

func (s *Service) animeSkipOnce(client *http.Client, ref Ref, clientID string) ([]Span, error) {
	var shows struct {
		Data struct {
			Shows []struct {
				ID string `json:"id"`
			} `json:"findShowsByExternalId"`
		} `json:"data"`
	}
	err := s.graphQL(client, clientID, `query ($id: String!) { findShowsByExternalId(service: ANILIST, serviceId: $id) { id } }`,
		map[string]any{"id": strconv.Itoa(ref.AniListID)}, &shows)
	if err != nil || len(shows.Data.Shows) == 0 {
		return nil, err
	}
	var episodes struct {
		Data struct {
			Episodes []struct {
				Number     string          `json:"number"`
				Timestamps []animeSkipMark `json:"timestamps"`
			} `json:"findEpisodesByShowId"`
		} `json:"data"`
	}
	err = s.graphQL(client, clientID, `query ($showId: ID!) { findEpisodesByShowId(showId: $showId) { number timestamps { at type { name } } } }`,
		map[string]any{"showId": shows.Data.Shows[0].ID}, &episodes)
	if err != nil {
		return nil, err
	}
	for _, ep := range episodes.Data.Episodes {
		if n, err := strconv.Atoi(strings.TrimSpace(ep.Number)); err == nil && n == ref.Episode {
			return animeSkipSpans(ep.Timestamps, ref.Length), nil
		}
	}
	return nil, nil
}

type animeSkipMark struct {
	At   float64 `json:"at"`
	Type struct {
		Name string `json:"name"`
	} `json:"type"`
}

// animeSkipSpans pairs Anime-Skip's markers into spans: a marker says where
// something begins and the next marker, of any kind, where it ends. The last
// marker runs to the end of the episode when its length is known.
func animeSkipSpans(marks []animeSkipMark, length float64) []Span {
	sorted := append([]animeSkipMark(nil), marks...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].At < sorted[j].At })
	var spans []Span
	seen := map[string]bool{}
	for i, m := range sorted {
		end := length
		if i+1 < len(sorted) {
			end = sorted[i+1].At
		}
		name := strings.ToLower(m.Type.Name)
		kind := ""
		switch {
		case strings.Contains(name, "intro"):
			kind = "op"
		case strings.Contains(name, "recap"):
			kind = "recap"
		case strings.Contains(name, "credits") || strings.Contains(name, "outro"):
			kind = "ed"
		}
		span := Span{Type: kind, Start: m.At, End: end}
		if kind == "" || seen[kind] || !span.Usable() {
			continue
		}
		seen[kind] = true
		spans = append(spans, span)
	}
	return spans
}

type refusedClientID struct{ message string }

func (e refusedClientID) Error() string { return "anime-skip: " + e.message }

func isRefusedClientID(err error) bool {
	_, ok := err.(refusedClientID)
	return ok
}

func (s *Service) graphQL(client *http.Client, clientID, query string, variables map[string]any, out any) error {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, s.animeSkipBase, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-ID", clientID)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("anime-skip: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var failure struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(body, &failure)
	message := ""
	if len(failure.Errors) > 0 {
		message = failure.Errors[0].Message
	}
	lower := strings.ToLower(message)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden ||
		strings.Contains(lower, "client id") || strings.Contains(lower, "client-id") {
		if message == "" {
			message = fmt.Sprintf("status %d", resp.StatusCode)
		}
		return refusedClientID{message}
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("anime-skip: status %d", resp.StatusCode)
	}
	if message != "" {
		return fmt.Errorf("anime-skip: %s", message)
	}
	return json.Unmarshal(body, out)
}

// Submission is a span a viewer marked, to send to AniSkip.
type Submission struct {
	MalID   int
	Episode int
	Type    string
	Start   float64
	End     float64
	Length  float64
	// Submitter is the viewer's random AniSkip submitter id.
	Submitter string
}

// Shortest and longest span AniSkip is sent: shorter is a mis-tap, longer is
// not an opening or ending.
const (
	minimumSpan = 5.0
	maximumSpan = 10 * 60.0
)

// Validate says what is wrong with a submission, in words for the viewer.
func (s Submission) Validate() error {
	switch {
	case s.MalID <= 0:
		return fmt.Errorf("this show has no MyAnimeList id, which AniSkip files times under")
	case s.Episode <= 0:
		return fmt.Errorf("no episode number")
	case s.Type != "op" && s.Type != "ed" && s.Type != "recap" && s.Type != "mixed-op" && s.Type != "mixed-ed":
		return fmt.Errorf("pick opening, ending or recap")
	case s.Length <= 0:
		return fmt.Errorf("the episode length is not known yet")
	case s.Start < 0 || s.End <= s.Start:
		return fmt.Errorf("the end must come after the start")
	case s.End > s.Length+1:
		return fmt.Errorf("the end is past the end of the episode")
	case s.End-s.Start < minimumSpan:
		return fmt.Errorf("that is too short for an opening or ending")
	case s.End-s.Start > maximumSpan:
		return fmt.Errorf("that is too long for an opening or ending")
	case !validSubmitter(s.Submitter):
		return fmt.Errorf("no submitter id")
	}
	return nil
}

func validSubmitter(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, r := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
		} else if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

// Submit sends a marked span to AniSkip and returns its new id.
func (s *Service) Submit(client *http.Client, sub Submission) (string, error) {
	if err := sub.Validate(); err != nil {
		return "", err
	}
	payload, _ := json.Marshal(map[string]any{
		"skipType":      sub.Type,
		"providerName":  "otakase",
		"startTime":     round3(sub.Start),
		"endTime":       round3(sub.End),
		"episodeLength": round3(sub.Length),
		"submitterId":   sub.Submitter,
	})
	answer, err := s.post(client, fmt.Sprintf("%s/%d/%d", s.aniSkipBase, sub.MalID, sub.Episode), payload)
	return answer.SkipID, err
}

// Vote marks an AniSkip span as right (up) or wrong (down).
func (s *Service) Vote(client *http.Client, id string, up bool) error {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 64 || strings.ContainsAny(id, "/?#") {
		return fmt.Errorf("no skip time to vote on")
	}
	vote := "downvote"
	if up {
		vote = "upvote"
	}
	payload, _ := json.Marshal(map[string]string{"voteType": vote})
	_, err := s.post(client, s.aniSkipBase+"/vote/"+id, payload)
	return err
}

type writeAnswer struct {
	Message string `json:"message"`
	SkipID  string `json:"skipId"`
}

func (s *Service) post(client *http.Client, url string, payload []byte) (writeAnswer, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return writeAnswer{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return writeAnswer{}, fmt.Errorf("aniskip: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var answer writeAnswer
	_ = json.Unmarshal(body, &answer)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if m := strings.TrimSpace(answer.Message); m != "" && len(m) < 200 {
			return answer, fmt.Errorf("AniSkip said: %s", m)
		}
		return answer, fmt.Errorf("AniSkip answered %d", resp.StatusCode)
	}
	return answer, nil
}

func round3(v float64) float64 { return float64(int64(v*1000+0.5)) / 1000 }
