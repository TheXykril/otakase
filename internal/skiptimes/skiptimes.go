// Package skiptimes finds opening, ending and recap times for an episode in
// every skip time service the app knows: AniSkip, TheIntroDB and Anime-Skip.
// It also sends times a viewer marked back to AniSkip and votes on them.
//
// The services are asked at once, and for each kind of span the first
// service in that order with a usable answer wins: AniSkip times are timed
// against the episode length and can be voted on, TheIntroDB is next, and
// Anime-Skip, which needs a client id, is last.
package skiptimes

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Ref names the episode to look up.
type Ref struct {
	MalID     int
	AniListID int
	Episode   int
	// Length is the episode's length in seconds, zero when unknown.
	Length float64
}

// Span is one stretch to skip.
type Span struct {
	// Type is op, ed or recap.
	Type   string  `json:"type"`
	Start  float64 `json:"start"`
	End    float64 `json:"end"`
	Source string  `json:"source"`
	// ID is set on AniSkip spans; it is what a vote names.
	ID string `json:"id,omitempty"`
}

// Usable reports whether the span can be skipped.
func (s Span) Usable() bool {
	return s.Start >= 0 && s.End > s.Start && s.End-s.Start >= 1
}

// Source names, in the order a span is taken from.
const (
	AniSkip   = "aniskip"
	IntroDB   = "theintrodb"
	AnimeSkip = "anime-skip"
)

var order = []string{AniSkip, IntroDB, AnimeSkip}

// Options choose which services are asked.
type Options struct {
	IntroDB bool
	// AllCandidates preserves alternatives when the app also has provider markers.
	AllCandidates bool
	// AnimeSkipClientID is the Anime-Skip client id: empty skips Anime-Skip,
	// "auto" uses the id Anime-Skip publishes for its API playground.
	AnimeSkipClientID string
}

// AutoClientID asks for the id Anime-Skip publishes on its API page.
const AutoClientID = "auto"

// Service keeps what lookups learn between episodes: the AniList to TMDB
// table TheIntroDB needs and the Anime-Skip client id.
type Service struct {
	dir     string
	mapping *tmdbMapping
	ids     *autoClientID

	// Endpoints, replaced in tests.
	aniSkipBase, introDBBase, animeSkipBase string
}

// New keeps its files in dir, or in the user cache directory when dir is
// empty. base carries the background download of the TMDB table; nil is
// the default transport.
func New(dir string, base http.RoundTripper) *Service {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		if cache, err := os.UserCacheDir(); err == nil {
			dir = filepath.Join(cache, "otakase")
		} else {
			dir = filepath.Join(os.TempDir(), "otakase")
		}
	}
	return &Service{
		dir:           dir,
		mapping:       newTMDBMapping(filepath.Join(dir, "tmdb_mapping.json"), animeListsURL, base),
		ids:           newAutoClientID(filepath.Join(dir, "anime_skip_client_id"), animeSkipPlaygroundURL),
		aniSkipBase:   aniSkipBase,
		introDBBase:   introDBBase,
		animeSkipBase: animeSkipEndpoint,
	}
}

// Lookup asks every service at once and merges their answers. Errors are
// returned for the log only: a service that failed simply has no times.
func (s *Service) Lookup(ctx context.Context, client *http.Client, ref Ref, opts Options) ([]Span, []error) {
	if ref.Episode <= 0 {
		return nil, nil
	}
	type answer struct {
		spans []Span
		err   error
	}
	answers := make(map[string]answer, len(order))
	var mu sync.Mutex
	var wg sync.WaitGroup
	ask := func(name string, lookup func() ([]Span, error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			spans, err := lookup()
			mu.Lock()
			answers[name] = answer{spans, err}
			mu.Unlock()
		}()
	}
	if ref.MalID > 0 {
		ask(AniSkip, func() ([]Span, error) { return s.aniSkip(client, ref) })
	}
	if opts.IntroDB && ref.AniListID > 0 {
		ask(IntroDB, func() ([]Span, error) { return s.introDB(ctx, client, ref) })
	}
	if id := strings.TrimSpace(opts.AnimeSkipClientID); id != "" && ref.AniListID > 0 {
		ask(AnimeSkip, func() ([]Span, error) { return s.animeSkip(client, ref, id) })
	}
	wg.Wait()

	var spans []Span
	var errs []error
	taken := map[string]bool{}
	for _, name := range order {
		a, ok := answers[name]
		if !ok {
			continue
		}
		if a.err != nil && !errors.Is(a.err, context.Canceled) {
			errs = append(errs, a.err)
		}
		if opts.AllCandidates {
			clear(taken)
		}
		for _, span := range a.spans {
			span.Source = name
			if taken[span.Type] || !span.Usable() || !fits(span, ref.Length) {
				continue
			}
			taken[span.Type] = true
			spans = append(spans, span)
		}
	}
	return spans, errs
}

// fits drops a span that runs past the end of the episode being played.
func fits(span Span, length float64) bool {
	return length <= 0 || span.End <= length+1
}

// wait is how long a lookup waits for the TMDB table on first use.
var wait = 15 * time.Second
