package internal

import "sync/atomic"

var (
	// globalAnime is read from background goroutines (tracker updates started
	// by StartNextEpisode) while the main flow replaces it, so it is atomic.
	globalAnime   atomic.Pointer[Anime]
	globalLogFile string
)

// SetGlobalAnime sets the global anime reference
func SetGlobalAnime(anime *Anime) {
	globalAnime.Store(anime)
}

// GetGlobalAnime gets the global anime reference
func GetGlobalAnime() *Anime {
	return globalAnime.Load()
}

// SetGlobalLogFile sets the global log file path
func SetGlobalLogFile(logFile string) {
	globalLogFile = logFile
}

// GetGlobalLogFile gets the global log file path
func GetGlobalLogFile() string {
	return globalLogFile
}
