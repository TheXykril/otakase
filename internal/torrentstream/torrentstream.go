// Package torrentstream plays a torrent without waiting for it to finish.
//
// otakase's other providers hand MPV an HTTP URL, so a torrent has to look like
// one too. A local HTTP server backed by a sequential reader does that: pieces
// are requested in the order the player asks for them, playback starts within
// seconds of the first block arriving, and seeking works because the reader
// honours range requests.
//
// Nothing is kept. The data directory is temporary and removed on shutdown --
// this is a player, not a downloader, and a media library filling up with
// half-watched episodes is not what anyone asked for.
package torrentstream

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
)

// publicTrackers are announced to in addition to DHT. Nyaa's magnet links carry
// their own list, but an infohash on its own does not, and a torrent with no
// trackers relies on DHT alone -- which is slow to bootstrap and sometimes
// finds nobody at all.
var publicTrackers = []string{
	"udp://open.stealth.si:80/announce",
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://exodus.desync.com:6969/announce",
	"udp://tracker.torrent.eu.org:451/announce",
	"http://nyaa.tracker.wf:7777/announce",
}

// metadataTimeout bounds the wait for a torrent's file list. A swarm with no
// reachable peers never answers, and the user should get an error rather than a
// menu that appears to hang.
const metadataTimeout = 45 * time.Second

var (
	mu       sync.Mutex
	client   *torrent.Client
	dataDir  string
	listener net.Listener
	served   = map[string]*torrent.File{}
)

// videoExtensions are the containers worth playing; a torrent usually also
// carries samples, subtitles and NFO files.
var videoExtensions = []string{".mkv", ".mp4", ".avi", ".webm", ".mov"}

func isVideo(path string) bool {
	lower := strings.ToLower(path)
	for _, ext := range videoExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// Stream makes the torrent's main video file playable and returns a URL for it.
func Stream(infoHash string) (string, error) {
	infoHash = strings.TrimSpace(strings.ToLower(infoHash))
	if infoHash == "" {
		return "", fmt.Errorf("torrentstream: empty infohash")
	}

	if err := ensureClient(); err != nil {
		return "", err
	}

	magnet := "magnet:?xt=urn:btih:" + infoHash
	for _, tracker := range publicTrackers {
		magnet += "&tr=" + tracker
	}

	mu.Lock()
	t, err := client.AddMagnet(magnet)
	mu.Unlock()
	if err != nil {
		return "", fmt.Errorf("torrentstream: add torrent: %w", err)
	}

	select {
	case <-t.GotInfo():
	case <-time.After(metadataTimeout):
		return "", fmt.Errorf("torrentstream: no peers answered for %s within %s", infoHash, metadataTimeout)
	}

	file := largestVideoFile(t)
	if file == nil {
		return "", fmt.Errorf("torrentstream: %q contains no video file", t.Name())
	}

	// Ask for the start of the file first so playback can begin while the rest
	// is still arriving.
	file.SetPriority(torrent.PiecePriorityNow)

	mu.Lock()
	served[infoHash] = file
	port := listener.Addr().(*net.TCPAddr).Port
	mu.Unlock()

	return fmt.Sprintf("http://127.0.0.1:%d/%s", port, infoHash), nil
}

// largestVideoFile picks the episode out of a torrent's contents. The largest
// video is the feature; anything else is a sample or an extra.
func largestVideoFile(t *torrent.Torrent) *torrent.File {
	files := t.Files()
	candidates := make([]*torrent.File, 0, len(files))
	for _, f := range files {
		if isVideo(f.DisplayPath()) {
			candidates = append(candidates, f)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Length() > candidates[j].Length()
	})
	return candidates[0]
}

func ensureClient() error {
	mu.Lock()
	defer mu.Unlock()
	if client != nil {
		return nil
	}

	dir, err := os.MkdirTemp("", "otakase-torrent-*")
	if err != nil {
		return fmt.Errorf("torrentstream: create cache directory: %w", err)
	}

	config := torrent.NewDefaultClientConfig()
	config.DataDir = dir
	// Seeding while watching is ordinary swarm behaviour and keeps peers willing
	// to serve us, which is what makes the next episode start quickly.
	config.Seed = true

	c, err := torrent.NewClient(config)
	if err != nil {
		os.RemoveAll(dir)
		return fmt.Errorf("torrentstream: start torrent client: %w", err)
	}

	// Port 0: the OS picks a free port, so two otakase instances cannot collide.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		c.Close()
		os.RemoveAll(dir)
		return fmt.Errorf("torrentstream: listen: %w", err)
	}

	client, dataDir, listener = c, dir, l
	go http.Serve(l, http.HandlerFunc(serveFile))
	return nil
}

// serveFile streams one torrent file over HTTP, honouring range requests so the
// player can seek.
func serveFile(w http.ResponseWriter, r *http.Request) {
	infoHash := strings.Trim(r.URL.Path, "/")

	mu.Lock()
	file, ok := served[infoHash]
	mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	reader := file.NewReader()
	defer reader.Close()
	// Read ahead of the player so a brief slow patch in the swarm does not
	// become a stall, and mark the reader responsive so seeks re-prioritise
	// rather than waiting for the sequential download to reach the new point.
	reader.SetReadahead(8 << 20)
	reader.SetResponsive()

	http.ServeContent(w, r, file.DisplayPath(), time.Time{}, reader)
}

// Shutdown stops the client and deletes everything it downloaded.
func Shutdown() {
	mu.Lock()
	defer mu.Unlock()
	if listener != nil {
		listener.Close()
		listener = nil
	}
	if client != nil {
		client.Close()
		client = nil
	}
	if dataDir != "" {
		os.RemoveAll(dataDir)
		dataDir = ""
	}
	served = map[string]*torrent.File{}
}
