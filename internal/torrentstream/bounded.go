package torrentstream

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"golang.org/x/time/rate"
)

const (
	BotTorrentLimit        int64 = 4 << 30
	BotTorrentStorageLimit int64 = 8 << 30
	maxBotTorrentGiB       int64 = 16
)

type botTorrentLimits struct {
	torrent int64
	storage int64
}

// Limits are expressed in GiB. An invalid setting falls back to its default.
// The episode plays from the torrent cache itself, so the storage budget only
// has to hold the torrent; a smaller budget lowers the torrent limit.
func readBotTorrentLimits(getenv func(string) string) botTorrentLimits {
	parse := func(name string, fallback, maxGiB int64) int64 {
		n, err := strconv.ParseInt(getenv(name), 10, 64)
		if err != nil || n < 1 || n > maxGiB {
			return fallback
		}
		return n << 30
	}
	torrentLimit := parse("OTAKASE_WATCH_TORRENT_MAX_GIB", BotTorrentLimit, maxBotTorrentGiB)
	storageLimit := parse("OTAKASE_WATCH_TORRENT_STORAGE_GIB", BotTorrentStorageLimit, maxBotTorrentGiB*2)
	if torrentLimit > storageLimit {
		torrentLimit = storageLimit
	}
	return botTorrentLimits{torrent: torrentLimit, storage: storageLimit}
}

type boundedStorage struct {
	storage.ClientImpl
	rejected chan error
	limit    int64
}

func (s boundedStorage) OpenTorrent(ctx context.Context, info *metainfo.Info, hash metainfo.Hash) (storage.TorrentImpl, error) {
	total := info.Length
	valid := total >= 0 && total <= s.limit && len(info.Files) <= 256
	if len(info.Files) > 0 {
		total = 0
		for _, file := range info.Files {
			if file.Length < 0 || file.Length > s.limit-total {
				valid = false
				break
			}
			total += file.Length
		}
	}
	if !valid || total <= 0 {
		err := fmt.Errorf("torrent exceeds watch-room size or file limit")
		select {
		case s.rejected <- err:
		default:
		}
		return storage.TorrentImpl{}, err
	}
	return s.ClientImpl.OpenTorrent(ctx, info, hash)
}

// BotStream is one episode a watch-room helper plays while it downloads.
// ffmpeg reads URL like any web source: reads block until the pieces they
// need arrive, and those pieces jump the queue, so playback starts after the
// first few megabytes instead of after the whole file.
type BotStream struct {
	URL string

	client *torrent.Client
	disk   storage.ClientImplCloser
	file   *torrent.File
	local  string
	server *http.Server
	cache  string
}

// StreamForBot joins the torrent and serves its episode on a loopback port.
// The cache lives in the caller's private directory and stays within the
// configured size limit. ctx bounds the wait for the torrent's file list;
// meanwhile waiting, when set, hears every few seconds how many peers are
// connected and how many are known.
func StreamForBot(ctx context.Context, hash, dir string, waiting func(connected, known int)) (_ *BotStream, err error) {
	limits := readBotTorrentLimits(os.Getenv)
	s := &BotStream{cache: filepath.Join(dir, "pieces")}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	// Files are stored under fixed safe names, so the finished episode can
	// be handed to ffmpeg's subtitle filter by path.
	s.disk = storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir: s.cache,
		FilePathMaker: func(opts storage.FilePathMakerOpts) string { return botFileName(opts.Info, opts.File) },
	})
	rejected := make(chan error, 1)
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = s.cache
	cfg.ListenPort = 0
	cfg.NoDefaultPortForwarding = true
	cfg.DefaultStorage = boundedStorage{ClientImpl: s.disk, rejected: rejected, limit: limits.torrent}
	cfg.NoUpload, cfg.Seed = true, false
	cfg.EstablishedConnsPerTorrent = 16
	cfg.HalfOpenConnsPerTorrent, cfg.TotalHalfOpenConns = 4, 4
	cfg.DownloadRateLimiter = rate.NewLimiter(8<<20, 1<<20)
	s.client, err = torrent.NewClient(cfg)
	if err != nil {
		// Containers often have no IPv6 at all; listen on IPv4 only there.
		cfg.DisableIPv6 = true
		s.client, err = torrent.NewClient(cfg)
	}
	if err != nil {
		return nil, err
	}
	magnet := "magnet:?xt=urn:btih:" + hash
	for _, tracker := range publicTrackers {
		magnet += "&tr=" + tracker
	}
	t, err := s.client.AddMagnet(magnet)
	if err != nil {
		return nil, err
	}
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for info := false; !info; {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err = <-rejected:
			return nil, err
		case <-t.GotInfo():
			info = true
		case <-tick.C:
			if waiting != nil {
				stats := t.Stats()
				waiting(stats.ActivePeers, stats.TotalPeers)
			}
		}
	}
	s.file = largestVideoFile(t)
	if s.file == nil {
		return nil, fmt.Errorf("torrent contains no video")
	}
	fi := s.file.FileInfo()
	s.local = filepath.Join(s.cache, botFileName(t.Info(), &fi))
	// The whole episode downloads in the background, the head and the tail
	// first: players read the container header and Matroska's seek index
	// before the first frame.
	s.file.SetPriority(torrent.PiecePriorityNormal)
	if info := t.Info(); info.PieceLength > 0 {
		begin, end := s.file.BeginPieceIndex(), s.file.EndPieceIndex()
		edge := int((tailBytes + info.PieceLength - 1) / info.PieceLength)
		for i := begin; i < end; i++ {
			if i < begin+edge || i >= end-edge {
				t.Piece(i).SetPriority(torrent.PiecePriorityHigh)
			}
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s.URL = fmt.Sprintf("http://127.0.0.1:%d/source%s", listener.Addr().(*net.TCPAddr).Port, sourceExt(s.file.Path()))
	s.server = &http.Server{Handler: http.HandlerFunc(s.serve), ReadHeaderTimeout: 5 * time.Second}
	go s.server.Serve(listener)
	return s, nil
}

func (s *BotStream) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/source"+sourceExt(s.file.Path()) {
		http.NotFound(w, r)
		return
	}
	reader := s.file.NewReader()
	defer reader.Close()
	reader.SetContext(r.Context())
	// Responsive: a seek re-prioritises the pieces at the new position
	// rather than waiting for the sequential download to get there.
	reader.SetReadahead(8 << 20)
	reader.SetResponsive()
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, "", time.Time{}, reader)
}

// Complete reports whether the whole episode is on disk.
func (s *BotStream) Complete() bool {
	return s.file.BytesCompleted() >= s.file.Length()
}

// Progress reports the downloaded and total bytes and the connected peers.
func (s *BotStream) Progress() (completed, length int64, peers int) {
	return s.file.BytesCompleted(), s.file.Length(), s.file.Torrent().Stats().ActivePeers
}

// WaitComplete waits for the whole episode and returns its local path.
func (s *BotStream) WaitComplete(ctx context.Context) (string, error) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for !s.Complete() {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-tick.C:
		}
	}
	// A finished file loses its .part suffix once every piece it shares
	// with its neighbours is in too; both names hold the whole episode.
	for _, path := range []string{s.local, s.local + ".part"} {
		if info, err := os.Stat(path); err == nil && info.Size() == s.file.Length() {
			return path, nil
		}
	}
	return "", fmt.Errorf("downloaded episode not found")
}

// Close stops the download and deletes the cache.
func (s *BotStream) Close() {
	if s.server != nil {
		s.server.Close()
	}
	if s.client != nil {
		s.client.Close()
	}
	if s.disk != nil {
		s.disk.Close()
	}
	os.RemoveAll(s.cache)
}

// botFileName is where a torrent's file is stored: a fixed name derived
// from its path, so nothing the torrent names reaches the file system.
func botFileName(info *metainfo.Info, file *metainfo.FileInfo) string {
	name := info.BestName()
	if file != nil && len(file.BestPath()) > 0 {
		name += "/" + strings.Join(file.BestPath(), "/")
	}
	sum := sha1.Sum([]byte(name))
	return hex.EncodeToString(sum[:10]) + sourceExt(name)
}

// sourceExt keeps the extracted file's name safe to put in an ffmpeg filter
// graph: the torrent names the file, so anything unusual becomes .mkv, which
// ffmpeg still opens by its content.
func sourceExt(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".mkv", ".mp4", ".m4v", ".webm", ".avi", ".ts", ".mov", ".wmv", ".flv", ".ogm":
		return ext
	}
	return ".mkv"
}
