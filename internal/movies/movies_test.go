package movies

import (
	"fmt"
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
	// The script lines as streamtape.com served them for a real movie: the
	// link is split mid-word, one line has an empty string in the middle,
	// and only robotlink's assembles to the right token.
	body := `<script>document.getElementById('ideoolink').innerHTML = "/streamtape.com/get_vi" + ''+ ('xcdbdeo?id=jvVlA0BJdqczRGx&expires=1791200717&ip=F0MOKREAKxSHDN&token=sHWHjT4mBGvQ').substring(1).substring(2);
document.getElementById('botlink').innerHTML = '//streamtape.com/get_v'+ ('xyzaideo?id=jvVlA0BJdqczRGx&expires=1791200717&ip=F0MOKREAKxSHDN&token=sHWHjT4mBGvX').substring(4);
document.getElementById('robotlink').innerHTML = '//streamtape.com/get_v'+ ('xcdideo?id=jvVlA0BJdqczRGx&expires=1791200717&ip=F0MOKREAKxSHDN&token=sHWHjT4mBGvQ').substring(2).substring(1);</script>`
	got, err := streamtapeLink(body)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://streamtape.com/get_video?id=jvVlA0BJdqczRGx&expires=1791200717&ip=F0MOKREAKxSHDN&token=sHWHjT4mBGvQ&stream=1"
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
	entry, _ := store.Get(movie.Key())
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
	if got, ok := reopened.Get(movie.Key()); !ok || !got.Watched {
		t.Errorf("not kept on disk: %+v", got)
	}
}

func storeDir(store *Store) string {
	return filepath.Dir(store.path)
}

// The call as a Doodstream embed page made it, fetched from a home
// connection: the path ends in the token the link needs.
func TestDoodPassPath(t *testing.T) {
	body := `dsplayer.addClass("vjs-waiting"); $.get('/pass_md5/281281071-158-129-1791131115-9053d321a814cdbbc198d82c916fb47b/16e9jd3d6cmp6c6v5a9qohic', function(data) { if (data === "RELOAD"){location.reload();}`
	pass := doodPassPattern.FindString(body)
	if pass != "/pass_md5/281281071-158-129-1791131115-9053d321a814cdbbc198d82c916fb47b/16e9jd3d6cmp6c6v5a9qohic" {
		t.Fatalf("pass path = %q", pass)
	}
	if got := doodLink("https://a.cdn/x~", pass, time.UnixMilli(1)); !strings.HasSuffix(got, "?token=16e9jd3d6cmp6c6v5a9qohic&expiry=1") {
		t.Errorf("link = %q", got)
	}
}

// Cut from an imgsto.re player page fetched from a home connection.
func TestImgstoreLink(t *testing.T) {
	body := `function _0x40b1(){var _0x5bfc1f=['553dPhsbd','953418DmLPex','2290ZPZZKj','aHR0cHMlM0ElMkYlMkZpbWdzdG8ucmUlMkZhcGklMkYxJTJGdCUyRjAxJTJGTGtYV2RuT21DdkNrQTFXdWd6QTRnUSUyRjM0NDk0MDc2JTJGMTc5MTE0OTY5Mi5tcDQ=','8814kmeeTP'];`
	got, err := imgstoreLink(body)
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://imgsto.re/api/1/t/01/LkXWdnOmCvCkA1WugzA4gQ/34494076/1791149692.mp4"; got != want {
		t.Errorf("link = %q\nwant   %q", got, want)
	}
}

func TestStoreWatchedAndRating(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	movie := Movie{Path: "/filmas/a/", Title: "A"}
	store.SetWatchlist(movie, true)
	store.SetProgress(movie, 300, 5000, false)
	if err := store.SetWatched(movie, true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRating(movie, 12); err != nil {
		t.Fatal(err)
	}
	entry, _ := store.Get(movie.Key())
	if !entry.Watched || entry.Position != 0 || entry.Watchlist || entry.Rating != 10 {
		t.Errorf("entry = %+v", entry)
	}
	store.SetWatched(movie, false)
	if entry, _ := store.Get(movie.Key()); entry.Watched {
		t.Errorf("still watched: %+v", entry)
	}
}

func TestDropAdvertCues(t *testing.T) {
	srt := "1\r\n00:00:06,000 --> 00:00:12,074\r\nWatch Online Movies and Series for FREE\r\nwww.osdb.link/lm\r\n\r\n2\r\n00:01:14,908 --> 00:01:17,094\r\nWell, my dad was a farmer.\r\n"
	got := dropAdvertCues(srt)
	if strings.Contains(got, "osdb") || !strings.Contains(got, "farmer") {
		t.Errorf("got %q", got)
	}
}

// Cut from filmukas.com's search results.
func TestFilmukasListing(t *testing.T) {
	body := `<li id="li_3">
		<a href="/pilnametraziai/lape-ir-suo-703"><i class="far fa-play-circle play_button"></i>
		<div class="sound_bars_wrap"><span class="sound_bar" title="Garso takelis: LT"><i class="fa fa-headphones"></i> LT</span></div>
			<img class="leading2 lazyload_" data-src="/inc/i02/media/u/1779455995838.jpg" src="/media/elem/default-image.png" alt="Lapė ir šuo" title="Lapė ir šuo" />
			<span>Lapė ir šuo </span>
		</a>
		</li>
		<li id="li_1">
		<a href="/pilnametraziai/didele-bloga-lape-ir-kitos-istorijos-the-big-bad-fox-and-other-tales-2017-649"><img data-src="/inc/x.jpg" alt="Didelė bloga lapė" /></a>
		</li>
		<li id="li_2"><a href="/serialai/kas-nors-12"><img data-src="/inc/y.jpg" alt="Serialas" /></a></li>
		<li id="li_4"><a href="/pilnametraziai/wonka-736"><img data-src="/inc/z.jpg" alt="Vonka" /><span>Vonka <i class="fas fa-lock thumb_lock"></i></span></a></li>`
	got := NewFilmukas().parseListing(body)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Title != "Lapė ir šuo" || got[0].Poster != "https://www.filmukas.com/inc/i02/media/u/1779455995838.jpg" || got[0].Key() != "filmukas:/pilnametraziai/lape-ir-suo-703" {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Year != "2017" {
		t.Errorf("year = %q", got[1].Year)
	}
}

// Trakt hears about a movie by its IMDb id, through the sync endpoints, and
// signs in with the device flow.
func TestTraktSync(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 512)
		n, _ := r.Body.Read(body)
		calls = append(calls, r.URL.Path+" "+r.Header.Get("Authorization")+" "+string(body[:n]))
		switch r.URL.Path {
		case "/oauth/device/code":
			w.Write([]byte(`{"device_code":"dev","user_code":"ABC123","verification_url":"https://trakt.tv/activate","expires_in":600,"interval":0}`))
		case "/oauth/device/token":
			w.Write([]byte(`{"access_token":"tok","refresh_token":"ref","expires_in":86400,"created_at":` + fmt.Sprint(time.Now().Unix()) + `}`))
		default:
			w.WriteHeader(http.StatusCreated)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	trakt := NewTrakt("id", "secret", dir)
	trakt.API = server.URL
	if trakt.SignedIn() {
		t.Fatal("signed in before signing in")
	}
	code, err := trakt.StartSignIn()
	if err != nil || code.UserCode != "ABC123" {
		t.Fatalf("code %+v, %v", code, err)
	}
	code.Interval = -5 // poll at once in the test
	if err := trakt.finishSignIn(code, 0); err != nil {
		t.Fatal(err)
	}
	if !NewTrakt("id", "secret", dir).SignedIn() {
		t.Fatal("token not kept")
	}
	movie := Movie{Title: "Interstellar", IMDb: "tt0816692"}
	if err := trakt.Watchlist(movie, true); err != nil {
		t.Fatal(err)
	}
	last := calls[len(calls)-1]
	if !strings.HasPrefix(last, "/sync/watchlist Bearer tok") || !strings.Contains(last, `"imdb":"tt0816692"`) {
		t.Errorf("call = %q", last)
	}
	if err := trakt.Rate(Movie{Title: "No id"}, 8); err == nil {
		t.Error("a movie without an IMDb id was sent")
	}
}

// Subtitles for the release vidsrc streams come before more downloaded ones
// for another release, and the hearing impaired ones after the plain.
func TestRankSubtitles(t *testing.T) {
	results := []openSubtitle{
		{FileName: "Interstellar.2014.720p.BluRay.x264-DAA.srt", Release: "Interstellar.2014.720p.BluRay.x264-DAA", Downloads: "2294580"},
		{FileName: "Interstellar.2014.1080p.BluRay.x264.YIFY-HI.srt", Release: "Interstellar.2014.1080p.BluRay.x264.YIFY", Downloads: "900000", HearingImpared: "1"},
		{FileName: "Interstellar.2014.1080p.BluRay.x264.YIFY.srt", Release: "Interstellar.2014.1080p.BluRay.x264.YIFY", Downloads: "570122"},
		{FileName: "Interstellar.2014.1080p.BluRay.x264.DTS-RARBG.eng.srt", Release: " Interstellar.2014.1080p.BluRay.x264.DTS-RARBG", Downloads: "274734"},
	}
	ranked := rankSubtitles(results, "Interstellar (2014) (2014) [1080p]/Interstellar.2014.2014.1080p.BluRay.x264.YIFY.mp4")
	if ranked[0].FileName != "Interstellar.2014.1080p.BluRay.x264.YIFY.srt" || ranked[1].HearingImpared != "1" {
		t.Errorf("ranked: %s, %s", ranked[0].FileName, ranked[1].FileName)
	}
	if ranked[3].Release != "Interstellar.2014.720p.BluRay.x264-DAA" {
		t.Errorf("last = %s", ranked[3].FileName)
	}
}

func TestPreferServer(t *testing.T) {
	sources := []Source{{Server: "server 3"}, {Server: "server 2"}, {Server: "imgsto.re"}}
	var names []string
	for _, source := range PreferServer(sources, "imgsto.re") {
		names = append(names, source.Server)
	}
	if strings.Join(names, ",") != "imgsto.re,server 3,server 2" {
		t.Errorf("order = %v", names)
	}
	if got := PreferServer(sources, "gone"); len(got) != 3 || got[0].Server != "server 3" {
		t.Errorf("unknown server reordered: %+v", got)
	}
}

func TestStoreKeepsTheServer(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	movie := Movie{Provider: VidsrcName, Path: "tt0816692", Title: "Interstellar"}
	if err := store.SetServer(movie, "vidsrc 2"); err != nil {
		t.Fatal(err)
	}
	if entry, _ := store.Get(movie.Key()); entry.Server != "vidsrc 2" {
		t.Errorf("entry = %+v", entry)
	}
}

// A Lithuanian title is looked up on Trakt with its year, and the first
// result's IMDb id taken.
func TestTraktFindIMDb(t *testing.T) {
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Write([]byte(`[{"type":"movie","movie":{"title":"Willy Wonka & the Chocolate Factory","year":1971,"ids":{"imdb":"tt0067992"}}},{"type":"movie","movie":{"title":"Wonka","year":2023,"ids":{"imdb":"tt6166392"}}}]`))
	}))
	defer server.Close()
	trakt := NewTrakt("id", "", t.TempDir())
	trakt.API = server.URL
	if got := trakt.FindIMDb([]string{"", "Vonka"}, "2023"); got != "tt6166392" {
		t.Errorf("imdb = %q", got)
	}
	if got := trakt.FindIMDb([]string{"Vonka"}, "1990"); got != "" {
		t.Errorf("a film from another year was taken: %q", got)
	}
	if !strings.Contains(query, "query=Vonka") || !strings.Contains(query, "aliases") {
		t.Errorf("query = %q", query)
	}
}

func TestFilmukasPageTitles(t *testing.T) {
	body := `<meta name="description" content="„Vonka“ („Wonka“) yra nuotykių ir fantazijos filmas" />{"datePublished":"2023","description":"x"}`
	if got := firstGroup(body, filmukasOriginalPattern); got != "Wonka" {
		t.Errorf("original = %q", got)
	}
	if got := firstGroup(body, filmukasPublishedPattern); got != "2023" {
		t.Errorf("year = %q", got)
	}
}

// Finishing a movie from one provider takes its copies from the others off
// Continue and the watchlist.
func TestWatchedClearsCopiesFromOtherProviders(t *testing.T) {
	store, _ := OpenStore(t.TempDir())
	vidsrc := Movie{Provider: VidsrcName, Path: "tt0111161", Title: "The Shawshank Redemption", Year: "1994", IMDb: "tt0111161"}
	filmai := Movie{Provider: FilmaiName, Path: "/filmas/x/", Title: "Pabėgimas iš Šoušenko", Original: "The Shawshank Redemption", Year: "1994"}
	other := Movie{Provider: FilmaiName, Path: "/filmas/y/", Title: "Other", Year: "1994"}
	for _, m := range []Movie{filmai, other} {
		if err := store.SetProgress(m, 900, 8000, false); err != nil {
			t.Fatal(err)
		}
		_ = store.SetWatchlist(m, true)
	}
	if err := store.SetProgress(vidsrc, 0, 8000, true); err != nil {
		t.Fatal(err)
	}
	if e, _ := store.Get(filmai.Key()); e.Started() || e.Watchlist {
		t.Errorf("copy still on Continue or watchlist: %+v", e)
	}
	if e, _ := store.Get(other.Key()); !e.Started() || !e.Watchlist {
		t.Errorf("another movie was cleared: %+v", e)
	}
}

func TestSameMovie(t *testing.T) {
	a := Movie{Title: "Wonka", Year: "2023"}
	if !SameMovie(a, Movie{Title: "wonka ", Year: "2023"}) {
		t.Error("same title and year should match")
	}
	if SameMovie(a, Movie{Title: "Wonka", Year: "1971"}) {
		t.Error("different year should not match")
	}
	if SameMovie(Movie{IMDb: "tt1", Title: "X", Year: "2000"}, Movie{IMDb: "tt2", Title: "X", Year: "2000"}) {
		t.Error("different IMDb ids should not match")
	}
}
