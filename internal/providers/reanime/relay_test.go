package reanime

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func encodedManifest(text string, key []byte) []byte {
	encrypted := []byte(text)
	for i := range encrypted {
		encrypted[i] ^= key[i%len(key)]
	}
	return []byte(base64.StdEncoding.EncodeToString(encrypted))
}

func TestDecodeManifest(t *testing.T) {
	key := bytes.Repeat([]byte{17}, 32)
	plain := "#EXTM3U\n#EXT-X-VERSION:3\nsegment.webp\n"
	b64 := base64.StdEncoding.EncodeToString(key)
	got, err := DecodeManifest(encodedManifest(plain, key), b64)
	if err != nil || string(got) != plain {
		t.Fatalf("decoded manifest = %q, %v", got, err)
	}
	got, err = DecodeManifest([]byte(plain), "")
	if err != nil || string(got) != strings.TrimSpace(plain) {
		t.Fatalf("plaintext manifest = %q, %v", got, err)
	}
	if _, err := DecodeManifest(encodedManifest(plain, key), base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0}, 32))); err == nil {
		t.Fatal("accepted incorrect manifest key")
	}
	if _, err := DecodeManifest([]byte("not an HLS manifest"), b64); err == nil {
		t.Fatal("accepted malformed playlist")
	}
}

func TestDecodeFlixSegment(t *testing.T) {
	video := bytes.Repeat([]byte{0x47}, 600)
	for i := range video {
		if i%188 != 0 {
			video[i] = byte(i)
		}
	}
	masked := bytes.Clone(video)
	for i := range masked {
		masked[i] ^= flixSegmentMask[i%len(flixSegmentMask)]
	}
	for _, wrapper := range [][]byte{
		[]byte("RIFF\x00\x00\x00\x00WEBP"),
		{137, 80, 78, 71, 13, 10, 26, 10},
	} {
		for _, payload := range [][]byte{video, masked} {
			got, err := DecodeSegment(append(bytes.Clone(wrapper), payload...))
			if err != nil || !bytes.Equal(got, video) {
				t.Fatalf("wrapped segment = %v, %v", got[:min(len(got), 8)], err)
			}
		}
	}
	got, err := DecodeSegment(video)
	if err != nil || !bytes.Equal(got, video) {
		t.Fatalf("unwrapped segment = %v", err)
	}
	if _, err := DecodeSegment([]byte("RIFF\x00\x00\x00\x00WEBPbad")); err == nil {
		t.Fatal("accepted corrupt wrapped video")
	}
}

func TestHLSRelayDecryptsAndRewrites(t *testing.T) {
	key := bytes.Repeat([]byte{23}, 32)
	manifest := "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=200000\nsub.m3u8\n"
	child := "#EXTM3U\n#EXTINF:2,\nseg.webp\n"
	video := bytes.Repeat([]byte{0x47}, 600)
	masked := bytes.Clone(video)
	for i := range masked {
		masked[i] ^= flixSegmentMask[i%len(flixSegmentMask)]
	}
	var upstreamHeaders []http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHeaders = append(upstreamHeaders, r.Header.Clone())
		switch r.URL.Path {
		case "/master.m3u8":
			_, _ = w.Write(encodedManifest(manifest, key))
		case "/sub.m3u8":
			_, _ = w.Write(encodedManifest(child, key))
		case "/seg.webp":
			_, _ = w.Write(append([]byte("RIFF\x00\x00\x00\x00WEBP"), masked...))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	client := &http.Client{Transport: relayTransport(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(req.Context())
		clone.URL.Scheme = "http"
		clone.URL.Host = strings.TrimPrefix(upstream.URL, "http://")
		clone.Host = clone.URL.Host
		return http.DefaultTransport.RoundTrip(clone)
	})}
	entry, stop, err := StartRelay("https://fetch1.flixcloud.cc/master.m3u8", base64.StdEncoding.EncodeToString(key), client)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	load := func(link string) string {
		t.Helper()
		resp, err := http.Get(link)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("relay returned %d", resp.StatusCode)
		}
		buf := make([]byte, 16<<10)
		n, _ := resp.Body.Read(buf)
		return string(buf[:n])
	}
	first := load(entry)
	if !strings.HasPrefix(first, "#EXTM3U") {
		t.Fatalf("master was not decoded: %.40q", first)
	}
	master := strings.TrimSpace(strings.Split(first, "\n")[2])
	u := entry[:strings.LastIndex(entry, "/")+1]
	second := load(u + master)
	childID := strings.TrimSpace(strings.Split(second, "\n")[2])
	segment := load(u + childID)
	if segment != string(video) {
		t.Fatalf("segment was not decoded: %d bytes", len(segment))
	}
	if len(upstreamHeaders) != 3 {
		t.Fatalf("upstream requests = %d", len(upstreamHeaders))
	}
	for _, h := range upstreamHeaders {
		if h.Get("User-Agent") != userAgent || h.Get("Referer") != flixURL+"/" {
			t.Fatalf("missing playback headers: %v", h)
		}
	}
	stop()
	if _, err := http.Get(entry); err == nil {
		t.Fatal("relay stayed active after stop")
	}
}

type relayTransport func(*http.Request) (*http.Response, error)

func (t relayTransport) RoundTrip(r *http.Request) (*http.Response, error) { return t(r) }

func ExampleDecodeManifest() {
	key := bytes.Repeat([]byte{1}, 32)
	playlist, _ := DecodeManifest(encodedManifest("#EXTM3U\n", key), base64.StdEncoding.EncodeToString(key))
	fmt.Print(string(playlist))
	// Output: #EXTM3U
}
