package jikan

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestPictures(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"jpg":{"image_url":"a.jpg","large_image_url":"b.jpg"},"webp":{"small_image_url":"c.webp"}}]}`)
	})
	got, err := c.Pictures(1)
	if err != nil || !reflect.DeepEqual(got, []string{"a.jpg", "b.jpg", "c.webp"}) {
		t.Fatalf("Pictures = %v, %v", got, err)
	}
}
