package main

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Game points: every game adds to one weekly and all-time board (/leaderboard),
// next to the guess the anime points. Higher or lower, the daily puzzle,
// emoji riddles, title scramble and watch bingo keep their points here;
// guess the anime keeps its own (store.Guess) and counts too. When a week
// ends, its winner gets the "Game Champion" role until the next one.

const championRole = "Game Champion"

// Points per game.
const (
	hlEvery    = 3 // a point for every 3 right in a row
	hlDailyCap = 5 // at most 5 higher or lower points a day
	bingoLine  = 2
	bingoFull  = 5
)

var gameNames = map[string]string{
	"guess":        "Guess the anime",
	"daily":        "Daily puzzle",
	"higher-lower": "Higher or lower",
	"riddle":       "Emoji riddles",
	"scramble":     "Title scramble",
	"bingo":        "Watch bingo",
	"coverle":      "Zoomed cover",
	"timeline":     "Timeline",
	"connections":  "Connections",
	"redacted":     "Redacted",
	"grid":         "Anime grid",
	"hangman":      "Hangman",
	"wordle":       "Wordle",
	"quiz":         "Quiz",
	"truths":       "Two truths, one lie",
	"imposter":     "Imposter",
	"draw":         "Draw it",
	"bomb":         "Hot potato",
	"who":          "Guess who",
}

// gameScore is a member's points from the games other than guess the anime.
type gameScore struct {
	Total      int            `json:"total"`
	Week       int            `json:"week"`
	WeekOf     string         `json:"week_of"`
	LastWeek   int            `json:"last_week,omitempty"` // points in the week before WeekOf
	LastWeekOf string         `json:"last_week_of,omitempty"`
	Games      map[string]int `json:"games,omitempty"` // all-time points by game
	// Higher or lower: points today (for the cap) and the best run.
	HLDay   string `json:"hl_day,omitempty"`
	HLToday int    `json:"hl_today,omitempty"`
	HLBest  int    `json:"hl_best,omitempty"`
	// Solo wins in the Activity today, for its cap.
	ActDay   string `json:"act_day,omitempty"`
	ActToday int    `json:"act_today,omitempty"`
}

// rollWeek moves the week's points to LastWeek when a new week started.
func (s *gameScore) rollWeek(week string) {
	if s.WeekOf == week {
		return
	}
	if s.WeekOf != "" {
		s.LastWeek, s.LastWeekOf = s.Week, s.WeekOf
	}
	s.Week, s.WeekOf = 0, week
}

// addPoints gives a member points in a game and returns their total for
// this week across all games.
func (b *bot) addPoints(user, game string, n int) int {
	if n <= 0 {
		return 0
	}
	week := weekKey(time.Now().UTC())
	var total int
	b.store.update(func(d *storeData) {
		addGamePoints(d, user, game, n, week)
		total = weekPoints(d, week)[user]
	})
	return total
}

func addGamePoints(d *storeData, user, game string, n int, week string) {
	if _, ok := benched(d, user, time.Now()); ok {
		return // on a break from games: no points
	}
	if d.Games == nil {
		d.Games = map[string]gameScore{}
	}
	s := d.Games[user]
	s.rollWeek(week)
	s.Week += n
	s.Total += n
	if s.Games == nil {
		s.Games = map[string]int{}
	}
	s.Games[game] += n
	d.Games[user] = s
	earnHanko(d, user, n)
}

// weekPoints is every member's points in a week, guess the anime included.
func weekPoints(d *storeData, week string) map[string]int {
	out := map[string]int{}
	for u, s := range d.Games {
		switch week {
		case s.WeekOf:
			out[u] += s.Week
		case s.LastWeekOf:
			out[u] += s.LastWeek
		}
	}
	for u, s := range d.Guess {
		switch week {
		case s.WeekOf:
			out[u] += s.Week
		case s.LastWeekOf:
			out[u] += s.LastWeek
		}
	}
	return out
}

// allPoints is every member's all-time points, guess the anime included.
func allPoints(d *storeData) map[string]int {
	out := map[string]int{}
	for u, s := range d.Games {
		out[u] += s.Total
	}
	for u, s := range d.Guess {
		out[u] += s.Total
	}
	return out
}

// ranked sorts points, most first, and keeps the top n (0 = all).
func ranked(points map[string]int, n int) []guessRank {
	var list []guessRank
	for u, p := range points {
		if p > 0 {
			list = append(list, guessRank{u, p})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Points != list[j].Points {
			return list[i].Points > list[j].Points
		}
		return list[i].User < list[j].User
	})
	if n > 0 && len(list) > n {
		list = list[:n]
	}
	return list
}

// gameCommands are the games' slash commands. The games themselves are
// played in the Activity; these open it, add riddles and show the board.
func gameCommands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{Name: "leaderboard", Description: "Game points leaderboard this week and all time, and a button to play"},
		{Name: "otakase", Description: "Open Otakase Activity: watch anime together or play games with friends"},
		{Name: "games", Description: "Open Otakase Activity (legacy shortcut for games and watch rooms)"},
		{Name: "game-ban", Description: "Give a member a break from games: no playing, no points (0 hours lifts it)", DefaultMemberPermissions: &permMod,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionUser, Name: "member", Description: "Who", Required: true},
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "hours", Description: "How long (0 lifts it, up to 720)", Required: true, MinValue: &zero, MaxValue: 720},
				{Type: discordgo.ApplicationCommandOptionString, Name: "reason", Description: "Why (they see it)", MaxLength: 200},
			}},
		{Name: "game-points", Description: "Give or take a member's game points", DefaultMemberPermissions: &permMod,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionUser, Name: "member", Description: "Who", Required: true},
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "points", Description: "How many, minus to take (-100 to 100)", Required: true, MinValue: &minusHundred, MaxValue: 100},
			}},
		{Name: "riddle-add", Description: "Add an emoji riddle for others to guess", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "anime", Description: "The answer (pick from the suggestions)", Required: true, Autocomplete: true},
			{Type: discordgo.ApplicationCommandOptionString, Name: "emojis", Description: "The riddle, emojis only", Required: true, MaxLength: 60},
		}},
	}
}

// gameCommand runs the game commands; false when name isn't one.
func (b *bot) gameCommand(i *discordgo.InteractionCreate, name string) bool {
	switch name {
	case "leaderboard":
		b.gamesBoard(i)
	case "games", "otakase":
		b.dailyCommand(i)
	case "riddle-add":
		b.addRiddle(i)
	case "game-ban":
		b.gameBanCommand(i)
	case "game-points":
		b.gamePointsCommand(i)
	default:
		return false
	}
	return true
}

var minusHundred = -100.0

// gameBanCommand benches a member from games, or lets them back.
func (b *bot) gameBanCommand(i *discordgo.InteractionCreate) {
	if !b.staffOnly(i) {
		return
	}
	o := optionMap(i)
	user := optUser(i, o["member"])
	hours := int(o["hours"].IntValue())
	reason := ""
	if r, ok := o["reason"]; ok {
		reason = r.StringValue()
	}
	if user == nil || user.Bot {
		b.respond(i, true, errorEmbed("Pick a member."))
		return
	}
	b.setGameBan(user.ID, time.Duration(hours)*time.Hour, reason, i.Member.User.ID)
	b.adminNote(i.Member.User.ID, user.ID, benchNote(hours, reason))
	text := fmt.Sprintf("<@%s> can play games again.", user.ID)
	if hours > 0 {
		text = fmt.Sprintf("<@%s> is on a break from games for %s: no playing, no points.", user.ID, dashPlural(hours, "hour"))
	}
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: text})
}

// gamePointsCommand gives or takes game points.
func (b *bot) gamePointsCommand(i *discordgo.InteractionCreate) {
	if !b.staffOnly(i) {
		return
	}
	o := optionMap(i)
	user := optUser(i, o["member"])
	n := int(o["points"].IntValue())
	if user == nil || user.Bot || n == 0 {
		b.respond(i, true, errorEmbed("Pick a member and a number other than 0."))
		return
	}
	total := b.adjustPoints(user.ID, n)
	b.adminNote(i.Member.User.ID, user.ID, fmt.Sprintf("changed the game points of %%s by %+d", n))
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("Changed <@%s>'s game points by %+d. They have %d now.", user.ID, n, total)})
}

// gameComponent handles the games' buttons; false when id isn't one.
func (b *bot) gameComponent(i *discordgo.InteractionCreate, id string) bool {
	switch {
	case id == "daily-launch":
		b.launchActivity(i)
	case id == "daily-board":
		b.dailyBoard(i, false)
	default:
		return false
	}
	return true
}

// gameAutocomplete answers the games' suggestions; false when not one.
func (b *bot) gameAutocomplete(i *discordgo.InteractionCreate, name string) bool {
	switch name {
	case "riddle-add":
		b.suggestTitles(i)
	default:
		return false
	}
	return true
}

func (b *bot) gamesBoard(i *discordgo.InteractionCreate) {
	week := weekKey(time.Now().UTC())
	var weekly, all []guessRank
	var champs []string
	b.store.view(func(d *storeData) {
		weekly, all = ranked(weekPoints(d, week), 10), ranked(allPoints(d), 10)
		champs = append(champs, d.Champions...)
	})
	lines := func(list []guessRank) string {
		if len(list) == 0 {
			return "Nobody yet."
		}
		var out []string
		for k, r := range list {
			out = append(out, fmt.Sprintf("**%d.** <@%s> · %d", k+1, r.User, r.Points))
		}
		return strings.Join(out, "\n")
	}
	desc := "Every game is in the **Otakase Activity**: the daily puzzle, guess the anime, higher or lower, " +
		"emoji riddles, title scramble, zoomed cover, timeline, connections, redacted, anime grid, hangman, wordle, quiz, " +
		"two truths and a lie, and watch bingo; with the call: quiz show, imposter, draw it, hot potato and guess who. " +
		"Play alone or together with everyone in a call. Open it with the button below or `/otakase` (or `/games`); add your own riddle with `/riddle-add`."
	if len(champs) > 0 {
		var who []string
		for _, c := range champs {
			who = append(who, "<@"+c+">")
		}
		desc += "\n\nLast week's " + championRole + ": " + strings.Join(who, ", ")
	}
	var comps []discordgo.MessageComponent
	if b.activityReady() {
		comps = append(comps, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "Play in the Activity", Style: discordgo.PrimaryButton, CustomID: "daily-launch", Emoji: &discordgo.ComponentEmoji{Name: "🎮"}},
		}})
	}
	fields := []*discordgo.MessageEmbedField{
		{Name: "This week", Value: lines(weekly), Inline: true},
		{Name: "All time", Value: lines(all), Inline: true},
	}
	if f := b.featuredField(); f != nil {
		fields = append(fields, f)
	}
	b.respond(i, false, &discordgo.MessageEmbed{Title: "Leaderboard", Color: shu, Description: desc, Fields: fields,
		Footer: &discordgo.MessageEmbedFooter{Text: "Points from every game · the week's winner gets " + championRole}}, comps...)
}

// playPlace refuses games in support and staff channels.
func (b *bot) playPlace(i *discordgo.InteractionCreate) bool {
	if p := b.place(i.ChannelID); p.Support || p.Staff {
		b.respond(i, true, errorEmbed("Play in a chat channel, not here."))
		return false
	}
	return true
}

// ── Game Champion ──

// championLoop gives last week's winner the Game Champion role once a new
// week starts, and says so in the games or general channel.
func (b *bot) championLoop() {
	go b.guess.load() // the games' shows, ready before anyone asks
	for {
		b.crownChampion(time.Now().UTC())
		time.Sleep(time.Hour)
	}
}

// weekWinners are the members with the most points in a week; ties share it.
func weekWinners(points map[string]int) []string {
	list := ranked(points, 0)
	var out []string
	for _, r := range list {
		if r.Points == list[0].Points {
			out = append(out, r.User)
		}
	}
	return out
}

func (b *bot) crownChampion(now time.Time) {
	last := weekKey(now.AddDate(0, 0, -7))
	var done bool
	var winners, old []string
	var points int
	b.store.view(func(d *storeData) {
		done = d.ChampionWeek == last
		if !done {
			p := weekPoints(d, last)
			winners = weekWinners(p)
			if len(winners) > 0 {
				points = p[winners[0]]
			}
			old = append(old, d.Champions...)
		}
	})
	if done || b.s == nil || b.s.State == nil || b.s.State.User == nil {
		return
	}
	role := b.championRoleID()
	if role != "" {
		for _, u := range old {
			if !contains(winners, u) {
				if err := b.s.GuildMemberRoleRemove(b.cfg.GuildID, u, role); err != nil {
					log.Printf("champion role remove: %v", err)
				}
			}
		}
		for _, u := range winners {
			if err := b.s.GuildMemberRoleAdd(b.cfg.GuildID, u, role); err != nil {
				log.Printf("champion role add: %v", err)
			}
		}
	}
	b.store.update(func(d *storeData) { d.ChampionWeek, d.Champions = last, winners })
	if len(winners) == 0 {
		return
	}
	var who []string
	for _, u := range winners {
		who = append(who, "<@"+u+">")
	}
	if ch := b.gamesChannel(); ch != "" {
		e := &discordgo.MessageEmbed{Title: "🏆 " + championRole, Color: shu,
			Description: fmt.Sprintf("%s won last week's games with **%d points**. The role is theirs until next Monday.\n\nA new week starts now: `/leaderboard` for the points and everything to play.", joinAnd(who), points)}
		if _, err := b.s.ChannelMessageSendComplex(ch, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{e},
			AllowedMentions: &discordgo.MessageAllowedMentions{Users: winners}}); err != nil {
			log.Printf("champion post: %v", err)
		}
	}
}

// championRoleID finds the Game Champion role, making it when it's missing.
func (b *bot) championRoleID() string {
	g, err := b.s.State.Guild(b.cfg.GuildID)
	if err != nil {
		return ""
	}
	for _, r := range g.Roles {
		if strings.EqualFold(strings.TrimSpace(r.Name), championRole) {
			return r.ID
		}
	}
	color, hoist := shu, true
	name := championRole
	r, err := b.s.GuildRoleCreate(b.cfg.GuildID, &discordgo.RoleParams{Name: name, Color: &color, Hoist: &hoist})
	if err != nil {
		log.Printf("champion role create: %v", err)
		return ""
	}
	return r.ID
}

// gamesChannel is a text channel named games, else general or chat.
func (b *bot) gamesChannel() string {
	byName := map[string]string{}
	for _, c := range b.chatChannels() {
		n := strings.ToLower(channelBase(c.Name))
		if _, ok := byName[n]; !ok {
			byName[n] = c.ID
		}
	}
	for _, n := range []string{"games", "game", "general", "chat", "lounge"} {
		if id := byName[n]; id != "" {
			return id
		}
	}
	return ""
}
