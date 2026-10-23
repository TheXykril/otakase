package torrentstream

import (
	"bytes"
	"context"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBoundedStorageRejectsBeforeOpening(t *testing.T) {
	for _, info := range []*metainfo.Info{{Length: BotTorrentLimit + 1}, {Length: 0}, {Files: make([]metainfo.FileInfo, 257)}} {
		rejected := make(chan error, 1)
		s := boundedStorage{rejected: rejected}
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

func TestDownloadForBotLocalSwarm(t *testing.T) {
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
	path, err := DownloadForBot(ctx, mi.HashInfoBytes().HexString(), dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("download differs: %v", err)
	}
	if _, err = os.Stat(filepath.Join(dir, "pieces")); !os.IsNotExist(err) {
		t.Fatal("torrent cache retained")
	}
}
