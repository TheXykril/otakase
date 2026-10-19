package providerhost

import (
	"strings"
	"testing"
)

func TestHTTPStatusOK(t *testing.T) {
	for status, want := range map[int]bool{199: false, 200: true, 204: true, 299: true, 300: false, 404: false} {
		if got := HTTPStatusOK(status); got != want {
			t.Errorf("HTTPStatusOK(%d) = %v", status, got)
		}
	}
}

func TestHTTPStatusError(t *testing.T) {
	if got := HTTPStatusError("search", 503, []byte("  ")).Error(); got != "search failed with status 503" {
		t.Errorf("empty body: %q", got)
	}
	if got := HTTPStatusError("search", 403, []byte(" denied \n")).Error(); got != "search failed with status 403: denied" {
		t.Errorf("short body: %q", got)
	}
	long := HTTPStatusError("search", 500, []byte(strings.Repeat("x", 400))).Error()
	if !strings.HasSuffix(long, ": "+strings.Repeat("x", 300)+"...") {
		t.Errorf("long body was not cut at 300 characters: %d long", len(long))
	}
}

func TestIsRateLimitedBody(t *testing.T) {
	if !IsRateLimitedBody([]byte(`{"error":"Too Many Requests"}`)) {
		t.Error("a throttling message was missed")
	}
	if IsRateLimitedBody([]byte(`{"results":[]}`)) {
		t.Error("an empty result read as throttling")
	}
	// Only the start of a body is read, so a page that merely mentions
	// limits far down is not taken for one.
	if IsRateLimitedBody([]byte(strings.Repeat(" ", 600) + "rate limit")) {
		t.Error("text past the first 512 bytes was read")
	}
}
