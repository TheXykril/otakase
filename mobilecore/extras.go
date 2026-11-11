package mobilecore

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/thexykril/otakase/internal/skiptimes"
)

// extrasRequest carries the skiptimes_* and presence_* operations.
type extrasRequest struct {
	Version   int    `json:"version"`
	Operation string `json:"operation"`

	MalID     int     `json:"mal_id"`
	AnilistID int     `json:"anilist_id"`
	Episode   int     `json:"episode"`
	Length    float64 `json:"length"`
	// IntroDB and AnimeSkip choose the optional services for a lookup.
	IntroDB   bool   `json:"introdb"`
	AnimeSkip string `json:"anime_skip"`
	// A series episode known by its TMDB id and season, for skiptimes_show.
	TmdbID int `json:"tmdb_id"`
	Season int `json:"season"`

	// Submit and vote.
	Type      string  `json:"type"`
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	Submitter string  `json:"submitter"`
	ID        string  `json:"id"`
	Up        bool    `json:"up"`

	Presence *presenceRequest `json:"presence"`
}

func isExtrasOperation(op string) bool {
	return strings.HasPrefix(op, "skiptimes_") || strings.HasPrefix(op, "presence_")
}

var skipServices struct {
	sync.Mutex
	byDir map[string]*skiptimes.Service
}

// skipService keeps one service per cache directory, so the TMDB table is
// downloaded once.
func (c *Core) skipService() *skiptimes.Service {
	c.mu.Lock()
	dir := c.cacheDir
	base := c.transport
	c.mu.Unlock()
	skipServices.Lock()
	defer skipServices.Unlock()
	if skipServices.byDir == nil {
		skipServices.byDir = map[string]*skiptimes.Service{}
	}
	s := skipServices.byDir[dir]
	if s == nil {
		s = skiptimes.New(dir, base)
		skipServices.byDir[dir] = s
	}
	return s
}

func (c *Core) executeExtras(ctx context.Context, input string) string {
	var req extrasRequest
	dec := json.NewDecoder(strings.NewReader(input))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return failure("invalid_request", "Invalid JSON request.", false)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return failure("invalid_request", "Expected one JSON object.", false)
	}
	if req.Version != 1 {
		return failure("invalid_request", "Use version 1.", false)
	}
	client := &http.Client{Transport: &requestTransport{ctx: ctx, base: c.transport}, Timeout: requestTimeout}
	switch req.Operation {
	case "skiptimes_lookup":
		if req.Episode <= 0 || req.Episode > 10000 || (req.MalID <= 0 && req.AnilistID <= 0) {
			return failure("invalid_request", "A tracker id and positive episode are required.", false)
		}
		// Leave time to return successful results before the app gives up.
		lookupCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		lookupClient := &http.Client{
			Transport: &requestTransport{ctx: lookupCtx, base: c.transport},
			Timeout:   20 * time.Second,
		}
		spans, _ := c.skipService().Lookup(lookupCtx, lookupClient, skiptimes.Ref{
			MalID: req.MalID, AniListID: req.AnilistID, Episode: req.Episode, Length: req.Length,
		}, skiptimes.Options{IntroDB: req.IntroDB, AnimeSkipClientID: req.AnimeSkip, AllCandidates: true})
		if ctx.Err() != nil {
			return contextFailure(ctx.Err())
		}
		if spans == nil {
			spans = []skiptimes.Span{}
		}
		return encode(response{Version: 1, Data: map[string]any{"spans": spans}})
	case "skiptimes_show":
		if req.TmdbID <= 0 || req.Season < 0 || req.Season > 1000 || req.Episode <= 0 || req.Episode > 100000 {
			return failure("invalid_request", "A TMDB id, season and positive episode are required.", false)
		}
		lookupCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		lookupClient := &http.Client{
			Transport: &requestTransport{ctx: lookupCtx, base: c.transport},
			Timeout:   20 * time.Second,
		}
		spans, _ := c.skipService().LookupShow(lookupClient, req.TmdbID, req.Season, req.Episode, req.Length)
		if ctx.Err() != nil {
			return contextFailure(ctx.Err())
		}
		if spans == nil {
			spans = []skiptimes.Span{}
		}
		return encode(response{Version: 1, Data: map[string]any{"spans": spans}})
	case "skiptimes_submit":
		id, err := c.skipService().Submit(client, skiptimes.Submission{
			MalID: req.MalID, Episode: req.Episode, Type: req.Type, Start: req.Start,
			End: req.End, Length: req.Length, Submitter: req.Submitter,
		})
		if err != nil {
			return failure("rejected", err.Error(), false)
		}
		return encode(response{Version: 1, Data: map[string]any{"id": id}})
	case "skiptimes_vote":
		if err := c.skipService().Vote(client, req.ID, req.Up); err != nil {
			return failure("rejected", err.Error(), false)
		}
		return encode(response{Version: 1, Data: map[string]any{"voted": true}})
	case "presence_set":
		if req.Presence == nil {
			return failure("invalid_request", "Presence details are required.", false)
		}
		if err := c.presence.set(*req.Presence); err != nil {
			return failure("unavailable", err.Error(), true)
		}
		return encode(response{Version: 1, Data: map[string]any{"shown": true}})
	case "presence_clear":
		c.presence.clear()
		return encode(response{Version: 1, Data: map[string]any{"cleared": true}})
	}
	return failure("invalid_request", "Unknown operation.", false)
}
