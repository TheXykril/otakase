package main

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Watch room invites: the host finds a member in the Activity and the bot
// sends them a direct message with what is playing and a link to the call.
// Nothing is given away by it — they still have to be able to join the
// voice channel — so an invite to an 18+ room also lets them into the
// private voice room, the way `/room-invite` does.

const watchInviteMax = 12 // members an invite search answers with

var watchInviteLimit = newRateLimit(10, 10*time.Minute)

type watchInvitee struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func init() {
	apiHandle("GET /activity/api/watch/people", (*bot).watchPeopleAPI)
	apiHandle("POST /activity/api/watch/invite", (*bot).watchInvite)
}

// watchPeople is the server members whose name matches q and who aren't in
// the room already, the shortest names first.
func (b *bot) watchPeople(q string, inRoom map[string]*watchMember) []watchInvitee {
	out := []watchInvitee{}
	if b.s == nil || b.s.State == nil {
		return out
	}
	g, err := b.s.State.Guild(b.cfg.GuildID)
	if err != nil {
		return out
	}
	q = strings.ToLower(strings.TrimSpace(q))
	for _, m := range g.Members {
		if m == nil || m.User == nil || m.User.Bot {
			continue
		}
		if _, here := inRoom[m.User.ID]; here {
			continue
		}
		name := displayName(m)
		if q != "" && !strings.Contains(strings.ToLower(name), q) && !strings.Contains(strings.ToLower(m.User.Username), q) {
			continue
		}
		out = append(out, watchInvitee{ID: m.User.ID, Name: name})
	}
	sort.Slice(out, func(a, c int) bool {
		if len(out[a].Name) != len(out[c].Name) {
			return len(out[a].Name) < len(out[c].Name)
		}
		return out[a].Name < out[c].Name
	})
	if len(out) > watchInviteMax {
		out = out[:watchInviteMax]
	}
	return out
}

func (b *bot) watchPeopleAPI(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	b.watch.mu.Lock()
	cur := b.watch.room(r.URL.Query().Get("room"))
	if cur == nil {
		b.watch.mu.Unlock()
		apiError(w, http.StatusNotFound, "the watch room is over")
		return
	}
	if _, in := cur.members[user]; !in {
		b.watch.mu.Unlock()
		apiError(w, http.StatusForbidden, "you're not in the watch room")
		return
	}
	people := b.watchPeople(r.URL.Query().Get("q"), cur.members)
	b.watch.mu.Unlock()
	writeJSON(w, http.StatusOK, people)
}

// watchInvite sends one member a direct message about the room.
func (b *bot) watchInvite(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.watchRead(w, r)
	if !ok {
		return
	}
	who := strings.TrimSpace(in.User)
	if who == "" || who == user || len(who) > 22 || strings.Trim(who, "0123456789") != "" {
		apiError(w, http.StatusBadRequest, "pick someone to invite")
		return
	}
	b.watch.mu.Lock()
	cur := b.inRoom(w, in.Room, user)
	if cur == nil {
		b.watch.mu.Unlock()
		return
	}
	if cur.host != user && !b.isAdmin(user) {
		b.watch.mu.Unlock()
		apiError(w, http.StatusForbidden, "only the host invites people")
		return
	}
	if _, here := cur.members[who]; here {
		b.watch.mu.Unlock()
		apiError(w, http.StatusBadRequest, "they're already watching")
		return
	}
	if !watchInviteLimit.allow(user, watchNow()) {
		b.watch.mu.Unlock()
		apiError(w, http.StatusTooManyRequests, "that's enough invites for now")
		return
	}
	channel, adult, show, ep := cur.channel, cur.adult, "", cur.ep
	if cur.show != nil {
		show = cur.show.Title
	}
	staff := b.isAdmin(user)
	name := cur.members[user].name
	b.watch.mu.Unlock()
	if err := b.sendWatchInvite(who, name, channel, show, ep, adult); err != nil {
		log.Printf("watch room invite: %v", err)
		apiError(w, http.StatusBadGateway, "couldn't send them a message. They may have direct messages off.")
		return
	}
	b.watch.mu.Lock()
	cur = b.inRoom(w, in.Room, user)
	if cur == nil {
		b.watch.mu.Unlock()
		return
	}
	b.watchAnswerView(w, cur, user, staff)
}

// sendWatchInvite is the direct message itself. For an 18+ room it also
// lets them into the private voice channel, like /room-invite.
func (b *bot) sendWatchInvite(who, from, channel, show string, ep int, adult bool) error {
	if b.s == nil {
		return fmt.Errorf("no discord session")
	}
	if adult && channel != "" {
		if err := b.inviteToRoom(channel, who); err != nil {
			return err
		}
	}
	ch, err := b.s.UserChannelCreate(who)
	if err != nil {
		return err
	}
	playing := "They're about to start something."
	if show != "" {
		playing = fmt.Sprintf("They're watching **%s**, episode %d.", show, ep)
	}
	link := ""
	if channel != "" && b.cfg.GuildID != "" {
		link = fmt.Sprintf("\n\nJoin them in <#%s>: https://discord.com/channels/%s/%s", channel, b.cfg.GuildID, channel)
	}
	_, err = b.s.ChannelMessageSendEmbed(ch.ID, &discordgo.MessageEmbed{Color: shu,
		Title:       "Watch Together",
		Description: fmt.Sprintf("**%s** invited you to their watch room. %s%s", from, playing, link),
		Footer:      &discordgo.MessageEmbedFooter{Text: "Open the Activity in that call to watch along."}})
	return err
}
