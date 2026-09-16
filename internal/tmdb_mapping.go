package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// animeListsURL is Fribb/anime-lists, which maps the ids the anime world
	// uses onto each other. It is the only public mapping that carries both an
	// AniList id and a TMDB season, and the season is the part that matters:
	// AniList files each cour as its own entry while TMDB groups them, so a
	// mapping without it would send episode 1 of a second season to episode 1
	// of the first.
	animeListsURL = "https://raw.githubusercontent.com/Fribb/anime-lists/master/anime-list-full.json"

	tmdbMappingFile    = "tmdb_mapping.json"
	tmdbMappingMaxAge  = 30 * 24 * time.Hour
	tmdbMappingTimeout = 3 * time.Minute
)

// tmdbShow is where an AniList entry lives on TMDB.
type tmdbShow struct {
	ID     int `json:"tmdb"`
	Season int `json:"season"`
}

// tmdbMapping answers "where is this AniList show on TMDB", from a table it
// keeps on disk.
//
// The table is built from a 7MB file, which is why it is built once and saved
// rather than fetched per lookup, and why a lookup that arrives before it is
// ready says it does not know instead of waiting. Nothing here is on the path
// of starting an episode.
type tmdbMapping struct {
	storagePath string
	http        *http.Client
	sourceURL   string

	mu       sync.Mutex
	shows    map[int]tmdbShow
	loaded   bool
	building bool
	failedAt time.Time
}

func newTMDBMapping(storagePath string) *tmdbMapping {
	return &tmdbMapping{
		storagePath: strings.TrimSpace(os.ExpandEnv(storagePath)),
		http:        &http.Client{Timeout: tmdbMappingTimeout},
		sourceURL:   animeListsURL,
	}
}

var (
	tmdbMappingsMu sync.Mutex
	tmdbMappings   = map[string]*tmdbMapping{}
)

func tmdbMappingFor(storagePath string) *tmdbMapping {
	key := strings.TrimSpace(os.ExpandEnv(storagePath))

	tmdbMappingsMu.Lock()
	defer tmdbMappingsMu.Unlock()
	if existing := tmdbMappings[key]; existing != nil {
		return existing
	}
	created := newTMDBMapping(key)
	tmdbMappings[key] = created
	return created
}

// Show looks up an AniList id, and reports false when the table cannot answer
// -- including when it is not built yet, which is the ordinary case the first
// time a show needs it.
func (m *tmdbMapping) Show(anilistID int) (tmdbShow, bool) {
	if anilistID <= 0 {
		return tmdbShow{}, false
	}

	m.mu.Lock()
	if !m.loaded {
		m.loadStored()
	}
	if m.loaded {
		show, ok := m.shows[anilistID]
		m.mu.Unlock()
		if ok && show.ID > 0 && show.Season > 0 {
			return show, true
		}
		return tmdbShow{}, false
	}
	m.startBuild()
	m.mu.Unlock()
	return tmdbShow{}, false
}

// loadStored reads the table written by an earlier run. Called with the lock.
func (m *tmdbMapping) loadStored() {
	path := m.storedPath()
	if path == "" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if time.Since(info.ModTime()) > tmdbMappingMaxAge {
		// Stale, but still the best answer available: use it and rebuild behind
		// the viewer rather than leaving them without skip times today.
		m.startBuild()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var stored map[string]tmdbShow
	if err := json.Unmarshal(data, &stored); err != nil {
		Log(fmt.Sprintf("tmdb mapping: unreadable table, rebuilding: %v", err))
		m.startBuild()
		return
	}

	shows := make(map[int]tmdbShow, len(stored))
	for key, show := range stored {
		if id, err := strconv.Atoi(key); err == nil {
			shows[id] = show
		}
	}
	m.shows = shows
	m.loaded = true
}

// startBuild fetches and condenses the mapping in the background. Called with
// the lock held.
func (m *tmdbMapping) startBuild() {
	if m.building || time.Since(m.failedAt) < time.Hour {
		return
	}
	m.building = true

	go func() {
		shows, err := m.fetch()

		m.mu.Lock()
		defer m.mu.Unlock()
		m.building = false
		if err != nil {
			m.failedAt = time.Now()
			Log(fmt.Sprintf("tmdb mapping: %v", err))
			return
		}
		m.shows = shows
		m.loaded = true
		m.writeStored(shows)
		Log(fmt.Sprintf("tmdb mapping: %d shows mapped onto TMDB", len(shows)))
	}()
}

func (m *tmdbMapping) fetch() (map[int]tmdbShow, error) {
	resp, err := m.http.Get(m.sourceURL)
	if err != nil {
		return nil, fmt.Errorf("fetch anime id mapping: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch anime id mapping: status %d", resp.StatusCode)
	}

	// Decoded entry by entry rather than into one slice: the file is 7MB of
	// ids, and all that is wanted from it is three numbers per entry.
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 64<<20))
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("anime id mapping: %w", err)
	}

	shows := map[int]tmdbShow{}
	for decoder.More() {
		var entry struct {
			AniListID  int `json:"anilist_id"`
			TheMovieDB struct {
				TV json.RawMessage `json:"tv"`
			} `json:"themoviedb_id"`
			Season struct {
				TMDB int `json:"tmdb"`
			} `json:"season"`
		}
		if err := decoder.Decode(&entry); err != nil {
			return nil, fmt.Errorf("anime id mapping: %w", err)
		}
		if entry.AniListID <= 0 || entry.Season.TMDB <= 0 {
			continue
		}
		// A show with several TMDB ids is ambiguous, and guessing which is
		// meant is how episodes end up matched to the wrong series.
		id := 0
		if err := json.Unmarshal(entry.TheMovieDB.TV, &id); err != nil || id <= 0 {
			continue
		}
		shows[entry.AniListID] = tmdbShow{ID: id, Season: entry.Season.TMDB}
	}
	if len(shows) == 0 {
		return nil, fmt.Errorf("anime id mapping: nothing usable in it")
	}
	return shows, nil
}

func (m *tmdbMapping) storedPath() string {
	if m.storagePath == "" {
		return ""
	}
	return filepath.Join(m.storagePath, tmdbMappingFile)
}

func (m *tmdbMapping) writeStored(shows map[int]tmdbShow) {
	path := m.storedPath()
	if path == "" {
		return
	}
	stored := make(map[string]tmdbShow, len(shows))
	for id, show := range shows {
		stored[strconv.Itoa(id)] = show
	}
	data, err := json.Marshal(stored)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		Log(fmt.Sprintf("tmdb mapping: could not save the table: %v", err))
	}
}
