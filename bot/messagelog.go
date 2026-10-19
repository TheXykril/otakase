package main

import (
	"container/list"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Message log: deleted and edited messages go to a staff-only "mod-log"
// channel (else mod-chat, without pinging anyone). Discord doesn't send the
// old text with a delete or edit, so the bot keeps its own small cache of
// recent messages. Messages the bot removed itself (scam filter, spam guard,
// /purge, report buttons) aren't logged again, except a purge, which shows
// up as one summary (also when it deleted a single message).

const (
	msgCacheSize = 5000
	msgCacheAge  = 24 * time.Hour
	removedKeep  = 10 * time.Minute // how long a "removed by the bot" mark waits for its delete event
	editColor    = 0xD9A33A
)

type attachmentRef struct{ Name, URL string }

type cachedMsg struct {
	ID, Channel     string
	Author, Name    string // user id and display name
	Avatar, Content string
	Attachments     []attachmentRef
	At              time.Time
}

// msgCache remembers recent guild messages, oldest dropped first.
type msgCache struct {
	mu      sync.Mutex
	size    int
	age     time.Duration
	order   *list.List // front is newest
	byID    map[string]*list.Element
	removed map[string]removal

	logOnce sync.Once
	logCh   string
}

type removal struct {
	reason string
	at     time.Time
}

func newMsgCache() *msgCache {
	return &msgCache{size: msgCacheSize, age: msgCacheAge, order: list.New(), byID: map[string]*list.Element{}, removed: map[string]removal{}}
}

func (c *msgCache) put(m cachedMsg, now time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.byID[m.ID]; ok {
		e.Value = m
		c.order.MoveToFront(e)
	} else {
		c.byID[m.ID] = c.order.PushFront(m)
	}
	for c.order.Len() > c.size {
		c.drop(c.order.Back())
	}
	for e := c.order.Back(); e != nil && now.Sub(e.Value.(cachedMsg).At) > c.age; e = c.order.Back() {
		c.drop(e)
	}
}

func (c *msgCache) drop(e *list.Element) {
	delete(c.byID, e.Value.(cachedMsg).ID)
	c.order.Remove(e)
}

func (c *msgCache) get(id string, now time.Time) (cachedMsg, bool) {
	if c == nil {
		return cachedMsg{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.byID[id]
	if !ok {
		return cachedMsg{}, false
	}
	m := e.Value.(cachedMsg)
	if now.Sub(m.At) > c.age {
		c.drop(e)
		return cachedMsg{}, false
	}
	return m, true
}

// take returns a message and forgets it.
func (c *msgCache) take(id string, now time.Time) (cachedMsg, bool) {
	m, ok := c.get(id, now)
	if ok {
		c.mu.Lock()
		if e, ok := c.byID[id]; ok {
			c.drop(e)
		}
		c.mu.Unlock()
	}
	return m, ok
}

// markRemoved notes that the bot is deleting these messages itself, and why.
func (c *msgCache) markRemoved(reason string, ids ...string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for id, r := range c.removed {
		if now.Sub(r.at) > removedKeep {
			delete(c.removed, id)
		}
	}
	for _, id := range ids {
		c.removed[id] = removal{reason, now}
	}
}

// removedBy returns why the bot removed a message, once.
func (c *msgCache) removedBy(id string) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.removed[id]
	delete(c.removed, id)
	return r.reason, ok
}

func cachedFrom(m *discordgo.Message) cachedMsg {
	c := cachedMsg{ID: m.ID, Channel: m.ChannelID, Content: m.Content, At: m.Timestamp}
	if c.At.IsZero() {
		c.At = time.Now()
	}
	if m.Author != nil {
		c.Author, c.Name, c.Avatar = m.Author.ID, m.Author.Username, m.Author.AvatarURL("64")
		if m.Member != nil && m.Member.Nick != "" {
			c.Name = m.Member.Nick
		} else if m.Author.GlobalName != "" {
			c.Name = m.Author.GlobalName
		}
	}
	for _, a := range m.Attachments {
		c.Attachments = append(c.Attachments, attachmentRef{a.Filename, a.URL})
	}
	return c
}

// msgLogChannel is OTAKASE_MOD_LOG_CHANNEL, else the "mod-log" channel,
// found once. Empty means mod-chat.
func (b *bot) msgLogChannel() string {
	c := b.msgs
	c.logOnce.Do(func() {
		c.logCh = os.Getenv("OTAKASE_MOD_LOG_CHANNEL")
		if c.logCh != "" {
			return
		}
		chans, err := b.s.GuildChannels(b.cfg.GuildID)
		if err != nil {
			log.Printf("message log: %v", err)
			return
		}
		for _, ch := range chans {
			if ch.Type == discordgo.ChannelTypeGuildText && strings.EqualFold(channelBase(ch.Name), "mod-log") {
				c.logCh = ch.ID
			}
		}
		log.Printf("message log channel %q", c.logCh)
	})
	return c.logCh
}

func (b *bot) postMsgLog(e *discordgo.MessageEmbed) {
	ch := b.msgLogChannel()
	if ch == "" {
		b.modLog(e, nil, false)
		return
	}
	if _, err := b.s.ChannelMessageSendComplex(ch, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{e}, AllowedMentions: &discordgo.MessageAllowedMentions{}}); err != nil {
		log.Printf("message log: %v", err)
	}
}

// cacheMessage remembers a member's message for the message log.
func (b *bot) cacheMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot || m.WebhookID != "" || m.GuildID != b.cfg.GuildID {
		return
	}
	b.msgs.put(cachedFrom(m.Message), time.Now())
}

func quoteText(s string, n int) string {
	if strings.TrimSpace(s) == "" {
		return "*(no text)*"
	}
	return clip(s, n)
}

func attachmentLines(as []attachmentRef) string {
	var lines []string
	for _, a := range as {
		lines = append(lines, fmt.Sprintf("[%s](%s)", strings.NewReplacer("[", "(", "]", ")").Replace(a.Name), a.URL))
	}
	return clip(strings.Join(lines, "\n"), 1000)
}

func msgAuthor(c cachedMsg) *discordgo.MessageEmbedAuthor {
	return &discordgo.MessageEmbedAuthor{Name: c.Name, IconURL: c.Avatar}
}

func deletedEmbed(guildID string, c cachedMsg, now time.Time) *discordgo.MessageEmbed {
	e := &discordgo.MessageEmbed{Title: "Message deleted", Color: shu, Author: msgAuthor(c),
		Description: quoteText(c.Content, 3000),
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Author", Value: "<@" + c.Author + ">", Inline: true},
			{Name: "Channel", Value: fmt.Sprintf("<#%s> · [around here](%s)", c.Channel, messageLink(guildID, c.Channel, c.ID)), Inline: true},
			{Name: "Sent", Value: fmt.Sprintf("<t:%d:f>", c.At.Unix()), Inline: true},
		},
		Footer:    &discordgo.MessageEmbedFooter{Text: "User ID " + c.Author},
		Timestamp: now.Format(time.RFC3339),
	}
	if len(c.Attachments) > 0 {
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: "Attachments", Value: attachmentLines(c.Attachments)})
	}
	return e
}

func editedEmbed(guildID string, old cachedMsg, known bool, now cachedMsg, at time.Time) *discordgo.MessageEmbed {
	before := quoteText(old.Content, 1000)
	if !known {
		before = "*(sent before the bot started keeping track)*"
	}
	e := &discordgo.MessageEmbed{Title: "Message edited", URL: messageLink(guildID, now.Channel, now.ID), Color: editColor, Author: msgAuthor(now),
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Before", Value: before},
			{Name: "After", Value: quoteText(now.Content, 1000)},
			{Name: "Author", Value: "<@" + now.Author + ">", Inline: true},
			{Name: "Channel", Value: "<#" + now.Channel + ">", Inline: true},
			{Name: "Sent", Value: fmt.Sprintf("<t:%d:f>", now.At.Unix()), Inline: true},
		},
		Footer:    &discordgo.MessageEmbedFooter{Text: "User ID " + now.Author},
		Timestamp: at.Format(time.RFC3339),
	}
	if known && len(old.Attachments) > len(now.Attachments) {
		var gone []attachmentRef
		for _, a := range old.Attachments {
			kept := false
			for _, n := range now.Attachments {
				kept = kept || n.URL == a.URL
			}
			if !kept {
				gone = append(gone, a)
			}
		}
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: "Attachments removed", Value: attachmentLines(gone)})
	}
	return e
}

// bulkEmbed sums up a bulk delete in one entry.
func bulkEmbed(channel string, total int, msgs []cachedMsg, reason string, now time.Time) *discordgo.MessageEmbed {
	desc := fmt.Sprintf("In <#%s>", channel)
	if reason != "" {
		desc += ", removed by the bot: " + reason
	}
	desc += "\n"
	shown := 0
	for _, m := range msgs {
		line := fmt.Sprintf("\n<@%s>: %s", m.Author, clip(strings.Join(strings.Fields(quoteText(m.Content, 200)), " "), 100))
		if len(m.Attachments) > 0 {
			line += fmt.Sprintf(" (+%d attachment(s))", len(m.Attachments))
		}
		if len(desc)+len(line) > 3800 {
			break
		}
		desc += line
		shown++
	}
	if rest := total - shown; rest > 0 {
		desc += fmt.Sprintf("\n\n%d more the bot hadn't seen or couldn't fit.", rest)
	}
	title := fmt.Sprintf("%d messages deleted", total)
	if total == 1 {
		title = "1 message deleted"
	}
	return &discordgo.MessageEmbed{Title: title, Color: shu, Description: desc, Timestamp: now.Format(time.RFC3339)}
}

func (b *bot) logMessageDelete(s *discordgo.Session, m *discordgo.MessageDelete) {
	if m.GuildID != b.cfg.GuildID {
		return
	}
	now := time.Now()
	reason, byBot := b.msgs.removedBy(m.ID)
	c, ok := b.msgs.take(m.ID, now)
	if byBot && strings.HasPrefix(reason, "/purge") {
		var msgs []cachedMsg
		if ok {
			msgs = append(msgs, c)
		}
		b.postMsgLog(bulkEmbed(m.ChannelID, 1, msgs, reason, now))
		return
	}
	if byBot || !ok {
		return // the bot logged it already, or it's not a member's message the bot saw
	}
	b.postMsgLog(deletedEmbed(b.cfg.GuildID, c, now))
}

func (b *bot) logMessageDeleteBulk(s *discordgo.Session, m *discordgo.MessageDeleteBulk) {
	if m.GuildID != b.cfg.GuildID || len(m.Messages) == 0 {
		return
	}
	now := time.Now()
	var msgs []cachedMsg
	reason := ""
	for _, id := range m.Messages {
		if r, ok := b.msgs.removedBy(id); ok {
			reason = r
		}
		if c, ok := b.msgs.take(id, now); ok {
			msgs = append(msgs, c)
		}
	}
	// Oldest first reads like the channel did.
	slices.SortFunc(msgs, func(x, y cachedMsg) int { return x.At.Compare(y.At) })
	b.postMsgLog(bulkEmbed(m.ChannelID, len(m.Messages), msgs, reason, now))
}

// editChanged reports whether an update is a real edit: Discord also sends
// updates when link previews load, without an edit time or new text.
func editChanged(old cachedMsg, known bool, m *discordgo.Message) bool {
	if m.EditedTimestamp == nil {
		return false
	}
	if !known {
		return true
	}
	return old.Content != m.Content || len(old.Attachments) != len(m.Attachments)
}

func (b *bot) logMessageUpdate(s *discordgo.Session, m *discordgo.MessageUpdate) {
	if m.Message == nil || m.GuildID != b.cfg.GuildID {
		return
	}
	now := time.Now()
	old, known := b.msgs.get(m.ID, now)
	if !known && (m.Author == nil || m.Author.Bot || m.WebhookID != "") {
		return
	}
	if !editChanged(old, known, m.Message) {
		return
	}
	cur := cachedFrom(m.Message)
	if known {
		// Updates can leave out the author; the cache has it.
		cur.Author, cur.Name, cur.Avatar, cur.At = old.Author, old.Name, old.Avatar, old.At
	} else if m.EditedTimestamp != nil && now.Sub(*m.EditedTimestamp) > time.Minute {
		return // an old edit replayed, not news
	}
	b.msgs.put(cur, now)
	b.postMsgLog(editedEmbed(b.cfg.GuildID, old, known, cur, now))
}
