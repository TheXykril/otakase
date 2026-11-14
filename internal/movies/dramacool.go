package movies

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Dramacool is dramacool.com.tr: Korean, Chinese, Japanese, Thai and other
// Asian dramas, shows and films in their own language with English
// subtitles (often Indonesian, Malay, Arabic and more beside them).
//
// Every title, film or series, has a page under /drama-detail/<slug>/ that
// lists its episodes; a film is a title with one episode. Each episode page
// holds the player as an iframe, and the player answers <player>/source with
// JSON: an HLS playlist and subtitle files. The player's own address must be
// the referrer for the playlist, its segments and the subtitles.
//
// The domain sends visitors on to a subdomain that changes now and then
// (ww5., wxx.); paths stay the same, so a title is kept by its path and the
// address is found again by following the redirect.
type Dramacool struct {
	Base   string
	Client *http.Client
	// Subtitles picks the subtitle languages to fetch, by the names the
	// player gives them ("English", "Indonesia"); nil fetches English.
	Subtitles func() []string
	// address is where Base redirected to, once known.
	address string
}

// NewDramacool returns dramacool.com.tr.
func NewDramacool() *Dramacool {
	return &Dramacool{Base: "https://dramacool.com.tr", Client: &http.Client{Timeout: 25 * time.Second}}
}

func (*Dramacool) Name() string  { return DramacoolName }
func (*Dramacool) Label() string { return "Dramacool (Asian dramas, English subtitles)" }

var (
	dramacoolItemPattern     = regexp.MustCompile(`(?s)<a href="https?://[^"/]+(/drama-detail/[^"/]+/)" class="img" title="[^"]*">.*?</a>`)
	dramacoolItemTitle       = regexp.MustCompile(`(?s)<h3 class="title"[^>]*>(.*?)</h3>`)
	dramacoolItemImage       = regexp.MustCompile(`data-original="([^"]+)"`)
	dramacoolHeadingPattern  = regexp.MustCompile(`(?s)<div class="info">\s*<h1>(.*?)</h1>`)
	dramacoolPosterPattern   = regexp.MustCompile(`(?s)<div class="details">\s*<div class="img">\s*<img src="([^"]+)"`)
	dramacoolReleasedPattern = regexp.MustCompile(`(?s)<span>Released:</span>\s*<a[^>]*>((?:19|20)\d\d)</a>`)
	dramacoolEpisodesPattern = regexp.MustCompile(`<span>Episodes:</span>\s*(\d+)`)
	dramacoolYearSuffix      = regexp.MustCompile(`\s*\(((?:19|20)\d\d)\)\s*$`)
	dramacoolPlayerPattern   = regexp.MustCompile(`data-video="(https?://[^"]+)"`)
	dramacoolSeasonPattern   = regexp.MustCompile(`(?i)\s*(?:[:\-–]\s*)?(?:season\s*(\d+)|\b(\d+)(?:st|nd|rd|th)?\s+season|\s([2-9]|1\d))\s*$`)
)

// dramacoolEpisodePattern finds a title's episode links: the title's slug,
// then -episode-<n>, and for an episode split in two, -<part>.
func dramacoolEpisodePattern(slug string) *regexp.Regexp {
	return regexp.MustCompile(`href="https?://[^"/]+(/` + regexp.QuoteMeta(slug) + `-episode-(\d+)(?:-(\d+))?/)"`)
}

// get fetches a path from the site, following its move to a subdomain.
func (d *Dramacool) get(path string) (string, error) {
	base := d.address
	if base == "" {
		base = d.Base
	}
	body, final, err := fetchFinal(d.Client, base+path, "")
	if err != nil {
		return "", err
	}
	if final != nil && d.address == "" {
		d.address = final.Scheme + "://" + final.Host
	}
	return body, nil
}

// Search finds titles by their English name. The site cannot tell a film
// from a series in its results; Open does, by the number of episodes.
func (d *Dramacool) Search(query string) ([]Movie, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	body, err := d.get("/?s=" + url.QueryEscape(query))
	if err != nil {
		return nil, err
	}
	return parseDramacoolListing(body), nil
}

func parseDramacoolListing(body string) []Movie {
	found := []Movie{}
	seen := map[string]bool{}
	for _, match := range dramacoolItemPattern.FindAllStringSubmatch(body, -1) {
		path := match[1]
		if seen[path] {
			continue
		}
		seen[path] = true
		name := firstGroup(match[0], dramacoolItemTitle)
		if name == "" {
			continue
		}
		title, year := splitDramacoolYear(name)
		movie := Movie{Provider: DramacoolName, Path: path, Title: title, Year: year}
		if image := dramacoolItemImage.FindStringSubmatch(match[0]); image != nil {
			movie.Poster = image[1]
		}
		found = append(found, movie)
	}
	return found
}

// splitDramacoolYear takes "Queen of Tears (2024)" apart.
func splitDramacoolYear(name string) (string, string) {
	if m := dramacoolYearSuffix.FindStringSubmatchIndex(name); m != nil {
		return strings.TrimSpace(name[:m[0]]), name[m[2]:m[3]]
	}
	return name, ""
}

// DramacoolSeason reads the season a title names, "Hospital Playlist
// Season 2", "Hospital Playlist 2nd Season" or "Hospital Playlist 2", and
// returns the name without it. A title naming none is season 1.
func DramacoolSeason(title string) (string, int) {
	if m := dramacoolSeasonPattern.FindStringSubmatch(title); m != nil {
		n, _ := strconv.Atoi(m[1] + m[2] + m[3])
		if n > 0 {
			return strings.TrimSpace(dramacoolSeasonPattern.ReplaceAllString(title, "")), n
		}
	}
	return title, 1
}

// dramacoolPage is a title's page: what it says about the title and where
// each episode is.
type dramacoolPage struct {
	movie    Movie
	count    int
	episodes map[int]string
}

func (d *Dramacool) page(path string) (dramacoolPage, error) {
	if !strings.HasPrefix(path, "/drama-detail/") {
		return dramacoolPage{}, fmt.Errorf("dramacool: %q is not a title page", path)
	}
	body, err := d.get(path)
	if err != nil {
		return dramacoolPage{}, err
	}
	return parseDramacoolPage(path, body), nil
}

func parseDramacoolPage(path, body string) dramacoolPage {
	page := dramacoolPage{movie: Movie{Provider: DramacoolName, Path: path}, episodes: map[int]string{}}
	if heading := firstGroup(body, dramacoolHeadingPattern); heading != "" {
		page.movie.Title, page.movie.Year = splitDramacoolYear(heading)
	}
	if year := firstGroup(body, dramacoolReleasedPattern); year != "" {
		page.movie.Year = year
	}
	page.movie.Poster = firstGroup(body, dramacoolPosterPattern)
	page.count, _ = strconv.Atoi(firstGroup(body, dramacoolEpisodesPattern))
	slug := strings.Trim(strings.TrimPrefix(path, "/drama-detail/"), "/")
	// An episode split in two is listed as n-1 and n-2 beside n, or only as
	// its parts; the whole one is taken, else its first part.
	parts := map[int]int{}
	for _, m := range dramacoolEpisodePattern(slug).FindAllStringSubmatch(body, -1) {
		n, _ := strconv.Atoi(m[2])
		if n <= 0 {
			continue
		}
		part, _ := strconv.Atoi(m[3])
		if have, ok := parts[n]; ok && (have == 0 || part > have) {
			continue
		}
		parts[n] = part
		page.episodes[n] = m[1]
	}
	if page.count == 0 {
		page.count = len(page.episodes)
	}
	page.movie.Series = page.count > 1
	return page
}

// Open reads a film's page; its servers are those of its one episode.
func (d *Dramacool) Open(movie Movie) (Movie, []Source, error) {
	page, err := d.page(movie.Path)
	if err != nil {
		return movie, nil, err
	}
	opened := page.movie
	opened.Series = movie.Series
	sources, err := d.episodeSources(page, 1)
	return opened, sources, err
}

// Episodes lists a title's episodes in order.
func (d *Dramacool) Episodes(show Movie) ([]Episode, error) {
	page, err := d.page(show.Path)
	if err != nil {
		return nil, err
	}
	_, season := DramacoolSeason(page.movie.Title)
	numbers := make([]int, 0, len(page.episodes))
	for n := range page.episodes {
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)
	episodes := make([]Episode, 0, len(numbers))
	for _, n := range numbers {
		episodes = append(episodes, Episode{Season: season, Number: n})
	}
	return episodes, nil
}

// OpenEpisode lists one episode's servers. The site has a page per season,
// so the episode's season is the page's and only its number counts.
func (d *Dramacool) OpenEpisode(show Movie, episode Episode) ([]Source, error) {
	page, err := d.page(show.Path)
	if err != nil {
		return nil, err
	}
	return d.episodeSources(page, episode.Number)
}

func (d *Dramacool) episodeSources(page dramacoolPage, number int) ([]Source, error) {
	path, ok := page.episodes[number]
	if !ok {
		return nil, fmt.Errorf("dramacool has no episode %d on %s", number, page.movie.Path)
	}
	body, err := d.get(path)
	if err != nil {
		return nil, err
	}
	players := []string{}
	seen := map[string]bool{}
	for _, m := range dramacoolPlayerPattern.FindAllStringSubmatch(body, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			players = append(players, m[1])
		}
	}
	if len(players) == 0 {
		return nil, fmt.Errorf("dramacool: no player on %s", path)
	}
	name := fmt.Sprintf("%s-%d", strings.Trim(strings.TrimPrefix(page.movie.Path, "/drama-detail/"), "/"), number)
	sources := make([]Source, 0, len(players))
	for i, player := range players {
		server := "Dramacool"
		if i > 0 {
			server = fmt.Sprintf("Dramacool %d", i+1)
		}
		sources = append(sources, Source{Server: server, Resolve: func() (Stream, error) {
			if parsed, err := url.Parse(player); err == nil && !strings.Contains(parsed.Path, "/kisskh/") {
				// Another host's embed, played by the extractors 8Filmai uses.
				return ResolveEmbed(player)
			}
			stream, err := d.resolvePlayer(player, name)
			stream.Server = server
			return stream, err
		}})
	}
	return sources, nil
}

// dramacoolSource is the player's answer.
type dramacoolSource struct {
	Status string `json:"status"`
	Source string `json:"source"`
	Tracks []struct {
		File  string `json:"file"`
		Label string `json:"label"`
		Kind  string `json:"kind"`
	} `json:"tracks"`
}

func (d *Dramacool) resolvePlayer(player, name string) (Stream, error) {
	player = strings.TrimSuffix(player, "/")
	body, err := fetchPage(d.Client, player+"/source", player)
	if err != nil {
		return Stream{}, err
	}
	var answer dramacoolSource
	if err := json.Unmarshal([]byte(body), &answer); err != nil {
		return Stream{}, fmt.Errorf("dramacool player: %w", err)
	}
	if answer.Status != "ok" || answer.Source == "" {
		return Stream{}, fmt.Errorf("dramacool player has no video (%s)", answer.Status)
	}
	link, err := url.Parse(answer.Source)
	if err != nil {
		return Stream{}, err
	}
	base, _ := url.Parse(player)
	link = base.ResolveReference(link)
	stream := Stream{URL: link.String(), Referrer: player, HLS: strings.Contains(link.Path, ".m3u8")}
	wanted := []string{"English"}
	if d.Subtitles != nil {
		wanted = d.Subtitles()
	}
	for _, track := range answer.Tracks {
		if track.File == "" || track.Kind != "" && track.Kind != "captions" && track.Kind != "subtitles" || !wantedTrack(track.Label, wanted) {
			continue
		}
		file, err := base.Parse(track.File)
		if err != nil {
			continue
		}
		saved, err := saveSubtitle(d.Client, file.String(), player, fmt.Sprintf("dramacool-%s-%d.srt", name, len(stream.Subtitles)+1))
		if err != nil {
			logf("dramacool subtitles %s: %v", track.Label, err)
			continue
		}
		stream.Subtitles = append(stream.Subtitles, Subtitle{URL: saved, Language: track.Label, Label: track.Label})
	}
	return stream, nil
}

// wantedTrack reports whether a subtitle track's label is one of the wanted
// languages. The player names some by country ("Indonesia" for Indonesian),
// so a shared start of four letters is enough.
func wantedTrack(label string, wanted []string) bool {
	label = strings.ToLower(strings.TrimSpace(label))
	for _, w := range wanted {
		w = strings.ToLower(strings.TrimSpace(w))
		if w == "" {
			continue
		}
		if label == w || len(label) >= 4 && len(w) >= 4 && label[:4] == w[:4] {
			return true
		}
	}
	return false
}

// saveSubtitle downloads a subtitle file the player serves only to itself,
// so the player gets a local file it can read without the referrer.
func saveSubtitle(client *http.Client, rawURL, referrer, name string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", referrer)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %d", rawURL, resp.StatusCode)
	}
	text, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	dir := filepath.Join(os.TempDir(), "otakase-subtitles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, safeFileName(name))
	return path, os.WriteFile(path, text, 0o644)
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func safeFileName(name string) string {
	return unsafeFileChars.ReplaceAllString(name, "_")
}

// EpisodeCount is how many episodes a title has: one for a film.
func (d *Dramacool) EpisodeCount(path string) (int, error) {
	page, err := d.page(path)
	if err != nil {
		return 0, err
	}
	return page.count, nil
}
