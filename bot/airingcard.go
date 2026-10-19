package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

// The airing board picture: the date and episode count on top, the day's
// most popular show big on the left (its AniList banner, or its blurred
// cover with the cover beside it), and the next six covers beside it. Times
// aren't drawn: Discord timestamps under it show them in each viewer's zone.

const (
	boardW      = 760
	boardH      = 334
	boardPad    = 22
	boardTop    = 62 // where the spotlight and covers start
	spotW       = 430
	spotH       = 250
	boardCovers = 6
)

// boardShow is one show on the picture, with its images downloaded.
type boardShow struct {
	Title   string
	Episode int
	Cover   image.Image
	Banner  image.Image
}

type boardFaces struct {
	label, date, count, unit, spotLabel, spotTitle, spotEp, chip, empty font.Face
}

var (
	boardFaceOnce sync.Once
	bFaces        boardFaces
	bFaceErr      error
)

func loadBoardFaces() (boardFaces, error) {
	boardFaceOnce.Do(func() {
		fonts := map[string]*opentype.Font{}
		for _, name := range []string{"Inter-Medium.otf", "Inter-SemiBold.otf", "Inter-ExtraBold.otf"} {
			data, err := cardFonts.ReadFile("assets/fonts/" + name)
			if err == nil {
				fonts[name], err = opentype.Parse(data)
			}
			if err != nil {
				bFaceErr = fmt.Errorf("%s: %w", name, err)
				return
			}
		}
		face := func(name string, size float64) font.Face {
			f, err := opentype.NewFace(fonts[name], &opentype.FaceOptions{Size: size * cardScale, DPI: 72, Hinting: font.HintingNone})
			if err != nil && bFaceErr == nil {
				bFaceErr = err
			}
			return f
		}
		medium, semi, heavy := "Inter-Medium.otf", "Inter-SemiBold.otf", "Inter-ExtraBold.otf"
		bFaces = boardFaces{
			label: face(semi, 13), date: face(heavy, 26), count: face(heavy, 20), unit: face(medium, 14),
			spotLabel: face(heavy, 12), spotTitle: face(heavy, 26), spotEp: face(medium, 15),
			chip: face(heavy, 11), empty: face(medium, 16),
		}
	})
	return bFaces, bFaceErr
}

// renderAiringBoard draws the picture as a PNG with rounded corners. shows
// are most popular first; the first is the spotlight.
func renderAiringBoard(date string, episodes int, shows []boardShow) ([]byte, error) {
	f, err := loadBoardFaces()
	if err != nil {
		return nil, err
	}
	d := &canvas{img: image.NewRGBA(image.Rect(0, 0, boardW*cardScale, boardH*cardScale))}
	d.fill(0, 0, boardW, boardH, cardBg)

	// Header: AIRING TODAY  Saturday 17 Oct ........ 20 episodes
	d.spaced(f.label, "AIRING TODAY", boardPad, 44, cardAccent, 0.18)
	d.text(f.date, date, boardPad+d.spacedWidth(f.label, "AIRING TODAY", 0.18)+14, 46, cardText)
	unit := " episodes"
	if episodes == 1 {
		unit = " episode"
	}
	n := fmt.Sprint(episodes)
	right := float64(boardW - boardPad)
	d.text(f.unit, unit, right-d.width(f.unit, unit), 45, cardMuted)
	d.text(f.count, n, right-d.width(f.unit, unit)-d.width(f.count, n), 45, cardText)

	if len(shows) == 0 {
		d.roundRect(boardPad, boardTop, boardW-2*boardPad, spotH, 10, cardTile)
		msg := "Nothing airing today"
		d.text(f.empty, msg, (boardW-d.width(f.empty, msg))/2, boardTop+spotH/2+6, cardMuted)
		return encodeRounded(d.img)
	}

	rest := shows[1:]
	if len(rest) > boardCovers {
		rest = rest[:boardCovers]
	}
	sw := float64(spotW)
	if len(rest) == 0 {
		sw = boardW - 2*boardPad
	}
	d.spotlight(shows[0], boardPad, boardTop, sw, spotH)

	// Up to six covers in a 3×2 grid beside the spotlight.
	gx := float64(boardPad + spotW + 16)
	gap := 8.0
	cw := (boardW - boardPad - gx - 2*gap) / 3
	ch := (spotH - gap) / 2
	for k, s := range rest {
		x, y := gx+float64(k%3)*(cw+gap), boardTop+float64(k/3)*(ch+gap)
		d.boardCover(s, x, y, cw, ch)
	}
	return encodeRounded(d.img)
}

// spotlight draws the main show: banner (or blurred cover plus the cover on
// the right), darkened at the bottom under SPOTLIGHT, title and episode.
func (d *canvas) spotlight(s boardShow, x, y, w, h float64) {
	f := bFaces
	r := d.rect(x, y, w, h)
	tile := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(tile, tile.Bounds(), image.NewUniform(cardTile), image.Point{}, draw.Src)
	textW := w - 32
	switch {
	case s.Banner != nil:
		draw.Draw(tile, tile.Bounds(), coverCrop(s.Banner, r.Dx(), r.Dy()), image.Point{}, draw.Src)
	case s.Cover != nil:
		draw.Draw(tile, tile.Bounds(), blur(coverCrop(s.Cover, r.Dx(), r.Dy()), 16), image.Point{}, draw.Src)
		for py := range r.Dy() {
			for px := range r.Dx() {
				tile.SetRGBA(px, py, blend(color.RGBA{A: 0xff}, tile.RGBAAt(px, py), 0.3))
			}
		}
		cw, ch := 150.0, 218.0
		cr := image.Rect(int((w-16-cw)*cardScale), int(16*cardScale), int((w-16)*cardScale), int((16+ch)*cardScale))
		draw.DrawMask(tile, cr, coverCrop(s.Cover, cr.Dx(), cr.Dy()), image.Point{},
			roundRectMask{x: float64(cr.Min.X), y: float64(cr.Min.Y), w: float64(cr.Dx()), h: float64(cr.Dy()), r: 8 * cardScale}, cr.Min, draw.Over)
		textW = w - cw - 48
	}
	// Darken the bottom so the text reads on any picture.
	th := r.Dy()
	for py := range th {
		fy := float64(py) / float64(th)
		a := 0.92 * clamp01((fy-0.35)/0.65)
		if a == 0 {
			continue
		}
		for px := range r.Dx() {
			tile.SetRGBA(px, py, blend(rgb(0x0a0a0c), tile.RGBAAt(px, py), a))
		}
	}
	draw.DrawMask(d.img, r, tile, image.Point{}, roundRectMask{x: float64(r.Min.X), y: float64(r.Min.Y), w: float64(r.Dx()), h: float64(r.Dy()), r: 10 * cardScale}, r.Min, draw.Over)

	lines := wrap(f.spotTitle, s.Title, textW*cardScale, 2)
	base := y + h - 38 - float64(len(lines)-1)*30
	d.spaced(f.spotLabel, "SPOTLIGHT", x+16, base-32, cardAccent, 0.14)
	for k, line := range lines {
		d.text(f.spotTitle, line, x+16, base+float64(k)*30, cardText)
	}
	d.text(f.spotEp, fmt.Sprintf("Episode %d", s.Episode), x+16, y+h-16, cardSoft)
}

// boardCover draws a cover with its episode number in a chip.
func (d *canvas) boardCover(s boardShow, x, y, w, h float64) {
	f := bFaces
	r := d.rect(x, y, w, h)
	tile := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(tile, tile.Bounds(), image.NewUniform(cardTile), image.Point{}, draw.Src)
	if s.Cover != nil {
		draw.Draw(tile, tile.Bounds(), coverCrop(s.Cover, r.Dx(), r.Dy()), image.Point{}, draw.Src)
	}
	draw.DrawMask(d.img, r, tile, image.Point{}, roundRectMask{x: float64(r.Min.X), y: float64(r.Min.Y), w: float64(r.Dx()), h: float64(r.Dy()), r: 6 * cardScale}, r.Min, draw.Over)
	ep := fmt.Sprint(s.Episode)
	cw := d.width(f.chip, ep) + 12
	d.roundRect(x+5, y+h-23, cw, 18, 4, color.RGBA{0x14, 0x14, 0x18, 0xe0})
	d.text(f.chip, ep, x+11, y+h-10, cardText)
}

// encodeRounded rounds the picture's corners and encodes it as PNG.
func encodeRounded(img *image.RGBA) ([]byte, error) {
	out := image.NewRGBA(img.Bounds())
	b := img.Bounds()
	draw.DrawMask(out, b, img, image.Point{}, roundRectMask{w: float64(b.Dx()), h: float64(b.Dy()), r: 12 * cardScale}, image.Point{}, draw.Over)
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
