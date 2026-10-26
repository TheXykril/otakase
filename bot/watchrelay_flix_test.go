package main

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func flixMaskManifest(text string, key []byte) []byte {
	data := []byte(text)
	for i := range data {
		data[i] ^= key[i%len(key)]
	}
	return []byte(base64.StdEncoding.EncodeToString(data))
}

func TestFlixManifestDecode(t *testing.T) {
	key := bytes.Repeat([]byte{19}, 32)
	plain := "#EXTM3U\n#EXT-X-VERSION:3\n"
	got, err := flixDecodeManifest(flixMaskManifest(plain, key), base64.StdEncoding.EncodeToString(key))
	if err != nil || string(got) != plain {
		t.Fatalf("decoded manifest = %q, %v", got, err)
	}
	if _, err := flixDecodeManifest(flixMaskManifest(plain, key), base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0}, 32))); err == nil {
		t.Fatal("accepted a wrong HLS key")
	}
}

func TestFlixSegmentDecode(t *testing.T) {
	original := bytes.Repeat([]byte{0x47}, 600)
	masked := bytes.Clone(original)
	for i := range masked {
		masked[i] ^= flixSegmentMask[i%len(flixSegmentMask)]
	}
	for _, wrapper := range [][]byte{[]byte("RIFF\x00\x00\x00\x00WEBP"), {137, 80, 78, 71, 13, 10, 26, 10}} {
		for _, sample := range [][]byte{original, masked} {
			got, err := flixDecodeSegment(append(bytes.Clone(wrapper), sample...))
			if err != nil || !bytes.Equal(got, original) {
				t.Fatalf("decoded segment mismatch: %v", err)
			}
		}
	}
}

func TestWatchRelayDecryptsFlixStream(t *testing.T) {
	key := bytes.Repeat([]byte{31}, 32)
	master := "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000000\nsub.m3u8\n"
	child := "#EXTM3U\n#EXTINF:2,\nfragment.webp\n"
	segment := bytes.Repeat([]byte{0x47}, 600)
	masked := bytes.Clone(segment)
	for i := range masked {
		masked[i] ^= flixSegmentMask[i%len(flixSegmentMask)]
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			w.Write(flixMaskManifest(master, key))
		case "/sub.m3u8":
			w.Write(flixMaskManifest(child, key))
		case "/fragment.webp":
			w.Write(append([]byte("RIFF\x00\x00\x00\x00WEBP"), masked...))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	media, entry, _ := newWatchMedia(streamLink{
		URL: upstream.URL + "/master.m3u8",
		FlixCloudKey: base64.StdEncoding.EncodeToString(key),
		Headers: map[string]string{"User-Agent": watchBrowserAgent},
	}, false)
	cache := newSegCache(1 << 20)
	load := func(id string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/activity/api/watch/media/"+media.key+"/"+id, nil)
		media.serve(rec, req, id, cache)
		if rec.Code != 200 {
			t.Fatalf("relay %s returned %d: %s", id, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	first := load(entry)
	if !strings.HasPrefix(first, "#EXTM3U") {
		t.Fatalf("master not decoded: %q", first)
	}
	childID := strings.TrimSpace(strings.Split(first, "\n")[2])
	second := load(childID)
	segmentID := strings.TrimSpace(strings.Split(second, "\n")[2])
	got := load(segmentID)
	if got != string(segment) {
		t.Fatalf("relay segment was not decoded")
	}
}
