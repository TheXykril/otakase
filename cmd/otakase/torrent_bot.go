package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/thexykril/otakase/internal/torrentstream"
)

var torrentBotHash = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

const segmentSeconds = 4

type botTrack struct {
	Index     int    `json:"index"`
	CodecType string `json:"codec_type"`
	CodecName string `json:"codec_name"`
	Tags      struct {
		Language string `json:"language"`
		Title    string `json:"title"`
	} `json:"tags"`
	Disposition struct {
		Default     int `json:"default"`
		Forced      int `json:"forced"`
		AttachedPic int `json:"attached_pic"`
	} `json:"disposition"`
}
type botProbe struct {
	Streams []botTrack `json:"streams"`
	Format  struct {
		Duration string `json:"duration"`
	} `json:"format"`
}
type botMedia struct {
	duration               float64
	video, audio, subtitle int
}

func chooseBotMedia(p botProbe, mode string) (botMedia, error) {
	m := botMedia{video: -1, audio: -1, subtitle: -1}
	m.duration, _ = strconv.ParseFloat(p.Format.Duration, 64)
	if math.IsNaN(m.duration) || math.IsInf(m.duration, 0) || m.duration <= 0 || m.duration > 4*3600 {
		return m, fmt.Errorf("unsupported video duration")
	}
	lang := "jpn"
	if mode == "dub" {
		lang = "eng"
	}
	bestAudio, bestSub, subOrdinal := -1, -1, 0
	for _, s := range p.Streams {
		language := strings.ToLower(s.Tags.Language)
		if language == "en" {
			language = "eng"
		}
		if language == "ja" {
			language = "jpn"
		}
		title := strings.ToLower(s.Tags.Title)
		switch s.CodecType {
		case "video":
			if m.video < 0 && s.Disposition.AttachedPic == 0 {
				m.video = s.Index
			}
		case "audio":
			if language != lang || strings.Contains(title, "commentary") {
				continue
			}
			score := s.Disposition.Default
			if score > bestAudio {
				bestAudio = score
				m.audio = s.Index
			}
		case "subtitle":
			if language == "eng" && (s.CodecName == "ass" || s.CodecName == "ssa" || s.CodecName == "subrip" || s.CodecName == "webvtt") {
				signs := s.Disposition.Forced != 0 || strings.Contains(title, "sign") || strings.Contains(title, "song")
				score := s.Disposition.Default
				if mode == "sub" && !signs {
					score += 10
				}
				if mode == "dub" && signs {
					score += 10
				}
				if (mode == "sub" || signs) && score > bestSub {
					bestSub = score
					m.subtitle = subOrdinal
				}
			}
			subOrdinal++
		}
	}
	if m.video < 0 || m.audio < 0 {
		return m, fmt.Errorf("requested %s audio unavailable or unlabelled", mode)
	}
	if mode == "sub" && (m.subtitle < 0 || bestSub < 10) {
		return m, fmt.Errorf("English dialogue subtitles unavailable or unsupported")
	}
	return m, nil
}

type botLimitedOutput struct {
	buffer bytes.Buffer
	limit  int
}

func (b *botLimitedOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		return 0, fmt.Errorf("media output exceeds size limit")
	}
	return b.buffer.Write(p)
}
func botCommandOutput(cmd *exec.Cmd, limit int) ([]byte, error) {
	output := &botLimitedOutput{limit: limit}
	cmd.Stdout = output
	err := cmd.Run()
	return output.buffer.Bytes(), err
}

func probeBotMedia(ctx context.Context, source, mode string) (botMedia, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := botCommandOutput(exec.CommandContext(ctx, "ffprobe", "-v", "error", "-threads", "1", "-show_streams", "-show_format", "-of", "json", source), 1<<20)
	if err != nil {
		return botMedia{}, fmt.Errorf("probe video: %w", err)
	}
	var p botProbe
	if err = json.Unmarshal(out, &p); err != nil {
		return botMedia{}, err
	}
	return chooseBotMedia(p, mode)
}

type botSegments struct {
	ctx         context.Context
	dir, source string
	media       botMedia
	primary     string
	variants    map[string]botMedia
	gate        chan struct{}
	mu          sync.Mutex
	cache       map[string][]byte
	order       []string
}

func (s *botSegments) playlist(mode string) string {
	media := s.media
	if selected, ok := s.variants[mode]; ok { media = selected }
	var b strings.Builder
	fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:4\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n")
	for i := 0; float64(i*segmentSeconds) < media.duration; i++ {
		fmt.Fprintf(&b, "#EXTINF:%.6f,\nseg%d.ts\n", math.Min(segmentSeconds, media.duration-float64(i*segmentSeconds)), i)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}
func (s *botSegments) segment(ctx context.Context, n int, mode string) ([]byte, error) {
	media := s.media
	if selected, ok := s.variants[mode]; ok { media = selected }
	key := fmt.Sprintf("%s:%d", mode, n)
	if n < 0 || float64(n*segmentSeconds) >= media.duration {
		return nil, fmt.Errorf("invalid segment")
	}
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
	s.mu.Lock()
	data := s.cache[key]
	s.mu.Unlock()
	if data != nil {
		return data, nil
	}
	run, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	start := strconv.Itoa(n * segmentSeconds)
	filter := "scale=w=min(1280\\,iw):h=min(720\\,ih):force_original_aspect_ratio=decrease:force_divisible_by=2,setsar=1"
	if media.subtitle >= 0 {
		filter = fmt.Sprintf("setpts=PTS+%s/TB,subtitles=source%s:si=%d,setpts=PTS-STARTPTS,", start, filepath.Ext(s.source), media.subtitle) + filter
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-threads", "1", "-ss", start, "-i", s.source, "-t", "4", "-map", fmt.Sprintf("0:%d", media.video), "-map", fmt.Sprintf("0:%d", media.audio), "-vf", filter, "-filter_threads", "1", "-c:v", "libx264", "-threads", "1", "-preset", "veryfast", "-crf", "26", "-maxrate", "1800k", "-bufsize", "3600k", "-pix_fmt", "yuv420p", "-r", "24", "-c:a", "aac", "-ac", "2", "-b:a", "128k", "-output_ts_offset", start, "-mpegts_copyts", "1", "-muxdelay", "0", "-f", "mpegts", "pipe:1"}
	cmd := exec.CommandContext(run, "ffmpeg", args...)
	cmd.Dir = s.dir
	data, err := botCommandOutput(cmd, 4<<20)
	if err != nil {
		return nil, fmt.Errorf("encode segment: %w", err)
	}
	if len(data) > 4<<20 {
		return nil, fmt.Errorf("segment exceeds size limit")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		s.cache = map[string][]byte{}
	}
	// At most 16 segments (64 MiB), shared by every viewer.
	if len(s.order) >= 16 {
		delete(s.cache, s.order[0])
		s.order = s.order[1:]
	}
	s.cache[key] = data
	s.order = append(s.order, key)
	return data, nil
}
func (s *botSegments) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	mode := s.primary
	path := r.URL.Path
	if strings.HasPrefix(path, "/sub/") || strings.HasPrefix(path, "/dub/") {
		parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)
		mode, path = parts[0], "/" + parts[1]
		if _, ok := s.variants[mode]; !ok { http.NotFound(w, r); return }
	}
	if path == "/playlist.m3u8" {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		io.WriteString(w, s.playlist(mode))
		return
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(path, "/seg"), ".ts")
	n, err := strconv.Atoi(raw)
	if err != nil || path != fmt.Sprintf("/seg%d.ts", n) || n < 0 || float64(n*segmentSeconds) >= s.media.duration {
		http.NotFound(w, r)
		return
	}
	data, err := s.segment(r.Context(), n, mode)
	if err != nil {
		http.Error(w, "Video segment unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method != "HEAD" {
		w.Write(data)
	}
}

func serveTorrentForBot(hash, mode string) error {
	if !torrentBotHash.MatchString(hash) || (mode != "sub" && mode != "dub") {
		return fmt.Errorf("invalid torrent playback request")
	}
	// The parent owns this private temporary root and passes its lock as fd 3.
	if root := os.Getenv("OTAKASE_ROOM_DIR"); root != "" && root == os.Getenv("TMPDIR") && strings.HasPrefix(filepath.Base(root), "otakase-room-torrent-") {
		defer os.RemoveAll(root)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() { io.Copy(io.Discard, os.Stdin); cancel() }()
	dir, err := os.MkdirTemp("", "otakase-room-media-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	download, done := context.WithTimeout(ctx, 10*time.Minute)
	source, err := torrentstream.DownloadForBot(download, hash, dir)
	done()
	if err != nil {
		return err
	}
	media, err := probeBotMedia(ctx, source, mode)
	if err != nil {
		return err
	}
	variants := map[string]botMedia{mode: media}
	other := "sub"
	if mode == "sub" { other = "dub" }
	if alt, altErr := probeBotMedia(ctx, source, other); altErr == nil {
		variants[other] = alt
	}
	segments := &botSegments{ctx: ctx, dir: dir, source: source, media: media, primary: mode, variants: variants, gate: make(chan struct{}, 1)}
	if _, err = segments.segment(ctx, 0, mode); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	server := &http.Server{Handler: segments, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	defer server.Close()
	failed := make(chan error, 1)
	go func() { failed <- server.Serve(listener) }()
	languages := mode
	if len(variants) > 1 { languages = "sub,dub" }
	fmt.Printf("http://127.0.0.1:%d/playlist.m3u8?languages=%s\n", listener.Addr().(*net.TCPAddr).Port, languages)
	select {
	case <-ctx.Done():
		return nil
	case err := <-failed:
		return err
	}
}
