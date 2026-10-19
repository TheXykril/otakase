package main

import (
	"math/rand/v2"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Koi-Koi: hanafuda, the flower card game, with anime covers. The 48 cards
// keep their 12 months (one show per month, so matching means the same
// show) and their kinds: brights, animals, ribbons and plains. You play a
// card onto one of the same month to take both, then flip one from the deck
// the same way. Taking a combo (yaku) lets you stop and score it, or call
// koi-koi and play on for more, but if someone else stops first you get
// nothing. One deck seats 4, so a call splits into tables of the size the
// host picks, all playing at once; bots fill the empty seats and take over
// for anyone who leaves. After 3 hands the best total wins.

const (
	kkHands      = 3
	kkTurnTime   = 45 * time.Second
	kkDecideTime = 20 * time.Second
	kkBotTime    = 1500 * time.Millisecond
	kkBetween    = 8 * time.Second
)

// kkKinds are each month's four cards: Bright, Animal, Ribbon, Plain.
var kkKinds = [12]string{"BRPP", "ARPP", "BRPP", "ARPP", "ARPP", "ARPP", "ARPP", "BAPP", "ARPP", "ARPP", "BARP", "BPPP"}

// Cards worth a name in a combo.
const (
	kkCurtain   = 2 * 4  // cherry blossom curtain
	kkMoon      = 7 * 4  // full moon
	kkRainMan   = 10 * 4 // willow, the rain man
	kkSake      = 8 * 4  // chrysanthemum sake cup
	kkButterfly = 5 * 4
	kkBoar      = 6 * 4
	kkDeer      = 9 * 4
)

var (
	kkPoem = []int{0*4 + 1, 1*4 + 1, 2*4 + 1} // red poem ribbons
	kkBlue = []int{5*4 + 1, 8*4 + 1, 9*4 + 1} // blue ribbons
)

func kkMonth(c int) int     { return c / 4 }
func kkKind(c int) byte     { return kkKinds[c/4][c%4] }
func kkMatch(a, c int) bool { return kkMonth(a) == kkMonth(c) }

// kkValue is how much a bot wants a card.
func kkValue(c int) int {
	switch kkKind(c) {
	case 'B':
		return 20
	case 'A':
		return 10
	case 'R':
		return 6
	}
	if c == kkSake {
		return 10
	}
	return 1
}

type kkYaku struct {
	Name   string `json:"name"`
	Points int    `json:"points"`
}

// kkScore is every combo in a pile of taken cards and their points.
func kkScore(got []int) ([]kkYaku, int) {
	has := map[int]bool{}
	n := map[byte]int{}
	for _, c := range got {
		has[c] = true
		n[kkKind(c)]++
	}
	all := func(cs ...int) bool {
		for _, c := range cs {
			if !has[c] {
				return false
			}
		}
		return true
	}
	var ys []kkYaku
	add := func(name string, p int) { ys = append(ys, kkYaku{name, p}) }
	switch b := n['B']; {
	case b == 5:
		add("Five brights", 10)
	case b == 4 && !has[kkRainMan]:
		add("Four brights", 8)
	case b == 4:
		add("Rainy four brights", 7)
	case b == 3 && !has[kkRainMan]:
		add("Three brights", 5)
	}
	if all(kkBoar, kkDeer, kkButterfly) {
		add("Boar, deer, butterfly", 5)
	}
	if n['A'] >= 5 {
		add("Animals", n['A']-4)
	}
	poem, blue := all(kkPoem...), all(kkBlue...)
	switch {
	case poem && blue:
		add("Red and blue ribbons", 10)
	case poem:
		add("Red poem ribbons", 5)
	case blue:
		add("Blue ribbons", 5)
	}
	if n['R'] >= 5 {
		add("Ribbons", n['R']-4)
	}
	plains := n['P']
	if has[kkSake] {
		plains++ // the sake cup counts as a plain too
	}
	if plains >= 10 {
		add("Plains", plains-9)
	}
	if all(kkCurtain, kkSake) {
		add("Flower viewing", 5)
	}
	if all(kkMoon, kkSake) {
		add("Moon viewing", 5)
	}
	total := 0
	for _, y := range ys {
		total += y.Points
	}
	return ys, total
}

type kkSeat struct {
	id    string // a member, or bot:N
	name  string
	bot   bool
	hand  []int
	got   []int
	koi   bool // called koi-koi this hand
	last  int  // combo points when they last chose to play on
	total int
}

type kkResult struct {
	Seat    int      `json:"seat"` // -1 when nobody scored
	Points  int      `json:"points"`
	Yaku    []kkYaku `json:"yaku"`
	Big     bool     `json:"big,omitempty"` // 7 or more, doubled
	Koi     bool     `json:"koi,omitempty"` // doubled: someone else had called koi-koi
	Through string   `json:"through,omitempty"`
}

type kkTable struct {
	seats    []*kkSeat
	deck     []int
	field    []int
	turn     int
	dealer   int
	stage    string // play, decide, done (a hand is over), finished
	hand     int
	deadline time.Time
	log      string
	last     []int // the cards that just moved
	result   *kkResult
	results  []kkResult // every hand so far
	talk     bool       // bots talk back (an admin's prank)
	say      string     // the last thing a bot said
}

type kkGame struct {
	tables []*kkTable
	shows  []guessShow // one per month
	watch  map[string]int
	size   int
}

// kkBotNames name the bots.
var kkBotNames = []string{"Hana", "Tsuki", "Kaze", "Yuki", "Sora", "Kumo", "Ame", "Hoshi"}

// newKoiKoi seats people at tables of size and deals the first hands.
func newKoiKoi(people []string, size int, pool []guessShow, name func(string) string, intn func(int) int) (*kkGame, bool) {
	var shows []guessShow
	for _, s := range pool[:min(len(pool), 200)] {
		if s.Cover != "" {
			shows = append(shows, s)
		}
	}
	if len(shows) < 12 {
		return nil, false
	}
	shuffle(shows, intn)
	g := &kkGame{shows: shows[:12], watch: map[string]int{}, size: size}
	people = slices.Clone(people)
	shuffle(people, intn)
	n := (len(people) + size - 1) / size
	for range n {
		g.tables = append(g.tables, &kkTable{})
	}
	for i, p := range people {
		t := g.tables[i%n]
		t.seats = append(t.seats, &kkSeat{id: p, name: name(p)})
	}
	bots := 0
	for _, t := range g.tables {
		for len(t.seats) < size {
			t.seats = append(t.seats, &kkSeat{id: "bot:" + strconv.Itoa(bots), name: kkBotNames[bots%len(kkBotNames)] + " (bot)", bot: true})
			bots++
		}
		shuffle(t.seats, intn)
		t.dealer = intn(len(t.seats))
	}
	return g, true
}

// kkDeal is how many cards each hand and the field get.
func kkDeal(players int) (hand, field int) {
	switch players {
	case 3:
		return 7, 6
	case 4:
		return 5, 8
	}
	return 8, 8
}

// deal starts a hand. A field with a whole month, or a hand with one,
// is dealt again.
func (t *kkTable) deal(now time.Time, intn func(int) int) {
	hand, field := kkDeal(len(t.seats))
	for {
		deck := make([]int, 48)
		for i := range deck {
			deck[i] = i
		}
		shuffle(deck, intn)
		ok := four(deck[:field])
		for i, s := range t.seats {
			s.hand = slices.Clone(deck[field+i*hand : field+(i+1)*hand])
			sort.Ints(s.hand)
			ok = ok && four(s.hand)
			s.got, s.koi, s.last = nil, false, 0
		}
		t.field = slices.Clone(deck[:field])
		t.deck = slices.Clone(deck[field+len(t.seats)*hand:])
		if ok {
			break
		}
	}
	t.hand++
	t.turn = t.dealer
	t.stage, t.result, t.log, t.last = "play", nil, "", nil
	t.deadline = now.Add(t.wait())
}

// four is false when a month has all four cards in cs.
func four(cs []int) bool {
	n := map[int]int{}
	for _, c := range cs {
		if n[kkMonth(c)]++; n[kkMonth(c)] == 4 {
			return false
		}
	}
	return true
}

func (t *kkTable) wait() time.Duration {
	s := t.seats[t.turn]
	switch {
	case s.bot:
		return kkBotTime
	case t.stage == "decide":
		return kkDecideTime
	}
	return kkTurnTime
}

// take lays c on the field, taking what it matches. pick says which field
// card when two match (-1 lets the best one go).
func (t *kkTable) take(s *kkSeat, c, pick int) ([]int, error) {
	var at []int
	for i, f := range t.field {
		if kkMatch(f, c) {
			at = append(at, i)
		}
	}
	switch len(at) {
	case 0:
		t.field = append(t.field, c)
		return nil, nil
	case 2:
		if pick < 0 {
			pick = at[0]
			if kkValue(t.field[at[1]]) > kkValue(t.field[pick]) {
				pick = at[1]
			}
		}
		if pick != at[0] && pick != at[1] {
			return nil, errPz("pick which of the two to take")
		}
		at = []int{pick}
	}
	took := []int{c}
	for _, i := range at {
		took = append(took, t.field[i])
	}
	t.field = slices.DeleteFunc(t.field, func(f int) bool { return slices.Contains(took, f) })
	s.got = append(s.got, took...)
	return took, nil
}

// play is one turn: a card from the hand, then one from the deck.
func (t *kkTable) play(card, pick int, now time.Time) error {
	if t.stage != "play" {
		return errPz("wait for your turn")
	}
	s := t.seats[t.turn]
	k := slices.Index(s.hand, card)
	if k < 0 {
		return errPz("that card isn't in your hand")
	}
	if pick >= 0 && (pick >= len(t.field) || !kkMatch(t.field[pick], card)) {
		pick = -2 // a pick that doesn't match: ask again if it matters
	}
	matches := 0
	for _, f := range t.field {
		if kkMatch(f, card) {
			matches++
		}
	}
	if matches == 2 && pick < 0 && !s.bot {
		return errPz("two cards match, tap the one to take")
	}
	if pick == -2 {
		pick = -1
	}
	took, err := t.take(s, card, pick)
	if err != nil {
		return err
	}
	s.hand = slices.Delete(s.hand, k, k+1)
	t.last = append([]int{card}, took...)
	words := s.name + kkVerb(took)
	if len(t.deck) > 0 {
		d := t.deck[0]
		t.deck = t.deck[1:]
		drew, _ := t.take(s, d, -1)
		t.last = append(t.last, d)
		t.last = append(t.last, drew...)
		if len(drew) > 0 {
			words += ", flipped and took " + strconv.Itoa(len(drew)) + " more"
		} else {
			words += ", flipped one onto the field"
		}
	}
	t.log = words
	if _, pts := kkScore(s.got); pts > s.last {
		if len(s.hand) == 0 {
			t.stop(t.turn, now)
			return nil
		}
		t.stage = "decide"
		t.deadline = now.Add(t.wait())
		return nil
	}
	t.next(now)
	return nil
}

func kkVerb(took []int) string {
	switch len(took) {
	case 0:
		return " laid a card down"
	case 4:
		return " took a whole month"
	}
	return " took a pair"
}

// next passes the turn on, or ends the hand once every hand is empty.
func (t *kkTable) next(now time.Time) {
	t.turn = (t.turn + 1) % len(t.seats)
	if len(t.seats[t.turn].hand) == 0 {
		t.result = &kkResult{Seat: -1, Through: "Nobody scored this hand"}
		t.endHand(now)
		return
	}
	t.stage = "play"
	t.deadline = now.Add(t.wait())
}

// decide is a seat's call after a combo: play on, or stop and score.
func (t *kkTable) decide(koi bool, now time.Time) error {
	if t.stage != "decide" {
		return errPz("nothing to decide")
	}
	s := t.seats[t.turn]
	if !koi {
		t.stop(t.turn, now)
		return nil
	}
	s.koi = true
	_, s.last = kkScore(s.got)
	t.log = s.name + " called koi-koi!"
	t.next(now)
	return nil
}

// stop scores seat's combos and ends the hand.
func (t *kkTable) stop(seat int, now time.Time) {
	s := t.seats[seat]
	ys, pts := kkScore(s.got)
	r := &kkResult{Seat: seat, Yaku: ys}
	if pts >= 7 {
		pts, r.Big = pts*2, true
	}
	for i, o := range t.seats {
		if i != seat && o.koi {
			pts, r.Koi = pts*2, true
			break
		}
	}
	r.Points = pts
	s.total += pts
	t.result = r
	t.log = s.name + " stopped and scored " + strconv.Itoa(pts)
	t.dealer = seat
	t.endHand(now)
}

func (t *kkTable) endHand(now time.Time) {
	t.results = append(t.results, *t.result)
	t.stage = "done"
	t.deadline = now.Add(kkBetween)
}

// botMove plays the current seat as a bot: the card that takes the most,
// or the cheapest card when nothing matches; it stops on 5 or more, or
// when few cards are left.
func (t *kkTable) botMove(now time.Time) {
	s := t.seats[t.turn]
	if t.stage == "decide" {
		_, pts := kkScore(s.got)
		t.decide(pts < 5 && len(s.hand) >= 4, now)
		return
	}
	_, base := kkScore(s.got)
	best, bestV, pick := s.hand[0], -1000, -1
	for _, c := range s.hand {
		var at []int
		for i, f := range t.field {
			if kkMatch(f, c) {
				at = append(at, i)
			}
		}
		v, p := -kkValue(c), -1 // nothing to take: give away the least
		if len(at) > 0 {
			took := []int{c}
			p = at[0]
			if len(at) == 2 && kkValue(t.field[at[1]]) > kkValue(t.field[at[0]]) {
				p = at[1]
			}
			if len(at) == 2 {
				took = append(took, t.field[p])
			} else {
				for _, i := range at {
					took = append(took, t.field[i])
				}
			}
			_, after := kkScore(append(slices.Clone(s.got), took...))
			v = 30*(after-base) + 50
			for _, x := range took {
				v += kkValue(x)
			}
		}
		if v > bestV {
			best, bestV, pick = c, v, p
		}
	}
	t.play(best, pick, now)
}

// step moves the table on: bots, people out of time, the next hand.
func (t *kkTable) step(now time.Time, here func(string) bool, intn func(int) int) bool {
	changed := false
	for i, s := range t.seats {
		if !s.bot && !here(s.id) {
			s.bot, changed = true, true
			s.name += " (bot)"
			if i == t.turn && t.deadline.After(now.Add(kkBotTime)) {
				t.deadline = now.Add(kkBotTime)
			}
		}
	}
	if t.stage == "finished" || now.Before(t.deadline) {
		return changed
	}
	switch t.stage {
	case "done":
		if t.hand >= kkHands {
			t.stage = "finished"
			return true
		}
		t.deal(now, intn)
	case "play", "decide":
		if t.talk && t.seats[t.turn].bot && rand.IntN(2) == 0 {
			t.say = strings.TrimSuffix(t.seats[t.turn].name, " (bot)") + ": " + kkTrash[rand.IntN(len(kkTrash))]
		}
		t.botMove(now)
	}
	return true
}

// ── In the party rooms ──

func (b *bot) kkStart(r *togetherRoom, pool []guessShow, now time.Time) error {
	size, _ := strconv.Atoi(r.stat)
	if size < 2 || size > 4 {
		size = 2
	}
	g, ok := newKoiKoi(r.partyPeople(), size, pool, func(id string) string { return b.roomName(r, id) }, rand.IntN)
	if !ok {
		return errPz("couldn't pick covers, try again")
	}
	for _, t := range g.tables {
		t.deal(now, rand.IntN)
		t.talk = r.trash
	}
	r.phase, r.kk = "koikoi", g
	return nil
}

func (b *bot) kkStep(r *togetherRoom, now time.Time) {
	g := r.kk
	here := func(id string) bool { _, ok := r.members[id]; return ok }
	changed, done := false, true
	for _, t := range g.tables {
		if t.step(now, here, rand.IntN) {
			changed = true
		}
		done = done && t.stage == "finished"
	}
	if done {
		b.kkFinish(r)
		return
	}
	if changed {
		r.changed()
	}
}

// kkFinish pays the best totals. A bot on top pays no one.
func (b *bot) kkFinish(r *togetherRoom) {
	best := 0
	var top []*kkSeat
	for _, t := range r.kk.tables {
		for _, s := range t.seats {
			if s.total > best {
				best, top = s.total, []*kkSeat{s}
			} else if s.total == best && best > 0 {
				top = append(top, s)
			}
		}
	}
	r.scores = map[string]int{}
	for _, t := range r.kk.tables {
		for _, s := range t.seats {
			if !strings.HasPrefix(s.id, "bot:") {
				r.scores[s.id] = s.total
			}
		}
	}
	r.phase, r.winners = "over", nil
	for _, s := range top {
		if !strings.HasPrefix(s.id, "bot:") {
			r.winners = append(r.winners, s.id)
		}
	}
	sort.Strings(r.winners)
	r.changed()
	for _, w := range r.winners {
		b.actPoint(w, r.game)
	}
}

// seatOf finds user's table and seat, or -1, -1.
func (g *kkGame) seatOf(user string) (int, int) {
	for i, t := range g.tables {
		for j, s := range t.seats {
			if s.id == user {
				return i, j
			}
		}
	}
	return -1, -1
}

type kkSeatView struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Bot   bool     `json:"bot,omitempty"`
	Hand  int      `json:"hand"`
	Got   []int    `json:"got"`
	Yaku  []kkYaku `json:"yaku"`
	Koi   bool     `json:"koi,omitempty"`
	Total int      `json:"total"`
}

type kkTableSum struct {
	Hand  int              `json:"hand"`
	Stage string           `json:"stage"`
	Seats []togetherPerson `json:"seats"`
}

type kkShow struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type kkView struct {
	Shows     []kkShow         `json:"shows"`
	Table     int              `json:"table"`
	Tables    []kkTableSum     `json:"tables"`
	Seats     []kkSeatView     `json:"seats"`
	Me        int              `json:"me"` // my seat, or -1 when watching
	Mine      []int            `json:"mine"`
	Field     []int            `json:"field"`
	Deck      int              `json:"deck"`
	Turn      int              `json:"turn"`
	Stage     string           `json:"stage"`
	Left      float64          `json:"left"`
	Hand      int              `json:"hand"`
	Hands     int              `json:"hands"`
	Log       string           `json:"log,omitempty"`
	Last      []int            `json:"last"`
	Result    *kkResult        `json:"result,omitempty"`
	Results   []kkResult       `json:"results"`
	Standings []togetherPerson `json:"standings"`
	Size      int              `json:"size"`
	Say       string           `json:"say,omitempty"`
}

func (b *bot) kkView(r *togetherRoom, user string, now time.Time) *kkView {
	g := r.kk
	ti, me := g.seatOf(user)
	if ti < 0 {
		ti = min(g.watch[user], len(g.tables)-1)
	}
	t := g.tables[ti]
	v := &kkView{Table: ti, Me: me, Mine: []int{}, Field: t.field, Deck: len(t.deck), Turn: t.turn, Stage: t.stage,
		Left: secs(t.deadline.Sub(now)), Hand: t.hand, Hands: kkHands, Log: t.log, Last: t.last, Result: t.result,
		Results: t.results, Standings: []togetherPerson{}, Size: g.size, Say: t.say}
	if v.Last == nil {
		v.Last = []int{}
	}
	if v.Results == nil {
		v.Results = []kkResult{}
	}
	for _, s := range g.shows {
		v.Shows = append(v.Shows, kkShow{ID: s.ID, Name: s.Name})
	}
	for _, s := range t.seats {
		ys, _ := kkScore(s.got)
		if ys == nil {
			ys = []kkYaku{}
		}
		got := s.got
		if got == nil {
			got = []int{}
		}
		v.Seats = append(v.Seats, kkSeatView{ID: s.id, Name: s.name, Bot: s.bot, Hand: len(s.hand), Got: got, Yaku: ys, Koi: s.koi, Total: s.total})
	}
	if me >= 0 {
		v.Mine = t.seats[me].hand
	}
	for _, x := range g.tables {
		sum := kkTableSum{Hand: x.hand, Stage: x.stage}
		for _, s := range x.seats {
			p := togetherPerson{ID: s.id, Name: s.name, Points: s.total}
			if s.bot && strings.HasPrefix(s.id, "bot:") {
				p.ID = ""
			}
			sum.Seats = append(sum.Seats, p)
			v.Standings = append(v.Standings, p)
		}
		v.Tables = append(v.Tables, sum)
	}
	sort.SliceStable(v.Standings, func(i, j int) bool { return v.Standings[i].Points > v.Standings[j].Points })
	return v
}

func (b *bot) kkAct(r *togetherRoom, user string, in togetherIn, now time.Time) error {
	g := r.kk
	if in.Kind == "watch" {
		if in.Cell < 0 || in.Cell >= len(g.tables) {
			return errPz("no such table")
		}
		g.watch[user] = in.Cell
		r.changed()
		return nil
	}
	ti, me := g.seatOf(user)
	if ti < 0 {
		return errPz("you're watching this game")
	}
	t := g.tables[ti]
	s := t.seats[me]
	if s.bot {
		return errPz("a bot took your seat when you left")
	}
	if t.turn != me || (t.stage != "play" && t.stage != "decide") {
		return errPz("it's not your turn")
	}
	var err error
	switch in.Kind {
	case "play":
		pick := -1
		if in.Pick != nil {
			pick = *in.Pick
		}
		err = t.play(in.Cell, pick, now)
	case "koi", "stop":
		err = t.decide(in.Kind == "koi", now)
	default:
		err = errPz("unknown move")
	}
	if err == nil {
		r.changed()
	}
	return err
}
