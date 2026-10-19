package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// GET /v1/news, no token needed: the latest post in the announcements
// channel as plain text, how many members are online and in the server,
// and the invite link. The app shows it in its menus. Cached for five
// minutes so the app can't make the bot hammer Discord.

const newsTTL = 5 * time.Minute

type newsAnnouncement struct {
	Title string `json:"title"`
	Text  string `json:"text"`
	URL   string `json:"url"`
	At    string `json:"at"`
}

type newsReply struct {
	Announcement *newsAnnouncement `json:"announcement"`
	Online       int               `json:"online"`
	Members      int               `json:"members"`
	Invite       string            `json:"invite"`
	// ScoreSharing tells the app whether to offer sharing scores (/settings
	// score_sharing), filled in per answer rather than cached.
	ScoreSharing bool `json:"score_sharing"`
}

var (
	newsLimit = newRateLimit(30, time.Minute)
	newsCache = struct {
		sync.Mutex
		reply  *newsReply
		at     time.Time
		failed time.Time // last failed fetch with nothing cached
	}{}
	// fetchNews is a variable so tests skip Discord.
	fetchNews = (*bot).fetchNews
)

func init() {
	apiHandle("GET /v1/news", (*bot).apiNews)
}

func (b *bot) apiNews(w http.ResponseWriter, r *http.Request) {
	now := appNow()
	if !newsLimit.allow(clientIP(r), now) {
		apiError(w, http.StatusTooManyRequests, "too many requests")
		return
	}
	reply, err := b.cachedNews(now)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "news unavailable right now")
		return
	}
	answer := *reply
	answer.ScoreSharing = b.settingBool("score_sharing")
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, answer)
}

// cachedNews is the news at most five minutes old; when Discord fails the
// last news is kept for another minute.
func (b *bot) cachedNews(now time.Time) (*newsReply, error) {
	newsCache.Lock()
	defer newsCache.Unlock()
	if newsCache.reply != nil && now.Sub(newsCache.at) < newsTTL {
		return newsCache.reply, nil
	}
	if newsCache.reply == nil && now.Sub(newsCache.failed) < time.Minute {
		return nil, errors.New("news failed a moment ago")
	}
	reply, err := fetchNews(b)
	if err != nil {
		log.Printf("news: %v", err)
		if newsCache.reply != nil {
			newsCache.at = now.Add(time.Minute - newsTTL)
			return newsCache.reply, nil
		}
		newsCache.failed = now
		return nil, err
	}
	newsCache.reply, newsCache.at = reply, now
	return reply, nil
}

func (b *bot) fetchNews() (*newsReply, error) {
	if b.s == nil {
		return nil, errors.New("not connected")
	}
	g, err := b.s.GuildWithCounts(b.cfg.GuildID)
	if err != nil {
		return nil, err
	}
	reply := &newsReply{Online: g.ApproximatePresenceCount, Members: g.ApproximateMemberCount, Invite: appInviteURL}
	if ch := b.announcementsChannel(); ch != "" {
		msgs, err := b.s.ChannelMessages(ch, 10, "", "", "")
		if err != nil {
			log.Printf("news messages: %v", err)
		}
		for _, m := range msgs {
			if a := announcementFrom(m, b.cfg.GuildID); a != nil {
				reply.Announcement = a
				break
			}
		}
	}
	return reply, nil
}

// announcementsChannel finds the announcements (or news) channel by name,
// a "告・" style prefix allowed.
func (b *bot) announcementsChannel() string {
	chans, err := b.s.GuildChannels(b.cfg.GuildID)
	if err != nil {
		log.Printf("listing channels: %v", err)
		return ""
	}
	var news string
	for _, c := range chans {
		if c.Type != discordgo.ChannelTypeGuildText && c.Type != discordgo.ChannelTypeGuildNews {
			continue
		}
		switch channelBase(c.Name) {
		case "announcements":
			return c.ID
		case "news":
			news = c.ID
		}
	}
	return news
}

// announcementFrom turns a message into the app's announcement: the first
// short line (or the embed's title) as title, the rest as plain text.
func announcementFrom(m *discordgo.Message, guild string) *newsAnnouncement {
	title, text := "", m.Content
	if strings.TrimSpace(text) == "" && len(m.Embeds) > 0 {
		title, text = m.Embeds[0].Title, m.Embeds[0].Description
	}
	text = newsText(text)
	if title = clip(newsText(title), 100); title == "" {
		first, rest, more := strings.Cut(text, "\n")
		switch {
		case more && len([]rune(first)) <= 100 && strings.TrimSpace(rest) != "":
			title, text = strings.TrimSpace(first), strings.TrimSpace(rest)
		default:
			title = clip(strings.TrimSpace(first), 80)
		}
	}
	if title == "" && text == "" {
		return nil
	}
	return &newsAnnouncement{
		Title: title,
		Text:  clip(text, 500),
		URL:   fmt.Sprintf("https://discord.com/channels/%s/%s/%s", guild, m.ChannelID, m.ID),
		At:    m.Timestamp.UTC().Format(time.RFC3339),
	}
}

var (
	mdTimeRe    = regexp.MustCompile(`<t:(-?\d{1,12})(?::[tTdDfFR])?>`)
	mdUserRe    = regexp.MustCompile(`<@!?\d+>`)
	mdRoleRe    = regexp.MustCompile(`<@&\d+>`)
	mdChannelRe = regexp.MustCompile(`<#\d+>`)
	mdEmojiRe   = regexp.MustCompile(`<a?(:\w+:)\d+>`)
	mdURLRe     = regexp.MustCompile(`<(https?://[^>\s]+)>`)
	newsLinkRe  = regexp.MustCompile(`\[([^\]]*)\]\((?:<)?(https?://[^)\s>]+)(?:>)?\)`)
	mdLineRe    = regexp.MustCompile(`(?m)^[ \t]*(?:#{1,3} |-# |>>> |> )`)
	mdFenceRe   = regexp.MustCompile("```[a-zA-Z0-9]*\n?")
	mdBlankRe   = regexp.MustCompile(`\n{3,}`)
)

// newsText strips Discord markdown for the app: formatting marks go,
// links keep their text, mentions become plain words and timestamps dates.
func newsText(s string) string {
	s = mdTimeRe.ReplaceAllStringFunc(s, func(t string) string {
		n, _ := strconv.ParseInt(mdTimeRe.FindStringSubmatch(t)[1], 10, 64)
		return time.Unix(n, 0).UTC().Format("2 Jan 2006")
	})
	s = mdRoleRe.ReplaceAllString(s, "@role")
	s = mdUserRe.ReplaceAllString(s, "@member")
	s = mdChannelRe.ReplaceAllString(s, "#channel")
	s = mdEmojiRe.ReplaceAllString(s, "$1")
	s = mdURLRe.ReplaceAllString(s, "$1")
	s = newsLinkRe.ReplaceAllString(s, "$1")
	s = mdFenceRe.ReplaceAllString(s, "")
	s = mdLineRe.ReplaceAllString(s, "")
	s = strings.NewReplacer("**", "", "__", "", "~~", "", "||", "", "`", "", "*", "").Replace(s)
	s = cleanText(s, true)
	return strings.TrimSpace(mdBlankRe.ReplaceAllString(s, "\n\n"))
}
