package main

import (
	"math/rand/v2"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Playing together in the Activity. Everyone who opens it from the same
// call shares a room (Discord's instance id): a lobby that shows who is
// there, where the host picks a game. Race rounds put one guess, riddle or
// scramble in front of everybody and the first right answer takes the
// round; higher or lower party has everyone vote at once and knocks out
// the wrong ones until one is left. Pages wait on the room's version
// (long polling), so a change reaches every screen right away. Wins give
// game points like the solo games, under the same daily cap.

const (
	togetherRounds    = 5
	togetherClueEvery = 12 * time.Second
	togetherBetween   = 5 * time.Second
	togetherVoteTime  = 12 * time.Second
	togetherReveal    = 5 * time.Second
	togetherIdle      = 45 * time.Second
	togetherWait      = 25 * time.Second
	togetherPzRounds  = 3
)

// togetherNow is the clock, swapped in tests.
var togetherNow = time.Now

var togetherRoomID = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,100}$`)

type togetherMember struct {
	joined time.Time
	seen   time.Time
}

type togetherFeed struct {
	Name string `json:"name"`
	Text string `json:"text"`
	OK   bool   `json:"ok"`
}

type togetherRoom struct {
	id      string
	members map[string]*togetherMember
	ver     int
	wake    chan struct{}
	phase   string // lobby, race, hl, pz, quiz, imposter, draw, bomb, who, koikoi, over
	game    string // guess, riddle, scramble, hl, a puzzle game or a party game
	running bool   // the room's clock is ticking

	// race
	round     int
	cur       *soloRound
	start     time.Time
	ends      time.Time // round over, or next round once over
	winner    string
	roundOver bool
	feed      []togetherFeed
	scores    map[string]int

	// higher or lower
	stat     string
	a, b     guessShow
	seen     []int
	votes    map[string]bool
	alive    []string
	out      []string // knocked out, latest last
	deadline time.Time
	revealed bool
	results  map[string]bool // the last reveal: right or not
	hlRound  int
	solo     bool // started alone: plays on until a miss

	// puzzle: everyone gets their own copy of the same board
	pzMode   string
	pzBase   actPuzzle
	pzBoards map[string]*pzEntry

	// party games (activitysocial.go)
	pool []guessShow
	quiz *quizParty
	imp  *impGame
	draw *drawGame
	bomb *bombGame
	who  *whoGame
	kk   *kkGame

	winners []string

	// admin powers and pranks (activityadmin.go)
	admins  map[string]bool // members the bot knows are staff
	kicked  map[string]bool // sat out until the next game
	muted   map[string]bool // their wrong answers show as dots
	paused  time.Time       // when an admin paused; zero while running
	trolls  map[string]*trollFx
	swap    map[string]string // avatar swaps: member → whose picture shows
	fx      []fxEvent
	fxN     int
	impRig  string // always the imposter, when playing
	bombRig string // the bomb waits for them
	trash   bool   // Koi-Koi bots talk back

	toggles  map[string]map[string]bool // pranks switched on, by member (activitypranks.go)
	renames  map[string]string          // prank names, by member
	locked   bool                       // nobody new joins
	hostTo   string                     // host picked by staff
	watchers map[string]bool            // spectators: they watch, never play
	roomOut  map[string]time.Time       // out of the room until
	slow     map[string]bool            // one answer every few seconds
	lastSaid map[string]time.Time
	roulette bool              // everyone shows someone else's name
	roulMap  map[string]string // whose name each member shows
	roulAt   time.Time
}

type togetherRooms struct {
	mu sync.Mutex
	m  map[string]*togetherRoom
}

// changed bumps the version and wakes the waiting pages. Hold the lock.
func (r *togetherRoom) changed() {
	r.ver++
	close(r.wake)
	r.wake = make(chan struct{})
}

// host is the member who has been in the room longest.
func (r *togetherRoom) host() string {
	if _, ok := r.members[r.hostTo]; ok {
		return r.hostTo
	}
	host := ""
	for id, m := range r.members {
		if host == "" || m.joined.Before(r.members[host].joined) || (m.joined.Equal(r.members[host].joined) && id < host) {
			host = id
		}
	}
	return host
}

func (p *togetherRooms) room(id string) *togetherRoom {
	if p.m == nil {
		p.m = map[string]*togetherRoom{}
	}
	r := p.m[id]
	if r == nil {
		r = &togetherRoom{id: id, members: map[string]*togetherMember{}, wake: make(chan struct{}), phase: "lobby"}
		p.m[id] = r
	}
	return r
}

// touch marks user as present, adding them when new. Hold the lock.
func (r *togetherRoom) touch(user string, now time.Time) {
	if r.outside(user, now) != "" {
		return
	}
	m := r.members[user]
	if m == nil {
		r.members[user] = &togetherMember{joined: now, seen: now}
		r.changed()
		return
	}
	m.seen = now
}

// prune drops members who stopped asking. Hold the lock.
func (r *togetherRoom) prune(now time.Time) {
	gone := false
	for id, m := range r.members {
		if now.Sub(m.seen) > togetherIdle {
			delete(r.members, id)
			gone = true
		}
	}
	if gone {
		r.changed()
	}
}

// ── What a page sees ──

type togetherPerson struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Host   bool   `json:"host,omitempty"`
	Points int    `json:"points,omitempty"`
	Out    bool   `json:"out,omitempty"`
	Voted  bool   `json:"voted,omitempty"`
	Right  *bool  `json:"right,omitempty"`
}

type togetherRace struct {
	Round     int              `json:"round"`
	Rounds    int              `json:"rounds"`
	Puzzle    string           `json:"puzzle,omitempty"`
	Clues     []string         `json:"clues"`
	Pic       string           `json:"pic,omitempty"`
	Left      float64          `json:"left"` // seconds to the round's end, or to the next round once over
	Over      bool             `json:"over"`
	Winner    string           `json:"winner,omitempty"`
	Answer    string           `json:"answer,omitempty"`
	URL       string           `json:"url,omitempty"`
	Cover     int              `json:"cover,omitempty"`
	Feed      []togetherFeed   `json:"feed"`
	Loading   bool             `json:"loading,omitempty"`
	Standings []togetherPerson `json:"standings"`
}

type togetherHL struct {
	Stat     string           `json:"stat"`
	Round    int              `json:"round"`
	A        actHLShow        `json:"a"`
	B        actHLShow        `json:"b"`
	Left     float64          `json:"left"` // seconds to vote, or to the next pair once revealed
	Revealed bool             `json:"revealed"`
	Higher   bool             `json:"higher,omitempty"` // b is higher, once revealed
	Mine     *bool            `json:"mine,omitempty"`   // my vote
	In       bool             `json:"in"`               // still in the game
	Players  []togetherPerson `json:"players"`
}

type togetherView struct {
	Ver     int               `json:"ver"`
	Room    string            `json:"room"`
	Me      string            `json:"me"`
	Host    string            `json:"host"`
	Phase   string            `json:"phase"`
	Game    string            `json:"game,omitempty"`
	Members []togetherPerson  `json:"members"`
	Race    *togetherRace     `json:"race,omitempty"`
	HL      *togetherHL       `json:"hl,omitempty"`
	Pz      *togetherPz       `json:"pz,omitempty"`
	Quiz    *quizPartyView    `json:"quiz,omitempty"`
	Imp     *impView          `json:"imp,omitempty"`
	Draw    *drawView         `json:"draw,omitempty"`
	Bomb    *bombView         `json:"bomb,omitempty"`
	Who     *whoView          `json:"who,omitempty"`
	KK      *kkView           `json:"kk,omitempty"`
	Winners []string          `json:"winners,omitempty"`
	Paused  bool              `json:"paused,omitempty"`
	Kicked  bool              `json:"kicked,omitempty"`
	Out     string            `json:"out,omitempty"`   // why I can't play here: kicked, watch, room, locked
	Off     []string          `json:"off,omitempty"`   // games staff turned off
	Troll   *trollView        `json:"troll,omitempty"` // pranks on me
	Swap    map[string]string `json:"swap,omitempty"`
	Fx      []fxEvent         `json:"fx,omitempty"`
	Admin   *adminView        `json:"admin,omitempty"` // only for staff
}

func secs(d time.Duration) float64 {
	if d < 0 {
		return 0
	}
	return float64(d.Round(100*time.Millisecond)) / float64(time.Second)
}

// view is the room as user sees it. Hold the lock.
func (b *bot) togetherViewOf(r *togetherRoom, user string, now time.Time) togetherView {
	if !r.paused.IsZero() {
		now = r.paused // clocks stand still
	}
	host := r.host()
	v := togetherView{Ver: r.ver, Room: r.id, Me: user, Host: host, Phase: r.phase, Game: r.game}
	ids := make([]string, 0, len(r.members))
	for id := range r.members {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return r.members[ids[i]].joined.Before(r.members[ids[j]].joined) })
	for _, id := range ids {
		v.Members = append(v.Members, togetherPerson{ID: id, Name: b.roomName(r, id), Host: id == host})
	}
	for _, w := range r.winners {
		v.Winners = append(v.Winners, b.roomName(r, w))
	}
	if _, social := socialGames[r.game]; social {
		b.socialView(r, &v, user, now)
	} else if _, pz := pzGames[r.game]; pz {
		v.Pz = b.togetherPzView(r, user, now)
	} else if r.game == "hl" {
		v.HL = b.togetherHLView(r, user, now)
	} else if r.game != "" {
		v.Race = b.togetherRaceView(r, now)
	}
	b.adminViewFill(r, &v, user, now)
	return v
}

func (b *bot) togetherStandings(r *togetherRoom) []togetherPerson {
	out := []togetherPerson{}
	for id, n := range r.scores {
		out = append(out, togetherPerson{ID: id, Name: b.roomName(r, id), Points: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Points != out[j].Points {
			return out[i].Points > out[j].Points
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (b *bot) togetherRaceView(r *togetherRoom, now time.Time) *togetherRace {
	v := &togetherRace{Round: r.round, Rounds: togetherRounds, Feed: append([]togetherFeed{}, r.feed...), Clues: []string{},
		Standings: b.togetherStandings(r), Over: r.roundOver, Left: secs(r.ends.Sub(now))}
	if r.cur == nil {
		v.Loading = r.phase == "race"
		return v
	}
	c := r.cur
	n := togetherShown(c, r.start, now, r.roundOver)
	v.Puzzle, v.Clues = c.puzzle, append(v.Clues, c.clues[:n]...)
	if c.game == "guess" {
		step := n - 1
		if r.roundOver {
			step = len(guessBlocks) - 1
		}
		v.Pic = "api/party/pic?room=" + r.id + "&round=" + strconv.Itoa(r.round) + "&step=" + strconv.Itoa(max(step, 0))
	}
	if r.roundOver {
		v.Answer, v.URL, v.Cover = c.name, c.url, c.cover
		if r.winner != "" {
			v.Winner = b.roomName(r, r.winner)
		}
	}
	return v
}

// togetherShown is how many clues a race round shows: the first ones, then
// one more every togetherClueEvery.
func togetherShown(c *soloRound, start, now time.Time, over bool) int {
	if over {
		return len(c.clues)
	}
	return min(len(c.clues), c.first+int(now.Sub(start)/togetherClueEvery))
}

// togetherRoundTime is how long a race round runs: until every clue has had
// its turn.
func togetherRoundTime(c *soloRound) time.Duration {
	return time.Duration(len(c.clues)-c.first+1) * togetherClueEvery * 3 / 2
}

func (b *bot) togetherHLView(r *togetherRoom, user string, now time.Time) *togetherHL {
	v := &togetherHL{Stat: r.stat, Round: r.hlRound, A: hlShow(r.a, r.stat, true), B: hlShow(r.b, r.stat, r.revealed),
		Left: secs(r.deadline.Sub(now)), Revealed: r.revealed}
	if r.revealed {
		v.Higher = hlValue(r.b, r.stat) > hlValue(r.a, r.stat)
	}
	if vote, ok := r.votes[user]; ok {
		v.Mine = &vote
	}
	for _, id := range r.alive {
		if id == user {
			v.In = true
		}
	}
	seen := map[string]bool{}
	add := func(id string, out bool) {
		if seen[id] {
			return
		}
		seen[id] = true
		p := togetherPerson{ID: id, Name: b.roomName(r, id), Out: out}
		_, p.Voted = r.votes[id]
		if right, ok := r.results[id]; ok && r.revealed {
			p.Right = &right
		}
		v.Players = append(v.Players, p)
	}
	for _, id := range r.alive {
		add(id, false)
	}
	for i := len(r.out) - 1; i >= 0; i-- {
		add(r.out[i], true)
	}
	sort.SliceStable(v.Players, func(i, j int) bool { return !v.Players[i].Out && v.Players[j].Out })
	return v
}

// ── The room's clock ──

// tick moves a running room on: clues, round ends, next rounds, votes.
// It runs while the room plays and stops in the lobby or once over.
func (b *bot) togetherLoop(r *togetherRoom) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for range t.C {
		b.together.mu.Lock()
		next, keep := b.togetherStep(r, togetherNow())
		if !keep {
			r.running = false
		}
		b.together.mu.Unlock()
		if !keep {
			return
		}
		if next != "" {
			b.togetherNextRound(r, next)
		}
	}
}

// togetherStep is one tick under the lock. It returns a game when a new race
// round has to be fetched (outside the lock), and false once the room no
// longer needs its clock.
func (b *bot) togetherStep(r *togetherRoom, now time.Time) (string, bool) {
	r.prune(now)
	if len(r.members) == 0 {
		delete(b.together.m, r.id)
		return "", false
	}
	if !r.paused.IsZero() {
		return "", true
	}
	r.fxPrune(now)
	switch r.phase {
	case "race":
		if r.cur == nil {
			return "", true // fetching
		}
		if !r.roundOver {
			if togetherShown(r.cur, r.start, now, false) > togetherShown(r.cur, r.start, now.Add(-250*time.Millisecond), false) {
				r.changed()
			}
			if !now.Before(r.ends) {
				r.roundOver = true
				r.ends = now.Add(togetherBetween)
				r.changed()
			}
			return "", true
		}
		if now.Before(r.ends) {
			return "", true
		}
		if r.round >= togetherRounds {
			r.finish()
			return "", false
		}
		r.cur, r.roundOver, r.winner, r.feed = nil, false, "", nil
		r.round++
		r.changed()
		return r.game, true
	case "pz":
		return b.togetherPzStep(r, now)
	case "quiz", "imposter", "draw", "bomb", "who", "koikoi":
		return b.socialStep(r, now)
	case "hl":
		if !r.revealed {
			allIn := len(r.alive) > 0
			for _, id := range r.alive {
				if _, ok := r.votes[id]; !ok {
					allIn = false
				}
			}
			if allIn || !now.Before(r.deadline) {
				b.togetherHLReveal(r, now)
			}
			return "", true
		}
		if now.Before(r.deadline) {
			return "", true
		}
		if r.phase == "hl" && len(r.alive) > 0 && !(len(r.alive) == 1 && !r.solo) {
			b.togetherHLNext(r, now)
			return "", true
		}
		r.phase = "over"
		r.changed()
		return "", false
	}
	return "", false
}

// finish ends a race or puzzle game: the most round wins take it.
func (r *togetherRoom) finish() {
	r.phase = "over"
	r.winners = nil
	best := 0
	for id, n := range r.scores {
		if n > best {
			best, r.winners = n, []string{id}
		} else if n == best && n > 0 {
			r.winners = append(r.winners, id)
		}
	}
	sort.Strings(r.winners)
	r.changed()
}

// togetherHLReveal shows the answer and knocks out the wrong.
func (b *bot) togetherHLReveal(r *togetherRoom, now time.Time) {
	r.revealed = true
	r.results = map[string]bool{}
	var right []string
	for _, id := range r.alive {
		vote, ok := r.votes[id]
		ok = ok && hlRight(r.a, r.b, r.stat, vote)
		r.results[id] = ok
		if ok {
			right = append(right, id)
		}
	}
	if !r.solo && len(right) > 0 {
		for _, id := range r.alive {
			if !r.results[id] {
				r.out = append(r.out, id)
			}
		}
	}
	switch {
	case r.solo:
		if len(right) == 0 {
			r.winners = nil
		}
		r.alive = right
	case len(right) == 0:
		// Everyone missed: the ones still in share the win.
		r.winners = append([]string{}, r.alive...)
		r.alive = nil
	case len(right) == 1:
		r.winners = right
		r.alive = right
		b.actPoint(right[0], "higher-lower")
	default:
		r.alive = right
	}
	r.deadline = now.Add(togetherReveal)
	r.changed()
}

// togetherHLNext moves to the next pair.
func (b *bot) togetherHLNext(r *togetherRoom, now time.Time) {
	pool, err := b.guess.shows()
	next, ok := guessShow{}, false
	if err == nil {
		next, ok = hlNext(pool, r.b, r.stat, r.seen, rand.IntN)
	}
	if !ok {
		r.phase = "over"
		r.winners = append([]string{}, r.alive...)
		r.changed()
		return
	}
	r.a, r.b = r.b, next
	r.seen = append(r.seen, next.ID)
	r.votes, r.revealed, r.results = map[string]bool{}, false, nil
	r.hlRound++
	r.deadline = now.Add(togetherVoteTime)
	r.changed()
}

// togetherNextRound fetches a race round outside the lock and puts it in.
func (b *bot) togetherNextRound(r *togetherRoom, game string) {
	if _, pz := pzGames[game]; pz {
		b.togetherNextPuzzle(r, game)
		return
	}
	var round *soloRound
	var err error
	for range 3 {
		if round, err = b.newSoloRound("", game); err == nil {
			break
		}
	}
	b.together.mu.Lock()
	defer b.together.mu.Unlock()
	if r.phase != "race" || r.cur != nil {
		return
	}
	if err != nil {
		r.phase = "lobby"
		r.game = ""
		r.changed()
		return
	}
	now := togetherNow()
	r.cur, r.start = round, now
	r.ends = now.Add(togetherRoundTime(round))
	r.changed()
}

// ── Routes ──

func init() {
	apiHandle("POST /activity/api/party/join", (*bot).togetherJoin)
	apiHandle("GET /activity/api/party/state", (*bot).togetherState)
	apiHandle("POST /activity/api/party/leave", (*bot).togetherLeave)
	apiHandle("POST /activity/api/party/start", (*bot).togetherStart)
	apiHandle("POST /activity/api/party/lobby", (*bot).togetherLobby)
	apiHandle("POST /activity/api/party/answer", (*bot).togetherAnswer)
	apiHandle("POST /activity/api/party/vote", (*bot).togetherVote)
	apiHandle("GET /activity/api/party/pic", (*bot).togetherPic)
	apiHandle("GET /activity/api/party/daily", (*bot).togetherDaily)
}

type togetherIn struct {
	Room   string `json:"room"`
	Game   string `json:"game"`
	Stat   string `json:"stat"`
	Text   string `json:"text"`
	Higher bool   `json:"higher"`
	Kind   string `json:"kind"`
	Cell   int    `json:"cell"`
	Pick   *int   `json:"pick"`
}

// togetherRead reads a request's body and checks the room id.
func (b *bot) togetherRead(w http.ResponseWriter, r *http.Request) (string, togetherIn, bool) {
	user := b.actUser(w, r)
	if user == "" {
		return "", togetherIn{}, false
	}
	var in togetherIn
	if !readJSON(w, r, 4<<10, &in) {
		return "", in, false
	}
	if !togetherRoomID.MatchString(in.Room) {
		apiError(w, http.StatusBadRequest, "no room")
		return "", in, false
	}
	return user, in, true
}

func (b *bot) togetherJoin(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.togetherRead(w, r)
	if !ok {
		return
	}
	admin := b.isAdmin(user)
	b.together.mu.Lock()
	room := b.together.room(in.Room)
	room.setAdmin(user, admin)
	room.touch(user, togetherNow())
	room.prune(togetherNow())
	v := b.togetherViewOf(room, user, togetherNow())
	b.together.mu.Unlock()
	writeJSON(w, http.StatusOK, v)
}

// togetherState answers once the room's version differs from v, or after
// togetherWait: a long poll that also keeps the member in the room.
func (b *bot) togetherState(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	id := r.URL.Query().Get("room")
	if !togetherRoomID.MatchString(id) {
		apiError(w, http.StatusBadRequest, "no room")
		return
	}
	have, _ := strconv.Atoi(r.URL.Query().Get("v"))
	admin := b.isAdmin(user)
	timeout := time.NewTimer(togetherWait)
	defer timeout.Stop()
	for {
		b.together.mu.Lock()
		room := b.together.room(id)
		room.setAdmin(user, admin)
		room.touch(user, togetherNow())
		room.prune(togetherNow())
		if room.ver != have {
			v := b.togetherViewOf(room, user, togetherNow())
			b.together.mu.Unlock()
			writeJSON(w, http.StatusOK, v)
			return
		}
		wake := room.wake
		b.together.mu.Unlock()
		select {
		case <-wake:
		case <-timeout.C:
			have = -1
		case <-r.Context().Done():
			return
		}
	}
}

func (b *bot) togetherLeave(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.togetherRead(w, r)
	if !ok {
		return
	}
	b.together.mu.Lock()
	if room := b.together.m[in.Room]; room != nil {
		if _, ok := room.members[user]; ok {
			delete(room.members, user)
			room.changed()
		}
		if len(room.members) == 0 && !room.running {
			delete(b.together.m, in.Room)
		}
	}
	b.together.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (b *bot) togetherStart(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.togetherRead(w, r)
	if !ok {
		return
	}
	_, race := actWrong[in.Game]
	_, social := socialGames[in.Game]
	_, pz := pzGames[in.Game]
	pz = pz && !social
	if !race && !pz && !social && in.Game != "hl" {
		apiError(w, http.StatusBadRequest, "no such game")
		return
	}
	if b.gameOff(in.Game) {
		apiError(w, http.StatusForbidden, "staff turned this game off")
		return
	}
	var pool []guessShow
	if in.Game == "hl" || social {
		var err error
		if pool, err = b.guess.shows(); err != nil {
			apiError(w, http.StatusBadGateway, "couldn't reach AniList, try again in a bit")
			return
		}
	}
	admin := b.isAdmin(user)
	b.together.mu.Lock()
	defer b.together.mu.Unlock()
	room := b.together.room(in.Room)
	now := togetherNow()
	room.touch(user, now)
	if room.host() != user && !admin {
		apiError(w, http.StatusForbidden, "only the host starts a game")
		return
	}
	if room.phase != "lobby" && room.phase != "over" {
		apiError(w, http.StatusConflict, "a game is running")
		return
	}
	room.newGame()
	room.game, room.winners, room.feed = in.Game, nil, nil
	room.scores = map[string]int{}
	fetch := ""
	if in.Game == "hl" {
		stat := in.Stat
		if stat != "popularity" {
			stat = "score"
		}
		var a guessShow
		for range 20 {
			if a = pool[rand.IntN(len(pool))]; hlValue(a, stat) > 0 {
				break
			}
		}
		next, ok := hlNext(pool, a, stat, nil, rand.IntN)
		if !ok {
			apiError(w, http.StatusBadGateway, "couldn't find shows, try again")
			return
		}
		room.phase, room.stat, room.a, room.b = "hl", stat, a, next
		room.seen = []int{a.ID, next.ID}
		room.votes, room.revealed, room.results, room.out, room.hlRound = map[string]bool{}, false, nil, nil, 1
		room.alive = room.alive[:0]
		for id := range room.members {
			room.alive = append(room.alive, id)
		}
		sort.Strings(room.alive)
		room.solo = len(room.alive) == 1
		room.deadline = now.Add(togetherVoteTime)
	} else if social {
		room.stat = in.Stat
		if err := b.socialStart(room, in.Game, pool, now); err != nil {
			room.game = ""
			apiError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else if pz {
		room.phase, room.round, room.roundOver, room.winner = "pz", 1, false, ""
		room.pzMode, room.pzBase, room.pzBoards = in.Stat, nil, nil
		fetch = in.Game
	} else {
		room.phase, room.round, room.cur, room.roundOver, room.winner = "race", 1, nil, false, ""
		fetch = in.Game
	}
	room.changed()
	if !room.running {
		room.running = true
		go b.togetherLoop(room)
	}
	if fetch != "" {
		go b.togetherNextRound(room, fetch)
	}
	writeJSON(w, http.StatusOK, b.togetherViewOf(room, user, now))
}

// togetherLobby takes the room back to the lobby (host only).
func (b *bot) togetherLobby(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.togetherRead(w, r)
	if !ok {
		return
	}
	admin := b.isAdmin(user)
	b.together.mu.Lock()
	defer b.together.mu.Unlock()
	room := b.together.room(in.Room)
	room.touch(user, togetherNow())
	if room.host() != user && !admin {
		apiError(w, http.StatusForbidden, "only the host can do that")
		return
	}
	room.toLobby()
	room.changed()
	writeJSON(w, http.StatusOK, b.togetherViewOf(room, user, togetherNow()))
}

func (b *bot) togetherAnswer(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.togetherRead(w, r)
	if !ok {
		return
	}
	b.together.mu.Lock()
	room := b.together.m[in.Room]
	if room == nil || room.phase != "race" || room.cur == nil || room.roundOver {
		b.together.mu.Unlock()
		apiError(w, http.StatusConflict, "no round is running")
		return
	}
	now := togetherNow()
	err := room.blocked(user)
	if err == nil {
		err = room.cooled(user, now)
	}
	if err != nil {
		b.together.mu.Unlock()
		apiError(w, http.StatusConflict, err.Error())
		return
	}
	room.touch(user, now)
	right := guessMatches(in.Text, room.cur.titles)
	text := in.Text
	if right {
		text = "got it"
	} else if len([]rune(text)) > 60 {
		text = string([]rune(text)[:60]) + "…"
	}
	if !right {
		text = room.said(user, text)
	}
	room.feed = append(room.feed, togetherFeed{Name: b.feedName(room, user, right), Text: text, OK: right})
	if len(room.feed) > 8 {
		room.feed = room.feed[len(room.feed)-8:]
	}
	if right {
		room.winner, room.roundOver = user, true
		room.ends = now.Add(togetherBetween)
		room.scores[user]++
	}
	by, game := room.cur.by, room.cur.game
	room.changed()
	v := b.togetherViewOf(room, user, now)
	b.together.mu.Unlock()
	if right && by != user {
		b.actPoint(user, game)
	}
	writeJSON(w, http.StatusOK, map[string]any{"right": right, "view": v})
}

func (b *bot) togetherVote(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.togetherRead(w, r)
	if !ok {
		return
	}
	b.together.mu.Lock()
	defer b.together.mu.Unlock()
	room := b.together.m[in.Room]
	if room == nil || room.phase != "hl" || room.revealed {
		apiError(w, http.StatusConflict, "no vote is open")
		return
	}
	if err := room.blocked(user); err != nil {
		apiError(w, http.StatusConflict, err.Error())
		return
	}
	room.touch(user, togetherNow())
	in2 := false
	for _, id := range room.alive {
		if id == user {
			in2 = true
		}
	}
	if !in2 {
		apiError(w, http.StatusForbidden, "you're out this game")
		return
	}
	if _, voted := room.votes[user]; !voted {
		room.votes[user] = in.Higher
		room.changed()
	}
	writeJSON(w, http.StatusOK, b.togetherViewOf(room, user, togetherNow()))
}

// togetherPic is the race round's guess cover at a step everyone can see.
func (b *bot) togetherPic(w http.ResponseWriter, r *http.Request) {
	if b.actUser(w, r) == "" {
		return
	}
	q := r.URL.Query()
	b.together.mu.Lock()
	room := b.together.m[q.Get("room")]
	if room == nil || room.cur == nil || room.cur.img == nil || strconv.Itoa(room.round) != q.Get("round") {
		b.together.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	c := room.cur
	allowed := togetherShown(c, room.start, togetherNow(), room.roundOver) - 1
	if room.roundOver {
		allowed = len(guessBlocks) - 1
	}
	b.together.mu.Unlock()
	step, err := strconv.Atoi(q.Get("step"))
	if err != nil || step < 0 {
		http.NotFound(w, r)
		return
	}
	step = min(step, allowed)
	k := len(guessBlocks) - len(c.clues) + step
	png, err := pixelate(c.img, guessBlocks[max(0, min(k, len(guessBlocks)-1))])
	if err != nil {
		http.Error(w, "couldn't draw", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}

// togetherDailyRow is someone's daily play without titles: colours only.
type togetherDailyRow struct {
	ID     string     `json:"id"`
	Name   string     `json:"name"`
	Me     bool       `json:"me,omitempty"`
	Rows   [][]string `json:"rows"`
	Done   bool       `json:"done"`
	Solved bool       `json:"solved"`
}

// togetherDaily shows how everyone in the room is doing at today's puzzle.
func (b *bot) togetherDaily(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	id := r.URL.Query().Get("room")
	b.together.mu.Lock()
	var ids []string
	names := map[string]string{}
	if room := b.together.m[id]; room != nil {
		room.touch(user, togetherNow())
		for m := range room.members {
			ids = append(ids, m)
			names[m] = b.roomName(room, m)
		}
	}
	b.together.mu.Unlock()
	sort.Strings(ids)
	day := dailyDay(time.Now())
	out := []togetherDailyRow{}
	for _, m := range ids {
		pl := b.dailyPlayOf(m, day)
		row := togetherDailyRow{ID: m, Name: names[m], Me: m == user, Rows: [][]string{}, Done: pl.Done, Solved: pl.Solved}
		for _, g := range pl.Guesses {
			hits := []string{}
			for _, c := range g.Cells {
				hits = append(hits, c.Hit)
			}
			row.Rows = append(row.Rows, hits)
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

// ── Puzzle rounds ──
//
// Everyone gets a copy of the same board and plays it at their own pace.
// The first to solve takes the round; the round ends when every board is
// done or time runs out, and then every board shows its answer.

type togetherPzPlayer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Moves int    `json:"moves"`
	Done  bool   `json:"done"`
	Won   bool   `json:"won"`
	First bool   `json:"first,omitempty"`
}

type togetherPz struct {
	Round     int                `json:"round"`
	Rounds    int                `json:"rounds"`
	Left      float64            `json:"left"` // seconds to the round's end, or to the next round once over
	Total     int                `json:"total"`
	Over      bool               `json:"over"`
	Loading   bool               `json:"loading,omitempty"`
	Mode      string             `json:"mode,omitempty"`
	Board     *pzOut             `json:"board,omitempty"`
	Players   []togetherPzPlayer `json:"players"`
	Winner    string             `json:"winner,omitempty"`
	Standings []togetherPerson   `json:"standings"`
}

// togetherBoard is user's board this round, made when first needed. Hold
// the lock.
func (b *bot) togetherBoard(r *togetherRoom, user string) *pzEntry {
	if r.pzBase == nil {
		return nil
	}
	if r.pzBoards == nil {
		r.pzBoards = map[string]*pzEntry{}
	}
	e := r.pzBoards[user]
	if e == nil {
		e = &pzEntry{p: r.pzBase.clone(), game: r.game}
		r.pzBoards[user] = e
	}
	return e
}

func (b *bot) togetherPzView(r *togetherRoom, user string, now time.Time) *togetherPz {
	v := &togetherPz{Round: r.round, Rounds: togetherPzRounds, Left: secs(r.ends.Sub(now)), Total: pzGames[r.game], Over: r.roundOver,
		Mode: r.pzMode, Players: []togetherPzPlayer{}, Standings: b.togetherStandings(r)}
	if r.pzBase == nil {
		v.Loading = r.phase == "pz"
		return v
	}
	if _, in := r.members[user]; in || r.pzBoards[user] != nil {
		if e := b.togetherBoard(r, user); e != nil {
			out := e.out()
			v.Board = &out
		}
	}
	ids := make([]string, 0, len(r.pzBoards))
	for id := range r.pzBoards {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := r.pzBoards[id]
		v.Players = append(v.Players, togetherPzPlayer{ID: id, Name: b.roomName(r, id), Moves: e.moves, Done: e.done, Won: e.won, First: id == r.winner})
	}
	sort.SliceStable(v.Players, func(i, j int) bool {
		a, c := v.Players[i], v.Players[j]
		if a.Won != c.Won {
			return a.Won
		}
		if a.Won {
			return r.pzBoards[a.ID].at.Before(r.pzBoards[c.ID].at)
		}
		return false
	})
	if r.winner != "" {
		v.Winner = b.roomName(r, r.winner)
	}
	return v
}

// togetherPzStep is the puzzle part of a tick. Hold the lock.
func (b *bot) togetherPzStep(r *togetherRoom, now time.Time) (string, bool) {
	if r.pzBase == nil {
		return "", true // fetching
	}
	if !r.roundOver {
		all := len(r.members) > 0
		for id := range r.members {
			if e := r.pzBoards[id]; e == nil || !e.done {
				all = false
			}
		}
		if all || !now.Before(r.ends) {
			for id := range r.members {
				b.togetherBoard(r, id)
			}
			for _, e := range r.pzBoards {
				e.apply(pzMove{GiveUp: true}, now)
			}
			r.roundOver = true
			r.ends = now.Add(togetherBetween + 3*time.Second)
			r.changed()
		}
		return "", true
	}
	if now.Before(r.ends) {
		return "", true
	}
	if r.round >= togetherPzRounds {
		r.finish()
		return "", false
	}
	r.pzBase, r.pzBoards, r.roundOver, r.winner = nil, nil, false, ""
	r.round++
	r.changed()
	return r.game, true
}

// togetherNextPuzzle makes a puzzle round outside the lock and puts it in.
func (b *bot) togetherNextPuzzle(r *togetherRoom, game string) {
	b.together.mu.Lock()
	mode := r.pzMode
	b.together.mu.Unlock()
	var p actPuzzle
	var err error
	for range 3 {
		if p, err = b.newPuzzle(game, mode); err == nil {
			break
		}
	}
	b.together.mu.Lock()
	defer b.together.mu.Unlock()
	if r.phase != "pz" || r.pzBase != nil {
		return
	}
	if err != nil {
		r.phase, r.game = "lobby", ""
		r.changed()
		return
	}
	now := togetherNow()
	r.pzBase, r.pzBoards = p, map[string]*pzEntry{}
	r.ends = now.Add(time.Duration(pzGames[game]) * time.Second)
	r.changed()
}

func init() {
	apiHandle("POST /activity/api/party/move", (*bot).togetherMove)
}

// togetherMove is a move on the member's own board in a puzzle round.
func (b *bot) togetherMove(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	var in struct {
		Room string `json:"room"`
		pzMove
	}
	if !b.pzReadMove(w, r, &in, &in.pzMove) {
		return
	}
	b.together.mu.Lock()
	room := b.together.m[in.Room]
	if room == nil || room.phase != "pz" || room.pzBase == nil || room.roundOver {
		b.together.mu.Unlock()
		apiError(w, http.StatusConflict, "no round is running")
		return
	}
	if err := room.blocked(user); err != nil {
		b.together.mu.Unlock()
		apiError(w, http.StatusConflict, err.Error())
		return
	}
	now := togetherNow()
	room.touch(user, now)
	e := b.togetherBoard(room, user)
	err := e.apply(in.pzMove, now)
	pay := false
	if e.won && room.winner == "" {
		room.winner, pay = user, true
		if room.scores == nil {
			room.scores = map[string]int{}
		}
		room.scores[user]++
	}
	room.changed()
	v := b.togetherViewOf(room, user, now)
	game := room.game
	b.together.mu.Unlock()
	if pay {
		b.actPoint(user, game)
	}
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"view": v})
}
