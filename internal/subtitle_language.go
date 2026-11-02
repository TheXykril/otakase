package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/thexykril/otakase/internal/providers"
)

// SubtitleTrack is one external subtitle file a provider offers.
type SubtitleTrack = providers.SubtitleTrack

// showPrefsFileName holds choices remembered per show, keyed by AniList id.
// A file of its own rather than more columns in the history CSV, which other
// tools read and which has no room for optional fields.
const showPrefsFileName = "show_prefs.json"

// ShowPrefs is what otakase remembers about how one show is watched.
type ShowPrefs struct {
	// SubtitleLanguage is the canonical language name ("english",
	// "portuguese") the viewer last picked in the player for this show.
	SubtitleLanguage string `json:"subtitleLanguage,omitempty"`
	// Quality overrides the Quality setting for this show: "best" or a
	// number of lines. Empty follows the setting.
	Quality string `json:"quality,omitempty"`
	// AudioMode is "sub" or "dub", whichever the viewer last switched this
	// show to.
	AudioMode string `json:"audioMode,omitempty"`
}

var showPrefsMu sync.Mutex

func showPrefsPath(storagePath string) string {
	return filepath.Join(os.ExpandEnv(storagePath), showPrefsFileName)
}

// loadShowPrefs reads every show's preferences. A missing or unreadable file
// is an empty set: preferences are a convenience, never a reason to fail.
func loadShowPrefs(storagePath string) map[string]ShowPrefs {
	prefs := map[string]ShowPrefs{}
	data, err := os.ReadFile(showPrefsPath(storagePath))
	if err != nil {
		return prefs
	}
	if err := json.Unmarshal(data, &prefs); err != nil {
		Log(fmt.Sprintf("show prefs: ignoring unreadable %s: %v", showPrefsFileName, err))
		return map[string]ShowPrefs{}
	}
	return prefs
}

// updateShowPrefs applies change to one show's preferences and writes the file.
func updateShowPrefs(storagePath string, anilistID int, change func(*ShowPrefs)) error {
	if anilistID <= 0 || strings.TrimSpace(storagePath) == "" {
		return nil
	}
	showPrefsMu.Lock()
	defer showPrefsMu.Unlock()

	prefs := loadShowPrefs(storagePath)
	key := strconv.Itoa(anilistID)
	entry := prefs[key]
	change(&entry)
	if entry == (ShowPrefs{}) {
		delete(prefs, key)
	} else {
		prefs[key] = entry
	}

	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	path := showPrefsPath(storagePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Written aside and renamed, so a crash mid-write cannot leave a half file
	// that loses every other show's preferences.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// subtitleLanguageFor is the language this show's subtitles should be in: the
// one last picked for it, else SubsLanguage, else English.
func subtitleLanguageFor(config *Config, anime *Anime) string {
	if config == nil {
		config = GetGlobalConfig()
	}
	storage := GetStoragePath()
	if config != nil && strings.TrimSpace(config.StoragePath) != "" {
		storage = config.StoragePath
	}
	if anime != nil && anime.AnilistId > 0 {
		if pref := loadShowPrefs(storage)[strconv.Itoa(anime.AnilistId)].SubtitleLanguage; pref != "" {
			return pref
		}
	}
	if config != nil {
		if language := providers.CanonicalLanguage(config.SubsLanguage); language != "" {
			return language
		}
	}
	return "english"
}

// pickSubtitleForHint chooses the subtitle to play from what a provider offered.
// The provider's own pick stays when no track is in the wanted language, which
// keeps a host that labels nothing working as it always has.
func pickSubtitleForHint(config *Config, anime *Anime, hint StreamPlaybackHint) string {
	if len(hint.Subtitles) == 0 {
		return hint.Subtitle
	}
	return providers.PickSubtitle(hint.Subtitles, subtitleLanguageFor(config, anime), hint.Subtitle)
}

// mpvSubtitleLanguageArgs sets --slang, which ranks the subtitle tracks inside
// a file -- a release carrying its own tracks, as torrents do. A viewer's own
// --slang in MpvArgs is left to win.
func mpvSubtitleLanguageArgs(language string, callerArgs []string) []string {
	for _, arg := range callerArgs {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(arg)), "--slang") {
			return nil
		}
	}
	codes := providers.MPVLanguageCodes(language)
	if codes == "" {
		return nil
	}
	return []string{"--slang=" + codes}
}

// addAlternateSubtitles offers mpv every other subtitle the provider listed,
// without selecting any, so the viewer can switch language with mpv's own
// subtitle key. The chosen track is already loaded; it is not added twice.
func addAlternateSubtitles(send func(string, []interface{}) (interface{}, error), socket, chosen string, tracks []SubtitleTrack) {
	if socket == "" {
		return
	}
	chosen = strings.TrimSpace(chosen)
	for _, track := range tracks {
		url := strings.TrimSpace(track.URL)
		if url == "" || url == chosen {
			continue
		}
		title := track.Label
		if title == "" {
			title = track.Language
		}
		language := providers.MPVLanguageCodes(providers.TrackLanguage(track))
		if i := strings.Index(language, ","); i >= 0 {
			language = language[:i]
		}
		if _, err := send(socket, []interface{}{"sub-add", url, "auto", title, language}); err != nil {
			Log(fmt.Sprintf("Failed to offer the %q subtitles: %v", title, err))
		}
	}
}
