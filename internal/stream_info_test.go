package internal

import "testing"

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
