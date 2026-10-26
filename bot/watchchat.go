package main

import (
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"
)

// The watch room's chat: short text messages beside the video for the
// people in the room. Nothing is stored; the chat goes with the room. Text
// between || marks is a spoiler the Activity blurs until it is tapped. The
// author, the host and staff can take a message down.

const (
	watchChatKeep  = 60  // messages the room keeps and shows
	watchChatRunes = 300 // longest message
)

var watchChatLimit = newRateLimit(10, 20*time.Second)

type watchChat struct {
	ID   int    `json:"id"`
	User string `json:"user"`
	Name string `json:"name"`
	Text string `json:"text"`
	At   int64  `json:"at"` // unix ms
}

// watchChatText tidies a message: one line, no control characters, at most
// watchChatRunes. "" means there is nothing to send.
func watchChatText(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case unicode.IsControl(r) || r == '​' || r == '‮' || r == '‭':
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > watchChatRunes {
		s = strings.TrimSpace(string(r[:watchChatRunes]))
	}
	return s
}

// watchChatSay posts a message, or takes one down when in.Act is "delete".
func (b *bot) watchChatSay(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.watchRead(w, r)
	if !ok {
		return
	}
	staff := b.isAdmin(user)
	text := watchChatText(in.Text)
	if in.Act != "delete" {
		switch {
		case text == "":
			apiError(w, http.StatusBadRequest, "write something first")
			return
		case capsuleInviteRe.MatchString(text):
			apiError(w, http.StatusBadRequest, "no server invites in the room chat")
			return
		case !watchChatLimit.allow(user, watchNow()):
			apiError(w, http.StatusTooManyRequests, "you're sending messages too fast")
			return
		}
	}
	b.watch.mu.Lock()
	cur := b.inRoom(w, in.Room, user)
	if cur == nil {
		b.watch.mu.Unlock()
		return
	}
	if in.Act == "delete" {
		k := slices.IndexFunc(cur.chat, func(c watchChat) bool { return c.ID == in.ID })
		switch {
		case k < 0:
			b.watch.mu.Unlock()
			apiError(w, http.StatusNotFound, "that message is gone")
			return
		case cur.chat[k].User != user && cur.host != user && !staff:
			b.watch.mu.Unlock()
			apiError(w, http.StatusForbidden, "only the host or staff can remove other people's messages")
			return
		}
		cur.chat = slices.Delete(cur.chat, k, k+1)
	} else {
		cur.chatN++
		cur.chat = append(cur.chat, watchChat{ID: cur.chatN, User: user, Name: cur.members[user].name, Text: text, At: watchNow().UnixMilli()})
		if len(cur.chat) > watchChatKeep {
			cur.chat = slices.Delete(cur.chat, 0, len(cur.chat)-watchChatKeep)
		}
	}
	cur.changed()
	b.watchAnswerView(w, cur, user, staff)
}
