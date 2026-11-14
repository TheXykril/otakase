package movies

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const dramacoolListing = `<ul class="switch-block list-episode-item">
<li>
    <a href="https://wxx.dramacool.com.tr/drama-detail/queen-of-tears/" class="img" title="Queen of Tears">
        <img src="grey.gif" class="lazy" alt="Queen of Tears (2024)" data-original="https://wxx.dramacool.com.tr/wp-content/uploads/queen.jpg">
        <span class="time">1 month ago</span>
        <h3 class="title" onclick="window.location = 'x'">Queen of Tears (2024)</h3>
    </a>
</li><li>
    <a href="https://wxx.dramacool.com.tr/drama-detail/hospital-playlist-2/" class="img" title="Hospital Playlist 2">
        <img src="grey.gif" class="lazy" alt="" data-original="https://wxx.dramacool.com.tr/hp2.jpg">
        <h3 class="title">Hospital Playlist Season 2 (2021)</h3>
    </a>
</li>
</ul>
<a href="https://wxx.dramacool.com.tr/drama-detail/queen-of-tears/" title="Popular">View more</a>`

func TestParseDramacoolListing(t *testing.T) {
	found := parseDramacoolListing(dramacoolListing)
	if len(found) != 2 {
		t.Fatalf("found %d: %+v", len(found), found)
	}
	first := found[0]
	if first.Path != "/drama-detail/queen-of-tears/" || first.Title != "Queen of Tears" || first.Year != "2024" || first.Poster == "" || first.Provider != DramacoolName {
		t.Fatalf("first = %+v", first)
	}
	if found[1].Title != "Hospital Playlist Season 2" || found[1].Year != "2021" {
		t.Fatalf("second = %+v", found[1])
	}
}

func TestDramacoolSeason(t *testing.T) {
	for title, want := range map[string]struct {
		name   string
		season int
	}{
		"Hospital Playlist Season 2":   {"Hospital Playlist", 2},
		"Hospital Playlist 2nd Season": {"Hospital Playlist", 2},
		"Sweet Home: Season 3":         {"Sweet Home", 3},
		"Queen of Tears":               {"Queen of Tears", 1},
		"Reply 1988":                   {"Reply 1988", 1},
	} {
		name, season := DramacoolSeason(title)
		if name != want.name || season != want.season {
			t.Errorf("%q = %q, %d; want %q, %d", title, name, season, want.name, want.season)
		}
	}
}

const dramacoolDetail = `<div class="details">
<div class="img">
<img src="https://wxx.dramacool.com.tr/poster.jpg" alt="Queen of Tears (2024)">
</div>
<div class="info">
<h1>Queen of Tears (2024)</h1>
<p><span>Episodes:</span> 16</p>
<p><span>Released:</span>
<a href="https://wxx.dramacool.com.tr/released/2024/" title="2024">2024</a>
</p></div></div>
<a href="https://wxx.dramacool.com.tr/queen-of-tears-episode-16-2/">16.2</a>
<a href="https://wxx.dramacool.com.tr/queen-of-tears-episode-16-1/">16.1</a>
<a href="https://wxx.dramacool.com.tr/queen-of-tears-episode-15-2/">15.2</a>
<a href="https://wxx.dramacool.com.tr/queen-of-tears-episode-15-1/">15.1</a>
<a href="https://wxx.dramacool.com.tr/queen-of-tears-episode-16/">16</a>
<a href="https://wxx.dramacool.com.tr/queen-of-tears-episode-2/">2</a>
<a href="https://wxx.dramacool.com.tr/queen-of-tears-episode-1/">1</a>
<a href="https://wxx.dramacool.com.tr/queen-of-tears-spinoff-episode-1/">other</a>`

func TestParseDramacoolPage(t *testing.T) {
	page := parseDramacoolPage("/drama-detail/queen-of-tears/", dramacoolDetail)
	if page.movie.Title != "Queen of Tears" || page.movie.Year != "2024" || page.movie.Poster == "" || !page.movie.Series || page.count != 16 {
		t.Fatalf("page = %+v", page)
	}
	want := map[int]string{
		1:  "/queen-of-tears-episode-1/",
		2:  "/queen-of-tears-episode-2/",
		15: "/queen-of-tears-episode-15-1/",
		16: "/queen-of-tears-episode-16/",
	}
	if len(page.episodes) != len(want) {
		t.Fatalf("episodes = %v", page.episodes)
	}
	for n, path := range want {
		if page.episodes[n] != path {
			t.Errorf("episode %d = %q, want %q", n, page.episodes[n], path)
		}
	}
}

func TestDramacoolResolvesPlayer(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != server.URL+"/kisskh/1" {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/kisskh/1/source":
			w.Write([]byte(`{"status":"ok","source":"/vid/a/b/ep1.m3u8","tracks":[
				{"file":"/sub/en.srt","label":"English","kind":"captions"},
				{"file":"/sub/id.srt","label":"Indonesia","kind":"captions"},
				{"file":"/sub/ar.srt","label":"Arabic","kind":"captions"}]}`))
		case "/sub/en.srt", "/sub/id.srt":
			w.Write([]byte("1\n00:00:01,000 --> 00:00:02,000\nhi\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	d := NewDramacool()
	d.Client = server.Client()
	d.Subtitles = func() []string { return []string{"indonesian", "English"} }
	stream, err := d.resolvePlayer(server.URL+"/kisskh/1", "queen-of-tears-1")
	if err != nil {
		t.Fatal(err)
	}
	if stream.URL != server.URL+"/vid/a/b/ep1.m3u8" || !stream.HLS || stream.Referrer != server.URL+"/kisskh/1" {
		t.Fatalf("stream = %+v", stream)
	}
	if len(stream.Subtitles) != 2 || stream.Subtitles[0].Language != "English" || stream.Subtitles[1].Language != "Indonesia" {
		t.Fatalf("subtitles = %+v", stream.Subtitles)
	}
	for _, sub := range stream.Subtitles {
		text, err := os.ReadFile(sub.URL)
		if err != nil || !strings.Contains(string(text), "hi") {
			t.Fatalf("%s: %q %v", sub.URL, text, err)
		}
	}
}

func TestWantedTrack(t *testing.T) {
	if !wantedTrack("Indonesia", []string{"indonesian"}) || !wantedTrack("English", []string{"english"}) || wantedTrack("Arabic", []string{"english"}) || wantedTrack("Eng", []string{"english"}) {
		t.Fatal("wantedTrack picked wrong")
	}
}

// Run with OTAKASE_LIVE_MOVIES=1 to reach Dramacool.
func TestDramacoolLive(t *testing.T) {
	if os.Getenv("OTAKASE_LIVE_MOVIES") == "" {
		t.Skip("set OTAKASE_LIVE_MOVIES=1 to reach dramacool")
	}
	t.Setenv("TMPDIR", t.TempDir())
	d := NewDramacool()
	found, err := d.Search("queen of tears")
	if err != nil || len(found) == 0 {
		t.Fatalf("search: %v %v", found, err)
	}
	show := found[0]
	for _, m := range found {
		if m.Path == "/drama-detail/queen-of-tears/" {
			show = m
		}
	}
	episodes, err := d.Episodes(show)
	if err != nil || len(episodes) < 16 {
		t.Fatalf("episodes: %d %v", len(episodes), err)
	}
	sources, err := d.OpenEpisode(show, episodes[0])
	if err != nil || len(sources) == 0 {
		t.Fatalf("open: %v %v", sources, err)
	}
	stream, err := sources[0].Resolve()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %.120s subs=%d", stream.Server, stream.URL, len(stream.Subtitles))
	req, _ := http.NewRequest(http.MethodGet, stream.URL, nil)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", stream.Referrer)
	resp, err := d.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(stream.Subtitles) == 0 {
		t.Fatalf("playlist answered %d, %d subtitles", resp.StatusCode, len(stream.Subtitles))
	}
}
