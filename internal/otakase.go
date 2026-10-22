package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gen2brain/beeep"

	"github.com/thexykril/otakase/internal/appicon"
	"github.com/thexykril/otakase/internal/torrentstream"
)

// inAppScreen is set while EnterAppScreen has the terminal on its
// alternate buffer, so LeaveAppScreen only switches back once.
var inAppScreen bool

// LogFileName is the debug log inside the storage directory.
//
// It carries the program's name rather than being a bare debug.log, because it
// leaves that directory constantly: it is the first thing a bug report asks
// for, and a file called debug.log in someone's downloads folder says nothing
// about what produced it.
const LogFileName = "otakase-debug.log"

// legacyLogFileName is what the log was called before it carried a name.
const legacyLogFileName = "debug.log"

// RemoveLegacyLogFile deletes a log left by an older version, so the storage
// directory does not keep a stale file that looks like the current one.
func RemoveLegacyLogFile(storageDir string) {
	storageDir = strings.TrimSpace(storageDir)
	if storageDir == "" {
		return
	}
	if err := os.Remove(filepath.Join(storageDir, legacyLogFileName)); err != nil && !os.IsNotExist(err) {
		Log(fmt.Sprintf("Could not remove the old debug log: %v", err))
	}
}

// ClearLogFile empties the log at path, creating it and its folder when
// they are missing. Every launch starts a fresh log.
func ClearLogFile(path string) error {
	// Drop any cached append handle first: it still points at the pre-truncation
	// file description, and writing through it would restore the old length.
	if err := CloseLogFile(); err != nil {
		return fmt.Errorf("failed to close log file: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating the log folder: %w", err)
	}
	if err := os.WriteFile(path, nil, 0o666); err != nil {
		return fmt.Errorf("emptying the log: %w", err)
	}
	return nil
}

// Log appends data to the debug log as JSON, stamped with the time and the
// file and line it was logged from.
func Log(data any) error {
	encoded, marshalErr := json.Marshal(data)
	if marshalErr != nil {
		return marshalErr
	}
	// Skip 1 is the caller of Log, which is the line worth knowing.
	_, file, line, found := runtime.Caller(1)
	if !found {
		return errors.New("unable to get caller information")
	}
	stamp := time.Now().Format("2006/01/02 15:04:05")
	return writeLogLine(fmt.Sprintf("[LOG] %s %s:%d: %s\n", stamp, file, line, encoded))
}

// EnterAppScreen moves the terminal to its alternate buffer, which the menus draw
// on, so the shell's scrollback is left as it was. AlternateScreen=false turns
// it off.
func EnterAppScreen() {
	if config := GetGlobalConfig(); config != nil && config.AlternateScreen {
		// Alternate buffer, clear it, cursor home.
		fmt.Print("\033[?1049h\033[2J\033[H")
		inAppScreen = true
	}
}

// LeaveAppScreen leaves the alternate buffer, if EnterAppScreen entered it.
func LeaveAppScreen() {
	if inAppScreen {
		fmt.Print("\033[?1049l")
		inAppScreen = false
	}
}

func Exit(err error) {
	LeaveAppScreen()

	// Quitting during a slow launch must not leave a "starting..." notification
	// on screen describing something that is no longer happening.
	EndStartupProgress()
	EndAllBusy()

	// Torrent-backed playback keeps a client and a temporary cache directory
	// alive for the session. Leaving either behind would mean a stray port and a
	// download the user never asked to keep.
	torrentstream.Shutdown()

	// An mpv still open would outlive the program. What closing it returned
	// is what gets reported below, in place of err.
	socketPath := playingSocket()
	if socketPath != "" {
		err = ExitMPV(socketPath)
	}

	// Credits roll after the player has gone, not over it.
	if err == nil {
		celebrateMilestoneOnExit(GetGlobalConfig())
	}

	// A real terminal gets a one-line goodbye; rofi does not. otakase launched
	// from rofi is a fresh process per action -- opening the menu, handing off
	// a cast, adding a show -- and each one reaches this same Exit(), so a
	// desktop notification here would fire after every one of them, not only
	// when someone is actually done with the tool. Out() would turn this into
	// exactly that notification whenever RofiSelection is set, so it is
	// skipped there rather than shown and immediately regretted.
	if exitGreetingWanted(GetGlobalConfig()) {
		Out("Have a great day!")
	}

	if !exitErrorWorthShowing(err, socketPath) {
		restoreAndExit(0)
	}
	Out(fmt.Sprintf("Error: %v", err))
	// A console window on Windows closes with the program, taking the
	// message with it.
	if runtime.GOOS == "windows" {
		fmt.Println("Press Enter to exit")
		AwaitEnter()
	}
	restoreAndExit(1)
}

// playingSocket is the mpv socket of the episode playing, "" when none is.
func playingSocket() string {
	if anime := GetGlobalAnime(); anime != nil {
		return anime.Ep.Player.SocketPath
	}
	return ""
}

// exitErrorWorthShowing reports whether Exit should print err and fail. An mpv
// that had already gone refuses the connection, which is no error at all.
func exitErrorWorthShowing(err error, socketPath string) bool {
	if err == nil {
		return false
	}
	return socketPath == "" || !strings.Contains(err.Error(), "dial unix "+socketPath+": connect: connection refused")
}

// exitGreetingWanted answers whether "Have a great day!" is worth showing on
// this exit.
//
// A real terminal keeps it -- one harmless line at the end of an interactive
// session. Rofi mode drops it: rofi launches otakase fresh for every action,
// so every one of those routine exits would otherwise surface as a desktop
// notification, which reads as otakase saying goodbye after something as
// small as opening the menu.
//
// A nil config -- Exit() reachable before SetGlobalConfig has run -- keeps the
// greeting: RofiSelection defaults false, and Out() treats a nil config the
// same way.
func exitGreetingWanted(cfg *Config) bool {
	return cfg == nil || !cfg.RofiSelection
}

func Out(data interface{}) {
	userConfig := GetGlobalConfig()
	if userConfig == nil {
		userConfig = &Config{}
	}
	busyNoteOutput()
	message := fmt.Sprintf("%v", data)
	// While the cast panel owns the screen, the terminal shows the panel and
	// nothing else. A message printed into it would either land inside the
	// frame or scroll it away, so it goes where a launch with no terminal
	// already sends its messages: a desktop notification.
	if castPanelOwnsScreen() {
		notifyDesktop(message)
		Log(message)
		return
	}

	if !userConfig.RofiSelection {
		// Raw mode, which the cast controls turn on, disables the translation
		// that makes \n also return the cursor. A bare newline there leaves the
		// next line starting wherever the last one ended, and the output walks
		// diagonally across the screen.
		if castRawModeActive() {
			fmt.Print(message + "\r\n")
		} else {
			fmt.Println(message)
		}
		return
	}

	// Launched from rofi there is no terminal to print to.
	switch runtime.GOOS {
	case "windows":
		notifyDesktop(message)
	case "linux":
		icon := ""
		if image, text, ok := splitImageNotice(message); ok && userConfig.ImagePreview {
			icon, message = image, text
		} else if strings.HasPrefix(message, "-i") && userConfig.ImagePreview {
			// Meant to carry a cover but malformed; dropped rather than
			// shown with the -i in it.
			return
		}
		if err := sendLinuxNotification(notifyTagMain, icon, message); err != nil {
			Log(fmt.Sprintf("Failed to send notification: %v", err))
		}
	}
}

// imageNotice is a message for Out that, in rofi mode with image previews on,
// is shown as a notification with the picture at imagePath as its icon.
func imageNotice(imagePath, text string) string {
	return fmt.Sprintf("-i %s \"%s\"", imagePath, text)
}

// splitImageNotice takes an imageNotice apart. The path is the second word,
// so it cannot contain a space.
func splitImageNotice(message string) (imagePath, text string, ok bool) {
	if !strings.HasPrefix(message, "-i") {
		return "", "", false
	}
	parts := strings.SplitN(message, " ", 3)
	if len(parts) != 3 {
		return "", "", false
	}
	return parts[1], strings.Trim(parts[2], "\""), true
}

// notifyDesktop sends a desktop notification, which is how otakase reaches a
// viewer who is not looking at a terminal -- either because there is none, or
// because the cast panel owns the one there is.
func notifyDesktop(message string) {
	if runtime.GOOS == "linux" {
		if err := sendLinuxNotification(notifyTagMain, "", message); err != nil {
			Log(fmt.Sprintf("Failed to send notification: %v", err))
		}
		return
	}
	if err := beeep.Notify(DisplayName, message, appicon.Path()); err != nil {
		Log(fmt.Sprintf("Failed to send notification: %v", err))
	}
}
