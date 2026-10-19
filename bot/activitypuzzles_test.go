package main

import (
	"fmt"
	"image"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

// pzPool is a pool with enough studios, years, sources and genres to build
// every puzzle.
func pzPool() []guessShow {
	studios := []string{"MAPPA", "Bones", "Madhouse", "Wit Studio", "Kyoto Animation", "Sunrise"}
	sources := []string{"MANGA", "LIGHT_NOVEL", "ORIGINAL", "VIDEO_GAME", "WEB_NOVEL"}
	genres := []string{"Action", "Comedy", "Drama", "Romance", "Sports", "Mecha", "Fantasy"}
	formats := []string{"TV", "TV", "TV", "MOVIE", "ONA"}
	var pool []guessShow
	for i := 1; i <= 240; i++ {
		name := fmt.Sprintf("Show %c%c Story", 'A'+i%26, 'a'+i/26)
		s := guessShow{ID: i, Name: name, Titles: []string{name}, Year: 2000 + i%25, Studio: studios[i%len(studios)],
			Source: sources[i%len(sources)], Format: formats[i%len(formats)], Genres: []string{genres[i%len(genres)], genres[(i/7)%len(genres)]},
			Rank: i, Leads: []string{fmt.Sprintf("Hero %c%c", 'A'+i%26, 'a'+i/26)},
			Desc: strings.Repeat("A quiet hero walks through the ruined city looking for answers. ", 4) + name + " begins."}
		if i%9 == 0 {
			s.Tags = []string{"Isekai"}
		}
		pool = append(pool, s)
	}
	return pool
}

func seeded() func(int) int { return rand.New(rand.NewPCG(1, 2)).IntN }

func TestCoverle(t *testing.T) {
	pool := pzPool()
	c := newCoverle(pool[0], image.NewRGBA(image.Rect(0, 0, 60, 90)), func() float64 { return 0.5 })
	if err := c.move(pzMove{}); err == nil {
		t.Error("a move without a show should fail")
	}
	wrong := pool[1]
	c.move(pzMove{show: &wrong})
	if err := c.move(pzMove{show: &wrong}); err == nil {
		t.Error("the same show twice should fail")
	}
	if c.step() != 1 || c.done {
		t.Fatalf("after a miss: step %d done %v", c.step(), c.done)
	}
	right := pool[0]
	c.move(pzMove{show: &right})
	if done, won := c.result(); !done || !won || c.tries[1].Close != 100 {
		t.Fatalf("right answer: %v %v %+v", done, won, c.tries)
	}
	if _, err := zoomPic(c.img, c.cx, c.cy, coverleZoom[0]); err != nil {
		t.Error(err)
	}
	a := guessShow{ID: 1, Year: 2016, Studio: "MAPPA", Genres: []string{"Action", "Drama"}, Format: "TV"}
	g := guessShow{ID: 2, Year: 2014, Studio: "Bones", Genres: []string{"Action"}, Format: "TV"}
	tr := coverleCompare(g, a)
	if tr.YearOK != "near" || tr.Arrow != "up" || tr.StuOK != "no" || tr.GenOK != "near" || tr.Genres != "1/2" || tr.Close <= 0 || tr.Close >= 100 {
		t.Errorf("compare: %+v", tr)
	}
}

func TestTimeline(t *testing.T) {
	tl, err := newTimeline(pzPool(), seeded())
	if err != nil {
		t.Fatal(err)
	}
	if len(tl.shows) != timelineShows || slices.Equal(tl.order, tl.ids()) {
		t.Fatalf("shows %d order %v", len(tl.shows), tl.order)
	}
	for k := 1; k < len(tl.shows); k++ {
		if tl.shows[k].Year <= tl.shows[k-1].Year {
			t.Fatal("years must rise")
		}
	}
	if err := tl.move(pzMove{IDs: tl.order[:3]}); err == nil {
		t.Error("a short order should fail")
	}
	tl.move(pzMove{IDs: slices.Clone(tl.order)})
	if tl.done || tl.tries != 1 {
		t.Fatal("one wrong try shouldn't end it")
	}
	tl.move(pzMove{IDs: tl.ids()})
	if done, won := tl.result(); !done || !won {
		t.Fatal("the right order wins")
	}
	v := tl.view().(timelineView)
	if len(v.Answer) != timelineShows || v.Items[0].Year == 0 {
		t.Errorf("view %+v", v)
	}
}

func TestConnections(t *testing.T) {
	c, err := newConnections(pzPool(), seeded())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.groups) != 4 || len(c.left) != 16 {
		t.Fatalf("groups %d tiles %d", len(c.groups), len(c.left))
	}
	// Every tile has one home.
	seen := map[int]bool{}
	for _, g := range c.groups {
		for _, id := range g.IDs {
			if seen[id] {
				t.Fatal("tile in two groups")
			}
			seen[id] = true
		}
	}
	mixed := []int{c.groups[0].IDs[0], c.groups[0].IDs[1], c.groups[0].IDs[2], c.groups[1].IDs[0]}
	c.move(pzMove{IDs: mixed})
	if c.mistakes != 1 || c.note != "One away!" {
		t.Errorf("one away: %d %q", c.mistakes, c.note)
	}
	if err := c.move(pzMove{IDs: mixed}); err == nil {
		t.Error("the same four twice should fail")
	}
	for _, g := range c.groups {
		c.move(pzMove{IDs: g.IDs})
	}
	if done, won := c.result(); !done || !won {
		t.Fatal("all four groups win")
	}
	if v := c.view().(connView); len(v.Found) != 4 || len(v.Tiles) != 0 {
		t.Errorf("view %+v", v)
	}
}

func TestRedacted(t *testing.T) {
	r, err := newRedacted(pzPool(), seeded())
	if err != nil {
		t.Fatal(err)
	}
	v := r.view().(redactView)
	for _, tok := range v.Body {
		if tok.Word && tok.Text != "" && !redactOpen[wordKey(tok.Text)] {
			t.Fatalf("%q should be hidden", tok.Text)
		}
	}
	r.move(pzMove{Text: "hero"})
	if r.guesses[0].Hits != 4 {
		t.Errorf("hero hits: %+v", r.guesses)
	}
	if err := r.move(pzMove{Text: "Hero"}); err == nil {
		t.Error("the same word twice should fail")
	}
	if err := r.move(pzMove{Text: "the"}); err == nil {
		t.Error("small words are shown already")
	}
	for _, w := range strings.Fields(r.ans.Name) {
		r.move(pzMove{Text: w})
	}
	if done, won := r.result(); !done || !won {
		t.Fatal("opening every title word wins")
	}
	if got := redactText("Some text.<br><br>(Source: Crunchyroll)<i>Note: x</i>"); got != "Some text." {
		t.Errorf("redactText: %q", got)
	}
}

func TestGrid(t *testing.T) {
	pool := pzPool()
	g, err := newGrid(pool, seeded())
	if err != nil {
		t.Fatal(err)
	}
	var fit, miss guessShow
	for _, s := range pool {
		if fit.ID == 0 && g.fits(0, s.ID) {
			fit = s
		}
		if miss.ID == 0 && !g.fits(0, s.ID) {
			miss = s
		}
	}
	g.move(pzMove{Cell: 0, show: &miss})
	if g.cells[0].ID != 0 || g.left != gridGuesses-1 || g.note == "" {
		t.Fatalf("a miss: %+v", g.cells[0])
	}
	g.move(pzMove{Cell: 0, show: &fit})
	if g.cells[0].ID != fit.ID {
		t.Fatal("a fit fills the square")
	}
	if err := g.move(pzMove{Cell: 0, show: &fit}); err == nil {
		t.Error("a filled square can't take another")
	}
	g.move(pzMove{GiveUp: true})
	v := g.view().(gridView)
	if !v.Done || v.Cells[1].Eg == "" {
		t.Errorf("give up shows answers: %+v", v.Cells)
	}
}

func TestHangman(t *testing.T) {
	h, err := newHangman(pzPool(), "title", seeded())
	if err != nil {
		t.Fatal(err)
	}
	if h.hint == "" {
		t.Error("no hint")
	}
	h.move(pzMove{Letter: "q"})
	h.move(pzMove{Letter: "q"})
	if h.misses != 1 || h.done {
		t.Fatalf("misses %d", h.misses)
	}
	for _, r := range strings.ToLower(h.word) {
		h.move(pzMove{Letter: string(r)})
	}
	if done, won := h.result(); !done || !won {
		t.Fatal("all letters win")
	}
	c, err := newHangman(pzPool(), "character", seeded())
	if err != nil || !strings.HasPrefix(c.word, "Hero") {
		t.Fatalf("character: %v %q", err, c.word)
	}
	c.move(pzMove{Text: c.word})
	if _, won := c.result(); !won {
		t.Error("typing the whole name wins")
	}
	if hangmanFold("進撃の巨人") != "" || hangmanFold("Pokémon") != "Pokemon" {
		t.Error("fold")
	}
}

func TestTogetherPuzzle(t *testing.T) {
	b, now := togetherBot(t)
	b.guess.pool = pzPool()
	for _, u := range []string{"a", "b"} {
		togetherPost(b, (*bot).togetherJoin, u, togetherIn{Room: "r"}, nil)
	}
	var v togetherView
	if code := togetherPost(b, (*bot).togetherStart, "a", togetherIn{Room: "r", Game: "timeline"}, &v); code != 200 || v.Phase != "pz" {
		t.Fatalf("start: %d %+v", code, v)
	}
	room := b.together.m["r"]
	for range 100 {
		b.together.mu.Lock()
		ready := room.pzBase != nil
		b.together.mu.Unlock()
		if ready {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	b.together.mu.Lock()
	ids := room.pzBase.(*timeline).ids()
	b.together.mu.Unlock()
	var res struct{ View togetherView }
	togetherPost(b, (*bot).togetherMove, "b", map[string]any{"room": "r", "ids": ids}, &res)
	if res.View.Pz == nil || res.View.Pz.Board == nil || !res.View.Pz.Board.Won || res.View.Pz.Winner == "" {
		t.Fatalf("b solves: %+v", res.View.Pz)
	}
	b.together.mu.Lock()
	if room.scores["b"] != 1 {
		t.Errorf("scores %v", room.scores)
	}
	// Time runs out: a's board ends too.
	*now = now.Add(2 * time.Minute)
	b.togetherPzStep(room, *now)
	if !room.roundOver || !room.pzBoards["a"].done {
		t.Errorf("round over %v", room.roundOver)
	}
	b.together.mu.Unlock()
}

func TestActivityAvatar(t *testing.T) {
	b := testGamesBot(t)
	for _, id := range []string{"../x", "12", "123456789012345678"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/activity/api/avatar/"+id, nil)
		r.SetPathValue("id", id)
		b.activityAvatar(w, r)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404 without Discord", id, w.Code)
		}
	}
}
