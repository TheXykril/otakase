package mobilecore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTorrentResolveGetsLongerTimeout(t *testing.T) {
	for input, want := range map[string]bool{
		`{"version":1,"operation":"resolve","provider":"nyaa"}`:    true,
		`{"version":1,"operation":"resolve","provider":"sukebei"}`: true,
		`{"version":1,"operation":"search","provider":"nyaa"}`:     false,
		`{"version":1,"operation":"resolve","provider":"megavid"}`: false,
		`not json`: false,
	} {
		if got := isTorrentResolve(input); got != want {
			t.Errorf("isTorrentResolve(%s) = %v", input, got)
		}
	}
	if torrentRequestTimeout <= torrentMetadataWait*torrentCandidates/2 {
		t.Fatal("torrent request cannot outlast the releases it tries")
	}
}

func TestTorrentProgressWithoutPlayback(t *testing.T) {
	c := NewCore()
	t.Cleanup(c.Close)
	out := c.Result(c.Start(`{"version":1,"operation":"torrent"}`))
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data["active"] != false {
		t.Fatalf("torrent = %s", out)
	}
}

func TestSetCacheDirRemovesLeftovers(t *testing.T) {
	base := t.TempDir()
	stale := filepath.Join(base, torrentDirName, "play-1", "pieces", "episode.mkv")
	if err := os.MkdirAll(filepath.Dir(stale), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	t.Cleanup(c.Close)
	c.SetCacheDir(base)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("leftover kept: %v", err)
	}
	root, err := c.torrentRoot()
	if err != nil || root != filepath.Join(base, torrentDirName) {
		t.Fatalf("root = %q, %v", root, err)
	}
}

func TestTorrentResolveValidatesFirst(t *testing.T) {
	c := NewCore()
	t.Cleanup(c.Close)
	start := time.Now()
	out := c.Result(c.Start(`{"version":1,"operation":"resolve","provider":"nyaa","mode":"sub","show_id":"","episode":1}`))
	var resp response
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil || resp.Error.Code != "invalid_request" || time.Since(start) > 5*time.Second {
		t.Fatalf("resolve = %s", out)
	}
}
