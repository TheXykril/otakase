package internal

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseStreamInfoArg(t *testing.T) {
	if id, ep, err := ParseStreamInfoArg(" 154587:12 "); err != nil || id != 154587 || ep != 12 {
		t.Fatalf("%d %d %v", id, ep, err)
	}
	for _, bad := range []string{"", "154587", "x:1", "1:0", "-1:2", "1:2:3"} {
		if _, _, err := ParseStreamInfoArg(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestStreamLinksKeepWebLinksBestFirst(t *testing.T) {
	result := ProviderEpisodeResult{
		Links: []string{"magnet:?xt=urn:btih:abc", "https://a.example/480p.mp4", "https://b.example/1080p.mp4"},
		LinkHints: map[string]StreamPlaybackHint{
			"https://b.example/1080p.mp4": {Referrer: "https://site/", Subtitle: "https://b.example/en.vtt"},
		},
	}
	got := streamLinks(result)
	if len(got) != 2 || got[0].URL != "https://b.example/1080p.mp4" || got[0].Referrer != "https://site/" ||
		len(got[0].Subtitles) != 1 || got[1].URL != "https://a.example/480p.mp4" {
		t.Fatalf("%+v", got)
	}
}

func TestTorrentLinksDoNotLeakLoopbackStreams(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef01234567"
	result := ProviderEpisodeResult{Links: []string{
		"http://127.0.0.1:39211/" + hash,
		"https://example.com/episode.mp4",
		"http://127.0.0.1:39211/" + hash,
		"http://127.0.0.1:39211/not-a-hash",
	}}
	torrents := torrentLinks(result)
	if len(torrents) != 1 || torrents[0].InfoHash != hash {
		t.Fatalf("torrent links: %+v", torrents)
	}
	streams := streamLinks(result)
	if len(streams) != 1 || streams[0].URL != "https://example.com/episode.mp4" {
		t.Fatalf("web links: %+v", streams)
	}
}

func TestTorrentLinksFromMagnetWithoutLocalServer(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef01234567"
	result := ProviderEpisodeResult{Links: []string{
		"magnet:?xt=urn:btih:" + hash,
		"magnet:?xt=urn:btih:" + hash,
		"magnet:?xt=urn:btih:not-valid",
	}}
	links := torrentLinks(result)
	if len(links) != 1 || links[0].InfoHash != hash {
		t.Fatalf("magnet infohashes: %+v", links)
	}
	if len(streamLinks(result)) != 0 {
		t.Fatal("magnet link must not be passed to the browser relay")
	}
}

func TestSearchStreamInfoSkipsSlowSiteAndOtherLanguage(t *testing.T) {
	oldLookup, oldWait := streamInfoLookup, streamInfoProviderWait
	t.Cleanup(func() { streamInfoLookup, streamInfoProviderWait = oldLookup, oldWait })
	streamInfoProviderWait = 50 * time.Millisecond
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var mu sync.Mutex
	var asked []string
	streamInfoLookup = func(_ Config, _ *Anime, _ int, mode string, names []string) (ProviderEpisodeResult, error) {
		mu.Lock()
		asked = append(asked, names[0]+" "+mode)
		mu.Unlock()
		switch names[0] {
		case "down":
			<-release
			return ProviderEpisodeResult{}, errors.New("522")
		case "nyaa":
			if mode == "sub" {
				return ProviderEpisodeResult{ProviderName: "nyaa", Links: []string{"magnet:?xt=urn:btih:" + strings.Repeat("ab", 20)}}, nil
			}
		}
		return ProviderEpisodeResult{}, errors.New("not found")
	}
	start := time.Now()
	info, err := searchStreamInfo(Config{SubOrDub: "sub"}, Anime{}, StreamInfo{}, 1, []string{"down", "nyaa", "other"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if info.Provider != "nyaa" || len(info.Torrents) != 1 || info.Mode != "sub" {
		t.Fatalf("info = %+v", info)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("a site that doesn't answer held the lookup for %s", took)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(asked, ","); got != "down sub,nyaa sub,other sub" {
		t.Fatalf("asked %s, want the sub sites only", got)
	}
}
