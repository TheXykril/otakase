package main

import (
	"math/rand/v2"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Party games made for a call: a quiz show where everyone answers the same
// question at once (quiz, or two truths and a lie), imposter (everyone has
// the same show but one), draw it (one draws a title, the rest guess), hot
// potato (name a show in the category before the bomb goes off) and guess
// who (1v1, find the other's character). They run in the same rooms as the
// other party games (activityparty.go); the most points, or the last one
// standing, takes a game point under the daily cap.

const (
	quizPartyLen    = 8
	quizAnswerTime  = 15 * time.Second
	truthAnswerTime = 20 * time.Second
	quizRevealTime  = 5 * time.Second

	impRounds     = 3
	impTalkTime   = 90 * time.Second
	impVoteTime   = 30 * time.Second
	impRevealTime = 8 * time.Second

	drawPickTime   = 12 * time.Second
	drawTime       = 80 * time.Second
	drawRevealTime = 6 * time.Second
	drawMaxPoints  = 60000

	bombMinFuse = 20 * time.Second
	bombMaxFuse = 45 * time.Second
	bombBoom    = 4 * time.Second
)

// socialGames are these games and how many players each needs.
var socialGames = map[string]int{"quiz": 1, "truths": 1, "imposter": 3, "draw": 2, "bomb": 2, "who": 2, "koikoi": 1}

// socialNames are what a too-small room is told.
var socialNames = map[string]string{"imposter": "Imposter", "draw": "Draw it", "bomb": "Hot potato", "who": "Guess who"}

// partyPeople are the members in the order they joined.
func (r *togetherRoom) partyPeople() []string {
	ids := make([]string, 0, len(r.members))
	for id := range r.members {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, c := r.members[ids[i]], r.members[ids[j]]
		if !a.joined.Equal(c.joined) {
			return a.joined.Before(c.joined)
		}
		return ids[i] < ids[j]
	})
	return ids
}

// finishPay ends a game and gives its winners a game point.
func (b *bot) finishPay(r *togetherRoom) {
	r.finish()
	for _, w := range r.winners {
		b.actPoint(w, r.game)
	}
}

// socialStart sets a room up for one of these games. Hold the lock.
func (b *bot) socialStart(r *togetherRoom, game string, pool []guessShow, now time.Time) error {
	people := r.partyPeople()
	if len(people) < socialGames[game] {
		return errPz(socialNames[game] + " needs at least " + strconv.Itoa(socialGames[game]) + " players")
	}
	r.pool = pool
	r.quiz, r.imp, r.draw, r.bomb, r.who, r.kk = nil, nil, nil, nil, nil, nil
	switch game {
	case "quiz", "truths":
		var qs []quizQ
		var err error
		if game == "quiz" {
			qs, err = newQuiz(pool, quizPartyLen, rand.IntN)
		} else {
			qs, err = newTruths(pool, truthsLen, rand.IntN)
		}
		if err != nil {
			return errPz("couldn't make questions, try again")
		}
		r.phase = "quiz"
		r.quiz = &quizParty{qs: qs}
		r.quiz.open(game, now)
	case "imposter":
		r.phase = "imposter"
		r.imp = &impGame{players: people, rig: r.impRig}
		if !r.imp.next(pool, now) {
			return errPz("couldn't pick shows, try again")
		}
	case "draw":
		r.phase = "draw"
		r.draw = &drawGame{players: people, rounds: min(8, max(len(people), 4))}
		r.draw.next(pool, now)
	case "bomb":
		r.phase = "bomb"
		r.bomb = &bombGame{alive: people, used: map[int]bool{}, cats: map[string]bool{}}
		if !r.bomb.fuse(pool, now) {
			return errPz("couldn't pick a category, try again")
		}
	case "who":
		cards := whoCards(pool, rand.IntN)
		if len(cards) < whoSize {
			return errPz("couldn't find enough characters, try again")
		}
		r.phase = "who"
		a, c := people[0], people[1]
		if rand.IntN(2) == 0 {
			a, c = c, a
		}
		r.who = &whoGame{cards: cards, players: [2]string{a, c}, turn: a,
			secret: map[string]int{}, down: map[string]map[int]bool{a: {}, c: {}}}
		r.who.secret[a] = rand.IntN(len(cards))
		r.who.secret[c] = (r.who.secret[a] + 1 + rand.IntN(len(cards)-1)) % len(cards)
	case "koikoi":
		return b.kkStart(r, pool, now)
	}
	return nil
}

// socialStep is a tick for these games. Hold the lock.
func (b *bot) socialStep(r *togetherRoom, now time.Time) (string, bool) {
	switch r.phase {
	case "quiz":
		b.quizStep(r, now)
	case "imposter":
		b.impStep(r, now)
	case "draw":
		b.drawStep(r, now)
	case "bomb":
		b.bombStep(r, now)
	case "who":
		b.whoStep(r)
	case "koikoi":
		b.kkStep(r, now)
	}
	return "", r.phase != "over"
}

// socialView fills in the view for these games. Hold the lock.
func (b *bot) socialView(r *togetherRoom, v *togetherView, user string, now time.Time) {
	switch {
	case r.quiz != nil:
		v.Quiz = b.quizView(r, user, now)
	case r.imp != nil:
		v.Imp = b.impView(r, user, now)
	case r.draw != nil:
		v.Draw = b.drawView(r, user, now)
	case r.bomb != nil:
		v.Bomb = b.bombView(r, user, now)
	case r.who != nil:
		v.Who = b.whoView(r, user)
	case r.kk != nil:
		v.KK = b.kkView(r, user, now)
	}
}

// person is a member as a view shows them.
func (b *bot) person(r *togetherRoom, id string) togetherPerson {
	return togetherPerson{ID: id, Name: b.roomName(r, id)}
}

// ── Quiz show ──

type quizParty struct {
	qs     []quizQ
	i      int
	isOpen bool
	start  time.Time
	ends   time.Time
	total  time.Duration
	ans    map[string]int
	at     map[string]time.Time
	gain   map[string]int
}

func (q *quizParty) open(game string, now time.Time) {
	q.total = quizAnswerTime
	if game == "truths" {
		q.total = truthAnswerTime
	}
	q.isOpen, q.start, q.ends = true, now, now.Add(q.total)
	q.ans, q.at, q.gain = map[string]int{}, map[string]time.Time{}, map[string]int{}
}

func (b *bot) quizStep(r *togetherRoom, now time.Time) {
	q := r.quiz
	if q.isOpen {
		all := len(r.members) > 0
		for id := range r.members {
			if _, ok := q.ans[id]; !ok {
				all = false
			}
		}
		if !all && now.Before(q.ends) {
			return
		}
		// Right answers score more the faster they came: 500 to 1000.
		for id, a := range q.ans {
			if a != q.qs[q.i].Answer {
				continue
			}
			left := q.ends.Sub(q.at[id])
			g := 500 + int(500*left/q.total)/10*10
			q.gain[id] = g
			r.scores[id] += g
		}
		q.isOpen, q.ends = false, now.Add(quizRevealTime)
		r.changed()
		return
	}
	if now.Before(q.ends) {
		return
	}
	if q.i+1 >= len(q.qs) {
		b.finishPay(r)
		return
	}
	q.i++
	q.open(r.game, now)
	r.changed()
}

type quizPartyView struct {
	Index     int              `json:"index"`
	Total     int              `json:"total"`
	Q         quizQ            `json:"q"`
	Open      bool             `json:"open"`
	Left      float64          `json:"left"`
	Time      float64          `json:"time"`
	Answer    int              `json:"answer"` // -1 while open
	Mine      int              `json:"mine"`   // -1 when not answered
	Gain      int              `json:"gain,omitempty"`
	Explain   string           `json:"explain,omitempty"`
	Counts    []int            `json:"counts,omitempty"`
	Players   []togetherPerson `json:"players"`
	Standings []togetherPerson `json:"standings"`
}

func (b *bot) quizView(r *togetherRoom, user string, now time.Time) *quizPartyView {
	q := r.quiz
	cur := q.qs[q.i]
	v := &quizPartyView{Index: q.i + 1, Total: len(q.qs), Q: cur, Open: q.isOpen, Left: secs(q.ends.Sub(now)), Time: secs(q.total),
		Answer: -1, Mine: -1, Players: []togetherPerson{}, Standings: b.togetherStandings(r)}
	if a, ok := q.ans[user]; ok {
		v.Mine = a
	}
	if !q.isOpen {
		v.Answer, v.Explain, v.Gain = cur.Answer, cur.Explain, q.gain[user]
		v.Counts = make([]int, len(cur.Options))
		for _, a := range q.ans {
			v.Counts[a]++
		}
		v.Time = secs(quizRevealTime)
	}
	for _, id := range r.partyPeople() {
		p := b.person(r, id)
		_, p.Voted = q.ans[id]
		if !q.isOpen {
			right := q.ans[id] == cur.Answer && p.Voted
			p.Right, p.Points = &right, q.gain[id]
		}
		v.Players = append(v.Players, p)
	}
	return v
}

func (b *bot) quizAnswer(r *togetherRoom, user string, in togetherIn, now time.Time) error {
	q := r.quiz
	if q == nil || !q.isOpen {
		return errPz("no question is open")
	}
	if _, ok := q.ans[user]; ok {
		return nil
	}
	if in.Cell < 0 || in.Cell >= len(q.qs[q.i].Options) {
		return errPz("pick an answer")
	}
	q.ans[user], q.at[user] = in.Cell, now
	r.changed()
	return nil
}

// ── Imposter ──

type impGame struct {
	players []string
	round   int
	imp     string
	past    []string
	show    guessShow
	odd     guessShow
	stage   string // talk, vote, reveal
	ends    time.Time
	votes   map[string]string
	caught  bool
	rig     string // an admin's pick: always the imposter
}

// next deals a round: one show for everyone, a similar one for the imposter.
func (g *impGame) next(pool []guessShow, now time.Time) bool {
	top := pool[:min(len(pool), 250)]
	for range 400 {
		a, c := top[rand.IntN(len(top))], top[rand.IntN(len(top))]
		if a.ID == c.ID || a.Studio == c.Studio || a.Format != c.Format {
			continue
		}
		// Close enough to bluff: two genres in common.
		shared := 0
		for _, x := range c.Genres {
			if slices.Contains(a.Genres, x) {
				shared++
			}
		}
		if shared < min(2, len(a.Genres)) || shared == 0 {
			continue
		}
		g.show, g.odd = a, c
		var fresh []string
		for _, p := range g.players {
			if !slices.Contains(g.past, p) {
				fresh = append(fresh, p)
			}
		}
		if len(fresh) == 0 {
			fresh = g.players
		}
		g.imp = fresh[rand.IntN(len(fresh))]
		if slices.Contains(g.players, g.rig) {
			g.imp = g.rig
		}
		g.past = append(g.past, g.imp)
		g.round++
		g.stage, g.ends, g.votes, g.caught = "talk", now.Add(impTalkTime), map[string]string{}, false
		return true
	}
	return false
}

func (b *bot) impStep(r *togetherRoom, now time.Time) {
	g := r.imp
	// Players who left can't vote; a round without its imposter ends.
	var here []string
	for _, p := range g.players {
		if _, ok := r.members[p]; ok {
			here = append(here, p)
		}
	}
	g.players = here
	if len(here) < 2 {
		b.finishPay(r)
		return
	}
	switch g.stage {
	case "talk":
		if now.Before(g.ends) {
			return
		}
		g.stage, g.ends = "vote", now.Add(impVoteTime)
	case "vote":
		all := true
		for _, p := range g.players {
			if _, ok := g.votes[p]; !ok {
				all = false
			}
		}
		if !all && now.Before(g.ends) {
			return
		}
		count := map[string]int{}
		for _, t := range g.votes {
			count[t]++
		}
		top, best := "", 0
		for id, n := range count {
			if n > best {
				top, best = id, n
			} else if n == best {
				top = "" // a tie: nobody is voted out
			}
		}
		g.caught = top == g.imp && top != ""
		for _, p := range g.players {
			switch {
			case g.caught && p != g.imp:
				r.scores[p]++
			case !g.caught && p == g.imp:
				r.scores[p] += 2
			}
		}
		g.stage, g.ends = "reveal", now.Add(impRevealTime)
	case "reveal":
		if now.Before(g.ends) {
			return
		}
		if g.round >= impRounds {
			b.finishPay(r)
			return
		}
		if !g.next(r.pool, now) {
			b.finishPay(r)
			return
		}
	}
	r.changed()
}

type impView struct {
	Round     int              `json:"round"`
	Rounds    int              `json:"rounds"`
	Stage     string           `json:"stage"`
	Left      float64          `json:"left"`
	Time      float64          `json:"time"`
	In        bool             `json:"in"`
	Show      *activityHit     `json:"show,omitempty"` // mine
	Main      *activityHit     `json:"main,omitempty"` // on reveal
	Odd       *activityHit     `json:"odd,omitempty"`  // on reveal
	Imposter  string           `json:"imposter,omitempty"`
	Caught    bool             `json:"caught,omitempty"`
	Mine      string           `json:"mine,omitempty"` // my vote
	Players   []togetherPerson `json:"players"`
	Standings []togetherPerson `json:"standings"`
}

func hitOf(s guessShow) *activityHit { return &activityHit{ID: s.ID, Name: s.Name, URL: s.URL} }

func (b *bot) impView(r *togetherRoom, user string, now time.Time) *impView {
	g := r.imp
	v := &impView{Round: g.round, Rounds: impRounds, Stage: g.stage, Left: secs(g.ends.Sub(now)), In: slices.Contains(g.players, user),
		Mine: g.votes[user], Players: []togetherPerson{}, Standings: b.togetherStandings(r)}
	v.Time = secs(map[string]time.Duration{"talk": impTalkTime, "vote": impVoteTime, "reveal": impRevealTime}[g.stage])
	if v.In {
		v.Show = hitOf(g.show)
		if user == g.imp {
			v.Show = hitOf(g.odd)
		}
	}
	count := map[string]int{}
	for _, t := range g.votes {
		count[t]++
	}
	for _, id := range g.players {
		p := b.person(r, id)
		_, p.Voted = g.votes[id]
		if g.stage == "reveal" {
			p.Points = count[id]
			bad := id == g.imp
			p.Out = bad
		}
		v.Players = append(v.Players, p)
	}
	if g.stage == "reveal" {
		v.Main, v.Odd, v.Imposter, v.Caught = hitOf(g.show), hitOf(g.odd), g.imp, g.caught
	}
	return v
}

func (b *bot) impAct(r *togetherRoom, user string, in togetherIn, now time.Time, admin bool) error {
	g := r.imp
	switch in.Kind {
	case "vote-now":
		if r.host() != user && !admin {
			return errPz("only the host can do that")
		}
		if g.stage == "talk" {
			g.stage, g.ends = "vote", now.Add(impVoteTime)
			r.changed()
		}
		return nil
	case "vote":
		if g.stage != "vote" {
			return errPz("voting isn't open")
		}
		if !slices.Contains(g.players, user) {
			return errPz("you're watching this round")
		}
		if in.Text == user || !slices.Contains(g.players, in.Text) {
			return errPz("vote for someone else")
		}
		g.votes[user] = in.Text
		r.changed()
		return nil
	}
	return errPz("unknown move")
}

// ── Draw it ──

// inkStroke is one line: a colour, a width and points as x, y pairs from 0
// to 1000. ID ties the pieces of a stroke still being drawn together.
type inkStroke struct {
	ID int   `json:"id"`
	C  int   `json:"c"`
	W  int   `json:"w"`
	P  []int `json:"p"`
}

type drawGame struct {
	players []string
	rounds  int
	round   int
	drawer  string
	stage   string // pick, draw, reveal
	choices []guessShow
	word    guessShow
	start   time.Time
	ends    time.Time
	hintAt  []int // letter positions opened as hints, in order
	ink     []inkStroke
	inkGen  int
	inkPts  int
	got     []string
	feed    []togetherFeed
}

// drawWords are titles that can be drawn: popular, short, in English letters.
func drawWords(pool []guessShow) []guessShow {
	var out []guessShow
	for _, s := range pool[:min(len(pool), 300)] {
		if hangmanFold(s.Name) != "" && len([]rune(s.Name)) <= 24 && !seasonWordRe.MatchString(normWords(s.Name)) && !trailingNumRe.MatchString(normWords(s.Name)) {
			out = append(out, s)
		}
	}
	return out
}

func (g *drawGame) next(pool []guessShow, now time.Time) {
	g.round++
	g.drawer = g.players[(g.round-1)%len(g.players)]
	words := drawWords(pool)
	g.choices = pickN(words, 3, rand.IntN)
	g.stage, g.ends = "pick", now.Add(drawPickTime)
	g.ink, g.inkPts, g.got, g.feed = nil, 0, nil, nil
	g.inkGen++
}

func (g *drawGame) pick(k int, now time.Time) {
	if k < 0 || k >= len(g.choices) {
		k = 0
	}
	g.word = g.choices[k]
	g.stage, g.start, g.ends = "draw", now, now.Add(drawTime)
	var pos []int
	for i, c := range []rune(g.word.Name) {
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			pos = append(pos, i)
		}
	}
	shuffle(pos, rand.IntN)
	g.hintAt = pos[:len(pos)/3]
}

// hints is how many hint letters are open: one at 35 s, then every 15 s.
func (g *drawGame) hints(now time.Time) int {
	t := now.Sub(g.start)
	if g.stage != "draw" || t < 35*time.Second {
		return 0
	}
	return min(len(g.hintAt), 1+int((t-35*time.Second)/(15*time.Second)))
}

func (b *bot) drawStep(r *togetherRoom, now time.Time) {
	g := r.draw
	if _, ok := r.members[g.drawer]; !ok && g.stage != "reveal" {
		// The drawer left: show the answer and move on.
		g.stage, g.ends = "reveal", now.Add(drawRevealTime)
		if g.word.ID == 0 && len(g.choices) > 0 {
			g.word = g.choices[0]
		}
		r.changed()
		return
	}
	switch g.stage {
	case "pick":
		if now.Before(g.ends) {
			return
		}
		g.pick(0, now)
	case "draw":
		guessers := 0
		for id := range r.members {
			if id != g.drawer {
				guessers++
			}
		}
		if (guessers == 0 || len(g.got) < guessers) && now.Before(g.ends) {
			if g.hints(now) != g.hints(now.Add(-250*time.Millisecond)) {
				r.changed()
			}
			return
		}
		g.stage, g.ends = "reveal", now.Add(drawRevealTime)
	case "reveal":
		if now.Before(g.ends) {
			return
		}
		var here []string
		for _, p := range g.players {
			if _, ok := r.members[p]; ok {
				here = append(here, p)
			}
		}
		g.players = here
		if g.round >= g.rounds || len(here) < 2 {
			b.finishPay(r)
			return
		}
		g.next(r.pool, now)
	}
	r.changed()
}

type drawView struct {
	Round     int              `json:"round"`
	Rounds    int              `json:"rounds"`
	Stage     string           `json:"stage"`
	Left      float64          `json:"left"`
	Time      float64          `json:"time"`
	Drawer    togetherPerson   `json:"drawer"`
	Mine      bool             `json:"mine"` // I draw
	Choices   []string         `json:"choices,omitempty"`
	Word      *activityHit     `json:"word,omitempty"`
	Mask      string           `json:"mask,omitempty"` // _ for letters still hidden
	Got       bool             `json:"got"`
	InkGen    int              `json:"ink_gen"`
	InkN      int              `json:"ink_n"`
	InkPts    int              `json:"ink_pts"`
	Feed      []togetherFeed   `json:"feed"`
	Players   []togetherPerson `json:"players"`
	Standings []togetherPerson `json:"standings"`
}

func (b *bot) drawView(r *togetherRoom, user string, now time.Time) *drawView {
	g := r.draw
	v := &drawView{Round: g.round, Rounds: g.rounds, Stage: g.stage, Left: secs(g.ends.Sub(now)), Drawer: b.person(r, g.drawer),
		Mine: user == g.drawer, Got: slices.Contains(g.got, user), InkGen: g.inkGen, InkN: len(g.ink), InkPts: g.inkPts,
		Feed: append([]togetherFeed{}, g.feed...), Players: []togetherPerson{}, Standings: b.togetherStandings(r)}
	v.Time = secs(map[string]time.Duration{"pick": drawPickTime, "draw": drawTime, "reveal": drawRevealTime}[g.stage])
	if g.stage == "pick" && v.Mine {
		for _, c := range g.choices {
			v.Choices = append(v.Choices, c.Name)
		}
	}
	if g.stage == "reveal" || (g.stage == "draw" && (v.Mine || v.Got)) {
		v.Word = hitOf(g.word)
	}
	if g.stage == "draw" {
		open := map[int]bool{}
		for _, p := range g.hintAt[:g.hints(now)] {
			open[p] = true
		}
		var m strings.Builder
		for i, c := range []rune(g.word.Name) {
			if (unicode.IsLetter(c) || unicode.IsDigit(c)) && !open[i] {
				m.WriteRune('_')
			} else {
				m.WriteRune(c)
			}
		}
		v.Mask = m.String()
	}
	for _, id := range g.players {
		p := b.person(r, id)
		p.Host = id == g.drawer
		p.Voted = slices.Contains(g.got, id)
		v.Players = append(v.Players, p)
	}
	return v
}

func (b *bot) drawAct(r *togetherRoom, user string, in togetherIn, now time.Time) error {
	g := r.draw
	switch in.Kind {
	case "pick":
		if user != g.drawer || g.stage != "pick" {
			return errPz("it's not your turn to pick")
		}
		g.pick(in.Cell, now)
		r.changed()
		return nil
	case "guess":
		if g.stage != "draw" {
			return errPz("no drawing to guess")
		}
		if user == g.drawer {
			return errPz("you're drawing")
		}
		if slices.Contains(g.got, user) || strings.TrimSpace(in.Text) == "" {
			return nil
		}
		if guessMatches(in.Text, g.word.Titles) {
			pts := 1
			if len(g.got) == 0 {
				pts = 2
				r.scores[g.drawer]++
			}
			r.scores[user] += pts
			g.got = append(g.got, user)
			g.feed = append(g.feed, togetherFeed{Name: b.roomName(r, user), Text: "got it", OK: true})
		} else {
			text := strings.TrimSpace(in.Text)
			if len([]rune(text)) > 40 {
				text = string([]rune(text)[:40]) + "…"
			}
			g.feed = append(g.feed, togetherFeed{Name: b.feedName(r, user, false), Text: r.said(user, text)})
		}
		if len(g.feed) > 10 {
			g.feed = g.feed[len(g.feed)-10:]
		}
		r.changed()
		return nil
	}
	return errPz("unknown move")
}

// ── Hot potato ──

type bombGame struct {
	alive   []string
	out     []string
	holder  int
	cat     poolGroup
	cats    map[string]bool // categories used this game
	used    map[int]bool
	said    []string
	explode time.Time
	stage   string // play, boom
	ends    time.Time
	boomed  string
	feed    []togetherFeed
}

// fuse lights a new bomb with a fresh category. It goes off at a hidden
// moment.
func (g *bombGame) fuse(pool []guessShow, now time.Time) bool {
	var cats []poolGroup
	for _, c := range poolGroups(pool, "studio", "year", "genre", "tag", "source") {
		// Themes only when common, so everyone knows a few.
		need := 10
		if c.kind == "tag" {
			need = 30
		}
		if len(c.ids) >= need && !g.cats[c.label] && c.label != "Adapted (not original)" {
			cats = append(cats, c)
		}
	}
	if len(cats) == 0 {
		return false
	}
	g.cat = cats[rand.IntN(len(cats))]
	g.cats[g.cat.label] = true
	g.used, g.said, g.feed = map[int]bool{}, nil, nil
	g.stage = "play"
	g.explode = now.Add(bombMinFuse + time.Duration(rand.Int64N(int64(bombMaxFuse-bombMinFuse))))
	return true
}

func (b *bot) bombStep(r *togetherRoom, now time.Time) {
	g := r.bomb
	// Members who left are out.
	for i := 0; i < len(g.alive); i++ {
		if _, ok := r.members[g.alive[i]]; !ok {
			g.out = append(g.out, g.alive[i])
			g.alive = slices.Delete(g.alive, i, i+1)
			if g.holder > i || g.holder >= len(g.alive) {
				g.holder = max(0, g.holder-1)
			}
			i--
			r.changed()
		}
	}
	if len(g.alive) <= 1 {
		r.winners = append([]string{}, g.alive...)
		b.finishBomb(r)
		return
	}
	switch g.stage {
	case "play":
		if now.Before(g.explode) {
			return
		}
		// Rigged: the fuse burns on until the bomb reaches them.
		if slices.Contains(g.alive, r.bombRig) && g.alive[g.holder] != r.bombRig && now.Sub(g.explode) < bombRigMax {
			return
		}
		g.boomed = g.alive[g.holder]
		g.out = append(g.out, g.boomed)
		g.alive = slices.Delete(g.alive, g.holder, g.holder+1)
		g.holder %= max(1, len(g.alive))
		g.stage, g.ends = "boom", now.Add(bombBoom)
		r.changed()
	case "boom":
		if now.Before(g.ends) {
			return
		}
		if len(g.alive) <= 1 || !g.fuse(r.pool, now) {
			b.finishBomb(r)
			return
		}
		r.changed()
	}
}

// finishBomb ends hot potato: the last one standing wins.
func (b *bot) finishBomb(r *togetherRoom) {
	g := r.bomb
	r.scores = map[string]int{}
	for i, id := range g.out {
		r.scores[id] = i
	}
	for _, id := range g.alive {
		r.scores[id] = len(g.out) + 1
	}
	b.finishPay(r)
}

type bombView struct {
	Stage    string           `json:"stage"`
	Category string           `json:"category"`
	Holder   togetherPerson   `json:"holder"`
	Mine     bool             `json:"mine"`
	In       bool             `json:"in"`
	Said     []string         `json:"said"`
	Feed     []togetherFeed   `json:"feed"`
	Boomed   string           `json:"boomed,omitempty"`
	Left     float64          `json:"left"`
	Players  []togetherPerson `json:"players"`
}

func (b *bot) bombView(r *togetherRoom, user string, now time.Time) *bombView {
	g := r.bomb
	v := &bombView{Stage: g.stage, Category: g.cat.label, Said: append([]string{}, g.said...), Feed: append([]togetherFeed{}, g.feed...),
		In: slices.Contains(g.alive, user), Left: secs(g.ends.Sub(now)), Players: []togetherPerson{}}
	if len(g.alive) > 0 {
		v.Holder = b.person(r, g.alive[g.holder%len(g.alive)])
		v.Mine = v.Holder.ID == user && g.stage == "play"
	}
	if g.boomed != "" {
		v.Boomed = b.roomName(r, g.boomed)
	}
	for _, id := range g.alive {
		p := b.person(r, id)
		p.Host = id == v.Holder.ID
		v.Players = append(v.Players, p)
	}
	for i := len(g.out) - 1; i >= 0; i-- {
		p := b.person(r, g.out[i])
		p.Out = true
		v.Players = append(v.Players, p)
	}
	return v
}

// poolFind is the pool show a typed title or a picked id names: an exact
// title first, then a near one.
func poolFind(pool []guessShow, id int, text string) (guessShow, bool) {
	for _, s := range pool {
		if id > 0 && s.ID == id {
			return s, true
		}
	}
	if id > 0 {
		return guessShow{}, false
	}
	c := compact(text)
	for _, s := range pool {
		for _, t := range s.Titles {
			if compact(t) == c {
				return s, true
			}
		}
	}
	for _, s := range pool {
		if guessMatches(text, s.Titles) {
			return s, true
		}
	}
	return guessShow{}, false
}

func (b *bot) bombAct(r *togetherRoom, user string, in togetherIn) error {
	g := r.bomb
	if g.stage != "play" || len(g.alive) == 0 || g.alive[g.holder] != user {
		return errPz("you don't have the bomb")
	}
	s, ok := poolFind(r.pool, in.Cell, in.Text)
	say := func(text string, ok bool) {
		g.feed = append(g.feed, togetherFeed{Name: b.feedName(r, user, ok), Text: text, OK: ok})
		if len(g.feed) > 6 {
			g.feed = g.feed[len(g.feed)-6:]
		}
	}
	switch {
	case !ok:
		say(r.said(user, strings.TrimSpace(in.Text))+": not one of the shows here", false)
	case g.used[s.ID]:
		say(s.Name+": already said", false)
	case !g.cat.ids[s.ID]:
		say(s.Name+": doesn't fit", false)
	default:
		g.used[s.ID] = true
		g.said = append(g.said, s.Name)
		say(s.Name, true)
		g.holder = (g.holder + 1) % len(g.alive)
	}
	r.changed()
	if !ok || !g.cat.ids[s.ID] {
		return errPz("doesn't count, try another")
	}
	return nil
}

// ── Guess who ──

const whoSize = 24

type whoCard struct {
	Name   string `json:"name"`
	Show   string `json:"show"`
	ShowID int    `json:"show_id"`
	K      int    `json:"k"` // which lead of the show, for the picture
	Face   bool   `json:"face"`
}

type whoGame struct {
	cards   []whoCard
	players [2]string
	turn    string
	secret  map[string]int
	down    map[string]map[int]bool
	winner  string
	missed  bool // the game ended on a wrong guess
}

// whoCards are characters from different popular shows, pictures first.
func whoCards(pool []guessShow, intn func(int) int) []whoCard {
	var with, without []whoCard
	for _, s := range pool[:min(len(pool), 300)] {
		for k, l := range s.Leads {
			if strings.Contains(normWords(s.Name), normWords(l)) {
				continue
			}
			c := whoCard{Name: l, Show: s.Name, ShowID: s.ID, K: k, Face: k < len(s.Faces) && s.Faces[k] != ""}
			if c.Face {
				with = append(with, c)
			} else {
				without = append(without, c)
			}
			break
		}
	}
	shuffle(with, intn)
	shuffle(without, intn)
	cards := append(with, without...)
	return cards[:min(len(cards), whoSize)]
}

func (g *whoGame) other(id string) string {
	if g.players[0] == id {
		return g.players[1]
	}
	return g.players[0]
}

func (b *bot) whoStep(r *togetherRoom) {
	g := r.who
	for _, p := range g.players {
		if _, ok := r.members[p]; !ok && g.winner == "" {
			g.winner = g.other(p)
		}
	}
	if g.winner != "" {
		r.scores = map[string]int{g.winner: 1}
		b.finishPay(r)
	}
}

type whoView struct {
	Cards   []whoCard        `json:"cards"`
	Players []togetherPerson `json:"players"`
	Turn    string           `json:"turn"`
	In      bool             `json:"in"`
	Secret  int              `json:"secret"` // mine, or -1
	Down    []int            `json:"down"`   // cards I flipped down
	Winner  string           `json:"winner,omitempty"`
	Missed  bool             `json:"missed,omitempty"`
	Secrets map[string]int   `json:"secrets,omitempty"` // once over
}

func (b *bot) whoView(r *togetherRoom, user string) *whoView {
	g := r.who
	v := &whoView{Cards: g.cards, Turn: g.turn, Secret: -1, Down: []int{}, Players: []togetherPerson{}, Missed: g.missed}
	for _, p := range g.players {
		pp := b.person(r, p)
		pp.Points = len(g.cards) - len(g.down[p]) // cards still up
		v.Players = append(v.Players, pp)
		if p == user {
			v.In = true
			v.Secret = g.secret[p]
			for k := range g.down[p] {
				v.Down = append(v.Down, k)
			}
			sort.Ints(v.Down)
		}
	}
	if g.winner != "" {
		v.Winner, v.Secrets = g.winner, g.secret
	}
	return v
}

func (b *bot) whoAct(r *togetherRoom, user string, in togetherIn) error {
	g := r.who
	if g.winner != "" {
		return nil
	}
	if g.secret == nil || (user != g.players[0] && user != g.players[1]) {
		return errPz("you're watching this game")
	}
	if in.Cell < 0 || in.Cell >= len(g.cards) {
		return errPz("pick a card")
	}
	switch in.Kind {
	case "flip":
		d := g.down[user]
		if d[in.Cell] {
			delete(d, in.Cell)
		} else {
			d[in.Cell] = true
		}
	case "end":
		if g.turn != user {
			return errPz("it's not your turn")
		}
		g.turn = g.other(user)
	case "guess":
		if g.turn != user {
			return errPz("guess on your turn")
		}
		if in.Cell == g.secret[g.other(user)] {
			g.winner = user
		} else {
			g.winner, g.missed = g.other(user), true
		}
		b.whoStep(r)
	default:
		return errPz("unknown move")
	}
	r.changed()
	return nil
}

// ── Routes ──

func init() {
	apiHandle("POST /activity/api/party/act", (*bot).togetherAct)
	apiHandle("POST /activity/api/party/ink", (*bot).togetherInk)
	apiHandle("GET /activity/api/party/ink", (*bot).togetherInkGet)
	apiHandle("GET /activity/api/face/{show}/{k}", (*bot).activityFace)
}

// togetherAct is a move in one of these games.
func (b *bot) togetherAct(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.togetherRead(w, r)
	if !ok {
		return
	}
	b.together.mu.Lock()
	room := b.together.m[in.Room]
	if room == nil {
		b.together.mu.Unlock()
		apiError(w, http.StatusConflict, "no game is running")
		return
	}
	err := room.blocked(user)
	if err == nil && (room.phase == "draw" && in.Kind == "guess" || room.phase == "bomb") {
		err = room.cooled(user, togetherNow())
	}
	if err != nil && in.Kind != "watch" {
		b.together.mu.Unlock()
		apiError(w, http.StatusConflict, err.Error())
		return
	}
	admin := room.admins[user]
	now := togetherNow()
	room.touch(user, now)
	if room.scores == nil {
		room.scores = map[string]int{}
	}
	err = nil
	switch room.phase {
	case "quiz":
		err = b.quizAnswer(room, user, in, now)
	case "imposter":
		err = b.impAct(room, user, in, now, admin)
	case "draw":
		err = b.drawAct(room, user, in, now)
	case "bomb":
		err = b.bombAct(room, user, in)
	case "who":
		err = b.whoAct(room, user, in)
	case "koikoi":
		err = b.kkAct(room, user, in, now)
	default:
		err = errPz("no game is running")
	}
	v := b.togetherViewOf(room, user, now)
	b.together.mu.Unlock()
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"view": v})
}

// togetherInk adds the drawer's new lines, or clears or undoes.
func (b *bot) togetherInk(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	var in struct {
		Room    string      `json:"room"`
		Strokes []inkStroke `json:"strokes"`
		Clear   bool        `json:"clear"`
		Undo    bool        `json:"undo"`
	}
	if !readJSON(w, r, 128<<10, &in) {
		return
	}
	b.together.mu.Lock()
	defer b.together.mu.Unlock()
	room := b.together.m[in.Room]
	if room == nil || room.draw == nil || room.phase != "draw" || room.draw.stage != "draw" || room.draw.drawer != user {
		apiError(w, http.StatusConflict, "you're not drawing")
		return
	}
	if err := room.blocked(user); err != nil {
		apiError(w, http.StatusConflict, err.Error())
		return
	}
	g := room.draw
	room.touch(user, togetherNow())
	switch {
	case in.Clear:
		g.ink, g.inkPts = nil, 0
		g.inkGen++
	case in.Undo:
		if n := len(g.ink); n > 0 {
			g.inkPts -= len(g.ink[n-1].P) / 2
			g.ink = g.ink[:n-1]
		}
		g.inkGen++
	}
	for _, s := range in.Strokes {
		pts := s.P[:len(s.P)/2*2]
		if g.inkPts+len(pts)/2 > drawMaxPoints {
			break
		}
		for i := range pts {
			pts[i] = max(0, min(1000, pts[i]))
		}
		s.C, s.W = max(0, min(7, s.C)), max(1, min(40, s.W))
		if n := len(g.ink); n > 0 && g.ink[n-1].ID == s.ID {
			g.ink[n-1].P = append(g.ink[n-1].P, pts...)
		} else {
			s.P = pts
			g.ink = append(g.ink, s)
		}
		g.inkPts += len(pts) / 2
	}
	room.changed()
	writeJSON(w, http.StatusOK, map[string]int{"ink_gen": g.inkGen, "ink_n": len(g.ink)})
}

// togetherInkGet is the drawing from stroke `from` on.
func (b *bot) togetherInkGet(w http.ResponseWriter, r *http.Request) {
	if b.actUser(w, r) == "" {
		return
	}
	q := r.URL.Query()
	from, _ := strconv.Atoi(q.Get("from"))
	b.together.mu.Lock()
	defer b.together.mu.Unlock()
	room := b.together.m[q.Get("room")]
	if room == nil || room.draw == nil {
		http.NotFound(w, r)
		return
	}
	g := room.draw
	from = max(0, min(from, len(g.ink)))
	writeJSON(w, http.StatusOK, map[string]any{"gen": g.inkGen, "from": from, "strokes": append([]inkStroke{}, g.ink[from:]...)})
}

// activityFace passes a main character's picture through.
func (b *bot) activityFace(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("show"))
	k, err2 := strconv.Atoi(r.PathValue("k"))
	if err != nil || err2 != nil {
		http.NotFound(w, r)
		return
	}
	s, ok := b.dailyShowByID(id)
	if !ok || k < 0 || k >= len(s.Faces) {
		http.NotFound(w, r)
		return
	}
	proxyAniList(w, r, s.Faces[k])
}
