package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// The app's inbox: things the bot sends to a member's linked apps. Until now
// the link only went app → bot; the inbox is the way back. Every linked app
// of the member gets its own copy, kept until that app takes it.
//
//	GET  /v1/inbox?wait=25 (auth)        → {"items":[…]}, held open up to wait seconds (max 25) while empty
//	POST /v1/inbox/ack {"ids":[…]} (auth) → {} and those items are gone
//
// The heartbeat's answer also carries "inbox": n, the number waiting.
// Item kinds are the app's to understand; one it doesn't know it acks and
// drops. See wiki/Developer-App-Link.md for the kinds.

const (
	maxInboxItems = 50
	inboxKeep     = 30 * 24 * time.Hour
	maxInboxWait  = 25 * time.Second
)

// inboxItem is one message for an app.
type inboxItem struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	At        time.Time `json:"at"`
	Expires   time.Time `json:"expires,omitzero"`
	Title     string    `json:"title,omitempty"`
	Text      string    `json:"text,omitempty"`
	URL       string    `json:"url,omitempty"`
	AniListID int       `json:"anilist_id,omitempty"`
	Episode   int       `json:"episode,omitempty"`
	From      string    `json:"from,omitempty"` // display name of the member who sent it
	Key       string    `json:"key,omitempty"`  // a setting's name, a remote action…
	Value     string    `json:"value,omitempty"`
}

var (
	inboxLimit    = newRateLimit(30, time.Minute)
	inboxAckLimit = newRateLimit(30, time.Minute)
)

// inboxWaiters wakes the long polls of an app (by token hash) when
// something arrives for it.
var inboxWaiters = struct {
	sync.Mutex
	m map[string][]chan struct{}
}{m: map[string][]chan struct{}{}}

func init() {
	apiHandle("GET /v1/inbox", (*bot).apiInbox)
	apiHandle("POST /v1/inbox/ack", (*bot).apiInboxAck)
	registerComponent("app-queue:", (*bot).queueButton)
	registerDMComponent("app-queue:", (*bot).queueButton)
}

// sendToApps puts an item in the inbox of every linked app of a member and
// returns how many apps that is. Expires, when zero, is inboxKeep from now.
func (b *bot) sendToApps(user string, it inboxItem) int {
	now := appNow()
	if it.At.IsZero() {
		it.At = now
	}
	if it.Expires.IsZero() {
		it.Expires = now.Add(inboxKeep)
	}
	var hashes []string
	b.store.touch(func(d *storeData) {
		if d.AppInbox == nil {
			d.AppInbox = map[string][]inboxItem{}
		}
		for h, l := range d.AppLinks {
			if l.User != user {
				continue
			}
			x := it
			x.ID = newToken(8)
			box := pruneInbox(d.AppInbox[h], now)
			if len(box) >= maxInboxItems {
				box = box[len(box)-maxInboxItems+1:]
			}
			d.AppInbox[h] = append(box, x)
			hashes = append(hashes, h)
		}
	})
	inboxWaiters.Lock()
	for _, h := range hashes {
		for _, c := range inboxWaiters.m[h] {
			select {
			case c <- struct{}{}:
			default:
			}
		}
	}
	inboxWaiters.Unlock()
	return len(hashes)
}

// pruneInbox drops expired items.
func pruneInbox(box []inboxItem, now time.Time) []inboxItem {
	kept := box[:0:0]
	for _, it := range box {
		if it.Expires.IsZero() || now.Before(it.Expires) {
			kept = append(kept, it)
		}
	}
	return kept
}

// inboxOf is what waits for an app, dropping what expired.
func (b *bot) inboxOf(hash string) []inboxItem {
	now := appNow()
	var out []inboxItem
	b.store.touch(func(d *storeData) {
		box := pruneInbox(d.AppInbox[hash], now)
		if len(box) == 0 {
			delete(d.AppInbox, hash)
		} else {
			d.AppInbox[hash] = box
		}
		out = append(out, box...)
	})
	return out
}

// inboxCount is the number of items waiting for an app.
func (b *bot) inboxCount(hash string) int {
	n := 0
	now := appNow()
	b.store.view(func(d *storeData) { n = len(pruneInbox(d.AppInbox[hash], now)) })
	return n
}

func (b *bot) apiInbox(w http.ResponseWriter, r *http.Request) {
	if b.requireApp(w, r) == "" {
		return
	}
	h := hashToken(bearerToken(r))
	if !inboxLimit.allow(h, appNow()) {
		apiError(w, http.StatusTooManyRequests, "asking too often")
		return
	}
	wait := 0 * time.Second
	if s, err := strconv.Atoi(r.URL.Query().Get("wait")); err == nil && s > 0 {
		wait = min(time.Duration(s)*time.Second, maxInboxWait)
	}
	items := b.inboxOf(h)
	if len(items) == 0 && wait > 0 {
		c := make(chan struct{}, 1)
		inboxWaiters.Lock()
		inboxWaiters.m[h] = append(inboxWaiters.m[h], c)
		inboxWaiters.Unlock()
		t := time.NewTimer(wait)
		select {
		case <-c:
		case <-t.C:
		case <-r.Context().Done():
		}
		t.Stop()
		inboxWaiters.Lock()
		list := inboxWaiters.m[h]
		for k, x := range list {
			if x == c {
				list = append(list[:k], list[k+1:]...)
				break
			}
		}
		if len(list) == 0 {
			delete(inboxWaiters.m, h)
		} else {
			inboxWaiters.m[h] = list
		}
		inboxWaiters.Unlock()
		items = b.inboxOf(h)
	}
	if items == nil {
		items = []inboxItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (b *bot) apiInboxAck(w http.ResponseWriter, r *http.Request) {
	if b.requireApp(w, r) == "" {
		return
	}
	h := hashToken(bearerToken(r))
	if !inboxAckLimit.allow(h, appNow()) {
		apiError(w, http.StatusTooManyRequests, "too many acks")
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if !readJSON(w, r, 8192, &req) {
		return
	}
	drop := map[string]bool{}
	for _, id := range req.IDs {
		drop[id] = true
	}
	b.store.touch(func(d *storeData) {
		box := d.AppInbox[h]
		kept := box[:0:0]
		for _, it := range box {
			if !drop[it.ID] {
				kept = append(kept, it)
			}
		}
		if len(kept) == 0 {
			delete(d.AppInbox, h)
		} else {
			d.AppInbox[h] = kept
		}
	})
	writeJSON(w, http.StatusOK, struct{}{})
}

// hasApp reports whether a member has a linked app.
func (b *bot) hasApp(user string) bool {
	found := false
	b.store.view(func(d *storeData) {
		for _, l := range d.AppLinks {
			if l.User == user {
				found = true
				return
			}
		}
	})
	return found
}

// Queue in Otakase.

// queueButtonFor is the Queue in Otakase button for a show, or nil.
func queueButtonFor(anilistID int) discordgo.MessageComponent {
	if anilistID <= 0 {
		return nil
	}
	return discordgo.Button{Label: "Queue in Otakase", Style: discordgo.SecondaryButton, CustomID: "app-queue:" + strconv.Itoa(anilistID)}
}

// withQueueButton adds Queue in Otakase to the last row of buttons, or in a
// row of its own when that one is full.
func withQueueButton(rows []discordgo.MessageComponent, anilistID int) []discordgo.MessageComponent {
	q := queueButtonFor(anilistID)
	if q == nil {
		return rows
	}
	if n := len(rows); n > 0 {
		if last, ok := rows[n-1].(discordgo.ActionsRow); ok && len(last.Components) < 5 {
			last.Components = append(last.Components, q)
			rows[n-1] = last
			return rows
		}
	}
	if len(rows) >= 5 {
		return rows
	}
	return append(rows, discordgo.ActionsRow{Components: []discordgo.MessageComponent{q}})
}

// queueButton sends a show to the presser's linked apps: it shows up under
// Community → From Discord, ready to play, the next time the app looks.
func (b *bot) queueButton(i *discordgo.InteractionCreate, arg string) {
	u := interactionUser(i)
	id, err := strconv.Atoi(arg)
	if u == nil || err != nil || id <= 0 {
		return
	}
	if !b.hasApp(u.ID) {
		b.respond(i, true, errorEmbed("Link the Otakase app first: run `otakase -discord-link` (or Community → Link to Discord in the app), then press this again."))
		return
	}
	info, ok := appShowInfo(id)
	title := ""
	if ok {
		if title = info.Title.English; title == "" {
			title = info.Title.Romaji
		}
	}
	if ok && info.IsAdult {
		b.respond(i, true, errorEmbed("That one can't be queued from here."))
		return
	}
	if title == "" {
		title = fmt.Sprintf("AniList %d", id)
	}
	n := b.sendToApps(u.ID, inboxItem{Kind: "queue", AniListID: id, Title: clip(title, 100), URL: playURL(id, 0)})
	log.Printf("queued %d for %s on %d apps", id, u.ID, n)
	b.respond(i, true, &discordgo.MessageEmbed{Color: shu, Description: fmt.Sprintf("Queued **%s** in Otakase (%s). Find it under **Community → From Discord**.",
		mdEscape(title), dashPlural(n, "app"))})
}
