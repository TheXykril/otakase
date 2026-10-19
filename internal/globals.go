package internal

import "sync/atomic"

// globalAnime is read from background goroutines (tracker updates started
// by StartNextEpisode) while the main flow replaces it, so it is atomic.
var globalAnime atomic.Pointer[Anime]

// globalLogFile is where Log writes; empty until main has read the config.
var globalLogFile string

// SetGlobalAnime makes anime the show the rest of the program acts on.
func SetGlobalAnime(anime *Anime) { globalAnime.Store(anime) }

// GetGlobalAnime is the show set by SetGlobalAnime, or nil.
func GetGlobalAnime() *Anime { return globalAnime.Load() }

// SetGlobalLogFile points the debug log at path.
func SetGlobalLogFile(path string) { globalLogFile = path }

// GetGlobalLogFile is the debug log's path.
func GetGlobalLogFile() string { return globalLogFile }

// globalUser is the signed-in account, for code far from main that needs the
// token or the list.
var globalUser *User

// SetGlobalUser makes user the signed-in account.
func SetGlobalUser(user *User) { globalUser = user }

// GetGlobalUser is the account set by SetGlobalUser, or nil.
func GetGlobalUser() *User { return globalUser }
