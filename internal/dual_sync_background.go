package internal

import (
	"fmt"
	"sync"
	"time"
)

// sleepBetweenRemoteWrites paces the background writes. Both trackers are rate
// limited, and MyAnimeList answers a throttled write with a redirect that never
// completes, so pacing is what keeps a large catch-up from stalling outright.
// Overridable so tests do not spend real seconds.
var sleepBetweenRemoteWrites = func() { time.Sleep(remoteTrackerWriteDelay) }

// Reconciling two trackers costs one paced write per entry they disagree on,
// and that was happening before the first menu could appear. On a real library
// the disagreement is not small and does not shrink: 78 entries the user has on
// AniList but not on MyAnimeList were re-sent on every launch, roughly half a
// minute of waiting to push a planning list nobody was about to watch.
//
// The merge itself is local and instant. Only the writes are slow, and nothing
// on screen depends on them having finished -- the merged list is already what
// otakase will show. So the writes run behind the menu instead of in front of it.
//
// Ordering is safe in practice: these are catch-up writes for entries that
// differed at launch, and anything the user does this session is written later
// by definition, so a background write cannot overwrite a fresher value.

// dualSyncWrites is the work a reconciliation found, separated from the merged
// list it produced so the two can happen at different times.
type dualSyncWrites struct {
	aniListToken string
	aniList      []Entry
	myAnimeList  []Entry
}

func (w dualSyncWrites) count() int {
	return len(w.aniList) + len(w.myAnimeList)
}

var (
	dualSyncMu      sync.Mutex
	dualSyncRunning bool
	dualSyncDone    chan struct{}
)

// startDualSyncWrites performs the queued writes in the background. A second
// call while one is running is ignored rather than queued: the next launch
// reconciles from scratch anyway, so duplicating in-flight work only doubles
// the rate limit pressure.
func startDualSyncWrites(config *Config, writes dualSyncWrites) {
	if config == nil || writes.count() == 0 {
		return
	}

	dualSyncMu.Lock()
	if dualSyncRunning {
		dualSyncMu.Unlock()
		Log("Dual sync: writes already in flight; skipping this round")
		return
	}
	dualSyncRunning = true
	done := make(chan struct{})
	dualSyncDone = done
	dualSyncMu.Unlock()

	Log(fmt.Sprintf("Dual sync: writing %d update(s) in the background", writes.count()))

	go func() {
		defer func() {
			dualSyncMu.Lock()
			dualSyncRunning = false
			dualSyncMu.Unlock()
			close(done)
		}()
		runDualSyncWrites(config, writes)
	}()
}

// WaitForDualSyncWrites blocks until any in-flight background sync has
// finished. Only tests need this; playback never waits on it.
func WaitForDualSyncWrites() {
	dualSyncMu.Lock()
	done := dualSyncDone
	running := dualSyncRunning
	dualSyncMu.Unlock()
	if running && done != nil {
		<-done
	}
}

func runDualSyncWrites(config *Config, writes dualSyncWrites) {
	failures := 0

	for index, entry := range writes.aniList {
		if index > 0 {
			sleepBetweenRemoteWrites()
		}
		if err := saveAniListTrackedEntry(writes.aniListToken, entry); err != nil {
			Log(fmt.Sprintf("Dual sync: AniList update for %q failed: %v", mediaDisplayTitle(entry.Media, config), err))
			failures++
		}
	}

	for index, entry := range writes.myAnimeList {
		if index > 0 {
			sleepBetweenRemoteWrites()
		}
		if err := saveMyAnimeListTrackedEntry(config, entry); err != nil {
			// One entry that cannot be written must not stop the rest: an anime
			// missing from MyAnimeList, or a single rejected write, used to abort
			// the whole sync.
			Log(fmt.Sprintf("Dual sync: MyAnimeList update for %q failed: %v", mediaDisplayTitle(entry.Media, config), err))
			failures++
		}
	}

	if failures == 0 {
		Log(fmt.Sprintf("Dual sync: %d update(s) written", writes.count()))
		return
	}

	// Worth saying out loud only when most of it failed. A couple of entries
	// MyAnimeList will not accept is normal and not worth a notification on
	// every launch; wholesale failure means something is actually wrong.
	Log(fmt.Sprintf("Dual sync: %d of %d update(s) failed", failures, writes.count()))
	if failures*2 > writes.count() {
		Out(fmt.Sprintf("Dual tracking: %d of %d updates could not be synced; see the log.",
			failures, writes.count()))
	}
}
