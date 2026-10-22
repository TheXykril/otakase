package internal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/cast"
	"github.com/thexykril/otakase/internal/icons"
)

// The CastBrowserSubtitles values.
const (
	CastBrowserSubtitlesSoft = "soft"
	CastBrowserSubtitlesBurn = "burn"
)

// castBrowserViewerWait is how long a browser cast waits for the link to be
// opened before giving up.
var castBrowserViewerWait = 10 * time.Minute

// castBrowserViewerPoll is how often that wait looks.
var castBrowserViewerPoll = 250 * time.Millisecond

// castBrowserOption is the browser entry in the device pickers.
var castBrowserOption = SelectionOption{
	Key:   cast.BrowserDeviceUUID,
	Label: cast.Device{Name: cast.BrowserDeviceName, Kind: cast.KindBrowser}.String(),
	Icon:  icons.OpenLink,
}

// castIsBrowser reports whether a device is the browser entry.
func castIsBrowser(device cast.Device) bool {
	return device.Kind == cast.KindBrowser
}

// castBrowserFirewallPort is the port the browser player's page needs let in,
// or 0 when the browser entry is off.
func castBrowserFirewallPort(config *Config) int {
	if config == nil || !config.CastBrowser {
		return 0
	}
	if config.CastBrowserPort > 0 {
		return config.CastBrowserPort
	}
	return cast.DefaultBrowserPort
}

// castBrowserStatus passes a panel message on to the browser player while a
// browser cast is on, so the page says what the terminal says.
func castBrowserStatus(message string) {
	if castRemoteControls.Load() {
		cast.BrowserStatus(message)
	}
}

// castBrowserNamed reports whether a device name picks the browser entry.
func castBrowserNamed(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), cast.BrowserDeviceName)
}

// castBrowserSetup tells package cast how to serve the player page and where
// the page's buttons go.
func castBrowserSetup(config *Config) {
	if config == nil {
		return
	}
	cast.SetBrowser(cast.BrowserSettings{Port: config.CastBrowserPort, Token: castBrowserToken(config)})
	cast.SetBrowserCommands(castBrowserCommand)
}

// castBrowserToken is the secret part of the page's link, kept in the storage
// folder so the link is the same every run: a TV can leave the page open or
// bookmark it.
func castBrowserToken(config *Config) string {
	path := filepath.Join(os.ExpandEnv(config.StoragePath), "cast-browser-token")
	if saved, err := os.ReadFile(path); err == nil {
		if token := strings.TrimSpace(string(saved)); token != "" {
			return token
		}
	}
	token := cast.NewBrowserToken()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
		if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
			Log(fmt.Sprintf("cast: could not save the browser link: %v", err))
		}
	}
	return token
}

// castBrowserCommand turns a press on the page into the key the terminal would
// have sent, so the page and the keyboard drive the cast the same way.
func castBrowserCommand(name string) {
	command := map[string]castCommand{
		"toggle":  castCmdPauseToggle,
		"back":    castCmdSeekBack,
		"forward": castCmdSeekForward,
		"volup":   castCmdVolumeUp,
		"voldown": castCmdVolumeDown,
		"stop":    castCmdStop,
	}[name]
	if command != castCmdNone {
		castDeliverCommand(command)
	}
}

// castBrowserLinkMessage is what the panel shows while it waits for the link
// to be opened: the link, and a code to scan it with when there is a panel to
// draw one in.
func castBrowserLinkMessage(link string, withCode bool) string {
	lines := []string{
		"Open this on a phone, TV or console on the same network:",
		link,
	}
	if withCode {
		if code := cast.QRLines(link); code != nil {
			lines = append(lines, "")
			lines = append(lines, code...)
		}
	}
	return strings.Join(lines, "\n")
}

// castAwaitBrowserViewer holds a browser cast until a page has the link open,
// showing the link meanwhile. A page already open -- the episode before this
// one, or a TV that kept it -- lets it straight through.
func castAwaitBrowserViewer(link string, withCode bool, status func(string)) error {
	if cast.BrowserViewers() > 0 {
		return nil
	}
	status(castBrowserLinkMessage(link, withCode))
	if !withCode {
		// Without a panel the message is a notification, which may be gone
		// before anyone types the link; the log keeps it.
		Log("cast: browser link " + link)
	}
	deadline := time.Now().Add(castBrowserViewerWait)
	for cast.BrowserViewers() == 0 {
		if !time.Now().Before(deadline) {
			return fmt.Errorf("cast: nobody opened %s in %s", link, castBrowserViewerWait)
		}
		time.Sleep(castBrowserViewerPoll)
	}
	return nil
}

// castBrowserSoftSubtitles reports whether a browser cast hands the page the
// subtitle file rather than burning it in.
func castBrowserSoftSubtitles(config *Config) bool {
	return config == nil || !strings.EqualFold(strings.TrimSpace(config.CastBrowserSubtitles), CastBrowserSubtitlesBurn)
}

// castBrowserSubtitleName is the WebVTT file a browser cast serves, beside the
// stream.
const castBrowserSubtitleName = "browser-subtitles.vtt"

// castBrowserSubtitleFile fetches a provider's subtitles and turns them into
// WebVTT, the one format every browser draws, in dir. It returns the file's
// name there.
func castBrowserSubtitleFile(ffmpeg, subtitleURL, referrer, dir string) (string, error) {
	fetched, err := fetchCastSubtitle(subtitleURL, referrer, dir)
	if err != nil {
		return "", err
	}
	out := filepath.Join(dir, castBrowserSubtitleName)
	if strings.EqualFold(filepath.Ext(fetched), ".vtt") {
		if err := os.Rename(fetched, out); err != nil {
			return "", fmt.Errorf("cast: could not keep the subtitles: %w", err)
		}
		return castBrowserSubtitleName, nil
	}
	// ffmpeg reads ASS, SRT and the rest and writes WebVTT by the extension.
	// The release's styling does not survive: WebVTT has no fonts, signs or
	// positions to carry it in.
	command := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-i", fetched, out)
	if output, err := command.CombinedOutput(); err != nil {
		return "", fmt.Errorf("cast: could not convert the subtitles for the browser: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return castBrowserSubtitleName, nil
}

// castPlay hands the device a stream that starts start seconds into the
// episode. A browser is told where, to line the subtitles up with it; a
// receiver only needs the stream.
func castPlay(session cast.Player, url string, start float64) error {
	if browser, ok := session.(*cast.BrowserPlayer); ok {
		return browser.PlayFrom(url, start)
	}
	return session.Play(url)
}

// castBrowserTitle is the line the page shows over the video.
func castBrowserTitle(anime *Anime) string {
	if anime == nil {
		return ""
	}
	title := GetAnimeName(*anime)
	if anime.Movie == nil && anime.Ep.Number > 0 {
		title += fmt.Sprintf(" · Episode %d", anime.Ep.Number)
	}
	return title
}
