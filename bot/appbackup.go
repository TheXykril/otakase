package main

import (
	"net/http"
	"time"
)

// Old settings backups. Apps used to keep a copy of their config with the
// bot; the Otakase account syncs settings now. The app takes a member's old
// backup into their account once and deletes it here. Nothing new is saved.
//
//	GET    /v1/backup (auth) → {"config","at","from_os","from_tv"}, 404 when none
//	DELETE /v1/backup (auth) → {}
//
// Both go one release after accounts ship, with the backups left in the
// data file.

var backupLimit = newRateLimit(10, time.Hour)

// appBackup is a member's saved config.
type appBackup struct {
	Config  string    `json:"config"`
	At      time.Time `json:"at"`
	FromOS  string    `json:"from_os,omitempty"`
	FromTV  bool      `json:"from_tv,omitempty"`
	Version string    `json:"version,omitempty"`
}

func init() {
	apiHandle("GET /v1/backup", (*bot).apiBackupGet)
	apiHandle("DELETE /v1/backup", (*bot).apiBackupDelete)
}

func (b *bot) apiBackupGet(w http.ResponseWriter, r *http.Request) {
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	if !backupLimit.allow(user, appNow()) {
		apiError(w, http.StatusTooManyRequests, "too many requests, try again later")
		return
	}
	bk, ok := b.memberBackup(user)
	if !ok {
		apiError(w, http.StatusNotFound, "no settings backup")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": bk.Config, "at": bk.At, "from_os": bk.FromOS, "from_tv": bk.FromTV})
}

func (b *bot) apiBackupDelete(w http.ResponseWriter, r *http.Request) {
	user := b.requireApp(w, r)
	if user == "" {
		return
	}
	b.store.update(func(d *storeData) { delete(d.AppBackups, user) })
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (b *bot) memberBackup(user string) (bk appBackup, ok bool) {
	b.store.view(func(d *storeData) { bk, ok = d.AppBackups[user] })
	return bk, ok
}
