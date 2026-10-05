package movies

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Trakt keeps a copy of the movie history on trakt.tv: what was watched,
// the watchlist, ratings, and where a movie was stopped. The local history
// stays the source of truth; Trakt is told about each change as it happens,
// and a failure to reach it is logged, never shown as a failed save.
//
// Sign-in is Trakt's device flow: otakase shows a short code, the viewer
// enters it at trakt.tv/activate, and otakase polls until it is accepted.
// Only movies with an IMDb id can be matched on Trakt.
type Trakt struct {
	ClientID     string
	ClientSecret string
	API          string
	Client       *http.Client
	tokenPath    string

	mu    sync.Mutex
	token *TraktToken
}

// TraktClientID is otakase's own Trakt app. Its id is public by design: the
// app has no secret, so the id alone cannot be turned into anyone's session.
const TraktClientID = "zbSBzFuTMKrNfuZyM5LawZUh-9BiKXsTyQWbbRTT9SA"

// TraktRedirectURI is the redirect URI the app is registered with. The device
// sign-in never visits it, but renewing a session names it.
const TraktRedirectURI = "https://thexykril.github.io/otakase/"

// TraktToken is a signed-in session, kept in trakt_token.json.
type TraktToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	CreatedAt    int64  `json:"created_at"`
}

func (t TraktToken) expired(now time.Time) bool {
	return t.ExpiresIn > 0 && now.Unix() > t.CreatedAt+t.ExpiresIn-60
}

// NewTrakt returns Trakt for the app credentials, with its token kept in
// storage. It is usable once SignedIn reports true.
func NewTrakt(clientID, clientSecret, storage string) *Trakt {
	t := &Trakt{
		ClientID:     strings.TrimSpace(clientID),
		ClientSecret: strings.TrimSpace(clientSecret),
		API:          "https://api.trakt.tv",
		Client:       &http.Client{Timeout: 20 * time.Second},
		tokenPath:    filepath.Join(storage, "trakt_token.json"),
	}
	if raw, err := os.ReadFile(t.tokenPath); err == nil {
		var token TraktToken
		if json.Unmarshal(raw, &token) == nil && token.AccessToken != "" {
			t.token = &token
		}
	}
	return t
}

// Configured reports whether there is an app to sign in with. A secret is
// optional: otakase's own app has none (Trakt calls it a PKCE app), and its
// device sign-in works with the Client ID alone.
func (t *Trakt) Configured() bool {
	return t != nil && t.ClientID != ""
}

// credentials adds the app's id, and its secret when it has one, to a
// sign-in request.
func (t *Trakt) credentials(body map[string]string) map[string]string {
	body["client_id"] = t.ClientID
	if t.ClientSecret != "" {
		body["client_secret"] = t.ClientSecret
	}
	return body
}

// SignedIn reports whether there is a session to sync with.
func (t *Trakt) SignedIn() bool {
	if !t.Configured() {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.token != nil
}

// SignOut forgets the session.
func (t *Trakt) SignOut() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.token = nil
	err := os.Remove(t.tokenPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (t *Trakt) saveToken(token TraktToken) error {
	raw, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(t.tokenPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(t.tokenPath, raw, 0o600); err != nil {
		return err
	}
	t.token = &token
	return nil
}

// call makes one API request. body is sent as JSON when not nil; out is
// filled from the answer when not nil.
func (t *Trakt) call(method, path, bearer string, body, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, t.API+path, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("trakt-api-version", "2")
	req.Header.Set("trakt-api-key", t.ClientID)
	req.Header.Set("User-Agent", "otakase")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := t.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("trakt %s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("trakt %s: %w", path, err)
		}
	}
	return resp.StatusCode, nil
}

// TraktDeviceCode is what the viewer is shown to sign in.
type TraktDeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// StartSignIn asks Trakt for a code for the viewer to enter.
func (t *Trakt) StartSignIn() (TraktDeviceCode, error) {
	var code TraktDeviceCode
	if !t.Configured() {
		return code, fmt.Errorf("TraktClientID is not set")
	}
	_, err := t.call(http.MethodPost, "/oauth/device/code", "", map[string]string{"client_id": t.ClientID}, &code)
	return code, err
}

// FinishSignIn polls until the viewer has entered the code, it expires, or
// they deny it.
func (t *Trakt) FinishSignIn(code TraktDeviceCode) error {
	return t.finishSignIn(code, 5*time.Second)
}

func (t *Trakt) finishSignIn(code TraktDeviceCode, least time.Duration) error {
	interval := max(time.Duration(code.Interval)*time.Second, least)
	deadline := time.Now().Add(time.Duration(max(code.ExpiresIn, 60)) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(interval)
		var token TraktToken
		status, err := t.call(http.MethodPost, "/oauth/device/token", "", t.credentials(map[string]string{
			"code": code.DeviceCode,
		}), &token)
		switch {
		case err == nil && token.AccessToken != "":
			t.mu.Lock()
			defer t.mu.Unlock()
			return t.saveToken(token)
		case status == http.StatusBadRequest:
			// Not entered yet.
			continue
		case status == http.StatusTooManyRequests:
			interval += time.Second
			continue
		case status == http.StatusGone || status == http.StatusNotFound:
			return fmt.Errorf("the code expired; try again")
		case status == http.StatusTeapot:
			return fmt.Errorf("sign-in was denied")
		case err != nil:
			return err
		}
	}
	return fmt.Errorf("the code expired; try again")
}

// bearer is a live access token, refreshed when it has run out.
func (t *Trakt) bearer() (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.token == nil {
		return "", fmt.Errorf("not signed in to Trakt")
	}
	if !t.token.expired(time.Now()) {
		return t.token.AccessToken, nil
	}
	var token TraktToken
	_, err := t.call(http.MethodPost, "/oauth/token", "", t.credentials(map[string]string{
		"refresh_token": t.token.RefreshToken,
		"redirect_uri":  TraktRedirectURI, "grant_type": "refresh_token",
	}), &token)
	if err != nil {
		return "", fmt.Errorf("renewing the Trakt session: %w", err)
	}
	if err := t.saveToken(token); err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

func (t *Trakt) send(path string, body any) error {
	bearer, err := t.bearer()
	if err != nil {
		return err
	}
	_, err = t.call(http.MethodPost, path, bearer, body, nil)
	return err
}

// traktMovie is how a movie is named to Trakt: by IMDb id.
func traktMovie(movie Movie, extra map[string]any) (map[string]any, error) {
	if !strings.HasPrefix(movie.IMDb, "tt") {
		return nil, fmt.Errorf("%s has no IMDb id to match on Trakt", movie.Label())
	}
	item := map[string]any{"ids": map[string]string{"imdb": movie.IMDb}}
	for key, value := range extra {
		item[key] = value
	}
	return item, nil
}

func (t *Trakt) syncMovies(path string, movie Movie, extra map[string]any) error {
	item, err := traktMovie(movie, extra)
	if err != nil {
		return err
	}
	return t.send(path, map[string]any{"movies": []any{item}})
}

// Watched adds a viewing to the Trakt history, or takes the movie's
// viewings off it.
func (t *Trakt) Watched(movie Movie, watched bool, at time.Time) error {
	if watched {
		return t.syncMovies("/sync/history", movie, map[string]any{"watched_at": at.UTC().Format(time.RFC3339)})
	}
	return t.syncMovies("/sync/history/remove", movie, nil)
}

// Watchlist puts the movie on the Trakt watchlist or takes it off.
func (t *Trakt) Watchlist(movie Movie, on bool) error {
	if on {
		return t.syncMovies("/sync/watchlist", movie, nil)
	}
	return t.syncMovies("/sync/watchlist/remove", movie, nil)
}

// Rate sets the movie's rating out of 10 on Trakt; 0 removes it.
func (t *Trakt) Rate(movie Movie, rating int) error {
	if rating <= 0 {
		return t.syncMovies("/sync/ratings/remove", movie, nil)
	}
	return t.syncMovies("/sync/ratings", movie, map[string]any{"rating": rating})
}

// Started tells Trakt the movie is playing from position, so it shows as
// being watched now. Trakt clears that by itself after the runtime, or when
// Paused is sent.
func (t *Trakt) Started(movie Movie, position, duration int) error {
	item, err := traktMovie(movie, nil)
	if err != nil {
		return err
	}
	progress := 0.0
	if duration > 0 && position > 0 {
		progress = min(float64(position)*100/float64(duration), 99)
	}
	return t.send("/scrobble/start", map[string]any{"movie": item, "progress": progress})
}

// Finished ends a play Started began, watched to the end: Trakt adds it to
// the history itself, so Watched is not sent for it as well.
func (t *Trakt) Finished(movie Movie) error {
	item, err := traktMovie(movie, nil)
	if err != nil {
		return err
	}
	return t.send("/scrobble/stop", map[string]any{"movie": item, "progress": 100})
}

// Paused records where the movie was stopped, as a percentage, so Trakt and
// the apps that read it can offer to carry on from there.
func (t *Trakt) Paused(movie Movie, position, duration int) error {
	if duration <= 0 || position <= 0 {
		return nil
	}
	item, err := traktMovie(movie, nil)
	if err != nil {
		return err
	}
	progress := min(float64(position)*100/float64(duration), 99)
	return t.send("/scrobble/pause", map[string]any{"movie": item, "progress": progress})
}

// Details is what Trakt says about a film or a show, for the menu.
type Details struct {
	Overview string   `json:"overview"`
	Tagline  string   `json:"tagline"`
	Runtime  int      `json:"runtime"`
	Genres   []string `json:"genres"`
	Rating   float64  `json:"rating"`
	// Certification is the age rating, such as PG-13.
	Certification string `json:"certification"`
	Network       string `json:"network"`
	Episodes      int    `json:"aired_episodes"`
}

// Details looks a movie or show up by its IMDb id. It needs no sign-in.
func (t *Trakt) Details(movie Movie) (Details, error) {
	if !strings.HasPrefix(movie.IMDb, "tt") {
		return Details{}, fmt.Errorf("%s has no IMDb id to look up", movie.Label())
	}
	kind := "movies"
	if movie.Series {
		kind = "shows"
	}
	var details Details
	_, err := t.call(http.MethodGet, "/"+kind+"/"+movie.IMDb+"?extended=full", "", nil, &details)
	return details, err
}

// FindIMDb looks a movie up on Trakt by its titles and year and returns its
// IMDb id, or "" when nothing matches. Trakt's search reads titles in other
// languages too (its translations and aliases), so a Lithuanian title finds
// the film. Its year filter is not applied to this search, so the year is
// checked here: a result from another year is a remake or a namesake, and
// syncing the wrong film is worse than syncing none.
func (t *Trakt) FindIMDb(titles []string, year string) string {
	for _, title := range titles {
		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}
		query := url.Values{"query": {title}, "fields": {"title,translations,aliases"}}
		var found []struct {
			Movie struct {
				Title string `json:"title"`
				Year  int    `json:"year"`
				IDs   struct {
					IMDb string `json:"imdb"`
				} `json:"ids"`
			} `json:"movie"`
		}
		if _, err := t.call(http.MethodGet, "/search/movie?"+query.Encode(), "", nil, &found); err != nil {
			logf("trakt: looking up %q: %v", title, err)
			continue
		}
		best, bestScore := "", 0
		for _, result := range found {
			movie := result.Movie
			if !strings.HasPrefix(movie.IDs.IMDb, "tt") {
				continue
			}
			score := 1
			if year != "" {
				switch gap := abs(movie.Year - atoi(year)); {
				case gap == 0:
					score += 4
				case gap == 1:
					// A release date in another country can be a year off.
					score += 2
				default:
					continue
				}
			}
			if strings.EqualFold(movie.Title, title) {
				score += 2
			}
			if score > bestScore {
				best, bestScore = movie.IDs.IMDb, score
			}
		}
		if best != "" {
			return best
		}
	}
	return ""
}

func atoi(text string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(text))
	return n
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
