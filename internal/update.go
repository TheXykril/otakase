package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultUpdateRepo is the fork every update path pulls from: the
	// launch check, the update prompt, and `otakase -u` alike.
	DefaultUpdateRepo     = "TheXykril/otakase"
	updatePendingFileName = "update_pending.json"
	// launchUpdateWait bounds how long the launch waits for its own update
	// check before moving on. The check runs alongside the tracker sign-in, so
	// it is normally done well before this; a slow network only costs this much.
	launchUpdateWait = 2 * time.Second
	// remindLaterDelay is how long "Remind me later" keeps the prompt away.
	remindLaterDelay = 24 * time.Hour
	// releaseNotesLimit caps the notes kept in the state file. The prompt
	// reads the full notes again before it shows them.
	releaseNotesLimit = 1200
)

// pendingUpdate is the launch check's result, kept in update_pending.json
// under StoragePath so a check that finishes late still counts next launch.
type pendingUpdate struct {
	Available     bool   `json:"available"`
	LatestVersion string `json:"latest_version"`
	LatestTag     string `json:"latest_tag"`
	ReleaseName   string `json:"release_name"`
	ReleaseNotes  string `json:"release_notes"`
	HTMLURL       string `json:"html_url"`
	AssetName     string `json:"asset_name"`
	CheckedAt     string `json:"checked_at"`
	// Changelog is what changed between the running version and this one,
	// newest first (see updateChangelog).
	Changelog      []changelogRelease `json:"changelog,omitempty"`
	SkippedVersion string             `json:"skipped_version,omitempty"`
	RemindAfter    string             `json:"remind_after,omitempty"`
}

var (
	launchUpdateCheckOnce sync.Once
	// launchUpdateDone is closed when this run's update check has finished.
	launchUpdateDone chan struct{}
	// launchUpdatePromptOnce keeps the prompt to one showing per run.
	launchUpdatePromptOnce sync.Once
)

func pendingUpdatePath(storagePath string) string {
	if strings.TrimSpace(storagePath) == "" {
		storagePath = GetStoragePath()
	}
	return filepath.Join(os.ExpandEnv(storagePath), updatePendingFileName)
}

// readPendingUpdate returns the stored check result. No file, or one that
// doesn't parse, is the zero value: nothing known yet.
func readPendingUpdate(storagePath string) pendingUpdate {
	raw, err := os.ReadFile(pendingUpdatePath(storagePath))
	if err != nil {
		return pendingUpdate{}
	}
	var pu pendingUpdate
	if jerr := json.Unmarshal(raw, &pu); jerr != nil {
		Log("Ignoring corrupt update state: " + jerr.Error())
		return pendingUpdate{}
	}
	return pu
}

// writePendingUpdate stores pu, creating the storage dir if needed.
func writePendingUpdate(storagePath string, pu pendingUpdate) error {
	raw, err := json.MarshalIndent(pu, "", "  ")
	if err != nil {
		return err
	}
	file := pendingUpdatePath(storagePath)
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		return err
	}
	return os.WriteFile(file, raw, 0600)
}

// updateChecksEnabled is false without a config, so callers that run before
// one is loaded never touch the network.
func updateChecksEnabled(config *Config) bool {
	return config != nil && config.CheckUpdates
}

// remindLaterPending reports whether the user asked to be reminded later and
// that time has not come yet. An unreadable time counts as no reminder.
func remindLaterPending(state pendingUpdate) bool {
	until, ok := remindAfterTime(state)
	return ok && time.Now().Before(until)
}

func remindAfterTime(state pendingUpdate) (time.Time, bool) {
	if state.RemindAfter == "" {
		return time.Time{}, false
	}
	until, err := time.Parse(time.RFC3339, state.RemindAfter)
	return until, err == nil
}

// recordRelease copies what the prompt shows about release into the state.
// The notes are left to the caller, which decides whether to shorten them.
func (state *pendingUpdate) recordRelease(release githubReleaseAPI, version string) {
	state.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	state.LatestTag = release.TagName
	state.LatestVersion = version
	state.ReleaseName = strings.TrimSpace(release.Name)
	if state.ReleaseName == "" {
		state.ReleaseName = DisplayName + " " + version
	}
	state.HTMLURL = release.HTMLURL
	if asset, err := releaseBinaryName(); err == nil {
		state.AssetName = asset
	}
}

// StartBackgroundUpdateCheck looks for a newer release at launch, without
// blocking it. HandleLaunchUpdatePrompt picks the result up in the same run;
// it is also written to StoragePath/update_pending.json for the next launch.
func StartBackgroundUpdateCheck(config *Config, currentVersion string) {
	if !updateChecksEnabled(config) {
		return
	}
	launchUpdateCheckOnce.Do(func() {
		done := make(chan struct{})
		launchUpdateDone = done
		go runLaunchUpdateCheck(config, currentVersion, done)
	})
}

func runLaunchUpdateCheck(config *Config, currentVersion string, done chan struct{}) {
	defer close(done)
	if err := checkForUpdateInBackground(config, currentVersion); err != nil {
		Log("Background update check failed: " + err.Error())
	}
}

// waitForLaunchUpdateCheck reports whether this run's check finished within wait.
func waitForLaunchUpdateCheck(wait time.Duration) bool {
	done := launchUpdateDone
	if done == nil {
		return false
	}
	select {
	case <-done:
		return true
	case <-time.After(wait):
		Log("Update check still running; its result is kept for the next launch")
		return false
	}
}

// checkForUpdateInBackground looks up the release on offer and stores it,
// marked available only when it is newer than currentVersion and not skipped.
func checkForUpdateInBackground(config *Config, currentVersion string) error {
	pu := readPendingUpdate(config.StoragePath)
	// While a "remind me later" is running there is nothing to show, so
	// there is no reason to ask GitHub either.
	if remindLaterPending(pu) {
		Log("Skipping update check until " + pu.RemindAfter)
		return nil
	}

	release, latest, err := fetchUpdateRelease(DefaultUpdateRepo, config.DevBuilds)
	if err != nil {
		return err
	}
	pu.recordRelease(release, latest)
	// Only a preview goes on disk; the prompt fetches the full notes.
	pu.ReleaseNotes = truncateReleaseNotes(release.Body)

	skipped := pu.SkippedVersion != "" && normalizeReleaseVersion(pu.SkippedVersion) == latest
	pu.Available = !skipped && isUpdateNewer(latest, currentVersion)
	if pu.Available {
		pu.Changelog = updateChangelog(DefaultUpdateRepo, release, latest, currentVersion)
		// An expired reminder is just clutter; drop it.
		if until, ok := remindAfterTime(pu); ok && !until.After(time.Now()) {
			pu.RemindAfter = ""
		}
		Log(fmt.Sprintf("Update available: %s → %s", currentVersion, latest))
	}
	return writePendingUpdate(config.StoragePath, pu)
}

// pendingUpdateShouldPrompt reports whether the stored update is one to offer
// now: newer than what runs, not skipped and not put off until later.
func pendingUpdateShouldPrompt(config *Config, currentVersion string, state pendingUpdate) bool {
	switch {
	case !updateChecksEnabled(config), !state.Available:
		return false
	case strings.TrimSpace(state.LatestVersion) == "":
		return false
	case !isUpdateNewer(state.LatestVersion, currentVersion):
		return false
	case normalizeReleaseVersion(state.SkippedVersion) == normalizeReleaseVersion(state.LatestVersion):
		return false
	}
	return !remindLaterPending(state)
}

// localClock renders t in the user's zone for the "remind me later" notice.
func localClock(t time.Time) string {
	return t.Local().Format("Mon Jan 2 2006, 3:04 PM MST")
}

// ansiStrip and pangoStrip remove terminal colour codes and pango tags, for
// matching a rofi selection back to its option.
var ansiStrip = regexp.MustCompile(`\x1b\[[0-9;]*m`)

var pangoStrip = regexp.MustCompile(`<[^>]*>`)

var pangoEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func escapePango(s string) string { return pangoEscaper.Replace(s) }

// refreshUpdateStateFromGitHub reads the release on offer again, for a state
// an earlier launch stored: GitHub may have a newer release by now, and the
// stored notes were shortened. On any error the state is left as it was.
func refreshUpdateStateFromGitHub(pu *pendingUpdate, devBuilds bool, currentVersion string) {
	if pu == nil {
		return
	}
	release, latest, err := fetchUpdateRelease(DefaultUpdateRepo, devBuilds)
	switch {
	case err != nil:
		Log("Could not refresh release notes from GitHub: " + err.Error())
		return
	case latest == "":
		return
	}
	pu.recordRelease(release, latest)
	pu.ReleaseNotes = strings.TrimSpace(release.Body) // the full text this time
	pu.Changelog = updateChangelog(DefaultUpdateRepo, release, latest, currentVersion)
	pu.Available = true
}

// updateUserMessage prints a single status line. In rofi mode there is no
// terminal to print to, so it becomes one short desktop notification.
func updateUserMessage(config *Config, msg string) {
	if msg = strings.TrimSpace(msg); msg == "" {
		return
	}
	Log("update: " + msg)
	if config == nil || !config.RofiSelection {
		fmt.Println(msg)
		return
	}
	_ = sendLinuxNotification(AppName+"-update", "", msg)
}

// HandleLaunchUpdatePrompt offers an update found by this launch's check,
// waiting briefly for it; if the check is slower than that, it falls back to
// what a previous launch found. It shows at most once per run.
// prompted reports whether the prompt was on screen; exit whether the caller
// should exit (user updated or chose to quit the session).
func HandleLaunchUpdatePrompt(config *Config, currentVersion string) (prompted, exit bool) {
	launchUpdatePromptOnce.Do(func() {
		if !updateChecksEnabled(config) {
			return
		}
		if launchUpdateDone != nil {
			select {
			case <-launchUpdateDone:
			default:
				StartupStage("Checking for updates")
			}
		}
		fresh := waitForLaunchUpdateCheck(launchUpdateWait)
		prompted, exit = handlePendingUpdatePrompt(config, currentVersion, !fresh)
	})
	return prompted, exit
}

// handlePendingUpdatePrompt shows the stored update, if there is one to offer.
// refresh re-reads the release from GitHub first, for a result an earlier
// launch stored; this launch's own check is already current.
func handlePendingUpdatePrompt(config *Config, currentVersion string, refresh bool) (prompted, exit bool) {
	if !updateChecksEnabled(config) {
		return false, false
	}
	pu := readPendingUpdate(config.StoragePath)
	if !pendingUpdateShouldPrompt(config, currentVersion, pu) {
		return false, false
	}

	if refresh {
		// Notes from an old launch were shortened, and GitHub may have moved
		// on; fetch the release again so the prompt matches the release page.
		refreshUpdateStateFromGitHub(&pu, config.DevBuilds, currentVersion)
		stillOffered := pendingUpdateShouldPrompt(config, currentVersion, pu)
		// Typically: the refreshed release is what's already running.
		pu.Available = pu.Available && stillOffered
		_ = writePendingUpdate(config.StoragePath, pu)
		if !stillOffered {
			return false, false
		}
	}

	// Changelog and release-page rows come back to this menu; only a real
	// choice ends it.
	picked, err := chooseUpdateAction(config, currentVersion, pu)
	if err != nil {
		return true, false
	}
	return true, applyUpdateChoice(config, &pu, NormalizeSelectionKey(picked))
}

func chooseUpdateAction(config *Config, currentVersion string, state pendingUpdate) (SelectionOption, error) {
	if config.RofiSelection {
		return chooseUpdateActionRofi(currentVersion, state)
	}
	return promptUpdateTerminal(currentVersion, state)
}

// applyUpdateChoice carries out what was picked in the update prompt and
// reports whether the program should exit afterwards. Back, Continue and
// anything unknown leave the update for next time and carry on.
func applyUpdateChoice(config *Config, state *pendingUpdate, choice SelectionOption) (exit bool) {
	// Quit in the pinned menu means quit the program, not drop into the app.
	if SelectionMeansQuit(choice) {
		Exit(nil)
		return true
	}
	if SelectionMeansBack(choice) {
		return false
	}
	if choice.Key == "update" {
		return installOfferedUpdate(config, state)
	}

	notice, ok := updateChoiceEffect(config, state, choice.Key)
	if !ok {
		return false
	}
	_ = writePendingUpdate(config.StoragePath, *state)
	updateUserMessage(config, notice)
	return false
}

// updateChoiceEffect applies the non-install answers to state and returns the
// line to tell the user. ok is false for keys it doesn't know.
func updateChoiceEffect(config *Config, state *pendingUpdate, key string) (notice string, ok bool) {
	switch key {
	case "remind":
		at := time.Now().Add(remindLaterDelay)
		state.Available, state.RemindAfter = true, at.UTC().Format(time.RFC3339)
		return "Will remind again after " + localClock(at) + ".", true
	case "skip-version":
		state.Available, state.RemindAfter, state.SkippedVersion = false, "", state.LatestVersion
		return "Skipping version " + state.LatestVersion + ".", true
	case "stop-checks":
		state.Available = false
		werr := setConfigBoolOption(ConfigFilePath, "CheckUpdates", false)
		if werr != nil {
			Log("disable CheckUpdates: " + werr.Error())
			return "Could not write config: " + werr.Error(), true
		}
		config.CheckUpdates = false
		return "Automatic update checks disabled (CheckUpdates=false).", true
	}
	return "", false
}

// installOfferedUpdate downloads the release on offer over the running
// binary. The new binary only takes over on the next start, so a successful
// update ends the session.
func installOfferedUpdate(config *Config, state *pendingUpdate) bool {
	updateUserMessage(config, "Downloading and installing update…")
	if err := SelfUpdate(DefaultUpdateRepo, state.LatestTag); err != nil {
		updateUserMessage(config, "Update failed: "+err.Error())
		return false
	}
	state.Available, state.RemindAfter = false, ""
	_ = writePendingUpdate(config.StoragePath, *state)
	updateUserMessage(config, "Updated to "+state.LatestVersion+". Please restart otakase.")
	return true
}

// setConfigBoolOption writes one boolean to the config file. A key the file
// does not have yet is appended, so the rest of the file stays as written.
func setConfigBoolOption(configPath, key string, value bool) error {
	if strings.TrimSpace(configPath) == "" {
		return errors.New("config path is empty")
	}
	values, err := LoadConfigFromFile(configPath)
	if err != nil {
		return err
	}
	_, known := values[key]
	values[key] = strconv.FormatBool(value)
	if known {
		return SaveConfigToFile(configPath, values)
	}
	return appendConfigKeys(configPath, values, []string{key})
}
