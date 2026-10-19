package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	xdraw "golang.org/x/image/draw"
)

// Guess the anime, played in the Activity: a pixelated cover of a popular
// show that gets sharper with each clue. Each round the bot picks three
// clues at random from a pool (source, tags, score, when it aired, format,
// genres, popularity, studio, main character), hardest first, and last
// comes the title with some letters blanked. The show pool here also feeds
// the other games.

const (
	guessPoolSize = 500
	guessRefresh  = 7 * 24 * time.Hour
)

// guessBlocks is how many pixels across the cover is at each step; there is
// one step per clue.
var guessBlocks = []int{6, 9, 13, 18}

// guessInfoClues is how many clues from the pool a round uses before the
// title clue.
const guessInfoClues = 3

type guessShow struct {
	ID       int
	URL      string
	Name     string   // shown on reveal
	Titles   []string // every title and synonym that counts
	Cover    string
	Format   string // AniList format, like TV or MOVIE
	Season   string // like FALL
	Year     int
	Genres   []string
	Studio   string
	Episodes int
	Source   string   // AniList source, like MANGA
	Score    int      // AniList average score, out of 100
	Tags     []string // best-fitting tags, no spoilers
	Leads    []string // main characters
	Faces    []string // their pictures, by Leads' order ("" when none)
	Rank     int      // place in the popularity list
	Popular  int      // members with it on their AniList
	Desc     string   // AniList synopsis, plain text
}

type guessGame struct {
	mu      sync.Mutex
	loading sync.Mutex // one pool fetch at a time
	pool    []guessShow
	fetched time.Time
	recent  []int // recently used shows, not picked again for a while
}

// guessScore is a member's points.
type guessScore struct {
	Total  int    `json:"total"`
	Week   int    `json:"week"`
	WeekOf string `json:"week_of"` // ISO week the Week points are for
	// The week before WeekOf, kept for the Game Champion.
	LastWeek   int    `json:"last_week,omitempty"`
	LastWeekOf string `json:"last_week_of,omitempty"`
}

func weekKey(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// ── Matching ──

// compact is a normalised title without spaces, so "Dan Da Dan" and
// "Dandadan" are the same.
func compact(s string) string { return strings.ReplaceAll(normTitle(s), " ", "") }

// editDistance is the Levenshtein distance between a and b in runes.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// typosAllowed is how many wrong letters a guess at a title of n letters
// may have.
func typosAllowed(n int) int {
	switch {
	case n < 5:
		return 0
	case n <= 8:
		return 1
	case n <= 15:
		return 2
	}
	return 3
}

// guessMatches reports whether guess names the show: any of its titles or
// synonyms, or a title's part before the subtitle, ignoring case,
// punctuation, "the" and season numbers, with a few typos allowed. The
// first words of a long title count when they are most of it.
func guessMatches(guess string, titles []string) bool {
	g := compact(guess)
	gw := strings.Fields(normTitle(guess))
	if len([]rune(g)) < 3 {
		return false
	}
	for _, t := range titles {
		for _, v := range []string{t, mainTitle(t)} {
			c := compact(v)
			if v == "" || len([]rune(c)) < 2 {
				continue
			}
			if g == c {
				return true
			}
			n := len([]rune(c))
			if len([]rune(g)) >= 4 && editDistance(g, c) <= typosAllowed(n) {
				return true
			}
			// "attack on titan" for "Attack on Titan Junior High".
			tw := strings.Fields(normTitle(v))
			if len(gw) >= 2 && len(gw) < len(tw) && slices.Equal(gw, tw[:len(gw)]) && len([]rune(g))*10 >= n*6 {
				return true
			}
		}
	}
	return false
}

// ── Clues ──

var guessFormats = map[string]string{"TV": "TV series", "TV_SHORT": "TV short", "MOVIE": "Movie",
	"SPECIAL": "Special", "OVA": "OVA", "ONA": "Web series", "MUSIC": "Music video"}

// titleBlanks is the last clue: the name with each word's first letter and
// a blank for every other letter, like "F _ _ _ _ _ _  B _ _ _ _ _ _".
// Digits and punctuation stay as they are.
func titleBlanks(name string) string {
	var words []string
	for _, w := range strings.Fields(name) {
		var out []string
		first := true
		for _, r := range w {
			switch {
			case unicode.IsLetter(r) && first:
				out = append(out, strings.ToUpper(string(r)))
				first = false
			case unicode.IsLetter(r):
				out = append(out, "_")
			default:
				out = append(out, string(r))
			}
		}
		words = append(words, strings.Join(out, " "))
	}
	return strings.Join(words, "   ")
}

var guessSources = map[string]string{"MANGA": "a manga", "LIGHT_NOVEL": "a light novel",
	"WEB_NOVEL": "a web novel", "NOVEL": "a novel", "VISUAL_NOVEL": "a visual novel",
	"VIDEO_GAME": "a video game", "GAME": "a game", "WEB_MANGA": "a web manga",
	"ONE_SHOT": "a one-shot manga", "LIVE_ACTION": "a live action show", "MULTIMEDIA_PROJECT": "a multimedia project"}

// guessClue is one clue; level 1 clues are the hardest and come first.
type guessClue struct {
	level int
	text  string
}

// infoClues is every clue in the pool the show has data for.
func infoClues(s guessShow) []guessClue {
	var c []guessClue
	add := func(level int, text string) { c = append(c, guessClue{level, text}) }
	if s.Source == "ORIGINAL" {
		add(1, "An **original** story, not based on anything")
	} else if src := guessSources[s.Source]; src != "" {
		add(1, "Based on **"+src+"**")
	}
	if len(s.Tags) > 0 {
		add(1, "Tags: **"+strings.Join(s.Tags[:min(len(s.Tags), 3)], ", ")+"**")
	}
	if s.Score > 0 {
		add(1, fmt.Sprintf("AniList score **%d%%**", s.Score))
	}
	if s.Year > 0 {
		when := fmt.Sprint(s.Year)
		if s.Season != "" {
			when = strings.ToUpper(s.Season[:1]) + strings.ToLower(s.Season[1:]) + " " + when
		}
		add(2, "Aired **"+when+"**")
	}
	if f := guessFormats[s.Format]; f != "" {
		if s.Episodes > 1 {
			add(2, fmt.Sprintf("**%s** · **%d episodes**", f, s.Episodes))
		} else {
			add(2, "**"+f+"**")
		}
	}
	if len(s.Genres) > 0 {
		add(2, "Genres: **"+strings.Join(s.Genres[:min(len(s.Genres), 3)], ", ")+"**")
	}
	if s.Rank > 0 {
		add(2, fmt.Sprintf("**#%d** most popular anime on AniList", s.Rank))
	}
	if s.Studio != "" {
		add(3, "Studio **"+s.Studio+"**")
	}
	if lead := guessLead(s); lead != "" {
		add(3, "Main character: **"+lead+"**")
	}
	return c
}

// guessLead is a main character whose name doesn't give the title away,
// like Naruto Uzumaki for Naruto.
func guessLead(s guessShow) string {
	title := map[string]bool{}
	for _, t := range s.Titles {
		for _, w := range strings.Fields(normTitle(t)) {
			title[w] = true
		}
	}
next:
	for _, l := range s.Leads {
		for _, w := range strings.Fields(normTitle(l)) {
			if len([]rune(w)) >= 3 && title[w] {
				continue next
			}
		}
		return l
	}
	return ""
}

// vowelBlanks is the other title clue: only the vowels shown, like
// "_ _ _ e _ e _".
func vowelBlanks(name string) string {
	var words []string
	for _, w := range strings.Fields(name) {
		var out []string
		for _, r := range w {
			switch {
			case strings.ContainsRune("aeiouAEIOU", r):
				out = append(out, strings.ToLower(string(r)))
			case unicode.IsLetter(r):
				out = append(out, "_")
			default:
				out = append(out, string(r))
			}
		}
		words = append(words, strings.Join(out, " "))
	}
	return strings.Join(words, "   ")
}

// guessClues are the round's clues, one per step: up to guessInfoClues
// picked at random from the pool, hardest first, then a title clue. intn
// is rand.IntN; tests pass their own.
func guessClues(s guessShow, intn func(int) int) []string {
	pool := infoClues(s)
	for k := len(pool) - 1; k > 0; k-- {
		j := intn(k + 1)
		pool[k], pool[j] = pool[j], pool[k]
	}
	pool = pool[:min(len(pool), guessInfoClues)]
	sort.SliceStable(pool, func(a, b int) bool { return pool[a].level < pool[b].level })
	var clues []string
	for _, c := range pool {
		clues = append(clues, c.text)
	}
	title := titleBlanks(s.Name)
	if intn(2) == 1 && strings.ContainsAny(s.Name, "aeiouAEIOU") {
		title = vowelBlanks(s.Name)
	}
	return append(clues, "Title: `"+title+"`")
}

// ── Pool ──

const guessPoolQuery = `query ($page: Int) {
  Page(page: $page, perPage: 50) {
    media(type: ANIME, isAdult: false, sort: POPULARITY_DESC) {
      id siteUrl synonyms title { romaji english native } coverImage { extraLarge large }
      format season seasonYear startDate { year } genres episodes source averageScore popularity description(asHtml: false)
      studios(isMain: true) { nodes { name } }
      tags { name rank isGeneralSpoiler isMediaSpoiler }
      characters(role: MAIN, sort: RELEVANCE, perPage: 2) { nodes { name { full } image { large } } }
    }
  }
}`

func fetchGuessPool() ([]guessShow, error) {
	var pool []guessShow
	for page := 1; page <= guessPoolSize/50; page++ {
		var r struct {
			Data struct {
				Page struct {
					Media []struct {
						ID       int      `json:"id"`
						SiteURL  string   `json:"siteUrl"`
						Synonyms []string `json:"synonyms"`
						Title    struct {
							Romaji, English, Native string
						} `json:"title"`
						CoverImage struct {
							ExtraLarge string `json:"extraLarge"`
							Large      string `json:"large"`
						} `json:"coverImage"`
						Format     string `json:"format"`
						Season     string `json:"season"`
						SeasonYear int    `json:"seasonYear"`
						StartDate  struct {
							Year int `json:"year"`
						} `json:"startDate"`
						Genres   []string `json:"genres"`
						Episodes int      `json:"episodes"`
						Studios  struct {
							Nodes []struct {
								Name string `json:"name"`
							} `json:"nodes"`
						} `json:"studios"`
						Source       string `json:"source"`
						AverageScore int    `json:"averageScore"`
						Popularity   int    `json:"popularity"`
						Description  string `json:"description"`
						Tags         []struct {
							Name             string `json:"name"`
							Rank             int    `json:"rank"`
							IsGeneralSpoiler bool   `json:"isGeneralSpoiler"`
							IsMediaSpoiler   bool   `json:"isMediaSpoiler"`
						} `json:"tags"`
						Characters struct {
							Nodes []struct {
								Name struct {
									Full string `json:"full"`
								} `json:"name"`
								Image struct {
									Large string `json:"large"`
								} `json:"image"`
							} `json:"nodes"`
						} `json:"characters"`
					} `json:"media"`
				} `json:"Page"`
			} `json:"data"`
		}
		if err := anilistPost("", guessPoolQuery, map[string]any{"page": page}, &r); err != nil {
			if len(pool) > 50 {
				break // enough to play with
			}
			return nil, err
		}
		for k, m := range r.Data.Page.Media {
			cover := m.CoverImage.ExtraLarge
			if cover == "" {
				cover = m.CoverImage.Large
			}
			name := m.Title.English
			if name == "" {
				name = m.Title.Romaji
			}
			if cover == "" || name == "" {
				continue
			}
			var titles []string
			for _, t := range append([]string{m.Title.English, m.Title.Romaji, m.Title.Native}, m.Synonyms...) {
				if strings.TrimSpace(t) != "" {
					titles = append(titles, t)
				}
			}
			show := guessShow{ID: m.ID, URL: m.SiteURL, Name: name, Titles: titles, Cover: cover,
				Format: m.Format, Season: m.Season, Year: m.SeasonYear, Genres: m.Genres, Episodes: m.Episodes,
				Source: m.Source, Score: m.AverageScore, Rank: (page-1)*50 + k + 1, Popular: m.Popularity, Desc: m.Description}
			for _, t := range m.Tags {
				if t.Rank >= 60 && !t.IsGeneralSpoiler && !t.IsMediaSpoiler {
					show.Tags = append(show.Tags, t.Name)
				}
			}
			for _, c := range m.Characters.Nodes {
				if c.Name.Full != "" {
					show.Leads = append(show.Leads, c.Name.Full)
					show.Faces = append(show.Faces, c.Image.Large)
				}
			}
			if show.Year == 0 {
				show.Year, show.Season = m.StartDate.Year, ""
			}
			if len(m.Studios.Nodes) > 0 {
				show.Studio = m.Studios.Nodes[0].Name
			}
			pool = append(pool, show)
		}
		time.Sleep(300 * time.Millisecond) // go easy on AniList
	}
	return pool, nil
}

// load fetches the pool when it is empty or a week old. The other games
// use the same popular shows.
func (g *guessGame) load() error {
	g.mu.Lock()
	stale := len(g.pool) == 0 || time.Since(g.fetched) > guessRefresh
	g.mu.Unlock()
	if !stale {
		return nil
	}
	g.loading.Lock()
	defer g.loading.Unlock()
	g.mu.Lock()
	stale = len(g.pool) == 0 || time.Since(g.fetched) > guessRefresh
	g.mu.Unlock()
	if !stale {
		return nil // fetched while waiting
	}
	pool, err := fetchGuessPool()
	g.mu.Lock()
	defer g.mu.Unlock()
	if err == nil {
		g.pool, g.fetched = pool, time.Now()
	}
	if len(g.pool) == 0 {
		if err == nil {
			err = fmt.Errorf("no shows")
		}
		return err
	}
	return nil
}

// shows is the pool, fetched when needed. Don't change it.
func (g *guessGame) shows() ([]guessShow, error) {
	if err := g.load(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.pool, nil
}

// pick chooses a show not used lately, fetching the pool when needed.
func (g *guessGame) pick() (guessShow, error) {
	if err := g.load(); err != nil {
		return guessShow{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var choices []guessShow
	for _, s := range g.pool {
		if !slices.Contains(g.recent, s.ID) {
			choices = append(choices, s)
		}
	}
	if len(choices) == 0 {
		choices, g.recent = g.pool, nil
	}
	s := choices[rand.IntN(len(choices))]
	g.recent = append(g.recent, s.ID)
	if len(g.recent) > len(g.pool)/3 {
		g.recent = g.recent[1:]
	}
	return s, nil
}

// pixelate draws the cover blocks pixels across, scaled back up so it
// shows as big squares.
func pixelate(img image.Image, blocks int) ([]byte, error) {
	const w, h = 300, 426
	full := coverCrop(img, w, h)
	small := image.NewRGBA(image.Rect(0, 0, blocks, max(blocks*h/w, 1)))
	xdraw.BiLinear.Scale(small, small.Bounds(), full, full.Bounds(), draw.Src, nil)
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.NearestNeighbor.Scale(out, out.Bounds(), small, small.Bounds(), draw.Src, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ── Rounds ──

// resetGuessWeek clears this week's points; all-time points stay.
func (st *store) resetGuessWeek() {
	st.update(func(d *storeData) {
		for id, s := range d.Guess {
			s.Week = 0
			d.Guess[id] = s
		}
	})
}

// ── Leaderboard ──

type guessRank struct {
	User   string
	Points int
}

// guessLeaders is the top 10 by points this week (week true) or all time.
func guessLeaders(scores map[string]guessScore, thisWeek string, week bool) []guessRank {
	var list []guessRank
	for id, s := range scores {
		p := s.Total
		if week {
			if s.WeekOf != thisWeek {
				continue
			}
			p = s.Week
		}
		if p > 0 {
			list = append(list, guessRank{id, p})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Points != list[j].Points {
			return list[i].Points > list[j].Points
		}
		return list[i].User < list[j].User
	})
	if len(list) > 10 {
		list = list[:10]
	}
	return list
}
