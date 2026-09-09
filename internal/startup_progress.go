package internal

import (
	"fmt"
	"sync"
	"time"
)

// Launched from a keybind rather than a terminal, Curd gives no sign it is
// running until its first menu appears. Refreshing a tracker token and pulling
// a large list can take several seconds, during which the only honest reading
// of a silent desktop is "did that even start?" -- so people press the key
// again, and now two copies are racing.
//
// A notification fills that gap, but only when there is a gap worth filling: a
// launch that reaches the menu quickly should stay silent rather than flash a
// message nobody had time to read. The elapsed seconds tick so a slow start
// still looks alive rather than hung.

const (
	// startupQuietPeriod is how long a launch may take before it owes the user
	// an explanation. Below this the menu is effectively immediate.
	startupQuietPeriod = 1200 * time.Millisecond
	// startupTickInterval refreshes the notification so it neither expires
	// mid-wait nor sits there looking frozen.
	startupTickInterval = 3 * time.Second
)

type startupProgress struct {
	mu      sync.Mutex
	stage   string
	started time.Time
	done    chan struct{}
	closed  bool
	// announced records whether anything was shown, so Done knows if there is a
	// notification to replace.
	announced bool
}

var (
	startupMu       sync.Mutex
	activeStartup   *startupProgress
	startupReporter = func(message string) { CurdOut(message) }
)

// BeginStartupProgress starts reporting a slow launch. It is a no-op when Curd
// is running in a terminal, where its output is already visible.
func BeginStartupProgress(config *CurdConfig, stage string) {
	if config == nil || !config.RofiSelection {
		return
	}

	startupMu.Lock()
	defer startupMu.Unlock()
	if activeStartup != nil {
		return
	}

	progress := &startupProgress{
		stage:   stage,
		started: time.Now(),
		done:    make(chan struct{}),
	}
	activeStartup = progress
	go progress.run()
}

// StartupStage updates what the launch is currently waiting on.
func StartupStage(stage string) {
	startupMu.Lock()
	progress := activeStartup
	startupMu.Unlock()
	if progress == nil {
		return
	}
	progress.mu.Lock()
	progress.stage = stage
	progress.mu.Unlock()
}

// EndStartupProgress stops reporting. It is safe to call more than once, and is
// called from the menus themselves: a visible menu is its own proof that Curd
// started, and any message still on screen at that point is stale.
func EndStartupProgress() {
	startupMu.Lock()
	progress := activeStartup
	activeStartup = nil
	startupMu.Unlock()
	if progress == nil {
		return
	}

	progress.mu.Lock()
	defer progress.mu.Unlock()
	if progress.closed {
		return
	}
	progress.closed = true
	close(progress.done)

	// Only worth a closing word if something was said in the first place;
	// otherwise the launch was quick and silence is the right outcome.
	if progress.announced {
		startupReporter("Ready.")
	}
}

func (p *startupProgress) run() {
	select {
	case <-p.done:
		return
	case <-time.After(startupQuietPeriod):
	}

	ticker := time.NewTicker(startupTickInterval)
	defer ticker.Stop()

	p.announce()
	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			p.announce()
		}
	}
}

func (p *startupProgress) announce() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.announced = true
	stage := p.stage
	elapsed := int(time.Since(p.started).Seconds())
	p.mu.Unlock()

	startupReporter(fmt.Sprintf("%s (%ds)", stage, elapsed))
}
