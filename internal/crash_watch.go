package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thexykril/otakase/internal/icons"
)

// Noticing a crash. Every interactive run leaves a small "still running"
// marker in the storage directory, and Go writes a crash's stack beside it;
// a clean exit removes both. A marker whose process is gone at the next
// launch means that run did not end well (a panic, or the process was
// killed), and that launch asks once whether to send a report to the
// Otakase Discord, through the usual report flow with the crash filled in.

const (
	crashDirName      = "crash"
	pendingCrashFile  = "pending.json"
	crashStackMax     = 3000
	crashForgetAfter  = 30 * 24 * time.Hour
	staleMarkerMaxAge = 7 * 24 * time.Hour
)

// runMarker says a run is going on.
type runMarker struct {
	PID     int       `json:"pid"`
	Started time.Time `json:"started"`
	Version string    `json:"version,omitempty"`
}

// pendingCrash is a run that ended badly and has not been asked about yet.
type pendingCrash struct {
	At      time.Time `json:"at"` // when that run started
	Version string    `json:"version,omitempty"`
	Stack   string    `json:"stack,omitempty"`
}

// crashProcessAlive is processAlive; tests replace it.
var crashProcessAlive = processAlive

var crashWatch struct {
	sync.Mutex
	marker string
	output *os.File
}

func crashDir(config *Config) string {
	storage := ""
	if config != nil {
		storage = os.ExpandEnv(config.StoragePath)
	}
	if strings.TrimSpace(storage) == "" {
		storage = GetStoragePath()
	}
	return filepath.Join(storage, crashDirName)
}

func markerPath(dir string, pid int) string {
	return filepath.Join(dir, "running-"+strconv.Itoa(pid)+".json")
}

func crashOutputPath(dir string, pid int) string {
	return filepath.Join(dir, "crash-"+strconv.Itoa(pid)+".txt")
}

// StartCrashWatch notes runs that ended badly since the last launch, then
// marks this one as running and sends a crash's stack to a file beside the
// marker. EndCrashWatch, or a clean exit, takes both away again.
func StartCrashWatch(config *Config) {
	dir := crashDir(config)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		Log(fmt.Sprintf("Crash watch: %v", err))
		return
	}
	collectCrashes(dir, os.Getpid(), time.Now())

	pid := os.Getpid()
	data, _ := json.Marshal(runMarker{PID: pid, Started: time.Now().UTC(), Version: Version()})
	if err := os.WriteFile(markerPath(dir, pid), data, 0o600); err != nil {
		Log(fmt.Sprintf("Crash watch: %v", err))
		return
	}
	crashWatch.Lock()
	defer crashWatch.Unlock()
	crashWatch.marker = markerPath(dir, pid)
	if f, err := os.OpenFile(crashOutputPath(dir, pid), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600); err == nil {
		if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
			Log(fmt.Sprintf("Crash watch: %v", err))
		}
		crashWatch.output = f
	}
}

// EndCrashWatch marks this run as ended well. It runs on every exit.
func EndCrashWatch() {
	crashWatch.Lock()
	defer crashWatch.Unlock()
	if crashWatch.output != nil {
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
		name := crashWatch.output.Name()
		_ = crashWatch.output.Close()
		_ = os.Remove(name)
		crashWatch.output = nil
	}
	if crashWatch.marker != "" {
		_ = os.Remove(crashWatch.marker)
		crashWatch.marker = ""
	}
}

// CrashGuard is deferred at the top of main: a panic keeps the marker (and
// carries on, so its stack reaches the crash file and the terminal); a
// normal return ends the watch.
func CrashGuard() {
	if r := recover(); r != nil {
		LeaveAppScreen()
		panic(r)
	}
	EndCrashWatch()
}

// collectCrashes turns the markers of runs that are gone into the pending
// crash (the newest one, when there were several) and clears them away.
func collectCrashes(dir string, self int, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var found *pendingCrash
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "running-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "running-"), ".json"))
		if err != nil || pid == self {
			continue
		}
		var m runMarker
		data, _ := os.ReadFile(filepath.Join(dir, name))
		_ = json.Unmarshal(data, &m)
		alive := crashProcessAlive(pid)
		if alive && now.Sub(m.Started) < staleMarkerMaxAge {
			continue // another otakase that is still running
		}
		stack, _ := os.ReadFile(crashOutputPath(dir, pid))
		_ = os.Remove(filepath.Join(dir, name))
		_ = os.Remove(crashOutputPath(dir, pid))
		if alive || m.Started.IsZero() {
			continue // its id belongs to something else by now; too old to ask about
		}
		c := pendingCrash{At: m.Started, Version: m.Version, Stack: strings.TrimSpace(string(stack))}
		if found == nil || c.At.After(found.At) {
			found = &c
		}
	}
	if found == nil {
		return
	}
	data, _ := json.Marshal(found)
	if err := os.WriteFile(filepath.Join(dir, pendingCrashFile), data, 0o600); err != nil {
		Log(fmt.Sprintf("Crash watch: %v", err))
	}
	Log(fmt.Sprintf("The run started at %s did not exit cleanly", found.At.Local().Format(time.DateTime)))
}

// takePendingCrash returns the crash to ask about and forgets it, so it is
// asked about once.
func takePendingCrash(dir string, now time.Time) (pendingCrash, bool) {
	path := filepath.Join(dir, pendingCrashFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return pendingCrash{}, false
	}
	_ = os.Remove(path)
	var c pendingCrash
	if json.Unmarshal(data, &c) != nil || now.Sub(c.At) > crashForgetAfter {
		return pendingCrash{}, false
	}
	return c, true
}

// crashReportText is the title and description a crash report starts with.
func crashReportText(c pendingCrash, note string) (title, description string) {
	title = "Otakase closed unexpectedly"
	if line, _, _ := strings.Cut(c.Stack, "\n"); strings.HasPrefix(line, "panic: ") || strings.HasPrefix(line, "fatal error: ") {
		title += ": " + strings.TrimSpace(line)
	}
	var b strings.Builder
	version := c.Version
	if version == "" {
		version = "unknown version"
	}
	fmt.Fprintf(&b, "Otakase (%s, %s/%s) closed unexpectedly in a run started %s.",
		version, runtime.GOOS, runtime.GOARCH, c.At.Local().Format("2006-01-02 15:04"))
	if note = strings.TrimSpace(note); note != "" {
		b.WriteString("\n\nWhat I was doing: " + note)
	}
	if c.Stack != "" {
		b.WriteString("\n\n```\n" + clipRunes(c.Stack, crashStackMax) + "\n```")
	} else {
		b.WriteString("\n\nNo crash message was saved: the process may have been killed or the computer shut down.")
	}
	return clipRunes(title, reportTitleMax), b.String()
}

// crashPromptWanted reports whether this launch can ask: someone is at a
// terminal or rofi, Discord features are on and this is no handed-off cast.
func crashPromptWanted(config *Config) bool {
	if config == nil || config.CastNonInteractive || !discordEnabled(config) {
		return false
	}
	return config.RofiSelection || stdinIsTerminal()
}

// OfferCrashReport asks once about a run that ended badly, and sends the
// report when the user wants to. It reports whether it showed anything.
func OfferCrashReport(config *Config) bool {
	if !crashPromptWanted(config) {
		return false
	}
	crash, ok := takePendingCrash(crashDir(config), time.Now())
	if !ok {
		return false
	}
	selected, err := selectWithOptionalMessage(config, []SelectionOption{
		{Key: "send", Label: "Yes, send a report · with the log and app details", Icon: icons.Yes},
		{Key: "no", Label: "No thanks", Icon: icons.No},
	}, "Otakase", "Otakase closed unexpectedly last time. Send a report to the Otakase Discord?")
	if err != nil {
		Log(fmt.Sprintf("Crash prompt: %v", err))
		return true
	}
	if selected = NormalizeSelectionKey(selected); SelectionMeansQuit(selected) {
		Exit(nil)
	}
	if selected.Key == "send" {
		said := false
		ui := menuReportUI{config: config, said: &said}
		EnterAppScreen()
		note, _ := ui.ask("What were you doing when it closed?", "Optional · Enter to skip")
		title, description := crashReportText(crash, note)
		deliverReport(config, ui, newProblemReport(config, title, description, reportLogPath(true)))
		if said {
			pauseAfterNotice(config)
		}
	}
	EnterAppScreen()
	return true
}
