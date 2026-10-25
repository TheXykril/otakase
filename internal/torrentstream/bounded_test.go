package torrentstream

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

func TestBoundedStorageRejectsBeforeOpening(t *testing.T) {
	for _, info := range []*metainfo.Info{{Length: BotTorrentLimit + 1}, {Length: 0}, {Files: make([]metainfo.FileInfo, 257)}} {
		rejected := make(chan error, 1)
		s := boundedStorage{rejected: rejected, limit: BotTorrentLimit}
		if _, err := s.OpenTorrent(context.Background(), info, metainfo.Hash{}); err == nil {
			t.Fatal("oversized storage accepted")
		}
		select {
		case <-rejected:
		default:
			t.Fatal("caller was not notified")
		}
	}
}

func TestStreamForBotLocalSwarm(t *testing.T) {
	seedDir := t.TempDir()
	payload := bytes.Repeat([]byte("local episode fixture"), 4096)
	source := filepath.Join(seedDir, "episode.mkv")
	if err := os.WriteFile(source, payload, 0600); err != nil {
		t.Fatal(err)
	}
	info := metainfo.Info{PieceLength: 16 << 10}
	if err := info.BuildFromFilePath(source); err != nil {
		t.Fatal(err)
	}
	raw, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := &metainfo.MetaInfo{InfoBytes: raw}
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = seedDir
	cfg.ListenPort = 0
	cfg.NoDHT = true
	cfg.NoDefaultPortForwarding = true
	cfg.DisableTrackers = true
	cfg.Seed = true
	cfg.ListenHost = func(string) string { return "127.0.0.1" }
	cfg.DisableIPv6 = true
	seed, err := torrent.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer seed.Close()
	tr, err := seed.AddTorrent(mi)
	if err != nil {
		t.Fatal(err)
	}
	tr.VerifyData()
	port := seed.ListenAddrs()[0].(*net.TCPAddr).Port
	peer := []byte{127, 0, 0, 1, byte(port >> 8), byte(port)}
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bencode.NewEncoder(w).Encode(map[string]interface{}{"interval": 1, "peers": string(peer)})
	}))
	defer tracker.Close()
	old := publicTrackers
	publicTrackers = []string{tracker.URL}
	defer func() { publicTrackers = old }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dir := t.TempDir()
	stream, err := StreamForBot(ctx, mi.HashInfoBytes().HexString(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A range from the middle answers before the rest has to be fetched.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, stream.URL, nil)
	req.Header.Set("Range", "bytes=40000-40099")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	part, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusPartialContent || !bytes.Equal(part, payload[40000:40100]) {
		t.Fatalf("range answer %d differs", res.StatusCode)
	}
	if res, err = http.Get(stream.URL + ".other"); err == nil {
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("unknown path answered %d", res.StatusCode)
		}
	}
	path, err := stream.WaitComplete(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, filepath.Join(dir, "pieces")) || strings.Contains(filepath.Base(path), "episode") {
		t.Fatalf("episode stored under the torrent's own name: %s", path)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("download differs: %v", err)
	}
	if done, length, _ := stream.Progress(); done != length || !stream.Complete() {
		t.Fatalf("progress %d of %d", done, length)
	}
	stream.Close()
	if _, err = os.Stat(filepath.Join(dir, "pieces")); !os.IsNotExist(err) {
		t.Fatal("torrent cache retained")
	}
}

func TestBotFileName(t *testing.T) {
	info := &metainfo.Info{Name: "[Group] Show"}
	a := botFileName(info, &metainfo.FileInfo{Path: []string{"Show - 01.mkv"}})
	b := botFileName(info, &metainfo.FileInfo{Path: []string{"Show - 02.mkv"}})
	evil := botFileName(info, &metainfo.FileInfo{Path: []string{"..", "x,movie=a:si=0"}})
	if a == b || !strings.HasSuffix(a, ".mkv") {
		t.Fatalf("names %q %q", a, b)
	}
	if strings.Count(evil, ".") != 1 || strings.ContainsAny(evil, "/,:=") {
		t.Fatalf("unsafe name %q", evil)
	}
}

func TestReadBotTorrentLimits(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		env                      map[string]string
		wantTorrent, wantStorage int64
	}{
		{"defaults", nil, 4 << 30, 8 << 30},
		{"custom", map[string]string{"OTAKASE_WATCH_TORRENT_MAX_GIB": "6", "OTAKASE_WATCH_TORRENT_STORAGE_GIB": "12"}, 6 << 30, 12 << 30},
		{"bad values", map[string]string{"OTAKASE_WATCH_TORRENT_MAX_GIB": "-1", "OTAKASE_WATCH_TORRENT_STORAGE_GIB": "not-a-number"}, 4 << 30, 8 << 30},
		{"oversized", map[string]string{"OTAKASE_WATCH_TORRENT_MAX_GIB": "99", "OTAKASE_WATCH_TORRENT_STORAGE_GIB": "99"}, 4 << 30, 8 << 30},
		{"small storage", map[string]string{"OTAKASE_WATCH_TORRENT_MAX_GIB": "6", "OTAKASE_WATCH_TORRENT_STORAGE_GIB": "4"}, 4 << 30, 4 << 30},
		{"largest", map[string]string{"OTAKASE_WATCH_TORRENT_MAX_GIB": "16", "OTAKASE_WATCH_TORRENT_STORAGE_GIB": "32"}, 16 << 30, 32 << 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := readBotTorrentLimits(func(key string) string { return tc.env[key] })
			if got.torrent != tc.wantTorrent || got.storage != tc.wantStorage {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestSourceExt(t *testing.T) {
	for in, want := range map[string]string{
		"Show - 01.mkv":         ".mkv",
		"Show - 01.MP4":         ".mp4",
		"a.x,movie=/etc/passwd": ".mkv",
		"a.mkv:si=0":            ".mkv",
		"no extension":          ".mkv",
		"[Group] Show 01.webm":  ".webm",
		"Show.01.avi'":          ".mkv",
	} {
		if got := sourceExt(in); got != want {
			t.Errorf("sourceExt(%q) = %q, want %q", in, got, want)
		}
	}
}
