package internal

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/thexykril/otakase/internal/mpvskin"
	"github.com/thexykril/otakase/internal/theme"
)

// mpvSkinSockets are the players started with the skin, which are the ones
// worth sending its state to.
var (
	mpvSkinSocketsMu sync.Mutex
	mpvSkinSockets   = map[string]bool{}
)

// mpvSkinArgs are the flags that give the mpv at binary otakase's look, or nil
// when it keeps its own: MpvSkin is off, the player is not mpv or too old for
// it, or the user already has a look of their own.
func mpvSkinArgs(config *Config, binary string) []string {
	if config == nil {
		return nil
	}
	mode := mpvskin.ParseMode(config.MpvSkin)
	if mode == mpvskin.ModeOff || !mpvskin.IsMPVBinary(binary) {
		return nil
	}
	if mpvskin.ArgsOverride(config.MpvArgs) {
		Log("mpv skin: MpvArgs set the player's controls; leaving them alone")
		return nil
	}
	user := mpvskin.ReadUserConfig(mpvskin.ConfigDirs(binary))
	if mode == mpvskin.ModeAuto && user.OwnSkin != "" {
		Log(fmt.Sprintf("mpv skin: the player already has its own look (%s); leaving it alone", user.OwnSkin))
		return nil
	}
	// The Windows build carries its own mpv, new enough and not worth asking:
	// a GUI mpv.exe may print its version nowhere a pipe can read it.
	bundled, _ := getBundledMPVPath()
	if !samePath(binary, bundled) && !mpvskin.BinarySupported(binary) {
		Log(fmt.Sprintf("mpv skin: %s is older than mpv %d.%d or its version could not be read", binary, mpvskin.MinMPV[0], mpvskin.MinMPV[1]))
		return nil
	}
	dir, err := mpvskin.Install(config.StoragePath)
	if err != nil {
		Log(fmt.Sprintf("mpv skin: could not write its files: %v", err))
		return nil
	}

	opts := mpvskin.Options{
		Dir:    dir,
		Colors: mpvSkinColors(theme.Active()),
		SkipOp: config.SkipOp,
		SkipEd: config.SkipEd,
		// The skip times menu sends what the Alt keys send; nothing answers
		// it unless the marker runs.
		Contribute: config.ContributeSkipTimes,
	}
	if !user.SetsOSDFont && !hasMPVFlag(config.MpvArgs, "--osd-font") {
		if font := theme.MonospaceFont(); font != "" && font != "monospace" {
			opts.Font = font
		}
	}
	return mpvskin.Args(opts)
}

func mpvSkinColors(p theme.Palette) mpvskin.Colors {
	return mpvskin.Colors{
		Background: p.Background,
		Surface:    p.Surface(),
		Foreground: p.Foreground,
		Bright:     p.BrightForeground,
		Dim:        p.MetaText(),
		Accent:     p.Accent,
		AccentText: theme.ReadableOn(p.Accent, p.Background, p.BrightForeground, "#000000", "#ffffff"),
		Highlight:  p.ResumeText(),
	}
}

func hasMPVFlag(args []string, flag string) bool {
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == flag || strings.HasPrefix(arg, flag+"=") {
			return true
		}
	}
	return false
}

func markMPVSkinSocket(socket string) {
	mpvSkinSocketsMu.Lock()
	mpvSkinSockets[socket] = true
	mpvSkinSocketsMu.Unlock()
}

func mpvSkinActive(socket string) bool {
	mpvSkinSocketsMu.Lock()
	defer mpvSkinSocketsMu.Unlock()
	return mpvSkinSockets[socket]
}

type mpvSkinChip struct {
	Icon string `json:"icon"`
	Text string `json:"text"`
}

// mpvSkinChips are the chips under the player's top bar: where progress is
// tracked and which source is playing. Icon names are Material Icons ligatures.
func mpvSkinChips(config *Config, anime *Anime) []mpvSkinChip {
	var chips []mpvSkinChip
	switch {
	case UsesAniListTracking(config) && UsesMyAnimeListTracking(config):
		chips = append(chips, mpvSkinChip{"sync", "AniList + MAL"})
	case UsesAniListTracking(config):
		chips = append(chips, mpvSkinChip{"sync", "AniList"})
	case UsesMyAnimeListTracking(config):
		chips = append(chips, mpvSkinChip{"sync", "MyAnimeList"})
	}
	if anime != nil && anime.SkipRemoteSync {
		chips = nil
	}
	if provider := strings.TrimSpace(CurrentAnimeProviderName(anime)); provider != "" && !isAdultProvider(provider) {
		chips = append(chips, mpvSkinChip{"dns", provider})
	}
	return chips
}

// mpvSkinEpisodeTitle is the episode's own name for the top bar, from Jikan:
// English when it has one, else romaji.
func mpvSkinEpisodeTitle(anime *Anime) string {
	if anime == nil {
		return ""
	}
	if title := strings.TrimSpace(anime.Ep.Title.English); title != "" {
		return title
	}
	return strings.TrimSpace(anime.Ep.Title.Romaji)
}

// SendMPVSkinState tells the skin in the player at socket what it shows that
// only otakase knows. Players without the skin are skipped. withTitle sends
// the episode title too; leave it off when anime.Ep may still describe the
// previous episode, so the skin keeps what it has rather than show a stale one.
func SendMPVSkinState(config *Config, anime *Anime, socket string, withTitle bool) {
	if socket == "" || !mpvSkinActive(socket) {
		return
	}
	state := struct {
		Chips        []mpvSkinChip `json:"chips"`
		EpisodeTitle *string       `json:"episode_title,omitempty"`
	}{Chips: mpvSkinChips(config, anime)}
	if withTitle {
		title := mpvSkinEpisodeTitle(anime)
		state.EpisodeTitle = &title
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return
	}
	// Right after mpv starts the script may not have registered yet, and a
	// message to a client mpv does not know yet is refused.
	var sendErr error
	for attempt := 0; attempt < 6; attempt++ {
		if attempt > 0 {
			time.Sleep(500 * time.Millisecond)
		}
		if _, sendErr = MPVSendCommand(socket, []interface{}{"script-message-to", mpvskin.ScriptName, "otakase-state", string(payload)}); sendErr == nil {
			return
		}
	}
	Log(fmt.Sprintf("mpv skin: could not send its state: %v", sendErr))
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
