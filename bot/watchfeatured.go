package main

import (
	"fmt"
	"hash/fnv"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// The featured anime: one show a day, picked the same way for everyone
// from AniList's most popular airing shows (never adult ones). Watching 10
// minutes of it in Otakase, sharing what you watch, gives a games bonus
// once a day. /featured shows it; so does /leaderboard.

const (
	featuredSecs  = 10 * 60
	featuredBonus = 5
	featuredGame  = "featured"
)

// featuredPick is the day's show.
type featuredPick struct {
	Day       string `json:"day"` // UTC, "2026-10-14"
	AniListID int    `json:"anilist_id"`
	Title     string `json:"title"`
	URL       string `json:"url,omitempty"`
	Cover     string `json:"cover,omitempty"`
}

// featuredWatch is a member's time with the day's show.
type featuredWatch struct {
	Day     string `json:"day"`
	Seconds int    `json:"seconds,omitempty"`
	Paid    bool   `json:"paid,omitempty"`
}

const featuredQuery = `query {
  Page(perPage: 30) {
    media(type: ANIME, status: RELEASING, isAdult: false, sort: POPULARITY_DESC) {
      id siteUrl isAdult coverImage { extraLarge } title { romaji english }
    }
  }
}`

// fetchFeaturedShows is variable so tests skip AniList.
var fetchFeaturedShows = func() ([]featuredPick, error) {
	var r struct {
		Data struct {
			Page struct {
				Media []struct {
					ID         int    `json:"id"`
					SiteURL    string `json:"siteUrl"`
					IsAdult    bool   `json:"isAdult"`
					CoverImage struct {
						ExtraLarge string `json:"extraLarge"`
					} `json:"coverImage"`
					Title struct{ Romaji, English string }
				} `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	if err := anilistPost("", featuredQuery, map[string]any{}, &r); err != nil {
		return nil, err
	}
	var out []featuredPick
	for _, m := range r.Data.Page.Media {
		if m.IsAdult {
			continue
		}
		t := m.Title.English
		if t == "" {
			t = m.Title.Romaji
		}
		out = append(out, featuredPick{AniListID: m.ID, Title: clip(t, 100), URL: m.SiteURL, Cover: m.CoverImage.ExtraLarge})
	}
	return out, nil
}

func init() {
	gameNames[featuredGame] = "Featured anime"
	onHeartbeat((*bot).featuredHeartbeat)
	registerJob("Featured anime", (*bot).featuredLoop)
	registerCommand(&discordgo.ApplicationCommand{Name: "featured", Description: "Today's featured anime: watch 10 minutes in Otakase for bonus game points"}, (*bot).featuredCommand)
	registerHelp(fmt.Sprintf("`/featured` today's featured anime: 10 minutes of it in Otakase gives %d game points", featuredBonus))
}

// pickFeatured picks the day's show from the candidates, the same for the
// same day and list whatever order AniList gave it in.
func pickFeatured(day string, shows []featuredPick) (featuredPick, bool) {
	if len(shows) == 0 {
		return featuredPick{}, false
	}
	list := append([]featuredPick(nil), shows...)
	sort.Slice(list, func(i, j int) bool { return list[i].AniListID < list[j].AniListID })
	h := fnv.New32a()
	h.Write([]byte(day))
	p := list[int(h.Sum32()%uint32(len(list)))]
	p.Day = day
	return p, true
}

// featuredToday is today's pick, fetched when the day has none yet.
func (b *bot) featuredToday(now time.Time) (featuredPick, bool) {
	day := now.UTC().Format(dayLayout)
	var p featuredPick
	var have bool
	b.store.view(func(d *storeData) {
		if d.Featured != nil && d.Featured.Day == day {
			p, have = *d.Featured, true
		}
	})
	if have {
		return p, true
	}
	shows, err := fetchFeaturedShows()
	if err != nil {
		log.Printf("featured anime: %v", err)
		return featuredPick{}, false
	}
	p, ok := pickFeatured(day, shows)
	if !ok {
		return featuredPick{}, false
	}
	b.store.update(func(d *storeData) {
		d.Featured = &p
		for u, w := range d.FeaturedWatch {
			if w.Day != day {
				delete(d.FeaturedWatch, u)
			}
		}
	})
	return p, true
}

// featuredLoop picks each day's show soon after midnight UTC, so heartbeats
// can count it without asking AniList.
func (b *bot) featuredLoop() {
	for {
		b.featuredToday(appNow())
		time.Sleep(15 * time.Minute)
	}
}

// addFeaturedWatch counts time with the day's show and reports whether it
// just reached the bonus.
func addFeaturedWatch(w *featuredWatch, day string, secs int) bool {
	if w.Day != day {
		*w = featuredWatch{Day: day}
	}
	if w.Paid {
		return false
	}
	w.Seconds += secs
	if w.Seconds >= featuredSecs {
		w.Paid = true
		return true
	}
	return false
}

func (b *bot) featuredHeartbeat(ev heartbeatEvent) {
	if ev.Show == nil || ev.Show.AniListID <= 0 || ev.Seconds <= 0 {
		return
	}
	day := ev.At.UTC().Format(dayLayout)
	b.store.touch(func(d *storeData) {
		f := d.Featured
		if f == nil || f.Day != day || f.AniListID != ev.Show.AniListID {
			return
		}
		if d.FeaturedWatch == nil {
			d.FeaturedWatch = map[string]featuredWatch{}
		}
		w := d.FeaturedWatch[ev.User]
		if addFeaturedWatch(&w, day, ev.Seconds) {
			addGamePoints(d, ev.User, featuredGame, featuredBonus, weekKey(ev.At.UTC()))
		}
		d.FeaturedWatch[ev.User] = w
	})
}

func (b *bot) featuredCommand(i *discordgo.InteractionCreate) {
	user := i.Member.User.ID
	b.deferThen(i, false, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		now := appNow()
		p, ok := b.featuredToday(now)
		if !ok {
			return errorEmbed("Couldn't pick today's featured anime from AniList right now. Try again in a bit."), nil
		}
		var w featuredWatch
		b.store.view(func(d *storeData) { w = d.FeaturedWatch[user] })
		e := featuredView(p, w, now)
		buttons := withPlayButton(nil, p.AniListID, 0)
		return e, []discordgo.MessageComponent{discordgo.ActionsRow{Components: buttons}}
	})
}

// featuredView is /featured's answer, with how far the asker got today.
func featuredView(p featuredPick, w featuredWatch, now time.Time) *discordgo.MessageEmbed {
	title := "**" + showText(p.Title) + "**"
	if p.URL != "" {
		title = fmt.Sprintf("[%s](%s)", strings.ReplaceAll(title, "]", ")"), p.URL)
	}
	status := fmt.Sprintf("You watched %s of the %d minutes today.", watchDuration(int64(w.Seconds)), featuredSecs/60)
	if w.Day != p.Day {
		status = "You haven't watched it today yet."
	}
	if w.Day == p.Day && w.Paid {
		status = fmt.Sprintf("You got today's %d points. ✓", featuredBonus)
	}
	next := now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	e := &discordgo.MessageEmbed{Title: "Featured anime today", Color: shu,
		Description: fmt.Sprintf("%s\n\nWatch %d minutes of it in Otakase today, with **Share what I watch** on, for **%d game points**.\n%s\n\nA new one <t:%d:R>.",
			title, featuredSecs/60, featuredBonus, status, next.Unix())}
	if p.Cover != "" {
		e.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: p.Cover}
	}
	return e
}

// featuredField is the /leaderboard line about today's show, nil when
// none is picked yet (the board doesn't wait for AniList).
func (b *bot) featuredField() *discordgo.MessageEmbedField {
	var p *featuredPick
	day := appNow().UTC().Format(dayLayout)
	b.store.view(func(d *storeData) {
		if d.Featured != nil && d.Featured.Day == day {
			x := *d.Featured
			p = &x
		}
	})
	if p == nil {
		return nil
	}
	return &discordgo.MessageEmbedField{Name: "Featured anime today",
		Value: fmt.Sprintf("**%s**: watch 10 minutes in Otakase for %d points · `/featured`", showText(p.Title), featuredBonus)}
}
