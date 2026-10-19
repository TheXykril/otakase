package main

import (
	"fmt"
	"net/http"
	"slices"
	"sort"
	"time"
)

// Watch parties in the app: the Community menu lists the planned parties,
// lets the member say they're in, and joins a synced one. Going members'
// apps also get a reminder shortly before the start, beside the DM.
//
//	GET  /v1/parties (auth) → {"parties":[{"id":"123","title":"Frieren","start":"2026-10-14T19:00:00Z",
//	       "anilist_id":154587,"sync":true,"host":"Alex","going":3,"rsvp":true,"live":false,
//	       "join_url":"https://otakase.xyverion.com/open/?party=123","event_url":"https://discord.com/events/…"}]}
//	POST /v1/party/{id}/rsvp {"going":true} (auth) → {"going":true,"count":4}
//
// join_url is only there for synced parties. live: it has started, or the
// host's app is already in the party. The reminder is an inbox "notice"
// whose url joins a synced party (else opens the event).

var (
	partiesLimit = newRateLimit(30, time.Minute)
	rsvpLimit    = newRateLimit(20, time.Minute)
)

func init() {
	apiHandle("GET /v1/parties", (*bot).apiParties)
	apiHandle("POST /v1/party/{id}/rsvp", (*bot).apiPartyRSVP)
	registerJob("Watch party app reminders", (*bot).partyAppReminderLoop)
}

// appParty is a planned party as the app lists it.
type appParty struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Start     time.Time `json:"start"`
	AniListID int       `json:"anilist_id,omitempty"`
	Sync      bool      `json:"sync"`
	Host      string    `json:"host"`
	Going     int       `json:"going"`
	RSVP      bool      `json:"rsvp"`
	Live      bool      `json:"live"`
	JoinURL   string    `json:"join_url,omitempty"`
	EventURL  string    `json:"event_url,omitempty"`
}

// appPartyList is every planned party for user, soonest first. name gives
// the host's display name and live whether a party's host is connected.
func appPartyList(parties map[string]watchParty, user string, now time.Time, name func(string) string, hostOn func(string) bool) []appParty {
	list := []appParty{}
	for id, p := range parties {
		a := appParty{ID: id, Title: p.Show, Start: p.Start, AniListID: p.AniListID, Sync: p.Sync, Host: name(p.Host),
			Going: len(p.Going), RSVP: slices.Contains(p.Going, user), EventURL: p.Link}
		a.Live = !now.Before(p.Start) || (p.Sync && hostOn(id))
		if p.Sync {
			a.JoinURL = partyJoinURL(id)
		}
		list = append(list, a)
	}
	sort.Slice(list, func(i, j int) bool {
		if !list[i].Start.Equal(list[j].Start) {
			return list[i].Start.Before(list[j].Start)
		}
		return list[i].ID < list[j].ID
	})
	return list
}

func (b *bot) apiParties(w http.ResponseWriter, r *http.Request) {
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	if !partiesLimit.allow(hashToken(bearerToken(r)), appNow()) {
		apiError(w, http.StatusTooManyRequests, "asking too often")
		return
	}
	parties := map[string]watchParty{}
	b.store.view(func(d *storeData) {
		for id, p := range d.Parties {
			parties[id] = p
		}
	})
	writeJSON(w, http.StatusOK, map[string]any{"parties": appPartyList(parties, user, appNow(), b.dashName, syncHub.hostOn)})
}

// setRSVP puts user on the going list or takes them off it, whatever they
// were before.
func setRSVP(p *watchParty, user string, going bool) {
	if slices.Contains(p.Going, user) == going {
		return
	}
	if going {
		toggleRSVP(p, user, true)
		return
	}
	p.Going = slices.DeleteFunc(p.Going, func(id string) bool { return id == user })
}

func (b *bot) apiPartyRSVP(w http.ResponseWriter, r *http.Request) {
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	if !rsvpLimit.allow(hashToken(bearerToken(r)), appNow()) {
		apiError(w, http.StatusTooManyRequests, "too many answers")
		return
	}
	var req struct {
		Going bool `json:"going"`
	}
	if !readJSON(w, r, 512, &req) {
		return
	}
	id := r.PathValue("id")
	var p watchParty
	var ok bool
	b.store.update(func(d *storeData) {
		if p, ok = d.Parties[id]; ok {
			setRSVP(&p, user, req.Going)
			d.Parties[id] = p
		}
	})
	if !ok {
		apiError(w, http.StatusNotFound, "this watch party is already over or cancelled")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"going": slices.Contains(p.Going, user), "count": len(p.Going)})
}

// The reminder.

// partyAppReminder is one reminder to send.
type partyAppReminder struct {
	ID string
	P  watchParty
}

// dueAppReminders marks and returns the parties whose going members' apps
// get their reminder now: at the same time as the DM, and not at all after
// a long outage.
func dueAppReminders(d *storeData, now time.Time) []partyAppReminder {
	var due []partyAppReminder
	for id, p := range d.Parties {
		if p.AppReminded || now.Before(partyReminderAt(p.Created, p.Start)) {
			continue
		}
		p.AppReminded = true
		d.Parties[id] = p
		if now.Sub(p.Start) < partyNoShowWait {
			due = append(due, partyAppReminder{id, p})
		}
	}
	return due
}

// partyReminderItem is the inbox notice for a party starting at start.
func partyReminderItem(id string, p watchParty, now time.Time) inboxItem {
	it := inboxItem{Kind: "notice", Title: "Watch party starting soon", AniListID: p.AniListID,
		URL: p.Link, Expires: p.Start.Add(time.Hour)}
	mins := int(p.Start.Sub(now).Round(time.Minute) / time.Minute)
	switch {
	case mins >= 1:
		it.Text = fmt.Sprintf("%s starts in %s", clip(p.Show, 100), dashPlural(mins, "minute"))
	default:
		it.Title = "Watch party starting now"
		it.Text = clip(p.Show, 100) + " is starting"
	}
	if p.Sync {
		it.URL = partyJoinURL(id)
		it.Text += ". Open this to join in Otakase."
	} else {
		it.Text += ". Open this for the event."
	}
	return it
}

func (b *bot) partyAppReminderLoop() {
	for {
		time.Sleep(20 * time.Second)
		now := appNow()
		waiting := false
		b.store.view(func(d *storeData) {
			for _, p := range d.Parties {
				waiting = waiting || (!p.AppReminded && !now.Before(partyReminderAt(p.Created, p.Start)))
			}
		})
		if !waiting {
			continue // no need to write the store
		}
		var due []partyAppReminder
		b.store.update(func(d *storeData) { due = dueAppReminders(d, now) })
		for _, x := range due {
			it := partyReminderItem(x.ID, x.P, now)
			for _, user := range x.P.Going {
				b.sendToApps(user, it)
			}
		}
	}
}
