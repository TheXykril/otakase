package internal

// PlaybackLossAction is the monitor's response when time-pos or has-playback
// stops answering.
type PlaybackLossAction int

const (
	// PlaybackLossWait means the gap is expected (playlist switch, demuxer
	// reload, buffering); keep polling.
	PlaybackLossWait PlaybackLossAction = iota
	// PlaybackLossComplete means the episode got past the completion
	// threshold, so it counts as watched however playback ended.
	PlaybackLossComplete
	// PlaybackLossExit means the viewer closed mpv before the threshold.
	PlaybackLossExit
)

// ClassifyPlaybackLoss maps a lost time-pos to a PlaybackLossAction.
//
// mpv answers "property unavailable" while it swaps playlist entries, and
// that must not end the session. Only an mpv that is actually gone, or an
// episode already watched far enough, does.
func ClassifyPlaybackLoss(socket string, started bool, percentageWatched float64, completeThreshold int) PlaybackLossAction {
	if !started || PlaylistSwitchInProgress() {
		return PlaybackLossWait
	}
	if int(percentageWatched) >= completeThreshold {
		return PlaybackLossComplete
	}
	if socket != "" && IsMPVRunning(socket) {
		// Open but short of the threshold: paused, buffering or jumping rows.
		return PlaybackLossWait
	}
	return PlaybackLossExit
}
