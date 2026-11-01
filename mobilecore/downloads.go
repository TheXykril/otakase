package mobilecore

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Downloads save an episode or a film to a file for watching offline. The
// app resolves a stream as it does for playing and hands it here; the file
// is written beside a .part file and renamed when complete.
//
// A whole file is fetched as it is, resuming a .part left by an earlier try.
// A playlist (HLS) goes to ffmpeg when the desktop has one, which also joins
// a separate audio track; otherwise its segments are fetched one by one,
// decrypted when the playlist says so, and joined into one .ts or .mp4.

const (
	downloadRunning   = "running"
	downloadDone      = "done"
	downloadFailed    = "failed"
	downloadCancelled = "cancelled"
)

type downloadSet struct {
	mu    sync.Mutex
	next  int64
	items map[int64]*download
}

type download struct {
	cancel context.CancelFunc

	mu         sync.Mutex
	state      string
	bytes      int64
	total      int64
	parts      int
	partsTotal int
	err        string
	path       string
}

type downloadStatus struct {
	ID         int64  `json:"id"`
	State      string `json:"state"`
	Bytes      int64  `json:"bytes"`
	Total      int64  `json:"total"`
	Parts      int    `json:"parts"`
	PartsTotal int    `json:"parts_total"`
	Error      string `json:"error,omitempty"`
	Path       string `json:"path,omitempty"`
}

func (d *download) status(id int64) downloadStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	return downloadStatus{ID: id, State: d.state, Bytes: d.bytes, Total: d.total, Parts: d.parts, PartsTotal: d.partsTotal, Error: d.err, Path: d.path}
}

func (d *download) update(change func(*download)) {
	d.mu.Lock()
	change(d)
	d.mu.Unlock()
}

func (s *downloadSet) cancelAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.items {
		d.cancel()
	}
}

func (c *Core) executeDownload(req movieRequest) string {
	set := &c.downloads
	switch req.Operation {
	case "download_start":
		u, err := url.Parse(req.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return failure("invalid_request", "A web address is required.", false)
		}
		base := filepath.Clean(req.Path)
		if req.Path == "" || !filepath.IsAbs(base) || strings.HasSuffix(req.Path, string(filepath.Separator)) {
			return failure("invalid_request", "A file path is required.", false)
		}
		if err := os.MkdirAll(filepath.Dir(base), 0o755); err != nil {
			return failure("storage", "The download folder cannot be written.", false)
		}
		c.mu.Lock()
		closed := c.closed
		c.mu.Unlock()
		if closed {
			return failure("cancelled", "Core closed.", false)
		}
		ctx, cancel := context.WithCancel(context.Background())
		d := &download{cancel: cancel, state: downloadRunning}
		set.mu.Lock()
		if set.items == nil {
			set.items = map[int64]*download{}
		}
		running := 0
		for _, other := range set.items {
			if other.status(0).State == downloadRunning {
				running++
			}
		}
		if running >= 4 {
			set.mu.Unlock()
			cancel()
			return failure("busy", "Four downloads are already running.", true)
		}
		set.next++
		id := set.next
		set.items[id] = d
		set.mu.Unlock()
		job := downloadJob{url: req.URL, headers: req.Headers, hls: req.HLS || strings.Contains(strings.ToLower(u.Path), ".m3u8"), base: base, quality: req.Quality, transport: c.transport, d: d}
		go func() {
			defer cancel()
			path, err := job.run(ctx)
			d.update(func(d *download) {
				switch {
				case ctx.Err() != nil:
					d.state = downloadCancelled
				case err != nil:
					d.state = downloadFailed
					d.err = err.Error()
				default:
					d.state = downloadDone
					d.path = path
				}
			})
		}()
		return encode(response{Version: 1, Data: map[string]any{"id": id}})
	case "download_status":
		set.mu.Lock()
		list := []downloadStatus{}
		for id, d := range set.items {
			if req.ID == 0 || req.ID == id {
				list = append(list, d.status(id))
			}
		}
		set.mu.Unlock()
		return encode(response{Version: 1, Data: map[string]any{"downloads": list}})
	case "download_cancel", "download_forget":
		set.mu.Lock()
		d := set.items[req.ID]
		if req.Operation == "download_forget" && d != nil && d.status(0).State != downloadRunning {
			delete(set.items, req.ID)
		}
		set.mu.Unlock()
		if d != nil && req.Operation == "download_cancel" {
			d.cancel()
		}
		return encode(response{Version: 1, Data: map[string]bool{"ok": d != nil}})
	}
	return failure("invalid_request", "Unknown operation.", false)
}

type downloadJob struct {
	url       string
	headers   map[string]string
	hls       bool
	base      string
	quality   int
	transport http.RoundTripper
	d         *download
}

func (j downloadJob) client() *http.Client {
	return &http.Client{Transport: j.transport}
}

func (j downloadJob) get(ctx context.Context, link string, extra map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36")
	for k, v := range j.headers {
		req.Header.Set(k, v)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := j.client().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return nil, fmt.Errorf("the server answered %d", resp.StatusCode)
	}
	return resp, nil
}

func (j downloadJob) run(ctx context.Context) (string, error) {
	if !j.hls {
		return j.file(ctx)
	}
	if ffmpeg := findFFmpeg(); ffmpeg != "" {
		return j.ffmpeg(ctx, ffmpeg)
	}
	return j.playlist(ctx)
}

// findFFmpeg is replaced in tests.
var findFFmpeg = desktopFFmpeg

// desktopFFmpeg is ffmpeg's path on a desktop that has it installed.
func desktopFFmpeg() string {
	if runtime.GOOS == "android" || runtime.GOOS == "ios" {
		return ""
	}
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return ""
	}
	return path
}

func fileExtension(link string) string {
	u, err := url.Parse(link)
	if err == nil {
		switch ext := strings.ToLower(filepath.Ext(u.Path)); ext {
		case ".mp4", ".mkv", ".webm", ".m4v", ".avi", ".ts":
			return ext
		}
	}
	return ".mp4"
}

// file fetches a whole file, resuming a .part an earlier try left.
func (j downloadJob) file(ctx context.Context) (string, error) {
	final := j.base + fileExtension(j.url)
	part := final + ".part"
	out, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer out.Close()
	have, _ := out.Seek(0, io.SeekEnd)
	extra := map[string]string{}
	if have > 0 {
		extra["Range"] = "bytes=" + strconv.FormatInt(have, 10) + "-"
	}
	resp, err := j.get(ctx, j.url, extra)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		// The server sent the whole file again.
		have = 0
		if err := out.Truncate(0); err != nil {
			return "", err
		}
		if _, err := out.Seek(0, io.SeekStart); err != nil {
			return "", err
		}
	}
	total := int64(-1)
	if resp.ContentLength >= 0 {
		total = have + resp.ContentLength
	}
	j.d.update(func(d *download) { d.bytes, d.total = have, max(total, 0) })
	w := &progressWriter{w: out, d: j.d}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return "", err
	}
	if total > 0 && have+w.n != total {
		return "", errors.New("the download ended early")
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return final, os.Rename(part, final)
}

type progressWriter struct {
	w io.Writer
	d *download
	n int64
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.n += int64(n)
	p.d.update(func(d *download) { d.bytes += int64(n) })
	return n, err
}

// ffmpeg copies the playlist's streams into a Matroska file, which takes any
// codec a site sends.
func (j downloadJob) ffmpeg(ctx context.Context, ffmpeg string) (string, error) {
	final := j.base + ".mkv"
	part := j.base + ".part.mkv"
	var headers strings.Builder
	for k, v := range j.headers {
		if strings.ContainsAny(k+v, "\r\n") {
			continue
		}
		headers.WriteString(k + ": " + v + "\r\n")
	}
	args := []string{"-nostdin", "-y", "-loglevel", "error", "-nostats", "-progress", "pipe:1"}
	if headers.Len() > 0 {
		args = append(args, "-headers", headers.String())
	}
	args = append(args, "-i", j.url, "-map", "0:v:0?", "-map", "0:a?", "-map", "0:s?", "-c", "copy", part)
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	hideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", err
	}
	lines := bufio.NewScanner(stdout)
	for lines.Scan() {
		if size, ok := strings.CutPrefix(lines.Text(), "total_size="); ok {
			if n, err := strconv.ParseInt(size, 10, 64); err == nil {
				j.d.update(func(d *download) { d.bytes = n })
			}
		}
	}
	if err := cmd.Wait(); err != nil {
		os.Remove(part)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		message := strings.TrimSpace(stderr.String())
		if len(message) > 300 {
			message = message[len(message)-300:]
		}
		return "", fmt.Errorf("ffmpeg: %s", orText(message, err.Error()))
	}
	return final, os.Rename(part, final)
}

func orText(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

type hlsKey struct {
	method string
	uri    string
	iv     []byte
}

type hlsSegment struct {
	uri string
	key *hlsKey
	seq int64
}

type hlsVariant struct {
	uri       string
	bandwidth int
	height    int
	audio     string
}

func attributes(line string) map[string]string {
	out := map[string]string{}
	_, list, _ := strings.Cut(line, ":")
	for list != "" {
		name, rest, ok := strings.Cut(list, "=")
		if !ok {
			break
		}
		var value string
		if strings.HasPrefix(rest, `"`) {
			end := strings.Index(rest[1:], `"`)
			if end < 0 {
				value, rest = rest[1:], ""
			} else {
				value, rest = rest[1:end+1], rest[end+2:]
			}
			rest = strings.TrimPrefix(rest, ",")
		} else {
			value, rest, _ = strings.Cut(rest, ",")
		}
		out[strings.TrimSpace(name)] = value
		list = rest
	}
	return out
}

func resolveURL(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(strings.TrimSpace(ref))
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

func (j downloadJob) text(ctx context.Context, link string) (string, error) {
	resp, err := j.get(ctx, link, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return string(body), err
}

// pickVariant takes the stream nearest the wanted height without going over
// it, or the best one when no height is wanted.
func pickVariant(variants []hlsVariant, quality int) hlsVariant {
	best := variants[0]
	better := func(v, than hlsVariant) bool {
		if v.height != than.height {
			return v.height > than.height
		}
		return v.bandwidth > than.bandwidth
	}
	fits := func(v hlsVariant) bool { return quality <= 0 || v.height == 0 || v.height <= quality }
	for _, v := range variants[1:] {
		switch {
		case fits(v) && !fits(best):
			best = v
		case fits(v) == fits(best) && better(v, best) == fits(v):
			best = v
		}
	}
	return best
}

// playlist fetches an HLS stream's segments in order and joins them.
func (j downloadJob) playlist(ctx context.Context) (string, error) {
	link := j.url
	body, err := j.text(ctx, link)
	if err != nil {
		return "", err
	}
	if strings.Contains(body, "#EXT-X-STREAM-INF") {
		var variants []hlsVariant
		separateAudio := map[string]bool{}
		lines := strings.Split(body, "\n")
		for i := 0; i < len(lines); i++ {
			line := strings.TrimSpace(lines[i])
			if strings.HasPrefix(line, "#EXT-X-MEDIA:") {
				a := attributes(line)
				if a["TYPE"] == "AUDIO" && a["URI"] != "" {
					separateAudio[a["GROUP-ID"]] = true
				}
			}
			if !strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
				continue
			}
			a := attributes(line)
			v := hlsVariant{audio: a["AUDIO"]}
			v.bandwidth, _ = strconv.Atoi(a["BANDWIDTH"])
			if _, h, ok := strings.Cut(a["RESOLUTION"], "x"); ok {
				v.height, _ = strconv.Atoi(h)
			}
			for i+1 < len(lines) {
				i++
				next := strings.TrimSpace(lines[i])
				if next != "" && !strings.HasPrefix(next, "#") {
					v.uri = resolveURL(link, next)
					break
				}
			}
			if v.uri != "" {
				variants = append(variants, v)
			}
		}
		if len(variants) == 0 {
			return "", errors.New("the playlist lists no streams")
		}
		chosen := pickVariant(variants, j.quality)
		if separateAudio[chosen.audio] {
			return "", errors.New("this copy keeps its sound apart from the picture; downloading it needs ffmpeg")
		}
		link = chosen.uri
		if body, err = j.text(ctx, link); err != nil {
			return "", err
		}
	}
	var segments []hlsSegment
	var key *hlsKey
	var initURI string
	seq := int64(0)
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
		case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
			seq, _ = strconv.ParseInt(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"), 10, 64)
		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			a := attributes(line)
			switch a["METHOD"] {
			case "NONE":
				key = nil
			case "AES-128":
				k := &hlsKey{method: a["METHOD"], uri: resolveURL(link, a["URI"])}
				if iv := strings.TrimPrefix(strings.TrimPrefix(a["IV"], "0x"), "0X"); iv != "" {
					if k.iv, err = hex.DecodeString(iv); err != nil || len(k.iv) != 16 {
						return "", errors.New("the playlist has a bad key")
					}
				}
				key = k
			default:
				return "", fmt.Errorf("the playlist is locked with %s", a["METHOD"])
			}
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			initURI = resolveURL(link, attributes(line)["URI"])
		case strings.HasPrefix(line, "#EXT-X-BYTERANGE"):
			return "", errors.New("the playlist uses byte ranges, which are not supported")
		case strings.HasPrefix(line, "#"):
		default:
			segments = append(segments, hlsSegment{uri: resolveURL(link, line), key: key, seq: seq})
			seq++
		}
	}
	if len(segments) == 0 {
		return "", errors.New("the playlist has no segments")
	}
	ext := ".ts"
	if initURI != "" {
		ext = ".mp4"
	}
	final := j.base + ext
	part := final + ".part"
	out, err := os.Create(part)
	if err != nil {
		return "", err
	}
	defer out.Close()
	j.d.update(func(d *download) { d.partsTotal = len(segments) })
	keys := map[string][]byte{}
	if initURI != "" {
		data, err := j.fetchSegment(ctx, initURI)
		if err != nil {
			return "", err
		}
		if _, err := out.Write(data); err != nil {
			return "", err
		}
	}
	for _, s := range segments {
		data, err := j.fetchSegment(ctx, s.uri)
		if err != nil {
			return "", err
		}
		if s.key != nil {
			secret, ok := keys[s.key.uri]
			if !ok {
				if secret, err = j.fetchSegment(ctx, s.key.uri); err != nil {
					return "", err
				}
				keys[s.key.uri] = secret
			}
			if data, err = decryptSegment(data, secret, s); err != nil {
				return "", err
			}
		}
		if _, err := out.Write(data); err != nil {
			return "", err
		}
		j.d.update(func(d *download) { d.parts++; d.bytes += int64(len(data)) })
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return final, os.Rename(part, final)
}

func (j downloadJob) fetchSegment(ctx context.Context, link string) ([]byte, error) {
	var lastErr error
	for attempt := range 4 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		resp, err := j.get(ctx, link, nil)
		if err != nil {
			lastErr = err
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
		resp.Body.Close()
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// decryptSegment undoes AES-128 CBC. A segment without an IV of its own uses
// its sequence number, as the HLS spec says.
func decryptSegment(data, secret []byte, s hlsSegment) ([]byte, error) {
	if len(secret) != 16 {
		return nil, errors.New("the playlist key is not 16 bytes")
	}
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, errors.New("a segment is not whole")
	}
	iv := s.key.iv
	if iv == nil {
		iv = make([]byte, 16)
		binary.BigEndian.PutUint64(iv[8:], uint64(s.seq))
	}
	block, err := aes.NewCipher(secret)
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, data)
	pad := int(plain[len(plain)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(plain) {
		return plain, nil
	}
	return plain[:len(plain)-pad], nil
}
