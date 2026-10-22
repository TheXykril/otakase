package main

// The cheat code. Entering the Konami code in Otakase (↑↑↓↓←→←→BA in the
// terminal, or typing uuddlrlrba in rofi) starts a puzzle hunt made for the
// member whose app is linked (cheatpuzzles.go). Solving it mints a one-time
// code. Codes are not tied to anyone: the finder can redeem it or give it
// away, and the first /redeem wins. Each redeemed code is numbered in the
// hall of fame (/cheaters) as "found by X, redeemed by Y", and the
// redeemer gets the Cheater role. A member finds at most one code a week.
//
//	POST /v1/cheat (auth)        → the member's hunt, started when there is none
//	POST /v1/cheat/answer (auth) {"answer":"…"} → {"correct", "message"} and the hunt
//
// Both answer a cheatView. Whispers are /whisper in Discord. None of the
// commands are in /help: finding them is part of it.

import (
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	cheatRole        = "Cheater"
	cheatHuntTTL     = 7 * 24 * time.Hour // an unsolved hunt is replaced after this
	cheatMaxWrong    = 25                 // wrong answers before a hunt is lost
	cheatLostPause   = time.Hour          // before a new hunt after losing one
	cheatCodeLetters = 12
)

// cheatHunt is a member's current hunt.
type cheatHunt struct {
	Puzzle  huntPuzzle `json:"puzzle"`
	Step    int        `json:"step"` // index into Puzzle.Steps
	Started time.Time  `json:"started"`
	Wrong   int        `json:"wrong,omitempty"`
	Lost    time.Time  `json:"lost,omitzero"` // too many wrong answers
}

// cheatCode is a minted code, by the code itself. Codes staff made with
// /codes (codes.go) have no Finder but MintedBy and Note, and stay out of
// the hall of fame.
type cheatCode struct {
	Finder     string    `json:"finder"` // Discord user id
	Found      time.Time `json:"found"`
	MintedBy   string    `json:"minted_by,omitempty"`
	Note       string    `json:"note,omitempty"` // what staff made it for, like a giveaway
	Kind       string    `json:"kind"`
	RedeemedBy string    `json:"redeemed_by,omitempty"`
	Redeemed   time.Time `json:"redeemed,omitzero"`
	Number     int       `json:"number,omitempty"` // place in the hall of fame
}

// cheatView is what the app shows. Status is "puzzle", "found" (a code,
// shown until the week is over) or "wait" (come back at Until).
type cheatView struct {
	Status    string    `json:"status"`
	Kind      string    `json:"kind,omitempty"`
	Step      int       `json:"step,omitempty"` // from 1
	Steps     int       `json:"steps,omitempty"`
	Text      string    `json:"text,omitempty"`
	Links     []string  `json:"links,omitempty"`
	Where     string    `json:"where,omitempty"` // "app" or "discord"
	WrongLeft int       `json:"wrong_left,omitempty"`
	Code      string    `json:"code,omitempty"`
	Redeemed  bool      `json:"redeemed,omitempty"`
	Until     time.Time `json:"until,omitzero"`
	Message   string    `json:"message,omitempty"`
}

var (
	cheatStartLimit  = newRateLimit(30, time.Hour)
	cheatAnswerLimit = newRateLimit(10, time.Hour) // wrong answers and whispers per member
	cheatRedeemFails = newRateLimit(5, 10*time.Minute)
)

// cheatRand is the puzzles' randomness, a variable for tests.
var cheatRand = func() func(int) int { return rand.IntN }

// cheatPool is the shows puzzles are built from, a variable for tests.
var cheatPool = func(b *bot) ([]guessShow, error) { return b.guess.shows() }

func init() {
	apiHandle("POST /v1/cheat", (*bot).apiCheat)
	apiHandle("POST /v1/cheat/answer", (*bot).apiCheatAnswer)
	registerCommand(&discordgo.ApplicationCommand{Name: "whisper", Description: "Whisper something to the bot",
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "words", Description: "What you whisper", Required: true, MaxLength: 100},
		}}, (*bot).whisperCommand)
	registerCommand(&discordgo.ApplicationCommand{Name: "redeem", Description: "Redeem a code",
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "code", Description: "The code, like ABCD-EFGH-JKLM", Required: true, MaxLength: 20},
		}}, (*bot).redeemCommand)
	registerCommand(&discordgo.ApplicationCommand{Name: "cheaters", Description: "The hall of fame"}, (*bot).cheatersCommand)
}

// nextWeek is when the ISO week of t ends: Monday 00:00 UTC.
func nextWeek(t time.Time) time.Time {
	t = t.UTC()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return day.AddDate(0, 0, 8-int((t.Weekday()+6)%7+1))
}

// weekCode is the code user found this ISO week, if any.
func weekCode(d *storeData, user string, now time.Time) (string, cheatCode, bool) {
	week := weekKey(now.UTC())
	for c, x := range d.CheatCodes {
		if x.Finder == user && weekKey(x.Found.UTC()) == week {
			return c, x, true
		}
	}
	return "", cheatCode{}, false
}

func newCheatCode() string {
	var b strings.Builder
	for b.Len() < cheatCodeLetters {
		b.WriteString(newLinkCode())
	}
	return b.String()[:cheatCodeLetters]
}

func formatCheatCode(c string) string { return c[:4] + "-" + c[4:8] + "-" + c[8:] }

// normalizeCheatCode reads a code as people type it, dashes and spaces optional.
func normalizeCheatCode(s string) (string, bool) {
	s = strings.ToUpper(strings.NewReplacer("-", "", " ", "", "–", "").Replace(strings.TrimSpace(s)))
	if len(s) != cheatCodeLetters {
		return "", false
	}
	for _, c := range s {
		if !strings.ContainsRune(linkCodeAlphabet, c) {
			return "", false
		}
	}
	return s, true
}

// huntView is the hunt as the app shows it.
func huntView(h cheatHunt) cheatView {
	s := h.Puzzle.Steps[h.Step]
	v := cheatView{Status: "puzzle", Kind: h.Puzzle.Kind, Step: h.Step + 1, Steps: len(h.Puzzle.Steps),
		Text: s.Text, Links: s.Links, Where: "app", WrongLeft: cheatMaxWrong - h.Wrong}
	if s.Whisper != "" {
		v.Where = "discord"
	}
	return v
}

// cheatState is the member's hunt, or why there is none: a code found this
// week, or a lost hunt still cooling off. ok is false when a new hunt is due.
func cheatState(d *storeData, user string, now time.Time) (cheatView, bool) {
	if c, x, found := weekCode(d, user, now); found {
		return cheatView{Status: "found", Code: formatCheatCode(c), Redeemed: x.RedeemedBy != "", Until: nextWeek(now),
			Message: "You found this week's code. Another hunt opens next week."}, true
	}
	h, ok := d.CheatHunts[user]
	switch {
	case !ok:
		return cheatView{}, false
	case !h.Lost.IsZero() && now.Sub(h.Lost) < cheatLostPause:
		return cheatView{Status: "wait", Until: h.Lost.Add(cheatLostPause),
			Message: "Too many wrong answers. The trail went cold; a new one appears in a while."}, true
	case !h.Lost.IsZero(), now.Sub(h.Started) > cheatHuntTTL, h.Step >= len(h.Puzzle.Steps):
		return cheatView{}, false
	}
	return huntView(h), true
}

// cheatStart is the member's hunt, made when they have none.
func (b *bot) cheatStart(user string, now time.Time) (cheatView, error) {
	var v cheatView
	var ok bool
	b.store.view(func(d *storeData) { v, ok = cheatState(d, user, now) })
	if ok {
		return v, nil
	}
	pool, err := cheatPool(b)
	if err != nil {
		return cheatView{}, err
	}
	p, err := newHuntPuzzle(pool, cheatRand())
	if err != nil {
		return cheatView{}, err
	}
	b.store.update(func(d *storeData) {
		// Another request may have started one meanwhile.
		if v, ok = cheatState(d, user, now); ok {
			return
		}
		if d.CheatHunts == nil {
			d.CheatHunts = map[string]cheatHunt{}
		}
		h := cheatHunt{Puzzle: p, Started: now}
		d.CheatHunts[user] = h
		v = huntView(h)
	})
	log.Printf("cheat: member %s started a %s hunt", user, p.Kind)
	return v, nil
}

// cheatAnswer checks an answer typed in the app.
func (b *bot) cheatAnswer(user, answer string, now time.Time) (correct bool, msg string, v cheatView) {
	norm := huntNorm(answer)
	b.store.update(func(d *storeData) {
		var ok bool
		if v, ok = cheatState(d, user, now); !ok || v.Status != "puzzle" {
			if !ok {
				v, msg = cheatView{Status: "gone"}, "This hunt is over. Enter the code again for a new one."
			}
			return
		}
		h := d.CheatHunts[user]
		step := h.Puzzle.Steps[h.Step]
		switch {
		case step.Whisper != "" && norm == step.Whisper:
			msg = "Not here. Somewhere else is listening."
			return
		case step.Whisper == "" && slices.Contains(step.Answers, norm):
			correct = true
			h.Step++
		default:
			h.Wrong++
			if h.Wrong >= cheatMaxWrong {
				h.Lost = now
			}
			msg = "Nothing happens."
		}
		if h.Step >= len(h.Puzzle.Steps) {
			code := newCheatCode()
			for d.CheatCodes[code].Finder != "" {
				code = newCheatCode()
			}
			if d.CheatCodes == nil {
				d.CheatCodes = map[string]cheatCode{}
			}
			d.CheatCodes[code] = cheatCode{Finder: user, Found: now, Kind: h.Puzzle.Kind}
			delete(d.CheatHunts, user)
			log.Printf("cheat: member %s solved a %s hunt", user, h.Puzzle.Kind)
			v = cheatView{Status: "found", Code: formatCheatCode(code), Until: nextWeek(now),
				Message: "Solved. This code works once, for whoever redeems it first with /redeem in Discord. Keep it or give it away."}
			return
		}
		d.CheatHunts[user] = h
		v, _ = cheatState(d, user, now)
	})
	return correct, msg, v
}

// cheatWhisper checks a /whisper. It returns the next step's text when the
// whisper was the one the hunt waits for.
func (b *bot) cheatWhisper(user, words string, now time.Time) (string, bool) {
	norm := huntNorm(words)
	var next string
	var ok bool
	b.store.update(func(d *storeData) {
		v, active := cheatState(d, user, now)
		if !active || v.Status != "puzzle" {
			return
		}
		h := d.CheatHunts[user]
		step := h.Puzzle.Steps[h.Step]
		if step.Whisper == "" {
			return // nothing to whisper yet; not counted against them
		}
		if norm != step.Whisper {
			h.Wrong++
			if h.Wrong >= cheatMaxWrong {
				h.Lost = now
			}
			d.CheatHunts[user] = h
			return
		}
		h.Step++
		d.CheatHunts[user] = h
		next, ok = h.Puzzle.Steps[h.Step].Text, true
	})
	return next, ok
}

// redeemCheat redeems a code for user and returns its number in the hall of
// fame and who found it.
func (b *bot) redeemCheat(user, input string, now time.Time) (cheatCode, error) {
	if cheatRedeemFails.full(user, now) {
		return cheatCode{}, errLinkTooMany
	}
	code, valid := normalizeCheatCode(input)
	var x cheatCode
	var err error
	b.store.update(func(d *storeData) {
		var ok bool
		x, ok = d.CheatCodes[code]
		switch {
		case !valid || !ok:
			err = errLinkWrong
		case x.RedeemedBy != "":
			err = errLinkNotFound
		default:
			x.RedeemedBy, x.Redeemed = user, now
			if x.Finder != "" {
				n := 1
				for _, c := range d.CheatCodes {
					n = max(n, c.Number+1)
				}
				x.Number = n
			}
			d.CheatCodes[code] = x
			if d.WatchTickets == nil {
				d.WatchTickets = map[string]int{}
			}
			d.WatchTickets[user]++
		}
	})
	if err == errLinkWrong {
		cheatRedeemFails.allow(user, now)
	}
	return x, err
}

// hallOfFame is the redeemed codes, first first.
func hallOfFame(d *storeData) []cheatCode {
	var out []cheatCode
	for _, c := range d.CheatCodes {
		if c.Number > 0 {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b cheatCode) int { return a.Number - b.Number })
	return out
}

// ── App side ──

func (b *bot) apiCheat(w http.ResponseWriter, r *http.Request) {
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	now := appNow()
	if !cheatStartLimit.allow(user, now) {
		apiError(w, http.StatusTooManyRequests, "slow down")
		return
	}
	v, err := b.cheatStart(user, now)
	if err != nil {
		log.Printf("cheat: %v", err)
		apiError(w, http.StatusServiceUnavailable, "the puzzles aren't ready, try again in a few minutes")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (b *bot) apiCheatAnswer(w http.ResponseWriter, r *http.Request) {
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	var req struct {
		Answer string `json:"answer"`
	}
	if !readJSON(w, r, 4096, &req) {
		return
	}
	now := appNow()
	if cheatAnswerLimit.full(user, now) {
		apiError(w, http.StatusTooManyRequests, "too many wrong answers, try again in an hour")
		return
	}
	correct, msg, v := b.cheatAnswer(user, cleanText(req.Answer, false), now)
	if !correct && v.Status == "puzzle" && msg == "Nothing happens." {
		cheatAnswerLimit.allow(user, now)
	}
	writeJSON(w, http.StatusOK, struct {
		Correct bool `json:"correct"`
		cheatView
	}{correct, withMessage(v, msg)})
}

func withMessage(v cheatView, msg string) cheatView {
	if msg != "" {
		v.Message = msg
	}
	return v
}

// ── Discord side ──

func (b *bot) whisperCommand(i *discordgo.InteractionCreate) {
	user := i.Member.User.ID
	words := ""
	for _, o := range i.ApplicationCommandData().Options {
		if o.Name == "words" {
			words = o.StringValue()
		}
	}
	now := appNow()
	if cheatAnswerLimit.full(user, now) {
		b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: "The bot is not listening right now."})
		return
	}
	next, ok := b.cheatWhisper(user, words, now)
	if !ok {
		cheatAnswerLimit.allow(user, now)
		b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: "The bot looks at you blankly."})
		return
	}
	b.respondQuiet(i, true, &discordgo.MessageEmbed{Color: shu, Description: next})
}

func (b *bot) redeemCommand(i *discordgo.InteractionCreate) {
	user := i.Member.User.ID
	input := ""
	for _, o := range i.ApplicationCommandData().Options {
		if o.Name == "code" {
			input = o.StringValue()
		}
	}
	x, err := b.redeemCheat(user, input, appNow())
	switch err {
	case nil:
	case errLinkTooMany:
		b.respond(i, true, errorEmbed("Too many wrong codes. Wait a few minutes."))
		return
	case errLinkNotFound:
		b.respond(i, true, errorEmbed("Someone already redeemed that code."))
		return
	default:
		b.respond(i, true, errorEmbed("That code doesn't exist."))
		return
	}
	if x.Finder == "" {
		b.respondQuiet(i, true, &discordgo.MessageEmbed{Title: "Code accepted", Color: shu,
			Description: "You got a **watch ticket**: one watch session hosted by the bot in Discord, once the watch room opens. " + watchTicketLine(b, user)})
		return
	}
	who := fmt.Sprintf("found by <@%s>", x.Finder)
	if x.Finder == user {
		who = "found and redeemed by you"
	}
	b.respondQuiet(i, true, &discordgo.MessageEmbed{Title: fmt.Sprintf("Cheater #%d", x.Number), Color: shu,
		Description: fmt.Sprintf("Code accepted, %s. You're in the hall of fame (`/cheaters`), the %s role is yours, and so is a **watch ticket** for the watch room once it opens. %s",
			who, cheatRole, watchTicketLine(b, user))})
	go b.cheatReward(x)
}

// cheatReward gives the role and tells the server.
func (b *bot) cheatReward(x cheatCode) {
	if b.s == nil || b.s.State == nil {
		return
	}
	if role := b.cheatRoleID(); role != "" {
		if err := b.s.GuildMemberRoleAdd(b.cfg.GuildID, x.RedeemedBy, role); err != nil {
			log.Printf("cheater role: %v", err)
		}
	}
	ch := b.gamesChannel()
	if ch == "" {
		return
	}
	line := fmt.Sprintf("Found by <@%s>, redeemed by <@%s>.", x.Finder, x.RedeemedBy)
	if x.Finder == x.RedeemedBy {
		line = fmt.Sprintf("Found and redeemed by <@%s>.", x.Finder)
	}
	e := &discordgo.MessageEmbed{Title: fmt.Sprintf("Cheat code #%d redeemed", x.Number), Color: shu,
		Description: line + "\n\nHow? That's for you to find out."}
	if _, err := b.s.ChannelMessageSendComplex(ch, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{e},
		AllowedMentions: &discordgo.MessageAllowedMentions{}}); err != nil {
		log.Printf("cheat post: %v", err)
	}
}

// cheatRoleID finds the Cheater role, making it when it's missing.
func (b *bot) cheatRoleID() string {
	g, err := b.s.State.Guild(b.cfg.GuildID)
	if err != nil {
		return ""
	}
	for _, r := range g.Roles {
		if strings.EqualFold(strings.TrimSpace(r.Name), cheatRole) {
			return r.ID
		}
	}
	color := 0x7FD962
	r, err := b.s.GuildRoleCreate(b.cfg.GuildID, &discordgo.RoleParams{Name: cheatRole, Color: &color})
	if err != nil {
		log.Printf("cheater role create: %v", err)
		return ""
	}
	return r.ID
}

func (b *bot) cheatersCommand(i *discordgo.InteractionCreate) {
	var fame []cheatCode
	b.store.view(func(d *storeData) { fame = hallOfFame(d) })
	b.respondQuiet(i, false, cheatersEmbed(fame))
}

func cheatersEmbed(fame []cheatCode) *discordgo.MessageEmbed {
	if len(fame) == 0 {
		return &discordgo.MessageEmbed{Title: "Hall of fame", Color: shu, Description: "Nobody yet. There is a way in, somewhere."}
	}
	var lines []string
	for _, c := range fame[max(0, len(fame)-25):] {
		who := fmt.Sprintf("found by <@%s>, redeemed by <@%s>", c.Finder, c.RedeemedBy)
		if c.Finder == c.RedeemedBy {
			who = fmt.Sprintf("found and redeemed by <@%s>", c.Finder)
		}
		lines = append(lines, fmt.Sprintf("**#%d** · %s · %s · <t:%d:d>", c.Number, who, c.Kind, c.Redeemed.Unix()))
	}
	return &discordgo.MessageEmbed{Title: "Hall of fame", Color: shu, Description: strings.Join(lines, "\n"),
		Footer: &discordgo.MessageEmbedFooter{Text: dashPlural(len(fame), "code") + " redeemed"}}
}
