package movies

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"testing"
	"time"
)

// Run with OTAKASE_LIVE_MOVIES=1 to reach vidsrc itself.
func TestVidsrcLive(t *testing.T) {
	if os.Getenv("OTAKASE_LIVE_MOVIES") == "" {
		t.Skip("set OTAKASE_LIVE_MOVIES=1 to reach vidsrc")
	}
	v := NewVidsrc()
	found, err := v.Search("interstellar")
	if err != nil || len(found) == 0 {
		t.Fatalf("search: %v %v", found, err)
	}
	t.Logf("first: %+v", found[0])
	movie, sources, err := v.Open(found[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %d sources", movie.Label(), len(sources))
	for _, source := range sources {
		stream, err := source.Resolve()
		if err != nil {
			t.Logf("%s: %v", source.Server, err)
			continue
		}
		t.Logf("%s: %.120s subs=%v", stream.Server, stream.URL, stream.Subtitles)
		return
	}
	t.Fatal("nothing resolved")
}

func TestFilmukasLive(t *testing.T) {
	if os.Getenv("OTAKASE_LIVE_MOVIES") == "" {
		t.Skip("set OTAKASE_LIVE_MOVIES=1 to reach filmukas.com")
	}
	f := NewFilmukas()
	found, err := f.Search("lapė")
	if err != nil || len(found) == 0 {
		t.Fatalf("search: %v %v", found, err)
	}
	t.Logf("first: %+v", found[0])
	movie, sources, err := f.Open(found[0])
	if err != nil {
		t.Fatal(err)
	}
	stream, err := sources[0].Resolve()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %.100s", movie.Label(), stream.URL)
}

// Asks imgsto.re for a movie's file the ways a player might, and logs what
// each answer was. OTAKASE_MOVIE_PATH is the 8Filmai page, such as
// /filmas/transporteris-2-online/.
func TestImgstoreProbe(t *testing.T) {
	path := os.Getenv("OTAKASE_MOVIE_PATH")
	if path == "" {
		t.Skip("set OTAKASE_MOVIE_PATH to an 8Filmai movie path")
	}
	site := NewSite(DefaultSiteURL, "", nil)
	moviePage, err := site.Page(path)
	if err != nil {
		t.Fatal(err)
	}
	base, _ := site.Base()
	t.Logf("site %s, servers %q", base, moviePage.Servers)
	if moviePage.Servers[0] == "" {
		t.Fatal("no imgsto.re server for this movie")
	}
	page := "https://imgsto.re/files/" + moviePage.Servers[0]
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 25 * time.Second}
	body, err := fetchPage(client, page, base+"/")
	if err != nil {
		t.Fatal(err)
	}
	link, err := imgstoreLink(body)
	if err != nil {
		t.Fatalf("%v\npage starts: %.600s", err, body)
	}
	t.Logf("link: %s", link)
	pageURL, _ := url.Parse(page)
	t.Logf("cookies from the page: %v", jar.Cookies(pageURL))
	try := func(name string, set func(*http.Request)) {
		req, _ := http.NewRequest(http.MethodGet, link, nil)
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Range", "bytes=0-1023")
		set(req)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Logf("%-22s error %v", name, err)
			return
		}
		resp.Body.Close()
		t.Logf("%-22s %d %s %s", name, resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Content-Length"))
	}
	try("no referrer", func(*http.Request) {})
	try("page referrer", func(r *http.Request) { r.Header.Set("Referer", page) })
	try("page ref + cookies", func(r *http.Request) {
		r.Header.Set("Referer", page)
		for _, c := range jar.Cookies(pageURL) {
			r.AddCookie(c)
		}
	})
	try("page ref + _gad=1", func(r *http.Request) {
		r.Header.Set("Referer", page)
		r.AddCookie(&http.Cookie{Name: "_gad", Value: "1"})
	})
	try("site referrer", func(r *http.Request) { r.Header.Set("Referer", base+"/") })
	try("again, page referrer", func(r *http.Request) { r.Header.Set("Referer", page) })
}
