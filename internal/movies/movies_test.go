package movies

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Cut from the site's own home page.
const listingHTML = `<div class="items">
<article id="post-featured-18344" class="item movies"><div class="poster br2" style="padding-top: 138%;"><img data-lazyloaded="1" src="data:image/gif;base64,R0lGOD" class="br3 h100" data-src="https://image.tmdb.org/t/p/w185/rFhKkXhk7ClU03jQ5rHIApJDwev.jpg" alt="Karo Mašina online"><a href="https://176.97.124.32/filmas/karo-masina-online/" data-wpel-link="internal"><div class="pname br2b">Karo Mašina</div><div class="see"></div></a></div></article>
<article id="post-1" class="item tvshows"><a href="https://176.97.124.32/serialai/kaulai-11-sezonas-online/"><div class="pname">Kaulai</div></a></article>
<article id="post-2" class="item movies"><a href="https://176.97.124.32/filmas/karo-masina-online/"><div class="pname">Karo Mašina</div></a></article>
<article><div class="image"><a href="https://176.97.124.32/filmas/alkis-online-3/"><img src="https://image.tmdb.org/t/p/w92/x.jpg" alt="Alkis online"></a></div><div class="details"><div class="title"><a href="https://176.97.124.32/filmas/alkis-online-3/">Alkis</a></div><span class="year">2026</span></div></article>
</div>`

// Cut from the site's page for the movie Alkis.
const pageHTML = `<link rel="canonical" href="https://176.97.124.32/filmas/alkis-online-3/">
<div class="sheader"><div class="poster psize"> <img data-lazyloaded="1" src="./x.jpg" data-src="https://image.tmdb.org/t/p/w185/4uNhvGxBDiO0mU5UhxuXBsJwKF3.jpg" alt="Filmas Hungry / Alkis online"></div><div class="data"><h1 class="h1e">Alkis</h1><div style="padding: 10px 0 5px 0px;font-size:15px;">( Hungry )</div><div class="extra"> <span class="date">2026&nbsp;</span><span class="imdb2">4.9</span> <span class="runtime">93 min&nbsp;</span><span class="pt10">Lietuvių k.</span></div></div></div>
<script>var imid='tt34494076';var yt='5zwBu4XdK3U';var data='t/01/34494076||qubt61nlwkxx|jvVlA0BJdqczRGx|';var _0x55291f=_0x253f;</script>`

func TestParseListingKeepsMoviesOnce(t *testing.T) {
	got := parseListing(listingHTML)
	if len(got) != 2 {
		t.Fatalf("want 2 movies (series and the repeat left out), got %d: %+v", len(got), got)
	}
	if got[0].Path != "/filmas/karo-masina-online/" || got[0].Title != "Karo Mašina" {
		t.Errorf("first movie = %+v", got[0])
	}
	if got[0].Poster != "https://image.tmdb.org/t/p/w185/rFhKkXhk7ClU03jQ5rHIApJDwev.jpg" {
		t.Errorf("poster = %q", got[0].Poster)
	}
	if got[1].Title != "Alkis" || got[1].Year != "2026" {
		t.Errorf("search-result layout read as %+v", got[1])
	}
}

func TestParsePage(t *testing.T) {
	page, err := parsePage(pageHTML)
	if err != nil {
		t.Fatal(err)
	}
	want := Movie{Path: "/filmas/alkis-online-3/", Title: "Alkis", Original: "Hungry", Year: "2026",
		Poster: "https://image.tmdb.org/t/p/w185/4uNhvGxBDiO0mU5UhxuXBsJwKF3.jpg", IMDb: "tt34494076"}
	if page.Movie != want {
		t.Errorf("movie = %+v\nwant   %+v", page.Movie, want)
	}
	if strings.Join(page.Servers, ",") != "t/01/34494076,,qubt61nlwkxx,jvVlA0BJdqczRGx" {
		t.Errorf("servers = %q", page.Servers)
	}
	if page.Label() != "Alkis / Hungry (2026)" {
		t.Errorf("label = %q", page.Label())
	}
}

func TestParsePageRejectsSeries(t *testing.T) {
	series := strings.Replace(pageHTML, "var data='t/01/34494076||qubt61nlwkxx|jvVlA0BJdqczRGx|'",
		"var data='1=2-a/1/31510819||zlimpqfjjx79|6p0wbRqd1WI93l2|lt;'", 1)
	if _, err := parsePage(series); err == nil {
		t.Fatal("a series page was read as a movie")
	}
}

func TestSourcesOrder(t *testing.T) {
	site := NewSite("https://example.invalid", "https://127.0.0.1", nil)
	page, _ := parsePage(pageHTML)
	var names []string
	for _, source := range site.Sources(page) {
		names = append(names, source.Server)
	}
	if strings.Join(names, ",") != "server 3,server 2,imgsto.re" {
		t.Errorf("sources = %v", names)
	}
}

// A Streamtape embed page has several lines writing a link, most of them
// decoys; the robotlink one is assembled from two pieces.
func TestStreamtapeLink(t *testing.T) {
	body := `<div id="ideoolink" style="display:none;">/streamtape.com/get_video?id=jvVlA0BJdqczRGx&expires=1&ip=x&token=bad</div>
<script>document.getElementById('ideoolink').innerHTML = "/streamtape.com/get_video?id=jvVlA0BJdqczRGx&expires=1&ip=x&token=" + ''+ ('xcdbadtoken').substring(1);
document.getElementById('robotlink').innerHTML = '//streamtape.com/get_video?id=jvVlA0BJdqczRGx&expires=1759600000&ip=FRuWKRSOKxSHDN&token=Ab' + ('xcdcD-goodtoken').substring(2).substring(1);</script>`
	got, err := streamtapeLink(body)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://streamtape.com/get_video?id=jvVlA0BJdqczRGx&expires=1759600000&ip=FRuWKRSOKxSHDN&token=AbcD-goodtoken&stream=1"
	if got != want {
		t.Errorf("link = %q\nwant   %q", got, want)
	}
}

func TestDoodLink(t *testing.T) {
	got := doodLink("https://xyz.cloudatacdn.com/u5kj/abc~", "/pass_md5/123-45-678-1759600000-deadbeef/qubt61nlwkxx", time.UnixMilli(1759600000123))
	if !strings.HasPrefix(got, "https://xyz.cloudatacdn.com/u5kj/abc~") || !strings.HasSuffix(got, "?token=qubt61nlwkxx&expiry=1759600000123") {
		t.Errorf("link = %q", got)
	}
	if len(got) != len("https://xyz.cloudatacdn.com/u5kj/abc~")+10+len("?token=qubt61nlwkxx&expiry=1759600000123") {
		t.Errorf("random part is not 10 characters: %q", got)
	}
}

// The entry domain redirects to wherever the site is; a search follows it,
// remembers the address, and asks again when that address stops answering.
func TestSiteFollowsTheEntryRedirect(t *testing.T) {
	var current *httptest.Server
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("s") != "alkis" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(listingHTML))
	}))
	defer origin.Close()
	current = origin
	entry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, current.URL+r.URL.Path, http.StatusMovedPermanently)
	}))
	defer entry.Close()

	var remembered string
	// Start from an address that no longer answers.
	site := NewSite(entry.URL, "http://127.0.0.1:1", func(address string) { remembered = address })
	got, err := site.Search("alkis")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d movies", len(got))
	}
	if remembered != origin.URL {
		t.Errorf("remembered %q, want %q", remembered, origin.URL)
	}
}

func TestStoreProgress(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	movie := Movie{Path: "/filmas/alkis-online-3/", Title: "Alkis"}
	if err := store.SetWatchlist(movie, true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProgress(movie, 600, 5580, false); err != nil {
		t.Fatal(err)
	}
	if got := store.Continue(); len(got) != 1 || got[0].Position != 600 || got[0].Title != "Alkis" {
		t.Fatalf("continue = %+v", got)
	}
	if err := store.SetProgress(Movie{Path: movie.Path}, 5500, 5580, true); err != nil {
		t.Fatal(err)
	}
	entry, _ := store.Get(movie.Path)
	if !entry.Watched || entry.Position != 0 || entry.Watchlist || entry.Title != "Alkis" {
		t.Errorf("after finishing: %+v", entry)
	}
	if len(store.Continue()) != 0 || len(store.History()) != 1 {
		t.Errorf("continue %d, history %d", len(store.Continue()), len(store.History()))
	}

	reopened, err := OpenStore(storeDir(store))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reopened.Get(movie.Path); !ok || !got.Watched {
		t.Errorf("not kept on disk: %+v", got)
	}
}

func storeDir(store *Store) string {
	return filepath.Dir(store.path)
}
