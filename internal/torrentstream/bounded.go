package torrentstream

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

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
func readBotTorrentLimits(getenv func(string) string) botTorrentLimits {
	parse := func(name string, fallback int64) int64 {
		n, err := strconv.ParseInt(getenv(name), 10, 64)
		if err != nil || n < 1 || n > maxBotTorrentGiB*2 {
			return fallback
		}
		return n << 30
	}
	torrentLimit := parse("OTAKASE_WATCH_TORRENT_MAX_GIB", BotTorrentLimit)
	storageLimit := parse("OTAKASE_WATCH_TORRENT_STORAGE_GIB", BotTorrentStorageLimit)
	if torrentLimit > maxBotTorrentGiB {
		torrentLimit = BotTorrentLimit
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

// DownloadForBot buffers one bounded episode. Both the cache and output belong
// to the caller's private directory. The cache and extracted video together
// cannot exceed the configured temporary payload budget.
func DownloadForBot(ctx context.Context, hash, dir string) (string, error) {
	limits := readBotTorrentLimits(os.Getenv)
	if limits.storage < 2*limits.torrent {
		return "", fmt.Errorf("torrent storage budget must be at least twice the torrent size limit")
	}
	cache := filepath.Join(dir, "pieces")
	defer os.RemoveAll(cache)
	disk := storage.NewFile(cache)
	defer disk.Close()
	rejected := make(chan error, 1)
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = cache
	cfg.ListenPort = 0
	cfg.NoDefaultPortForwarding = true
	cfg.DefaultStorage = boundedStorage{ClientImpl: disk, rejected: rejected, limit: limits.torrent}
	cfg.NoUpload, cfg.Seed = true, false
	cfg.EstablishedConnsPerTorrent = 16
	cfg.HalfOpenConnsPerTorrent, cfg.TotalHalfOpenConns = 4, 4
	cfg.DownloadRateLimiter = rate.NewLimiter(8<<20, 1<<20)
	client, err := torrent.NewClient(cfg)
	if err != nil {
		return "", err
	}
	defer client.Close()
	magnet := "magnet:?xt=urn:btih:" + hash
	for _, tracker := range publicTrackers {
		magnet += "&tr=" + tracker
	}
	t, err := client.AddMagnet(magnet)
	if err != nil {
		return "", err
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case err := <-rejected:
		return "", err
	case <-t.GotInfo():
	}
	f := largestVideoFile(t)
	if f == nil {
		return "", fmt.Errorf("torrent contains no video")
	}
	path := filepath.Join(dir, "source"+filepath.Ext(f.Path()))
	out, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer out.Close()
	reader := f.NewReader()
	defer reader.Close()
	reader.SetContext(ctx)
	reader.SetReadahead(8 << 20)
	_, err = io.Copy(out, reader)
	if err != nil {
		return "", err
	}
	return path, out.Close()
}
