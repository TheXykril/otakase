package main

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Top on the server: shared watch time per show and ISO week, from the
// heartbeats of members who share what they watch. When a new week starts
// (Monday, UTC) the bot posts last week's most watched shows in a channel
// named "top" or "watch-top", else the anime channel. /server-watch-top
// shows this week so far or last week. (/server-top is the AniList one.)
// Adult titles never show, nor shows AniList couldn't confirm.

const (
	keepServerWeeks  = 5
	maxServerShows   = 300 // shows kept per week
	maxServerViewers = 50  // members kept per show and week
	serverTopShown   = 10
)

// serverShow is a show's shared watch time in one week.
type serverShow struct {
	Title   string   `json:"title,omitempty"`
	Seconds int64    `json:"seconds"`
	Viewers []string `json:"viewers,omitempty"`
}

func init() {
	onHeartbeat((*bot).serverWatchHeartbeat)
	registerJob("Server watch top", (*bot).serverWatchLoop)
	registerCommand(&discordgo.ApplicationCommand{Name: "server-watch-top", Description: "The shows members watched most in Otakase this week or last",
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "week", Description: "Which week (default this one)", Choices: []*discordgo.ApplicationCommandOptionChoice{
				{Name: "This week so far", Value: "this"},
				{Name: "Last week", Value: "last"},
			}},
		}}, (*bot).serverWatchTopCommand)
	registerHelp("`/server-watch-top` the shows members watched most in Otakase this week")
}

// addServerWatch adds a shared heartbeat to its week.
func addServerWatch(weeks map[string]map[int]serverShow, user string, show appShow, secs int, now time.Time) {
	week := isoWeekKey(now)
	m := weeks[week]
	if m == nil {
		m = map[int]serverShow{}
		weeks[week] = m
	}
	s, had := m[show.AniListID]
	if !had && len(m) >= maxServerShows {
		return
	}
	s.Seconds += int64(secs)
	if show.Title != "" {
		s.Title = show.Title
	}
	if !contains(s.Viewers, user) && len(s.Viewers) < maxServerViewers {
		s.Viewers = append(s.Viewers, user)
	}
	m[show.AniListID] = s
	if len(weeks) > keepServerWeeks {
		keys := make([]string, 0, len(weeks))
		for k := range weeks {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys[:len(keys)-keepServerWeeks] {
			delete(weeks, k)
		}
	}
}

func (b *bot) serverWatchHeartbeat(ev heartbeatEvent) {
	if ev.Show == nil || ev.Show.AniListID <= 0 || ev.Seconds <= 0 {
		return
	}
	b.store.touch(func(d *storeData) {
		if d.ServerWatch == nil {
			d.ServerWatch = map[string]map[int]serverShow{}
		}
		addServerWatch(d.ServerWatch, ev.User, *ev.Show, ev.Seconds, ev.At)
	})
}

// serverWeek is a copy of one week's shows.
func (b *bot) serverWeek(week string) map[int]serverShow {
	out := map[int]serverShow{}
	b.store.view(func(d *storeData) {
		for id, s := range d.ServerWatch[week] {
			s.Viewers = append([]string(nil), s.Viewers...)
			out[id] = s
		}
	})
	return out
}

// serverTopView lists a week's most watched shows. lookup checks each on
// AniList; adult shows and ones it can't check are skipped. n is how many
// it lists.
func serverTopView(title string, shows map[int]serverShow, lookup func(int) (airingShowInfo, bool)) (e *discordgo.MessageEmbed, n int) {
	ids := make([]int, 0, len(shows))
	for id := range shows {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, c := shows[ids[i]], shows[ids[j]]
		if a.Seconds != c.Seconds {
			return a.Seconds > c.Seconds
		}
		return ids[i] < ids[j]
	})
	e = &discordgo.MessageEmbed{Title: title, Color: shu,
		Footer: &discordgo.MessageEmbedFooter{Text: "Watch time in Otakase of members who share what they watch"}}
	var lines []string
	for _, id := range ids {
		if len(lines) == serverTopShown {
			break
		}
		s := shows[id]
		if s.Seconds < 60 {
			break
		}
		info, ok := lookup(id)
		if !ok || info.IsAdult {
			continue
		}
		name := info.Title.English
		if name == "" {
			name = info.Title.Romaji
		}
		if name == "" {
			name = s.Title
		}
		name = "**" + showText(clip(name, 60)) + "**"
		if info.SiteURL != "" {
			name = fmt.Sprintf("[%s](%s)", strings.ReplaceAll(name, "]", ")"), info.SiteURL)
		}
		if len(lines) == 0 && info.CoverImage.ExtraLarge != "" {
			e.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: info.CoverImage.ExtraLarge}
		}
		lines = append(lines, fmt.Sprintf("**%d.** %s · %s · %s", len(lines)+1, name, watchDuration(s.Seconds), dashPlural(len(s.Viewers), "member")))
	}
	if len(lines) == 0 {
		e.Description = "Nothing shared yet. Turn on **Share what I watch** in Otakase's Community menu to count here."
		return e, 0
	}
	e.Description = strings.Join(lines, "\n")
	return e, len(lines)
}

func (b *bot) serverWatchTopCommand(i *discordgo.InteractionCreate) {
	now := appNow()
	week, title := isoWeekKey(now), "Most watched this week"
	if o, ok := optionMap(i)["week"]; ok && o.StringValue() == "last" {
		week, title = isoWeekKey(now.AddDate(0, 0, -7)), "Most watched last week"
	}
	b.deferThen(i, onlyAsker(i), func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		e, _ := serverTopView(title, b.serverWeek(week), appShowInfo)
		return e, nil
	})
}

// serverWatchLoop posts last week's top once a new week starts.
func (b *bot) serverWatchLoop() {
	for {
		b.postServerWatchTop(appNow())
		time.Sleep(time.Hour)
	}
}

func (b *bot) postServerWatchTop(now time.Time) {
	last := isoWeekKey(now.AddDate(0, 0, -7))
	var due bool
	b.store.view(func(d *storeData) { due = d.ServerWatchPosted != last && len(d.ServerWatch[last]) > 0 })
	if !due || b.s == nil {
		return
	}
	ch := b.textChannelNamed("top", "watch-top")
	if ch == "" {
		ch = b.cfg.AiringChannel
	}
	if ch == "" {
		return
	}
	e, n := serverTopView("Most watched last week", b.serverWeek(last), appShowInfo)
	if n == 0 {
		b.store.update(func(d *storeData) { d.ServerWatchPosted = last })
		return // nothing that can be shown
	}
	if _, err := b.s.ChannelMessageSendComplex(ch, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{e},
		AllowedMentions: &discordgo.MessageAllowedMentions{}}); err != nil {
		log.Printf("server watch top: %v", err)
		return
	}
	b.store.update(func(d *storeData) { d.ServerWatchPosted = last })
}
