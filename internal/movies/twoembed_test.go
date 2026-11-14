package movies

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTwoEmbedPlaysAServer(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/embed/tv/tt0903747/1/2":
			w.Write([]byte("<script>\n  var Q = {\"type\":\"tv\",\"t\":\"tok.sig\",\"ssr\":{\"servers\":[{\"ref\":\"r1\",\"name\":\"Server VNY\"},{\"ref\":\"r2\",\"name\":\"Server VNR\"}]}};\n</script>"))
		case "/pl/api.php":
			if r.Header.Get("Referer") != server.URL+"/embed/tv/tt0903747/1/2" || r.URL.Query().Get("t") != "tok.sig" {
				http.Error(w, `{"error":"unavailable"}`, http.StatusForbidden)
				return
			}
			if r.URL.Query().Get("ref") == "r1" {
				w.Write([]byte(`{"error":"dead"}`))
				return
			}
			w.Write([]byte(`{"url":"\/_stream?id=abc","type":"hls"}`))
		case "/subtitles":
			w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	e := NewTwoEmbed()
	e.Base = server.URL
	e.Client = server.Client()
	sources, err := e.OpenEpisode(Movie{Provider: TwoEmbedName, Path: "tt0903747", Series: true}, Episode{Season: 1, Number: 2})
	if err != nil || len(sources) != 2 {
		t.Fatalf("sources = %v, %v", sources, err)
	}
	if _, err := e.play(server.URL+"/embed/tv/tt0903747/1/2", "r1", "tok.sig"); err == nil {
		t.Fatal("a server without a stream must fail")
	}
	stream, err := e.play(server.URL+"/embed/tv/tt0903747/1/2", "r2", "tok.sig")
	if err != nil {
		t.Fatal(err)
	}
	if stream.URL != server.URL+"/_stream?id=abc" || !stream.HLS || stream.Referrer != server.URL+"/embed/tv/tt0903747/1/2" {
		t.Fatalf("stream = %+v", stream)
	}
}

func TestTwoEmbedNeedsAnIMDbID(t *testing.T) {
	if _, _, err := NewTwoEmbed().Open(Movie{Path: "/drama-detail/x/"}); err == nil {
		t.Fatal("want an error without an IMDb id")
	}
}
