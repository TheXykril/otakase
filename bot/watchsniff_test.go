package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// megavid serves some playlists under addresses with no .m3u8 in them. The
// relay has to treat those as playlists, or the browser is handed a
// "video file" it cannot play.
func TestBarePlaylistAddressIsRelayedAsHLS(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://megavid.invalid/" {
			http.Error(w, "no referer", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/vid/typed":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Write([]byte("#EXTM3U\n"))
		case "/vid/untyped":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte("#EXTM3U\n#EXT-X-VERSION:3\n"))
		case "/episode.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			w.Write([]byte("\x00\x00\x00\x18ftypmp42"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	headers := map[string]string{"Referer": "https://megavid.invalid/"}
	info := streamInfo{
		Streams: []streamLink{
			{URL: upstream.URL + "/vid/typed", Headers: headers},
			{URL: upstream.URL + "/vid/untyped", Headers: headers},
			{URL: upstream.URL + "/episode.mp4", Headers: headers},
			{URL: upstream.URL + "/gone", Headers: headers},
		},
		AudioVariants: map[string]streamLink{"en": {URL: upstream.URL + "/vid/typed", Headers: headers}},
	}
	markHLSLinks(context.Background(), &info)

	for k, want := range []bool{true, true, false, false} {
		if got := info.Streams[k].isHLS(); got != want {
			t.Errorf("%s: HLS %v, want %v", info.Streams[k].URL, got, want)
		}
	}
	if !info.AudioVariants["en"].isHLS() {
		t.Error("the audio variant was not marked")
	}
	if _, entry, _ := newWatchMedia(info.Streams[0], false); !strings.HasSuffix(entry, ".m3u8") {
		t.Errorf("entry %q is not served as a playlist", entry)
	}
	if _, entry, _ := newWatchMedia(info.Streams[2], false); !strings.HasSuffix(entry, ".mp4") {
		t.Errorf("entry %q: a file became a playlist", entry)
	}
}
