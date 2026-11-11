package mobilecore

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadsAreCastFromALoopbackAddress(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Episode 3.mp4")
	sub := filepath.Join(dir, "Episode 3.srt")
	os.WriteFile(video, []byte("0123456789"), 0o644)
	os.WriteFile(sub, []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644)
	in, err := localCastAddresses(castRequest{URL: video, Subtitle: "file://" + sub, Headers: map[string]string{"Referer": "x"}, HLS: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(in.URL, "http://127.0.0.1:") || !strings.HasPrefix(in.Subtitle, "http://127.0.0.1:") || in.Headers != nil || in.HLS {
		t.Fatalf("%+v", in)
	}
	req, _ := http.NewRequest(http.MethodGet, in.URL, nil)
	req.Header.Set("Range", "bytes=4-6")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || string(body) != "456" {
		t.Errorf("range: %d %q", resp.StatusCode, body)
	}
	// Other files on the device are not served.
	other := in.URL[:strings.LastIndex(strings.TrimSuffix(in.URL, "/Episode%203.mp4"), "/")] + "/0000/secret"
	if resp, err := http.Get(other); err == nil {
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("unknown path answered %d", resp.StatusCode)
		}
		resp.Body.Close()
	}
	// Web streams pass as they are.
	web, _ := localCastAddresses(castRequest{URL: "https://cdn.example/a.m3u8", HLS: true})
	if web.URL != "https://cdn.example/a.m3u8" || !web.HLS {
		t.Errorf("web %+v", web)
	}
	if _, err := localCastAddresses(castRequest{URL: filepath.Join(dir, "missing.mp4")}); err == nil {
		t.Error("a missing file was served")
	}
}
