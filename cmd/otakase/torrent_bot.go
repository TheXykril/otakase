package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"sort"
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

func probeBotMedia(ctx context.Context, source, mode string, wait time.Duration) (botMedia, error) {
	ctx, cancel := context.WithTimeout(ctx, wait)
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

// botAudioLanguage normalizes ffprobe language tags for the Activity's
// selector and the local relay routes. Unknown tracks retain their stream
// number so they remain selectable without inventing a language.
func botAudioLanguage(tag string, index int) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	tag = strings.SplitN(strings.ReplaceAll(tag, "_", "-"), "-", 2)[0]
	codes := map[string]string{
		"jpn": "ja", "eng": "en", "deu": "de", "ger": "de",
		"spa": "es", "fra": "fr", "fre": "fr", "hin": "hi",
		"ita": "it", "por": "pt", "tam": "ta", "tel": "te",
		"kor": "ko", "zho": "zh", "chi": "zh", "rus": "ru",
		"ara": "ar", "pol": "pl", "tur": "tr", "ukr": "uk",
		"ind": "id", "nld": "nl", "dut": "nl", "tha": "th",
	}
	if code := codes[tag]; code != "" {
		return code
	}
	if len(tag) == 2 {
		for _, ch := range tag {
			if ch < 'a' || ch > 'z' {
				return fmt.Sprintf("track-%d", index)
			}
		}
		return tag
	}
	return fmt.Sprintf("track-%d", index)
}

// probeBotMediaVariants reads ffprobe once and keeps a playable rendition for
// every audio language, not just the Japanese/English tracks used to start
// playback. Duplicates prefer the file's default track.
func probeBotMediaVariants(ctx context.Context, source, mode string, wait time.Duration) (botMedia, map[string]botMedia, error) {
	run, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	out, err := botCommandOutput(exec.CommandContext(run, "ffprobe", "-v", "error", "-threads", "1",
		"-show_streams", "-show_format", "-of", "json", source), 1<<20)
	if err != nil {
		return botMedia{}, nil, fmt.Errorf("probe video: %w", err)
	}
	var p botProbe
	if err = json.Unmarshal(out, &p); err != nil {
		return botMedia{}, nil, err
	}
	primary, err := chooseBotMedia(p, mode)
	if err != nil {
		return botMedia{}, nil, err
	}
	variants := map[string]botMedia{mode: primary}
	defaults := map[string]int{}
	for _, stream := range p.Streams {
		if stream.CodecType != "audio" || strings.Contains(strings.ToLower(stream.Tags.Title), "commentary") {
			continue
		}
		code := botAudioLanguage(stream.Tags.Language, stream.Index)
		if _, exists := variants[code]; exists && stream.Disposition.Default <= defaults[code] {
			continue
		}
		track := primary
		track.audio = stream.Index
		track.subtitle = -1 // Other languages should never burn in English subtitles.
		variants[code] = track
		defaults[code] = stream.Disposition.Default
		if len(variants) >= 26 {
			break
		}
	}
	// Keep sub/dub aliases for older clients and for the primary URL.
	if ja, ok := variants["ja"]; ok {
		if sub, subErr := chooseBotMedia(p, "sub"); subErr == nil {
			ja = sub
		}
		variants["sub"] = ja
	}
	if en, ok := variants["en"]; ok {
		if dub, dubErr := chooseBotMedia(p, "dub"); dubErr == nil {
			en = dub
		}
		variants["dub"] = en
	}
	variants[mode] = primary
	return primary, variants, nil
}

// botSubtitles is the episode's dialogue track as WebVTT. ffmpeg converts
// it while the torrent downloads, so the file grows as the episode comes
// in; until it is finished it carries a marker telling the player to fetch
// it again.
type botSubtitles struct {
	path string
	mu   sync.Mutex
	done bool
}

const botSubtitlesPartial = "NOTE otakase-partial"

// followBotSubtitles starts converting text track ordinal of source. It
// returns nil when ffmpeg cannot convert the track, so the caller burns the
// subtitles in instead.
func followBotSubtitles(ctx context.Context, source, dir string, ordinal int) *botSubtitles {
	if ordinal < 0 {
		return nil
	}
	s := &botSubtitles{path: filepath.Join(dir, "subtitles.vtt")}
	_ = os.Remove(s.path)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-i", source, "-map", fmt.Sprintf("0:s:%d", ordinal), "-c:s", "webvtt",
		"-flush_packets", "1", "-fs", strconv.Itoa(4<<20), "-f", "webvtt", s.path)
	if err := cmd.Start(); err != nil {
		return nil
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		s.mu.Lock()
		s.done = true
		s.mu.Unlock()
		close(exited)
	}()
	// ffmpeg fails at once on a track it cannot convert. One still running
	// after a short while is converting; on a slow torrent its first line
	// may be a while off, and the player is told to check back meanwhile.
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	settle := time.NewTimer(15 * time.Second)
	defer settle.Stop()
	for {
		if data, _ := s.body(); bytes.HasPrefix(data, []byte("WEBVTT")) {
			return s
		}
		select {
		case <-exited:
			if data, _ := s.body(); bytes.HasPrefix(data, []byte("WEBVTT")) {
				return s
			}
			return nil
		case <-settle.C:
			return s
		case <-ctx.Done():
			<-exited
			return nil
		case <-tick.C:
		}
	}
}

// body returns the cues converted so far. An unfinished file is cut after
// its last whole cue and marked partial.
func (s *botSubtitles) body() ([]byte, bool) {
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	f, err := os.Open(s.path)
	if err != nil {
		return nil, done
	}
	defer f.Close()
	data, _ := io.ReadAll(io.LimitReader(f, 4<<20))
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(data, []byte("WEBVTT")) {
		if done {
			return data, done
		}
		data = []byte("WEBVTT\n\n") // nothing written yet
	}
	if done {
		return data, done
	}
	if cut := bytes.LastIndex(data, []byte("\n\n")); cut >= 0 {
		data = data[:cut+2]
	}
	header, rest := data, []byte(nil)
	if split := bytes.Index(data, []byte("\n\n")); split >= 0 {
		header, rest = data[:split], data[split+2:]
	}
	out := append(append([]byte{}, header...), "\n\n"+botSubtitlesPartial+"\n\n"...)
	return append(out, rest...), false
}

// botSegments serves the episode as HLS, transcoded four seconds at a time
// when a player asks. The picture is encoded once and shared by every
// language; each language only adds an audio encode, which is cheap, and
// the two are remuxed into one segment. Each language keeps its own plain
// playlist, which every player, including the light Hls.js build, plays.
type botSegments struct {
	ctx      context.Context
	dir      string
	input    string // what ffmpeg reads: the torrent's loopback URL or a local file
	burnFile string // the local file subtitles are burnt in from, relative to dir
	media    botMedia
	primary  string
	subs     *botSubtitles
	variants map[string]botMedia

	videoGate, audioGate chan struct{}
	mu                   sync.Mutex
	cache                map[string][]byte
	order                []string
	size                 int
}

// botSegmentCache bounds the shared cache of encoded pictures and sound:
// about a minute of video and every language's audio for it.
const botSegmentCache = 64 << 20

func newBotSegments(ctx context.Context, dir, input string, media botMedia, primary string, variants map[string]botMedia) *botSegments {
	return &botSegments{ctx: ctx, dir: dir, input: input, media: media, primary: primary, variants: variants,
		videoGate: make(chan struct{}, 1), audioGate: make(chan struct{}, 2)}
}

func (s *botSegments) variant(mode string) (botMedia, bool) {
	if selected, ok := s.variants[mode]; ok {
		return selected, true
	}
	return s.media, mode == s.primary
}

// segments counts the playlist's segments. A sliver of a few frames at
// the end joins the segment before it: ffmpeg writes nothing for it, and
// the player stops on the failed request.
func (s *botSegments) segments() int {
	n := int(math.Ceil(s.media.duration / segmentSeconds))
	if n > 1 && s.media.duration-float64((n-1)*segmentSeconds) < 1 {
		n--
	}
	return n
}

// duration is segment n's length as an ffmpeg -t value; the last segment
// reads to the end.
func (s *botSegments) duration(n int) string {
	if n == s.segments()-1 {
		return strconv.FormatFloat(s.length(n)+1, 'f', 3, 64)
	}
	return strconv.Itoa(segmentSeconds)
}

// length is how many seconds segment n lasts.
func (s *botSegments) length(n int) float64 {
	if n == s.segments()-1 {
		return math.Max(s.media.duration-float64(n*segmentSeconds), 0.1)
	}
	return segmentSeconds
}

func (s *botSegments) playlist() string {
	var b strings.Builder
	fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:5\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n")
	for i := 0; i < s.segments(); i++ {
		fmt.Fprintf(&b, "#EXTINF:%.6f,\nseg%d.ts\n", s.length(i), i)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

// encode runs one ffmpeg job behind gate, or answers from the cache. A
// request that waited on the gate finds a job someone else ran in the cache.
func (s *botSegments) encode(ctx context.Context, gate chan struct{}, key string, args []string) ([]byte, error) {
	s.mu.Lock()
	data := s.cache[key]
	s.mu.Unlock()
	if data != nil {
		return data, nil
	}
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
	s.mu.Lock()
	data = s.cache[key]
	s.mu.Unlock()
	if data != nil {
		return data, nil
	}
	data, err := s.ffmpeg(ctx, args)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		s.cache = map[string][]byte{}
	}
	if _, ok := s.cache[key]; !ok {
		for len(s.order) > 0 && s.size+len(data) > botSegmentCache {
			s.size -= len(s.cache[s.order[0]])
			delete(s.cache, s.order[0])
			s.order = s.order[1:]
		}
		s.cache[key] = data
		s.order = append(s.order, key)
		s.size += len(data)
	}
	return data, nil
}

func (s *botSegments) ffmpeg(ctx context.Context, args []string) ([]byte, error) {
	// Pieces of a torrent still downloading may take a while to arrive; the
	// first segment, encoded at startup, may wait for peers even longer.
	limit := 45 * time.Second
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) > limit {
		limit = time.Until(deadline)
	}
	run, cancel := context.WithTimeout(s.ctx, limit)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	cmd := exec.CommandContext(run, "ffmpeg", append([]string{"-hide_banner", "-loglevel", "error", "-nostdin"}, args...)...)
	cmd.Dir = s.dir
	stderr := &botLimitedOutput{limit: 2048}
	cmd.Stderr = stderr
	data, err := botCommandOutput(cmd, 4<<20)
	if err != nil {
		if detail := strings.TrimSpace(stderr.buffer.String()); detail != "" {
			lines := strings.Split(detail, "\n")
			err = fmt.Errorf("%w: %s", err, strings.TrimSpace(lines[len(lines)-1]))
		}
		return nil, fmt.Errorf("encode segment: %w", err)
	}
	return data, nil
}

// video encodes segment n of the picture, with subtitle track subtitle
// burnt in when it is not negative.
func (s *botSegments) video(ctx context.Context, n, subtitle int) ([]byte, error) {
	if n < 0 || n >= s.segments() {
		return nil, fmt.Errorf("invalid segment")
	}
	start := strconv.Itoa(n * segmentSeconds)
	filter := "scale=w=min(1280\\,iw):h=min(720\\,ih):force_original_aspect_ratio=decrease:force_divisible_by=2,setsar=1"
	if subtitle >= 0 {
		if s.burnFile == "" {
			return nil, fmt.Errorf("subtitles cannot be burnt in")
		}
		filter = fmt.Sprintf("setpts=PTS+%s/TB,subtitles=%s:si=%d,setpts=PTS-STARTPTS,", start, s.burnFile, subtitle) + filter
	}
	// Every core helps: on a Raspberry Pi one core decodes and encodes a
	// 1080p segment slower than it plays, and the player gives up waiting.
	// No B-frames: they start each segment's decode times a frame or two
	// before its first picture, so the first segment, which cannot go below
	// zero, overlaps the next and Chromium's built-in HLS player stops with
	// "Parsed buffers not in DTS sequence".
	args := []string{"-threads", "0", "-ss", start, "-i", s.input, "-t", s.duration(n),
		"-map", fmt.Sprintf("0:%d", s.media.video), "-an", "-sn", "-vf", filter,
		"-c:v", "libx264", "-threads", "0", "-preset", "veryfast", "-bf", "0", "-crf", "26", "-maxrate", "1800k", "-bufsize", "3600k",
		"-pix_fmt", "yuv420p", "-r", "24", "-output_ts_offset", start, "-mpegts_copyts", "1", "-muxdelay", "0", "-f", "mpegts", "pipe:1"}
	return s.encode(ctx, s.videoGate, fmt.Sprintf("v%d:%d", subtitle, n), args)
}

// audio encodes segment n of audio stream track.
func (s *botSegments) audio(ctx context.Context, n, track int) ([]byte, error) {
	if n < 0 || n >= s.segments() {
		return nil, fmt.Errorf("invalid segment")
	}
	start := strconv.Itoa(n * segmentSeconds)
	args := []string{"-threads", "1", "-ss", start, "-i", s.input, "-t", s.duration(n),
		"-map", fmt.Sprintf("0:%d", track), "-vn", "-sn", "-c:a", "aac", "-ac", "2", "-b:a", "128k",
		"-output_ts_offset", start, "-mpegts_copyts", "1", "-muxdelay", "0", "-f", "mpegts", "pipe:1"}
	return s.encode(ctx, s.audioGate, fmt.Sprintf("a%d:%d", track, n), args)
}

// segment is segment n of language mode: the shared picture and that
// language's sound, encoded side by side and remuxed without re-encoding.
func (s *botSegments) segment(ctx context.Context, n int, mode string) ([]byte, error) {
	media, ok := s.variant(mode)
	if !ok || n < 0 || n >= s.segments() {
		return nil, fmt.Errorf("invalid segment")
	}
	type result struct {
		data []byte
		err  error
	}
	sound := make(chan result, 1)
	go func() {
		data, err := s.audio(ctx, n, media.audio)
		sound <- result{data, err}
	}()
	picture, err := s.video(ctx, n, media.subtitle)
	audio := <-sound
	if err == nil {
		err = audio.err
	}
	if err != nil {
		return nil, err
	}
	parts := make([]string, 2)
	for k, data := range [][]byte{picture, audio.data} {
		f, err := os.CreateTemp(s.dir, "part-*.ts")
		if err != nil {
			return nil, err
		}
		parts[k] = f.Name()
		defer os.Remove(f.Name())
		_, err = f.Write(data)
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, err
		}
	}
	return s.ffmpeg(ctx, []string{"-i", parts[0], "-i", parts[1], "-map", "0:v", "-map", "1:a", "-c", "copy",
		"-copyts", "-mpegts_copyts", "1", "-muxdelay", "0", "-f", "mpegts", "pipe:1"})
}

func (s *botSegments) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	write := func(typ string, data []byte) {
		w.Header().Set("Content-Type", typ)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if r.Method != "HEAD" {
			_, _ = w.Write(data)
		}
	}
	if r.URL.Path == "/subs/en.vtt" && s.subs != nil {
		data, done := s.subs.body()
		if !done {
			w.Header().Set("Cache-Control", "no-store")
		}
		write("text/vtt; charset=utf-8", data)
		return
	}
	mode, path := s.primary, r.URL.Path
	if dir, rest, ok := strings.Cut(strings.TrimPrefix(path, "/"), "/"); ok {
		if _, known := s.variant(dir); !known {
			http.NotFound(w, r)
			return
		}
		mode, path = dir, "/"+rest
	}
	if path == "/playlist.m3u8" {
		write("application/vnd.apple.mpegurl", []byte(s.playlist()))
		return
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(path, "/seg"), ".ts")
	n, err := strconv.Atoi(raw)
	if err != nil || path != fmt.Sprintf("/seg%d.ts", n) || n < 0 || n >= s.segments() {
		http.NotFound(w, r)
		return
	}
	begun := time.Now()
	data, err := s.segment(r.Context(), n, mode)
	if err != nil {
		// The bot writes "note" lines to its log.
		fmt.Fprintf(os.Stderr, "note segment %d (%s) failed after %s: %v\n", n, mode, time.Since(begun).Round(time.Second), err)
		http.Error(w, "Video segment unavailable", http.StatusServiceUnavailable)
		return
	}
	write("video/mp2t", data)
}

// botBufferWait bounds the wait for a slow download before playback starts
// anyway; botStallWait gives up on a download that stopped completely.
const (
	botBufferWait = 8 * time.Minute
	botStallWait  = 3 * time.Minute
)

// bufferBotStream holds playback back until the episode downloads in order
// far enough ahead that it can keep up: a minute of video, or more when the
// download is slower than the video plays, enough that it never catches up
// with the player before the end.
func bufferBotStream(ctx context.Context, stream *torrentstream.BotStream, duration float64, progress *botProgress) error {
	stream.Sequential(ctx)
	_, length, _ := stream.Progress()
	if duration <= 0 || length <= 0 {
		return nil
	}
	need := float64(length) / duration // bytes per second of video
	type sample struct {
		at   time.Time
		done int64
	}
	var samples []sample
	begun, moved := time.Now(), time.Now()
	last, shown := stream.Ahead(), 0
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		ahead := stream.Ahead()
		done, _, _ := stream.Progress()
		now := time.Now()
		if ahead != last {
			last, moved = ahead, now
		}
		samples = append(samples, sample{now, done})
		for len(samples) > 1 && now.Sub(samples[0].at) > 20*time.Second {
			samples = samples[1:]
		}
		speed := -1.0 // not known yet
		if span := now.Sub(samples[0].at).Seconds(); span >= 10 {
			speed = float64(done-samples[0].done) / span
		}
		target := botBufferTarget(need, speed, duration, length)
		switch {
		case float64(ahead) >= target:
			return nil
		case now.Sub(begun) > botBufferWait:
			fmt.Fprintf(os.Stderr, "note buffering stopped after %s at %d of %.0f bytes; playing anyway\n", botBufferWait, ahead, target)
			return nil
		case now.Sub(moved) > botStallWait:
			return fmt.Errorf("download stalled for %s while buffering", botStallWait)
		}
		// The target moves with the speed; the number shown only goes up.
		shown = max(shown, min(int(float64(ahead)*100/target), 99))
		progress.stepPlain(fmt.Sprintf("Buffering %d%%", shown))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// botBufferTarget is how many bytes must be downloaded in order before
// playback starts: a minute of video, or, when the download (speed bytes a
// second, negative while unknown) is slower than the video plays, enough
// that the player never catches up with it before the end.
func botBufferTarget(need, speed, duration float64, length int64) float64 {
	target := need * 60
	if speed < 0 {
		target = need * 90 // no speed yet: assume a little slow
	} else if shortfall := need*1.1 - speed; shortfall > 0 {
		target = max(target, shortfall*duration)
	}
	return min(target, float64(length))
}

// reportBotStream writes the download state to stderr every minute while
// the episode plays; the bot keeps the lines in its log.
func reportBotStream(ctx context.Context, stream *torrentstream.BotStream) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	prev, _, _ := stream.Progress()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		done, length, peers := stream.Progress()
		fmt.Fprintf(os.Stderr, "note download %d%%, %d MB in order, %d KB/s, %d peers\n", done*100/max(length, 1), stream.Ahead()>>20, (done-prev)/60/1024, peers)
		prev = done
		if done >= length {
			return
		}
	}
}

// botProgress tells the bot what the helper is doing before the playlist
// is ready, as "progress <text>" lines on stdout, with the download state
// once the torrent is joined.
type botProgress struct {
	mu      sync.Mutex
	text    string
	stream  *torrentstream.BotStream
	done    chan struct{}
	stopped bool
	plain   bool // the step carries its own percentage
}

func (p *botProgress) step(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.text, p.plain = text, false
	p.printLocked()
}

// stepPlain shows text without the download percentage.
func (p *botProgress) stepPlain(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.text, p.plain = text, true
	p.printLocked()
}

func (p *botProgress) printLocked() {
	if p.stopped {
		return
	}
	line := p.text
	if p.stream != nil && !p.plain {
		done, total, _ := p.stream.Progress()
		if total > 0 {
			line += fmt.Sprintf(" (%d%% loaded)", done*100/total)
		}
	}
	fmt.Printf("progress %s\n", line)
}

// follow repeats the current step with fresh download numbers.
func (p *botProgress) follow(stream *torrentstream.BotStream) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stream = stream
	if p.stopped || p.done != nil {
		return
	}
	p.done = make(chan struct{})
	go func(done chan struct{}) {
		tick := time.NewTicker(3 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				p.mu.Lock()
				p.printLocked()
				p.mu.Unlock()
			}
		}
	}(p.done)
}

func (p *botProgress) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.stopped && p.done != nil {
		close(p.done)
	}
	p.stopped = true
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
	progress := &botProgress{}
	defer progress.stop()
	// Play while downloading: ffmpeg reads the torrent through a loopback
	// server that fetches the pieces it asks for first.
	progress.step("Connecting to source")
	join, joined := context.WithTimeout(ctx, 3*time.Minute)
	// Viewers see neutral wording; peer counts stay out of the room.
	stream, err := torrentstream.StreamForBot(join, hash, dir, func(connected, _ int) {
		progress.step(fmt.Sprintf("Connecting to source (%d connections)", connected))
	})
	joined()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("no torrent peers sent the file list within 3 minutes")
		}
		return err
	}
	defer stream.Close()
	progress.follow(stream)
	progress.step("Reading the video")
	media, variants, err := probeBotMediaVariants(ctx, stream.URL, mode, 3*time.Minute)
	if err != nil {
		return err
	}
	segments := newBotSegments(ctx, dir, stream.URL, media, mode, variants)
	// Soft subtitles let every viewer turn them off; the dub's signs track
	// is used when there is no dialogue track.
	ordinal := media.subtitle
	if sub, ok := variants["sub"]; ok && sub.subtitle >= 0 {
		ordinal = sub.subtitle
	}
	progress.step("Preparing subtitles")
	segments.subs = followBotSubtitles(ctx, stream.URL, dir, ordinal)
	if segments.subs != nil {
		for language, track := range variants {
			track.subtitle = -1
			variants[language] = track
		}
		segments.media.subtitle = -1
	} else if ordinal >= 0 {
		// A track ffmpeg cannot convert is burnt in instead. The subtitle
		// filter reads the whole track up front, so this one case still
		// waits for the full download.
		progress.step("Loading the whole episode for subtitles")
		wait, done := context.WithTimeout(ctx, 10*time.Minute)
		local, err := stream.WaitComplete(wait)
		done()
		if err != nil {
			return fmt.Errorf("download for burnt-in subtitles: %w", err)
		}
		segments.input, segments.burnFile = local, "source"+filepath.Ext(local)
		if strings.HasSuffix(local, ".part") {
			segments.burnFile = "source" + filepath.Ext(strings.TrimSuffix(local, ".part"))
		}
		if err = os.Symlink(local, filepath.Join(dir, segments.burnFile)); err != nil {
			return err
		}
	}
	progress.step("Preparing the first part")
	warm, warmed := context.WithTimeout(ctx, 3*time.Minute)
	_, err = segments.segment(warm, 0, mode)
	warmed()
	if err != nil {
		return fmt.Errorf("first segment: %w", err)
	}
	if err = bufferBotStream(ctx, stream, media.duration, progress); err != nil {
		return err
	}
	go reportBotStream(ctx, stream)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	server := &http.Server{Handler: segments, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	defer server.Close()
	failed := make(chan error, 1)
	go func() { failed <- server.Serve(listener) }()
	// Advertise only real language routes; "sub" and "dub" are aliases.
	languages := make([]string, 0, len(variants))
	for name := range variants {
		if name != "sub" && name != "dub" {
			languages = append(languages, name)
		}
	}
	sort.Strings(languages)
	subquery := ""
	if segments.subs != nil {
		subquery = "&subtitles=en"
	}
	progress.stop()
	fmt.Printf("http://127.0.0.1:%d/playlist.m3u8?languages=%s%s\n", listener.Addr().(*net.TCPAddr).Port, strings.Join(languages, ","), subquery)
	select {
	case <-ctx.Done():
		return nil
	case err := <-failed:
		return err
	}
}
