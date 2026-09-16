package internal

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// AnimeSkipAutoClientID is the config value that asks for the client id to
	// be fetched rather than typed.
	AnimeSkipAutoClientID = "auto"

	animeSkipPlaygroundURL = "https://api.anime-skip.com/"
	animeSkipClientIDFile  = "anime_skip_client_id"

	// animeSkipRefetchInterval keeps a rejected id from turning into a request
	// per episode. If the page is down, or the id it serves is the one that was
	// just refused, waiting is the only sensible response.
	animeSkipRefetchInterval = 10 * time.Minute
)

// animeSkipClientIDPattern matches the id as the playground page embeds it. The
// page is small and the shape has been stable, but a miss is an ordinary
// outcome here rather than a failure worth shouting about: Anime-Skip simply
// goes unasked, and the other skip sources carry on.
var animeSkipClientIDPattern = regexp.MustCompile(`Client-ID"\s*:\s*"([A-Za-z0-9_-]{8,})"`)

// clientIDSource supplies the X-Client-ID header value Anime-Skip requires.
type clientIDSource interface {
	ClientID() (string, error)
	// Invalidate reports that the id was refused, so the next ClientID must
	// find another rather than repeating it.
	Invalidate()
}

// fixedClientID is an id the user configured. Being refused is something only
// they can fix, so there is nothing to invalidate.
type fixedClientID string

func (f fixedClientID) ClientID() (string, error) { return strings.TrimSpace(string(f)), nil }
func (fixedClientID) Invalidate()                 {}

// autoClientID reads the client id Anime-Skip publishes for its own GraphQL
// playground, and remembers it.
//
// This is opt-in, and deliberately not the default. The id belongs to
// Anime-Skip, shared by everyone who opens that page, so it can be rate-limited
// or rotated at any time -- which is the whole reason for fetching it rather
// than writing it down, but also the reason not to point every install at it
// without being asked to.
type autoClientID struct {
	storagePath string
	http        *http.Client
	pageURL     string

	mu           sync.Mutex
	cached       string
	refused      map[string]bool
	blockedUntil time.Time
}

func newAutoClientID(storagePath string) *autoClientID {
	return &autoClientID{
		storagePath: strings.TrimSpace(os.ExpandEnv(storagePath)),
		http:        &http.Client{Timeout: animeSkipTimeout},
		pageURL:     animeSkipPlaygroundURL,
		refused:     map[string]bool{},
	}
}

var (
	animeSkipResolversMu sync.Mutex
	animeSkipResolvers   = map[string]*autoClientID{}
)

// animeSkipClientIDs returns the resolver for a storage path, making it once.
//
// The skip sources are rebuilt for every episode, so a resolver created along
// with them would forget everything each time: it would re-read the page on
// every lookup, and could not know that the id it is about to use is the one
// just refused.
func animeSkipClientIDs(storagePath string) *autoClientID {
	key := strings.TrimSpace(os.ExpandEnv(storagePath))

	animeSkipResolversMu.Lock()
	defer animeSkipResolversMu.Unlock()
	if existing := animeSkipResolvers[key]; existing != nil {
		return existing
	}
	created := newAutoClientID(key)
	animeSkipResolvers[key] = created
	return created
}

func (a *autoClientID) ClientID() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.cached != "" && !a.refused[a.cached] {
		return a.cached, nil
	}
	// The file survives restarts, so the usual run costs no request at all.
	if stored := a.readStored(); stored != "" && !a.refused[stored] {
		a.cached = stored
		return stored, nil
	}
	// The wait is earned by a fetch that did not help, not by one that did: a
	// refused id has to be replaceable at once, or rotation would cost the
	// user ten minutes of unskipped openings.
	if time.Now().Before(a.blockedUntil) {
		return "", fmt.Errorf("anime-skip: no usable client id; not asking again for %s", animeSkipRefetchInterval)
	}

	fetched, err := a.fetch()
	if err != nil {
		a.blockedUntil = time.Now().Add(animeSkipRefetchInterval)
		return "", err
	}
	if a.refused[fetched] {
		a.blockedUntil = time.Now().Add(animeSkipRefetchInterval)
		return "", fmt.Errorf("anime-skip: the published client id is the one that was refused")
	}

	a.blockedUntil = time.Time{}
	a.cached = fetched
	a.writeStored(fetched)
	return fetched, nil
}

func (a *autoClientID) Invalidate() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cached != "" {
		a.refused[a.cached] = true
	}
	if stored := a.readStored(); stored != "" {
		a.refused[stored] = true
	}
	a.cached = ""
	a.removeStored()
}

func (a *autoClientID) fetch() (string, error) {
	resp, err := a.http.Get(a.pageURL)
	if err != nil {
		return "", fmt.Errorf("anime-skip: fetch client id: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anime-skip: fetch client id: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("anime-skip: fetch client id: %w", err)
	}
	match := animeSkipClientIDPattern.FindSubmatch(body)
	if match == nil {
		return "", fmt.Errorf("anime-skip: no client id published at %s", a.pageURL)
	}
	return string(match[1]), nil
}

func (a *autoClientID) storedPath() string {
	if a.storagePath == "" {
		return ""
	}
	return filepath.Join(a.storagePath, animeSkipClientIDFile)
}

func (a *autoClientID) readStored() string {
	path := a.storedPath()
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (a *autoClientID) writeStored(id string) {
	path := a.storedPath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		Log(fmt.Sprintf("anime-skip: could not create the storage directory for the client id: %v", err))
		return
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0o644); err != nil {
		Log(fmt.Sprintf("anime-skip: could not save the client id: %v", err))
	}
}

func (a *autoClientID) removeStored() {
	if path := a.storedPath(); path != "" {
		_ = os.Remove(path)
	}
}

// isRefusedClientIDError reports whether Anime-Skip rejected the id itself, as
// opposed to failing for any of the ordinary reasons.
//
// The two messages it answers with are distinct, which is what makes fetching a
// new id on rotation safe: "must be passed" means this program sent nothing,
// and no amount of re-fetching fixes that.
func isRefusedClientIDError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "invalid x-client-id") ||
		strings.Contains(message, "api client not found")
}
