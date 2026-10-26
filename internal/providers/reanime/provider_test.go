package reanime

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/thexykril/otakase/internal/providers"
)

func TestSearchAnimeAndEpisodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/search":
			if r.URL.Query().Get("q") != "My Anime" {
				t.Errorf("search query: %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"results":[{"anime_id":"my-anime-abc","title":{"english":"My Anime","romaji":"Anime"},"episodes":3,"mal_id":456,"subbed":3,"dubbed":2,"cover_image":{"large":"https://img.example/cover.jpg"}},{"anime_id":"my-anime-abc","title":"duplicate"}]}`)
		case "/api/v1/anime/my-anime-abc/episodes":
			fmt.Fprint(w, `{"data":[{"episode_number":3},{"episode_number":1},{"episode_number":2},{"episode_number":2},{"episode_number":0}]}`)
		case "/api/v1/anime/my-anime-abc":
			fmt.Fprint(w, `{"anime_id":"my-anime-abc","subbed":3,"dubbed":2}`)
		default:
			t.Errorf("unexpected request: %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := &Provider{client: &apiClient{base: server.URL, flix: server.URL, http: server.Client()}}
	found, err := p.SearchAnime("My Anime", "dub")
	if err != nil || len(found) != 1 {
		t.Fatalf("SearchAnime: %v, %+v", err, found)
	}
	if found[0].Key != "my-anime-abc" || found[0].Title != "My Anime" || found[0].Thumbnail == "" {
		t.Fatalf("wrong match: %+v", found[0])
	}
	hit, ok := found[0].ExtraData.(providers.SearchHit)
	if !ok || hit.MalID != 456 || hit.Episodes != 3 {
		t.Fatalf("missing matching metadata: %+v", found[0].ExtraData)
	}
	for _, item := range []struct {
		mode string
		want []string
	}{
		{"sub", []string{"1", "2", "3"}},
		{"dub", []string{"1", "2"}},
	} {
		got, err := p.EpisodesList("my-anime-abc", item.mode)
		if err != nil || !reflect.DeepEqual(got, item.want) {
			t.Errorf("EpisodesList(%s) = %v, %v, want %v", item.mode, got, err, item.want)
		}
	}
}

func TestSearchFallsBackOnlyForMissingNewEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"older-slug","title":"Older Anime"}]}`)
	}))
	defer server.Close()
	p := &Provider{client: &apiClient{base: server.URL, http: server.Client()}}
	matches, err := p.SearchAnime("older", "sub")
	if err != nil || len(matches) != 1 || matches[0].Key != "older-slug" {
		t.Fatalf("legacy API fallback = %+v, %v", matches, err)
	}
}

func TestServerModeFilteringAndFallbackOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/watch/test-slug/2" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"episode_links":[
			{"serverName":"HD-1","dataType":"sub","dataLink":"https://flixcloud.cc/e/sub1?v=1"},
			{"serverName":"HD-2","dataType":"sub","dataLink":"https://flixcloud.cc/e/sub2?v=2"},
			{"serverName":"HD-2","dataType":"sub","dataLink":"https://flixcloud.cc/e/sub2?v=2"},
			{"serverName":"HD-1","dataType":"dub","dataLink":"https://flixcloud.cc/e/dub1?v=1"},
			{"serverName":"HD-2","dataType":"dub","dataLink":"http://127.0.0.1/private"},
			{"serverName":"bad","dataType":"sub","dataLink":"https://evil.example/e/fake"}
		]}`)
	}))
	defer server.Close()
	client := &apiClient{base: server.URL, http: server.Client()}
	sub, err := client.servers("test-slug", 2, "sub")
	if err != nil || len(sub) != 2 || sub[0].Name != "HD-2" || sub[1].Name != "HD-1" {
		t.Fatalf("sub servers: %+v, %v", sub, err)
	}
	dub, err := client.servers("test-slug", 2, "dub")
	if err != nil || len(dub) != 1 || !strings.Contains(dub[0].Link, "dub1") {
		t.Fatalf("dub servers: %+v, %v", dub, err)
	}
	if _, err := client.servers("../../private", 2, "sub"); err == nil {
		t.Fatal("unsafe show id accepted")
	}
}

func TestSSRFieldsAndTracks(t *testing.T) {
	seed := strings.Repeat("a", 40)
	first := seed
	for i := 0; i < 3; i++ {
		first = shaHex(first + strconv.Itoa(i))
	}
	script := fmt.Sprintf(`<script>window.X={type:"data",data:{obfuscation_seed:"%s",%s:"s3cr3t",subtitles:[{url:"https://cdn.example/english.vtt",language:"en",label:"English",default:!0},{url:"https://cdn.example/japanese.vtt",language:"ja",label:"Japanese"}],intro_chapter:{start:83,end:170},quoted:"brace } inside text"}};</script>`, seed, first[48:64]+"_"+first[56:64])
	ssr := extractSSR([]byte(script))
	if ssr == "" || jsStringField(ssr, "obfuscation_seed") != seed || jsStringField(ssr, first[48:64]+"_"+first[56:64]) != "s3cr3t" {
		t.Fatal("did not recover SSR player fields")
	}
	tracks, preferred := parseSubtitles(ssr)
	if len(tracks) != 2 || tracks[1].Language != "ja" || !strings.Contains(preferred, "english") {
		t.Fatalf("subtitle tracks = %+v, preferred %q", tracks, preferred)
	}
	if got := chapterRange(parseChapter(ssr, "intro_chapter")); !reflect.DeepEqual(got, []int{83, 170}) {
		t.Fatalf("chapter = %v", got)
	}
	if got := extractSSR([]byte(`{type:"data",data:{bad:"unfinished"`)); got != "" {
		t.Fatalf("accepted unfinished SSR: %q", got)
	}
}

func TestAESDecryptionAndValidation(t *testing.T) {
	key := bytes.Repeat([]byte{9}, 32)
	iv := bytes.Repeat([]byte{7}, aes.BlockSize)
	plain := []byte("https://fetch1.flixcloud.cc/_v7/test/master.m3u8")
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(bytes.Clone(plain), bytes.Repeat([]byte{byte(pad)}, pad)...)
	block, _ := aes.NewCipher(key)
	encrypted := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(encrypted, padded)
	got, err := decryptCBC(key, iv, encrypted)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("decryptCBC = %q, %v", got, err)
	}
	encrypted[len(encrypted)-1] ^= 0xff
	if _, err := decryptCBC(key, iv, encrypted); err == nil {
		t.Fatal("accepted corrupted padding")
	}
	for _, host := range []struct {
		host string
		want bool
	}{
		{"fetch1.flixcloud.cc", true},
		{"flixcloud.cc", true},
		{"flixcloud.cc.evil.test", false},
		{"localhost", false},
		{"127.0.0.1", false},
	} {
		if flixHost(host.host) != host.want {
			t.Errorf("flixHost(%q) = %v", host.host, !host.want)
		}
	}
	if _, err := base64.StdEncoding.DecodeString("!!"); err == nil {
		t.Fatal("expected invalid fixture to fail base64 decode")
	}
}

func TestSkipRangeCache(t *testing.T) {
	p := &Provider{}
	p.skips = map[string]skipTimes{skipKey("show", 3, "sub"): {intro: []int{80, 170}, outro: []int{1320, 1390}}}
	intro, outro, err := p.SkipRange("show", "sub", 3)
	if err != nil || !reflect.DeepEqual(intro, []int{80, 170}) || !reflect.DeepEqual(outro, []int{1320, 1390}) {
		t.Fatalf("cached skip ranges: %v %v %v", intro, outro, err)
	}
	intro, outro, _ = p.SkipRange("show", "dub", 3)
	if len(intro) != 0 || len(outro) != 0 {
		t.Fatal("reused another audio release's skip timings")
	}
}

func TestSearchJSONShapes(t *testing.T) {
	for _, raw := range []string{`"series title"`, `{"romaji":"Series","native":"日本語"}`} {
		var show show
		if err := json.Unmarshal([]byte(fmt.Sprintf(`{"title":%s}`, raw)), &show); err != nil || show.title() == "" {
			t.Fatalf("title shape %s: %v", raw, err)
		}
	}
}

func TestSkipRangeCacheBounded(t *testing.T) {
	p := &Provider{}
	for i := 1; i <= maxSkipEntries+1; i++ {
		p.rememberSkip(skipKey("series", i, "sub"), skipTimes{intro: []int{i, i + 10}})
	}
	if len(p.skips) != maxSkipEntries || len(p.skipOrder) != maxSkipEntries {
		t.Fatalf("skip cache grew past %d entries: %d", maxSkipEntries, len(p.skips))
	}
	if _, present := p.skips[skipKey("series", 1, "sub")]; present {
		t.Fatal("oldest skip entry was not removed")
	}
	p.rememberSkip(skipKey("series", maxSkipEntries+1, "sub"), skipTimes{outro: []int{900, 940}})
	if len(p.skips) != maxSkipEntries {
		t.Fatal("updating an entry duplicated the cache key")
	}
}

func TestServersUseFlixWhenWatchIsGone(t *testing.T) {
	var detailRequests, watchRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/anime/my-show":
			detailRequests.Add(1)
			fmt.Fprint(w, `{"anime_id":"my-show","anilist_id":12345}`)
		case "/api/flix/12345/2":
			fmt.Fprint(w, `{"success":true,"servers":[
				{"serverName":"HD-1","dataType":"sub","dataLink":"https://flixcloud.cc/e/shared?v=2"},
				{"serverName":"HD-2","dataType":"dub","dataLink":"https://flixcloud.cc/e/shared?v=2"},
				{"serverName":"HD-2","dataType":"sub","dataLink":"https://flixcloud.cc/e/sub-only?v=2"}
			]}`)
		case "/api/watch/my-show/2":
			watchRequests.Add(1)
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &apiClient{base: server.URL, http: server.Client()}
	dubs, err := client.servers("my-show", 2, "dub")
	if err != nil || len(dubs) != 1 || !dubs[0].SharedWithSub {
		t.Fatalf("dub servers with watch 404 = %+v, %v", dubs, err)
	}
	subs, err := client.servers("my-show", 2, "sub")
	if err != nil || len(subs) != 2 || subs[0].Name != "HD-2" {
		t.Fatalf("sub servers = %+v, %v", subs, err)
	}
	if detailRequests.Load() != 1 || watchRequests.Load() != 0 {
		t.Fatalf("unexpected requests: detail=%d watch=%d", detailRequests.Load(), watchRequests.Load())
	}
}

func TestServersWatch404IsOptionalFallback(t *testing.T) {
	var watchRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/anime/my-show":
			fmt.Fprint(w, `{"anilist_id":12345}`)
		case "/api/flix/12345/2":
			fmt.Fprint(w, `{"success":true,"servers":[{"dataType":"sub","serverName":"HD-2","dataLink":"https://flixcloud.cc/e/sub?v=2"}]}`)
		case "/api/watch/my-show/2":
			watchRequests.Add(1)
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &apiClient{base: server.URL, http: server.Client()}
	_, err := client.servers("my-show", 2, "dub")
	if err == nil || !strings.Contains(err.Error(), "no dub server") {
		t.Fatalf("missing dub returned %v, want no dub server rather than watch HTTP 404", err)
	}
	if watchRequests.Load() != 1 {
		t.Fatalf("optional watch fallback requests = %d, want 1", watchRequests.Load())
	}
}

func TestServersReuseAniListIDFromSearch(t *testing.T) {
	var detailRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/search":
			fmt.Fprint(w, `{"results":[{"anime_id":"cached-show","anilist_id":9876,"title":"Cached Show"}]}`)
		case "/api/v1/anime/cached-show":
			detailRequests.Add(1)
			http.NotFound(w, r)
		case "/api/flix/9876/1":
			fmt.Fprint(w, `{"success":true,"servers":[{"serverName":"HD-2","dataType":"sub","dataLink":"https://flixcloud.cc/e/cached?v=2"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &apiClient{base: server.URL, http: server.Client()}
	if _, err := client.search("cached"); err != nil {
		t.Fatal(err)
	}
	found, err := client.servers("cached-show", 1, "sub")
	if err != nil || len(found) != 1 || detailRequests.Load() != 0 {
		t.Fatalf("cached AniList id: %+v, %v; detail requests=%d", found, err, detailRequests.Load())
	}
}

func TestPlaylistHasEnglishAudio(t *testing.T) {
	tests := []struct {
		name, playlist string
		want           bool
	}{
		{"English audio", `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",LANGUAGE="en",NAME="English",URI="eng.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",LANGUAGE="ja",NAME="Japanese",URI="jpn.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=2500000,CODECS="avc1.4d401f,mp4a.40.2",AUDIO="audio"
video.m3u8`, true},
		{"Japanese audio", `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",LANGUAGE="ja",NAME="Japanese",URI="jpn.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=1000000,AUDIO="audio"
video.m3u8`, false},
		{"English audio unused", `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="unused",LANGUAGE="en",NAME="English",URI="eng.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=1000000,AUDIO="main"
video.m3u8`, false},
		{"Only English subtitles", `#EXTM3U
#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",LANGUAGE="en",NAME="English",URI="eng.vtt"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",LANGUAGE="ja",NAME="Japanese",URI="ja.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=1000000,AUDIO="audio",SUBTITLES="subs"
video.m3u8`, false},
		{"No audio metadata", `#EXTM3U
#EXT-X-TARGETDURATION:8
#EXTINF:8,
segment.ts`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := playlistHasEnglishAudio([]byte(tc.playlist)); got != tc.want {
				t.Fatalf("playlistHasEnglishAudio = %v, want %v", got, tc.want)
			}
		})
	}
}

type rewriteTransport func(*http.Request) (*http.Response, error)

func (fn rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestSharedDubRequiresEnglishHLSAudio(t *testing.T) {
	for _, tc := range []struct {
		name, language string
		wantDub        bool
	}{
		{"English available", "en", true},
		{"Only Japanese", "ja", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/anime/my-show":
					fmt.Fprint(w, `{"anilist_id":12345}`)
				case "/api/flix/12345/1":
					fmt.Fprint(w, `{"success":true,"servers":[
						{"dataType":"sub","serverName":"HD-2","dataLink":"https://flixcloud.cc/e/shared?v=2"},
						{"dataType":"dub","serverName":"HD-2","dataLink":"https://flixcloud.cc/e/shared?v=2"}
					]}`)
				case "/master.m3u8":
					fmt.Fprintf(w, "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",LANGUAGE=%q,NAME=\"Audio\",URI=\"a.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=1000000,AUDIO=\"audio\"\nvideo.m3u8\n", tc.language)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client := &http.Client{Transport: rewriteTransport(func(req *http.Request) (*http.Response, error) {
				if req.URL.Hostname() != "cdn.flixcloud.cc" {
					return http.DefaultTransport.RoundTrip(req)
				}
				forward := req.Clone(req.Context())
				forward.URL.Scheme = "http"
				forward.URL.Host = strings.TrimPrefix(server.URL, "http://")
				forward.Host = forward.URL.Host
				return http.DefaultTransport.RoundTrip(forward)
			})}
			p := &Provider{client: &apiClient{
				base: server.URL,
				http: client,
				resolveEmbed: func(string) (resolved, error) {
					return resolved{URL: "https://cdn.flixcloud.cc/master.m3u8"}, nil
				},
			}}
			links, _, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "my-show", 1, "dub")
			if tc.wantDub && (err != nil || len(links) != 1) {
				t.Fatalf("English dub not usable: %v, %v", links, err)
			}
			if !tc.wantDub && (err == nil || len(links) != 0) {
				t.Fatalf("unverified dub accepted: %v, %v", links, err)
			}
		})
	}
}

func TestDecryptPlayerSeedLength(t *testing.T) {
	for _, tc := range []struct {
		name string
		seed string
		want string
	}{
		{
			name: "sixteen-character seed",
			seed: "0123456789abcdef",
			// Without the rest of the crypto fields, a valid seed must
			// reach the next validation rather than being rejected.
			want: "reanime: invalid player crypto fields",
		},
		{
			name: "short seed",
			seed: "1234567",
			want: "reanime: invalid player seed",
		},
		{
			name: "nonhexadecimal prefix",
			seed: "not-hex-seed123",
			want: "reanime: invalid player seed",
		},
		{
			name: "missing seed",
			seed: "",
			want: "reanime: missing player seed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := fmt.Sprintf(`<script>window.X={type:"data",data:{obfuscation_seed:%q}};</script>`, tc.seed)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/e/episode" {
					t.Errorf("unexpected request: %s", r.URL)
					http.NotFound(w, r)
					return
				}
				fmt.Fprint(w, page)
			}))
			defer server.Close()
			client := &apiClient{http: &http.Client{Transport: rewriteTransport(func(req *http.Request) (*http.Response, error) {
				forward := req.Clone(req.Context())
				forward.URL.Scheme = "http"
				forward.URL.Host = strings.TrimPrefix(server.URL, "http://")
				forward.Host = forward.URL.Host
				return http.DefaultTransport.RoundTrip(forward)
			})}}
			_, err := client.decrypt("https://flixcloud.cc/e/episode?v=2")
			if err == nil || err.Error() != tc.want {
				t.Fatalf("decrypt error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestDecodeStreamTokensWithMetadata(t *testing.T) {
	token := "player-token"
	video := bytes.Repeat([]byte{5}, aes.BlockSize)
	key := bytes.Repeat([]byte{8}, 32)
	fields := map[string]any{
		shaHex(token + "vid")[:10]: base64.StdEncoding.EncodeToString(video),
		shaHex(token + "key")[:10]: base64.StdEncoding.EncodeToString(key),
		"metadata": map[string]any{
			"server": "HD-2",
			"audio":  []string{"ja", "en"},
			"valid":  true,
		},
		"status": 200,
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	gotVideo, gotKey, err := decodeStreamTokens(payload, token)
	if err != nil {
		t.Fatalf("stream tokens with nested metadata: %v", err)
	}
	if !bytes.Equal(gotVideo, video) || !bytes.Equal(gotKey, key) {
		t.Fatalf("decoded token bytes differ from expected")
	}
}

func TestDecodeStreamTokensRejectsBadFields(t *testing.T) {
	token := "player-token"
	vid := shaHex(token + "vid")[:10]
	key := shaHex(token + "key")[:10]
	valid := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	for _, tc := range []struct {
		name   string
		fields map[string]any
	}{
		{"missing video", map[string]any{key: valid}},
		{"missing key", map[string]any{vid: valid}},
		{"object instead of video string", map[string]any{vid: map[string]any{"value": valid}, key: valid}},
		{"object instead of key string", map[string]any{vid: valid, key: map[string]any{"value": valid}}},
		{"invalid video base64", map[string]any{vid: "!!!", key: valid}},
		{"invalid key base64", map[string]any{vid: valid, key: "!!!"}},
		{"empty video", map[string]any{vid: "", key: valid}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(tc.fields)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := decodeStreamTokens(payload, token); err == nil {
				t.Fatal("accepted malformed stream token fields")
			}
		})
	}
	if _, _, err := decodeStreamTokens([]byte("not-json"), token); err == nil {
		t.Fatal("accepted invalid stream token JSON")
	}
}

func TestFlixCloudPlaybackRequiresBrowserUserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/anime/my-show":
			fmt.Fprint(w, `{"anilist_id":12345}`)
		case "/api/flix/12345/1":
			fmt.Fprint(w, `{"success":true,"servers":[
				{"serverName":"HD-2","dataType":"sub","dataLink":"https://flixcloud.cc/e/sub?v=2"},
				{"serverName":"HD-2","dataType":"dub","dataLink":"https://flixcloud.cc/e/dub?v=2"}
			]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	const playlist = "https://cdn.flixcloud.cc/master.m3u8"
	p := &Provider{client: &apiClient{
		base: server.URL,
		http: server.Client(),
		resolveEmbed: func(string) (resolved, error) {
			return resolved{URL: playlist}, nil
		},
	}}
	for _, mode := range []string{"sub", "dub"} {
		t.Run(mode, func(t *testing.T) {
			links, hints, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "my-show", 1, mode)
			if err != nil || len(links) != 1 || links[0] != playlist {
				t.Fatalf("%s stream = %v, %v", mode, links, err)
			}
			hint, ok := hints[playlist]
			if !ok || !hint.HLS || hint.Referrer != flixURL+"/" {
				t.Fatalf("%s stream hint = %+v", mode, hint)
			}
			if hint.Headers["User-Agent"] != userAgent {
				t.Fatalf("%s playback browser User-Agent = %q, want %q", mode, hint.Headers["User-Agent"], userAgent)
			}
			if hint.Headers["Origin"] != "" {
				t.Fatalf("%s requested unnecessary Origin header", mode)
			}
		})
	}
}

func TestSharedDubEncryptedHLSAudio(t *testing.T) {
	key := bytes.Repeat([]byte{42}, 32)
	master := "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",LANGUAGE=\"en\",NAME=\"English\",URI=\"en.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=1000000,AUDIO=\"audio\"\nvideo.m3u8\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/anime/test":
			fmt.Fprint(w, `{"anilist_id":123}`)
		case "/api/flix/123/1":
			fmt.Fprint(w, `{"success":true,"servers":[{"serverName":"HD-2","dataType":"sub","dataLink":"https://flixcloud.cc/e/one?v=2"},{"serverName":"HD-2","dataType":"dub","dataLink":"https://flixcloud.cc/e/one?v=2"}]}`)
		case "/master.m3u8":
			if r.Header.Get("User-Agent") == "" || r.Header.Get("Referer") != flixURL+"/" {
				t.Errorf("missing CDN headers: %v", r.Header)
			}
			_, _ = w.Write(encodedManifest(master, key))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &http.Client{Transport: rewriteTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() != "fetch1.flixcloud.cc" {
			return http.DefaultTransport.RoundTrip(req)
		}
		forward := req.Clone(req.Context())
		forward.URL.Scheme = "http"
		forward.URL.Host = strings.TrimPrefix(server.URL, "http://")
		forward.Host = forward.URL.Host
		return http.DefaultTransport.RoundTrip(forward)
	})}
	p := &Provider{client: &apiClient{
		base: server.URL,
		http: client,
		resolveEmbed: func(string) (resolved, error) {
			return resolved{URL: "https://fetch1.flixcloud.cc/master.m3u8", ManifestKey: base64.StdEncoding.EncodeToString(key)}, nil
		},
	}}
	links, hints, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{}, "test", 1, "dub")
	if err != nil || len(links) != 1 {
		t.Fatalf("encrypted English HLS = %v, %v", links, err)
	}
	if got := hints[links[0]].FlixCloudKey; got != base64.StdEncoding.EncodeToString(key) {
		t.Fatalf("manifest key was not forwarded")
	}
}
