package main

import (
	"net/http"
	"regexp"
	"strings"
)

type watchLanguagePreference struct {
	Audio     string `json:"audio"`
	Subtitles string `json:"subtitles"`
}

var watchLanguageCode = regexp.MustCompile(`^[a-z]{2,3}(?:-[a-z0-9]{2,8}){0,2}$`)

func watchValidLanguage(value string, subtitles bool) bool {
	if value == "auto" || subtitles && value == "off" {
		return true
	}
	return watchLanguageCode.MatchString(value)
}

func (b *bot) watchLanguageSettings(w http.ResponseWriter, r *http.Request) {
	user, in, ok := b.watchRead(w, r)
	if !ok {
		return
	}
	b.watch.mu.Lock()
	cur := b.inRoom(w, in.Room, user)
	b.watch.mu.Unlock()
	if cur == nil {
		return
	}
	audio := strings.ToLower(strings.TrimSpace(in.Audio))
	subs := strings.ToLower(strings.TrimSpace(in.Subtitles))
	if !watchValidLanguage(audio, false) || !watchValidLanguage(subs, true) {
		apiError(w, http.StatusBadRequest, "invalid language preference")
		return
	}
	pref := watchLanguagePreference{Audio: audio, Subtitles: subs}
	b.store.update(func(d *storeData) {
		if d.WatchLanguages == nil {
			d.WatchLanguages = make(map[string]watchLanguagePreference)
		}
		d.WatchLanguages[user] = pref
	})
	writeJSON(w, http.StatusOK, pref)
}
