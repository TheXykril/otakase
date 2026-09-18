package nyaa

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thexykril/otakase/internal/providerhost"
)

// feedURL is Nyaa's RSS endpoint. Category 1_2 is "Anime - English-translated",
// and f=0 means no filter, so trusted and untrusted releases both come back and
// the ranking below decides between them.
const feedURL = "https://nyaa.si/?page=rss&c=1_2&f=0&q="

type rssFeed struct {
	Items []rssItem `xml:"channel>item"`
}

type rssItem struct {
	Title    string `xml:"title"`
	InfoHash string `xml:"infoHash"`
	Size     string `xml:"size"`
	Seeders  string `xml:"seeders"`
	Trusted  string `xml:"trusted"`
}

// A search, an episode listing and a stream resolve all ask the same question
// of the index in quick succession. Caching the parsed feed for a short while
// keeps that to one request without ever holding data long enough to hide a
// newly published episode.
const feedCacheTTL = 90 * time.Second

var (
	feedMu    sync.Mutex
	feedCache = map[string]cachedFeed{}
)

type cachedFeed struct {
	releases []Release
	fetched  time.Time
}

// fetchReleases returns the indexed releases matching query, most seeded first.
func fetchReleases(query string) ([]Release, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("nyaa: empty search query")
	}

	feedMu.Lock()
	if cached, ok := feedCache[query]; ok && time.Since(cached.fetched) < feedCacheTTL {
		feedMu.Unlock()
		return cached.releases, nil
	}
	feedMu.Unlock()

	req, err := http.NewRequest("GET", feedURL+url.QueryEscape(query), nil)
	if err != nil {
		return nil, fmt.Errorf("nyaa: build request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	resp, err := providerhost.HTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("nyaa: search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("nyaa: read search response: %w", err)
	}
	if !providerhost.HTTPStatusOK(resp.StatusCode) {
		return nil, providerhost.HTTPStatusError("nyaa search", resp.StatusCode, body)
	}

	releases, err := parseFeed(body)
	if err != nil {
		return nil, err
	}

	feedMu.Lock()
	feedCache[query] = cachedFeed{releases: releases, fetched: time.Now()}
	feedMu.Unlock()
	return releases, nil
}

// parseFeed turns the RSS payload into releases, best first.
func parseFeed(body []byte) ([]Release, error) {
	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("nyaa: parse search response: %w", err)
	}

	releases := make([]Release, 0, len(feed.Items))
	for _, item := range feed.Items {
		title := strings.TrimSpace(item.Title)
		if title == "" || strings.TrimSpace(item.InfoHash) == "" {
			continue
		}
		release := parseRelease(title)
		release.InfoHash = strings.TrimSpace(item.InfoHash)
		release.Size = strings.TrimSpace(item.Size)
		release.Seeders, _ = strconv.Atoi(strings.TrimSpace(item.Seeders))
		release.Trusted = strings.EqualFold(strings.TrimSpace(item.Trusted), "yes")
		releases = append(releases, release)
	}

	sortReleasesByPreference(releases)
	return releases, nil
}

// preferredQualities is the order a release is chosen in when several carry the
// same episode. 1080p first because that is what most groups publish; an
// unstated resolution ranks last rather than being discarded, since some
// perfectly good releases simply do not say.
var preferredQualities = []int{1080, 720, 480}

func qualityRank(quality int) int {
	for i, q := range preferredQualities {
		if quality == q {
			return i
		}
	}
	return len(preferredQualities)
}

// sortReleasesByPreference orders releases by how likely they are to play well:
// resolution first, then swarm health. Seeders matter more than any label --
// a "trusted" release with two peers starts slower than an untrusted one with
// four hundred -- so trust only breaks ties.
func sortReleasesByPreference(releases []Release) {
	sort.SliceStable(releases, func(i, j int) bool {
		a, b := releases[i], releases[j]
		if ra, rb := qualityRank(a.Quality), qualityRank(b.Quality); ra != rb {
			return ra < rb
		}
		if a.Seeders != b.Seeders {
			return a.Seeders > b.Seeders
		}
		return a.Trusted && !b.Trusted
	})
}
