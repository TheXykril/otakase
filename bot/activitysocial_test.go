package main

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestQuizQuestions(t *testing.T) {
	pool := pzPool()
	for i := range pool {
		pool[i].Score, pool[i].Episodes = 40+i%55, 12+i%3
		pool[i].Genres = append(pool[i].Genres, "Drama")
	}
	qs, err := newQuiz(pool, quizLen, seeded())
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range qs {
		if len(q.Options) != 4 || q.Answer < 0 || q.Answer > 3 || q.Prompt == "" {
			t.Fatalf("bad question %+v", q)
		}
		seen := map[string]bool{}
		for _, o := range q.Options {
			if seen[o] {
				t.Fatalf("repeated option in %+v", q)
			}
			seen[o] = true
		}
	}
	p := &qset{game: "quiz", qs: qs, need: quizNeed}
	for _, q := range qs {
		p.move(pzMove{Cell: q.Answer})
	}
	if done, won := p.result(); !done || !won || p.right != quizLen {
		t.Fatalf("all right: %v %v %d", done, won, p.right)
	}
	v := p.view().(qsetView)
	if len(v.Review) != quizLen || v.Q != nil {
		t.Errorf("review %d", len(v.Review))
	}
	if err := (&qset{qs: qs}).move(pzMove{Cell: 9}); err == nil {
		t.Error("an answer past the options should fail")
	}
}

func TestTwoTruths(t *testing.T) {
	pool := pzPool()
	qs, err := newTruths(pool, truthsLen, seeded())
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range qs {
		if len(q.Options) != 3 || !strings.HasPrefix(q.Explain, "The truth: ") {
			t.Fatalf("bad facts %+v", q)
		}
		if strings.TrimPrefix(q.Explain, "The truth: ") == q.Options[q.Answer] {
			t.Errorf("the lie is true: %+v", q)
		}
	}
	p := &qset{game: "truths", qs: qs, need: truthsNeed}
	for i, q := range qs {
		pick := q.Answer
		if i < 2 {
			pick = (q.Answer + 1) % 3
		}
		p.move(pzMove{Cell: pick})
	}
	if done, won := p.result(); !done || won {
		t.Errorf("3 of 5 shouldn't win: %v %v", done, won)
	}
}

func TestWordle(t *testing.T) {
	if got := strings.Join(wordleMarks("allee", "apple"), " "); got != "yes near no no yes" {
		t.Errorf("marks %s", got)
	}
	if got := strings.Join(wordleMarks("lllll", "hello"), " "); got != "no no yes yes no" {
		t.Errorf("repeated letters %s", got)
	}
	pool := []guessShow{{ID: 1, Name: "Frieren: Beyond Journey's End", Leads: []string{"Frieren", "Fern Ponytail"}}}
	w, err := newWordle(pool, "title", seeded())
	if err != nil || !slices.Contains([]string{"frieren", "beyond", "journey"}, w.word) {
		t.Fatalf("word %q %v", w.word, err)
	}
	if err := w.move(pzMove{Text: "abc"}); err == nil {
		t.Error("wrong length should fail")
	}
	w.move(pzMove{Text: strings.Repeat("z", len(w.word))})
	if err := w.move(pzMove{Text: strings.Repeat("z", len(w.word))}); err == nil {
		t.Error("the same guess twice should fail")
	}
	w.move(pzMove{Text: strings.ToUpper(w.word)})
	if done, won := w.result(); !done || !won {
		t.Errorf("right word: %v %v", done, won)
	}
	c, _ := newWordle(pool, "character", seeded())
	if c.word != "fern" {
		t.Errorf("character word %q (Frieren is in the title)", c.word)
	}
	if v := c.view().(wordleView); v.Len != 4 || strings.Contains(v.Hint, "Frieren") {
		t.Errorf("view %+v", v)
	}
}

// socialRoom is a room with members, set up for game without its clock.
func socialRoom(t *testing.T, game string, users ...string) (*bot, *togetherRoom, *time.Time) {
	t.Helper()
	b, now := togetherBot(t)
	b.guess.pool = pzPool()
	r := b.together.room("r")
	for i, u := range users {
		r.touch(u, now.Add(time.Duration(i)*time.Second))
	}
	r.game, r.scores = game, map[string]int{}
	if err := b.socialStart(r, game, b.guess.pool, *now); err != nil {
		t.Fatal(err)
	}
	return b, r, now
}

func TestQuizParty(t *testing.T) {
	b, r, now := socialRoom(t, "quiz", "a", "b")
	right := r.quiz.qs[0].Answer
	b.quizAnswer(r, "a", togetherIn{Cell: right}, now.Add(time.Second))
	b.quizAnswer(r, "b", togetherIn{Cell: (right + 1) % 4}, now.Add(2*time.Second))
	b.quizStep(r, *now)
	if r.quiz.isOpen || r.scores["a"] < 900 || r.scores["b"] != 0 {
		t.Fatalf("after reveal: open %v scores %v", r.quiz.isOpen, r.scores)
	}
	v := b.quizView(r, "b", *now)
	if v.Answer != right || v.Mine == right || v.Counts[right] != 1 {
		t.Errorf("view %+v", v)
	}
	*now = now.Add(quizRevealTime)
	b.quizStep(r, *now)
	if r.quiz.i != 1 || !r.quiz.isOpen {
		t.Errorf("next question: %d %v", r.quiz.i, r.quiz.isOpen)
	}
	r.quiz.i = len(r.quiz.qs) - 1
	*now = now.Add(time.Minute)
	b.quizStep(r, *now)
	*now = now.Add(quizRevealTime)
	b.quizStep(r, *now)
	if r.phase != "over" || !slices.Equal(r.winners, []string{"a"}) {
		t.Errorf("game over: %s %v", r.phase, r.winners)
	}
}

func TestImposter(t *testing.T) {
	if _, err := func() (*bot, error) {
		b, _ := togetherBot(t)
		r := b.together.room("r")
		r.touch("a", time.Now())
		return b, b.socialStart(r, "imposter", pzPool(), time.Now())
	}(); err == nil {
		t.Error("imposter needs 3 players")
	}
	b, r, now := socialRoom(t, "imposter", "a", "b", "c")
	g := r.imp
	if g.stage != "talk" || g.show.ID == g.odd.ID || !slices.Contains(g.players, g.imp) {
		t.Fatalf("deal %+v", g)
	}
	if v := b.impView(r, g.imp, *now); v.Show.ID != g.odd.ID {
		t.Error("the imposter should see the other show")
	}
	if err := b.impAct(r, "b", togetherIn{Kind: "vote-now"}, *now, false); err == nil {
		t.Error("only the host skips the talk")
	}
	b.impAct(r, "a", togetherIn{Kind: "vote-now"}, *now, false)
	if g.stage != "vote" {
		t.Fatal("host should start the vote")
	}
	for _, p := range g.players {
		target := g.imp
		if p == g.imp {
			target = g.players[(slices.Index(g.players, p)+1)%3]
		}
		if err := b.impAct(r, p, togetherIn{Kind: "vote", Text: target}, *now, false); err != nil {
			t.Fatal(err)
		}
	}
	b.impStep(r, *now)
	if g.stage != "reveal" || !g.caught {
		t.Fatalf("reveal %s caught %v", g.stage, g.caught)
	}
	for _, p := range g.players {
		if want := map[bool]int{true: 0, false: 1}[p == g.imp]; r.scores[p] != want {
			t.Errorf("%s scored %d", p, r.scores[p])
		}
	}
	imp := g.imp
	*now = now.Add(impRevealTime)
	b.impStep(r, *now)
	if g.round != 2 || g.imp == imp {
		t.Errorf("next round %d imposter %s again", g.round, g.imp)
	}
}

func TestDrawIt(t *testing.T) {
	b, r, now := socialRoom(t, "draw", "a", "b", "c")
	g := r.draw
	if g.drawer != "a" || g.stage != "pick" || len(g.choices) != 3 {
		t.Fatalf("deal %+v", g)
	}
	if err := b.drawAct(r, "b", togetherIn{Kind: "pick"}, *now); err == nil {
		t.Error("only the drawer picks")
	}
	b.drawAct(r, "a", togetherIn{Kind: "pick", Cell: 1}, *now)
	if g.word.ID != g.choices[1].ID || g.stage != "draw" {
		t.Fatal("pick")
	}
	if v := b.drawView(r, "b", *now); v.Word != nil || strings.ContainsAny(v.Mask, "abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("a guesser sees the title: %+v", v)
	}
	b.drawAct(r, "b", togetherIn{Kind: "guess", Text: "nope"}, *now)
	b.drawAct(r, "b", togetherIn{Kind: "guess", Text: g.word.Name}, *now)
	b.drawAct(r, "c", togetherIn{Kind: "guess", Text: g.word.Name}, *now)
	if r.scores["b"] != 2 || r.scores["c"] != 1 || r.scores["a"] != 1 {
		t.Errorf("scores %v", r.scores)
	}
	b.drawStep(r, *now)
	if g.stage != "reveal" {
		t.Error("everyone got it: reveal")
	}
	if v := b.drawView(r, "b", now.Add(time.Minute)); v.Mask != "" || v.Word == nil {
		t.Errorf("reveal view %+v", v)
	}
	*now = now.Add(drawRevealTime)
	b.drawStep(r, *now)
	if g.drawer != "b" || g.round != 2 {
		t.Errorf("next drawer %s", g.drawer)
	}
}

func TestDrawHints(t *testing.T) {
	g := &drawGame{choices: []guessShow{{Name: "Attack on Titan"}}}
	now := time.Now()
	g.pick(0, now)
	if g.hints(now.Add(30*time.Second)) != 0 || g.hints(now.Add(36*time.Second)) != 1 || g.hints(now.Add(79*time.Second)) != 3 {
		t.Errorf("hints %d %d %d", g.hints(now.Add(30*time.Second)), g.hints(now.Add(36*time.Second)), g.hints(now.Add(79*time.Second)))
	}
}

func TestHotPotato(t *testing.T) {
	b, r, now := socialRoom(t, "bomb", "a", "b", "c")
	g := r.bomb
	var fits, misfit guessShow
	for _, s := range r.pool {
		if g.cat.ids[s.ID] && fits.ID == 0 {
			fits = s
		}
		if !g.cat.ids[s.ID] && misfit.ID == 0 {
			misfit = s
		}
	}
	holder := g.alive[g.holder]
	if err := b.bombAct(r, "zz", togetherIn{Text: fits.Name}); err == nil {
		t.Error("only the holder answers")
	}
	if err := b.bombAct(r, holder, togetherIn{Text: misfit.Name}); err == nil || g.alive[g.holder] != holder {
		t.Error("a show that doesn't fit keeps the bomb")
	}
	if err := b.bombAct(r, holder, togetherIn{Text: fits.Name}); err != nil || g.alive[g.holder] == holder {
		t.Fatalf("a show that fits passes it: %v", err)
	}
	next := g.alive[g.holder]
	if err := b.bombAct(r, next, togetherIn{Cell: fits.ID}); err != nil || g.alive[g.holder] != next {
		t.Error("a show already said keeps the bomb")
	}
	*now = g.explode
	b.bombStep(r, *now)
	if g.stage != "boom" || g.boomed != next || slices.Contains(g.alive, next) {
		t.Fatalf("boom %+v", g)
	}
	*now = now.Add(bombBoom)
	b.bombStep(r, *now)
	*now = g.explode
	b.bombStep(r, *now)
	*now = now.Add(bombBoom)
	b.bombStep(r, *now)
	if r.phase != "over" || len(r.winners) != 1 || r.winners[0] == next {
		t.Errorf("last one standing: %s %v", r.phase, r.winners)
	}
}

func TestGuessWho(t *testing.T) {
	b, r, _ := socialRoom(t, "who", "a", "b", "c")
	g := r.who
	if len(g.cards) != whoSize || g.secret[g.players[0]] == g.secret[g.players[1]] {
		t.Fatalf("deal %d cards, secrets %v", len(g.cards), g.secret)
	}
	if err := b.whoAct(r, "c", togetherIn{Kind: "flip", Cell: 1}); err == nil {
		t.Error("a watcher can't flip")
	}
	first, second := g.turn, g.other(g.turn)
	if err := b.whoAct(r, second, togetherIn{Kind: "guess", Cell: 0}); err == nil {
		t.Error("guessing off turn should fail")
	}
	b.whoAct(r, second, togetherIn{Kind: "flip", Cell: 3})
	if v := b.whoView(r, second); !slices.Equal(v.Down, []int{3}) || v.Secret != g.secret[second] {
		t.Errorf("view %+v", v)
	}
	if v := b.whoView(r, "c"); v.Secret != -1 || v.Secrets != nil {
		t.Error("a watcher sees a secret")
	}
	b.whoAct(r, first, togetherIn{Kind: "end"})
	b.whoAct(r, second, togetherIn{Kind: "guess", Cell: g.secret[first]})
	if r.phase != "over" || !slices.Equal(r.winners, []string{second}) {
		t.Errorf("right guess wins: %s %v", r.phase, r.winners)
	}
}
