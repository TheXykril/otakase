package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// skipRangingProvider is the little of a provider this needs. The internal and
// providers packages each declare their own Provider interface and they are not
// interchangeable, so depending on either here would tie the skip engine to one
// side of that split for no reason.
type skipRangingProvider interface {
	Name() string
	SkipRange(id, mode string, epNo int) (intro, outro []int, err error)
}

// providerSkipSource asks the provider that is already resolving the stream.
//
// This is first in the chain because it is free: a provider that ships timings
// alongside the stream has already paid for the request, so consulting it costs
// nothing and often answers both halves before any service is contacted.
type providerSkipSource struct {
	provider skipRangingProvider
}

func (s providerSkipSource) Name() string {
	if s.provider == nil {
		return "provider"
	}
	return s.provider.Name()
}

func (s providerSkipSource) Lookup(ref SkipRef) (SkipTimes, bool, error) {
	if s.provider == nil || ref.ProviderID == "" {
		return SkipTimes{}, false, nil
	}
	intro, outro, err := s.provider.SkipRange(ref.ProviderID, ref.Mode, ref.Episode)
	if err != nil {
		return SkipTimes{}, false, err
	}
	times := SkipTimes{}
	if len(intro) == 2 {
		times.Op = Skip{Start: intro[0], End: intro[1]}
	}
	if len(outro) == 2 {
		times.Ed = Skip{Start: outro[0], End: outro[1]}
	}
	return times, usableSpan(times.Op) || usableSpan(times.Ed), nil
}

// aniSkipSource is the existing AniSkip service, which keys on MyAnimeList ids.
type aniSkipSource struct{}

func (aniSkipSource) Name() string { return "aniskip" }

func (s aniSkipSource) Lookup(ref SkipRef) (SkipTimes, bool, error) {
	times, _, found, err := s.LookupIdentified(ref)
	return times, found, err
}

// LookupIdentified also returns the id of each entry, which is what a vote
// refers to.
func (aniSkipSource) LookupIdentified(ref SkipRef) (SkipTimes, SkipIDs, bool, error) {
	if ref.MalID <= 0 {
		// Without a MyAnimeList id there is nothing to ask about. This is the
		// ordinary state for a show tracked only on AniList, not an error.
		return SkipTimes{}, SkipIDs{}, false, nil
	}
	body, err := GetAniSkipData(ref.MalID, ref.Episode)
	if err != nil {
		return SkipTimes{}, SkipIDs{}, false, err
	}

	var data skipTimesResponse
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return SkipTimes{}, SkipIDs{}, false, fmt.Errorf("aniskip: %w", err)
	}
	if !data.Found {
		return SkipTimes{}, SkipIDs{}, false, nil
	}

	times, ids := aniSkipTimesFrom(data.Results, 0)
	return times, ids, usableSpan(times.Op) || usableSpan(times.Ed), nil
}

const (
	animeSkipEndpoint = "https://api.anime-skip.com/graphql"
	animeSkipTimeout  = 20 * time.Second
)

// animeSkipSource queries Anime-Skip's GraphQL API.
//
// It needs an X-Client-ID, which identifies the application rather than the
// user. There is deliberately no bundled default: the only ones in circulation
// belong to other projects, and borrowing one makes this program's traffic look
// like theirs and puts their key at risk of being rate-limited or revoked. With
// no client id configured the source reports that it knows nothing, and the
// other sources carry on.
//
// Where the id comes from is the clientIDSource's business: one the user wrote
// down, or -- if they asked for it -- the one Anime-Skip publishes for its own
// playground, fetched again when it stops being accepted.
type animeSkipSource struct {
	clientIDs clientIDSource
	endpoint  string
	http      *http.Client
}

func newAnimeSkipSource(clientID string) animeSkipSource {
	return newAnimeSkipSourceFrom(fixedClientID(clientID))
}

func newAnimeSkipSourceFrom(ids clientIDSource) animeSkipSource {
	return animeSkipSource{
		clientIDs: ids,
		endpoint:  animeSkipEndpoint,
		http:      &http.Client{Timeout: animeSkipTimeout},
	}
}

func (animeSkipSource) Name() string { return "anime-skip" }

func (s animeSkipSource) Lookup(ref SkipRef) (SkipTimes, bool, error) {
	if s.clientIDs == nil || ref.AniListID <= 0 || ref.Episode <= 0 {
		return SkipTimes{}, false, nil
	}

	times, found, err := s.lookupOnce(ref)
	if !isRefusedClientIDError(err) {
		return times, found, err
	}
	// The id was refused rather than the request failing, which is what a
	// rotated id looks like. Ask for another and try once more; a source that
	// cannot find one says so, and the attempt costs a single request.
	Log("anime-skip: the client id was refused; looking for a new one")
	s.clientIDs.Invalidate()
	return s.lookupOnce(ref)
}

func (s animeSkipSource) lookupOnce(ref SkipRef) (SkipTimes, bool, error) {
	clientID, err := s.clientIDs.ClientID()
	if err != nil {
		// Not having an id is not a failure of this episode: it means
		// Anime-Skip cannot be asked at all, which the other sources cover.
		Log(fmt.Sprintf("anime-skip: no client id: %v", err))
		return SkipTimes{}, false, nil
	}
	if clientID == "" {
		return SkipTimes{}, false, nil
	}

	showID, err := s.findShow(clientID, ref.AniListID)
	if err != nil || showID == "" {
		return SkipTimes{}, false, err
	}
	return s.episodeTimestamps(clientID, showID, ref.Episode)
}

type animeSkipErrors struct {
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (s animeSkipSource) query(clientID, query string, variables map[string]any, out any) error {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, s.endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-ID", clientID)

	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("anime-skip: status %d", resp.StatusCode)
	}
	// GraphQL reports failure in the body with a 200, so the status alone says
	// nothing about whether the query worked.
	var failure animeSkipErrors
	if err := json.Unmarshal(body, &failure); err == nil && len(failure.Errors) > 0 {
		return fmt.Errorf("anime-skip: %s", failure.Errors[0].Message)
	}
	return json.Unmarshal(body, out)
}

func (s animeSkipSource) findShow(clientID string, anilistID int) (string, error) {
	const query = `query ($id: String!) {
		findShowsByExternalId(service: ANILIST, serviceId: $id) { id }
	}`
	var out struct {
		Data struct {
			FindShowsByExternalID []struct {
				ID string `json:"id"`
			} `json:"findShowsByExternalId"`
		} `json:"data"`
	}
	if err := s.query(clientID, query, map[string]any{"id": strconv.Itoa(anilistID)}, &out); err != nil {
		return "", err
	}
	if len(out.Data.FindShowsByExternalID) == 0 {
		return "", nil
	}
	return out.Data.FindShowsByExternalID[0].ID, nil
}

func (s animeSkipSource) episodeTimestamps(clientID, showID string, episode int) (SkipTimes, bool, error) {
	const query = `query ($showId: ID!) {
		findEpisodesByShowId(showId: $showId) {
			number
			timestamps { at type { name } }
		}
	}`
	var out struct {
		Data struct {
			FindEpisodesByShowID []struct {
				Number     string `json:"number"`
				Timestamps []struct {
					At   float64 `json:"at"`
					Type struct {
						Name string `json:"name"`
					} `json:"type"`
				} `json:"timestamps"`
			} `json:"findEpisodesByShowId"`
		} `json:"data"`
	}
	if err := s.query(clientID, query, map[string]any{"showId": showID}, &out); err != nil {
		return SkipTimes{}, false, err
	}

	for _, ep := range out.Data.FindEpisodesByShowID {
		if parsed, err := strconv.Atoi(strings.TrimSpace(ep.Number)); err != nil || parsed != episode {
			continue
		}
		times := timestampsToSkips(ep.Timestamps)
		return times, usableSpan(times.Op) || usableSpan(times.Ed), nil
	}
	return SkipTimes{}, false, nil
}

// timestampsToSkips turns Anime-Skip's marker list into spans.
//
// Anime-Skip records single points in time with a type, not ranges: an
// "Intro" marker says where the opening begins and the next marker of any kind
// says where it ends. So the list is sorted and each marker of interest is
// paired with whatever follows it.
func timestampsToSkips(stamps []struct {
	At   float64 `json:"at"`
	Type struct {
		Name string `json:"name"`
	} `json:"type"`
}) SkipTimes {
	type marker struct {
		at   float64
		name string
	}
	markers := make([]marker, 0, len(stamps))
	for _, stamp := range stamps {
		markers = append(markers, marker{at: stamp.At, name: strings.ToLower(stamp.Type.Name)})
	}
	for i := 1; i < len(markers); i++ {
		for j := i; j > 0 && markers[j].at < markers[j-1].at; j-- {
			markers[j], markers[j-1] = markers[j-1], markers[j]
		}
	}

	times := SkipTimes{}
	for i, m := range markers {
		end := 0.0
		if i+1 < len(markers) {
			end = markers[i+1].at
		}
		if end <= m.at {
			continue
		}
		switch {
		case strings.Contains(m.name, "intro") && !usableSpan(times.Op):
			times.Op = Skip{Start: int(m.at), End: int(end)}
		case strings.Contains(m.name, "outro") || strings.Contains(m.name, "credits"):
			if !usableSpan(times.Ed) {
				times.Ed = Skip{Start: int(m.at), End: int(end)}
			}
		}
	}
	return times
}

// DefaultSkipSources builds the chain in the order that costs least.
//
// The active provider comes first because a provider that ships timings with
// the stream has already paid for that request. AniSkip follows: it is keyless
// and covers a great deal. Anime-Skip is last because it needs two requests and
// a client id, and is therefore the one most often unavailable.
func DefaultSkipSources(config *Config, provider any) []SkipSource {
	sources := []SkipSource{}
	if ranger, ok := provider.(skipRangingProvider); ok && ranger != nil {
		sources = append(sources, providerSkipSource{provider: ranger})
	}
	sources = append(sources, aniSkipSource{})
	if config != nil && config.IntroDBSkipTimes {
		sources = append(sources, newIntroDBSource(config.StoragePath))
	}
	if config != nil {
		if configured := strings.TrimSpace(config.AnimeSkipClientID); configured != "" {
			if strings.EqualFold(configured, AnimeSkipAutoClientID) {
				sources = append(sources, newAnimeSkipSourceFrom(animeSkipClientIDs(config.StoragePath)))
			} else {
				sources = append(sources, newAnimeSkipSource(configured))
			}
		}
	}
	return sources
}

// ApplySkipTimes resolves an episode's timings and hands them to the player.
// It reports what happened rather than returning an error: skipping is a
// convenience, and failing to find timings must never interrupt playback.
func ApplySkipTimes(anime *Anime, episode int, config *Config, provider any) SkipResolution {
	if anime == nil || episode <= 0 {
		return SkipResolution{}
	}
	ref := SkipRef{
		MalID:     anime.MalId,
		AniListID: anime.AnilistId,
		Episode:   episode,
		Mode:      anime.Ep.Mode,
	}
	if named, ok := provider.(interface{ Name() string }); ok && named != nil {
		ref.Provider = named.Name()
		ref.ProviderID = anime.ProviderId
	}

	resolution := ResolveSkipTimes(ref, DefaultSkipSources(config, provider)...)
	for _, err := range resolution.Errors {
		Log(fmt.Sprintf("skip lookup: %v", err))
	}
	Log(fmt.Sprintf("Episode %d: %s", episode, resolution.Describe()))

	if !resolution.Found() {
		return resolution
	}
	anime.Ep.SkipTimes = resolution.Times
	if err := SendSkipTimesToMPV(anime); err != nil {
		Log(fmt.Sprintf("sending skip times to the player: %v", err))
	}
	return resolution
}
