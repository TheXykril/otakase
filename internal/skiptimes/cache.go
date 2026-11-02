package skiptimes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"
)

const (
	// anime-lists maps AniList ids onto TMDB shows and seasons. The mini
	// list carries the ids without titles, about 6 MB.
	animeListsURL       = "https://raw.githubusercontent.com/Fribb/anime-lists/master/anime-list-mini.json"
	mappingMaxAge       = 30 * 24 * time.Hour
	mappingTimeout      = 3 * time.Minute
	mappingRetry        = time.Hour
	animeSkipPlayground = "https://api.anime-skip.com/"
	clientIDRetry       = 10 * time.Minute
)

var animeSkipPlaygroundURL = animeSkipPlayground

type tmdbShow struct {
	ID     int `json:"tmdb"`
	Season int `json:"season"`
}

// tmdbMapping is the AniList to TMDB table, kept on disk for a month and
// built again in the background when older.
type tmdbMapping struct {
	path, source string
	client       *http.Client

	mu       sync.Mutex
	shows    map[int]tmdbShow
	loaded   bool
	building chan struct{}
	failedAt time.Time
}

func newTMDBMapping(path, source string, base http.RoundTripper) *tmdbMapping {
	return &tmdbMapping{path: path, source: source, client: &http.Client{Transport: base, Timeout: mappingTimeout}}
}

// Show finds an AniList show's TMDB id and season. On first use it waits up
// to wait for the table to download; a show asked about meanwhile is found
// next time.
func (m *tmdbMapping) Show(ctx context.Context, anilistID int, wait time.Duration) (tmdbShow, bool) {
	m.mu.Lock()
	if !m.loaded {
		m.loadStored()
	}
	if !m.loaded {
		done := m.startBuild()
		m.mu.Unlock()
		if done == nil {
			return tmdbShow{}, false
		}
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			return tmdbShow{}, false
		case <-ctx.Done():
			return tmdbShow{}, false
		}
		m.mu.Lock()
	}
	show, ok := m.shows[anilistID]
	m.mu.Unlock()
	return show, ok && show.ID > 0 && show.Season > 0
}

func (m *tmdbMapping) loadStored() {
	info, err := os.Stat(m.path)
	if err != nil {
		return
	}
	data, err := os.ReadFile(m.path)
	if err != nil {
		return
	}
	var stored map[string]tmdbShow
	if json.Unmarshal(data, &stored) != nil {
		m.startBuild()
		return
	}
	shows := make(map[int]tmdbShow, len(stored))
	for key, show := range stored {
		if id, err := strconv.Atoi(key); err == nil {
			shows[id] = show
		}
	}
	m.shows, m.loaded = shows, true
	if time.Since(info.ModTime()) > mappingMaxAge {
		m.startBuild()
	}
}

// startBuild downloads the table in the background. It returns a channel
// closed when the build ends, or nil when one failed within the hour.
// Called with m.mu held.
func (m *tmdbMapping) startBuild() chan struct{} {
	if m.building != nil {
		return m.building
	}
	if time.Since(m.failedAt) < mappingRetry {
		return nil
	}
	done := make(chan struct{})
	m.building = done
	go func() {
		shows, err := m.fetch()
		m.mu.Lock()
		m.building = nil
		if err != nil {
			m.failedAt = time.Now()
		} else {
			m.shows, m.loaded = shows, true
		}
		m.mu.Unlock()
		if err == nil {
			m.store(shows)
		}
		close(done)
	}()
	return done
}

func (m *tmdbMapping) fetch() (map[int]tmdbShow, error) {
	resp, err := m.client.Get(m.source)
	if err != nil {
		return nil, fmt.Errorf("anime id table: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anime id table: status %d", resp.StatusCode)
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 64<<20))
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("anime id table: %w", err)
	}
	shows := map[int]tmdbShow{}
	for dec.More() {
		var entry struct {
			AniListID int `json:"anilist_id"`
			TMDB      struct {
				TV json.RawMessage `json:"tv"`
			} `json:"themoviedb_id"`
			Season struct {
				TMDB int `json:"tmdb"`
			} `json:"season"`
		}
		if err := dec.Decode(&entry); err != nil {
			return nil, fmt.Errorf("anime id table: %w", err)
		}
		id := 0
		if entry.AniListID <= 0 || entry.Season.TMDB <= 0 || json.Unmarshal(entry.TMDB.TV, &id) != nil || id <= 0 {
			continue
		}
		shows[entry.AniListID] = tmdbShow{ID: id, Season: entry.Season.TMDB}
	}
	if len(shows) == 0 {
		return nil, fmt.Errorf("anime id table: nothing usable in it")
	}
	return shows, nil
}

func (m *tmdbMapping) store(shows map[int]tmdbShow) {
	stored := make(map[string]tmdbShow, len(shows))
	for id, show := range shows {
		stored[strconv.Itoa(id)] = show
	}
	data, err := json.Marshal(stored)
	if err != nil || os.MkdirAll(filepath.Dir(m.path), 0o700) != nil {
		return
	}
	tmp := m.path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, m.path)
	}
}

var clientIDPattern = regexp.MustCompile(`Client-ID"\s*:\s*"([A-Za-z0-9_-]{8,})"`)

// autoClientID is the client id Anime-Skip publishes for its API
// playground, fetched again when it stops being accepted.
type autoClientID struct {
	path, page string

	mu           sync.Mutex
	cached       string
	refused      map[string]bool
	blockedUntil time.Time
}

func newAutoClientID(path, page string) *autoClientID {
	return &autoClientID{path: path, page: page, refused: map[string]bool{}}
}

func (a *autoClientID) ClientID(client *http.Client) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cached != "" && !a.refused[a.cached] {
		return a.cached, nil
	}
	if data, err := os.ReadFile(a.path); err == nil {
		if stored := string(trimSpace(data)); stored != "" && !a.refused[stored] {
			a.cached = stored
			return stored, nil
		}
	}
	if time.Now().Before(a.blockedUntil) {
		return "", fmt.Errorf("anime-skip: no usable client id yet")
	}
	id, err := a.fetch(client)
	if err == nil && a.refused[id] {
		err = fmt.Errorf("anime-skip: the published client id was refused")
	}
	if err != nil {
		a.blockedUntil = time.Now().Add(clientIDRetry)
		return "", err
	}
	a.cached = id
	if os.MkdirAll(filepath.Dir(a.path), 0o700) == nil {
		_ = os.WriteFile(a.path, []byte(id+"\n"), 0o600)
	}
	return id, nil
}

func (a *autoClientID) Invalidate() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cached != "" {
		a.refused[a.cached] = true
	}
	a.cached = ""
	_ = os.Remove(a.path)
}

func (a *autoClientID) fetch(client *http.Client) (string, error) {
	resp, err := client.Get(a.page)
	if err != nil {
		return "", fmt.Errorf("anime-skip: client id: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anime-skip: client id: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	match := clientIDPattern.FindSubmatch(body)
	if match == nil {
		return "", fmt.Errorf("anime-skip: no client id on its API page")
	}
	return string(match[1]), nil
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return b
}
