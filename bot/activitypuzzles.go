package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"math/rand/v2"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"

	xdraw "golang.org/x/image/draw"
)

// The Activity's puzzle games: zoomed cover (coverle), put shows in airing
// order (timeline), find the four groups (connections), the blacked-out
// synopsis (redacted), the 3×3 grid and hangman. Each is a board the bot
// keeps; the page sends moves and gets back what it may see. Played alone
// (activitypuzzleapi.go) or by everyone in a call on the same puzzle
// (activityparty.go).

// pzMove is one move on any board; each game reads the fields it needs.
type pzMove struct {
	ID     int    `json:"id"`
	IDs    []int  `json:"ids"`
	Text   string `json:"text"`
	Letter string `json:"letter"`
	Cell   int    `json:"cell"`
	GiveUp bool   `json:"give_up"`
	show   *guessShow
}

// actPuzzle is a board.
type actPuzzle interface {
	move(m pzMove) error // an error is shown to the player and costs nothing
	result() (done, won bool)
	view() any
	clone() actPuzzle
}

// errPz is a message for the player.
type errPz string

func (e errPz) Error() string { return string(e) }

// pzGames are the puzzle games and their party round time in seconds.
var pzGames = map[string]int{"coverle": 90, "timeline": 60, "connections": 180, "redacted": 180, "grid": 180, "hangman": 90,
	"wordle": 120, "quiz": 240, "truths": 150}

// newPuzzle makes a board from the show pool. mode picks hangman's and
// wordle's word kind (title or character).
func (b *bot) newPuzzle(game, mode string) (actPuzzle, error) {
	pool, err := b.guess.shows()
	if err != nil {
		return nil, err
	}
	switch game {
	case "coverle":
		var s guessShow
		for range 5 {
			if s, err = b.guess.pick(); err != nil {
				return nil, err
			}
			img, err := fetchImage(s.Cover)
			if err == nil {
				return newCoverle(s, img, rand.Float64), nil
			}
		}
		return nil, err
	case "timeline":
		return newTimeline(pool, rand.IntN)
	case "connections":
		return newConnections(pool, rand.IntN)
	case "redacted":
		return newRedacted(pool, rand.IntN)
	case "grid":
		return newGrid(pool, rand.IntN)
	case "hangman":
		return newHangman(pool, mode, rand.IntN)
	case "wordle":
		return newWordle(pool, mode, rand.IntN)
	case "quiz":
		qs, err := newQuiz(pool, quizLen, rand.IntN)
		return &qset{game: game, qs: qs, need: quizNeed}, err
	case "truths":
		qs, err := newTruths(pool, truthsLen, rand.IntN)
		return &qset{game: game, qs: qs, need: truthsNeed}, err
	}
	return nil, fmt.Errorf("unknown game")
}

// shuffle mixes a slice in place with intn.
func shuffle[T any](s []T, intn func(int) int) {
	for k := len(s) - 1; k > 0; k-- {
		j := intn(k + 1)
		s[k], s[j] = s[j], s[k]
	}
}

// ── Coverle ──

const coverleTries = 6

// coverleZoom is how much of the cover each step shows, closest first.
var coverleZoom = []float64{0.1, 0.15, 0.22, 0.32, 0.46, 0.66, 1}

type coverleTry struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Year   string `json:"year"`
	YearOK string `json:"year_hit"`
	Arrow  string `json:"arrow,omitempty"` // the answer is newer (up) or older (down)
	Studio string `json:"studio"`
	StuOK  string `json:"studio_hit"`
	Genres string `json:"genres"`
	GenOK  string `json:"genres_hit"`
	Format string `json:"format"`
	ForOK  string `json:"format_hit"`
	Close  int    `json:"close"` // how close overall, 0 to 100
}

type coverle struct {
	ans    guessShow
	img    image.Image
	cx, cy float64 // where the zoom looks, 0..1
	tries  []coverleTry
	done   bool
	won    bool
}

func newCoverle(s guessShow, img image.Image, rnd func() float64) *coverle {
	return &coverle{ans: s, img: img, cx: 0.2 + 0.6*rnd(), cy: 0.15 + 0.6*rnd()}
}

func (c *coverle) clone() actPuzzle {
	n := *c
	n.tries = slices.Clone(c.tries)
	return &n
}

func (c *coverle) result() (bool, bool) { return c.done, c.won }

// step is how far the picture has zoomed out.
func (c *coverle) step() int {
	if c.done {
		return len(coverleZoom) - 1
	}
	return min(len(c.tries), len(coverleZoom)-1)
}

func hit(ok bool) string {
	if ok {
		return "yes"
	}
	return "no"
}

// coverleCompare is how a guess compares with the answer.
func coverleCompare(g, a guessShow) coverleTry {
	t := coverleTry{ID: g.ID, Name: g.Name, Year: "?", YearOK: "no", Studio: g.Studio, StuOK: "no", Format: dailyFormats[g.Format], ForOK: "no"}
	if t.Studio == "" {
		t.Studio = "?"
	}
	if t.Format == "" {
		t.Format = "?"
	}
	yearScore := 0.0
	if g.Year > 0 && a.Year > 0 {
		t.Year = fmt.Sprint(g.Year)
		d := a.Year - g.Year
		switch {
		case d == 0:
			t.YearOK = "yes"
		case d > -4 && d < 4:
			t.YearOK = "near"
		}
		if d > 0 {
			t.Arrow = "up"
		} else if d < 0 {
			t.Arrow = "down"
		}
		yearScore = max(0, 1-math.Abs(float64(d))/12)
	}
	studio := g.Studio != "" && g.Studio == a.Studio
	t.StuOK = hit(studio)
	shared := 0
	for _, x := range g.Genres {
		if slices.Contains(a.Genres, x) {
			shared++
		}
	}
	union := len(g.Genres) + len(a.Genres) - shared
	t.Genres = fmt.Sprintf("%d/%d", shared, len(a.Genres))
	switch {
	case shared > 0 && shared == len(a.Genres) && len(g.Genres) == len(a.Genres):
		t.GenOK = "yes"
	case shared > 0:
		t.GenOK = "near"
	default:
		t.GenOK = "no"
	}
	format := g.Format == a.Format
	t.ForOK = hit(format)
	jac := 0.0
	if union > 0 {
		jac = float64(shared) / float64(union)
	}
	src := 0.0
	if g.Source != "" && g.Source == a.Source {
		src = 1
	}
	b2f := func(b bool) float64 {
		if b {
			return 1
		}
		return 0
	}
	t.Close = min(99, int(math.Round(100*(0.3*yearScore+0.2*b2f(studio)+0.3*jac+0.1*b2f(format)+0.1*src))))
	if g.ID == a.ID {
		t.Close = 100
	}
	return t
}

func (c *coverle) move(m pzMove) error {
	if c.done {
		return nil
	}
	if m.GiveUp {
		c.done = true
		return nil
	}
	if m.show == nil {
		return errPz("pick a show from the list")
	}
	for _, t := range c.tries {
		if t.ID == m.show.ID {
			return errPz("you tried that one")
		}
	}
	c.tries = append(c.tries, coverleCompare(*m.show, c.ans))
	if m.show.ID == c.ans.ID {
		c.done, c.won = true, true
	} else if len(c.tries) >= coverleTries {
		c.done = true
	}
	return nil
}

type coverleView struct {
	Step   int          `json:"step"`
	Tries  []coverleTry `json:"tries"`
	Max    int          `json:"max"`
	Done   bool         `json:"done"`
	Won    bool         `json:"won"`
	Answer *activityHit `json:"answer,omitempty"`
}

func (c *coverle) view() any {
	v := coverleView{Step: c.step(), Tries: slices.Clone(c.tries), Max: coverleTries, Done: c.done, Won: c.won}
	if v.Tries == nil {
		v.Tries = []coverleTry{}
	}
	if c.done {
		v.Answer = &activityHit{ID: c.ans.ID, Name: c.ans.Name, URL: c.ans.URL}
	}
	return v
}

// zoomPic draws the part of the cover a step shows, scaled up to the
// full card.
func zoomPic(img image.Image, cx, cy, frac float64) ([]byte, error) {
	const w, h = 300, 426
	full := coverCrop(img, w, h)
	cw, ch := frac*w, frac*h
	x0 := math.Max(0, math.Min(cx*w-cw/2, w-cw))
	y0 := math.Max(0, math.Min(cy*h-ch/2, h-ch))
	src := image.Rect(int(x0), int(y0), int(math.Ceil(x0+cw)), int(math.Ceil(y0+ch))).Intersect(full.Bounds())
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(out, out.Bounds(), full, src, draw.Src, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ── Timeline ──

const (
	timelineShows = 5
	timelineTries = 3
)

type timeline struct {
	shows []guessShow // the right order, oldest first
	order []int       // the order last sent (or the first shuffle)
	hits  []bool      // per place, from the last try
	tries int
	done  bool
	won   bool
}

func newTimeline(pool []guessShow, intn func(int) int) (*timeline, error) {
	byYear := map[int][]guessShow{}
	for _, s := range pool {
		if s.Year > 0 {
			byYear[s.Year] = append(byYear[s.Year], s)
		}
	}
	var years []int
	for y := range byYear {
		years = append(years, y)
	}
	if len(years) < timelineShows {
		return nil, fmt.Errorf("not enough years")
	}
	sort.Ints(years)
	shuffle(years, intn)
	years = years[:timelineShows]
	sort.Ints(years)
	t := &timeline{}
	for _, y := range years {
		list := byYear[y]
		t.shows = append(t.shows, list[intn(len(list))])
	}
	for _, s := range t.shows {
		t.order = append(t.order, s.ID)
	}
	for sorted := true; sorted; {
		shuffle(t.order, intn)
		sorted = slices.Equal(t.order, t.ids())
	}
	return t, nil
}

func (t *timeline) ids() []int {
	var out []int
	for _, s := range t.shows {
		out = append(out, s.ID)
	}
	return out
}

func (t *timeline) clone() actPuzzle {
	n := *t
	n.order, n.hits = slices.Clone(t.order), slices.Clone(t.hits)
	return &n
}

func (t *timeline) result() (bool, bool) { return t.done, t.won }

func (t *timeline) move(m pzMove) error {
	if t.done {
		return nil
	}
	if m.GiveUp {
		t.done = true
		return nil
	}
	want := t.ids()
	got := slices.Clone(m.IDs)
	a, b := slices.Clone(want), slices.Clone(got)
	sort.Ints(a)
	sort.Ints(b)
	if !slices.Equal(a, b) {
		return errPz("send every show once")
	}
	t.order, t.tries, t.hits = got, t.tries+1, make([]bool, len(got))
	all := true
	for k := range got {
		t.hits[k] = got[k] == want[k]
		all = all && t.hits[k]
	}
	t.won = all
	t.done = all || t.tries >= timelineTries
	return nil
}

type timelineItem struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Year int    `json:"year,omitempty"` // once placed right, or at the end
	Hit  string `json:"hit,omitempty"`
}

type timelineView struct {
	Items  []timelineItem `json:"items"`
	Answer []timelineItem `json:"answer,omitempty"`
	Left   int            `json:"left"`
	Done   bool           `json:"done"`
	Won    bool           `json:"won"`
}

func (t *timeline) view() any {
	byID := map[int]guessShow{}
	for _, s := range t.shows {
		byID[s.ID] = s
	}
	v := timelineView{Left: timelineTries - t.tries, Done: t.done, Won: t.won}
	for k, id := range t.order {
		s := byID[id]
		it := timelineItem{ID: id, Name: s.Name}
		if t.hits != nil {
			it.Hit = hit(t.hits[k])
			if t.hits[k] || t.done {
				it.Year = s.Year
			}
		}
		v.Items = append(v.Items, it)
	}
	if t.done {
		for _, s := range t.shows {
			v.Answer = append(v.Answer, timelineItem{ID: s.ID, Name: s.Name, Year: s.Year})
		}
	}
	return v
}

// ── Connections ──

const connMistakes = 4

type connGroup struct {
	Label string
	IDs   []int
}

type connections struct {
	groups   []connGroup
	names    map[int]string
	left     []int // tiles not in a found group, in the shown order
	found    []int // groups found, in order
	mistakes int
	tried    [][]int
	note     string
	done     bool
	won      bool
}

// pzAdultWord marks genres and tags kept out of puzzle groups.
var pzAdultWord = regexp.MustCompile(`(?i)sex|nud|ecchi|hentai|fan ?service|harem|incest|rape|lolicon|shotacon|boys' love|yuri|yaoi`)

// poolGroups are the ways shows can belong together, with their members.
// Each kind is a rule like the studio or the year.
type poolGroup struct {
	kind  string
	label string
	ids   map[int]bool
}

func poolGroups(pool []guessShow, kinds ...string) []poolGroup {
	m := map[string]*poolGroup{}
	add := func(kind, label string, id int) {
		key := kind + "/" + label
		g := m[key]
		if g == nil {
			g = &poolGroup{kind: kind, label: label, ids: map[int]bool{}}
			m[key] = g
		}
		g.ids[id] = true
	}
	want := map[string]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	for _, s := range pool {
		if want["studio"] && s.Studio != "" {
			add("studio", "Made by "+s.Studio, s.ID)
		}
		if want["year"] && s.Year > 0 {
			add("year", fmt.Sprint("Aired in ", s.Year), s.ID)
		}
		if want["era"] && s.Year > 0 {
			era := "2020 or later"
			switch {
			case s.Year < 2010:
				era = "Before 2010"
			case s.Year < 2015:
				era = "2010 to 2014"
			case s.Year < 2020:
				era = "2015 to 2019"
			}
			add("era", era, s.ID)
		}
		if want["source"] && dailySources[s.Source] != "" {
			add("source", "Based on: "+dailySources[s.Source], s.ID)
			if s.Source != "ORIGINAL" {
				add("source", "Adapted (not original)", s.ID)
			}
		}
		if want["format"] && s.Format != "" && s.Format != "TV" && dailyFormats[s.Format] != "" {
			add("format", dailyFormats[s.Format], s.ID)
		}
		if want["genre"] {
			for _, x := range s.Genres {
				if !pzAdultWord.MatchString(x) {
					add("genre", "Genre: "+x, s.ID)
				}
			}
		}
		if want["tag"] {
			for _, x := range s.Tags {
				if !pzAdultWord.MatchString(x) {
					add("tag", "Theme: "+x, s.ID)
				}
			}
		}
	}
	var out []poolGroup
	for _, g := range m {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].kind+out[i].label < out[j].kind+out[j].label })
	return out
}

func newConnections(pool []guessShow, intn func(int) int) (*connections, error) {
	var groups []poolGroup
	for _, g := range poolGroups(pool, "studio", "year", "source", "format", "tag", "genre") {
		// "Adapted" is nearly everything, and broad genres fit too many shows.
		if len(g.ids) >= 4 && g.label != "Adapted (not original)" && g.label != "Based on: Manga" && len(g.ids) < len(pool)/4 {
			groups = append(groups, g)
		}
	}
	if len(groups) < 4 {
		return nil, fmt.Errorf("not enough groups")
	}
	names := map[int]string{}
	for _, s := range pool {
		names[s.ID] = s.Name
	}
	for range 400 {
		pick := make([]poolGroup, 0, 4)
		kinds := map[string]int{}
		for tries := 0; len(pick) < 4 && tries < 40; tries++ {
			g := groups[intn(len(groups))]
			if kinds[g.kind] >= 2 || slices.ContainsFunc(pick, func(p poolGroup) bool { return p.label == g.label }) {
				continue
			}
			pick = append(pick, g)
			kinds[g.kind]++
		}
		if len(pick) < 4 {
			continue
		}
		c := &connections{names: map[int]string{}}
		used := map[int]bool{}
		ok := true
		for k, g := range pick {
			// Only shows that fit no other group, so every tile has one home.
			var fits []int
			for id := range g.ids {
				if used[id] {
					continue
				}
				only := true
				for j, o := range pick {
					if j != k && o.ids[id] {
						only = false
						break
					}
				}
				if only {
					fits = append(fits, id)
				}
			}
			if len(fits) < 4 {
				ok = false
				break
			}
			sort.Ints(fits)
			shuffle(fits, intn)
			ids := fits[:4]
			for _, id := range ids {
				used[id] = true
				c.names[id] = names[id]
			}
			c.groups = append(c.groups, connGroup{Label: g.label, IDs: slices.Clone(ids)})
		}
		if !ok {
			continue
		}
		for _, g := range c.groups {
			c.left = append(c.left, g.IDs...)
		}
		shuffle(c.left, intn)
		return c, nil
	}
	return nil, fmt.Errorf("couldn't build groups")
}

func (c *connections) clone() actPuzzle {
	n := *c
	n.left, n.found = slices.Clone(c.left), slices.Clone(c.found)
	n.tried = slices.Clone(c.tried)
	return &n
}

func (c *connections) result() (bool, bool) { return c.done, c.won }

func (c *connections) move(m pzMove) error {
	if c.done {
		return nil
	}
	c.note = ""
	if m.GiveUp {
		c.done = true
		return nil
	}
	if len(m.IDs) != 4 {
		return errPz("pick four")
	}
	pick := slices.Clone(m.IDs)
	sort.Ints(pick)
	for k, id := range pick {
		if !slices.Contains(c.left, id) || (k > 0 && pick[k-1] == id) {
			return errPz("pick four different tiles")
		}
	}
	for _, t := range c.tried {
		if slices.Equal(t, pick) {
			return errPz("you tried those four")
		}
	}
	c.tried = append(c.tried, pick)
	for k, g := range c.groups {
		in := 0
		for _, id := range g.IDs {
			if slices.Contains(pick, id) {
				in++
			}
		}
		if in == 4 {
			c.found = append(c.found, k)
			c.left = slices.DeleteFunc(c.left, func(id int) bool { return slices.Contains(g.IDs, id) })
			if len(c.found) == len(c.groups) {
				c.done, c.won = true, true
			}
			return nil
		}
		if in == 3 && !slices.Contains(c.found, k) {
			c.note = "One away!"
		}
	}
	c.mistakes++
	if c.mistakes >= connMistakes {
		c.done = true
	}
	return nil
}

type connTile struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type connFound struct {
	Label string     `json:"label"`
	Tiles []connTile `json:"tiles"`
	Color int        `json:"color"` // which group it is, for its colour
	Found bool       `json:"found"` // false when shown at the end
}

type connView struct {
	Tiles []connTile  `json:"tiles"`
	Found []connFound `json:"found"`
	Left  int         `json:"left"` // mistakes left
	Note  string      `json:"note,omitempty"`
	Done  bool        `json:"done"`
	Won   bool        `json:"won"`
}

func (c *connections) view() any {
	v := connView{Tiles: []connTile{}, Found: []connFound{}, Left: connMistakes - c.mistakes, Note: c.note, Done: c.done, Won: c.won}
	group := func(k int, found bool) connFound {
		f := connFound{Label: c.groups[k].Label, Color: k, Found: found}
		for _, id := range c.groups[k].IDs {
			f.Tiles = append(f.Tiles, connTile{ID: id, Name: c.names[id]})
		}
		return f
	}
	for _, k := range c.found {
		v.Found = append(v.Found, group(k, true))
	}
	if c.done {
		for k := range c.groups {
			if !slices.Contains(c.found, k) {
				v.Found = append(v.Found, group(k, false))
			}
		}
		return v
	}
	for _, id := range c.left {
		v.Tiles = append(v.Tiles, connTile{ID: id, Name: c.names[id]})
	}
	return v
}

// ── Redacted ──

var (
	pzTagRe     = regexp.MustCompile(`<[^>]*>`)
	sourceNote  = regexp.MustCompile(`(?is)[\(\[]\s*(source|written by|from)[^\)\]]*[\)\]]`)
	noteLine    = regexp.MustCompile(`(?im)^\s*(note|notes|source)\s*:.*$`)
	redactWord  = regexp.MustCompile(`[\p{L}\p{N}]+(?:['’][\p{L}]+)?`)
	redactSpace = regexp.MustCompile(`\s+`)
)

// redactOpen are the small words that stay shown.
var redactOpen = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an the and or but nor of to in on at by for with from as into onto upon about over under
		after before during while than then so not no is are was were be been being am it its it's this that these those
		he she they his her hers their theirs him them who whom whose which what when where why how
		has have had having do does did will would shall should can could may might must
		one all any each every some many much more most such there here i my me we our us you your
		up out off also only very just even if because both other own same s t too again once`) {
		redactOpen[w] = true
	}
}

// redactText is a show's description as plain text, short enough to play.
func redactText(desc string) string {
	s := strings.ReplaceAll(desc, "<br>", "\n")
	s = pzTagRe.ReplaceAllString(s, "")
	s = sourceNote.ReplaceAllString(s, "")
	s = noteLine.ReplaceAllString(s, "")
	for _, e := range [][2]string{{"&amp;", "&"}, {"&quot;", `"`}, {"&#039;", "'"}, {"&lt;", "<"}, {"&gt;", ">"}, {"&mdash;", "—"}} {
		s = strings.ReplaceAll(s, e[0], e[1])
	}
	s = strings.TrimSpace(redactSpace.ReplaceAllString(s, " "))
	if len(s) <= 700 {
		return s
	}
	cut := s[:700]
	if k := strings.LastIndexAny(cut, ".!?"); k > 250 {
		return cut[:k+1]
	}
	if k := strings.LastIndex(cut, " "); k > 0 {
		return cut[:k] + "…"
	}
	return cut
}

// wordKey is a word as typed: lower case, no accents.
func wordKey(w string) string {
	return strings.ReplaceAll(normWords(strings.ReplaceAll(w, "’", "'")), " ", "")
}

type redactTok struct {
	Text string `json:"t,omitempty"`
	Len  int    `json:"n,omitempty"` // hidden word's length
	Hit  bool   `json:"h,omitempty"` // just shown by the last guess
	Word bool   `json:"w,omitempty"` // a word (not spaces or marks)
	key  string // what reveals it
}

type redactGuess struct {
	Word string `json:"word"`
	Hits int    `json:"hits"`
}

type redacted struct {
	ans     guessShow
	title   []redactTok
	body    []redactTok
	shown   map[string]bool
	last    string
	guesses []redactGuess
	note    string
	done    bool
	won     bool
}

// redactTokens splits text into words and the bits between them.
func redactTokens(s string) []redactTok {
	var out []redactTok
	at := 0
	for _, m := range redactWord.FindAllStringIndex(s, -1) {
		if m[0] > at {
			out = append(out, redactTok{Text: s[at:m[0]]})
		}
		w := s[m[0]:m[1]]
		out = append(out, redactTok{Text: w, Word: true, key: wordKey(w)})
		at = m[1]
	}
	if at < len(s) {
		out = append(out, redactTok{Text: s[at:]})
	}
	return out
}

func newRedacted(pool []guessShow, intn func(int) int) (*redacted, error) {
	var choices []guessShow
	for _, s := range pool {
		if len(redactText(s.Desc)) >= 160 {
			choices = append(choices, s)
		}
	}
	if len(choices) == 0 {
		return nil, fmt.Errorf("no synopses")
	}
	s := choices[intn(len(choices))]
	return &redacted{ans: s, title: redactTokens(s.Name), body: redactTokens(redactText(s.Desc)), shown: map[string]bool{}}, nil
}

func (r *redacted) clone() actPuzzle {
	n := *r
	n.shown = map[string]bool{}
	for k, v := range r.shown {
		n.shown[k] = v
	}
	n.guesses = slices.Clone(r.guesses)
	return &n
}

func (r *redacted) result() (bool, bool) { return r.done, r.won }

// titleOpen reports whether every title word is shown.
func (r *redacted) titleOpen() bool {
	for _, t := range r.title {
		if t.Word && !redactOpen[t.key] && !r.shown[t.key] {
			return false
		}
	}
	return true
}

func (r *redacted) move(m pzMove) error {
	if r.done {
		return nil
	}
	r.note, r.last = "", ""
	if m.GiveUp {
		r.done = true
		return nil
	}
	text := strings.TrimSpace(m.Text)
	if text == "" {
		return errPz("type a word or the title")
	}
	if guessMatches(text, r.ans.Titles) {
		r.done, r.won = true, true
		return nil
	}
	if len(strings.Fields(text)) > 1 {
		r.note = "Not the title. Guess one word at a time to open the text."
		r.guesses = append([]redactGuess{{Word: text, Hits: -1}}, r.guesses...)
		return nil
	}
	k := wordKey(text)
	if k == "" {
		return errPz("type a word")
	}
	if redactOpen[k] {
		return errPz("small words are already shown")
	}
	if r.shown[k] {
		return errPz("you tried that word")
	}
	r.shown[k], r.last = true, k
	hits := 0
	for _, list := range [][]redactTok{r.title, r.body} {
		for _, t := range list {
			if t.Word && t.key == k {
				hits++
			}
		}
	}
	r.guesses = append([]redactGuess{{Word: text, Hits: hits}}, r.guesses...)
	if r.titleOpen() {
		r.done, r.won = true, true
	}
	return nil
}

type redactView struct {
	Title   []redactTok   `json:"title"`
	Body    []redactTok   `json:"body"`
	Guesses []redactGuess `json:"guesses"`
	Note    string        `json:"note,omitempty"`
	Done    bool          `json:"done"`
	Won     bool          `json:"won"`
	Answer  *activityHit  `json:"answer,omitempty"`
}

func (r *redacted) view() any {
	show := func(list []redactTok) []redactTok {
		out := make([]redactTok, 0, len(list))
		for _, t := range list {
			switch {
			case !t.Word:
				out = append(out, redactTok{Text: t.Text})
			case r.done || redactOpen[t.key] || r.shown[t.key]:
				out = append(out, redactTok{Text: t.Text, Word: true, Hit: t.key == r.last && r.last != ""})
			default:
				out = append(out, redactTok{Len: len([]rune(t.Text)), Word: true})
			}
		}
		return out
	}
	v := redactView{Title: show(r.title), Body: show(r.body), Guesses: slices.Clone(r.guesses), Note: r.note, Done: r.done, Won: r.won}
	if v.Guesses == nil {
		v.Guesses = []redactGuess{}
	}
	if r.done {
		v.Answer = &activityHit{ID: r.ans.ID, Name: r.ans.Name, URL: r.ans.URL}
	}
	return v
}

// ── Grid ──

const gridGuesses = 12

type gridCell struct {
	ID   int    `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Eg   string `json:"eg,omitempty"` // an answer that fit, shown at the end
}

type grid struct {
	rows, cols []poolGroup
	pool       map[int]guessShow
	cells      [9]gridCell
	used       map[int]bool
	left       int
	note       string
	done       bool
	won        bool
}

func newGrid(pool []guessShow, intn func(int) int) (*grid, error) {
	min3 := func(gs []poolGroup) []poolGroup {
		var out []poolGroup
		for _, g := range gs {
			if len(g.ids) >= 6 && g.label != "Adapted (not original)" {
				out = append(out, g)
			}
		}
		return out
	}
	rowsAll := min3(poolGroups(pool, "genre", "tag"))
	colsAll := min3(poolGroups(pool, "studio", "era", "source", "format"))
	if len(rowsAll) < 3 || len(colsAll) < 3 {
		return nil, fmt.Errorf("not enough rules")
	}
	byID := map[int]guessShow{}
	for _, s := range pool {
		byID[s.ID] = s
	}
	pick3 := func(all []poolGroup) []poolGroup {
		var out []poolGroup
		kinds := map[string]int{}
		for tries := 0; len(out) < 3 && tries < 30; tries++ {
			g := all[intn(len(all))]
			if kinds[g.kind] >= 2 || slices.ContainsFunc(out, func(p poolGroup) bool { return p.label == g.label }) {
				continue
			}
			out = append(out, g)
			kinds[g.kind]++
		}
		return out
	}
	for range 3000 {
		rows, cols := pick3(rowsAll), pick3(colsAll)
		if len(rows) < 3 || len(cols) < 3 {
			continue
		}
		ok := true
		for _, r := range rows {
			for _, c := range cols {
				n := 0
				for id := range r.ids {
					if c.ids[id] {
						n++
					}
				}
				if n < 3 {
					ok = false
				}
			}
		}
		if ok {
			return &grid{rows: rows, cols: cols, pool: byID, used: map[int]bool{}, left: gridGuesses}, nil
		}
	}
	return nil, fmt.Errorf("couldn't build a grid")
}

func (g *grid) clone() actPuzzle {
	n := *g
	n.used = map[int]bool{}
	for k, v := range g.used {
		n.used[k] = v
	}
	return &n
}

func (g *grid) result() (bool, bool) { return g.done, g.won }

func (g *grid) fits(cell, id int) bool { return g.rows[cell/3].ids[id] && g.cols[cell%3].ids[id] }

func (g *grid) move(m pzMove) error {
	if g.done {
		return nil
	}
	g.note = ""
	if m.GiveUp {
		g.finish()
		return nil
	}
	if m.Cell < 0 || m.Cell > 8 || g.cells[m.Cell].ID != 0 {
		return errPz("pick an empty square")
	}
	if m.show == nil {
		return errPz("pick a show from the list")
	}
	if g.used[m.show.ID] {
		return errPz("that show is already on the grid")
	}
	g.left--
	if g.fits(m.Cell, m.show.ID) {
		g.cells[m.Cell] = gridCell{ID: m.show.ID, Name: m.show.Name}
		g.used[m.show.ID] = true
	} else {
		g.note = fmt.Sprintf("%s doesn't fit there.", m.show.Name)
	}
	full := true
	for _, c := range g.cells {
		full = full && c.ID != 0
	}
	if full {
		g.won = true
		g.finish()
	} else if g.left <= 0 {
		g.finish()
	}
	return nil
}

// finish ends the grid and fills the empty squares with the most popular
// show that fit.
func (g *grid) finish() {
	g.done = true
	taken := map[int]bool{}
	for k := range g.cells {
		if g.cells[k].ID != 0 {
			continue
		}
		best := guessShow{}
		for id := range g.rows[k/3].ids {
			s := g.pool[id]
			if g.fits(k, id) && !g.used[id] && !taken[id] && (best.ID == 0 || s.Rank < best.Rank) {
				best = s
			}
		}
		g.cells[k].Eg = best.Name
		if best.ID != 0 {
			taken[best.ID] = true
		}
	}
}

type gridView struct {
	Rows  []string   `json:"rows"`
	Cols  []string   `json:"cols"`
	Cells []gridCell `json:"cells"`
	Left  int        `json:"left"`
	Note  string     `json:"note,omitempty"`
	Done  bool       `json:"done"`
	Won   bool       `json:"won"`
}

func (g *grid) view() any {
	v := gridView{Cells: g.cells[:], Left: g.left, Note: g.note, Done: g.done, Won: g.won}
	v.Cells = slices.Clone(v.Cells)
	for _, r := range g.rows {
		v.Rows = append(v.Rows, r.label)
	}
	for _, c := range g.cols {
		v.Cols = append(v.Cols, c.label)
	}
	return v
}

// ── Hangman ──

const hangmanMisses = 6

type hangman struct {
	mode    string // title or character
	word    string
	hint    string
	ans     guessShow
	guessed map[rune]bool
	misses  int
	done    bool
	won     bool
}

// hangmanFold is the word with accents dropped, or "" when it has letters
// outside a–z (Japanese text, say).
func hangmanFold(s string) string {
	f := accents.Replace(s)
	letters := 0
	for _, r := range f {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			letters++
		case unicode.IsLetter(r):
			return ""
		}
	}
	if letters < 4 || len([]rune(f)) > 32 {
		return ""
	}
	return f
}

func newHangman(pool []guessShow, mode string, intn func(int) int) (*hangman, error) {
	if mode != "character" {
		mode = "title"
	}
	type pickT struct {
		word string
		s    guessShow
	}
	var choices []pickT
	for _, s := range pool {
		if mode == "title" {
			// No "Season 2" or "Cour 2": the name alone, as people know it.
			if w := hangmanFold(s.Name); w != "" && !seasonWordRe.MatchString(normWords(s.Name)) && !trailingNumRe.MatchString(normWords(s.Name)) {
				choices = append(choices, pickT{w, s})
			}
			continue
		}
		for _, l := range s.Leads {
			if w := hangmanFold(l); w != "" && !strings.Contains(normWords(s.Name), normWords(l)) {
				choices = append(choices, pickT{w, s})
			}
		}
	}
	if len(choices) == 0 {
		return nil, fmt.Errorf("no words")
	}
	p := choices[intn(len(choices))]
	h := &hangman{mode: mode, word: p.word, ans: p.s, guessed: map[rune]bool{}}
	if mode == "character" {
		h.hint = "A main character from " + p.s.Name
	} else {
		var bits []string
		if f := dailyFormats[p.s.Format]; f != "" {
			bits = append(bits, f)
		}
		if p.s.Year > 0 {
			bits = append(bits, fmt.Sprint(p.s.Year))
		}
		if len(p.s.Genres) > 0 {
			bits = append(bits, strings.Join(p.s.Genres[:min(2, len(p.s.Genres))], ", "))
		}
		h.hint = strings.Join(bits, " · ")
	}
	return h, nil
}

func (h *hangman) clone() actPuzzle {
	n := *h
	n.guessed = map[rune]bool{}
	for k, v := range h.guessed {
		n.guessed[k] = v
	}
	return &n
}

func (h *hangman) result() (bool, bool) { return h.done, h.won }

func (h *hangman) solved() bool {
	for _, r := range strings.ToLower(h.word) {
		if r >= 'a' && r <= 'z' && !h.guessed[r] {
			return false
		}
	}
	return true
}

func (h *hangman) move(m pzMove) error {
	if h.done {
		return nil
	}
	if m.GiveUp {
		h.done = true
		return nil
	}
	if t := strings.TrimSpace(m.Text); t != "" {
		if wordKey(t) == wordKey(h.word) {
			for _, r := range strings.ToLower(h.word) {
				h.guessed[r] = true
			}
			h.done, h.won = true, true
			return nil
		}
		h.misses++
		h.done = h.misses >= hangmanMisses
		return nil
	}
	l := []rune(strings.ToLower(m.Letter))
	if len(l) != 1 || l[0] < 'a' || l[0] > 'z' {
		return errPz("pick a letter")
	}
	if h.guessed[l[0]] {
		return nil
	}
	h.guessed[l[0]] = true
	if !strings.ContainsRune(strings.ToLower(h.word), l[0]) {
		h.misses++
	}
	switch {
	case h.solved():
		h.done, h.won = true, true
	case h.misses >= hangmanMisses:
		h.done = true
	}
	return nil
}

type hangmanView struct {
	Mode   string       `json:"mode"`
	Hint   string       `json:"hint"`
	Words  [][]string   `json:"words"` // per word, each character; "" while hidden
	Hits   string       `json:"hits"`  // letters tried that are in it
	Misses string       `json:"misses"`
	Left   int          `json:"left"`
	Done   bool         `json:"done"`
	Won    bool         `json:"won"`
	Answer *activityHit `json:"answer,omitempty"`
	Word   string       `json:"word,omitempty"`
}

func (h *hangman) view() any {
	v := hangmanView{Mode: h.mode, Hint: h.hint, Words: [][]string{}, Left: hangmanMisses - h.misses, Done: h.done, Won: h.won}
	for _, w := range strings.Fields(h.word) {
		var cs []string
		for _, r := range w {
			lr := unicode.ToLower(r)
			if lr >= 'a' && lr <= 'z' && !h.guessed[lr] && !h.done {
				cs = append(cs, "")
			} else {
				cs = append(cs, string(r))
			}
		}
		v.Words = append(v.Words, cs)
	}
	low := strings.ToLower(h.word)
	for r := 'a'; r <= 'z'; r++ {
		if !h.guessed[r] {
			continue
		}
		if strings.ContainsRune(low, r) {
			v.Hits += string(r)
		} else {
			v.Misses += string(r)
		}
	}
	if h.done {
		v.Answer = &activityHit{ID: h.ans.ID, Name: h.ans.Name, URL: h.ans.URL}
		v.Word = h.word
	}
	return v
}
