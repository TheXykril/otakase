package internal

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestComposeIdleCardProducesAValidPNGWithNoCover(t *testing.T) {
	data := composeIdleCard(nil, []string{"Attack on Titan", "Episode 5 watched"})

	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("composeIdleCard did not produce a decodable PNG: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != idleCardWidth || bounds.Dy() != idleCardHeight {
		t.Fatalf("got %dx%d, want %dx%d", bounds.Dx(), bounds.Dy(), idleCardWidth, idleCardHeight)
	}
}

func TestComposeIdleCardStretchesTheCoverToFillTheFrame(t *testing.T) {
	cover := image.NewRGBA(image.Rect(0, 0, 40, 30))
	// A distinctive color away from the card's plain background and dark
	// band, so a pixel sampled from the top-left corner (outside the band)
	// proves the cover was actually drawn rather than skipped.
	fill := color.RGBA{R: 200, G: 10, B: 10, A: 255}
	for y := 0; y < 30; y++ {
		for x := 0; x < 40; x++ {
			cover.Set(x, y, fill)
		}
	}

	data := composeIdleCard(cover, []string{"Title"})
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("composeIdleCard did not produce a decodable PNG: %v", err)
	}

	r, g, b, _ := img.At(10, 10).RGBA()
	if r>>8 < 150 || g>>8 > 60 || b>>8 > 60 {
		t.Fatalf("top-left pixel is %d,%d,%d -- cover was not drawn", r>>8, g>>8, b>>8)
	}
}

func TestFetchCoverImageRejectsAnEmptyURL(t *testing.T) {
	if _, err := fetchCoverImage(""); err == nil {
		t.Fatal("expected an error for an empty URL")
	}
}

func TestFetchCoverImageDecodesAServedImage(t *testing.T) {
	cover := image.NewRGBA(image.Rect(0, 0, 10, 10))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_ = png.Encode(w, cover)
	}))
	defer server.Close()

	img, err := fetchCoverImage(server.URL)
	if err != nil {
		t.Fatalf("fetchCoverImage returned an error: %v", err)
	}
	if img.Bounds().Dx() != 10 || img.Bounds().Dy() != 10 {
		t.Fatalf("got %dx%d, want 10x10", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestFetchCoverImageReportsA404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	if _, err := fetchCoverImage(server.URL); err == nil {
		t.Fatal("expected an error for a 404")
	}
}

func TestBuildIdleCardPNGNeverErrorsWhenTheCoverCannotBeFetched(t *testing.T) {
	data := buildIdleCardPNG("http://127.0.0.1:1/no-such-host", []string{"Title", "Message"})
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("buildIdleCardPNG did not produce a decodable PNG on a fetch failure: %v", err)
	}
}
