package nyaa

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/wraient/curd/internal/curdhost"
	"github.com/wraient/curd/internal/providers"
	"github.com/wraient/curd/internal/torrentstream"
)

type Provider struct{}

func (p *Provider) Name() string { return "nyaa" }

// dubMarkers identify a release that carries an English audio track. The index
// is overwhelmingly subtitled, so dub is opt-in rather than assumed.
var dubMarkers = regexp.MustCompile(`(?i)\b(dual[\s-]?audio|dual|dub(bed)?|multi[\s-]?audio)\b`)

// matchesMode reports whether a release can satisfy the requested audio.
//
// Asking for a dub and being handed a subtitled release is worse than being
// told there is none: Curd can fall back to sub deliberately, announcing it,
// but it cannot undo playing the wrong audio.
func matchesMode(release Release, mode string) bool {
	if providers.NormalizeTranslationType(mode) == "dub" {
		return dubMarkers.MatchString(release.Title)
	}
	return true
}

// usableReleases drops batches, releases for the other audio, and anything with
// no peers -- a release nobody is seeding cannot be played, so offering it would
// only produce a menu entry that stalls.
func usableReleases(releases []Release, mode string) []Release {
	usable := make([]Release, 0, len(releases))
	for _, release := range releases {
		if release.Episode <= 0 || release.Seeders <= 0 || !matchesMode(release, mode) {
			continue
		}
		usable = append(usable, release)
	}
	return usable
}

func (p *Provider) SearchAnime(query, mode string) ([]providers.SelectionOption, error) {
	releases, err := fetchReleases(query)
	if err != nil {
		return nil, err
	}

	// One series has many releases; the picker wants one row per show.
	type series struct {
		title    string
		episodes map[int]struct{}
		seeders  int
	}
	grouped := map[string]*series{}
	for _, release := range usableReleases(releases, mode) {
		key := seriesKey(release.Title)
		if key == "" {
			continue
		}
		entry, ok := grouped[key]
		if !ok {
			entry = &series{title: key, episodes: map[int]struct{}{}}
			grouped[key] = entry
		}
		entry.episodes[release.Episode] = struct{}{}
		if release.Seeders > entry.seeders {
			entry.seeders = release.Seeders
		}
	}

	if len(grouped) == 0 {
		return nil, fmt.Errorf("no results for %q", query)
	}

	options := make([]providers.SelectionOption, 0, len(grouped))
	for key, entry := range grouped {
		options = append(options, providers.SelectionOption{
			Key:   key,
			Title: entry.title,
			Label: fmt.Sprintf("%s · %d episodes", entry.title, len(entry.episodes)),
		})
	}
	// Most-seeded first: the healthiest swarm is the likeliest correct match for
	// what the user typed.
	sort.SliceStable(options, func(i, j int) bool {
		return grouped[options[i].Key].seeders > grouped[options[j].Key].seeders
	})
	return options, nil
}

func (p *Provider) EpisodesList(showID, mode string) ([]string, error) {
	releases, err := fetchReleases(showID)
	if err != nil {
		return nil, err
	}

	// Every release here already came back from a search for showID, so they are
	// all releases of that show. Insisting the derived series key match exactly
	// would drop most of them: groups title the same show differently -- one
	// publishes "Saijo no Osewa", another "Rich Girl Caretaker", a third the full
	// English name -- and the episodes then split across those spellings, so each
	// looked like a separate show carrying a third of the season.
	seen := map[int]struct{}{}
	for _, release := range usableReleases(releases, mode) {
		seen[release.Episode] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("no episodes found for %q", showID)
	}

	numbers := make([]int, 0, len(seen))
	for episode := range seen {
		numbers = append(numbers, episode)
	}
	sort.Ints(numbers)

	episodes := make([]string, 0, len(numbers))
	for _, n := range numbers {
		episodes = append(episodes, strconv.Itoa(n))
	}
	return episodes, nil
}

func (p *Provider) GetEpisodeURL(config providers.PlaybackConfig, id string, epNo int) ([]string, error) {
	return p.GetEpisodeURLForMode(config, id, epNo, config.SubOrDub)
}

func (p *Provider) GetEpisodeURLForMode(config providers.PlaybackConfig, id string, epNo int, mode string) ([]string, error) {
	releases, err := fetchReleases(id)
	if err != nil {
		return nil, err
	}

	// Prefer releases titled the way the stored id spells the show, but do not
	// require it: the same show is published under several spellings, and
	// demanding an exact match makes episodes that exist look missing.
	candidates := usableReleases(releases, mode)
	sort.SliceStable(candidates, func(i, j int) bool {
		return seriesKey(candidates[i].Title) == id && seriesKey(candidates[j].Title) != id
	})

	// fetchReleases already ordered by resolution then swarm health, so within
	// each group the first match for this episode is the best one available.
	for _, release := range candidates {
		if release.Episode != epNo {
			continue
		}
		curdhost.Log(fmt.Sprintf("nyaa: episode %d -> %q (%d seeders, %s)",
			epNo, release.Title, release.Seeders, release.Size))

		streamURL, err := torrentstream.Stream(release.InfoHash)
		if err != nil {
			// Another release may have a healthier swarm.
			curdhost.Log(fmt.Sprintf("nyaa: %q did not start: %v", release.Title, err))
			continue
		}
		return []string{streamURL}, nil
	}

	if providers.NormalizeTranslationType(mode) == "dub" {
		return nil, fmt.Errorf("no dub release indexed for episode %d", epNo)
	}
	return nil, fmt.Errorf("no release indexed for episode %d", epNo)
}

// ResolveProviderID lets Curd recover when a stored id no longer matches how
// the index titles the show.
func (p *Provider) ResolveProviderID(providerID, query string) (string, error) {
	if strings.TrimSpace(providerID) != "" {
		if episodes, err := p.EpisodesList(providerID, "sub"); err == nil && len(episodes) > 0 {
			return providerID, nil
		}
	}
	options, err := p.SearchAnime(query, "sub")
	if err != nil {
		return "", err
	}
	return options[0].Key, nil
}
