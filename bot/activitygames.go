package main

import (
	"errors"
	"fmt"
	"image"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// The Activity's other games, played alone: guess the anime, emoji riddles
// and title scramble (type the title; each wrong try or Hint shows the
// next clue), higher or lower, the watch bingo card and the points board.
// Answers stay on the bot until the round ends. A right answer is a game
// point, at most actDailyCap a day from these three; higher or lower keeps
// its own cap.

const actDailyCap = 10

// actWrong is how many wrong tries a round allows.
var actWrong = map[string]int{"guess": 4, "riddle": 3, "scramble": 3}

// soloRound is one member's round of a typed-answer game.
type soloRound struct {
	game   string
	name   string
	url    string
	cover  int // pool show id for the cover, 0 = none
	titles []string
	by     string // riddle author: no points for them
	puzzle string // riddle emojis or scrambled letters
	clues  []string
	first  int // clues shown from the start
	hints  int
	wrong  int
	done   bool
	right  bool
	points int
	img    image.Image // guess: the cover, pixelated per step
}

// shown is how many clues are visible.
func (r *soloRound) shown() int {
	return min(len(r.clues), r.first+r.hints+r.wrong)
}

type soloRounds struct {
	mu sync.Mutex
	m  map[string]*soloRound // user + "/" + game
}

func (s *soloRounds) get(user, game string) *soloRound {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[user+"/"+game]
}

func (s *soloRounds) set(user, game string, r *soloRound) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]*soloRound{}
	}
	s.m[user+"/"+game] = r
}

// soloView is what the page gets: no answer until the round is over.
type soloView struct {
	Game   string   `json:"game"`
	Puzzle string   `json:"puzzle,omitempty"`
	Clues  []string `json:"clues"`
	More   bool     `json:"more"` // a hint is left
	Pic    string   `json:"pic,omitempty"`
	Left   int      `json:"left"` // wrong tries left
	Done   bool     `json:"done"`
	Right  bool     `json:"right"`
	Points int      `json:"points,omitempty"`
	Answer string   `json:"answer,omitempty"`
	URL    string   `json:"url,omitempty"`
	Cover  int      `json:"cover,omitempty"`
	Note   string   `json:"note,omitempty"`
}

func (r *soloRound) view() soloView {
	n := r.shown()
	v := soloView{Game: r.game, Puzzle: r.puzzle, Clues: append([]string{}, r.clues[:n]...), More: n < len(r.clues) && !r.done,
		Left: actWrong[r.game] - r.wrong, Done: r.done, Right: r.right, Points: r.points}
	if r.game == "guess" {
		step := n - 1
		if r.done {
			step = len(guessBlocks) - 1
		}
		v.Pic = fmt.Sprintf("api/play/guess/pic?step=%d&t=%d", step, time.Now().UnixNano())
	}
	if r.done {
		v.Answer, v.URL, v.Cover = r.name, r.url, r.cover
	}
	return v
}

// stripMarkdown drops the ** the chat clues use.
func stripMarkdown(s string) string {
	out := []rune{}
	for _, c := range s {
		if c != '*' && c != '`' {
			out = append(out, c)
		}
	}
	return string(out)
}

func (b *bot) newSoloRound(user, game string) (*soloRound, error) {
	switch game {
	case "guess":
		s, err := b.guess.pick()
		if err != nil {
			return nil, err
		}
		img, err := fetchImage(s.Cover)
		if err != nil {
			return nil, err
		}
		r := &soloRound{game: game, name: s.Name, url: s.URL, cover: s.ID, titles: s.Titles, first: 1, img: img}
		for _, c := range guessClues(s, rand.IntN) {
			r.clues = append(r.clues, stripMarkdown(c))
		}
		return r, nil
	case "riddle":
		rd, ok := b.pickRiddle()
		if !ok {
			return nil, fmt.Errorf("no riddles")
		}
		return &soloRound{game: game, name: rd.Name, url: rd.URL, titles: rd.Titles, by: rd.By, puzzle: rd.Emojis,
			clues: []string{"Title: " + titleBlanks(rd.Name)}}, nil
	case "scramble":
		pool, err := b.guess.shows()
		if err != nil {
			return nil, err
		}
		var choices []guessShow
		for _, s := range pool {
			if scrambleable(s.Name) {
				choices = append(choices, s)
			}
		}
		if len(choices) == 0 {
			return nil, fmt.Errorf("no shows")
		}
		s := choices[rand.IntN(len(choices))]
		return &soloRound{game: game, name: s.Name, url: s.URL, cover: s.ID, titles: s.Titles,
			puzzle: scrambleTitle(s.Name, rand.IntN), clues: []string{"Starts with: " + scrambleHint(s.Name)}}, nil
	}
	return nil, fmt.Errorf("unknown game")
}

// actPoint gives a point for a solo win, unless today's cap is reached.
func (b *bot) actPoint(user, game string) int {
	day := time.Now().UTC().Format(time.DateOnly)
	week := weekKey(time.Now().UTC())
	got := 0
	b.store.update(func(d *storeData) {
		if d.Games == nil {
			d.Games = map[string]gameScore{}
		}
		s := d.Games[user]
		if s.ActDay != day {
			s.ActDay, s.ActToday = day, 0
		}
		if s.ActToday >= actDailyCap {
			return
		}
		s.ActToday++
		d.Games[user] = s
		addGamePoints(d, user, game, 1, week)
		got = 1
	})
	return got
}

// soloAnswer checks a try; give up ends the round.
func (b *bot) soloAnswer(user string, r *soloRound, text string, giveUp bool) {
	if r.done {
		return
	}
	switch {
	case giveUp:
		r.done = true
	case guessMatches(text, r.titles):
		r.done, r.right = true, true
		if r.by != user {
			r.points = b.actPoint(user, r.game)
		}
	default:
		r.wrong++
		if r.wrong >= actWrong[r.game] {
			r.done = true
		}
	}
}

func init() {
	apiHandle("POST /activity/api/play/{game}/start", (*bot).actSoloStart)
	apiHandle("POST /activity/api/play/{game}/hint", (*bot).actSoloHint)
	apiHandle("POST /activity/api/play/{game}/answer", (*bot).actSoloAnswer)
	apiHandle("GET /activity/api/play/guess/pic", (*bot).actGuessPic)
	apiHandle("POST /activity/api/hl/start", (*bot).actHLStart)
	apiHandle("POST /activity/api/hl/answer", (*bot).actHLAnswer)
	apiHandle("GET /activity/api/bingo", (*bot).actBingo)
	apiHandle("GET /activity/api/board", (*bot).actBoard)
}

// actUser is the signed-in member, answering 401 when there is none.
func (b *bot) actUser(w http.ResponseWriter, r *http.Request) string {
	u := b.acts.user(r)
	if u == "" {
		apiError(w, http.StatusUnauthorized, "sign in again")
		return ""
	}
	if ban, ok := b.gameBanned(u); ok {
		apiError(w, http.StatusForbidden, benchText(ban))
		return ""
	}
	return u
}

func (b *bot) actSoloStart(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	game := r.PathValue("game")
	if _, ok := actWrong[game]; !ok {
		apiError(w, http.StatusNotFound, "no such game")
		return
	}
	if b.gameOff(game) {
		apiError(w, http.StatusForbidden, "staff turned this game off")
		return
	}
	round, err := b.newSoloRound(user, game)
	if err != nil {
		apiError(w, http.StatusBadGateway, "couldn't reach AniList, try again in a bit")
		return
	}
	b.solo.set(user, game, round)
	writeJSON(w, http.StatusOK, round.view())
}

func (b *bot) actSoloHint(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	round := b.solo.get(user, r.PathValue("game"))
	if round == nil {
		apiError(w, http.StatusNotFound, "start a round first")
		return
	}
	b.solo.mu.Lock()
	if !round.done && round.shown() < len(round.clues) {
		round.hints++
	}
	v := round.view()
	b.solo.mu.Unlock()
	writeJSON(w, http.StatusOK, v)
}

func (b *bot) actSoloAnswer(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	var in struct {
		Text   string `json:"text"`
		GiveUp bool   `json:"give_up"`
	}
	if !readJSON(w, r, 4<<10, &in) {
		return
	}
	round := b.solo.get(user, r.PathValue("game"))
	if round == nil {
		apiError(w, http.StatusNotFound, "start a round first")
		return
	}
	b.solo.mu.Lock()
	b.soloAnswer(user, round, in.Text, in.GiveUp)
	v := round.view()
	b.solo.mu.Unlock()
	if v.Right && round.by == user {
		v.Note = "Your own riddle, so no point for this one."
	}
	writeJSON(w, http.StatusOK, v)
}

// actGuessPic is the member's guess cover at a step they can see.
func (b *bot) actGuessPic(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	round := b.solo.get(user, "guess")
	step, err := strconv.Atoi(r.URL.Query().Get("step"))
	if round == nil || err != nil || step < 0 || step >= len(guessBlocks) {
		http.NotFound(w, r)
		return
	}
	b.solo.mu.Lock()
	allowed := round.shown() - 1
	if round.done {
		allowed = len(guessBlocks) - 1
	}
	img := round.img
	b.solo.mu.Unlock()
	step = min(step, allowed)
	// The sharpest picture goes with the last clue, as in chat.
	k := len(guessBlocks) - len(round.clues) + step
	png, err := pixelate(img, guessBlocks[max(0, min(k, len(guessBlocks)-1))])
	if err != nil {
		http.Error(w, "couldn't draw", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}

// ── Higher or lower ──

type actHLShow struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

type actHLView struct {
	Stat   string    `json:"stat"`
	A      actHLShow `json:"a"`
	B      actHLShow `json:"b"`
	Streak int       `json:"streak"`
	Best   int       `json:"best"`
	Over   bool      `json:"over"`
	Right  bool      `json:"right"`
	Shown  actHLShow `json:"shown,omitempty"` // the show just answered, with its value
}

func hlShow(s guessShow, stat string, value bool) actHLShow {
	v := actHLShow{ID: s.ID, Name: s.Name}
	if value {
		v.Value = hlValueText(s, stat)
	}
	return v
}

func (b *bot) actHLStart(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	var in struct {
		Stat string `json:"stat"`
	}
	if !readJSON(w, r, 1<<10, &in) {
		return
	}
	if in.Stat != "popularity" {
		in.Stat = "score"
	}
	if b.gameOff("hl") {
		apiError(w, http.StatusForbidden, "staff turned this game off")
		return
	}
	pool, err := b.guess.shows()
	if err != nil {
		apiError(w, http.StatusBadGateway, "couldn't reach AniList, try again in a bit")
		return
	}
	var a guessShow
	for range 20 {
		if a = pool[rand.IntN(len(pool))]; hlValue(a, in.Stat) > 0 {
			break
		}
	}
	next, ok := hlNext(pool, a, in.Stat, nil, rand.IntN)
	if !ok {
		apiError(w, http.StatusBadGateway, "couldn't find shows, try again")
		return
	}
	run := &hlRun{user: user, stat: in.Stat, a: a, b: next, seen: []int{a.ID, next.ID}, last: time.Now()}
	b.hl.mu.Lock()
	if b.hl.runs == nil {
		b.hl.runs = map[string]*hlRun{}
	}
	b.hl.runs["act:"+user] = run
	b.hl.mu.Unlock()
	var best int
	b.store.view(func(d *storeData) { best = d.Games[user].HLBest })
	writeJSON(w, http.StatusOK, actHLView{Stat: run.stat, A: hlShow(a, run.stat, true), B: hlShow(next, run.stat, false), Best: best})
}

func (b *bot) actHLAnswer(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	var in struct {
		Higher bool `json:"higher"`
	}
	if !readJSON(w, r, 1<<10, &in) {
		return
	}
	pool, _ := b.guess.shows()
	h := &b.hl
	h.mu.Lock()
	run := h.runs["act:"+user]
	if run == nil {
		h.mu.Unlock()
		apiError(w, http.StatusNotFound, "start a run first")
		return
	}
	answered := run.b
	v := actHLView{Stat: run.stat, Shown: hlShow(answered, run.stat, true)}
	v.Right = hlRight(run.a, run.b, run.stat, in.Higher)
	next, ok := guessShow{}, false
	if v.Right {
		run.streak++
		run.a = run.b
		if next, ok = hlNext(pool, run.a, run.stat, run.seen, rand.IntN); ok {
			run.b = next
			run.seen = append(run.seen, next.ID)
		}
	}
	v.Over = !v.Right || !ok
	if v.Over {
		delete(h.runs, "act:"+user)
	}
	streak := run.streak
	v.Streak = streak
	v.A, v.B = hlShow(run.a, run.stat, true), hlShow(run.b, run.stat, false)
	h.mu.Unlock()
	if v.Right && streak%hlEvery == 0 {
		b.hlPoint(user)
	}
	if v.Over {
		v.Best = b.hlBest(user, streak)
	} else {
		b.store.view(func(d *storeData) { v.Best = d.Games[user].HLBest })
	}
	writeJSON(w, http.StatusOK, v)
}

// ── Bingo and the board ──

func (b *bot) actBingo(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	now := time.Now().UTC()
	card := bingoCard(bingoMonth(now))
	type square struct {
		Label string `json:"label"`
		Show  string `json:"show,omitempty"`
		Free  bool   `json:"free,omitempty"`
	}
	out := struct {
		Month   string   `json:"month"`
		Linked  bool     `json:"linked"`
		Private bool     `json:"private,omitempty"`
		Squares []square `json:"squares"`
		Lines   int      `json:"lines"`
		Full    bool     `json:"full"`
		Got     int      `json:"got,omitempty"`
	}{Month: now.Format("January")}
	var link anilistLink
	b.store.view(func(d *storeData) { link, out.Linked = d.AniList[user] })
	fill := make([]string, 9)
	if out.Linked {
		entries, err := anilistLists.get(link.ID)
		switch {
		case errors.Is(err, errPrivateList):
			out.Private = true
		case err != nil:
			apiError(w, http.StatusBadGateway, "couldn't reach AniList, try again in a bit")
			return
		default:
			fill = bingoFill(card, entries, now)
			out.Got = b.bingoAward(user, fill, now)
		}
	}
	lines, full := bingoScore(fill)
	out.Lines, out.Full = bitCount(lines), full
	for k, s := range card {
		if s == nil {
			out.Squares = append(out.Squares, square{Label: "Free", Free: true})
			continue
		}
		out.Squares = append(out.Squares, square{Label: s.Label, Show: fill[k]})
	}
	writeJSON(w, http.StatusOK, out)
}

func (b *bot) actBoard(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	type row struct {
		Name   string `json:"name"`
		Points int    `json:"points"`
		Me     bool   `json:"me,omitempty"`
	}
	var weekly, all []guessRank
	var champs []string
	b.store.view(func(d *storeData) {
		weekly, all = ranked(weekPoints(d, weekKey(time.Now().UTC())), 10), ranked(allPoints(d), 10)
		champs = append(champs, d.Champions...)
	})
	rows := func(list []guessRank) []row {
		out := []row{}
		for _, x := range list {
			out = append(out, row{Name: b.dashName(x.User), Points: x.Points, Me: x.User == user})
		}
		return out
	}
	var champNames []string
	for _, c := range champs {
		champNames = append(champNames, b.dashName(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"week": rows(weekly), "all": rows(all), "champions": champNames})
}
