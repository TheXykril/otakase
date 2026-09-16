package internal

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// playgroundPage is the shape the real page embeds the id in, trimmed to the
// part that matters.
func playgroundPage(id string) string {
	return `<!DOCTYPE html><html><body><script>window.__SETTINGS__ = ` +
		`{"endpoint": "/graphql", "headers": {"X-Client-ID": "` + id + `"}};</script></body></html>`
}

func autoIDAgainst(t *testing.T, handler http.HandlerFunc) (*autoClientID, string) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	resolver := newAutoClientID(t.TempDir())
	resolver.pageURL = server.URL
	resolver.http = server.Client()
	return resolver, server.URL
}

func TestTheClientIDIsReadFromThePublishedPage(t *testing.T) {
	resolver, _ := autoIDAgainst(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, playgroundPage("ZGfO0sMF3eCwLYf8yMSCJjlynwNGRXWE"))
	})

	got, err := resolver.ClientID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ZGfO0sMF3eCwLYf8yMSCJjlynwNGRXWE" {
		t.Errorf("read the id as %q", got)
	}
}

// Fetching once per episode would be a request per episode. The id is kept in
// memory, and on disk so a restart does not pay for it either.
func TestTheClientIDIsFetchedOnceAndRemembered(t *testing.T) {
	var fetches int32
	resolver, url := autoIDAgainst(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&fetches, 1)
		fmt.Fprint(w, playgroundPage("first-client-id"))
	})

	for i := 0; i < 3; i++ {
		if _, err := resolver.ClientID(); err != nil {
			t.Fatalf("lookup %d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&fetches); got != 1 {
		t.Errorf("the page was fetched %d times", got)
	}

	stored, err := os.ReadFile(filepath.Join(resolver.storagePath, animeSkipClientIDFile))
	if err != nil {
		t.Fatalf("the id was not saved: %v", err)
	}
	if strings.TrimSpace(string(stored)) != "first-client-id" {
		t.Errorf("saved %q", strings.TrimSpace(string(stored)))
	}

	// A fresh resolver over the same storage answers without asking again.
	restarted := newAutoClientID(resolver.storagePath)
	restarted.pageURL = url
	restarted.http = &http.Client{}
	if got, err := restarted.ClientID(); err != nil || got != "first-client-id" {
		t.Errorf("after a restart: %q, %v", got, err)
	}
	if got := atomic.LoadInt32(&fetches); got != 1 {
		t.Errorf("a restart cost %d fetches", got-1)
	}
}

// The point of fetching rather than writing the id down: when Anime-Skip
// rotates it, the next lookup picks up the new one.
func TestARefusedClientIDIsReplaced(t *testing.T) {
	published := "first-client-id"
	resolver, _ := autoIDAgainst(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, playgroundPage(published))
	})

	if got, _ := resolver.ClientID(); got != published {
		t.Fatalf("first id was %q", got)
	}

	published = "second-client-id"
	resolver.Invalidate()

	got, err := resolver.ClientID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "second-client-id" {
		t.Errorf("after the rotation the id is %q", got)
	}
}

// Refusing the id the page still serves must not become a fetch per episode.
func TestARefusalIsNotRetriedImmediately(t *testing.T) {
	var fetches int32
	resolver, _ := autoIDAgainst(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&fetches, 1)
		fmt.Fprint(w, playgroundPage("the-only-client-id"))
	})

	if _, err := resolver.ClientID(); err != nil {
		t.Fatalf("first lookup: %v", err)
	}
	resolver.Invalidate()

	// The page has nothing else to offer, so this asks once and then stops.
	for i := 0; i < 3; i++ {
		if _, err := resolver.ClientID(); err == nil {
			t.Fatalf("lookup %d returned the refused id", i)
		}
	}
	if got := atomic.LoadInt32(&fetches); got != 2 {
		t.Errorf("the page was fetched %d times, want 2", got)
	}
}

// "must be passed" means this program sent nothing, which fetching cannot fix.
// Only a refusal of the id itself is worth acting on.
func TestOnlyARefusedIDAsksForANewOne(t *testing.T) {
	refused := []string{
		"anime-skip: Invalid X-Client-ID header, API client not found",
		"anime-skip: invalid x-client-id header",
	}
	for _, message := range refused {
		if !isRefusedClientIDError(fmt.Errorf("%s", message)) {
			t.Errorf("%q should count as a refused id", message)
		}
	}

	others := []string{
		"anime-skip: The X-Client-ID header must be passed",
		"anime-skip: status 502",
		"anime-skip: context deadline exceeded",
	}
	for _, message := range others {
		if isRefusedClientIDError(fmt.Errorf("%s", message)) {
			t.Errorf("%q should not send us looking for a new id", message)
		}
	}
	if isRefusedClientIDError(nil) {
		t.Error("no error at all is not a refusal")
	}
}

// A page that stops publishing an id leaves Anime-Skip unasked, which the other
// sources cover. It must not be an error that reaches playback.
func TestAPageWithoutAnIDIsNotFatal(t *testing.T) {
	resolver, _ := autoIDAgainst(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "<html><body>nothing to see</body></html>")
	})

	if _, err := resolver.ClientID(); err == nil {
		t.Fatal("a page with no id should report that it has none")
	}

	source := newAnimeSkipSourceFrom(resolver)
	times, found, err := source.Lookup(SkipRef{AniListID: 154587, Episode: 1})
	if err != nil {
		t.Errorf("a missing client id reached the caller as an error: %v", err)
	}
	if found || usableSpan(times.Op) {
		t.Error("it claimed to know something without being able to ask")
	}
}

// The whole point, end to end: the id on disk has been rotated away, and the
// episode still gets its skip times without the user touching anything.
func TestARotatedClientIDIsReplacedMidLookup(t *testing.T) {
	const (
		oldID = "the-rotated-away-id"
		newID = "the-current-id"
	)
	var refusals, accepted int32

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, playgroundPage(newID))
	})
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		query := string(body[:n])

		if r.Header.Get("X-Client-ID") != newID {
			atomic.AddInt32(&refusals, 1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"errors":[{"message":"Invalid X-Client-ID header, API client not found"}]}`)
			return
		}
		atomic.AddInt32(&accepted, 1)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(query, "findShowsByExternalId") {
			fmt.Fprint(w, `{"data":{"findShowsByExternalId":[{"id":"show-1"}]}}`)
			return
		}
		fmt.Fprint(w, `{"data":{"findEpisodesByShowId":[{"number":"1","timestamps":[`+
			`{"at":30,"type":{"name":"Intro"}},{"at":120,"type":{"name":"Canon"}}]}]}}`)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	storage := t.TempDir()
	if err := os.WriteFile(filepath.Join(storage, animeSkipClientIDFile), []byte(oldID+"\n"), 0o644); err != nil {
		t.Fatalf("seeding the stored id: %v", err)
	}

	resolver := newAutoClientID(storage)
	resolver.pageURL = server.URL + "/"
	resolver.http = server.Client()

	source := newAnimeSkipSourceFrom(resolver)
	source.endpoint = server.URL + "/graphql"
	source.http = server.Client()

	times, found, err := source.Lookup(SkipRef{AniListID: 154587, Episode: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found || times.Op.Start != 30 || times.Op.End != 120 {
		t.Fatalf("the opening was not found after the rotation: %+v found=%v", times, found)
	}
	if got := atomic.LoadInt32(&refusals); got != 1 {
		t.Errorf("the stale id was sent %d times", got)
	}
	if got := atomic.LoadInt32(&accepted); got < 2 {
		t.Errorf("the new id answered %d requests, want both", got)
	}

	// The replacement is what is on disk now, so the next run starts from it.
	stored, _ := os.ReadFile(filepath.Join(storage, animeSkipClientIDFile))
	if strings.TrimSpace(string(stored)) != newID {
		t.Errorf("the stale id is still stored: %q", strings.TrimSpace(string(stored)))
	}
}

// "auto" is a word, not an id: it has to reach the resolver rather than being
// sent to Anime-Skip as a client id, whatever case it is written in.
func TestAutoSelectsTheResolverAndAnythingElseIsAnID(t *testing.T) {
	for _, written := range []string{"auto", "Auto", "AUTO"} {
		sources := DefaultSkipSources(&CurdConfig{AnimeSkipClientID: written, StoragePath: t.TempDir()}, nil)
		source, ok := lastAnimeSkipSource(sources)
		if !ok {
			t.Fatalf("%q did not add Anime-Skip at all", written)
		}
		if _, isAuto := source.clientIDs.(*autoClientID); !isAuto {
			t.Errorf("%q was taken for a literal client id", written)
		}
	}

	sources := DefaultSkipSources(&CurdConfig{AnimeSkipClientID: "ZGfO0sMF3eCwLYf8yMSCJjlynwNGRXWE"}, nil)
	source, ok := lastAnimeSkipSource(sources)
	if !ok {
		t.Fatal("a configured id did not add Anime-Skip")
	}
	if got, isFixed := source.clientIDs.(fixedClientID); !isFixed || string(got) != "ZGfO0sMF3eCwLYf8yMSCJjlynwNGRXWE" {
		t.Errorf("a written id did not reach the source as itself: %#v", source.clientIDs)
	}

	if _, ok := lastAnimeSkipSource(DefaultSkipSources(&CurdConfig{}, nil)); ok {
		t.Error("Anime-Skip was added with nothing configured")
	}
}

func lastAnimeSkipSource(sources []SkipSource) (animeSkipSource, bool) {
	for i := len(sources) - 1; i >= 0; i-- {
		if source, ok := sources[i].(animeSkipSource); ok {
			return source, true
		}
	}
	return animeSkipSource{}, false
}
