package main

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// The server club: staff pick a show and an episode to reach by a deadline
// (/club set, Manage Server). /club shows it with who caught up: members
// whose linked app shares what they watch and played that episode (or a
// later one) of the show. A board in the club channel ("club", "anime-club"
// or "watch-club", else the anime channel) is edited as people catch up.
// The app shows the club in its Community menu from GET /v1/club:
//
//	{"club":{"anilist_id":154587,"title":"Frieren","episode":12,
//	         "deadline":"2026-10-21T18:00:00Z","caught_up":3,"joined":5,"url":"https://discord.com/channels/…"}}
//
// or {"club":null} when there is none running.

const (
	maxClubMembers = 500
	clubBoardDelay = 20 * time.Second // catch-ups within this go in one board edit
)

// watchClub is the club's current pick.
type watchClub struct {
	AniListID int                  `json:"anilist_id"`
	Title     string               `json:"title"`
	URL       string               `json:"url,omitempty"`   // AniList page
	Cover     string               `json:"cover,omitempty"` // cover image
	Episode   int                  `json:"episode"`
	Deadline  time.Time            `json:"deadline"`
	SetBy     string               `json:"set_by"`
	Joined    []string             `json:"joined,omitempty"`
	Caught    map[string]time.Time `json:"caught,omitempty"` // member → when they reached the episode
	Channel   string               `json:"channel,omitempty"`
	Message   string               `json:"message,omitempty"` // the board
	Closed    bool                 `json:"closed,omitempty"`  // the board says it ended
}

// running reports whether the club is on at now.
func (c *watchClub) running(now time.Time) bool {
	return c != nil && !c.Closed && now.Before(c.Deadline)
}

var clubLimit = newRateLimit(30, time.Minute)

func init() {
	registerCommand(&discordgo.ApplicationCommand{Name: "club", Description: "The server club: one show to watch together, up to an episode by a date",
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "show", Description: "This club's show and who caught up"},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "set", Description: "Pick the club's show (Manage Server)", Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "anime", Description: "The show (pick from the suggestions)", Required: true, Autocomplete: true},
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "episode", Description: "Episode to reach", Required: true, MinValue: &one, MaxValue: 2000},
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "deadline_days", Description: "Days to get there (default 7)", MinValue: &one, MaxValue: 60},
			}},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "end", Description: "End the club now (Manage Server)"},
		}}, (*bot).clubCommand)
	registerAutocomplete("club", (*bot).clubSuggest)
	registerComponent("club-join:", (*bot).clubJoin)
	registerJob("Server club", (*bot).clubLoop)
	onHeartbeat((*bot).clubHeartbeat)
	apiHandle("GET /v1/club", (*bot).apiClub)
	registerHelp("`/club` the server club's show of the week and who caught up")
}

func (b *bot) clubCommand(i *discordgo.InteractionCreate) {
	d := i.ApplicationCommandData()
	if len(d.Options) == 0 {
		return
	}
	sub := d.Options[0]
	opts := map[string]*discordgo.ApplicationCommandInteractionDataOption{}
	for _, o := range sub.Options {
		opts[o.Name] = o
	}
	switch sub.Name {
	case "show":
		e, comps := b.clubView(appNow())
		b.respond(i, false, e, comps...)
	case "set":
		if !clubStaff(i) {
			return
		}
		days := int64(7)
		if o, ok := opts["deadline_days"]; ok {
			days = o.IntValue()
		}
		b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
			return b.setClub(i.Member.User.ID, opts["anime"].StringValue(), int(opts["episode"].IntValue()), int(days)), nil
		})
	case "end":
		if !clubStaff(i) {
			return
		}
		var had bool
		b.store.update(func(d *storeData) {
			if had = d.Club.running(appNow()); had {
				d.Club.Deadline = appNow()
			}
		})
		if !had {
			b.respond(i, true, errorEmbed("There's no club running."))
			return
		}
		b.closeClub()
		b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: "Ended the club."})
	}
}

// clubStaff lets members with Manage Server change the club.
func clubStaff(i *discordgo.InteractionCreate) bool {
	return i.Member.Permissions&(discordgo.PermissionManageGuild|discordgo.PermissionAdministrator) != 0
}

// setClub looks the show up, starts the club and posts its board.
func (b *bot) setClub(by, anime string, episode, days int) *discordgo.MessageEmbed {
	show, err := findPartyShow(anime)
	if err != nil || show.AniListID == 0 {
		return errorEmbed("Couldn't find that show on AniList. Pick one from the suggestions.")
	}
	now := appNow()
	c := &watchClub{AniListID: show.AniListID, Title: clip(show.Title, 100), URL: show.URL, Episode: episode,
		Deadline: now.Add(time.Duration(days) * 24 * time.Hour), SetBy: by}
	if info, ok := appShowInfo(show.AniListID); ok {
		c.Cover = info.CoverImage.ExtraLarge
	}
	var old *watchClub
	b.store.update(func(d *storeData) {
		old, d.Club = d.Club, c
	})
	if old != nil && old.Message != "" && !old.Closed {
		old.Closed = true
		b.editClubBoardOf(old)
	}
	if b.s != nil {
		if ch := b.clubChannel(); ch != "" {
			e, comps := clubEmbed(c, now)
			m, err := b.s.ChannelMessageSendComplex(ch, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{e}, Components: comps,
				AllowedMentions: &discordgo.MessageAllowedMentions{}})
			if err != nil {
				log.Printf("club board: %v", err)
			} else {
				b.store.update(func(d *storeData) {
					if d.Club == c {
						d.Club.Channel, d.Club.Message = ch, m.ID
					}
				})
			}
		}
	}
	return &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("The club is on: **%s** up to episode %d by <t:%d:f>.",
		showText(c.Title), c.Episode, c.Deadline.Unix())}
}

// clubChannel is a text channel named club, anime-club or watch-club,
// else the anime channel.
func (b *bot) clubChannel() string {
	if id := b.textChannelNamed("club", "anime-club", "watch-club"); id != "" {
		return id
	}
	return b.cfg.AiringChannel
}

// textChannelNamed is the first of names the server has as a text
// channel the bot can post in, a "版・" style prefix allowed.
func (b *bot) textChannelNamed(names ...string) string {
	byName := map[string]string{}
	for _, c := range b.postChannels() {
		n := strings.ToLower(channelBase(c.Name))
		if _, ok := byName[n]; !ok {
			byName[n] = c.ID
		}
	}
	for _, n := range names {
		if id := byName[n]; id != "" {
			return id
		}
	}
	return ""
}

func (b *bot) clubView(now time.Time) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	var c *watchClub
	b.store.view(func(d *storeData) { c = d.Club.copy() })
	if c == nil {
		return errorEmbed("There's no club right now. Staff start one with `/club set`."), nil
	}
	return clubEmbed(c, now)
}

// copy is a copy safe to read outside the store's lock.
func (c *watchClub) copy() *watchClub {
	if c == nil {
		return nil
	}
	x := *c
	x.Joined = append([]string(nil), c.Joined...)
	x.Caught = make(map[string]time.Time, len(c.Caught))
	for k, v := range c.Caught {
		x.Caught[k] = v
	}
	return &x
}

// clubEmbed is the club's board, and /club's answer.
func clubEmbed(c *watchClub, now time.Time) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	caught := make([]string, 0, len(c.Caught))
	for u := range c.Caught {
		caught = append(caught, u)
	}
	// Who got there first, first.
	sortByTime(caught, c.Caught)
	var lines []string
	if c.running(now) {
		lines = append(lines, fmt.Sprintf("Watch up to **episode %d** by <t:%d:f> (<t:%d:R>).", c.Episode, c.Deadline.Unix(), c.Deadline.Unix()))
	} else {
		lines = append(lines, fmt.Sprintf("Ended <t:%d:R>: the goal was episode %d.", c.Deadline.Unix(), c.Episode))
	}
	lines = append(lines, "")
	switch {
	case len(caught) == 0:
		lines = append(lines, "Nobody caught up yet.")
	default:
		shown := caught
		if len(shown) > 40 {
			shown = shown[:40]
		}
		who := make([]string, len(shown))
		for k, u := range shown {
			who[k] = "<@" + u + ">"
		}
		more := ""
		if len(caught) > len(shown) {
			more = fmt.Sprintf(" and %d more", len(caught)-len(shown))
		}
		lines = append(lines, fmt.Sprintf("**Caught up (%d):** %s%s", len(caught), strings.Join(who, " "), more))
	}
	if len(c.Joined) > 0 {
		lines = append(lines, fmt.Sprintf("%s in the club.", dashPlural(len(c.Joined), "member")))
	}
	e := &discordgo.MessageEmbed{Title: "Server club: " + clip(c.Title, 200), URL: c.URL, Color: shu, Description: strings.Join(lines, "\n"),
		Footer: &discordgo.MessageEmbedFooter{Text: "Caught up counts members whose Otakase app shares what they watch"}}
	if c.Cover != "" {
		e.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: c.Cover}
	}
	if !c.running(now) {
		return e, nil
	}
	buttons := withPlayButton([]discordgo.MessageComponent{
		discordgo.Button{Label: "Join the club", Style: discordgo.PrimaryButton, CustomID: "club-join:" + fmt.Sprint(c.AniListID)},
	}, c.AniListID, 0)
	return e, []discordgo.MessageComponent{discordgo.ActionsRow{Components: buttons}}
}

func sortByTime(users []string, at map[string]time.Time) {
	sort.Slice(users, func(i, j int) bool {
		a, c := at[users[i]], at[users[j]]
		if !a.Equal(c) {
			return a.Before(c)
		}
		return users[i] < users[j]
	})
}

// clubJoin adds the presser to the club and queues the show in their
// linked apps.
func (b *bot) clubJoin(i *discordgo.InteractionCreate, arg string) {
	u := interactionUser(i)
	if u == nil {
		return
	}
	now := appNow()
	var c *watchClub
	joined := false
	b.store.update(func(d *storeData) {
		if !d.Club.running(now) || fmt.Sprint(d.Club.AniListID) != arg {
			return
		}
		if !contains(d.Club.Joined, u.ID) && len(d.Club.Joined) < maxClubMembers {
			d.Club.Joined = append(d.Club.Joined, u.ID)
			joined = true
		}
		c = d.Club.copy()
	})
	if c == nil {
		b.respond(i, true, errorEmbed("This club has ended."))
		return
	}
	text := fmt.Sprintf("You're in: **%s** up to episode %d by <t:%d:f>.", showText(c.Title), c.Episode, c.Deadline.Unix())
	if !joined {
		text = "You're already in the club."
	}
	if b.hasApp(u.ID) {
		b.sendToApps(u.ID, inboxItem{Kind: "queue", AniListID: c.AniListID, Title: c.Title, URL: playURL(c.AniListID, 0),
			Text: fmt.Sprintf("Server club: up to episode %d", c.Episode)})
		text += "\nIt's in your Otakase under Community → From Discord."
	} else {
		text += "\nLink Otakase (`/link-app`) and your watching counts as caught up on its own."
	}
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: text})
	if joined {
		b.queueClubBoard()
	}
}

// clubHeartbeat marks a member caught up once they play the goal episode
// (or later) while sharing what they watch.
func (b *bot) clubHeartbeat(ev heartbeatEvent) {
	if ev.Show == nil || ev.Seconds <= 0 {
		return
	}
	caught := false
	b.store.touch(func(d *storeData) {
		c := d.Club
		if !c.running(ev.At) || ev.Show.AniListID != c.AniListID || ev.Show.Episode < c.Episode {
			return
		}
		if _, ok := c.Caught[ev.User]; ok || len(c.Caught) >= maxClubMembers {
			return
		}
		if c.Caught == nil {
			c.Caught = map[string]time.Time{}
		}
		c.Caught[ev.User] = ev.At
		caught = true
	})
	if caught {
		b.queueClubBoard()
	}
}

var clubBoard struct {
	sync.Mutex
	pending bool
}

// queueClubBoard edits the board in a little while, once for every
// catch-up meanwhile.
func (b *bot) queueClubBoard() {
	if b.s == nil {
		return
	}
	clubBoard.Lock()
	defer clubBoard.Unlock()
	if clubBoard.pending {
		return
	}
	clubBoard.pending = true
	go func() {
		time.Sleep(clubBoardDelay)
		clubBoard.Lock()
		clubBoard.pending = false
		clubBoard.Unlock()
		var c *watchClub
		b.store.view(func(d *storeData) { c = d.Club.copy() })
		b.editClubBoardOf(c)
	}()
}

func (b *bot) editClubBoardOf(c *watchClub) {
	if b.s == nil || c == nil || c.Message == "" {
		return
	}
	e, comps := clubEmbed(c, appNow())
	if c.Closed {
		comps = nil
	}
	if _, err := b.s.ChannelMessageEditComplex(&discordgo.MessageEdit{Channel: c.Channel, ID: c.Message,
		Embeds: &[]*discordgo.MessageEmbed{e}, Components: &comps, AllowedMentions: &discordgo.MessageAllowedMentions{}}); err != nil {
		log.Printf("club board edit: %v", err)
	}
}

// closeClub marks a club past its deadline as ended and edits the board
// one last time.
func (b *bot) closeClub() {
	now := appNow()
	var c *watchClub
	b.store.update(func(d *storeData) {
		if d.Club != nil && !d.Club.Closed && !now.Before(d.Club.Deadline) {
			d.Club.Closed = true
			c = d.Club.copy()
		}
	})
	b.editClubBoardOf(c)
}

func (b *bot) clubLoop() {
	for {
		b.closeClub()
		time.Sleep(10 * time.Minute)
	}
}

// clubSuggest answers typing in /club set's anime option.
func (b *bot) clubSuggest(i *discordgo.InteractionCreate) {
	var q string
	for _, sub := range i.ApplicationCommandData().Options {
		for _, o := range sub.Options {
			if o.Name == "anime" && o.Focused {
				q = strings.TrimSpace(o.StringValue())
			}
		}
	}
	choices := []*discordgo.ApplicationCommandOptionChoice{}
	if len(q) >= 2 && !pickedShowRe.MatchString(q) {
		choices = titleChoices(q)
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{Choices: choices},
	}); err != nil {
		log.Printf("club suggestions: %v", err)
	}
}

// clubReply is GET /v1/club's answer.
type clubReply struct {
	Club *clubInfo `json:"club"`
}

type clubInfo struct {
	AniListID int       `json:"anilist_id"`
	Title     string    `json:"title"`
	Episode   int       `json:"episode"`
	Deadline  time.Time `json:"deadline"`
	CaughtUp  int       `json:"caught_up"`
	Joined    int       `json:"joined"`
	URL       string    `json:"url,omitempty"` // the board in Discord
}

func (b *bot) apiClub(w http.ResponseWriter, r *http.Request) {
	now := appNow()
	if !clubLimit.allow(clientIP(r), now) {
		apiError(w, http.StatusTooManyRequests, "too many requests")
		return
	}
	var reply clubReply
	b.store.view(func(d *storeData) {
		c := d.Club
		if !c.running(now) {
			return
		}
		reply.Club = &clubInfo{AniListID: c.AniListID, Title: c.Title, Episode: c.Episode, Deadline: c.Deadline.UTC(),
			CaughtUp: len(c.Caught), Joined: len(c.Joined)}
		if c.Message != "" {
			reply.Club.URL = fmt.Sprintf("https://discord.com/channels/%s/%s/%s", b.cfg.GuildID, c.Channel, c.Message)
		}
	})
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, reply)
}
