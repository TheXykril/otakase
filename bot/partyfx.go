package main

import (
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/bwmarrin/discordgo"
)

// Party extras on the sync hub: skip votes from the players, and emoji and
// chat from the party's voice channel chat shown on everyone's screen.
//
//	app → bot: {"type":"vote","what":"skip"} (skip the opening or ending) or "next" (the next episode)
//	bot → all: {"type":"votes","what":"skip","votes":2,"needed":3} after each vote
//	           {"type":"voted","what":"skip"} once more than half of the
//	           members in the party agree: the host's app skips (or plays the
//	           next episode) and the others follow it as usual
//	bot → all: {"type":"react","emoji":"🔥","name":"Alex"} for an emoji-only
//	           message or a reaction in the voice channel's chat
//	           {"type":"chat","name":"Alex","text":"…"} for any other message
//
// Only synced /watchparty parties have a voice channel, so Watch with me
// parties get votes but no chat.

const (
	partyVoteWindow = 90 * time.Second // a vote counts this long
	partyRelayRate  = 5                // chat lines and reactions per second per party
	partyChatMax    = 200              // runes of a chat line
	partyEmojiMax   = 24               // runes of a reaction
	partyNameMax    = 32
)

func init() {
	onStart(func(b *bot) {
		b.s.AddHandler(func(_ *discordgo.Session, m *discordgo.MessageCreate) { b.partyChatMessage(m) })
		b.s.AddHandler(func(_ *discordgo.Session, r *discordgo.MessageReactionAdd) { b.partyChatReaction(r) })
	})
}

// votesNeeded is more than half of the members.
func votesNeeded(members int) int { return members/2 + 1 }

// roomUsers is how many members (the host too) are in the party.
func roomUsers(r *partyRoom) int {
	users := map[string]bool{}
	for c := range r.conns {
		users[c.user] = true
	}
	return len(users)
}

// vote counts c's vote for what and tells everyone the tally, or that it
// passed. The hub's lock is held.
func (h *partyHub) vote(r *partyRoom, c *partyConn, what string, now time.Time) {
	if what != "skip" && what != "next" {
		return
	}
	if r.votes == nil {
		r.votes = map[string]map[string]time.Time{}
	}
	v := r.votes[what]
	if v == nil {
		v = map[string]time.Time{}
		r.votes[what] = v
	}
	for u, at := range v {
		if now.Sub(at) > partyVoteWindow {
			delete(v, u)
		}
	}
	v[c.user] = now
	h.tally(r, what)
}

// tally sends the count for what, or "voted" and a fresh start once enough
// members agree.
func (h *partyHub) tally(r *partyRoom, what string) {
	n, need := len(r.votes[what]), votesNeeded(roomUsers(r))
	if n >= need {
		delete(r.votes, what)
		h.broadcast(r, mustJSON(map[string]string{"type": "voted", "what": what}))
		return
	}
	h.broadcast(r, mustJSON(map[string]any{"type": "votes", "what": what, "votes": n, "needed": need}))
}

// forgetVotes drops the votes of a member who left, when it was their last
// connection, and recounts: fewer members can mean a vote now passes.
func (h *partyHub) forgetVotes(r *partyRoom, user string) {
	for c := range r.conns {
		if c.user == user {
			return
		}
	}
	for what, v := range r.votes {
		if _, ok := v[user]; ok {
			delete(v, user)
		}
		if len(v) > 0 {
			h.tally(r, what)
		}
	}
}

func (h *partyHub) broadcast(r *partyRoom, msg []byte) {
	for c := range r.conns {
		h.sendTo(r, c, msg)
	}
}

// relay sends a reaction or chat line to everyone in party id, at most
// partyRelayRate a second. It reports whether anyone got it.
func (h *partyHub) relay(id string, msg []byte) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[id]
	if r == nil || len(r.conns) == 0 {
		return false
	}
	now := h.now()
	if now.Sub(r.relayAt) >= time.Second {
		r.relayAt, r.relayed = now, 0
	}
	if r.relayed >= partyRelayRate {
		return false
	}
	r.relayed++
	h.broadcast(r, msg)
	return true
}

// hostOn reports whether the host's app is connected to party id.
func (h *partyHub) hostOn(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.rooms[id]; r != nil {
		for c := range r.conns {
			if c.host {
				return true
			}
		}
	}
	return false
}

// The voice channel's chat.

// syncPartyIn is the synced party whose voice channel is channel, or "".
func (b *bot) syncPartyIn(channel string) string {
	id := ""
	b.store.view(func(d *storeData) {
		for k, p := range d.Parties {
			if p.Sync && p.Channel == channel {
				id = k
				return
			}
		}
	})
	return id
}

func (b *bot) partyChatMessage(m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot || m.GuildID != b.cfg.GuildID {
		return
	}
	id := b.syncPartyIn(m.ChannelID)
	if id == "" {
		return
	}
	text := partyChatText(m.Message)
	if text == "" {
		return
	}
	name := clip(b.dashName(m.Author.ID), partyNameMax)
	msg := map[string]string{"type": "chat", "name": name, "text": text}
	if emojiOnly(text) {
		msg = map[string]string{"type": "react", "name": name, "emoji": clip(text, partyEmojiMax)}
	}
	syncHub.relay(id, mustJSON(msg))
}

func (b *bot) partyChatReaction(r *discordgo.MessageReactionAdd) {
	if r.GuildID != b.cfg.GuildID || (b.s.State != nil && b.s.State.User != nil && r.UserID == b.s.State.User.ID) {
		return
	}
	id := b.syncPartyIn(r.ChannelID)
	if id == "" {
		return
	}
	emoji := r.Emoji.Name
	if r.Emoji.ID != "" {
		emoji = ":" + emoji + ":" // the players can't draw a custom one
	}
	if emoji = clip(cleanText(emoji, false), partyEmojiMax); emoji == "" {
		return
	}
	name := clip(b.dashName(r.UserID), partyNameMax)
	syncHub.relay(id, mustJSON(map[string]string{"type": "react", "name": name, "emoji": emoji}))
}

var (
	customEmojiRe = regexp.MustCompile(`<a?:(\w{1,32}):\d+>`)
	userMentionRe = regexp.MustCompile(`<@!?(\d+)>`)
	otherTokenRe  = regexp.MustCompile(`<(#|@&)\d+>`)
	emojiNameRe   = regexp.MustCompile(`:\w{1,32}:`)
)

// partyChatText is a message as the players show it: mentions as @name,
// custom emoji as :name:, one line, clipped. A message with only
// attachments says so.
func partyChatText(m *discordgo.Message) string {
	s := customEmojiRe.ReplaceAllString(m.Content, ":$1:")
	s = userMentionRe.ReplaceAllStringFunc(s, func(tok string) string {
		id := userMentionRe.FindStringSubmatch(tok)[1]
		for _, u := range m.Mentions {
			if u != nil && u.ID == id {
				if u.GlobalName != "" {
					return "@" + u.GlobalName
				}
				return "@" + u.Username
			}
		}
		return "@someone"
	})
	s = otherTokenRe.ReplaceAllStringFunc(s, func(tok string) string {
		if strings.HasPrefix(tok, "<#") {
			return "#channel"
		}
		return "@role"
	})
	s = clip(cleanText(s, false), partyChatMax)
	if s == "" && len(m.Attachments) > 0 {
		s = "sent an attachment"
	}
	return s
}

// emojiOnly reports whether s is nothing but emoji (custom ones already
// written as :name:) and spaces.
func emojiOnly(s string) bool {
	s = emojiNameRe.ReplaceAllString(s, "🙂")
	seen := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
		case unicode.Is(unicode.So, r), r >= 0x1F3FB && r <= 0x1F3FF:
			seen = true
		case r == 0x200D, r == 0xFE0F, r == 0x20E3, r >= 0xE0020 && r <= 0xE007F:
			// joiners, variation selectors, keycaps and tags inside an emoji
		default:
			return false
		}
	}
	return seen
}
