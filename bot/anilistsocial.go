package main

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Shared parts of the AniList social commands (/season-preview,
// /server-top, /compare, /recommend): reading members' lists, the show card
// with Plan to watch, and adding a show to a member's AniList.
//
// AniList allows 90 requests a minute. These commands keep to 60 between
// them and cache each member's list for an hour, so a busy server doesn't
// starve /profile, the airing board or follow DMs.

func socialCommands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{Name: "season-preview", Description: "Post the season's most anticipated shows in the anime channel", DefaultMemberPermissions: &permPurge,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "season", Description: "Which season (default: the current one)", Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "This season", Value: "current"},
					{Name: "Next season", Value: "next"},
				}},
			}},
		{Name: "server-top", Description: "What members here are watching now and finished this week"},
		{Name: "compare", Description: "Compare your AniList with another member's", Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionUser, Name: "member", Description: "Who to compare with", Required: true},
		}},
		{Name: "recommend", Description: "Five shows for you, from your AniList and what members here loved"},
	}
}

// socialCommand runs one of socialCommands.
func (b *bot) socialCommand(i *discordgo.InteractionCreate, name string) {
	switch name {
	case "season-preview":
		b.seasonPreviewCommand(i)
	case "server-top":
		b.serverTop(i)
	case "compare":
		b.compare(i)
	case "recommend":
		b.recommend(i)
	}
}

// rateLimiter lets at most per calls through in any window, making the
// rest wait.
type rateLimiter struct {
	mu     sync.Mutex
	per    int
	window time.Duration
	sent   []time.Time
	now    func() time.Time
	sleep  func(time.Duration)
}

func (l *rateLimiter) wait() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for {
		now := l.now()
		cut := now.Add(-l.window)
		for len(l.sent) > 0 && !l.sent[0].After(cut) {
			l.sent = l.sent[1:]
		}
		if len(l.sent) < l.per {
			l.sent = append(l.sent, now)
			return
		}
		l.sleep(l.sent[0].Sub(cut))
	}
}

var socialLimit = &rateLimiter{per: 60, window: time.Minute, now: time.Now, sleep: time.Sleep}

// anilistSocialPost is anilistPost under the social commands' rate limit.
// A 429 waits a minute and tries once more.
func anilistSocialPost(token, query string, vars map[string]any, out any) error {
	socialLimit.wait()
	err := anilistPost(token, query, vars, out)
	if err != nil && strings.Contains(err.Error(), "429") {
		time.Sleep(time.Minute)
		socialLimit.wait()
		err = anilistPost(token, query, vars, out)
	}
	return err
}

type fuzzyDate struct{ Year, Month, Day int }

// time is the date at midnight UTC, zero when the year is unknown.
func (f fuzzyDate) time() time.Time {
	if f.Year == 0 {
		return time.Time{}
	}
	return time.Date(f.Year, time.Month(max(f.Month, 1)), max(f.Day, 1), 0, 0, 0, 0, time.UTC)
}

// mediaBrief is what the social commands need to show a show.
type mediaBrief struct {
	ID          int      `json:"id"`
	IDMal       int      `json:"idMal"`
	SiteURL     string   `json:"siteUrl"`
	IsAdult     bool     `json:"isAdult"`
	Popularity  int      `json:"popularity"`
	Format      string   `json:"format"`
	Genres      []string `json:"genres"`
	BannerImage string   `json:"bannerImage"`
	Episodes    int      `json:"episodes"`
	Score       int      `json:"averageScore"`
	Source      string   `json:"source"`
	StartDate   struct {
		Year int `json:"year"`
	} `json:"startDate"`
	Title      struct{ Romaji, English string }
	CoverImage struct {
		Large      string `json:"large"`
		ExtraLarge string `json:"extraLarge"`
	} `json:"coverImage"`
}

const mediaBriefFields = `id idMal siteUrl isAdult popularity format genres bannerImage episodes averageScore source startDate { year } title { romaji english } coverImage { large extraLarge }`

func (m mediaBrief) name() string {
	if m.Title.English != "" {
		return m.Title.English
	}
	return m.Title.Romaji
}

func (m mediaBrief) url() string {
	if m.SiteURL != "" {
		return m.SiteURL
	}
	return fmt.Sprintf("https://anilist.co/anime/%d", m.ID)
}

func (m mediaBrief) cover() string {
	if m.CoverImage.ExtraLarge != "" {
		return m.CoverImage.ExtraLarge
	}
	return m.CoverImage.Large
}

// listEntry is one show on a member's AniList. Score is out of 100, 0
// when they didn't score it.
type listEntry struct {
	MediaID     int        `json:"mediaId"`
	Status      string     `json:"status"`
	Score       float64    `json:"score"`
	UpdatedAt   int64      `json:"updatedAt"`
	CompletedAt fuzzyDate  `json:"completedAt"`
	Media       mediaBrief `json:"media"`
}

// seen reports whether they watched any of it (anything but planning).
func (e listEntry) seen() bool { return e.Status != "PLANNING" }

const listQuery = `query ($id: Int, $chunk: Int) {
  MediaListCollection(userId: $id, type: ANIME, chunk: $chunk, perChunk: 500) {
    hasNextChunk
    lists {
      isCustomList
      entries { mediaId status score(format: POINT_100) updatedAt completedAt { year month day } media { ` + mediaBriefFields + ` } }
    }
  }
}`

// errPrivateList is a list AniList won't show (private, or the user is gone).
var errPrivateList = errors.New("list is private")

// fetchAniListList reads a member's whole anime list, each show once.
func fetchAniListList(id int) ([]listEntry, error) {
	var out []listEntry
	seen := map[int]bool{}
	for chunk := 1; chunk <= 8; chunk++ {
		var r struct {
			Data struct {
				MediaListCollection *struct {
					HasNextChunk bool `json:"hasNextChunk"`
					Lists        []struct {
						IsCustomList bool        `json:"isCustomList"`
						Entries      []listEntry `json:"entries"`
					} `json:"lists"`
				} `json:"MediaListCollection"`
			} `json:"data"`
		}
		if err := anilistSocialPost("", listQuery, map[string]any{"id": id, "chunk": chunk}, &r); err != nil {
			if strings.Contains(err.Error(), "404") {
				return nil, errPrivateList
			}
			return nil, err
		}
		c := r.Data.MediaListCollection
		if c == nil {
			return nil, errPrivateList
		}
		for _, l := range c.Lists {
			if l.IsCustomList {
				continue
			}
			for _, e := range l.Entries {
				if !seen[e.MediaID] {
					seen[e.MediaID] = true
					out = append(out, e)
				}
			}
		}
		if !c.HasNextChunk {
			break
		}
	}
	return out, nil
}

// listCache keeps members' lists for an hour (a private list for ten
// minutes, so making it public shows up soon).
type listCache struct {
	mu    sync.Mutex
	lists map[int]cachedList
	fetch func(int) ([]listEntry, error)
	now   func() time.Time
}

type cachedList struct {
	at      time.Time
	entries []listEntry
	err     error
}

const (
	listTTL    = time.Hour
	privateTTL = 10 * time.Minute
)

var anilistLists = &listCache{lists: map[int]cachedList{}, fetch: fetchAniListList, now: time.Now}

func (c *listCache) get(id int) ([]listEntry, error) {
	c.mu.Lock()
	cl, ok := c.lists[id]
	c.mu.Unlock()
	now := c.now()
	if ok {
		ttl := listTTL
		if cl.err != nil {
			ttl = privateTTL
		}
		if now.Sub(cl.at) < ttl {
			return cl.entries, cl.err
		}
	}
	entries, err := c.fetch(id)
	if err != nil && !errors.Is(err, errPrivateList) {
		return nil, err // AniList trouble: don't remember it
	}
	c.mu.Lock()
	c.lists[id] = cachedList{at: now, entries: entries, err: err}
	c.mu.Unlock()
	return entries, err
}

// maxSocialMembers caps how many linked lists one command reads.
const maxSocialMembers = 200

// linkedMembers copies the linked AniList accounts.
func (b *bot) linkedMembers() map[string]anilistLink {
	out := map[string]anilistLink{}
	b.store.view(func(d *storeData) {
		for k, v := range d.AniList {
			out[k] = v
		}
	})
	return out
}

// inServer reports whether a member is still in the server. When Discord
// can't say, they count as here.
func (b *bot) inServer(user string) bool {
	if m, err := b.s.State.Member(b.cfg.GuildID, user); err == nil && m != nil {
		return true
	}
	_, err := b.s.GuildMember(b.cfg.GuildID, user)
	var rest *discordgo.RESTError
	if errors.As(err, &rest) && rest.Response != nil && rest.Response.StatusCode == 404 {
		return false
	}
	return true
}

// memberLists reads the lists of linked members still in the server,
// leaving out skip and private lists.
func (b *bot) memberLists(skip string) (map[string][]listEntry, error) {
	out := map[string][]listEntry{}
	n := 0
	var lastErr error
	for user, link := range b.linkedMembers() {
		if user == skip || n >= maxSocialMembers || !b.inServer(user) {
			continue
		}
		n++
		entries, err := anilistLists.get(link.ID)
		if err != nil {
			if !errors.Is(err, errPrivateList) {
				lastErr = err
				log.Printf("anilist list %s: %v", link.Name, err)
			}
			continue
		}
		out[user] = entries
	}
	if len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return out, nil
}

// Plan to watch and the show card.

const planShowQuery = `query ($id: Int) {
  Media(id: $id) {
    id idMal siteUrl isAdult averageScore genres status format episodes
    description(asHtml: false)
    startDate { year month day }
    nextAiringEpisode { airingAt episode }
    coverImage { extraLarge }
    title { romaji english }
  }
}`

type planShowInfo struct {
	airingShowInfo
	Status            string    `json:"status"`
	Format            string    `json:"format"`
	Episodes          int       `json:"episodes"`
	StartDate         fuzzyDate `json:"startDate"`
	NextAiringEpisode *struct {
		AiringAt int64 `json:"airingAt"`
		Episode  int   `json:"episode"`
	} `json:"nextAiringEpisode"`
}

// formatNames are AniList formats as people say them.
var formatNames = map[string]string{"TV": "TV", "TV_SHORT": "TV short", "ONA": "ONA", "OVA": "OVA", "MOVIE": "Movie", "SPECIAL": "Special", "MUSIC": "Music"}

// planCard is a show's card with Plan to watch, DM me when it airs (while
// it still airs) and the AniList and MyAnimeList links.
func planCard(info planShowInfo) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	e, _ := airingCard(info.airingShowInfo, airing{}, false)
	var facts []string
	if f := formatNames[info.Format]; f != "" {
		facts = append(facts, f)
	}
	if info.Episodes > 0 {
		facts = append(facts, plural(info.Episodes, "episode"))
	}
	switch n := info.NextAiringEpisode; {
	case n != nil && n.Episode == 1:
		facts = append(facts, fmt.Sprintf("starts <t:%d:D> (<t:%d:R>)", n.AiringAt, n.AiringAt))
	case n != nil:
		facts = append(facts, fmt.Sprintf("episode %d <t:%d:R>", n.Episode, n.AiringAt))
	case info.Status == "NOT_YET_RELEASED" && !info.StartDate.time().IsZero():
		facts = append(facts, "starts "+startText(info.StartDate))
	}
	if len(facts) > 0 {
		e.Description = strings.TrimSpace(strings.Join(facts, " · ") + "\n" + e.Description)
	}
	id := strconv.Itoa(info.ID)
	row := discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: "Plan to watch", Style: discordgo.PrimaryButton, CustomID: "plan-watch:" + id},
	}}
	if info.Status != "FINISHED" && info.Status != "CANCELLED" {
		row.Components = append(row.Components, discordgo.Button{Label: "DM me when it airs", Style: discordgo.SecondaryButton, CustomID: "airing-follow:" + id})
	}
	row.Components = append(row.Components, discordgo.Button{Label: "AniList", Style: discordgo.LinkButton, URL: info.SiteURL})
	if info.IDMal > 0 {
		row.Components = append(row.Components, discordgo.Button{Label: "MyAnimeList", Style: discordgo.LinkButton, URL: fmt.Sprintf("https://myanimelist.net/anime/%d", info.IDMal)})
	}
	return e, withQueueButton([]discordgo.MessageComponent{row}, info.ID)
}

// startText is a start date as precise as AniList knows it: "3 Oct 2026",
// "Oct 2026" or "2026".
func startText(f fuzzyDate) string {
	t := f.time()
	switch {
	case f.Month == 0:
		return strconv.Itoa(f.Year)
	case f.Day == 0:
		return t.Format("Jan 2006")
	}
	return t.Format("2 Jan 2006")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// pickPlanShow answers a pick from the season preview or /recommend menu
// with the show's card.
func (b *bot) pickPlanShow(i *discordgo.InteractionCreate) {
	vals := i.MessageComponentData().Values
	if len(vals) == 0 {
		return
	}
	id, _ := strconv.Atoi(vals[0])
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		var r struct {
			Data struct {
				Media *planShowInfo `json:"Media"`
			} `json:"data"`
		}
		if err := anilistSocialPost("", planShowQuery, map[string]any{"id": id}, &r); err != nil || r.Data.Media == nil || r.Data.Media.IsAdult {
			if err != nil {
				log.Printf("show card: %v", err)
			}
			return errorEmbed("Couldn't load that show from AniList. Try again."), nil
		}
		return planCard(*r.Data.Media)
	})
}

// listStatusNames are AniList list statuses as the site shows them.
var listStatusNames = map[string]string{"CURRENT": "Watching", "PLANNING": "Planning", "COMPLETED": "Completed",
	"DROPPED": "Dropped", "PAUSED": "Paused", "REPEATING": "Rewatching"}

// errAniListToken is AniList refusing a member's stored access.
var errAniListToken = errors.New("anilist token refused")

// anilistPlan adds a show to the member's list as Planning, unless it is on
// their list already; then it returns the status it has there.
func anilistPlan(token string, id int) (added bool, status string, err error) {
	var r struct {
		Data struct {
			Media *struct {
				MediaListEntry *struct {
					Status string `json:"status"`
				} `json:"mediaListEntry"`
			} `json:"Media"`
		} `json:"data"`
	}
	if err := anilistSocialPost(token, `query ($id: Int) { Media(id: $id) { mediaListEntry { status } } }`, map[string]any{"id": id}, &r); err != nil {
		return false, "", tokenErr(err)
	}
	if r.Data.Media != nil && r.Data.Media.MediaListEntry != nil {
		return false, r.Data.Media.MediaListEntry.Status, nil
	}
	var w struct {
		Data struct {
			SaveMediaListEntry *struct {
				Status string `json:"status"`
			} `json:"SaveMediaListEntry"`
		} `json:"data"`
	}
	if err := anilistSocialPost(token, `mutation ($id: Int) { SaveMediaListEntry(mediaId: $id, status: PLANNING) { id status } }`, map[string]any{"id": id}, &w); err != nil {
		return false, "", tokenErr(err)
	}
	if w.Data.SaveMediaListEntry == nil {
		return false, "", fmt.Errorf("not saved")
	}
	return true, "PLANNING", nil
}

// tokenErr turns AniList's 400 and 401 answers into errAniListToken.
func tokenErr(err error) error {
	if s := err.Error(); strings.Contains(s, ": 401") || strings.Contains(s, ": 400") {
		return fmt.Errorf("%w: %v", errAniListToken, err)
	}
	return err
}

// planToWatch is the Plan to watch button.
func (b *bot) planToWatch(i *discordgo.InteractionCreate, idText string) {
	id, err := strconv.Atoi(idText)
	if err != nil {
		return
	}
	user := i.Member.User.ID
	var link anilistLink
	var linked bool
	b.store.view(func(d *storeData) { link, linked = d.AniList[user] })
	page := fmt.Sprintf("https://anilist.co/anime/%d", id)
	if !linked {
		b.respond(i, true, errorEmbed("Link your AniList with `/link-anilist` first, then **Plan to watch** adds shows to your list."),
			linkButtons("Open on AniList", page)...)
		return
	}
	if link.Token == "" {
		msg := "This server links AniList by username only, so the bot can't change your list. Add it on AniList instead."
		if b.cfg.AniListClientID != "" && b.cfg.AniListSecret != "" {
			msg = "Your AniList is linked by username, so the bot can't change your list. Run `/link-anilist` again and approve the bot on AniList, or add it there yourself."
		}
		b.respond(i, true, errorEmbed("%s", msg), linkButtons("Open on AniList", page)...)
		return
	}
	b.deferThen(i, true, func() (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
		added, status, err := anilistPlan(link.Token, id)
		switch {
		case errors.Is(err, errAniListToken):
			log.Printf("plan to watch: %v", err)
			b.store.update(func(d *storeData) {
				if l, ok := d.AniList[user]; ok && l.Token == link.Token {
					l.Token = ""
					d.AniList[user] = l
				}
			})
			return errorEmbed("AniList no longer accepts the bot's access to your list. Run `/link-anilist` again to fix it."), linkButtons("Open on AniList", page)
		case err != nil:
			log.Printf("plan to watch: %v", err)
			return errorEmbed("Couldn't reach AniList. Try again in a minute."), linkButtons("Open on AniList", page)
		case !added:
			return &discordgo.MessageEmbed{Description: fmt.Sprintf("It's on your AniList already, as **%s**.", listStatusNames[status]), Color: shu},
				linkButtons("Open on AniList", page)
		}
		// Their list changed: read it fresh next time.
		anilistLists.mu.Lock()
		delete(anilistLists.lists, link.ID)
		anilistLists.mu.Unlock()
		return &discordgo.MessageEmbed{Description: "Added to your AniList as **Planning**.", Color: shu}, linkButtons("Open on AniList", page)
	})
}
