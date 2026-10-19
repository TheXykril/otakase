package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// /wrap: a member's year in Otakase, kept by the badge counting
// (watchbadges.go): watch time, episodes, days watched, the longest streak
// and the shows they spent the most time with. Shows and episodes come
// only from heartbeats they shared; adult titles are left out.

// yearWrap is one member's year.
type yearWrap struct {
	Seconds  int64            `json:"seconds,omitempty"`
	Episodes int              `json:"episodes,omitempty"`
	Days     int              `json:"days,omitempty"`
	Streak   int              `json:"streak,omitempty"` // longest streak this year
	Shows    map[int]wrapShow `json:"shows,omitempty"`  // by AniList id, the top keepWrapShows
}

type wrapShow struct {
	Title   string `json:"title,omitempty"`
	Seconds int64  `json:"seconds"`
}

func init() {
	registerCommand(&discordgo.ApplicationCommand{Name: "wrap", Description: "A year in Otakase: watch time, episodes, streaks and top shows",
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionInteger, Name: "year", Description: "Which year (default this one)", MinValue: &wrapFirstYear, MaxValue: 2100},
			{Type: discordgo.ApplicationCommandOptionUser, Name: "member", Description: "Whose (default you)"},
		}}, (*bot).wrapCommand)
	registerHelp("`/wrap` your year in Otakase: watch time, episodes, longest streak and top shows")
}

var wrapFirstYear = 2024.0

// addWrapShow adds time to a show of the year, keeping only the top shows:
// when there are too many the smallest goes, never the one just watched.
func addWrapShow(w *yearWrap, id int, title string, secs int64) {
	if w.Shows == nil {
		w.Shows = map[int]wrapShow{}
	}
	s := w.Shows[id]
	s.Seconds += secs
	if title != "" {
		s.Title = clip(title, 100)
	}
	w.Shows[id] = s
	for len(w.Shows) > keepWrapShows {
		least, leastSecs := 0, int64(-1)
		for k, v := range w.Shows {
			if k != id && (leastSecs < 0 || v.Seconds < leastSecs || (v.Seconds == leastSecs && k < least)) {
				least, leastSecs = k, v.Seconds
			}
		}
		delete(w.Shows, least)
	}
}

// topWrapShows is the year's shows, most watched first.
func topWrapShows(w yearWrap) []int {
	ids := make([]int, 0, len(w.Shows))
	for id := range w.Shows {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, c := w.Shows[ids[i]], w.Shows[ids[j]]
		if a.Seconds != c.Seconds {
			return a.Seconds > c.Seconds
		}
		return ids[i] < ids[j]
	})
	return ids
}

func (b *bot) wrapCommand(i *discordgo.InteractionCreate) {
	who := i.Member.User
	year := appNow().UTC().Year()
	o := optionMap(i)
	if v, ok := o["member"]; ok {
		who = optUser(i, v)
	}
	if v, ok := o["year"]; ok {
		year = int(v.IntValue())
	}
	self := who.ID == i.Member.User.ID
	b.deferThen(i, false, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		st, _ := b.watchStatsOf(who.ID)
		return wrapView(who.ID, self, year, st.Years[year], appNow(), appShowInfo), nil
	})
}

// wrapView is the year as an embed. lookup finds a show's English title
// and whether it is adult; shows it can't check are left out.
func wrapView(user string, self bool, year int, w yearWrap, now time.Time, lookup func(int) (airingShowInfo, bool)) *discordgo.MessageEmbed {
	if w.Seconds < 60 {
		if self {
			return errorEmbed("Nothing in %d yet. Link Otakase with `otakase -discord-link` and what you play counts here.", year)
		}
		return errorEmbed("<@%s> has nothing in Otakase for %d.", user, year)
	}
	e := &discordgo.MessageEmbed{Title: fmt.Sprintf("%d in Otakase", year), Color: shu,
		Description: fmt.Sprintf("<@%s>'s year, from their linked Otakase app.", user),
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Watch time", Value: watchDuration(w.Seconds), Inline: true},
			{Name: "Episodes", Value: strconv.Itoa(w.Episodes), Inline: true},
			{Name: "Days watched", Value: strconv.Itoa(w.Days), Inline: true},
			{Name: "Longest streak", Value: dashPlural(w.Streak, "day"), Inline: true},
		},
		Footer: &discordgo.MessageEmbedFooter{Text: "Episodes and shows count with Share what I watch on"}}
	var lines []string
	for _, id := range topWrapShows(w) {
		if len(lines) == 5 {
			break
		}
		info, ok := lookup(id)
		if !ok || info.IsAdult {
			continue
		}
		title := info.Title.English
		if title == "" {
			title = info.Title.Romaji
		}
		if title == "" {
			title = w.Shows[id].Title
		}
		line := fmt.Sprintf("**%d.** %s · %s", len(lines)+1, showText(clip(title, 60)), watchDuration(w.Shows[id].Seconds))
		if info.SiteURL != "" {
			line = fmt.Sprintf("**%d.** [%s](%s) · %s", len(lines)+1, strings.ReplaceAll(showText(clip(title, 60)), "]", ")"), info.SiteURL, watchDuration(w.Shows[id].Seconds))
		}
		if len(lines) == 0 && info.CoverImage.ExtraLarge != "" {
			e.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: info.CoverImage.ExtraLarge}
		}
		lines = append(lines, line)
	}
	if len(lines) > 0 {
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: "Top shows", Value: strings.Join(lines, "\n")})
	}
	if year == now.UTC().Year() {
		e.Footer.Text = "So far this year · " + e.Footer.Text
	}
	return e
}
