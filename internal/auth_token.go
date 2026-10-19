package internal

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// mentionsInvalidToken matches how both trackers word a rejected token.
func mentionsInvalidToken(text string) bool {
	lower := strings.ToLower(text)
	for _, phrase := range []string{"invalid token", "invalid_token"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// isInvalidTokenResponse is true for a 401, or any answer whose body says the
// token was refused.
func isInvalidTokenResponse(code int, body []byte) bool {
	if code == http.StatusUnauthorized {
		return true
	}
	return mentionsInvalidToken(string(body))
}

func isInvalidTokenError(cause error) bool {
	if cause == nil {
		return false
	}
	return mentionsInvalidToken(cause.Error())
}

// anilistTokenPath is where the AniList sign-in is saved.
func anilistTokenPath(cfg *Config) string {
	storage := os.ExpandEnv(cfg.StoragePath)
	return filepath.Join(storage, "anilist_token.json")
}

// configOrGlobal falls back to the process-wide config when cfg is nil.
func configOrGlobal(cfg *Config) *Config {
	if cfg != nil {
		return cfg
	}
	return GetGlobalConfig()
}

// savedAniListToken reads the AniList token on disk, or "" when there is no
// config, no storage dir or no usable token.
func savedAniListToken(cfg *Config) string {
	if cfg == nil || strings.TrimSpace(cfg.StoragePath) == "" {
		return ""
	}
	saved, err := GetTokenFromFile(anilistTokenPath(cfg))
	if !hasToken(saved, err) {
		return ""
	}
	return saved
}

// anilistTokenForAPI picks the token for an AniList call. Callers pass
// user.Token, which under MyAnimeList or dual tracking is the MyAnimeList
// token; AniList rejects that as "Invalid token", and treating the rejection
// as an expired sign-in used to open the browser on every run. The saved
// AniList token therefore takes precedence and held is only the fallback.
func anilistTokenForAPI(cfg *Config, held string) string {
	if saved := savedAniListToken(configOrGlobal(cfg)); saved != "" {
		return saved
	}
	return held
}

// ReauthenticateAniList discards the saved AniList token and runs the browser
// sign-in again. A non-empty reason is included in what is shown and logged.
func ReauthenticateAniList(config *Config, user *User, reason string) (string, error) {
	cfg := configOrGlobal(config)
	if cfg == nil {
		return "", errors.New("missing config for AniList reauthentication")
	}

	detail := ""
	if reason != "" {
		detail = " (" + reason + ")"
	}
	Out("AniList sign-in required" + detail + ". Opening browser...")
	Log("AniList token invalid" + detail + ", starting browser authentication")

	path := anilistTokenPath(cfg)
	clearStoredTokenFile(path)
	fresh, err := anilistBrowserLogin(path)
	if err != nil {
		return "", err
	}

	if user == nil {
		setGlobalUserToken(fresh)
	} else {
		user.Token = fresh
	}
	return fresh, nil
}

// tryRenewAniListTokenForAPI finds a replacement for a token AniList refused.
// The refused one may have been the MyAnimeList token standing in for
// AniList's, in which case retrying with the saved AniList token is enough;
// only when that is what failed does the user get sent to the browser.
func tryRenewAniListTokenForAPI(rejected string, reason string) (string, bool, error) {
	cfg := GetGlobalConfig()
	if cfg == nil {
		return rejected, false, nil
	}

	if saved, err := GetTokenFromFile(anilistTokenPath(cfg)); hasToken(saved, err) && saved != rejected {
		return saved, true, nil
	}

	fresh, err := ReauthenticateAniList(cfg, GetGlobalUser(), reason)
	if err != nil {
		return rejected, false, err
	}
	return fresh, true, nil
}

// clearStoredTokenFile deletes a token file the tracker no longer accepts.
// It being gone already is not an error.
func clearStoredTokenFile(path string) {
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return
	}
	Log(fmt.Sprintf("Failed to clear stale token file %s: %v", path, err))
}
