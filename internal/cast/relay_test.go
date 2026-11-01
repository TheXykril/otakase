package cast

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func newTestRelay(t *testing.T) *Relay {
	t.Helper()
	r, err := newRelayOn(nil, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func get(t *testing.T, address string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

// tsPacket is one transport stream packet with its sync byte.
func tsPackets(n int, fill byte) []byte {
	out := make([]byte, 0, n*188)
	for range n {
		p := make([]byte, 188)
		p[0] = 0x47
		for i := 1; i < len(p); i++ {
			p[i] = fill
		}
		out = append(out, p...)
	}
	return out
}

func TestRewritePlaylistMaster(t *testing.T) {
	base, _ := url.Parse("https://cdn.example/show/master.m3u8")
	body := "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",URI=\"audio/index.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=800000,AUDIO=\"a\"\nlow/index.m3u8\n"
	got := rewritePlaylist(body, base, func(kind, u string) string { return "[" + kind + " " + u + "]" })
	want := "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",URI=\"[p https://cdn.example/show/audio/index.m3u8]\"\n#EXT-X-STREAM-INF:BANDWIDTH=800000,AUDIO=\"a\"\n[p https://cdn.example/show/low/index.m3u8]\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestRewritePlaylistMedia(t *testing.T) {
	base, _ := url.Parse("https://cdn.example/show/low/index.m3u8")
	body := "#EXTM3U\r\n#EXT-X-KEY:METHOD=AES-128,URI=\"/key.bin\"\r\n#EXTINF:4.0,\r\nseg-1.jpg\r\n#EXT-X-ENDLIST\r\n"
	got := rewritePlaylist(body, base, func(kind, u string) string { return "[" + kind + " " + u + "]" })
	want := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"[o https://cdn.example/key.bin]\"\n#EXTINF:4.0,\n[s https://cdn.example/show/low/seg-1.jpg]\n#EXT-X-ENDLIST\n"
	if got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
}

func TestRelayPassesHeadersAndRewrites(t *testing.T) {
	segment := append([]byte("\x89PNG fake picture header"), tsPackets(4, 1)...)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://site.example/" {
			http.Error(w, "no referer", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/master.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\nmedia.m3u8\n")
		case "/media.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\nseg1.png\n#EXTINF:4,\nseg2.png\n#EXT-X-ENDLIST\n")
		case "/seg1.png", "/seg2.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(segment)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	r := newTestRelay(t)
	stream := r.Serve(upstream.URL+"/master.m3u8", map[string]string{"Referer": "https://site.example/"})
	resp, master := get(t, stream.Playlist)
	if resp.StatusCode != 200 || !strings.Contains(master, "/x/p/") || strings.Contains(master, upstream.URL) {
		t.Fatalf("master not rewritten: %d %s", resp.StatusCode, master)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("no CORS header")
	}
	variant := strings.TrimSpace(strings.Split(master, "\n")[2])
	_, media := get(t, variant)
	lines := strings.Split(media, "\n")
	var seg string
	for _, l := range lines {
		if strings.Contains(l, "/x/s/") {
			seg = l
			break
		}
	}
	if seg == "" || !strings.HasSuffix(seg, ".png.ts") {
		t.Fatalf("segment not rewritten: %s", media)
	}
	resp, body := get(t, seg)
	if resp.Header.Get("Content-Type") != "video/mp2t" || body[0] != 0x47 || len(body) != 4*188 {
		t.Fatalf("segment not cleaned: %s %d", resp.Header.Get("Content-Type"), len(body))
	}

	if d := r.Duration(t.Context()); d != 8 {
		t.Fatalf("duration %v", d)
	}
	_, joined := get(t, stream.Progressive)
	if len(joined) != 8*188 {
		t.Fatalf("progressive length %d", len(joined))
	}
	if start, _ := r.ProgressiveStart(t.Context(), 5); start != 4 {
		t.Fatalf("progressive start %v", start)
	}
	_, later := get(t, stream.Progressive+"?from=4")
	if len(later) != 4*188 {
		t.Fatalf("progressive from 4 length %d", len(later))
	}
}

func TestRelayRefusesUnsignedAddresses(t *testing.T) {
	r := newTestRelay(t)
	stream := r.Serve("https://cdn.example/master.m3u8", nil)
	base := strings.TrimSuffix(stream.Playlist, PlaylistName)
	forged := base + "x/s/AAAA/aHR0cDovLzEyNy4wLjAuMS9zZWNyZXQ/secret.ts"
	if resp, _ := get(t, forged); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("forged address answered %d", resp.StatusCode)
	}
	wrongToken := strings.Replace(stream.Playlist, r.base, "http://"+r.listener.Addr().String()+"/nottoken", 1)
	if resp, _ := get(t, wrongToken); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("wrong token answered %d", resp.StatusCode)
	}
	next := r.Serve("https://cdn.example/other.m3u8", nil)
	if next.Playlist == stream.Playlist {
		t.Fatal("a new source kept the old address")
	}
}

func TestRelayFileRangeAndType(t *testing.T) {
	data := strings.Repeat("0123456789", 100)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, "x", timeZero, strings.NewReader(data))
	}))
	defer upstream.Close()
	r := newTestRelay(t)
	stream := r.Serve(upstream.URL+"/episode.mkv", nil)
	req, _ := http.NewRequest(http.MethodGet, stream.File, nil)
	req.Header.Set("Range", "bytes=10-19")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || string(body) != "0123456789" {
		t.Fatalf("range: %d %q", resp.StatusCode, body)
	}
	if resp.Header.Get("Content-Type") != "video/x-matroska" {
		t.Fatalf("type %q", resp.Header.Get("Content-Type"))
	}
	if !strings.HasSuffix(stream.File, ".mkv") {
		t.Fatalf("file address %s", stream.File)
	}
}

func TestRelayDecryptsProgressive(t *testing.T) {
	key := []byte("0123456789abcdef")
	plain := tsPackets(2, 7)
	encrypt := func(sequence int64) []byte {
		block, _ := aes.NewCipher(key)
		iv := make([]byte, 16)
		binary.BigEndian.PutUint64(iv[8:], uint64(sequence))
		pad := 16 - len(plain)%16
		padded := append(append([]byte{}, plain...), strings.Repeat(string(rune(pad)), pad)...)
		out := make([]byte, len(padded))
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
		return out
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:5\n#EXT-X-KEY:METHOD=AES-128,URI=\"k\"\n#EXTINF:2,\na.ts\n#EXTINF:2,\nb.ts\n#EXT-X-ENDLIST\n")
		case "/k":
			w.Write(key)
		case "/a.ts":
			w.Write(encrypt(5))
		case "/b.ts":
			w.Write(encrypt(6))
		}
	}))
	defer upstream.Close()
	r := newTestRelay(t)
	stream := r.Serve(upstream.URL+"/index.m3u8", nil)
	_, joined := get(t, stream.Progressive)
	if joined != string(plain)+string(plain) {
		t.Fatalf("decrypted %d bytes, want %d", len(joined), 2*len(plain))
	}
}

func TestBestVariant(t *testing.T) {
	base, _ := url.Parse("https://cdn.example/m.m3u8")
	body := "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"aud\",URI=\"a.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=100\nlow.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=900,AUDIO=\"aud\"\nhigh.m3u8\n"
	best, audio := bestVariant(body, base)
	if best != "https://cdn.example/high.m3u8" || !audio {
		t.Fatalf("got %s %v", best, audio)
	}
}

func TestTransportStreamStart(t *testing.T) {
	if _, ok := transportStreamStart(tsPackets(3, 0)); ok {
		t.Fatal("a clean stream was cut")
	}
	if skip, ok := transportStreamStart(append([]byte("junk!"), tsPackets(3, 0)...)); !ok || skip != 5 {
		t.Fatalf("skip %d %v", skip, ok)
	}
	if _, ok := transportStreamStart([]byte("ftypisom not a transport stream at all")); ok {
		t.Fatal("found sync in an mp4")
	}
}

var timeZero time.Time
