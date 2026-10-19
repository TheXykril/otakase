package internal

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Values of Config.TrackingRemote.
const (
	// TrackingRemoteNone keeps local history only.
	TrackingRemoteNone = "none"

	// TrackingRemoteAniList and TrackingRemoteMyAnimeList track on one site.
	TrackingRemoteAniList = "anilist"

	TrackingRemoteMyAnimeList = "myanimelist"

	// TrackingRemoteBoth keeps AniList and MyAnimeList in step.
	TrackingRemoteBoth = "anilist+myanimelist"
)

// remoteTrackerWriteDelay paces writes to either tracker. Both rate limit, and
// a first dual sync can easily be hundreds of entries.
const remoteTrackerWriteDelay = 350 * time.Millisecond

// remoteTracker is the configured remote, normalized. A nil config is
// AniList, which is what the app tracked with before there was a choice. An
// unrecognised value comes back as "": remote, but no tracker matches it.
func remoteTracker(cfg *Config) string {
	if cfg != nil {
		return canonicalTrackerName(cfg.TrackingRemote)
	}
	return TrackingRemoteAniList
}

// remoteTrackerIs reports whether the configured remote is one of modes.
func remoteTrackerIs(cfg *Config, modes ...string) bool {
	current := remoteTracker(cfg)
	for _, mode := range modes {
		if current == mode {
			return true
		}
	}
	return false
}

func UsesRemoteTracking(cfg *Config) bool {
	return !remoteTrackerIs(cfg, TrackingRemoteNone)
}

// ShouldWriteRemoteTracking is false for local-only tracking and for a show
// marked to stay off the remote lists.
func ShouldWriteRemoteTracking(cfg *Config, show *Anime) bool {
	if show != nil && show.SkipRemoteSync {
		return false
	}
	return UsesRemoteTracking(cfg)
}

func UsesAniListTracking(cfg *Config) bool {
	return remoteTrackerIs(cfg, TrackingRemoteAniList, TrackingRemoteBoth)
}

func UsesMyAnimeListTracking(cfg *Config) bool {
	return remoteTrackerIs(cfg, TrackingRemoteMyAnimeList, TrackingRemoteBoth)
}

func UsesDualRemoteTracking(cfg *Config) bool {
	return remoteTrackerIs(cfg, TrackingRemoteBoth)
}

// remoteOnlyCategories are the menu entries that only make sense with a
// remote list behind them.
var remoteOnlyCategories = map[string]bool{
	"UPDATE": true, "PLANNING": true, "COMPLETED": true, "PAUSED": true,
	"DROPPED": true, "REWATCHING": true, "SURPRISE": true,
}

func trackingCategoryEnabled(cfg *Config, key string) bool {
	switch {
	case key == "STATS":
		// Kept for local tracking as well: the page is where a local-only
		// user finds out stats come from AniList or MyAnimeList, which a
		// missing entry would never tell them.
		return true
	case key == "MOVIES":
		// Movies are always tracked locally, so only the experimental
		// switch matters.
		return cfg != nil && cfg.ExperimentalMovies
	case key == "COMMUNITY":
		return discordEnabled(cfg)
	case remoteOnlyCategories[key]:
		return UsesRemoteTracking(cfg)
	}
	// The rest runs off local history.
	return true
}

// trackerChoices are the tracking modes, as offered at first launch and from
// the tracker menu. Each key is a value of TrackingRemote.
func trackerChoices() []SelectionOption {
	return []SelectionOption{
		{Key: TrackingRemoteNone, Label: "local"},
		{Key: TrackingRemoteAniList, Label: "anilist"},
		{Key: TrackingRemoteMyAnimeList, Label: "myanimelist"},
		{Key: TrackingRemoteBoth, Label: "anilist + myanimelist"},
	}
}

// selectionCancelled is true for the keys a menu returns when backed out of.
func selectionCancelled(picked SelectionOption) bool {
	switch picked.Key {
	case "-1", "-2":
		return true
	}
	return false
}

// setTracker records remote as the tracker and saves it. Local history is
// kept on in every mode.
func setTracker(cfg *Config, remote string) error {
	cfg.TrackingRemote = remote
	cfg.TrackingLocal, cfg.TrackingConfigured = true, true
	normalizeTrackingConfig(cfg)
	return persistTrackingConfig(cfg)
}

// EnsureTrackingConfigured asks for a tracking mode the first time round.
// Afterwards there is nothing to do; saving only happens on a change.
func EnsureTrackingConfigured(cfg *Config) error {
	if cfg == nil {
		return errors.New("missing config")
	}
	normalizeTrackingConfig(cfg)
	if cfg.TrackingConfigured {
		return nil
	}

	Out("Choose tracking mode. Local history stays enabled in every mode.")
	picked, selectErr := DynamicSelect(trackerChoices())
	switch {
	case selectErr != nil:
		return selectErr
	case selectionCancelled(picked):
		return errors.New("tracking setup cancelled")
	}
	mode := canonicalTrackerName(picked.Key)
	if mode == "" {
		return fmt.Errorf("unsupported tracking selection: %s", picked.Key)
	}
	return setTracker(cfg, mode)
}

// persistTrackingConfig writes the tracking settings into the config file,
// leaving the rest of it as the user has it.
func persistTrackingConfig(cfg *Config) error {
	if cfg == nil || ConfigFilePath == "" {
		return nil
	}

	onDisk, loadErr := LoadConfigFromFile(ConfigFilePath)
	if loadErr != nil {
		return loadErr
	}
	flag := strconv.FormatBool
	onDisk["TrackingLocal"] = flag(cfg.TrackingLocal)
	onDisk["TrackingRemote"] = cfg.TrackingRemote
	onDisk["TrackingConfigured"] = flag(cfg.TrackingConfigured)
	onDisk["MyAnimeListClientID"] = cfg.MyAnimeListClientID
	onDisk["MyAnimeListClientSecret"] = cfg.MyAnimeListClientSecret
	onDisk["MyAnimeListImported"] = flag(cfg.MyAnimeListImported)
	onDisk["MyAnimeListImportDismissed"] = flag(cfg.MyAnimeListImportDismissed)
	return SaveConfigToFile(ConfigFilePath, onDisk)
}

// promptTrackingInput asks for one piece of tracker setup: a client id, a
// secret, a pasted callback URL.
//
// These used to be bare line reads from a reader made for that one read, which
// dropped anything typed past the line, so pasting the id and secret together
// lost the secret. Escaping out of an optional question (where empty already
// meant "skip") counts as skipping it; on a required one it is refused as an
// empty answer always was.
func promptTrackingInput(question string, optional bool) (string, error) {
	hint := "esc to go back"
	if optional {
		hint = "leave empty to skip · esc to skip"
	}

	answer, backedOut, promptErr := promptCancelable(GetGlobalConfig(), "Tracker", question, hint)
	if promptErr != nil {
		return "", promptErr
	}
	if !backedOut {
		return answer, nil
	}
	if optional {
		return "", nil
	}
	return "", errors.New("input required")
}

// promptAnimeScoreValue asks for a score out of ten, and reports backing out
// of the question rather than treating it as a score.
//
// Not rating is the normal answer here -- the question turns up unasked after
// an episode ends -- and it used to quit the program: the empty line did not
// parse, and every caller handed that error to one that exited on it.
func promptAnimeScoreValue() (float64, bool, error) {
	return valueFromAnswers(parseAnimeScore, askAnimeScore)
}

func askAnimeScore() (string, bool, error) {
	return promptCancelable(GetGlobalConfig(), "Score", "Rate this anime", "0 to 10 · esc to skip")
}

// parseAnimeScore accepts a number from 0 to 10, surrounding space allowed.
func parseAnimeScore(typed string) (float64, error) {
	value, convErr := strconv.ParseFloat(strings.TrimSpace(typed), 64)
	if convErr != nil {
		return 0, fmt.Errorf("invalid score: %w", convErr)
	}
	if value < 0 || value > 10 {
		return 0, errors.New("score must be between 0 and 10")
	}
	return value, nil
}

// hasToken reports whether a token lookup produced something usable.
func hasToken(token string, err error) bool {
	return err == nil && strings.TrimSpace(token) != ""
}

// storedAniListToken is the AniList token saved at sign-in. It is read on its
// own because with dual tracking user.Token may hold the MyAnimeList one.
func storedAniListToken(cfg *Config) (string, error) {
	return GetTokenFromFile(anilistTokenPath(cfg))
}

// ensureMyAnimeListCredentialsConfigured asks for the MAL app's client id
// (and optionally its secret) when none is set yet, and saves them.
func ensureMyAnimeListCredentialsConfigured(cfg *Config) error {
	if id, _ := myAnimeListClientCredentials(cfg); id != "" {
		return nil
	}

	Out("MyAnimeList sync needs MAL application credentials.")
	id, idErr := promptTrackingInput("Enter your MyAnimeList client ID", false)
	if idErr != nil {
		return idErr
	}
	secret, secretErr := promptTrackingInput("Enter your MyAnimeList client secret (optional)", true)
	if secretErr != nil {
		return secretErr
	}
	cfg.MyAnimeListClientID = id
	cfg.MyAnimeListClientSecret = secret
	return persistTrackingConfig(cfg)
}

// ensureAniListTrackerReady signs in to AniList when there is no saved token.
// With one, it becomes user's token unless user already holds some token.
func ensureAniListTrackerReady(cfg *Config, user *User) {
	saved, readErr := storedAniListToken(cfg)
	switch {
	case !hasToken(saved, readErr):
		ChangeToken(cfg, user)
	case user != nil && user.Token == "":
		user.Token = saved
	}
}

// ensureMyAnimeListTrackerReady makes sure MAL has app credentials and a
// sign-in, asking for whichever is missing.
func ensureMyAnimeListTrackerReady(cfg *Config, user *User) error {
	if credErr := ensureMyAnimeListCredentialsConfigured(cfg); credErr != nil {
		return credErr
	}
	if !hasToken(GetMyAnimeListAccessToken(cfg)) {
		return ChangeMyAnimeListToken(cfg, user)
	}
	return nil
}

// EnsureConfiguredTrackersReady signs in to whichever trackers are configured
// and lack a token, then loads the one the session runs on.
func EnsureConfiguredTrackersReady(cfg *Config, user *User) error {
	if cfg == nil {
		return errors.New("missing config")
	}
	if UsesAniListTracking(cfg) {
		ensureAniListTrackerReady(cfg, user)
	}
	if UsesMyAnimeListTracking(cfg) {
		if malErr := ensureMyAnimeListTrackerReady(cfg, user); malErr != nil {
			return malErr
		}
	}
	return LoadTokenForConfiguredTracking(cfg, user)
}

// localAnimeListFromHistory presents local history as a watching list.
// History stores the episode to play next, so progress is one behind it. A
// show saved without a title goes by its AniList ID.
func localAnimeListFromHistory(saved []Anime) AnimeList {
	watching := make([]Entry, 0, len(saved))
	for _, row := range saved {
		name := row.Title
		if name.Romaji == "" && name.English == "" {
			idText := strconv.Itoa(row.AnilistId)
			name.English = idText
			name.Romaji = idText
		}
		media := Media{ID: row.AnilistId, MalID: row.MalId, Title: name, Episodes: row.TotalEpisodes}
		progress := max(0, row.Ep.Number-1)
		watching = append(watching, Entry{Media: media, Status: "CURRENT", Progress: progress})
	}
	if len(watching) == 0 {
		return AnimeList{}
	}
	return AnimeList{Watching: watching}
}

// nextEpisodeFromProgress is the episode after the last one watched.
func nextEpisodeFromProgress(watched int) int {
	if watched < 0 {
		watched = 0
	}
	return watched + 1
}

func BuildLocalAnimeList(storageDir string) AnimeList {
	history := LocalGetAllAnime(HistoryFile(storageDir))
	return localAnimeListFromHistory(history)
}

// hasAnyEntries is true when any status bucket of list holds a show.
func hasAnyEntries(list AnimeList) bool {
	for _, bucket := range [][]Entry{list.Watching, list.Completed, list.Paused, list.Dropped, list.Planning, list.Rewatching} {
		if len(bucket) > 0 {
			return true
		}
	}
	return false
}

// countEntries counts the entries of list that match, each show once.
func countEntries(list AnimeList, match func(Entry) bool) int {
	matched := 0
	for _, candidate := range getEntriesByCategory(list, "ALL") {
		if match(candidate) {
			matched++
		}
	}
	return matched
}

// Predicates for countEntries.
func anyEntry(Entry) bool        { return true }
func lacksMalID(e Entry) bool    { return e.Media.MalID == 0 }
func hasAniListRow(e Entry) bool { return e.ListID != 0 }

// publishAnimeList makes list the user's current one. An existing ListSync is
// updated in place so menus already on screen redraw.
func publishAnimeList(user *User, list AnimeList) {
	user.AnimeList = list
	if user.ListSync != nil {
		user.ListSync.Replace(list, true)
	} else {
		user.ListSync = NewAnimeListSync(list)
	}
	user.ListSync.MarkRefreshDone()
}

// InitializeUserAnimeList loads the list for the configured tracker. Local
// tracking builds it from history, which is never stale.
func InitializeUserAnimeList(cfg *Config, user *User) error {
	if cfg == nil || user == nil {
		return errors.New("missing user or config")
	}

	var loadErr error
	switch remoteTracker(cfg) {
	case TrackingRemoteAniList:
		return InitializeAniListUserAnimeList(cfg, user)
	case TrackingRemoteMyAnimeList:
		loadErr = InitializeMyAnimeListUserAnimeList(cfg, user)
	case TrackingRemoteBoth:
		loadErr = InitializeCombinedRemoteAnimeList(cfg, user)
	default:
		history := BuildLocalAnimeList(cfg.StoragePath)
		user.AnimeList = history
		user.ListSync = settledAnimeListSync(history)
		return nil
	}
	if loadErr != nil {
		return loadErr
	}
	// Either MyAnimeList mode may start empty with an AniList list to bring.
	return maybeImportAniListToMyAnimeList(cfg, user)
}

// RefreshUserAnimeList reloads the list for the configured tracker.
func RefreshUserAnimeList(cfg *Config, user *User) error {
	if cfg == nil || user == nil {
		return errors.New("missing user or config")
	}

	refreshers := map[string]func(*Config, *User) error{
		TrackingRemoteAniList:     RefreshAniListUserAnimeList,
		TrackingRemoteMyAnimeList: RefreshMyAnimeListUserAnimeList,
		TrackingRemoteBoth:        RefreshCombinedRemoteAnimeList,
	}
	if refresh, remote := refreshers[remoteTracker(cfg)]; remote {
		return refresh(cfg, user)
	}
	publishAnimeList(user, BuildLocalAnimeList(cfg.StoragePath))
	return nil
}

// LoadTokenForConfiguredTracking sets user.Token to the token the session
// runs on. With both trackers that is AniList's, or MyAnimeList's when there
// is no AniList token; with none it is empty.
func LoadTokenForConfiguredTracking(cfg *Config, user *User) error {
	if cfg == nil || user == nil {
		return errors.New("missing config or user")
	}

	var (
		token     string
		lookupErr error
	)
	switch remoteTracker(cfg) {
	case TrackingRemoteAniList:
		token, lookupErr = storedAniListToken(cfg)
	case TrackingRemoteMyAnimeList:
		token, lookupErr = GetMyAnimeListAccessToken(cfg)
	case TrackingRemoteBoth:
		token, lookupErr = storedAniListToken(cfg)
		if !hasToken(token, lookupErr) {
			token, lookupErr = GetMyAnimeListAccessToken(cfg)
		}
	}
	if lookupErr != nil {
		return lookupErr
	}
	user.Token = token
	return nil
}

// exitOnError ends the program over a non-nil err.
func exitOnError(err error) {
	if err == nil {
		return
	}
	Exit(err)
}

// ChangeTrackingToken signs in again to the configured trackers.
func ChangeTrackingToken(cfg *Config, user *User) {
	if !UsesAniListTracking(cfg) && !UsesMyAnimeListTracking(cfg) {
		Out("Remote tracking is disabled.")
		return
	}
	mode := remoteTracker(cfg)
	if mode == TrackingRemoteBoth {
		exitOnError(ensureMyAnimeListCredentialsConfigured(cfg))
	}
	if mode != TrackingRemoteMyAnimeList {
		ChangeToken(cfg, user)
	}
	if mode != TrackingRemoteAniList {
		exitOnError(ChangeMyAnimeListToken(cfg, user))
	}
	if mode == TrackingRemoteBoth {
		exitOnError(LoadTokenForConfiguredTracking(cfg, user))
	}
}

// RemoteTrackingDisplayName names the configured tracker for messages.
func RemoteTrackingDisplayName(cfg *Config) string {
	names := map[string]string{
		TrackingRemoteAniList:     "AniList",
		TrackingRemoteMyAnimeList: "MyAnimeList",
		TrackingRemoteBoth:        "AniList + MyAnimeList",
	}
	if name, ok := names[remoteTracker(cfg)]; ok {
		return name
	}
	return "remote tracking"
}
