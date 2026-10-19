package internal

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

const testMaster = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360,CODECS="avc1.4d401e,mp4a.40.2"
360/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=1400000,RESOLUTION=854x480,CODECS="avc1.4d401f,mp4a.40.2"
480/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2800000,RESOLUTION=1280x720,CODECS="avc1.4d401f,mp4a.40.2"
/abs/720/index.m3u8?token=a,b
#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080
https://other.example/1080/index.m3u8
`

func TestParseHLSMaster(t *testing.T) {
	base, _ := url.Parse("https://cdn.example/show/ep1/master.m3u8?sig=x")
	variants := parseHLSMaster(testMaster, base)
	if len(variants) != 4 {
		t.Fatalf("got %d variants: %+v", len(variants), variants)
	}
	want := []struct {
		uri       string
		height    int
		bandwidth int
	}{
		{"https://cdn.example/show/ep1/360/index.m3u8", 360, 800000},
		{"https://cdn.example/show/ep1/480/index.m3u8", 480, 1400000},
		{"https://cdn.example/abs/720/index.m3u8?token=a,b", 720, 2800000},
		{"https://other.example/1080/index.m3u8", 1080, 5000000},
	}
	for i, w := range want {
		v := variants[i]
		if v.URI != w.uri || v.Height != w.height || v.Bandwidth != w.bandwidth || v.Index != i {
			t.Errorf("variant %d = %+v, want %+v", i, v, w)
		}
	}
}

func TestParseHLSAttributesKeepsQuotedCommas(t *testing.T) {
	attrs := parseHLSAttributes(`BANDWIDTH=1,CODECS="avc1,mp4a",AUDIO="aud1",RESOLUTION=1x2`)
	if attrs["CODECS"] != "avc1,mp4a" || attrs["AUDIO"] != "aud1" || attrs["RESOLUTION"] != "1x2" {
		t.Fatalf("attrs = %+v", attrs)
	}
}

func TestParseHLSMasterIgnoresAMediaPlaylist(t *testing.T) {
	media := "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4.0,\nseg0.ts\n#EXT-X-ENDLIST\n"
	if variants := parseHLSMaster(media, nil); len(variants) != 0 {
		t.Fatalf("media playlist gave variants: %+v", variants)
	}
}

func TestPickHLSVariantNearestHeight(t *testing.T) {
	variants := parseHLSMaster(testMaster, nil)
	cases := []struct {
		want, got int
	}{
		{1080, 1080},
		{720, 720},
		{600, 480}, // nearest at or below, not the nearer 720
		{2160, 1080},
		{240, 360}, // nothing below: the smallest above
	}
	for _, c := range cases {
		v, ok := pickHLSVariant(variants, c.want)
		if !ok || v.Height != c.got {
			t.Errorf("want %d: picked %+v", c.want, v)
		}
	}
	if _, ok := pickHLSVariant([]hlsVariant{{URI: "audio.m3u8", Bandwidth: 1}}, 720); ok {
		t.Error("picked a variant with no resolution")
	}
}

func TestParseQuality(t *testing.T) {
	for in, want := range map[string]int{"": 0, "best": 0, "BEST": 0, "720": 720, "1080p": 1080, " 480 ": 480, "900": 900, "-1": 0, "high": 0} {
		if got := parseQuality(in); got != want {
			t.Errorf("parseQuality(%q) = %d, want %d", in, got, want)
		}
	}
}

// masterServer serves body as master.m3u8 and counts requests, checking the
// hint's referrer and headers arrive.
func masterServer(t *testing.T, body string) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.Header.Get("Referer") != "https://player.example/" || r.Header.Get("Origin") != "https://player.example" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func qualityTestResult(link string) ProviderEpisodeResult {
	return ProviderEpisodeResult{
		Links: []string{link},
		LinkHints: map[string]StreamPlaybackHint{link: {
			Referrer: "https://player.example/",
			Headers:  map[string]string{"Origin": "https://player.example"},
			Subtitle: "en.vtt",
		}},
	}
}

func TestApplyQualitySubstitutesTheVariant(t *testing.T) {
	master := strings.Replace(testMaster, "https://other.example/1080/index.m3u8", "1080/index.m3u8", 1)
	server, _ := masterServer(t, master)
	link := server.URL + "/show/master.m3u8"
	result := qualityTestResult(link)

	applyQualityPreference(&Config{StoragePath: t.TempDir(), Quality: "720"}, &Anime{}, &result)

	want := server.URL + "/abs/720/index.m3u8?token=a,b"
	if len(result.Links) != 1 || result.Links[0] != want {
		t.Fatalf("links = %v, want %s", result.Links, want)
	}
	hint, ok := result.LinkHints[want]
	if !ok || hint.Subtitle != "en.vtt" || hint.Referrer != "https://player.example/" || hint.HLSBitrate != 0 {
		t.Fatalf("hint did not follow the link: %+v", result.LinkHints)
	}
	if _, stale := result.LinkHints[link]; stale {
		t.Fatal("the master's hint was left behind")
	}
}

func TestApplyQualityKeepsTheMasterWhenAudioIsSeparate(t *testing.T) {
	master := `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="Japanese",DEFAULT=YES,URI="audio/jp.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=1400000,RESOLUTION=854x480,AUDIO="aud"
480/video.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2800000,RESOLUTION=1280x720,AUDIO="aud"
720/video.m3u8
`
	server, _ := masterServer(t, master)
	link := server.URL + "/master.m3u8"
	result := qualityTestResult(link)

	applyQualityPreference(&Config{StoragePath: t.TempDir(), Quality: "480"}, &Anime{}, &result)

	if len(result.Links) != 1 || result.Links[0] != link {
		t.Fatalf("a picture-only variant replaced the master: %v", result.Links)
	}
	if got := result.LinkHints[link].HLSBitrate; got != 1400000 {
		t.Fatalf("HLSBitrate = %d, want the 480p variant's 1400000", got)
	}
	if args := mpvHLSBitrateArgs(result.LinkHints[link].HLSBitrate, nil); len(args) != 1 || args[0] != "--hls-bitrate=1400000" {
		t.Fatalf("mpv args = %v", args)
	}
	if args := mpvHLSBitrateArgs(1400000, []string{"--hls-bitrate=min"}); args != nil {
		t.Fatalf("overrode the viewer's own --hls-bitrate: %v", args)
	}
}

func TestApplyQualityBestLeavesTheResultAlone(t *testing.T) {
	server, hits := masterServer(t, testMaster)
	link := server.URL + "/master.m3u8"
	for _, quality := range []string{"", "best"} {
		result := qualityTestResult(link)
		applyQualityPreference(&Config{StoragePath: t.TempDir(), Quality: quality}, &Anime{}, &result)
		if result.Links[0] != link || result.LinkHints[link].HLSBitrate != 0 {
			t.Fatalf("Quality %q changed the result: %+v", quality, result)
		}
	}
	if n := atomic.LoadInt32(hits); n != 0 {
		t.Fatalf("best fetched the master %d times", n)
	}
}

func TestApplyQualityFallsBackWhenTheMasterFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer server.Close()
	for _, link := range []string{
		server.URL + "/master.m3u8",
		"http://127.0.0.1:1/unreachable.m3u8",
	} {
		result := qualityTestResult(link)
		applyQualityPreference(&Config{StoragePath: t.TempDir(), Quality: "720"}, &Anime{}, &result)
		if len(result.Links) != 1 || result.Links[0] != link || result.LinkHints[link].Subtitle != "en.vtt" {
			t.Fatalf("a failed fetch changed the result: %+v", result)
		}
	}
}

func TestApplyQualitySkipsLinksThatAreNotHLS(t *testing.T) {
	server, hits := masterServer(t, testMaster)
	result := qualityTestResult(server.URL + "/episode.mp4")
	applyQualityPreference(&Config{StoragePath: t.TempDir(), Quality: "720"}, &Anime{}, &result)
	if n := atomic.LoadInt32(hits); n != 0 {
		t.Fatalf("fetched an mp4 as a playlist %d times", n)
	}
}

func TestQualityForPrefersTheShow(t *testing.T) {
	dir := t.TempDir()
	config := &Config{StoragePath: dir, Quality: "1080"}
	anime := &Anime{AnilistId: 21}
	if got := qualityFor(config, anime); got != 1080 {
		t.Fatalf("without a show pref got %d", got)
	}
	if err := setShowQuality(config, 21, "480"); err != nil {
		t.Fatal(err)
	}
	if got := qualityFor(config, anime); got != 480 {
		t.Fatalf("with a show pref got %d", got)
	}
	// "best" for one show overrides a capped setting.
	if err := setShowQuality(config, 21, "best"); err != nil {
		t.Fatal(err)
	}
	if got := qualityFor(config, anime); got != 0 {
		t.Fatalf("best for the show got %d", got)
	}
	if got := qualityFor(config, &Anime{AnilistId: 99}); got != 1080 {
		t.Fatalf("another show got %d", got)
	}
	// Clearing goes back to the setting and drops the entry.
	if err := setShowQuality(config, 21, ""); err != nil {
		t.Fatal(err)
	}
	if _, kept := loadShowPrefs(dir)["21"]; kept {
		t.Fatal("cleared quality left an entry")
	}
}

func TestApplyStreamPlaybackHintsCarriesTheBitrate(t *testing.T) {
	anime := &Anime{}
	applyStreamPlaybackHints(anime, []string{"m.m3u8"}, map[string]StreamPlaybackHint{"m.m3u8": {HLSBitrate: 42}})
	if anime.Ep.StreamHLSBitrate != 42 {
		t.Fatalf("StreamHLSBitrate = %d", anime.Ep.StreamHLSBitrate)
	}
	applyStreamPlaybackHints(anime, []string{"other.m3u8"}, nil)
	if anime.Ep.StreamHLSBitrate != 0 {
		t.Fatal("the last episode's cap was kept")
	}
}

func TestResetMPVHLSBitrate(t *testing.T) {
	var sent [][]interface{}
	send := func(_ string, cmd []interface{}) (interface{}, error) {
		sent = append(sent, cmd)
		return nil, nil
	}
	resetMPVHLSBitrate(send, "sock", 0)
	resetMPVHLSBitrate(send, "sock", 900000)
	if len(sent) != 2 || sent[0][2] != "max" || sent[1][2] != "900000" {
		t.Fatalf("sent %v", sent)
	}
}
