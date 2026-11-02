package mobilecore

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func waitDownload(t *testing.T, c *Core, id int64) downloadStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var list []downloadStatus
		movieData(t, call(t, c, fmt.Sprintf(`{"version":1,"operation":"download_status","id":%d}`, id)), "downloads", &list)
		if len(list) != 1 {
			t.Fatalf("status list = %+v", list)
		}
		if list[0].State != downloadRunning {
			return list[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("download did not finish")
	return downloadStatus{}
}

func startDownload(t *testing.T, c *Core, fields map[string]any) int64 {
	t.Helper()
	fields["version"] = 1
	fields["operation"] = "download_start"
	raw, _ := json.Marshal(fields)
	var id int64
	movieData(t, call(t, c, string(raw)), "id", &id)
	return id
}

func TestDownloadFileResumesAPart(t *testing.T) {
	body := bytes.Repeat([]byte("0123456789"), 5000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://site.example/" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		http.ServeContent(w, r, "film.mp4", time.Time{}, bytes.NewReader(body))
	}))
	defer server.Close()
	dir := t.TempDir()
	base := filepath.Join(dir, "Film")
	if err := os.WriteFile(base+".mp4.part", body[:1234], 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	defer c.Close()
	id := startDownload(t, c, map[string]any{"url": server.URL + "/v/film.mp4", "path": base, "headers": map[string]string{"Referer": "https://site.example/"}})
	s := waitDownload(t, c, id)
	if s.State != downloadDone || s.Path != base+".mp4" || s.Bytes != int64(len(body)) || s.Total != int64(len(body)) {
		t.Fatalf("status = %+v", s)
	}
	got, _ := os.ReadFile(base + ".mp4")
	if !bytes.Equal(got, body) {
		t.Fatal("file differs")
	}
	if _, err := os.Stat(base + ".mp4.part"); !os.IsNotExist(err) {
		t.Fatal("part file left behind")
	}
	movieData(t, call(t, c, fmt.Sprintf(`{"version":1,"operation":"download_forget","id":%d}`, id)), "ok", new(bool))
	var list []downloadStatus
	movieData(t, call(t, c, `{"version":1,"operation":"download_status"}`), "downloads", &list)
	if len(list) != 0 {
		t.Fatalf("forgotten download still listed: %+v", list)
	}
}

func encrypt(t *testing.T, plain, key, iv []byte) []byte {
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	plain = append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, plain)
	return out
}

func TestDownloadPlaylistPicksQualityAndDecrypts(t *testing.T) {
	old := findFFmpeg
	findFFmpeg = func() string { return "" }
	defer func() { findFFmpeg = old }()
	key := []byte("0123456789abcdef")
	seqIV := make([]byte, 16)
	seqIV[15] = 7
	segments := map[string][]byte{
		"/720/a.ts": encrypt(t, []byte("first-segment"), key, seqIV),
		"/720/b.ts": []byte("second-plain"),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=900000,RESOLUTION=1920x1080\n1080/index.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=500000,RESOLUTION=1280x720\n720/index.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=200000,RESOLUTION=640x360\n360/index.m3u8\n")
		case "/720/index.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:7\n#EXT-X-KEY:METHOD=AES-128,URI=\"/key\"\n#EXTINF:4,\na.ts\n#EXT-X-KEY:METHOD=NONE\n#EXTINF:4,\nb.ts\n#EXT-X-ENDLIST\n")
		case "/key":
			w.Write(key)
		default:
			if data, ok := segments[r.URL.Path]; ok {
				w.Write(data)
				return
			}
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := NewCore()
	defer c.Close()
	base := filepath.Join(t.TempDir(), "Show S1E1")
	id := startDownload(t, c, map[string]any{"url": server.URL + "/master.m3u8", "path": base, "hls": true, "quality": 720})
	s := waitDownload(t, c, id)
	if s.State != downloadDone || s.Path != base+".ts" || s.Parts != 2 || s.PartsTotal != 2 {
		t.Fatalf("status = %+v", s)
	}
	got, _ := os.ReadFile(base + ".ts")
	if string(got) != "first-segmentsecond-plain" {
		t.Fatalf("joined = %q", got)
	}
}

func TestDownloadPlaylistWithSeparateAudioNeedsFFmpeg(t *testing.T) {
	old := findFFmpeg
	findFFmpeg = func() string { return "" }
	defer func() { findFFmpeg = old }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"aud\",NAME=\"English\",URI=\"audio.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=1,RESOLUTION=1280x720,AUDIO=\"aud\"\nvideo.m3u8\n")
	}))
	defer server.Close()
	c := NewCore()
	defer c.Close()
	id := startDownload(t, c, map[string]any{"url": server.URL + "/m.m3u8", "path": filepath.Join(t.TempDir(), "x"), "hls": true})
	s := waitDownload(t, c, id)
	if s.State != downloadFailed || !strings.Contains(s.Error, "ffmpeg") {
		t.Fatalf("status = %+v", s)
	}
}

func TestDownloadCancelStopsIt(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		w.Write([]byte("start"))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	c := NewCore()
	defer c.Close()
	id := startDownload(t, c, map[string]any{"url": server.URL + "/f.mkv", "path": filepath.Join(t.TempDir(), "f")})
	call(t, c, fmt.Sprintf(`{"version":1,"operation":"download_cancel","id":%d}`, id))
	if s := waitDownload(t, c, id); s.State != downloadCancelled {
		t.Fatalf("status = %+v", s)
	}
}

func TestDownloadStartIsChecked(t *testing.T) {
	c := NewCore()
	defer c.Close()
	for _, input := range []string{
		`{"version":1,"operation":"download_start","url":"file:///etc/passwd","path":"/tmp/x"}`,
		`{"version":1,"operation":"download_start","url":"https://a.example/x.mp4","path":"relative"}`,
		`{"version":1,"operation":"download_start","url":"https://a.example/x.mp4"}`,
	} {
		if r := call(t, c, input); r.Error == nil || r.Error.Code != "invalid_request" {
			t.Errorf("%s: got %+v", input, r)
		}
	}
}

func TestPickVariant(t *testing.T) {
	v := []hlsVariant{{uri: "a", height: 1080}, {uri: "b", height: 720}, {uri: "c", height: 360}}
	for quality, want := range map[int]string{0: "a", 1080: "a", 720: "b", 480: "c", 240: "c"} {
		if got := pickVariant(v, quality).uri; got != want {
			t.Errorf("quality %d: got %s, want %s", quality, got, want)
		}
	}
}

func TestDownloadPlaylistWithFFmpeg(t *testing.T) {
	ffmpeg := desktopFFmpeg()
	if ffmpeg == "" {
		t.Skip("no ffmpeg on this machine")
	}
	dir := t.TempDir()
	gen := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=160x90:rate=10", "-f", "lavfi", "-i", "sine", "-t", "2", "-c:v", "mpeg2video", "-c:a", "mp2", "-f", "hls", "-hls_time", "1", filepath.Join(dir, "index.m3u8"))
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot make a test playlist: %v %s", err, out)
	}
	server := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer server.Close()
	c := NewCore()
	defer c.Close()
	base := filepath.Join(t.TempDir(), "clip")
	id := startDownload(t, c, map[string]any{"url": server.URL + "/index.m3u8", "path": base, "hls": true})
	s := waitDownload(t, c, id)
	if s.State != downloadDone || s.Path != base+".mkv" {
		t.Fatalf("status = %+v", s)
	}
	if info, err := os.Stat(base + ".mkv"); err != nil || info.Size() == 0 {
		t.Fatalf("no file: %v", err)
	}
}
