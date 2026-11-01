package mobilecore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thexykril/otakase/internal/providers/nyaa"
	"github.com/thexykril/otakase/internal/torrentstream"
)

// torrentRequestTimeout bounds a torrent resolve: the source search plus the
// wait for each tried release's file list.
const torrentRequestTimeout = 3 * time.Minute

// torrentMetadataWait is how long one release gets to name its files. A
// swarm with nobody reachable never answers, and the next release may.
const torrentMetadataWait = 40 * time.Second

// torrentCandidates is how many releases a resolve tries, best first.
const torrentCandidates = 3

// torrentDirName is the folder under the cache dir that holds downloads.
const torrentDirName = "otakase-torrent"

var errPlaybackClosed = errors.New("playback closed")

// SetCacheDir sets where torrent sources keep the episode being played.
// Android passes the app's cache directory: the system temp directory is not
// writable there. Anything a previous run left behind is removed.
func (c *Core) SetCacheDir(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	c.mu.Lock()
	c.cacheDir = dir
	busy := c.torrent != nil
	c.mu.Unlock()
	if !busy {
		os.RemoveAll(filepath.Join(dir, torrentDirName))
	}
}

func (c *Core) torrentRoot() (string, error) {
	c.mu.Lock()
	base := c.cacheDir
	c.mu.Unlock()
	if base == "" {
		base = os.TempDir()
	}
	root := filepath.Join(base, torrentDirName)
	return root, os.MkdirAll(root, 0o700)
}

// isTorrentResolve reports whether input asks a torrent source for a stream,
// which needs longer than other requests.
func isTorrentResolve(input string) bool {
	var peek struct {
		Operation string `json:"operation"`
		Provider  string `json:"provider"`
	}
	if len(input) > 16<<10 || json.Unmarshal([]byte(input), &peek) != nil {
		return false
	}
	return peek.Operation == "resolve" && (peek.Provider == "nyaa" || peek.Provider == "sukebei")
}

// resolveTorrent joins the best release of the episode that answers and
// serves it on a loopback port. Only one torrent runs at a time: it replaces
// the previous playback and is deleted when playback stops.
func (c *Core) resolveTorrent(ctx context.Context, p *nyaa.Provider, req request) ([]stream, error) {
	releases, err := p.EpisodeReleases(req.ShowID, req.Episode, req.Mode, req.Quality)
	if err != nil {
		return nil, err
	}
	root, err := c.torrentRoot()
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, release := range releases[:min(len(releases), torrentCandidates)] {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		dir, err := os.MkdirTemp(root, "play-*")
		if err != nil {
			return nil, err
		}
		wait, cancel := context.WithTimeout(ctx, torrentMetadataWait)
		s, err := torrentstream.StreamForBot(wait, release.InfoHash, dir, nil)
		cancel()
		if err != nil {
			os.RemoveAll(dir)
			lastErr = err
			continue
		}
		stop := func() {
			c.mu.Lock()
			if c.torrent == s {
				c.torrent = nil
			}
			c.mu.Unlock()
			s.Close()
			os.RemoveAll(dir)
		}
		if ctx.Err() != nil {
			stop()
			return nil, ctx.Err()
		}
		if !c.replaceRelays([]func(){stop}) {
			return nil, errPlaybackClosed
		}
		c.mu.Lock()
		c.torrent = s
		c.mu.Unlock()
		return []stream{{URL: s.URL, Headers: map[string]string{}, Subtitles: []subtitle{}, Torrent: true}}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no release would start")
	}
	return nil, lastErr
}

// torrentProgress describes the torrent being played, for a loading screen.
func (c *Core) torrentProgress() map[string]any {
	c.mu.Lock()
	s := c.torrent
	c.mu.Unlock()
	if s == nil {
		return map[string]any{"active": false}
	}
	completed, length, peers := s.Progress()
	return map[string]any{
		"active":    true,
		"completed": completed,
		"length":    length,
		"ahead":     s.Ahead(),
		"peers":     peers,
	}
}
