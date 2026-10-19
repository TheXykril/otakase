package jikan

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

func server(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	c := New(nil)
	c.Base = s.URL
	return c
}

func TestEpisode(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/anime/21/episodes/5" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Write([]byte(`{"data":{"title":"Five","title_romanji":"Go","filler":true,"recap":false,"aired":"2000-01-01"}}`))
	})
	got, err := c.Episode(21, 5)
	if err != nil || got.Title != "Five" || got.TitleRomaji != "Go" || !got.Filler || got.Aired != "2000-01-01" {
		t.Fatalf("Episode = %+v, %v", got, err)
	}
}

func TestEpisodeMissingData(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) })
	if _, err := c.Episode(1, 1); err == nil {
		t.Fatal("want an error for an answer without data")
	}
}

func TestFillerEpisodesFollowsPages(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "1":
			w.Write([]byte(`{"data":[{"mal_id":1},{"mal_id":2,"filler":true}],"pagination":{"has_next_page":true}}`))
		case "2":
			w.Write([]byte(`{"data":[{"mal_id":3,"filler":true}],"pagination":{"has_next_page":false}}`))
		default:
			t.Errorf("unexpected page %q", r.URL.Query().Get("page"))
		}
	})
	got, err := c.FillerEpisodes(1)
	if err != nil || !reflect.DeepEqual(got, []int{2, 3}) {
		t.Fatalf("FillerEpisodes = %v, %v", got, err)
	}
}

func TestBusyIsRetried(t *testing.T) {
	var calls atomic.Int32
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"data":{"title":"ok"}}`))
	})
	if got, err := c.Episode(1, 1); err != nil || got.Title != "ok" {
		t.Fatalf("Episode = %+v, %v", got, err)
	}
}

func TestPictures(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"jpg":{"image_url":"a.jpg","large_image_url":"b.jpg"},"webp":{"small_image_url":"c.webp"}}]}`)
	})
	got, err := c.Pictures(1)
	if err != nil || !reflect.DeepEqual(got, []string{"a.jpg", "b.jpg", "c.webp"}) {
		t.Fatalf("Pictures = %v, %v", got, err)
	}
}
