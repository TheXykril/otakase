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

// These account-wide defaults apply only when the player uses Automatic.
// Subtitles applies to dubbed audio, SubSubtitles to the original Japanese
// audio, so a member can watch dubs without subtitles and subs with them.
type watchPlaybackPreferences struct {
	Audio        string `json:"audio"`
	BackupAudio  string `json:"backup_audio"`
	Subtitles    string `json:"subtitles"`
	SubSubtitles string `json:"sub_subtitles"`
}

func watchDefaultPreferences() watchPlaybackPreferences {
	return watchPlaybackPreferences{Audio: "auto", BackupAudio: "auto", Subtitles: "auto", SubSubtitles: "auto"}
}

func watchPreferredLanguage(value string, subtitles bool) bool {
	// Container track indices are episode-specific and must not become
	// account-wide preferences.
	if value == "off" {
		return subtitles
	}
	return value == "auto" || watchLanguageCode.MatchString(value)
}

func watchNormalizePreferences(p watchPlaybackPreferences) watchPlaybackPreferences {
	normal := watchDefaultPreferences()
	if watchPreferredLanguage(p.Audio, false) {
		normal.Audio = p.Audio
	}
	if watchPreferredLanguage(p.BackupAudio, false) {
		normal.BackupAudio = p.BackupAudio
	}
	if watchPreferredLanguage(p.Subtitles, true) {
		normal.Subtitles = p.Subtitles
	}
	if watchPreferredLanguage(p.SubSubtitles, true) {
		normal.SubSubtitles = p.SubSubtitles
	}
	if normal.Audio == "auto" || normal.Audio == normal.BackupAudio {
		normal.BackupAudio = "auto"
	}
	return normal
}

func (b *bot) watchPreferencesGet(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	var prefs watchPlaybackPreferences
	b.store.view(func(d *storeData) { prefs = watchNormalizePreferences(d.WatchPreferences[user]) })
	writeJSON(w, http.StatusOK, prefs)
}

func (b *bot) watchPreferencesSet(w http.ResponseWriter, r *http.Request) {
	user := b.actUser(w, r)
	if user == "" {
		return
	}
	if !watchActLimit.allow(user, watchNow()) {
		apiError(w, http.StatusTooManyRequests, "slow down")
		return
	}
	var p watchPlaybackPreferences
	if !readJSON(w, r, 4<<10, &p) {
		return
	}
	p.Audio = strings.ToLower(strings.TrimSpace(p.Audio))
	p.BackupAudio = strings.ToLower(strings.TrimSpace(p.BackupAudio))
	p.Subtitles = strings.ToLower(strings.TrimSpace(p.Subtitles))
	p.SubSubtitles = strings.ToLower(strings.TrimSpace(p.SubSubtitles))
	if p.SubSubtitles == "" {
		// Pages loaded before the setting existed don't send it.
		p.SubSubtitles = "auto"
	}
	if !watchPreferredLanguage(p.Audio, false) ||
		!watchPreferredLanguage(p.BackupAudio, false) ||
		!watchPreferredLanguage(p.Subtitles, true) ||
		!watchPreferredLanguage(p.SubSubtitles, true) {
		apiError(w, http.StatusBadRequest, "invalid language preference")
		return
	}
	p = watchNormalizePreferences(p)
	b.store.update(func(d *storeData) {
		if d.WatchPreferences == nil {
			d.WatchPreferences = make(map[string]watchPlaybackPreferences)
		}
		d.WatchPreferences[user] = p
		// Choosing account-wide defaults selects Automatic in the player.
		// A later manual player selection still overrides these defaults.
		if d.WatchLanguages == nil {
			d.WatchLanguages = make(map[string]watchLanguagePreference)
		}
		d.WatchLanguages[user] = watchLanguagePreference{Audio: "auto", Subtitles: "auto"}
	})
	writeJSON(w, http.StatusOK, p)
}

var watchLanguageCode = regexp.MustCompile(`^[a-z]{2,3}(?:-[a-z0-9]{2,8}){0,2}$`)

func watchValidLanguage(value string, subtitles bool) bool {
	if value == "off" {
		return subtitles
	}
	if value == "auto" {
		return true
	}
	// Unlabelled container and HLS tracks are selected by their bounded index.
	if !subtitles && (strings.HasPrefix(value, "track-") || strings.HasPrefix(value, "hls-")) {
		i := strings.IndexByte(value, '-')
		n := value[i+1:]
		if len(n) == 0 || len(n) > 3 {
			return false
		}
		for _, ch := range n {
			if ch < '0' || ch > '9' {
				return false
			}
		}
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
