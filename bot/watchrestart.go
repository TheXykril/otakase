package main

import (
	"encoding/json"
	"log"
	"time"
)

// Watch rooms across a restart: the bot keeps every open room in the
// store (on shutdown, and every watchSaveGap for a crash) and opens them
// again when it starts. Members' pages keep polling while the bot is away
// and find their room again; the episode loads anew and waits for everyone
// at the spot it was at. A room whose time ran out meanwhile is dropped.

const watchSaveGap = 30 * time.Second

// watchSaved is an open room as the store keeps it.
type watchSaved struct {
	ID         string             `json:"id"`
	Channel    string             `json:"channel"`
	Adult      bool               `json:"adult,omitempty"`
	Owner      string             `json:"owner"`
	Host       string             `json:"host"`
	Opened     time.Time          `json:"opened"`
	Started    time.Time          `json:"started,omitzero"`
	Spent      bool               `json:"spent,omitempty"`
	Perks      string             `json:"perks,omitempty"`
	Watched    bool               `json:"watched,omitempty"`
	Members    []watchSavedMember `json:"members"`
	Show       *watchShow         `json:"show,omitempty"`
	Ep         int                `json:"ep,omitempty"`
	Dub        bool               `json:"dub,omitempty"`
	Pos        float64            `json:"pos,omitempty"`
	AutoSkipOP bool               `json:"skip_op,omitempty"`
	AutoSkipED bool               `json:"skip_ed,omitempty"`
	Chat       []watchChat        `json:"chat,omitempty"`
	ChatN      int                `json:"chat_n,omitempty"`
	Episodes   int                `json:"episodes,omitempty"`
	Peak       int                `json:"peak,omitempty"`
	Ver        int                `json:"ver"`
}

type watchSavedMember struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Joined  time.Time `json:"joined"`
	Watched float64   `json:"watched,omitempty"`
	Counted bool      `json:"counted,omitempty"`
}

func init() {
	onStart(func(b *bot) { b.restoreWatchRooms() })
	registerJob("Watch room saves", func(b *bot) {
		for {
			time.Sleep(watchSaveGap)
			b.saveWatchRooms()
		}
	})
}

// saveWatchRooms writes the open rooms to the store when they changed
// since the last save.
func (b *bot) saveWatchRooms() {
	now := watchNow()
	b.watch.mu.Lock()
	saved := make([]watchSaved, 0, len(b.watch.rooms))
	for _, r := range b.watch.list() {
		saved = append(saved, r.saved(now))
	}
	b.watch.mu.Unlock()
	js, _ := json.Marshal(saved)
	if string(js) == b.watch.lastSave {
		return
	}
	b.watch.lastSave = string(js)
	b.store.update(func(d *storeData) {
		d.WatchOpen = nil
		if len(saved) > 0 {
			d.WatchOpen = saved
		}
	})
}

// saved is the room as the store keeps it. Hold the lock.
func (r *watchRoom) saved(now time.Time) watchSaved {
	s := watchSaved{ID: r.id, Channel: r.channel, Adult: r.adult, Owner: r.owner, Host: r.host, Opened: r.opened,
		Started: r.started, Spent: r.spent, Perks: r.perks, Watched: r.watched, Ep: r.ep, Dub: r.dub,
		AutoSkipOP: r.autoSkipOP, AutoSkipED: r.autoSkipED, Chat: r.chat, ChatN: r.chatN,
		Episodes: r.episodes, Peak: r.peak, Ver: r.ver}
	if r.show != nil && r.phase != "pick" {
		show := *r.show
		s.Show = &show
		s.Pos = r.startAt
		if r.phase == "play" {
			s.Pos = r.position(now)
			if r.hold != 0 {
				s.Pos = r.holdPos
			}
		}
	}
	for _, id := range r.people() {
		m := r.members[id]
		s.Members = append(s.Members, watchSavedMember{ID: id, Name: m.name, Joined: m.joined, Watched: m.watched, Counted: m.counted})
	}
	return s
}

// restoreWatchRooms opens the rooms the store kept, before the Activity
// API answers anyone.
func (b *bot) restoreWatchRooms() {
	var saved []watchSaved
	b.store.view(func(d *storeData) { saved = d.WatchOpen })
	now := watchNow()
	b.watch.mu.Lock()
	defer b.watch.mu.Unlock()
	for _, s := range saved {
		r := s.room(now)
		if e := r.ends(); !e.IsZero() && now.After(e) || len(r.members) == 0 {
			continue
		}
		b.watch.add(r)
		if s.Show != nil && s.Ep > 0 {
			b.loadEpisode(r, *s.Show, s.Ep, s.Dub, s.Pos)
		}
		r.note("The bot restarted. Picking up where you were…")
		r.changed()
		watchClock(b, r)
		log.Printf("watch room in channel %s back after a restart", r.channel)
	}
}

// room makes a saved room open again. Everyone in it counts as just seen,
// so they have watchGone to come back.
func (s watchSaved) room(now time.Time) *watchRoom {
	r := &watchRoom{id: s.ID, channel: s.Channel, adult: s.Adult, owner: s.Owner, host: s.Host, opened: s.Opened,
		started: s.Started, spent: s.Spent, perks: s.Perks, watched: s.Watched, phase: "pick",
		members: map[string]*watchMember{}, autoSkipOP: s.AutoSkipOP, autoSkipED: s.AutoSkipED,
		chat: s.Chat, chatN: s.ChatN, episodes: s.Episodes, peak: s.Peak,
		// Pages still hold the old version: anything else answers them.
		ver: s.Ver + 1}
	for _, m := range s.Members {
		r.members[m.ID] = &watchMember{name: m.Name, joined: m.Joined, seen: now, watched: m.Watched, counted: m.Counted}
	}
	return r
}
