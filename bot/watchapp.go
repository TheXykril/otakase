package main

import "net/http"

// Linked apps join existing rooms through the Activity's watch protocol.
func watchAppRoute(r *http.Request) bool {
	switch r.Method + " " + r.URL.Path {
	case "GET /activity/api/watch/status", "GET /activity/api/watch/state", "GET /activity/api/watch/preferences",
		"POST /activity/api/watch/open", "POST /activity/api/watch/leave", "POST /activity/api/watch/beat",
		"POST /activity/api/watch/ask", "POST /activity/api/watch/ctl", "POST /activity/api/watch/ready",
		"POST /activity/api/watch/answer", "POST /activity/api/watch/sync", "POST /activity/api/watch/count":
		return true
	}
	return false
}

func (b *bot) watchAppInChannel(user, channel string) bool {
	if b.s == nil || b.s.State == nil || channel == "" {
		return false
	}
	state, err := b.s.State.VoiceState(b.cfg.GuildID, user)
	return err == nil && state.ChannelID == channel
}

// Discord voice state, rather than a channel sent by a phone, proves access.
func (b *bot) watchAppAccess(w http.ResponseWriter, r *http.Request, cur *watchRoom, user string) bool {
	if b.acts.user(r) != "" || r.URL.Path == "/activity/api/watch/leave" {
		return true
	}
	if cur == nil {
		apiError(w, http.StatusNotFound, "the watch room is over")
		return false
	}
	if cur.adult && !b.watchAppInChannel(user, cur.channel) {
		apiError(w, http.StatusForbidden, "That room is private. Join its voice channel to watch along.")
		return false
	}
	return true
}
