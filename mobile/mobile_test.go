package mobile

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// decode is what a caller in Kotlin does: parse the envelope, check ok, read data.
func decode(t *testing.T, payload string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		t.Fatalf("the result is not JSON a caller could parse: %v\n%s", err, payload)
	}
	return out
}

// Every exported function must return something parseable, including when it
// fails: an app that gets a blank string cannot tell the user anything.
func TestFailuresAreStillParseable(t *testing.T) {
	for name, payload := range map[string]string{
		"search":   Search("nonexistent-provider", "frieren", "sub"),
		"episodes": Episodes("nonexistent-provider", "1", "sub"),
		"stream":   Stream("nonexistent-provider", "1", "sub", 1),
	} {
		out := decode(t, payload)
		if out["ok"] != false {
			t.Errorf("%s: an unknown provider should not report success", name)
		}
		if message, _ := out["error"].(string); !strings.Contains(message, "nonexistent-provider") {
			t.Errorf("%s: the error does not say what was wrong: %q", name, message)
		}
	}
}

// The registry has to survive the crossing, or nothing else can be called.
func TestProvidersAreVisibleThroughTheBinding(t *testing.T) {
	out := decode(t, Providers())
	if out["ok"] != true {
		t.Fatalf("listing providers failed: %v", out["error"])
	}
	names, _ := out["data"].([]any)
	if len(names) == 0 {
		t.Fatal("no providers are registered, so the init side effects did not run")
	}
	found := false
	for _, name := range names {
		if name == "anikoto" {
			found = true
		}
	}
	if !found {
		t.Errorf("anikoto is missing from %v", names)
	}
}

// The whole point of the probe: does a real episode come back, with the headers
// a player needs? Guarded, since it depends on a third party being up.
func TestLiveStreamThroughTheBinding(t *testing.T) {
	if os.Getenv("CURD_LIVE_ANIKOTO") != "1" {
		t.Skip("set CURD_LIVE_ANIKOTO=1 to run against the real host")
	}

	search := decode(t, Search("anikoto", "frieren", "sub"))
	if search["ok"] != true {
		t.Fatalf("search failed: %v", search["error"])
	}
	matches, _ := search["data"].([]any)
	first, _ := matches[0].(map[string]any)
	showID, _ := first["id"].(string)
	t.Logf("search: %d matches, first %q (%s)", len(matches), first["title"], showID)

	episodes := decode(t, Episodes("anikoto", showID, "sub"))
	list, _ := episodes["data"].([]any)
	t.Logf("episodes: %d", len(list))

	stream := decode(t, Stream("anikoto", showID, "sub", 1))
	if stream["ok"] != true {
		t.Fatalf("stream failed: %v", stream["error"])
	}
	data, _ := stream["data"].(map[string]any)
	streams, _ := data["streams"].([]any)
	if len(streams) == 0 {
		t.Fatal("no streams came back")
	}
	one, _ := streams[0].(map[string]any)
	url, _ := one["url"].(string)
	headers, _ := one["headers"].(map[string]any)

	t.Logf("stream: %.60s", url)
	t.Logf("headers: %v", headers)
	t.Logf("subtitle: %.50s", one["subtitle"])
	t.Logf("intro=%v outro=%v", data["intro"], data["outro"])

	// A player on Android is handed a URL and a header map. Without the headers
	// the stream fails with nothing to show the user.
	if len(headers) == 0 {
		t.Error("no headers came through; the CDN would reject every segment")
	}
	if !strings.HasPrefix(url, "http") {
		t.Errorf("that is not a playable URL: %q", url)
	}
}
