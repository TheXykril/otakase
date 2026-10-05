package movies

import (
	"cmp"
	"compress/gzip"
	"context"
	"encoding/base64"
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
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
)

// Vidsrc is vidsrc's movie library, in English, keyed by IMDb id. Titles are
// searched on IMDb's own suggestion list, which needs no key.
//
// vidsrc.to's player pages only call a JSON API (data.vidsrc.sh), which
// answers an IMDb id with its stream links encrypted, and a small WebAssembly
// module holding the key, which turns every five minutes. The player runs the
// module to read the links; so does this, through wazero, with nothing else in
// it: the module imports nothing. A link then needs a token from its own
// host, tied to the address that asked for it.
type Vidsrc struct {
	API       string
	SearchURL string
	Client    *http.Client
	// Subtitles picks the subtitle language for a movie; nil is English.
	Subtitles func(Movie) SubtitleLanguage
	// ListEpisodes lists a series' episodes by its IMDb id. vidsrc has no
	// list of its own; Trakt's is used.
	ListEpisodes func(imdb string) ([]Episode, error)
}

// NewVidsrc returns vidsrc at its usual addresses.
func NewVidsrc() *Vidsrc {
	return &Vidsrc{
		API:       "https://data.vidsrc.sh",
		SearchURL: "https://v3.sg.media-imdb.com/suggestion/x/",
		Client:    &http.Client{Timeout: 25 * time.Second},
	}
}

func (*Vidsrc) Name() string  { return VidsrcName }
func (*Vidsrc) Label() string { return "vidsrc (English)" }

// imdbSuggestions is IMDb's suggestion list.
type imdbSuggestions struct {
	D []struct {
		ID    string `json:"id"`
		Title string `json:"l"`
		Year  int    `json:"y"`
		Kind  string `json:"qid"`
		Image struct {
			URL string `json:"imageUrl"`
		} `json:"i"`
	} `json:"d"`
}

func (v *Vidsrc) get(rawURL string, header map[string]string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	for key, value := range header {
		req.Header.Set(key, value)
	}
	resp, err := v.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %d", rawURL, resp.StatusCode)
	}
	return body, nil
}

// Search finds films and series by title.
func (v *Vidsrc) Search(query string) ([]Movie, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	body, err := v.get(v.SearchURL+url.PathEscape(strings.ToLower(query))+".json", nil)
	if err != nil {
		return nil, err
	}
	var found imdbSuggestions
	if err := json.Unmarshal(body, &found); err != nil {
		return nil, fmt.Errorf("IMDb search: %w", err)
	}
	movies := []Movie{}
	for _, item := range found.D {
		series := item.Kind == "tvSeries" || item.Kind == "tvMiniSeries"
		if !strings.HasPrefix(item.ID, "tt") || (item.Kind != "movie" && item.Kind != "tvMovie" && !series) {
			continue
		}
		movie := Movie{Provider: VidsrcName, Path: item.ID, Title: item.Title, IMDb: item.ID, Poster: item.Image.URL, Series: series}
		if item.Year > 0 {
			movie.Year = fmt.Sprint(item.Year)
		}
		movies = append(movies, movie)
	}
	return movies, nil
}

// vidsrcAnswer is the API's answer for one film or episode.
type vidsrcAnswer struct {
	Data struct {
		Title      string `json:"title"`
		FileName   string `json:"file_name"`
		StreamURLs string `json:"stream_urls"`
	} `json:"data"`
	VS struct {
		WasmURL string `json:"wasm_url"`
	} `json:"vs"`
}

// Open lists the film's streams.
func (v *Vidsrc) Open(movie Movie) (Movie, []Source, error) {
	movie.Provider = VidsrcName
	if movie.IMDb == "" {
		movie.IMDb = movie.Path
	}
	sources, err := v.streams(movie, Episode{})
	return movie, sources, err
}

// Episodes lists a series' aired episodes.
func (v *Vidsrc) Episodes(show Movie) ([]Episode, error) {
	if v.ListEpisodes == nil {
		return nil, fmt.Errorf("no episode list for vidsrc series")
	}
	return v.ListEpisodes(cmp.Or(show.IMDb, show.Path))
}

// OpenEpisode lists one episode's streams.
func (v *Vidsrc) OpenEpisode(show Movie, episode Episode) ([]Source, error) {
	if show.IMDb == "" {
		show.IMDb = show.Path
	}
	return v.streams(show, episode)
}

// streams lists a film's or an episode's streams. They are all resolved at
// once, by one request and one decryption; each source then only fetches its
// token.
func (v *Vidsrc) streams(movie Movie, episode Episode) ([]Source, error) {
	query := "type=movie&imdb=" + url.QueryEscape(movie.IMDb)
	name := movie.IMDb
	if episode.Number > 0 {
		query = fmt.Sprintf("type=tv&imdb=%s&season=%d&episode=%d", url.QueryEscape(movie.IMDb), episode.Season, episode.Number)
		name = fmt.Sprintf("%s-s%de%d", movie.IMDb, episode.Season, episode.Number)
	}
	body, err := v.get(v.API+"/api.php?"+query+"&stream_urls", nil)
	if err != nil {
		return nil, err
	}
	var answer vidsrcAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, fmt.Errorf("vidsrc: %w", err)
	}
	if answer.Data.StreamURLs == "" || answer.VS.WasmURL == "" {
		return nil, fmt.Errorf("vidsrc has no streams for %s", name)
	}
	links, err := v.decrypt(answer.Data.StreamURLs, answer.VS.WasmURL)
	if err != nil {
		return nil, fmt.Errorf("vidsrc: %w", err)
	}
	// Fetched once, by the first source that resolves.
	var subtitlesOnce sync.Once
	var found []Subtitle
	subtitles := func() []Subtitle {
		subtitlesOnce.Do(func() {
			found = openSubtitles(v.Client, openSubtitlesQuery(movie.IMDb, episode), name, answer.Data.FileName, v.subtitleLanguage(movie))
		})
		return found
	}
	sources := []Source{}
	for i, link := range links {
		server := fmt.Sprintf("vidsrc %d", i+1)
		sources = append(sources, Source{Server: server, Resolve: func() (Stream, error) {
			stream, err := v.withToken(link)
			if err != nil {
				return Stream{}, err
			}
			stream.Server = server
			stream.Subtitles = subtitles()
			return stream, nil
		}})
	}
	return sources, nil
}

func (v *Vidsrc) subtitleLanguage(movie Movie) SubtitleLanguage {
	if v.Subtitles == nil {
		return English
	}
	return v.Subtitles(movie)
}

// decrypt runs the key module over the encrypted links: alloc room for them,
// copy them in, and decrypt in place, which leaves the plain text twelve
// bytes in (after the nonce), one link per line.
func (v *Vidsrc) decrypt(encoded, wasmURL string) ([]string, error) {
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	module, err := v.get(wasmURL, nil)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	runtime := wazero.NewRuntime(ctx)
	defer runtime.Close(ctx)
	instance, err := runtime.Instantiate(ctx, module)
	if err != nil {
		return nil, fmt.Errorf("key module: %w", err)
	}
	alloc, decrypt, memory := instance.ExportedFunction("alloc"), instance.ExportedFunction("decrypt"), instance.Memory()
	if alloc == nil || decrypt == nil || memory == nil {
		return nil, fmt.Errorf("key module changed: no alloc, decrypt or memory")
	}
	result, err := alloc.Call(ctx, uint64(len(sealed)))
	if err != nil {
		return nil, err
	}
	ptr := uint32(result[0])
	if !memory.Write(ptr, sealed) {
		return nil, fmt.Errorf("key module: out of memory")
	}
	result, err = decrypt.Call(ctx, uint64(ptr), uint64(len(sealed)))
	if err != nil {
		return nil, err
	}
	plain, ok := memory.Read(ptr+12, uint32(result[0]))
	if !ok {
		return nil, fmt.Errorf("key module: decrypted past its memory")
	}
	links := []string{}
	for _, line := range strings.Split(string(plain), "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "http") {
			links = append(links, line)
		}
	}
	if len(links) == 0 {
		return nil, fmt.Errorf("no links in the decrypted answer")
	}
	return links, nil
}

// withToken asks a stream's host for the token its playlist needs.
func (v *Vidsrc) withToken(link string) (Stream, error) {
	parsed, err := url.Parse(link)
	if err != nil {
		return Stream{}, err
	}
	token, err := v.get(parsed.Scheme+"://"+parsed.Host+"/generate.php", nil)
	if err != nil {
		return Stream{}, fmt.Errorf("token: %w", err)
	}
	jwt := strings.TrimSpace(string(token))
	if strings.Count(jwt, ".") != 2 {
		return Stream{}, fmt.Errorf("token: %s gave no token", parsed.Host)
	}
	switch {
	case strings.Contains(link, "__TOKEN__"):
		link = strings.ReplaceAll(link, "__TOKEN__", jwt)
	case strings.Contains(link, "?"):
		link += "&token=" + jwt
	default:
		link += "?token=" + jwt
	}
	// No referrer: the host answers any segment asked for with one, even its
	// own address, with 403.
	return Stream{URL: link, HLS: true, AudioLanguage: "en,eng"}, nil
}

// SubtitleLanguage is a language to fetch subtitles in.
type SubtitleLanguage struct {
	// Code is the ISO 639-2 code OpenSubtitles names it by, such as "lit".
	Code string
	// Name is how it reads, such as "Lithuanian".
	Name string
}

// English is the language subtitles fall back to.
var English = SubtitleLanguage{Code: "eng", Name: "English"}

// openSubtitles fetches subtitles from OpenSubtitles' keyless API, the one
// vidsrc's own player uses: up to three in the language asked for, then two
// in English when that is another language, as a language with few
// subtitles may have none for the film. English alone gets four. query names the film
// (imdbid-0816692) or the episode (episode-1/imdbid-0903747/season-1).
//
// Subtitles are timed to one release of a film, so the ones made for the
// release vidsrc streams (its file name, such as
// Interstellar.2014.1080p.BluRay.x264.YIFY.mp4) come first; a few others
// follow, for the viewer to switch to in the player when the first is out of
// time. They come gzipped and are unpacked into the temporary folder.
func openSubtitles(client *http.Client, query, name, release string, language SubtitleLanguage) []Subtitle {
	languages := []SubtitleLanguage{language}
	if language.Code == "" {
		languages = []SubtitleLanguage{English}
	} else if language.Code != English.Code {
		languages = append(languages, English)
	}
	subtitles := []Subtitle{}
	seen := map[string]bool{}
	for i, language := range languages {
		limit := maxSubtitles
		switch {
		case len(languages) > 1 && i == 0:
			limit = 3
		case i > 0:
			limit = len(subtitles) + 2
		}
		for _, result := range searchOpenSubtitles(client, query+"/sublanguageid-"+language.Code, release) {
			if len(subtitles) >= limit {
				break
			}
			key := strings.ToLower(result.FileName)
			if seen[key] {
				continue
			}
			seen[key] = true
			file, err := saveGzipped(client, result.Link, fmt.Sprintf("%s-%s-%d.%s", name, language.Code, len(subtitles)+1, strings.ToLower(result.Format)))
			if err != nil {
				logf("subtitles for %s: %v", name, err)
				continue
			}
			subtitles = append(subtitles, Subtitle{URL: file, Language: language.Name, Label: strings.TrimSuffix(result.FileName, filepath.Ext(result.FileName))})
		}
	}
	return subtitles
}

// openSubtitlesQuery names a film to OpenSubtitles by its IMDb id, as IMDb
// writes it, zeros and all: without them the API redirects to a broken
// address. An episode is named by its show's id, season and number.
func openSubtitlesQuery(imdb string, episode Episode) string {
	id := strings.TrimPrefix(imdb, "tt")
	if id == "" {
		return ""
	}
	if episode.Number > 0 {
		return fmt.Sprintf("episode-%d/imdbid-%s/season-%d", episode.Number, id, episode.Season)
	}
	return "imdbid-" + id
}

// searchOpenSubtitles runs one search and ranks what it finds.
func searchOpenSubtitles(client *http.Client, query, release string) []openSubtitle {
	if strings.HasPrefix(query, "/") {
		return nil
	}
	req, err := http.NewRequest(http.MethodGet, "https://rest.opensubtitles.org/search/"+query, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("X-User-Agent", "trailers.to-UA")
	resp, err := client.Do(req)
	if err != nil {
		logf("subtitles %s: %v", query, err)
		return nil
	}
	defer resp.Body.Close()
	var results []openSubtitle
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&results); err != nil || len(results) == 0 {
		return nil
	}
	return rankSubtitles(results, release)
}

// maxSubtitles is how many subtitle files are offered for one movie.
const maxSubtitles = 4

type openSubtitle struct {
	FileName       string `json:"SubFileName"`
	Release        string `json:"MovieReleaseName"`
	Link           string `json:"SubDownloadLink"`
	Format         string `json:"SubFormat"`
	Downloads      string `json:"SubDownloadsCnt"`
	HearingImpared string `json:"SubHearingImpaired"`
}

var releaseWordPattern = regexp.MustCompile(`[a-z0-9]+`)

// releaseWords splits a release name into its lowercase words.
func releaseWords(name string) []string {
	// Only a file's extension: in a bare release name the last dotted part
	// is the group (….x264.YIFY), which filepath.Ext would take.
	switch strings.ToLower(filepath.Ext(name)) {
	case ".srt", ".sub", ".ass", ".ssa", ".vtt", ".mp4", ".mkv", ".avi":
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	return releaseWordPattern.FindAllString(strings.ToLower(name), -1)
}

// rankSubtitles orders subtitles by how well their release matches the
// stream's: the release group (the last word, such as YIFY) matters most,
// as it decides the cut and the frame timing, then the source and the
// resolution. Ties go to the most downloaded, and subtitles for the hearing
// impaired come after the plain ones.
func rankSubtitles(results []openSubtitle, release string) []openSubtitle {
	want := releaseWords(filepath.Base(release))
	group := ""
	if len(want) > 0 {
		group = want[len(want)-1]
	}
	wanted := map[string]bool{}
	for _, word := range want {
		wanted[word] = true
	}
	score := func(result openSubtitle) int {
		words := releaseWords(result.Release)
		if len(words) == 0 {
			words = releaseWords(result.FileName)
		}
		points := 0
		for i, word := range words {
			switch {
			case word == group && group != "" && i == len(words)-1:
				points += 100
			case word == group && group != "":
				points += 60
			case wanted[word]:
				points += 5
			}
		}
		if result.HearingImpared == "1" {
			points -= 3
		}
		return points
	}
	ranked := append([]openSubtitle(nil), results...)
	sort.SliceStable(ranked, func(i, j int) bool {
		si, sj := score(ranked[i]), score(ranked[j])
		if si != sj {
			return si > sj
		}
		di, _ := strconv.Atoi(ranked[i].Downloads)
		dj, _ := strconv.Atoi(ranked[j].Downloads)
		return di > dj
	})
	return ranked
}

func saveGzipped(client *http.Client, rawURL, name string) (string, error) {
	if rawURL == "" {
		return "", fmt.Errorf("no download link")
	}
	resp, err := client.Get(rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %d", rawURL, resp.StatusCode)
	}
	unpacked, err := gzip.NewReader(resp.Body)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(os.TempDir(), "otakase-subtitles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	out, err := os.Create(path)
	if err != nil {
		return "", err
	}
	text, err := io.ReadAll(io.LimitReader(unpacked, 16<<20))
	if err != nil {
		out.Close()
		return "", err
	}
	if _, err := out.WriteString(dropAdvertCues(string(text))); err != nil {
		out.Close()
		return "", err
	}
	return path, out.Close()
}

// linkLinePattern is a subtitle line that is only a web address.
var linkLinePattern = regexp.MustCompile(`(?i)^(?:https?://|www\.)\S+$`)

// bareLinkCue reports whether a cue's text is only a web address, as the
// credit a subtitle site adds for itself (www.subtitrai.net).
func bareLinkCue(cue string) bool {
	lines := strings.Split(strings.TrimSpace(cue), "\n")
	text := []string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "-->") || isDigits(line) {
			continue
		}
		text = append(text, line)
	}
	if len(text) == 0 {
		return false
	}
	for _, line := range text {
		if !linkLinePattern.MatchString(line) {
			return false
		}
	}
	return true
}

func isDigits(text string) bool {
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return text != ""
}

// dropAdvertCues takes out the cues OpenSubtitles adds to its files to
// advertise itself, such as "Watch Online Movies and Series for FREE".
func dropAdvertCues(srt string) string {
	srt = strings.ReplaceAll(srt, "\r\n", "\n")
	cues := strings.Split(srt, "\n\n")
	kept := cues[:0]
	for _, cue := range cues {
		lower := strings.ToLower(cue)
		if strings.Contains(lower, "osdb.link") || strings.Contains(lower, "opensubtitles") || bareLinkCue(cue) {
			continue
		}
		kept = append(kept, cue)
	}
	return strings.Join(kept, "\n\n")
}
