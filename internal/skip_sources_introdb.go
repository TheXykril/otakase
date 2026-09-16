package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	introDBEndpoint = "https://api.theintrodb.org/v3/media"
	introDBTimeout  = 15 * time.Second
)

// introDBSource asks theintrodb.org, which files openings and endings under
// TMDB ids.
//
// It knows a great deal, including anime, but it is reached through an id this
// program does not have: everything here is keyed on AniList and MyAnimeList,
// and TMDB is a third naming of the same shows. That is what the mapping table
// is for, and why this source sits late in the chain -- the ones that answer to
// ids already in hand are cheaper.
type introDBSource struct {
	mapping  *tmdbMapping
	endpoint string
	http     *http.Client
}

func newIntroDBSource(storagePath string) introDBSource {
	return introDBSource{
		mapping:  tmdbMappingFor(storagePath),
		endpoint: introDBEndpoint,
		http:     &http.Client{Timeout: introDBTimeout},
	}
}

func (introDBSource) Name() string { return "theintrodb" }

func (s introDBSource) Lookup(ref SkipRef) (SkipTimes, bool, error) {
	if s.mapping == nil || ref.AniListID <= 0 || ref.Episode <= 0 {
		return SkipTimes{}, false, nil
	}
	show, ok := s.mapping.Show(ref.AniListID)
	if !ok {
		// Either this show has no TMDB id or the table is still being built.
		// Both are "cannot answer", not failures.
		return SkipTimes{}, false, nil
	}

	url := fmt.Sprintf("%s?tmdb_id=%d&season=%d&episode=%d", s.endpoint, show.ID, show.Season, ref.Episode)
	resp, err := s.http.Get(url)
	if err != nil {
		return SkipTimes{}, false, fmt.Errorf("theintrodb: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return SkipTimes{}, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return SkipTimes{}, false, fmt.Errorf("theintrodb: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return SkipTimes{}, false, err
	}
	times := introDBTimes(body)
	return times, usableSpan(times.Op) || usableSpan(times.Ed), nil
}

// introDBSpan is one marked stretch. Either end can be missing: an opening that
// begins the moment the episode does has no start, and credits that run to the
// end have no end.
type introDBSpan struct {
	StartMS *float64 `json:"start_ms"`
	EndMS   *float64 `json:"end_ms"`
}

func introDBTimes(body []byte) SkipTimes {
	var answer struct {
		Intro   []introDBSpan `json:"intro"`
		Credits []introDBSpan `json:"credits"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return SkipTimes{}
	}

	times := SkipTimes{}
	if len(answer.Intro) > 0 {
		// A missing start means the opening is the first thing in the episode.
		times.Op = introDBSkip(answer.Intro[0], true)
	}
	if len(answer.Credits) > 0 {
		// A missing end would mean skipping to nowhere: the episode length is
		// not in this answer, so credits that run to the end are left alone.
		times.Ed = introDBSkip(answer.Credits[0], false)
	}
	return times
}

func introDBSkip(span introDBSpan, startAtZero bool) Skip {
	if span.EndMS == nil {
		return Skip{}
	}
	start := 0.0
	switch {
	case span.StartMS != nil:
		start = *span.StartMS
	case !startAtZero:
		return Skip{}
	}
	return Skip{
		Start: int(start/1000 + 0.5),
		End:   int(*span.EndMS/1000 + 0.5),
	}
}
