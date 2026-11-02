package mobilecore

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/thexykril/otakase/internal/movies"
)

func movieData(t *testing.T, r response, key string, out any) {
	t.Helper()
	if r.Error != nil {
		t.Fatalf("error: %+v", r.Error)
	}
	raw, _ := json.Marshal(r.Data.(map[string]any)[key])
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatal(err)
	}
}

func TestMovieProvidersCarryTheirLanguage(t *testing.T) {
	c := NewCore()
	defer c.Close()
	var list []movieSource
	movieData(t, call(t, c, `{"version":1,"operation":"movie_providers"}`), "providers", &list)
	languages := map[string]string{}
	for _, s := range list {
		languages[s.Name] = s.Language
	}
	if languages["vidsrc"] != "en" || languages["8filmai"] != "lt" || languages["filmukas"] != "lt" {
		t.Fatalf("languages = %v", languages)
	}
}

func TestVidsrcMatchUsesIMDbWithoutRequests(t *testing.T) {
	c := NewCore()
	defer c.Close()
	c.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("unexpected request to %s", r.URL)
		return reply(r, 500, nil), nil
	})
	var m movieTitle
	movieData(t, call(t, c, `{"version":1,"operation":"movie_match","provider":"vidsrc","titles":["Interstellar"],"year":2014,"imdb":"tt0816692"}`), "movie", &m)
	if m.Path != "tt0816692" || m.IMDb != "tt0816692" || m.Provider != "vidsrc" {
		t.Fatalf("movie = %+v", m)
	}
	r := call(t, c, `{"version":1,"operation":"movie_match","provider":"vidsrc","titles":["No id"]}`)
	if r.Error == nil || r.Error.Code != "not_found" {
		t.Fatalf("want not_found, got %+v", r)
	}
}

const filmukasListing = `<ul>
<li id="li_1"><a href="/filmai/vonka-2023-11"><img data-src="/img/a.jpg" alt="Vonka"></a></li>
<li id="li_2"><a href="/filmai/vonka-1971-12"><img data-src="/img/b.jpg" alt="Vonka"></a></li>
<li id="li_3"><a href="/filmai/kita-2023-13"><img data-src="/img/c.jpg" alt="Kita"></a></li>
</ul>`

func filmukasCore(t *testing.T) *Core {
	c := NewCore()
	t.Cleanup(c.Close)
	c.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Path == "/visi":
			return reply(r, 200, []byte(filmukasListing)), nil
		case r.URL.Path == "/filmai/vonka-2023-11":
			return reply(r, 200, []byte(`<h1 itemprop="name">Vonka</h1><script>loadScript('/@videojs-v5.js?id=11')</script>`)), nil
		case r.URL.Path == "/@videojs-v5.js":
			return reply(r, 200, []byte(`var videoSrc = 'https://cdn.example/v/11/master.m3u8';`)), nil
		}
		return reply(r, 404, nil), nil
	})
	return c
}

func TestFilmukasMatchPicksTheSameYear(t *testing.T) {
	c := filmukasCore(t)
	var m movieTitle
	movieData(t, call(t, c, `{"version":1,"operation":"movie_match","provider":"filmukas","titles":["Vonka","Wonka"],"year":2023,"imdb":"tt6166392"}`), "movie", &m)
	if m.Path != "/filmai/vonka-2023-11" {
		t.Fatalf("movie = %+v", m)
	}
	r := call(t, c, `{"version":1,"operation":"movie_match","provider":"filmukas","titles":["Vonka"],"year":2023,"series":true,"season":1}`)
	if r.Error == nil || r.Error.Code != "not_found" {
		t.Fatalf("filmukas has no series, got %+v", r)
	}
	r = call(t, c, `{"version":1,"operation":"movie_match","provider":"filmukas","titles":["Vonka"],"year":1990}`)
	if r.Error == nil || r.Error.Code != "not_found" {
		t.Fatalf("another year must not match, got %+v", r)
	}
}

func TestFilmukasResolveGivesTheStreamWithReferrer(t *testing.T) {
	c := filmukasCore(t)
	var s movieStream
	movieData(t, call(t, c, `{"version":1,"operation":"movie_resolve","movie":{"provider":"filmukas","path":"/filmai/vonka-2023-11","title":"Vonka"}}`), "stream", &s)
	if s.URL != "https://cdn.example/v/11/master.m3u8" || !s.HLS || s.Headers["Referer"] != "https://www.filmukas.com/" || s.Server != "Filmukas" || s.AudioLanguage != "lt" {
		t.Fatalf("stream = %+v", s)
	}
	r := call(t, c, `{"version":1,"operation":"movie_resolve","movie":{"provider":"filmukas","path":"/filmai/vonka-2023-11"},"skip":["Filmukas"]}`)
	if r.Error == nil || r.Error.Code != "no_stream" {
		t.Fatalf("skipped server must not play, got %+v", r)
	}
}

func TestMovieRequestsAreChecked(t *testing.T) {
	c := NewCore()
	defer c.Close()
	for _, input := range []string{
		`{"version":1,"operation":"movie_match","provider":"vidsrc"}`,
		`{"version":1,"operation":"movie_resolve"}`,
		`{"version":1,"operation":"movie_resolve","movie":{"provider":"vidsrc","path":"tt1","series":true}}`,
		`{"version":1,"operation":"movie_match","provider":"vidsrc","imdb":"tt1","bogus":1}`,
		`{"version":1,"operation":"movie_nothing"}`,
	} {
		if r := call(t, c, input); r.Error == nil || r.Error.Code != "invalid_request" {
			t.Errorf("%s: want invalid_request, got %+v", input, r)
		}
	}
	r := call(t, c, `{"version":1,"operation":"movie_match","provider":"nowhere","titles":["x"]}`)
	if r.Error == nil {
		t.Fatal("unknown source must fail")
	}
}

func TestMovieScore(t *testing.T) {
	req := movieRequest{Titles: []string{"Tarp žvaigždžių", "Interstellar"}, Year: 2014}
	same := movieScore(movies.Movie{Title: "Tarp žvaigždžių", Year: "2014"}, req)
	original := movieScore(movies.Movie{Title: "Kita", Original: "Interstellar", Year: "2015"}, req)
	remake := movieScore(movies.Movie{Title: "Interstellar", Year: "1990"}, req)
	other := movieScore(movies.Movie{Title: "Gravity", Year: "2014"}, req)
	if same != 100 || original < 50 || remake >= 50 || other != 0 {
		t.Fatalf("scores same=%d original=%d remake=%d other=%d", same, original, remake, other)
	}
	if seasonOfPath("/serialai/tamsa-2-sezonas-online/") != 2 || seasonOfPath("/serialai/x-online/") != 1 {
		t.Fatal("season path")
	}
	if !strings.HasPrefix(movieSources[0].Name, "vidsrc") {
		t.Fatal("vidsrc should come first")
	}
}
