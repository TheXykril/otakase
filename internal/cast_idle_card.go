package internal

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io"
	"strings"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// idleCardWidth and idleCardHeight are the frame size pushed to the device.
// 1280x720 is a plain 16:9 the Default Media Receiver displays at full
// screen without letterboxing on any device this project has been run
// against.
const (
	idleCardWidth      = 1280
	idleCardHeight     = 720
	idleCardBandHeight = 160
)

// sanitizeForCardFont maps a string to what basicfont.Face7x13 can actually
// draw. The face only covers U+0020-U+007E; anything else -- an em dash, an
// accented letter, a non-Latin title -- draws as an unreadable replacement
// box on the device instead. '?' is at least legible.
func sanitizeForCardFont(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7E {
			return '?'
		}
		return r
	}, s)
}

// composeIdleCard draws cover art, stretched to fill the frame, under a dark
// band carrying up to a few lines of text.
//
// cover may be nil -- a fetch failure must still produce a screen, not no
// screen at all -- in which case the frame is a plain dark background.
//
// The cover is stretched rather than cropped to preserve aspect ratio: a
// centered crop needs source-and-destination aspect math this feature does
// not need to get exactly right, and a slightly stretched cover for ten
// seconds between episodes is not worth the extra code.
func composeIdleCard(cover image.Image, lines []string) []byte {
	canvas := image.NewRGBA(image.Rect(0, 0, idleCardWidth, idleCardHeight))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.RGBA{R: 20, G: 20, B: 20, A: 255}), image.Point{}, draw.Src)
	if cover != nil {
		xdraw.CatmullRom.Scale(canvas, canvas.Bounds(), cover, cover.Bounds(), xdraw.Over, nil)
	}

	band := image.Rect(0, idleCardHeight-idleCardBandHeight, idleCardWidth, idleCardHeight)
	draw.Draw(canvas, band, image.NewUniform(color.RGBA{A: 190}), image.Point{}, draw.Over)

	face := basicfont.Face7x13
	lineHeight := face.Metrics().Height.Ceil() + 6
	y := idleCardHeight - idleCardBandHeight + 30
	for _, line := range lines {
		drawer := &font.Drawer{
			Dst:  canvas,
			Src:  image.White,
			Face: face,
			Dot:  fixed.Point26_6{X: fixed.I(40), Y: fixed.I(y)},
		}
		drawer.DrawString(sanitizeForCardFont(line))
		y += lineHeight
	}

	var buf bytes.Buffer
	// png.Encode only errors if the writer fails; bytes.Buffer's Write never
	// does, so there is nothing a caller could do with this error.
	_ = png.Encode(&buf, canvas)
	return buf.Bytes()
}

// fetchCoverImage downloads and decodes a cover art URL.
//
// Any failure here is meant to be logged and treated as non-fatal by the
// caller -- buildIdleCardPNG is what callers actually use, and it never
// fails even when this does.
func fetchCoverImage(url string) (image.Image, error) {
	if url == "" {
		return nil, fmt.Errorf("cast: no cover image URL")
	}
	resp, err := sharedHTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("cast: could not fetch cover image: %w", err)
	}
	defer resp.Body.Close()
	if !httpStatusOK(resp.StatusCode) {
		return nil, fmt.Errorf("cast: cover image request failed with status %d", resp.StatusCode)
	}
	// 10MB is far more than any AniList/MAL cover art needs; it exists to
	// bound a misbehaving or malicious response, not real covers.
	img, _, err := image.Decode(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("cast: could not decode cover image: %w", err)
	}
	return img, nil
}

// buildIdleCardPNG is what callers use: cover art plus lines of text,
// encoded as a PNG. It never returns an error -- a cover that could not be
// fetched still produces a card, just without the picture, because the
// alternative is no screen at all between episodes.
func buildIdleCardPNG(coverURL string, lines []string) []byte {
	cover, err := fetchCoverImage(coverURL)
	if err != nil {
		Log(fmt.Sprintf("cast: idle card cover image unavailable: %v", err))
	}
	return composeIdleCard(cover, lines)
}
