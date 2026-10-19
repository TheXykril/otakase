package main

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Members' profile pictures for the Activity's people (Play together, the
// daily friends panel). The Activity can only load from its own address,
// so the bot fetches the picture from Discord and passes it on, keeping
// it for an hour. Only server members' pictures are served.

const avatarTTL = time.Hour

type avatarHit struct {
	data []byte
	typ  string
	at   time.Time
}

var avatarCache = struct {
	sync.Mutex
	m map[string]avatarHit
}{m: map[string]avatarHit{}}

func init() {
	apiHandle("GET /activity/api/avatar/{id}", (*bot).activityAvatar)
}

// avatarURL is a member's picture on Discord's CDN, or "" when they aren't
// on the server.
func (b *bot) avatarURL(id string) string {
	if b.s == nil {
		return ""
	}
	if b.s.State != nil {
		if m, err := b.s.State.Member(b.cfg.GuildID, id); err == nil && m.User != nil {
			return m.AvatarURL("128")
		}
	}
	m, err := b.s.GuildMember(b.cfg.GuildID, id)
	if err != nil || m.User == nil {
		return ""
	}
	if m.GuildID == "" {
		m.GuildID = b.cfg.GuildID
	}
	if b.s.State != nil {
		_ = b.s.State.MemberAdd(m)
	}
	return m.AvatarURL("128")
}

func (b *bot) activityAvatar(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if len(id) < 5 || len(id) > 22 || strings.Trim(id, "0123456789") != "" {
		http.NotFound(w, r)
		return
	}
	avatarCache.Lock()
	hit, ok := avatarCache.m[id]
	avatarCache.Unlock()
	if !ok || time.Since(hit.at) > avatarTTL {
		hit = avatarHit{at: time.Now()}
		if u := b.avatarURL(id); strings.HasPrefix(u, "https://cdn.discordapp.com/") {
			if resp, err := activityClient.Get(u); err == nil {
				if resp.StatusCode == http.StatusOK {
					hit.data, _ = io.ReadAll(io.LimitReader(resp.Body, 2<<20))
					hit.typ = resp.Header.Get("Content-Type")
				}
				resp.Body.Close()
			}
		}
		avatarCache.Lock()
		if len(avatarCache.m) > 500 {
			for k, v := range avatarCache.m {
				if time.Since(v.at) > avatarTTL {
					delete(avatarCache.m, k)
				}
			}
		}
		avatarCache.m[id] = hit
		avatarCache.Unlock()
	}
	if len(hit.data) == 0 {
		http.NotFound(w, r)
		return
	}
	if !strings.HasPrefix(hit.typ, "image/") {
		hit.typ = "image/png"
	}
	w.Header().Set("Content-Type", hit.typ)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(hit.data)
}
