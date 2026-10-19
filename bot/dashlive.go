package main

import (
	"fmt"
	"html/template"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// The Live games dashboard page: every Activity room with who is in it and
// the admin steps that fit a phone (pause, skip, end, sit someone out),
// members on a break from games, and fixing game points.

func init() {
	dashSections = append(dashSections, dashSection{Slug: "live-games", Name: "Live games", Render: (*bot).dashLive, Controls: true})
	for do, f := range map[string]func(b *bot, in dashInput, who string) error{
		"live-room":   (*bot).dashLiveRoom,
		"live-kick":   (*bot).dashLiveKick,
		"game-bench":  (*bot).dashBench,
		"game-unban":  (*bot).dashUnbench,
		"game-points": (*bot).dashGamePoints,
		"game-off":    (*bot).dashGameOff,
	} {
		dashMoreControls[do] = f
	}
}

// liveRoom is a room as the page lists it.
type liveRoom struct {
	id, phase, game string
	paused          bool
	members         []string
	kicked          []string
}

func (b *bot) liveRooms() []liveRoom {
	var out []liveRoom
	b.together.mu.Lock()
	for _, r := range b.together.m {
		if len(r.members) == 0 {
			continue
		}
		lr := liveRoom{id: r.id, phase: r.phase, game: r.game, paused: !r.paused.IsZero(), members: r.partyPeople()}
		for id, k := range r.kicked {
			if k {
				lr.kicked = append(lr.kicked, id)
			}
		}
		sort.Strings(lr.kicked)
		out = append(out, lr)
	}
	b.together.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].members) != len(out[j].members) {
			return len(out[i].members) > len(out[j].members)
		}
		return out[i].id < out[j].id
	})
	return out
}

// liveGameName is a room's game in words.
func liveGameName(game string) string {
	switch game {
	case "":
		return "Lobby"
	case "hl":
		return gameNames["higher-lower"]
	case "quiz":
		return "Quiz show"
	case "truths":
		return "Two truths and a lie"
	case "imposter":
		return "Imposter"
	case "draw":
		return "Draw it"
	case "bomb":
		return "Hot potato"
	case "who":
		return "Guess who"
	case "koikoi":
		return "Koi-Koi"
	}
	if n, ok := gameNames[game]; ok {
		return n
	}
	return game
}

func (b *bot) dashLive() template.HTML {
	rooms := b.liveRooms()
	bans := b.gameBans()
	players := 0
	playing := 0
	for _, r := range rooms {
		players += len(r.members)
		if r.phase != "lobby" && r.phase != "over" {
			playing++
		}
	}

	var roomParts []template.HTML
	for k, r := range rooms {
		state := "in the lobby"
		switch {
		case r.paused:
			state = "paused"
		case r.phase == "over":
			state = "game over"
		case r.phase != "lobby":
			state = "playing"
		}
		var names []string
		var choices []dashChoice
		for _, id := range r.members {
			names = append(names, b.dashName(id))
			choices = append(choices, dashChoice{Value: id, Label: b.dashName(id)})
		}
		var ctls []dashControl
		if r.phase != "lobby" && r.phase != "over" {
			if r.paused {
				ctls = append(ctls, dashControl{Section: "live-games", Do: "live-room", Name: r.id, Choices: []dashChoice{{Value: "resume", Label: "Resume"}}, Button: "Go"})
			} else {
				ctls = append(ctls, dashControl{Section: "live-games", Do: "live-room", Name: r.id,
					Choices: []dashChoice{{Value: "pause", Label: "Pause"}, {Value: "skip", Label: "Skip round"}, {Value: "end", Label: "End game"}}, Button: "Go"})
			}
		}
		if len(choices) > 0 {
			ctls = append(ctls, dashControl{Section: "live-games", Do: "live-kick", Name: r.id, Choices: choices, Button: "Sit out this game", Quiet: true})
		}
		sub := dashPlural(len(r.members), "player") + " · " + state
		if len(r.kicked) > 0 {
			var out []string
			for _, id := range r.kicked {
				out = append(out, b.dashName(id))
			}
			sub += " · sitting out: " + strings.Join(out, ", ")
		}
		roomParts = append(roomParts,
			dashHeading(fmt.Sprintf("Room %d: %s", k+1, liveGameName(r.game))),
			dashNote(sub),
			dashNote(strings.Join(names, ", ")),
			dashControls(ctls...))
	}
	if len(roomParts) == 0 {
		roomParts = append(roomParts, dashNote("Nobody has the games Activity open."))
	}

	var banRows [][]template.HTML
	for _, x := range bans {
		reason := x.Ban.Reason
		if reason == "" {
			reason = "—"
		}
		by := "—"
		if x.Ban.By != "" {
			by = b.dashName(x.Ban.By)
		}
		banRows = append(banRows, []template.HTML{
			dashText(template.HTMLEscapeString(b.dashName(x.User))),
			dashText(x.Ban.Until.UTC().Format("Jan 2, 15:04 UTC")),
			dashText(template.HTMLEscapeString(reason)),
			dashText(template.HTMLEscapeString(by)),
			dashControls(dashControl{Section: "live-games", Do: "game-unban", Name: x.User, Button: "Lift", Quiet: true}),
		})
	}

	people := b.liveMemberChoices(rooms)
	return dashJoin(
		dashStats(
			dashStat{"Rooms open", fmt.Sprint(len(rooms))},
			dashStat{"In the Activity", fmt.Sprint(players)},
			dashStat{"Playing a game", fmt.Sprint(playing)},
			dashStat{"On a break from games", fmt.Sprint(len(bans))},
		),
		dashNote("Staff get the same steps, and the pranks, from the shield button inside the Activity."),
		dashJoin(roomParts...),
		dashHeading("Breaks from games"),
		dashNote("A member on a break can't open the games or earn points until it ends. /game-ban does the same in Discord."),
		dashHTMLTable([]string{"Member", "Until", "Reason", "By", ""}, banRows, "Nobody is on a break."),
		dashControls(dashControl{Section: "live-games", Do: "game-bench", Choices: people,
			Number: &dashNumber{Value: "24", Min: "1", Max: "720", Step: "1", Field: "name"}, Line: "Reason (they see it)", Button: "Give a break (hours)"}),
		dashHeading("Games on and off"),
		dashNote("A game turned off is greyed out for everyone, alone or in a room, until it is turned back on."),
		dashNote(b.offNote()),
		dashControls(dashControl{Section: "live-games", Do: "game-off", Choices: b.offChoices(), Button: "Turn off / on", Quiet: true}),
		dashHeading("Admin log"),
		dashNote("The last 50 moderation steps from the Activity and this page, newest first. Pranks stay out of it."),
		dashHTMLTable([]string{"When", "Who", "Step", "Member"}, b.adminLogRows(), "No steps yet since the bot started."),
		dashHeading("Fix points"),
		dashNote("Give or take game points, for a lost win or a cheated one. Minus takes points; nobody goes below 0."),
		dashControls(dashControl{Section: "live-games", Do: "game-points", Choices: people,
			Number: &dashNumber{Value: "1", Min: "-100", Max: "100", Step: "1", Field: "name"}, Button: "Change points"}),
	)
}

// liveMemberChoices are the members a form can pick: everyone in a room,
// then everyone with game points.
func (b *bot) liveMemberChoices(rooms []liveRoom) []dashChoice {
	seen := map[string]bool{}
	var out []dashChoice
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, dashChoice{Value: id, Label: b.dashName(id)})
	}
	for _, r := range rooms {
		for _, id := range r.members {
			add(id)
		}
	}
	var top []guessRank
	b.store.view(func(d *storeData) { top = ranked(allPoints(d), 60) })
	for _, r := range top {
		add(r.User)
	}
	return out
}

func (b *bot) dashLiveRoom(in dashInput, who string) error {
	switch in.V {
	case "pause", "resume", "skip", "end":
	default:
		return fmt.Errorf("Pick pause, resume, skip or end.")
	}
	if err := b.liveDo(in.Name, adminIn{Kind: in.V}); err != nil {
		return err
	}
	if in.V == "end" {
		b.staffNote(&discordgo.MessageEmbed{Title: "Games", Color: shu, Description: who + " ended a game in the Activity."})
	}
	setFlash(false, "Done: %s.", map[string]string{"pause": "paused", "resume": "running again", "skip": "round skipped", "end": "back in the lobby"}[in.V])
	return nil
}

func (b *bot) dashLiveKick(in dashInput, who string) error {
	if in.V == "" {
		return fmt.Errorf("Pick someone.")
	}
	if err := b.liveDo(in.Name, adminIn{Kind: "kick", Target: in.V}); err != nil {
		return err
	}
	b.staffNote(&discordgo.MessageEmbed{Title: "Games", Color: shu, Description: fmt.Sprintf("%s sat <@%s> out of a game until the next one.", who, in.V)})
	setFlash(false, "%s sits out until the next game.", b.dashName(in.V))
	return nil
}

// liveDo runs an admin step on a room from the dashboard.
func (b *bot) liveDo(roomID string, in adminIn) error {
	now := togetherNow()
	b.together.mu.Lock()
	defer b.together.mu.Unlock()
	r := b.together.m[roomID]
	if r == nil {
		return fmt.Errorf("That room is gone.")
	}
	if _, err := b.adminDo(r, "", in, now); err != nil {
		msg := err.Error()
		return fmt.Errorf("%s.", strings.ToUpper(msg[:1])+msg[1:])
	}
	r.changed()
	return nil
}

func (b *bot) dashBench(in dashInput, who string) error {
	hours, err := strconv.Atoi(in.Name)
	if err != nil || hours < 1 || hours > 720 || in.V == "" {
		return fmt.Errorf("Pick someone and 1 to 720 hours.")
	}
	b.setGameBan(in.V, time.Duration(hours)*time.Hour, in.Text, "")
	logAdmin(togetherNow(), "", adminIn{Kind: "bench", Target: in.V, Text: in.Text})
	b.staffNote(&discordgo.MessageEmbed{Title: "Games", Color: shu,
		Description: fmt.Sprintf("%s gave <@%s> a break from games for %s: %s", who, in.V, dashPlural(hours, "hour"), clip(in.Text, 200))})
	setFlash(false, "%s is on a break from games for %s.", b.dashName(in.V), dashPlural(hours, "hour"))
	return nil
}

func (b *bot) dashUnbench(in dashInput, who string) error {
	b.setGameBan(in.Name, 0, "", "")
	logAdmin(togetherNow(), "", adminIn{Kind: "bench", Target: in.Name, Text: "lifted"})
	b.staffNote(&discordgo.MessageEmbed{Title: "Games", Color: shu, Description: fmt.Sprintf("%s let <@%s> play games again.", who, in.Name)})
	setFlash(false, "%s can play games again.", b.dashName(in.Name))
	return nil
}

// modSteps are the logged steps in words; pranks aren't in it.
var modSteps = map[string]string{
	"kick": "sat out until the next game", "unkick": "let back in", "mute": "hid answers", "unmute": "showed answers",
	"pause": "paused", "resume": "resumed", "skip": "skipped a round", "end": "ended the game", "void": "took a win back",
	"clear-ink": "cleared a drawing", "hide": "hid a feed line", "bench": "break from games", "points": "changed points",
	"lock": "locked / unlocked the room", "host": "made host", "watch": "spectator on / off", "room-out": "out of the room / back",
	"slow": "slow answers on / off", "warn": "warned", "hide-name": "hid / showed the name", "game-off": "turned a game off / on",
}

func (b *bot) adminLogRows() [][]template.HTML {
	var rows [][]template.HTML
	for _, e := range adminLogged() {
		what, ok := modSteps[e.Kind]
		if !ok {
			continue
		}
		by := "Dashboard"
		if e.By != "" {
			by = b.dashName(e.By)
		}
		switch e.Kind {
		case "game-off":
			what += ": " + liveGameName(e.Text)
		case "warn", "bench", "points":
			if e.Text != "" {
				what += ": " + e.Text
			}
		}
		member := "—"
		if e.Target != "" {
			member = b.dashName(e.Target)
		}
		rows = append(rows, []template.HTML{
			dashText(e.At.UTC().Format("Jan 2, 15:04")),
			dashText(template.HTMLEscapeString(by)),
			dashText(template.HTMLEscapeString(what)),
			dashText(template.HTMLEscapeString(member)),
		})
	}
	return rows
}

func (b *bot) offNote() string {
	off := b.gamesOff()
	if len(off) == 0 {
		return "Every game is on."
	}
	var names []string
	for _, g := range off {
		names = append(names, liveGameName(g))
	}
	return "Off now: " + strings.Join(names, ", ") + "."
}

func (b *bot) offChoices() []dashChoice {
	off := b.gamesOff()
	var out []dashChoice
	for _, g := range offable {
		label := liveGameName(g)
		if slices.Contains(off, g) {
			label += " (off)"
		}
		out = append(out, dashChoice{Value: g, Label: label})
	}
	return out
}

func (b *bot) dashGameOff(in dashInput, who string) error {
	if !slices.Contains(offable, in.V) {
		return fmt.Errorf("Pick a game.")
	}
	logAdmin(togetherNow(), "", adminIn{Kind: "game-off", Text: in.V})
	if b.toggleGameOff(in.V) {
		b.staffNote(&discordgo.MessageEmbed{Title: "Games", Color: shu, Description: who + " turned off " + liveGameName(in.V) + " in the games."})
		setFlash(false, "%s is off.", liveGameName(in.V))
		return nil
	}
	b.staffNote(&discordgo.MessageEmbed{Title: "Games", Color: shu, Description: who + " turned " + liveGameName(in.V) + " back on in the games."})
	setFlash(false, "%s is on again.", liveGameName(in.V))
	return nil
}

func (b *bot) dashGamePoints(in dashInput, who string) error {
	n, err := strconv.Atoi(in.Name)
	if err != nil || n == 0 || n < -100 || n > 100 || in.V == "" {
		return fmt.Errorf("Pick someone and -100 to 100 points, not 0.")
	}
	total := b.adjustPoints(in.V, n)
	logAdmin(togetherNow(), "", adminIn{Kind: "points", Target: in.V, Text: fmt.Sprintf("%+d", n)})
	b.staffNote(&discordgo.MessageEmbed{Title: "Games", Color: shu, Description: fmt.Sprintf("%s changed the game points of <@%s> by %+d.", who, in.V, n)})
	setFlash(false, "%s has %d game points now.", b.dashName(in.V), total)
	return nil
}
