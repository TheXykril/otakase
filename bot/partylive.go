package main

import (
	"crypto/rand"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// Watch with me: while an episode plays, a linked app with Watch with me on
// asks for a party of its own and puts its join link on a button in the
// member's Discord status. It is a synced party like a /watchparty
// sync:True one, with no event, voice channel or post: it lives in memory,
// the member is its host, and it ends once the host's app has been gone for
// liveGrace or sends {"type":"end"}.
//
//	POST /v1/party/start {"anilist_id":154587,"episode":12,"title":"Frieren"} (auth)
//	  → {"id":"4821093371526630","join_url":"https://otakase.xyverion.com/open/?party=4821093371526630"}
//
// A member has one at a time: starting again returns the same party, with
// the show updated. Ids are 16 digits, so they never meet an event id.

const (
	liveGrace      = 2 * time.Minute // a host's app reconnecting keeps the party
	maxLiveParties = 200
)

// liveParty is a running Watch with me party.
type liveParty struct {
	ID        string
	Host      string
	Show      string
	AniListID int
	Episode   int
	HostSeen  time.Time // the host's app was last connected
}

var liveParties = struct {
	sync.Mutex
	byID   map[string]*liveParty
	byHost map[string]string
}{byID: map[string]*liveParty{}, byHost: map[string]string{}}

var livePartyLimit = newRateLimit(20, time.Hour)

func init() {
	apiHandle("POST /v1/party/start", (*bot).apiPartyStart)
	registerJob("Watch with me parties", (*bot).livePartyLoop)
}

func newLivePartyID() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(9e15))
	return n.Add(n, big.NewInt(1e15)).String()
}

// startLiveParty returns user's party, made now if they have none. ok is
// false when there are too many parties already.
func startLiveParty(user string, show appShow, now time.Time) (liveParty, bool) {
	liveParties.Lock()
	defer liveParties.Unlock()
	if id, ok := liveParties.byHost[user]; ok {
		p := liveParties.byID[id]
		p.Show, p.AniListID, p.Episode, p.HostSeen = show.Title, show.AniListID, show.Episode, now
		return *p, true
	}
	if len(liveParties.byID) >= maxLiveParties {
		return liveParty{}, false
	}
	p := &liveParty{ID: newLivePartyID(), Host: user, Show: show.Title, AniListID: show.AniListID, Episode: show.Episode, HostSeen: now}
	liveParties.byID[p.ID] = p
	liveParties.byHost[user] = p.ID
	return *p, true
}

// livePartyOf is the party user hosts with Watch with me, if any.
func livePartyOf(user string) (liveParty, bool) {
	liveParties.Lock()
	defer liveParties.Unlock()
	if id, ok := liveParties.byHost[user]; ok {
		return *liveParties.byID[id], true
	}
	return liveParty{}, false
}

// liveSyncParty is a Watch with me party as the hub sees a watch party.
func liveSyncParty(id string) (watchParty, bool) {
	liveParties.Lock()
	defer liveParties.Unlock()
	p, ok := liveParties.byID[id]
	if !ok {
		return watchParty{}, false
	}
	return watchParty{Show: p.Show, Host: p.Host, AniListID: p.AniListID, Sync: true}, true
}

// dropLiveParty forgets a Watch with me party; false when id isn't one.
func dropLiveParty(id string) bool {
	liveParties.Lock()
	defer liveParties.Unlock()
	p, ok := liveParties.byID[id]
	if !ok {
		return false
	}
	delete(liveParties.byID, id)
	if liveParties.byHost[p.Host] == id {
		delete(liveParties.byHost, p.Host)
	}
	return true
}

func liveIDs() []string {
	liveParties.Lock()
	defer liveParties.Unlock()
	ids := make([]string, 0, len(liveParties.byID))
	for id := range liveParties.byID {
		ids = append(ids, id)
	}
	return ids
}

// expireLiveParties notes which hosts are connected (hostOn, by party id)
// and drops the parties whose host has been gone for liveGrace. It returns
// the ids it dropped.
func expireLiveParties(hostOn map[string]bool, now time.Time) []string {
	var gone []string
	liveParties.Lock()
	for id, p := range liveParties.byID {
		if hostOn[id] {
			p.HostSeen = now
			continue
		}
		if now.Sub(p.HostSeen) > liveGrace {
			gone = append(gone, id)
		}
	}
	liveParties.Unlock()
	for _, id := range gone {
		dropLiveParty(id)
	}
	return gone
}

// livePartyLoop ends the parties whose host left. The hub is asked first,
// without the parties' lock: the hub takes that lock under its own.
func (b *bot) livePartyLoop() {
	for {
		time.Sleep(15 * time.Second)
		on := map[string]bool{}
		for _, id := range liveIDs() {
			on[id] = syncHub.hostOn(id)
		}
		for _, id := range expireLiveParties(on, time.Now()) {
			syncHub.end(id)
		}
	}
}

// apiPartyStart is POST /v1/party/start.
func (b *bot) apiPartyStart(w http.ResponseWriter, r *http.Request) {
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	if !livePartyLimit.allow(hashToken(bearerToken(r)), appNow()) {
		apiError(w, http.StatusTooManyRequests, "starting parties too often")
		return
	}
	var req appShow
	if !readJSON(w, r, 2048, &req) {
		return
	}
	show := cleanShow(&req)
	if show == nil || show.AniListID <= 0 || show.Episode <= 0 {
		apiError(w, http.StatusBadRequest, "anilist_id and episode are needed")
		return
	}
	p, ok := startLiveParty(user, *show, appNow())
	if !ok {
		apiError(w, http.StatusServiceUnavailable, "too many parties right now")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": p.ID, "join_url": partyJoinURL(p.ID)})
}
