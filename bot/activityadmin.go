package main

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Admin powers in the games Activity. Staff (a staff role, Manage Server,
// Moderate Members or Administrator) get a shield menu in every room,
// whoever hosts it: start or end a game, pause, skip a round, void a win,
// sit someone out until the next game, hide their wrong answers, clear a
// drawing, fix points and bench members from games for a while (also from
// the dashboard and /game-ban).
//
// The same menu has harmless pranks. They only change what one screen
// shows or who a party game picks, and they end when the room goes back to
// the lobby or a new game starts after one finished: nobody loses points or
// data to a prank. Moderation steps are logged to staff; pranks are not,
// so they stay a surprise.

const (
	gameAdminCache = 2 * time.Minute
	dodgeTime      = 30 * time.Second
	fxKeep         = 15 * time.Second
	bombRigMax     = 40 * time.Second
	gameBanMax     = 30 * 24 * time.Hour
)

// kkTrash is what Koi-Koi bots say when an admin lets them talk back.
var kkTrash = []string{
	"Is that really the card you wanted?",
	"My grandma plays faster than this.",
	"I've seen better hands at a sushi conveyor belt.",
	"Koi-koi? More like koi-oh-no.",
	"Take your time. I'm ageing like fine sake.",
	"That move was... a choice.",
	"Bold. Wrong, but bold.",
	"I'm not even trying.",
	"You call that a combo?",
	"Even the willow card feels sorry for you.",
	"Did you shuffle your brain too?",
	"I'd say good luck, but you'll need more than luck.",
}

// sillyHints are the fake hints a prank sends when the admin types none.
var sillyHints = []string{
	"It's Shrek.",
	"The main character is a toaster.",
	"Think harder. No, harder.",
	"It aired on a Tuesday. Probably.",
	"The answer has letters in it.",
	"Your mum watched this one.",
	"It's the one with the guy.",
	"Starts with a vowel, or a consonant.",
}

// ── Who is staff ──

// isGameAdmin is the staff check, swapped in tests.
var isGameAdmin = (*bot).discordStaff

var gameAdmins struct {
	sync.Mutex
	m map[string]adminSeen
}

type adminSeen struct {
	ok bool
	at time.Time
}

// isAdmin reports whether user may use the admin menu, from a short cache.
// Call it without holding the rooms' lock: a miss asks Discord.
func (b *bot) isAdmin(user string) bool {
	if user == "" {
		return false
	}
	now := time.Now()
	gameAdmins.Lock()
	s, ok := gameAdmins.m[user]
	gameAdmins.Unlock()
	if ok && now.Sub(s.at) < gameAdminCache {
		return s.ok
	}
	yes := isGameAdmin(b, user)
	gameAdmins.Lock()
	if gameAdmins.m == nil {
		gameAdmins.m = map[string]adminSeen{}
	}
	gameAdmins.m[user] = adminSeen{yes, now}
	gameAdmins.Unlock()
	return yes
}

// discordStaff reports whether a member is the owner, has a staff role, or
// a role that can manage the server or moderate members.
func (b *bot) discordStaff(user string) bool {
	if b.s == nil || b.cfg.GuildID == "" {
		return false
	}
	m, err := b.s.State.Member(b.cfg.GuildID, user)
	if err != nil || m == nil {
		if m, err = b.s.GuildMember(b.cfg.GuildID, user); err != nil || m == nil {
			return false
		}
	}
	g, _ := b.s.State.Guild(b.cfg.GuildID)
	if g != nil && g.OwnerID == user {
		return true
	}
	const staffPerms = discordgo.PermissionAdministrator | discordgo.PermissionManageGuild | discordgo.PermissionModerateMembers
	for _, id := range m.Roles {
		if contains(b.cfg.StaffRoles, id) {
			return true
		}
		if g == nil {
			continue
		}
		for _, r := range g.Roles {
			if r.ID == id && r.Permissions&staffPerms != 0 {
				return true
			}
		}
	}
	return false
}

// isGuildOwner grants ticket management only to the Discord server owner.
func (b *bot) isGuildOwner(user string) bool {
	if user == "" || b.s == nil || b.s.State == nil {
		return false
	}
	g, err := b.s.State.Guild(b.cfg.GuildID)
	return err == nil && g != nil && g.OwnerID == user
}

var ticketUserID = regexp.MustCompile(`^[0-9]{17,20}package main

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Admin powers in the games Activity. Staff (a staff role, Manage Server,
// Moderate Members or Administrator) get a shield menu in every room,
// whoever hosts it: start or end a game, pause, skip a round, void a win,
// sit someone out until the next game, hide their wrong answers, clear a
// drawing, fix points and bench members from games for a while (also from
// the dashboard and /game-ban).
//
// The same menu has harmless pranks. They only change what one screen
// shows or who a party game picks, and they end when the room goes back to
// the lobby or a new game starts after one finished: nobody loses points or
// data to a prank. Moderation steps are logged to staff; pranks are not,
// so they stay a surprise.

const (
	gameAdminCache = 2 * time.Minute
	dodgeTime      = 30 * time.Second
	fxKeep         = 15 * time.Second
	bombRigMax     = 40 * time.Second
	gameBanMax     = 30 * 24 * time.Hour
)

// kkTrash is what Koi-Koi bots say when an admin lets them talk back.
var kkTrash = []string{
	"Is that really the card you wanted?",
	"My grandma plays faster than this.",
	"I've seen better hands at a sushi conveyor belt.",
	"Koi-koi? More like koi-oh-no.",
	"Take your time. I'm ageing like fine sake.",
	"That move was... a choice.",
	"Bold. Wrong, but bold.",
	"I'm not even trying.",
	"You call that a combo?",
	"Even the willow card feels sorry for you.",
	"Did you shuffle your brain too?",
	"I'd say good luck, but you'll need more than luck.",
}

// sillyHints are the fake hints a prank sends when the admin types none.
var sillyHints = []string{
	"It's Shrek.",
	"The main character is a toaster.",
	"Think harder. No, harder.",
	"It aired on a Tuesday. Probably.",
	"The answer has letters in it.",
	"Your mum watched this one.",
	"It's the one with the guy.",
	"Starts with a vowel, or a consonant.",
}

// ── Who is staff ──

// isGameAdmin is the staff check, swapped in tests.
var isGameAdmin = (*bot).discordStaff

var gameAdmins struct {
	sync.Mutex
	m map[string]adminSeen
}

type adminSeen struct {
	ok bool
	at time.Time
}

// isAdmin reports whether user may use the admin menu, from a short cache.
// Call it without holding the rooms' lock: a miss asks Discord.
func (b *bot) isAdmin(user string) bool {
	if user == "" {
		return false
	}
	now := time.Now()
	gameAdmins.Lock()
	s, ok := gameAdmins.m[user]
	gameAdmins.Unlock()
	if ok && now.Sub(s.at) < gameAdminCache {
		return s.ok
	}
	yes := isGameAdmin(b, user)
	gameAdmins.Lock()
	if gameAdmins.m == nil {
		gameAdmins.m = map[string]adminSeen{}
	}
	gameAdmins.m[user] = adminSeen{yes, now}
	gameAdmins.Unlock()
	return yes
}

// discordStaff reports whether a member is the owner, has a staff role, or
// a role that can manage the server or moderate members.
func (b *bot) discordStaff(user string) bool {
	if b.s == nil || b.cfg.GuildID == "" {
		return false
	}
	m, err := b.s.State.Member(b.cfg.GuildID, user)
	if err != nil || m == nil {
		if m, err = b.s.GuildMember(b.cfg.GuildID, user); err != nil || m == nil {
			return false
		}
	}
	g, _ := b.s.State.Guild(b.cfg.GuildID)
	if g != nil && g.OwnerID == user {
		return true
	}
	const staffPerms = discordgo.PermissionAdministrator | discordgo.PermissionManageGuild | discordgo.PermissionModerateMembers
	for _, id := range m.Roles {
		if contains(b.cfg.StaffRoles, id) {
			return true
		}
		if g == nil {
			continue
		}
		for _, r := range g.Roles {
			if r.ID == id && r.Permissions&staffPerms != 0 {
				return true
			}
		}
	}
	return false
}

)

// adjustWatchTickets clamps removal at zero and returns the resulting balance.
func (b *bot) adjustWatchTickets(user string, delta int) int {
	balance := 0
	b.store.update(func(d *storeData) {
		if d.WatchTickets == nil {
			d.WatchTickets = map[string]int{}
		}
		balance = max(0, d.WatchTickets[user]+delta)
		if balance == 0 {
			delete(d.WatchTickets, user)
		} else {
			d.WatchTickets[user] = balance
		}
	})
	return balance
}

// setAdmin remembers whether a member is staff. Hold the lock.
func (r *togetherRoom) setAdmin(user string, admin bool) {
	if r.admins == nil {
		r.admins = map[string]bool{}
	}
	r.admins[user] = admin
}

// ── Game bans ──

type gameBan struct {
	Until  time.Time `json:"until"`
	By     string    `json:"by,omitempty"`
	Reason string    `json:"reason,omitempty"`
}

// benched is a member's game ban while it lasts.
func benched(d *storeData, user string, now time.Time) (gameBan, bool) {
	ban, ok := d.GameBans[user]
	return ban, ok && now.Before(ban.Until)
}

// gameBanned is a member's game ban, if one is running.
func (b *bot) gameBanned(user string) (gameBan, bool) {
	var ban gameBan
	var ok bool
	b.store.view(func(d *storeData) { ban, ok = benched(d, user, time.Now()) })
	return ban, ok
}

// benchText is what a benched member is told.
func benchText(ban gameBan) string {
	text := "You're on a break from games until " + ban.Until.UTC().Format("Jan 2, 15:04 UTC")
	if ban.Reason != "" {
		text += ": " + ban.Reason
	}
	return text + "."
}

// setGameBan benches user from games for d (0 lifts it) and takes them out
// of every room.
func (b *bot) setGameBan(user string, d time.Duration, reason, by string) {
	now := time.Now()
	b.store.update(func(s *storeData) {
		if d <= 0 {
			delete(s.GameBans, user)
			return
		}
		if s.GameBans == nil {
			s.GameBans = map[string]gameBan{}
		}
		s.GameBans[user] = gameBan{Until: now.Add(min(d, gameBanMax)), By: by, Reason: clip(reason, 200)}
		for id, ban := range s.GameBans {
			if !now.Before(ban.Until) {
				delete(s.GameBans, id)
			}
		}
	})
	if d <= 0 {
		return
	}
	b.together.mu.Lock()
	for _, r := range b.together.m {
		if _, in := r.members[user]; in {
			delete(r.members, user)
			r.changed()
		}
	}
	b.together.mu.Unlock()
}

// gameBans are the running bans, soonest over first.
func (b *bot) gameBans() []struct {
	User string
	Ban  gameBan
} {
	var out []struct {
		User string
		Ban  gameBan
	}
	now := time.Now()
	b.store.view(func(d *storeData) {
		for id, ban := range d.GameBans {
			if now.Before(ban.Until) {
				out = append(out, struct {
					User string
					Ban  gameBan
				}{id, ban})
			}
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Ban.Until.Before(out[j].Ban.Until) })
	return out
}

// ── Points ──

// adjustPoints adds n (or takes -n) game points, never below zero. They
// count under "admin" so the by-game board stays about games.
func (b *bot) adjustPoints(user string, n int) int {
	week := weekKey(time.Now().UTC())
	total := 0
	b.store.update(func(d *storeData) {
		if d.Games == nil {
			d.Games = map[string]gameScore{}
		}
		s := d.Games[user]
		s.rollWeek(week)
		s.Week, s.Total = max(0, s.Week+n), max(0, s.Total+n)
		if s.Games == nil {
			s.Games = map[string]int{}
		}
		s.Games["admin"] += n
		d.Games[user] = s
		total = s.Total
	})
	return total
}

// ── Room state ──

// trollFx are the pranks on one member.
type trollFx struct {
	Flip      string    // upside or mirror
	FakeWrong bool      // right answers stamp 残念 first, then "jk"
	Dodge     time.Time // buttons run away until then
}

type trollView struct {
	Flip      string   `json:"flip,omitempty"`
	FakeWrong bool     `json:"fake_wrong,omitempty"`
	Dodge     float64  `json:"dodge,omitempty"` // seconds left
	On        []string `json:"on,omitempty"`    // prank switches (prankToggles)
}

// fxEvent is a one-off effect on one screen or all: confetti, horn, boom,
// a fake hint or fake points.
type fxEvent struct {
	ID   int    `json:"id"`
	Kind string `json:"kind"`
	To   string `json:"to,omitempty"`
	Text string `json:"text,omitempty"`
	at   time.Time
}

type adminMember struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Here      bool     `json:"here"`
	Admin     bool     `json:"admin,omitempty"`
	Kicked    bool     `json:"kicked,omitempty"`
	Muted     bool     `json:"muted,omitempty"`
	Flip      string   `json:"flip,omitempty"`
	FakeWrong bool     `json:"fake_wrong,omitempty"`
	Dodge     bool     `json:"dodge,omitempty"`
	On        []string `json:"on,omitempty"`
	Out       string   `json:"out,omitempty"` // kicked, watch, room (out for a while)
	Rename    string   `json:"rename,omitempty"`
	Slow      bool     `json:"slow,omitempty"`
	Hidden    bool     `json:"hidden,omitempty"` // name hidden as Player N
}

type adminView struct {
	Owner    bool          `json:"owner,omitempty"`
	Members  []adminMember `json:"members"`
	ImpRig   string        `json:"imp_rig,omitempty"`
	BombRig  string        `json:"bomb_rig,omitempty"`
	Trash    bool          `json:"trash,omitempty"`
	Swap     bool          `json:"swap,omitempty"`
	Roulette bool          `json:"roulette,omitempty"`
	Void     string        `json:"void,omitempty"` // the round's winner, to take back
	Skip     bool          `json:"skip,omitempty"` // the game has rounds to skip
	Ink      bool          `json:"ink,omitempty"`  // a drawing to clear
	Locked   bool          `json:"locked,omitempty"`
	Off      []string      `json:"off"`   // games turned off server-wide
	Games    []string      `json:"games"` // games that can be turned off
}

// blocked says why a member can't move right now. Hold the lock.
func (r *togetherRoom) blocked(user string) error {
	switch r.outside(user, togetherNow()) {
	case "kicked":
		return errPz("an admin sat you out until the next game")
	case "watch":
		return errPz("an admin made you a spectator")
	case "room":
		return errPz("an admin took you out of this room for a while")
	case "locked":
		return errPz("an admin locked this room")
	}
	switch {
	case !r.paused.IsZero():
		return errPz("an admin paused the game")
	}
	return nil
}

// said is a member's typed text as the feed shows it: dots when muted.
func (r *togetherRoom) said(user, text string) string {
	if r.muted[user] {
		return "•••"
	}
	return text
}

// newGame is a game starting: whoever sat out comes back, and pranks from
// a finished game end. Set-up made in the lobby stays. Hold the lock.
func (r *togetherRoom) newGame() {
	r.kicked = nil
	if r.phase == "over" {
		r.trolls, r.swap, r.toggles, r.renames = nil, nil, nil, nil
		r.roulette, r.roulMap = false, nil
	}
	r.paused = time.Time{}
}

// toLobby takes a room back to the lobby, ending every prank. Hold the lock.
func (r *togetherRoom) toLobby() {
	r.phase, r.game, r.cur, r.winners = "lobby", "", nil, nil
	r.pzBase, r.pzBoards = nil, nil
	r.pool, r.quiz, r.imp, r.draw, r.bomb, r.who, r.kk = nil, nil, nil, nil, nil, nil, nil
	r.kicked, r.muted, r.trolls, r.swap = nil, nil, nil, nil
	r.toggles, r.renames, r.slow, r.locked = nil, nil, nil, false
	r.roulette, r.roulMap = false, nil
	r.impRig, r.bombRig, r.trash = "", "", false
	r.paused = time.Time{}
}

// fxPrune forgets old effects. Hold the lock.
func (r *togetherRoom) fxPrune(now time.Time) {
	for len(r.fx) > 0 && now.Sub(r.fx[0].at) > fxKeep {
		r.fx = r.fx[1:]
	}
}

func (r *togetherRoom) addFx(kind, to, text string, now time.Time) {
	r.fxPrune(now)
	r.fxN++
	r.fx = append(r.fx, fxEvent{ID: r.fxN, Kind: kind, To: to, Text: text, at: now})
	if len(r.fx) > 20 {
		r.fx = r.fx[len(r.fx)-20:]
	}
}

func (r *togetherRoom) troll(user string) *trollFx {
	if r.trolls == nil {
		r.trolls = map[string]*trollFx{}
	}
	t := r.trolls[user]
	if t == nil {
		t = &trollFx{}
		r.trolls[user] = t
	}
	return t
}

// shiftClock moves every deadline on by d, after a pause. Hold the lock.
func (r *togetherRoom) shiftClock(d time.Duration) {
	move := func(t *time.Time) {
		if !t.IsZero() {
			*t = t.Add(d)
		}
	}
	move(&r.start)
	move(&r.ends)
	move(&r.deadline)
	if q := r.quiz; q != nil {
		move(&q.start)
		move(&q.ends)
		for id, at := range q.at {
			q.at[id] = at.Add(d)
		}
	}
	if g := r.imp; g != nil {
		move(&g.ends)
	}
	if g := r.draw; g != nil {
		move(&g.start)
		move(&g.ends)
	}
	if g := r.bomb; g != nil {
		move(&g.explode)
		move(&g.ends)
	}
	if g := r.kk; g != nil {
		for _, t := range g.tables {
			move(&t.deadline)
		}
	}
	for _, t := range r.trolls {
		move(&t.Dodge)
	}
}

// swapFaces shuffles whose picture shows for whom, nobody keeping their own
// when there are two or more.
func swapFaces(ids []string, intn func(int) int) map[string]string {
	if len(ids) < 2 {
		return nil
	}
	sort.Strings(ids)
	k := 1 + intn(len(ids)-1)
	out := map[string]string{}
	for i, id := range ids {
		out[id] = ids[(i+k)%len(ids)]
	}
	return out
}

// adminViewFill adds what user should see of the admin state. Hold the lock.
func (b *bot) adminViewFill(r *togetherRoom, v *togetherView, user string, now time.Time) {
	v.Paused = !r.paused.IsZero()
	v.Kicked = r.kicked[user]
	v.Out = r.outside(user, now)
	v.Swap = r.swap
	v.Off = b.gamesOff()
	tv := trollView{On: r.prankOn(user)}
	if t := r.trolls[user]; t != nil {
		tv.Flip, tv.FakeWrong, tv.Dodge = t.Flip, t.FakeWrong, secs(t.Dodge.Sub(now))
	}
	if tv.Flip != "" || tv.FakeWrong || tv.Dodge > 0 || len(tv.On) > 0 {
		v.Troll = &tv
	}
	for _, f := range r.fx {
		if (f.To == "" || f.To == user) && now.Sub(f.at) <= fxKeep {
			v.Fx = append(v.Fx, f)
		}
	}
	if !r.admins[user] {
		return
	}
	a := &adminView{Members: []adminMember{}, ImpRig: r.impRig, BombRig: r.bombRig, Trash: r.trash, Swap: r.swap != nil, Roulette: r.roulette,
		Locked: r.locked, Off: v.Off, Games: offable, Owner: b.isGuildOwner(user)}
	if a.Off == nil {
		a.Off = []string{}
	}
	ids := r.partyPeople()
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	var away []string
	for _, m := range []map[string]bool{r.kicked, r.watchers} {
		for id, on := range m {
			if on && !seen[id] {
				seen[id] = true
				away = append(away, id)
			}
		}
	}
	for id, until := range r.roomOut {
		if now.Before(until) && !seen[id] {
			seen[id] = true
			away = append(away, id)
		}
	}
	sort.Strings(away)
	hidden := b.hiddenNames()
	for _, id := range append(ids, away...) {
		_, here := r.members[id]
		m := adminMember{ID: id, Name: b.dashName(id), Here: here, Admin: r.admins[id], Kicked: r.kicked[id], Muted: r.muted[id],
			On: r.prankOn(id), Out: r.outside(id, now), Rename: r.renames[id], Slow: r.slow[id], Hidden: hidden[id]}
		if m.Out == "locked" {
			m.Out = ""
		}
		if t := r.trolls[id]; t != nil {
			m.Flip, m.FakeWrong, m.Dodge = t.Flip, t.FakeWrong, now.Before(t.Dodge)
		}
		a.Members = append(a.Members, m)
	}
	switch r.phase {
	case "race", "pz":
		a.Skip = !r.roundOver
		if r.roundOver || r.phase == "pz" {
			a.Void = r.winner
		}
	case "hl", "quiz", "imposter", "draw", "bomb":
		a.Skip = true
	}
	a.Ink = r.phase == "draw" && r.draw != nil && len(r.draw.ink) > 0
	v.Admin = a
}

// ── The admin menu ──

func init() {
	apiHandle("POST /activity/api/party/admin", (*bot).togetherAdmin)
}

type adminIn struct {
	Room   string `json:"room"`
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Text   string `json:"text"`
	N      int    `json:"n"`
}

// togetherAdmin runs one step from the admin menu.
func (b *bot) togetherAdmin(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	var in adminIn
	if !readJSON(w, r, 4<<10, &in) {
		return
	}
	if !togetherRoomID.MatchString(in.Room) {
		apiError(w, http.StatusBadRequest, "no room")
		return
	}
	if !b.isAdmin(user) {
		apiError(w, http.StatusForbidden, "only staff can do that")
		return
	}
	// Ticket balances belong to the server, not a particular room.
	if in.Kind == "tickets" {
		if !b.isGuildOwner(user) {
			apiError(w, http.StatusForbidden, "only the server owner can change tickets")
			return
		}
		if !ticketUserID.MatchString(in.Target) || in.N == 0 || in.N < -100 || in.N > 100 {
			apiError(w, http.StatusBadRequest, "enter a Discord user ID and 1 to 100 tickets")
			return
		}
		before := b.watchTickets(in.Target)
		balance := b.adjustWatchTickets(in.Target, in.N)
		b.adminNote(user, in.Target, fmt.Sprintf("changed %%s watch tickets from %d to %d", before, balance))
		writeJSON(w, http.StatusOK, map[string]int{"balance": balance})
		return
	}
	// Bans and points live outside the room.
	switch in.Kind {
	case "bench":
		if in.Target == "" || in.Target == user {
			apiError(w, http.StatusBadRequest, "pick someone else")
			return
		}
		hours := max(0, min(in.N, int(gameBanMax/time.Hour)))
		b.setGameBan(in.Target, time.Duration(hours)*time.Hour, in.Text, user)
		b.adminNote(user, in.Target, benchNote(hours, in.Text))
	case "points":
		if in.Target == "" || in.N == 0 || in.N < -100 || in.N > 100 {
			apiError(w, http.StatusBadRequest, "pick someone and up to 100 points")
			return
		}
		b.adjustPoints(in.Target, in.N)
		b.adminNote(user, in.Target, fmt.Sprintf("changed the game points of %%s by %+d", in.N))
	}
	now := togetherNow()
	b.together.mu.Lock()
	room := b.together.m[in.Room]
	if room == nil {
		b.together.mu.Unlock()
		if in.Kind == "bench" || in.Kind == "points" {
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		}
		apiError(w, http.StatusConflict, "this room is empty")
		return
	}
	room.setAdmin(user, true)
	room.touch(user, now)
	res, err := b.adminDo(room, user, in, now)
	if err == nil {
		room.changed()
	}
	v := b.togetherViewOf(room, user, now)
	b.together.mu.Unlock()
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	if res.take != "" {
		b.adjustPoints(res.take, -1)
	}
	if res.warn != "" {
		count, _ := b.warnMember(res.warn, user, res.reason)
		b.adminNote(user, res.warn, fmt.Sprintf("warned %%s from the games (warning %d)", count))
	}
	if res.note != "" {
		b.adminNote(user, res.who, res.note)
	}
	writeJSON(w, http.StatusOK, map[string]any{"view": v})
}

// adminResult is what a menu step leaves to do outside the lock: a note for
// staff about who, and a point to take back.
type adminResult struct {
	note, who, take string
	warn, reason    string // a warning to give, outside the lock
}

func benchNote(hours int, reason string) string {
	if hours == 0 {
		return "let %s play games again"
	}
	text := fmt.Sprintf("benched %%s from games for %s", dashPlural(hours, "hour"))
	if reason != "" {
		text += ": " + strings.ReplaceAll(clip(reason, 200), "%", "%%")
	}
	return text
}

// adminNote tells staff about a moderation step: "<admin> <what>", with %s
// in what for the member it was done to.
func (b *bot) adminNote(by, target, what string) {
	if strings.Contains(what, "%s") {
		what = fmt.Sprintf(what, "<@"+target+">")
	}
	b.staffNote(&discordgo.MessageEmbed{Title: "Games", Color: shu, Description: "<@" + by + "> " + what + "."})
}

// adminDo is one menu step on a room. Moderation steps return a note for
// staff. Hold the lock.
func (b *bot) adminDo(r *togetherRoom, user string, in adminIn, now time.Time) (adminResult, error) {
	res, err := b.adminStep(r, user, in, now)
	if err == nil {
		logAdmin(now, user, in)
	}
	if res.note != "" && res.who == "" {
		res.who = in.Target
	}
	return res, err
}

func (b *bot) adminStep(r *togetherRoom, user string, in adminIn, now time.Time) (adminResult, error) {
	var none adminResult
	note := func(text string) (adminResult, error) { return adminResult{note: text}, nil }
	target := in.Target
	needTarget := func() error {
		if target == "" {
			return errPz("pick someone")
		}
		return nil
	}
	switch in.Kind {
	case "bench", "points":
		return none, nil // done above
	case "kick":
		if err := needTarget(); err != nil {
			return none, err
		}
		if target == user {
			return none, errPz("you can't sit yourself out")
		}
		if r.kicked == nil {
			r.kicked = map[string]bool{}
		}
		r.kicked[target] = true
		delete(r.members, target)
		return note("sat %s out of a game until the next one")
	case "unkick":
		delete(r.kicked, target)
	case "mute":
		if err := needTarget(); err != nil {
			return none, err
		}
		if r.muted == nil {
			r.muted = map[string]bool{}
		}
		r.muted[target] = true
		return note("hid the answers of %s in a game")
	case "unmute":
		delete(r.muted, target)
	case "pause":
		if r.phase == "lobby" || r.phase == "over" {
			return none, errPz("no game is running")
		}
		if r.paused.IsZero() {
			r.paused = now
		}
	case "resume":
		if !r.paused.IsZero() {
			r.shiftClock(now.Sub(r.paused))
			r.paused = time.Time{}
		}
	case "skip":
		if !r.paused.IsZero() {
			return none, errPz("resume first")
		}
		return none, b.adminSkip(r, now)
	case "end":
		if r.phase == "lobby" {
			return none, errPz("no game is running")
		}
		r.toLobby()
		return note("ended a game in the Activity")
	case "void":
		if (r.phase != "race" && r.phase != "pz") || r.winner == "" {
			return none, errPz("no win to take back")
		}
		w := r.winner
		if r.scores[w] > 0 {
			r.scores[w]--
		}
		r.winner = ""
		if r.phase == "race" {
			r.feed = append(r.feed, togetherFeed{Name: "Admin", Text: "took that round back"})
		}
		return adminResult{note: "took back a round win of %s", who: w, take: w}, nil
	case "clear-ink":
		if r.draw == nil || r.phase != "draw" {
			return none, errPz("nobody is drawing")
		}
		g := r.draw
		g.ink, g.inkPts = nil, 0
		g.inkGen++
		return note("cleared a drawing")
	case "hide":
		feed := r.adminFeed()
		if feed == nil || in.N < 0 || in.N >= len(*feed) {
			return none, errPz("nothing to hide")
		}
		(*feed)[in.N].Text = "(hidden by an admin)"
	// Pranks: they end with the game, and staff aren't told.
	case "flip":
		if err := needTarget(); err != nil {
			return none, err
		}
		t := r.troll(target)
		switch {
		case in.Text != "upside" && in.Text != "mirror", t.Flip == in.Text:
			t.Flip = ""
		default:
			t.Flip = in.Text
		}
	case "fake-wrong":
		if err := needTarget(); err != nil {
			return none, err
		}
		t := r.troll(target)
		t.FakeWrong = !t.FakeWrong
	case "dodge":
		if err := needTarget(); err != nil {
			return none, err
		}
		t := r.troll(target)
		if now.Before(t.Dodge) {
			t.Dodge = time.Time{}
		} else {
			t.Dodge = now.Add(dodgeTime)
		}
	case "hint":
		if err := needTarget(); err != nil {
			return none, err
		}
		text := strings.TrimSpace(clip(in.Text, 120))
		if text == "" {
			text = sillyHints[rand.IntN(len(sillyHints))]
		}
		r.addFx("hint", target, text, now)
	case "fake-points":
		if err := needTarget(); err != nil {
			return none, err
		}
		r.addFx("points", target, "", now)
	case "confetti", "horn", "boom":
		r.addFx(in.Kind, target, "", now)
	case "fx":
		if !fxKinds[in.Text] {
			return none, errPz("unknown effect")
		}
		switch in.Text {
		case "facerain", "boo", "applause":
			// About someone, for everyone: their face rains, the room boos
			// or cheers them.
			r.addFx(in.Text, "", target, now)
		default:
			r.addFx(in.Text, target, fxText(in.Text), now)
		}
	case "toggle":
		if err := needTarget(); err != nil {
			return none, err
		}
		if !prankToggles[in.Text] {
			return none, errPz("unknown prank")
		}
		r.togglePrank(target, in.Text)
	case "rename":
		if err := needTarget(); err != nil {
			return none, err
		}
		r.renamePrank(target, strings.TrimSpace(clip(in.Text, 32)))
	case "puppet":
		if err := needTarget(); err != nil {
			return none, err
		}
		return none, b.puppet(r, target, strings.TrimSpace(clip(in.Text, 60)))
	case "lock":
		r.locked = !r.locked
		if r.locked {
			return note("locked a games room")
		}
	case "host":
		if err := needTarget(); err != nil {
			return none, err
		}
		if _, here := r.members[target]; !here {
			return none, errPz("they aren't in the room")
		}
		r.hostTo = target
	case "watch":
		if err := needTarget(); err != nil {
			return none, err
		}
		if r.watchers[target] {
			delete(r.watchers, target)
			return none, nil
		}
		if target == user {
			return none, errPz("you can't make yourself a spectator")
		}
		if r.watchers == nil {
			r.watchers = map[string]bool{}
		}
		r.watchers[target] = true
		delete(r.members, target)
		return note("made %s a spectator in a games room")
	case "room-out":
		if err := needTarget(); err != nil {
			return none, err
		}
		if now.Before(r.roomOut[target]) {
			delete(r.roomOut, target)
			return none, nil
		}
		if target == user {
			return none, errPz("you can't put yourself out")
		}
		if r.roomOut == nil {
			r.roomOut = map[string]time.Time{}
		}
		r.roomOut[target] = now.Add(roomOutTime)
		delete(r.members, target)
		return note("put %s out of a games room for 10 minutes")
	case "slow":
		if err := needTarget(); err != nil {
			return none, err
		}
		if r.slow[target] {
			delete(r.slow, target)
			return none, nil
		}
		if r.slow == nil {
			r.slow = map[string]bool{}
		}
		r.slow[target] = true
		return note("slowed the answers of %s in a game")
	case "warn":
		if err := needTarget(); err != nil {
			return none, err
		}
		reason := strings.TrimSpace(clip(in.Text, 300))
		if reason == "" {
			return none, errPz("say why")
		}
		if target == user {
			return none, errPz("you can't warn yourself")
		}
		r.addFx("warn", target, reason, now)
		return adminResult{warn: target, reason: reason}, nil
	case "hide-name":
		if err := needTarget(); err != nil {
			return none, err
		}
		if b.toggleHiddenName(target) {
			return note("hid the Discord name of %s in the games")
		}
	case "game-off":
		if !slices.Contains(offable, in.Text) {
			return none, errPz("no such game")
		}
		if b.toggleGameOff(in.Text) {
			return adminResult{note: "turned off " + liveGameName(in.Text) + " in the games"}, nil
		}
		return adminResult{note: "turned " + liveGameName(in.Text) + " back on in the games"}, nil
	case "roulette":
		r.roulette, r.roulMap = !r.roulette, nil
	case "swap":
		if r.swap != nil {
			r.swap = nil
		} else {
			r.swap = swapFaces(r.partyPeople(), rand.IntN)
		}
	case "rig-imposter":
		if r.impRig == target {
			target = ""
		}
		r.impRig = target
		if r.imp != nil {
			r.imp.rig = target
		}
	case "rig-bomb":
		if r.bombRig == target {
			target = ""
		}
		r.bombRig = target
	case "trash":
		r.trash = !r.trash
		if r.kk != nil {
			for _, t := range r.kk.tables {
				t.talk = r.trash
				if !r.trash {
					t.say = ""
				}
			}
		}
	default:
		return none, errPz("unknown admin step")
	}
	return none, nil
}

// adminFeed is the running game's answer feed, if it has one. Hold the lock.
func (r *togetherRoom) adminFeed() *[]togetherFeed {
	switch {
	case r.phase == "race":
		return &r.feed
	case r.phase == "draw" && r.draw != nil:
		return &r.draw.feed
	case r.phase == "bomb" && r.bomb != nil:
		return &r.bomb.feed
	}
	return nil
}

// adminSkip ends the current round now, nobody winning it. Hold the lock.
func (b *bot) adminSkip(r *togetherRoom, now time.Time) error {
	switch r.phase {
	case "race":
		if r.cur == nil || r.roundOver {
			return errPz("no round to skip")
		}
		r.ends = now
	case "pz":
		if r.pzBase == nil || r.roundOver {
			return errPz("no round to skip")
		}
		r.ends = now
	case "hl":
		r.deadline = now
	case "quiz":
		r.quiz.ends = now
	case "imposter":
		r.imp.ends = now
	case "draw":
		r.draw.ends = now
	case "bomb":
		g := r.bomb
		if g.stage == "play" {
			// A fresh category, nobody blown up.
			if !g.fuse(r.pool, now) {
				return errPz("no categories left")
			}
		} else {
			g.ends = now
		}
	default:
		return errPz("this game has no rounds to skip")
	}
	return nil
}
