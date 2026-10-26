package internal

import (
	"sync"

	"github.com/thexykril/otakase/internal/providers/reanime"
)

var flixPlayback struct {
	sync.Mutex
	stop func()
}

// startFlixPlaybackRelay keeps a loopback relay for the current mpv episode.
// The previous relay is closed when switching links or stopping mpv.
func startFlixPlaybackRelay(link, key string) (string, error) {
	flixPlayback.Lock()
	defer flixPlayback.Unlock()
	if flixPlayback.stop != nil {
		flixPlayback.stop()
		flixPlayback.stop = nil
	}
	stream, stop, err := reanime.StartRelay(link, key, sharedHTTPClient)
	if err != nil {
		return "", err
	}
	flixPlayback.stop = stop
	return stream, nil
}

func stopFlixPlaybackRelay() {
	flixPlayback.Lock()
	defer flixPlayback.Unlock()
	if flixPlayback.stop != nil {
		flixPlayback.stop()
		flixPlayback.stop = nil
	}
}
