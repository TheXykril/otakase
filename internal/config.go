package internal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"sort"

	// "io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/browser"

	"github.com/thexykril/otakase/internal/icons"
)

const (
	anilistOAuthURL     = "https://anilist.co/api/v2/oauth"
	anilistClientID     = "20686"
	anilistClientSecret = "APfx41cOgSQVMvi88v7PbN7g6kzed2ZQRcxmACod"
	anilistRedirectURI  = "http://localhost:8000/oauth/callback"
	anilistServerPort   = 8000
)

// AnilistToken represents the OAuth token response from Anilist
type AnilistToken struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// Config struct with field names that match the config keys
type Config struct {
	Player                   string   `config:"Player"`
	MpvArgs                  []string `config:"MpvArgs"`
	MpvSkin                  string   `config:"MpvSkin"`
	MpvPlaybackStartTimeout  int      `config:"MpvPlaybackStartTimeout"`
	SubsLanguage             string   `config:"SubsLanguage"`
	SubOrDub                 string   `config:"SubOrDub"`
	SubStyle                 string   `config:"SubStyle"`
	StoragePath              string   `config:"StoragePath"`
	AnimeNameLanguage        string   `config:"AnimeNameLanguage"`
	Theme                    string   `config:"Theme"`
	ThemeOverrides           string   `config:"ThemeOverrides"`
	ThemeFile                string   `config:"ThemeFile"`
	Icons                    string   `config:"Icons"`
	DownloadDir              string   `config:"DownloadDir"`
	DownloadFormat           string   `config:"DownloadFormat"`
	MenuOrder                string   `config:"MenuOrder"`
	MenuActions              string   `config:"MenuActions"`
	ContinueWatchingRows     int      `config:"ContinueWatchingRows"`
	PercentageToMarkComplete int      `config:"PercentageToMarkComplete"`
	NextEpisodePrompt        bool     `config:"NextEpisodePrompt"`
	AutoAudioFallback        bool     `config:"AutoAudioFallback"`
	SkipOp                   bool     `config:"SkipOp"`
	SkipEd                   bool     `config:"SkipEd"`
	SkipFiller               bool     `config:"SkipFiller"`
	ImagePreview             bool     `config:"ImagePreview"`
	SkipRecap                bool     `config:"SkipRecap"`
	RofiSelection            bool     `config:"RofiSelection"`
	// SubOrDubFlag records that -sub or -dub was given for this run, which
	// outranks the audio remembered for a show. Not a setting either.
	SubOrDubFlag               bool   `config:"-"`
	ScoreOnCompletion          bool   `config:"ScoreOnCompletion"`
	SaveMpvSpeed               bool   `config:"SaveMpvSpeed"`
	AddMissingOptions          bool   `config:"AddMissingOptions"`
	AlternateScreen            bool   `config:"AlternateScreen"`
	DiscordPresence            bool   `config:"DiscordPresence"`
	DiscordClientId            string `config:"DiscordClientId"`
	VimKeys                    bool   `config:"VimKeys"`
	AnimeSkipClientID          string `config:"AnimeSkipClientID"`
	ContributeSkipTimes        bool   `config:"ContributeSkipTimes"`
	IntroDBSkipTimes           bool   `config:"IntroDBSkipTimes"`
	CheckUpdates               bool   `config:"CheckUpdates"`
	DevBuilds                  bool   `config:"DevBuilds"`
	MpvEpisodePlaylist         bool   `config:"MpvEpisodePlaylist"`
	Provider                   string `config:"Provider"`
	DisabledProviders          string `config:"DisabledProviders"`
	ManualProviderSearch       bool   `config:"ManualProviderSearch"`
	TrackingLocal              bool   `config:"TrackingLocal"`
	TrackingRemote             string `config:"TrackingRemote"`
	TrackingConfigured         bool   `config:"TrackingConfigured"`
	MyAnimeListClientID        string `config:"MyAnimeListClientID"`
	MyAnimeListClientSecret    string `config:"MyAnimeListClientSecret"`
	MyAnimeListImported        bool   `config:"MyAnimeListImported"`
	MyAnimeListImportDismissed bool   `config:"MyAnimeListImportDismissed"`
	ShowNewEpisodes            bool   `config:"ShowNewEpisodes"`
	CastDevice                 string `config:"CastDevice"`
	// CastTerminal is the terminal emulator to open for a cast started from
	// rofi, which has no terminal of its own to show controls in. Empty means
	// $TERMINAL, then whatever is installed.
	CastTerminal string `config:"CastTerminal"`
	// CastPort is the port the stream server listens on, or 0 for a random
	// free one. Set it when a firewall drops inbound connections by default:
	// a random port cannot be allowed through without opening the whole
	// ephemeral range, and a device that cannot fetch looks identical to one
	// that never started.
	CastPort int `config:"CastPort"`
	// CastDiscoveryPort is the UDP port discovery sends from and hears TVs
	// answer on, or 0 for a random one. Fixed by default so a firewall needs
	// one rule for it rather than every UDP port.
	CastDiscoveryPort int `config:"CastDiscoveryPort"`
	// KodiHost names Kodi instances to offer as cast devices when discovery
	// does not find them -- another subnet, or zeroconf turned off in Kodi.
	// host or host:port, several separated by commas.
	KodiHost     string `config:"KodiHost"`
	KodiUser     string `config:"KodiUser"`
	KodiPassword string `config:"KodiPassword"`
	// CastBurnSubtitles draws a stream's subtitles into the picture when
	// casting it. The Chromecast cannot render the subtitle files these
	// providers supply, so this is the only way to see them on a soft-subbed
	// stream -- at the cost of re-encoding the video, which the copy path
	// never does.
	CastBurnSubtitles bool `config:"CastBurnSubtitles"`
	// CastEncoder forces the encoder used for that: "vaapi", "software", or
	// empty to detect what this machine can actually do.
	CastEncoder string `config:"CastEncoder"`
	// CastNextEpisode is what a cast does when an episode ends: "countdown"
	// counts down on the panel and plays the next one, "stop" ends the cast.
	// Local playback has NextEpisodePrompt for the same question.
	CastNextEpisode string `config:"CastNextEpisode"`
	// Quality is the picture height to play HLS streams at: "best" leaves
	// the master playlist as the provider gave it, a number of lines (1080,
	// 720, 480) picks the nearest variant at or below. A show can override
	// it in show_prefs.json.
	Quality string `config:"Quality"`
	// AdultContent shows 18+ titles in your lists and in search. Off hides
	// them; they stay on the tracker and keep syncing.
	AdultContent bool `config:"AdultContent"`
	// ContinueWatchingAdult lets 18+ shows into the continue-watching rows.
	// They open the home menu, so it is off by default and needs AdultContent.
	ContinueWatchingAdult bool `config:"ContinueWatchingAdult"`
	// CastToDevice records that -cast was given for this run. It is not a
	// setting, so it carries no config tag.
	CastToDevice bool `config:"-"`
	// CastNonInteractive records that this process is the terminal a rofi cast
	// was handed off to, and so has a viewer who is not at the keyboard.
	//
	// It is not a setting either. What it changes is that a prompt with no
	// answer available resolves to its declared default instead of drawing a
	// menu: the process-global cast reader is parked in os.Stdin.Read for the
	// whole run and would split the keystrokes with a Bubble Tea reader on the
	// same descriptor, and a menu nobody is at blocks the season outright.
	// See wiki/Developer-Cast-Window-Prompts.md.
	CastNonInteractive bool `config:"-"`
	// ExperimentalMovies turns on the Movies menu: non-anime movies from a
	// site outside AniList and MyAnimeList, tracked locally only. Off by
	// default while it is experimental.
	ExperimentalMovies bool `config:"ExperimentalMovies"`
	// MovieSite is the address that redirects to wherever the movie site is
	// now. The site itself moves between bare IP addresses.
	MovieSite string `config:"MovieSite"`
	// MovieProvider is where movies are searched: all (every provider at
	// once), vidsrc (English), 8filmai or filmukas (Lithuanian). Changed from
	// the Movies menu.
	MovieProvider string `config:"MovieProvider"`
	// TraktClientID and TraktClientSecret name another Trakt app to sync
	// movies through than otakase's own, which is used when they are empty.
	TraktClientID     string `config:"TraktClientID"`
	TraktClientSecret string `config:"TraktClientSecret"`
}

const (
	DefaultMpvPlaybackStartTimeout = 20
	maxMpvPlaybackStartTimeout     = 600
)

func MpvPlaybackStartTimeoutDuration(config *Config) time.Duration {
	seconds := DefaultMpvPlaybackStartTimeout
	if config != nil && config.MpvPlaybackStartTimeout > 0 {
		seconds = config.MpvPlaybackStartTimeout
	}
	return time.Duration(seconds) * time.Second
}

func GetStoragePath() string {
	if globalConfig != nil && globalConfig.StoragePath != "" {
		return os.ExpandEnv(globalConfig.StoragePath)
	}
	return filepath.Join(os.ExpandEnv("$HOME"), ".local", "share", AppName)
}

// Default configuration values as a map
func defaultConfigMap() map[string]string {
	return map[string]string{
		"Player":                  "mpv",
		"MpvArgs":                 "[]",
		"MpvSkin":                 "true",
		"MpvPlaybackStartTimeout": "20",
		"StoragePath":             StoragePathDefault,
		"AnimeNameLanguage":       "english",
		"SubsLanguage":            "english",
		// The lists, then the things to do. Kept short on purpose: every entry
		// is a row in the rofi menu, and the lists left out are still a tab or
		// a filter away in "Show All".
		"MenuOrder":                "CURRENT,PLANNING,ALL",
		"MenuActions":              "CONTINUE_LAST,UNTRACKED,UPDATE,CAST,SURPRISE,STATS,TRACKER,PROVIDER",
		"ContinueWatchingRows":     "5",
		"SubOrDub":                 "sub",
		"SubStyle":                 "ask",
		"Quality":                  "best",
		"PercentageToMarkComplete": "85",
		"NextEpisodePrompt":        "false",
		// A show that exists only in the other language should play, not stop to
		// ask a question with one useful answer. otakase still says which it used.
		"AutoAudioFallback":          "true",
		"SkipOp":                     "true",
		"SkipEd":                     "true",
		"SkipFiller":                 "true",
		"SkipRecap":                  "true",
		"RofiSelection":              "false",
		"ImagePreview":               "false",
		"ScoreOnCompletion":          "true",
		"SaveMpvSpeed":               "true",
		"AddMissingOptions":          "true",
		"AlternateScreen":            "true",
		"DiscordPresence":            "true",
		"DiscordClientId":            "1287457464148820089",
		"VimKeys":                    "false",
		"AnimeSkipClientID":          "",
		"ContributeSkipTimes":        "true",
		"IntroDBSkipTimes":           "true",
		"CheckUpdates":               "true",
		"DevBuilds":                  "false",
		"ExperimentalMovies":         "false",
		"MovieSite":                  "https://8filmai.eu",
		"MovieProvider":              "all",
		"TraktClientID":              "",
		"TraktClientSecret":          "",
		"AdultContent":               "false",
		"ContinueWatchingAdult":      "false",
		"MpvEpisodePlaylist":         "true",
		"Provider":                   "stacked",
		"DisabledProviders":          "[]",
		"ManualProviderSearch":       "false",
		"TrackingLocal":              "true",
		"TrackingRemote":             "anilist",
		"TrackingConfigured":         "false",
		"MyAnimeListClientID":        "f76d4c827152327b60654285d221222c",
		"MyAnimeListClientSecret":    "",
		"MyAnimeListImported":        "false",
		"MyAnimeListImportDismissed": "false",
		"ShowNewEpisodes":            "true",
		"Theme":                      "auto",
		"ThemeOverrides":             "",
		"ThemeFile":                  "",
		"Icons":                      "auto",
		"DownloadDir":                "$HOME/Downloads/" + AppName,
		"DownloadFormat":             "mkv",
		"CastDevice":                 "",
		"CastTerminal":               "",
		"CastPort":                   "0",
		"CastDiscoveryPort":          "8011",
		"KodiHost":                   "",
		"KodiUser":                   "",
		"KodiPassword":               "",
		"CastBurnSubtitles":          "true",
		"CastEncoder":                "",
		"CastNextEpisode":            CastNextEpisodeCountdown,
	}
}

// VimKeysEnabled reports whether selection menus should use vim-style motions.
func VimKeysEnabled(config *Config) bool {
	if config != nil {
		return config.VimKeys
	}
	if globalConfig != nil {
		return globalConfig.VimKeys
	}
	return false
}

var globalConfig *Config

func SetGlobalConfig(config *Config) {
	globalConfig = config
}

func GetGlobalConfig() *Config {
	return globalConfig
}

func useEnglishAnimeNames(config *Config) bool {
	return config == nil || strings.EqualFold(strings.TrimSpace(config.AnimeNameLanguage), "english")
}

// Helper function to parse string array from config
func parseStringArray(value string) []string {
	value = strings.TrimSpace(value)
	var parsed []string
	if err := json.Unmarshal([]byte(value), &parsed); err == nil {
		result := make([]string, 0, len(parsed))
		for _, part := range parsed {
			part = strings.TrimSpace(part)
			if part != "" {
				result = append(result, part)
			}
		}
		return result
	}

	// Remove brackets and split by comma
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	if value == "" {
		return nil
	}

	// Split by comma and trim spaces and quotes from each element
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		// Trim spaces and quotes
		part = strings.TrimSpace(part)
		part = strings.Trim(part, "\"")
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

var GlobalConfigPath string

func persistSubStylePreference(style string) error {
	style = strings.ToLower(strings.TrimSpace(style))
	if style != "soft" && style != "hard" {
		return nil
	}
	if cfg := GetGlobalConfig(); cfg != nil {
		cfg.SubStyle = style
	}
	if strings.TrimSpace(GlobalConfigPath) == "" {
		return nil
	}
	configMap, err := LoadConfigFromFile(GlobalConfigPath)
	if err != nil {
		return err
	}
	configMap["SubStyle"] = style
	return SaveConfigToFile(GlobalConfigPath, configMap)
}

// LoadConfig reads or creates the config file, adds missing fields, and returns the populated Config struct
func LoadConfig(configPath string) (Config, error) {
	configPath = os.ExpandEnv(configPath) // Substitute environment variables like $HOME
	GlobalConfigPath = configPath
	createdConfig := false

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Create the config file with default values if it doesn't exist
		Out("Config file not found. Creating default config...")
		if err := createDefaultConfig(configPath); err != nil {
			return Config{}, fmt.Errorf("error creating default config file: %v", err)
		}
		createdConfig = true
	}

	// Load the config from file
	configMap, err := LoadConfigFromFile(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("error loading config file: %v", err)
	}

	// Settings otakase does not recognise are kept on disk but never applied, so a
	// typo such as SkipOP=true would otherwise fail in complete silence.
	WarnAboutUnknownConfigKeys(configMap)

	// Check AddMissingOptions setting first
	addMissing := true
	if val, exists := configMap["AddMissingOptions"]; exists {
		if parsed, parseErr := strconv.ParseBool(val); parseErr == nil {
			addMissing = parsed
		}
	}

	_, hadTrackingRemote := configMap["TrackingRemote"]
	_, hadTrackingConfigured := configMap["TrackingConfigured"]
	legacyConfig := !createdConfig && !hadTrackingRemote && !hadTrackingConfigured

	// fileMap = keys actually present on disk.
	// workMap = file keys + in-memory defaults for PopulateConfig.
	fileMap := configMap
	workMap := make(map[string]string, len(fileMap)+len(defaultConfigMap()))
	for key, value := range fileMap {
		workMap[key] = value
	}
	for key, defaultValue := range defaultConfigMap() {
		if _, exists := workMap[key]; !exists {
			workMap[key] = defaultValue
		}
	}

	// One-time legacy tracking migration: only touch tracking keys on disk.
	updated := false
	if legacyConfig {
		fileMap["TrackingLocal"] = "true"
		fileMap["TrackingRemote"] = TrackingRemoteAniList
		fileMap["TrackingConfigured"] = "true"
		workMap["TrackingLocal"] = "true"
		workMap["TrackingRemote"] = TrackingRemoteAniList
		workMap["TrackingConfigured"] = "true"
		updated = true
	}

	if providerValue, exists := fileMap["Provider"]; exists {
		normalizedProviderValue := canonicalProviderConfigValue(providerValue)
		if normalizedProviderValue != providerValue {
			fileMap["Provider"] = normalizedProviderValue
			workMap["Provider"] = normalizedProviderValue
			updated = true
		}
	}

	// New config keys are NOT appended here. MigrateOnVersionUpgrade adds the
	// options registered in configOptionsIntroducedInVersion() when the version
	// changes, so a normal launch never rewrites the file.

	// Persist legacy tracking / provider-token normalize (rewrite only those cases).
	if addMissing && updated {
		if err := SaveConfigToFile(configPath, fileMap); err != nil {
			return Config{}, fmt.Errorf("error saving updated config file: %v", err)
		}
	}

	// Populate the Config struct from the complete (in-memory) work map
	config := PopulateConfig(workMap)
	normalizeTrackingConfig(&config)

	return config, nil
}

// Create a config file with default values in key=value format
// Ensure the directory exists before creating the file
func createDefaultConfig(path string) error {
	defaultConfig := defaultConfigMap()

	// Ensure the directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("error creating directory: %v", err)
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("error creating file: %v", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for key, value := range defaultConfig {
		line := fmt.Sprintf("%s=%s\n", key, value)
		if _, err := writer.WriteString(line); err != nil {
			return fmt.Errorf("error writing to file: %v", err)
		}
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("error flushing writer: %v", err)
	}
	return nil
}

// authenticateWithBrowser performs OAuth authentication using browser
func authenticateWithBrowser(tokenPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Try to load existing token first
	if token, err := loadToken(tokenPath); err == nil && isTokenValid(token) {
		return token.AccessToken, nil
	}

	// Start local server to handle OAuth callback
	callbackCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := http.NewServeMux()
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", anilistServerPort),
		Handler: mux,
	}

	// Handle OAuth callback - for authorization code grant, code comes in query params
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		errorParam := r.URL.Query().Get("error")

		w.Header().Set("Content-Type", "text/html")

		if errorParam != "" {
			w.WriteHeader(http.StatusBadRequest)
			htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <title>Otakase Authentication</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 50px; text-align: center; background: #1a1a1a; color: white; }
        .error { color: #f44336; font-size: 18px; margin-bottom: 20px; }
    </style>
</head>
<body>
    <div class="error">Authentication failed: %s</div>
    <p>You can close this window and try again.</p>
</body>
</html>`, html.EscapeString(errorParam))
			fmt.Fprint(w, htmlBody)
			errCh <- fmt.Errorf("oauth error: %s", errorParam)
			return
		}

		if code == "" {
			w.WriteHeader(http.StatusBadRequest)
			htmlBody := `<!DOCTYPE html>
<html>
<head>
    <title>Otakase Authentication</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 50px; text-align: center; background: #1a1a1a; color: white; }
        .error { color: #f44336; font-size: 18px; margin-bottom: 20px; }
    </style>
</head>
<body>
    <div class="error">No authorization code received</div>
    <p>You can close this window and try again.</p>
</body>
</html>`
			fmt.Fprint(w, htmlBody)
			errCh <- fmt.Errorf("no authorization code received")
			return
		}

		// Exchange authorization code for access token
		go func() {
			tokenURL := fmt.Sprintf("%s/token", anilistOAuthURL)
			data := url.Values{
				"grant_type":    {"authorization_code"},
				"client_id":     {anilistClientID},
				"client_secret": {anilistClientSecret},
				"redirect_uri":  {anilistRedirectURI},
				"code":          {code},
			}

			resp, err := sharedHTTPClient.PostForm(tokenURL, data)
			if err != nil {
				errCh <- fmt.Errorf("failed to exchange code for token: %w", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				errCh <- fmt.Errorf("token exchange failed with status: %d", resp.StatusCode)
				return
			}

			var tokenResponse struct {
				AccessToken string `json:"access_token"`
				TokenType   string `json:"token_type"`
				ExpiresIn   int    `json:"expires_in"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&tokenResponse); err != nil {
				errCh <- fmt.Errorf("failed to parse token response: %w", err)
				return
			}

			if tokenResponse.AccessToken == "" {
				errCh <- fmt.Errorf("no access token in response")
				return
			}

			callbackCh <- tokenResponse.AccessToken
		}()

		// Show success page immediately
		htmlBody := `<!DOCTYPE html>
<html>
<head>
    <title>Otakase Authentication</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 50px; text-align: center; background: #1a1a1a; color: white; }
        .loading { color: #2196F3; font-size: 18px; margin-bottom: 20px; }
    </style>
</head>
<body>
    <div class="loading">Processing authentication...</div>
    <p>Exchanging authorization code for token. You can close this window.</p>
</body>
</html>`
		fmt.Fprint(w, htmlBody)
	})

	// Start server in background
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("failed to start server: %w", err)
		}
	}()
	defer srv.Shutdown(ctx)

	// Give server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Open browser for authentication using Authorization Code Grant flow (response_type=code)
	authURL := fmt.Sprintf("%s/authorize?client_id=%s&redirect_uri=%s&response_type=code",
		anilistOAuthURL,
		anilistClientID,
		url.QueryEscape(anilistRedirectURI))

	fmt.Println("Opening browser for AniList authentication...")
	fmt.Printf("If the browser doesn't open automatically, visit: %s\n", authURL)

	if err := browser.OpenURL(authURL); err != nil {
		fmt.Printf("Failed to open browser automatically: %v\n", err)
		fmt.Println("Please copy and paste the URL above into your browser")
	}

	// Wait for token
	var accessToken string
	select {
	case accessToken = <-callbackCh:
	case err := <-errCh:
		return "", fmt.Errorf("authentication failed: %w", err)
	case <-ctx.Done():
		return "", fmt.Errorf("authentication timeout after 5 minutes")
	}

	// Create token object and save
	token := &AnilistToken{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   31536000, // AniList tokens are valid for 1 year
		ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
	}

	// Save token to file
	if err := saveToken(tokenPath, token); err != nil {
		return "", fmt.Errorf("failed to save token: %w", err)
	}

	fmt.Println("Authentication successful!")
	return token.AccessToken, nil
}

// loadToken loads the token from the token file
func loadToken(tokenPath string) (*AnilistToken, error) {
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read token file: %w", err)
	}

	var token AnilistToken
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("failed to parse token file: %w", err)
	}

	return &token, nil
}

// saveToken saves the token to the token file
func saveToken(tokenPath string, token *AnilistToken) error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	return os.WriteFile(tokenPath, data, 0o600)
}

// isTokenValid checks if the token is still valid
func isTokenValid(token *AnilistToken) bool {
	return token != nil && token.AccessToken != "" && time.Now().Before(token.ExpiresAt)
}

// GetTokenFromFile loads the token from the token file (supports both old text format and new JSON format)
func GetTokenFromFile(tokenPath string) (string, error) {
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return "", fmt.Errorf("failed to read token from file: %w", err)
	}

	// Try to parse as JSON first (new format)
	var token AnilistToken
	if err := json.Unmarshal(data, &token); err == nil {
		// It's JSON format, check if token is valid
		if isTokenValid(&token) {
			return token.AccessToken, nil
		}
		return "", fmt.Errorf("token has expired")
	}

	// Fall back to plain text format (old format)
	plainToken := strings.TrimSpace(string(data))
	if plainToken == "" {
		return "", fmt.Errorf("empty token file")
	}

	return plainToken, nil
}

func ChangeToken(config *Config, user *User) {
	var err error
	tokenPath := filepath.Join(os.ExpandEnv(config.StoragePath), "anilist_token.json")

	// Try browser-based OAuth first
	fmt.Println("Starting browser-based authentication...")
	user.Token, err = authenticateWithBrowser(tokenPath)
	if err != nil {
		Log("Browser authentication failed: " + err.Error())
		fmt.Printf("Browser authentication failed: %v\n", err)
		fmt.Println("Falling back to manual token entry...")

		// Simple CLI fallback
		fmt.Println("Please visit: https://anilist.co/api/v2/oauth/authorize?client_id=20686&response_type=token&redirect_uri=http://localhost:8000/oauth/callback")

		// Read through the same prompt as everything else, so stdin has one
		// reader: a bare fmt.Scanln here would race the buffered one for
		// whatever is already typed. Backing out still stops -- there is
		// nothing this program can do without a token -- but it says so
		// rather than looking like a crash.
		pasted, cancelled, inputErr := promptCancelable(config, "Login",
			"Paste your AniList access token",
			"the part of the redirect URL after access_token=")
		if inputErr != nil {
			Log("Failed to read the pasted token: " + inputErr.Error())
		}
		if cancelled || pasted == "" {
			Exit(fmt.Errorf("no token provided"))
		}
		user.Token = pasted

		// Save the manually entered token as JSON format
		token := &AnilistToken{
			AccessToken: user.Token,
			TokenType:   "Bearer",
			ExpiresIn:   31536000, // AniList tokens are valid for 1 year
			ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
		}

		if err := saveToken(tokenPath, token); err != nil {
			Exit(fmt.Errorf("failed to save token: %w", err))
		}
	}

	if user.Token == "" {
		Exit(fmt.Errorf("no token provided"))
	}

	fmt.Println("Token saved successfully!")
}

// LoadConfigFromFile loads config file from disk into a map (key=value format)
func LoadConfigFromFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	configMap := make(map[string]string)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue // Skip empty lines and comments
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			configMap[key] = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return configMap, nil
}

// SaveConfigToFile saves updated config map to file in key=value format.
//
// The write is atomic. This file holds tracker credentials and is rewritten on
// most runs, and truncating it in place meant a crash or two concurrent runs
// could leave it mangled -- a real config in the wild ended up with
// "nimeListClientID", which is "MyAnimeListClientID" missing its first three
// characters.
func SaveConfigToFile(path string, configMap map[string]string) error {
	keys := make([]string, 0, len(configMap))
	for key := range configMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	for _, key := range keys {
		if _, err := fmt.Fprintf(&buf, "%s=%s\n", key, configMap[key]); err != nil {
			return err
		}
	}

	return writeFileAtomic(path, buf.Bytes(), 0o644)
}

// PopulateConfig populates the Config struct from a map
func PopulateConfig(configMap map[string]string) Config {
	config := Config{}
	configValue := reflect.ValueOf(&config).Elem()
	defaults := defaultConfigMap()

	for i := 0; i < configValue.NumField(); i++ {
		field := configValue.Type().Field(i)
		tag := field.Tag.Get("config")

		value, exists := configMap[tag]
		if !exists || value == "" {
			value = defaults[tag]
		}
		fieldValue := configValue.FieldByName(field.Name)

		if fieldValue.CanSet() {
			switch fieldValue.Kind() {
			case reflect.String:
				fieldValue.SetString(value)
			case reflect.Int:
				intVal, err := strconv.Atoi(value)
				if err != nil {
					intVal, _ = strconv.Atoi(defaults[tag])
				}
				fieldValue.SetInt(int64(intVal))
			case reflect.Bool:
				boolVal, err := strconv.ParseBool(value)
				if err != nil {
					boolVal, _ = strconv.ParseBool(defaults[tag])
				}
				fieldValue.SetBool(boolVal)
			}
		}
	}

	// Handle MpvArgs specially
	if mpvArgs, exists := configMap["MpvArgs"]; exists {
		config.MpvArgs = parseStringArray(mpvArgs)
	}

	// Validate PercentageToMarkComplete range (0-100)
	if config.PercentageToMarkComplete < 0 {
		config.PercentageToMarkComplete = 0
	} else if config.PercentageToMarkComplete > 100 {
		config.PercentageToMarkComplete = 100
	}

	if config.MpvPlaybackStartTimeout <= 0 {
		config.MpvPlaybackStartTimeout = DefaultMpvPlaybackStartTimeout
	} else if config.MpvPlaybackStartTimeout > maxMpvPlaybackStartTimeout {
		config.MpvPlaybackStartTimeout = maxMpvPlaybackStartTimeout
	}

	return config
}

func normalizeRemoteTracker(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "none", "disabled", "off", "local":
		return TrackingRemoteNone
	case "anilist", "ani-list":
		return TrackingRemoteAniList
	case "myanimelist", "my_anime_list", "my-anime-list", "mal":
		return TrackingRemoteMyAnimeList
	case "anilist+myanimelist", "anilist,myanimelist", "myanimelist+anilist", "myanimelist,anilist", "both", "dual":
		return TrackingRemoteBoth
	default:
		return ""
	}
}

func normalizeTrackingConfig(config *Config) {
	if config == nil {
		return
	}

	remote := normalizeRemoteTracker(config.TrackingRemote)
	if remote == "" {
		config.TrackingConfigured = false
		remote = TrackingRemoteAniList
	}

	config.TrackingRemote = remote
	config.TrackingLocal = true
}

// categoryIcon is the icon for a main menu entry or a list category.
func categoryIcon(key string) icons.Icon {
	switch key {
	case "CURRENT":
		return icons.Watching
	case "ALL":
		return icons.AllShows
	case "UNTRACKED":
		return icons.Search
	case "UPDATE":
		return icons.Edit
	case "REMAP_PROVIDER":
		return icons.Remap
	case "CONTINUE_LAST":
		return icons.History
	case "SURPRISE":
		return icons.Surprise
	case "PLANNING":
		return icons.Planning
	case "COMPLETED":
		return icons.Completed
	case "PAUSED":
		return icons.Paused
	case "DROPPED":
		return icons.Dropped
	case "REWATCHING", "REPEATING":
		return icons.Rewatching
	case "TRACKER":
		return icons.Tracker
	case "PROVIDER":
		return icons.Provider
	case "CAST":
		return icons.Cast
	case "STATS":
		return icons.Stats
	case "MOVIES":
		return icons.TV
	}
	return 0
}

// menuKeys is every key the home menu names, in order: MenuOrder's, then
// MenuActions'. Configs written before MenuActions existed keep their actions
// in MenuOrder, so either setting may hold either kind; a key named twice
// counts where it first appears.
func menuKeys(config *Config) []string {
	if config == nil {
		return nil
	}
	keys := []string{}
	seen := map[string]bool{}
	for _, raw := range strings.Split(config.MenuOrder+","+config.MenuActions, ",") {
		key := strings.ToUpper(strings.TrimSpace(raw))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys
}

// defaultMenuKeys is the home menu of a fresh config.
func defaultMenuKeys() []string {
	defaults := defaultConfigMap()
	return menuKeys(&Config{MenuOrder: defaults["MenuOrder"], MenuActions: defaults["MenuActions"]})
}
