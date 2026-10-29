package mobilecore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thexykril/otakase/internal/providerhost"
	"github.com/thexykril/otakase/internal/providers"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func replay(t *testing.T) *Core {
	t.Helper()
	c := NewCore()
	t.Cleanup(c.Close)
	c.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var name string
		switch {
		case r.URL.Host == "graphql.anilist.co":
			body, _ := io.ReadAll(r.Body)
			name = "megavid-episodes.json"
			if bytes.Contains(body, []byte("Page(")) {
				name = "megavid-search.json"
			}
		case r.URL.Host == "megavid.buzz":
			name = "megavid-source.json"
		case r.URL.Path == "/ajax/anime/search":
			name = "anikoto-search.json"
		case r.URL.Path == "/filter":
			return reply(r, 200, []byte("<div></div>")), nil
		case strings.HasPrefix(r.URL.Path, "/watch/"):
			name = "anikoto-watch.html"
		case strings.HasPrefix(r.URL.Path, "/ajax/episode/list/"):
			name = "anikoto-episodes.json"
		case r.URL.Path == "/ajax/server/list":
			name = "anikoto-servers.json"
		case r.URL.Path == "/ajax/server":
			name = "anikoto-embed.json"
		case r.URL.Path == "/stream/getSources":
			name = "anikoto-sources.json"
		case strings.HasPrefix(r.URL.Path, "/stream/"):
			name = "anikoto-player.html"
		default:
			return nil, fmt.Errorf("unrecorded path %s", r.URL.Path)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("provider omitted User-Agent")
		}
		return reply(r, 200, fixture(t, name)), nil
	})
	return c
}
func reply(r *http.Request, status int, b []byte) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(b)), Header: make(http.Header), Request: r}
}

func call(t *testing.T, c *Core, input string) response {
	t.Helper()
	id := c.Start(input)
	if id == 0 {
		t.Fatal("request rejected")
	}
	raw := c.Result(id)
	var r response
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("invalid response %q: %v", raw, err)
	}
	if r.Version != 1 {
		t.Fatal(raw)
	}
	return r
}
func requestJSON(op, provider, id string) string {
	b, _ := json.Marshal(request{Version: 1, Operation: op, Provider: provider, Query: "frieren", ShowID: id, Episode: 1, Mode: "sub"})
	return string(b)
}
func dataAs[T any](t *testing.T, r response) T {
	t.Helper()
	if r.Error != nil {
		t.Fatalf("API error %+v", r.Error)
	}
	b, _ := json.Marshal(r.Data)
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRecordedProviderFlows(t *testing.T) {
	for _, provider := range []string{"megavid", "anikoto"} {
		t.Run(provider, func(t *testing.T) {
			c := replay(t)
			search := dataAs[struct {
				Shows []show `json:"shows"`
			}](t, call(t, c, requestJSON("search", provider, "")))
			if len(search.Shows) == 0 || !strings.Contains(search.Shows[0].Title, "Frieren") {
				t.Fatalf("search: %+v", search)
			}
			id := search.Shows[0].ID
			episodes := dataAs[struct {
				Episodes []string `json:"episodes"`
			}](t, call(t, c, requestJSON("episodes", provider, id)))
			if len(episodes.Episodes) != 28 || episodes.Episodes[0] != "1" {
				t.Fatalf("episodes: %+v", episodes)
			}
			result := dataAs[struct {
				Streams []stream `json:"streams"`
			}](t, call(t, c, requestJSON("resolve", provider, id)))
			if len(result.Streams) == 0 {
				t.Fatal("no streams")
			}
			s := result.Streams[0]
			wantRefer := "https://megavid.buzz/"
			if provider == "anikoto" {
				wantRefer = "https://megaplay.buzz/"
			}
			if s.URL != "http://127.0.0.1/"+provider+"/master.m3u8" || s.Referrer != wantRefer || s.Headers["Referer"] != wantRefer || s.Headers["User-Agent"] == "" || !s.HLS {
				t.Fatalf("lost playback metadata: %+v", s)
			}
			wantTracks := 9
			if provider == "megavid" {
				wantTracks = 10
			}
			if len(s.Subtitles) != wantTracks || s.Subtitle == "" {
				t.Fatalf("lost captions or thumbnail included: %+v", s)
			}
			for _, track := range s.Subtitles {
				if track.URL == "" {
					t.Fatalf("bad track: %+v", track)
				}
			}
		})
	}
}

func TestValidationAndTypedErrors(t *testing.T) {
	cases := []struct{ input, code string }{
		{`{`, "invalid_request"},
		{`null`, "invalid_request"},
		{`{"version":1,"mode":"sub","unknown":1}`, "invalid_request"},
		{`{"version":1,"mode":"sub"} {}`, "invalid_request"},
		{`{"version":2,"mode":"sub"}`, "invalid_request"},
		{`{"version":1,"mode":"raw"}`, "invalid_request"},
		{requestJSON("search", "nyaa", ""), "unsupported_provider"},
		{requestJSON("download", "megavid", "1"), "invalid_request"},
		{requestJSON("episodes", "megavid", ""), "invalid_request"},
		{strings.Repeat(" ", 17<<10), "invalid_request"},
	}
	for _, tt := range cases {
		r := call(t, replay(t), tt.input)
		if r.Error == nil || r.Error.Code != tt.code {
			t.Fatalf("input %q: %+v", tt.input, r)
		}
	}
	c := NewCore()
	defer c.Close()
	c.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { return reply(r, 429, []byte("SECRET_SIGNED_URL")), nil })
	r := call(t, c, requestJSON("search", "anikoto", ""))
	if r.Error == nil || r.Error.Code != "rate_limited" || !r.Error.Retryable {
		t.Fatalf("rate limit: %+v", r)
	}
	raw, _ := json.Marshal(r)
	if bytes.Contains(raw, []byte("SECRET")) {
		t.Fatal("upstream body leaked")
	}
}

func TestCancellationReachesHTTPAndResult(t *testing.T) {
	c := NewCore()
	defer c.Close()
	entered := make(chan struct{})
	stopped := make(chan struct{})
	var once sync.Once
	c.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		once.Do(func() { close(entered) })
		<-r.Context().Done()
		select {
		case <-stopped:
		default:
			close(stopped)
		}
		return nil, r.Context().Err()
	})
	id := c.Start(requestJSON("search", "megavid", ""))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("HTTP not started")
	}
	c.Cancel(id)
	result := make(chan string, 1)
	go func() { result <- c.Result(id) }()
	select {
	case raw := <-result:
		if !strings.Contains(raw, `"code":"cancelled"`) {
			t.Fatal(raw)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel blocked")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("HTTP context not cancelled")
	}
}

func TestTimeoutCloseLimitsAndHooks(t *testing.T) {
	c := NewCore()
	c.timeout = time.Millisecond
	c.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	r := call(t, c, requestJSON("search", "megavid", ""))
	if r.Error == nil || r.Error.Code != "timeout" {
		t.Fatalf("timeout: %+v", r)
	}
	c.Close()
	if c.Start("{}") != 0 {
		t.Fatal("closed core accepted work")
	}
	c = NewCore()
	defer c.Close()
	handles := make([]int64, maxRequests)
	for i := range handles {
		handles[i] = c.Start("{}")
		if handles[i] == 0 {
			t.Fatal("unexpected rejection")
		}
	}
	if c.Start("{}") != 0 {
		t.Fatal("request limit ignored")
	}
	for _, id := range handles {
		c.Result(id)
	}
	if !strings.Contains(c.Result(handles[0]), `"code":"invalid_handle"`) {
		t.Fatal("handle collected twice")
	}
	if providerhost.HTTPClient() == nil || providerhost.CurrentSubStyle() != "soft" {
		t.Fatal("hooks missing")
	}
	if _, err := providerhost.PromptSelect(nil); err != errSelectionRequired {
		t.Fatal("prompt did not fail")
	}
	providerhost.Log("not logged")
	providerhost.Out("not printed")
	if providerhost.PersistSubStylePreference("hard") == nil {
		t.Fatal("style persistence unexpectedly available")
	}
}

func TestProvidersOperationListsEverySource(t *testing.T) {
	c := NewCore()
	t.Cleanup(c.Close)
	out := c.Result(c.Start(`{"version":1,"operation":"providers"}`))
	var resp struct {
		Data struct {
			Providers []string `json:"providers"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data.Providers) != 6 {
		t.Fatalf("providers = %v", resp.Data.Providers)
	}
	for _, name := range resp.Data.Providers {
		if _, ok := newProvider(name, http.DefaultClient); !ok {
			t.Errorf("%s has no constructor", name)
		}
	}
}

func TestMatchUsesTrackerIDsWithoutSearching(t *testing.T) {
	c := NewCore()
	t.Cleanup(c.Close)
	c.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("unexpected request to %s", r.URL)
		return nil, fmt.Errorf("offline")
	})
	for provider, want := range map[string]string{"megavid": "52991", "animex": "154587"} {
		out := c.Result(c.Start(fmt.Sprintf(`{"version":1,"operation":"match","provider":%q,"mode":"sub","titles":["Frieren"],"mal_id":52991,"anilist_id":154587}`, provider)))
		if !strings.Contains(out, `"id":"`+want+`"`) {
			t.Errorf("%s: %s", provider, out)
		}
	}
}

func TestMatchScorePrefersSameMALID(t *testing.T) {
	req := request{Titles: []string{"Sousou no Frieren", "Frieren: Beyond Journey's End"}, MalID: 52991, Episodes: 28}
	same := providers.SelectionOption{Title: "Something else", ExtraData: providers.SearchHit{MalID: 52991}}
	other := providers.SelectionOption{Title: "Sousou no Frieren", ExtraData: providers.SearchHit{MalID: 1}}
	exact := providers.SelectionOption{Title: "Frieren - Beyond Journey's End"}
	sequel := providers.SelectionOption{Title: "Sousou no Frieren 2nd Season"}
	if got := matchScore(same, req); got != 100 {
		t.Errorf("same MAL id scored %d", got)
	}
	if got := matchScore(other, req); got != 0 {
		t.Errorf("different MAL id scored %d", got)
	}
	if a, b := matchScore(exact, req), matchScore(sequel, req); a <= b || a < 50 {
		t.Errorf("exact %d should beat sequel %d", a, b)
	}
}

func TestSkipRangeValidation(t *testing.T) {
	for _, r := range [][]int{nil, {5}, {10, 5}, {-1, 5}} {
		if got := validRange(r); len(got) != 0 {
			t.Errorf("validRange(%v) = %v", r, got)
		}
	}
	if got := validRange([]int{80, 170}); len(got) != 2 {
		t.Errorf("valid range dropped: %v", got)
	}
}

func TestPlaybackRelayLifecycle(t *testing.T) {
	c := NewCore()
	var stopped atomic.Int32
	if !c.replaceRelays([]func(){func() { stopped.Add(1) }}) {
		t.Fatal("open core refused relay")
	}
	if !c.replaceRelays([]func(){func() { stopped.Add(10) }}) || stopped.Load() != 1 {
		t.Fatal("replacing playback did not close previous relay")
	}
	out := c.Result(c.Start(`{"version":1,"operation":"stop"}`))
	if !strings.Contains(out, `"stopped":true`) || stopped.Load() != 11 {
		t.Fatalf("stop: %s, callbacks %d", out, stopped.Load())
	}
	c.StopPlayback()
	if stopped.Load() != 11 {
		t.Fatal("relay stopped twice")
	}
	c.replaceRelays([]func(){func() { stopped.Add(100) }})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(c.Close)
		wg.Go(c.StopPlayback)
	}
	wg.Wait()
	if stopped.Load() != 111 {
		t.Fatalf("concurrent close: %d", stopped.Load())
	}
	if c.replaceRelays([]func(){func() { stopped.Add(1000) }}) || stopped.Load() != 1111 {
		t.Fatal("closed core retained relay")
	}
}
