package main

import (
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

func (b *bot) onMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot || m.GuildID != b.cfg.GuildID {
		return
	}
	if b.filterMessage(m) || b.spam.check(b, m) {
		return
	}
	go b.linkIssues(m)
	if downRe.MatchString(m.Content) {
		b.downCheck(m)
		return
	}
	if f, ok := b.auto.match(m.ChannelID, m.Content); ok {
		b.sendFAQ(m, f)
		return
	}
	// Questions the patterns miss are matched by meaning, when Ollama is set up.
	go func() {
		intent, ok := b.sem.match(m.Content)
		if !ok {
			return
		}
		if intent == "down" {
			b.downCheck(m)
			return
		}
		if f, found := faqByKey(intent); found && b.auto.allow(m.ChannelID, intent) {
			b.sendFAQ(m, f)
		}
	}()
}

func (b *bot) sendFAQ(m *discordgo.MessageCreate, f faqEntry) {
	_, err := b.s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Embeds:          []*discordgo.MessageEmbed{{Title: f.Question, Description: f.Answer, Color: shu}},
		Reference:       m.Reference(),
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	})
	if err != nil {
		log.Printf("auto reply: %v", err)
	}
}

func (b *bot) onMemberAdd(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
	if m.User.Bot {
		return
	}
	b.checkJoin(m)
	if !b.cfg.WelcomeDM {
		return
	}
	ch, err := s.UserChannelCreate(m.User.ID)
	if err != nil {
		return
	}
	// Members who block DMs from servers are skipped quietly.
	_, _ = s.ChannelMessageSendComplex(ch.ID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{{Description: welcomeDM, Color: shu}},
		Components: linkButtons("Website", siteURL, "Wiki", wikiURL),
	})
}

// autoReplier answers common questions seen in chat with the matching FAQ
// entry, at most once per topic per channel in a while.
type autoReplier struct {
	mu   sync.Mutex
	last map[string]time.Time
}

type autoRule struct {
	re  *regexp.Regexp
	faq string
}

var autoRules = []autoRule{
	{regexp.MustCompile(`(?i)\bmpv\b.*\b(not found|missing|not installed|isn'?t installed|no such file)|(not found|missing).*\bmpv\b`), "mpv"},
	{regexp.MustCompile(`(?i)\bicons?\b.*\b(boxes|squares|question marks|broken|weird)\b`), "icons"},
	{regexp.MustCompile(`(?i)\b(nothing|no results?|no anime|can'?t find any)\b.*\b(found|show|search|anime)\b`), "nothing-found"},
	{regexp.MustCompile(`(?i)\b(anilist|mal|myanimelist|progress)\b.*\b(not|doesn'?t|didn'?t|won'?t)\b.*\b(sync|updat|track)`), "sync"},
	{regexp.MustCompile(`(?i)\b(cast|chromecast|tv|kodi|dlna)\b.*\b(not found|can'?t find|doesn'?t (show|find)|not showing)\b`), "cast"},
	{regexp.MustCompile(`(?i)\bhow (do i|to|can i)\b.*\b(install|update|upgrade)\b`), "install"},
}

// allow reports whether the topic wasn't answered in the channel lately,
// and marks it answered.
func (a *autoReplier) allow(channel, faq string) bool {
	key := channel + "/" + faq
	a.mu.Lock()
	defer a.mu.Unlock()
	if time.Since(a.last[key]) < 30*time.Minute {
		return false
	}
	a.last[key] = time.Now()
	return true
}

func newAutoReplier() *autoReplier { return &autoReplier{last: map[string]time.Time{}} }

func (a *autoReplier) match(channel, text string) (faqEntry, bool) {
	if len(text) < 12 {
		return faqEntry{}, false
	}
	for _, r := range autoRules {
		if !r.re.MatchString(text) {
			continue
		}
		if !a.allow(channel, r.faq) {
			return faqEntry{}, false
		}
		return faqByKey(r.faq)
	}
	return faqEntry{}, false
}

// spamGuard catches the usual scam pattern Discord's AutoMod misses: the
// same message posted in several channels within a minute. The copies are
// deleted, the author is timed out for an hour and moderators are told.
type spamGuard struct {
	mu   sync.Mutex
	seen map[string][]sighting
}

type sighting struct {
	at        time.Time
	channel   string
	messageID string
}

func newSpamGuard() *spamGuard { return &spamGuard{seen: map[string][]sighting{}} }

const (
	spamWindow   = time.Minute
	spamChannels = 3
)

func (g *spamGuard) check(b *bot, m *discordgo.MessageCreate) bool {
	text := strings.ToLower(strings.Join(strings.Fields(m.Content), " "))
	if len(text) < 10 && len(m.Attachments) == 0 {
		return false
	}
	if b.isStaff(m) {
		return false
	}
	key := m.Author.ID + "\x00" + text
	g.mu.Lock()
	now := time.Now()
	var recent []sighting
	for _, s := range g.seen[key] {
		if now.Sub(s.at) < spamWindow {
			recent = append(recent, s)
		}
	}
	recent = append(recent, sighting{now, m.ChannelID, m.ID})
	g.seen[key] = recent
	channels := map[string]bool{}
	for _, s := range recent {
		channels[s.channel] = true
	}
	hit := len(channels) >= spamChannels
	if hit {
		delete(g.seen, key)
	}
	// Forget old entries now and then so the map stays small.
	if len(g.seen) > 5000 {
		g.seen = map[string][]sighting{}
	}
	g.mu.Unlock()
	if !hit {
		return false
	}

	for _, s := range recent {
		_ = b.s.ChannelMessageDelete(s.channel, s.messageID)
	}
	until := now.Add(time.Hour)
	if err := b.s.GuildMemberTimeout(b.cfg.GuildID, m.Author.ID, &until, discordgo.WithAuditLogReason("Same message in several channels")); err != nil {
		log.Printf("timeout: %v", err)
	}
	if b.cfg.ModChannel != "" {
		_, _ = b.s.ChannelMessageSendComplex(b.cfg.ModChannel, &discordgo.MessageSend{
			Embeds: []*discordgo.MessageEmbed{{
				Title:       "Spam removed",
				Description: "<@" + m.Author.ID + "> posted the same message in " + strconv.Itoa(len(channels)) + " channels. Copies deleted, timed out for 1 hour.\n```\n" + truncate(m.Content, 800) + "\n```",
				Color:       shu,
			}},
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		})
	}
	return true
}

func truncate(s string, n int) string {
	r := []rune(strings.ReplaceAll(s, "```", "'''"))
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return string(r)
}
