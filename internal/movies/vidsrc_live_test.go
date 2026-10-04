package movies

import (
	"os"
	"testing"
)

// Run with OTAKASE_LIVE_MOVIES=1 to reach vidsrc itself.
func TestVidsrcLive(t *testing.T) {
	if os.Getenv("OTAKASE_LIVE_MOVIES") == "" {
		t.Skip("set OTAKASE_LIVE_MOVIES=1 to reach vidsrc")
	}
	v := NewVidsrc()
	found, err := v.Search("interstellar")
	if err != nil || len(found) == 0 {
		t.Fatalf("search: %v %v", found, err)
	}
	t.Logf("first: %+v", found[0])
	movie, sources, err := v.Open(found[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %d sources", movie.Label(), len(sources))
	for _, source := range sources {
		stream, err := source.Resolve()
		if err != nil {
			t.Logf("%s: %v", source.Server, err)
			continue
		}
		t.Logf("%s: %.120s subs=%v", stream.Server, stream.URL, stream.Subtitles)
		return
	}
	t.Fatal("nothing resolved")
}
