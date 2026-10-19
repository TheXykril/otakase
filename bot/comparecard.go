package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

// The /compare picture, in the /profile card's style: the banner faded
// into the card, both avatars with their names, the score agreement gauge
// between them and how many shows they share; then the shows both rated
// highest and the ones the other member loved that the caller hasn't seen.

type compareCard struct {
	You, Them             string
	YouAvatar, ThemAvatar image.Image
	Banner                image.Image // the caller's AniList banner
	Hero                  image.Image // blurred there when there is no banner
	Common                int
	Agreement             int // -1 when no show was scored by both
	Shared                []tileShow
	Loved                 []tileShow
}

func renderCompareCard(c compareCard) ([]byte, error) {
	f, err := loadFaces()
	if err != nil {
		return nil, err
	}
	d := &canvas{f: f}
	colW := (cardWidth - 2*cardPad - 24) / 2.0
	coverW := (colW - 2*12) / 3
	coverH := coverW * 65 / 46
	top := bodyY + 26.0
	foot := top + coverH + 18 + 16 + 30
	h := foot + 22
	d.img = image.NewRGBA(image.Rect(0, 0, cardWidth*cardScale, int(h)*cardScale))
	d.fill(0, 0, cardWidth, h, cardBg)
	d.hero(c.Banner, c.Hero)

	// Avatars at both ends, names beside them, the gauge in the middle.
	as := 104.0
	ay := 38.0
	d.avatar(c.YouAvatar, cardPad, ay, as)
	d.avatar(c.ThemAvatar, cardWidth-cardPad-as, ay, as)
	gx, gy, gr := cardWidth/2.0, 92.0, 42.0
	nameW := gx - gr - 18 - (cardPad + as + 16)
	you := fit(f.big, c.You, nameW*cardScale)
	them := fit(f.big, c.Them, nameW*cardScale)
	d.text(f.big, you, cardPad+as+16, 98, cardText)
	d.text(f.big, them, cardWidth-cardPad-as-16-d.width(f.big, them), 98, cardText)
	d.text(f.legend, "you", cardPad+as+16, 118, cardMuted)
	d.text(f.legend, "them", cardWidth-cardPad-as-16-d.width(f.legend, "them"), 118, cardMuted)

	d.ring(gx, gy, gr+3, gr-3, color.RGBA{0x2a, 0x2a, 0x33, 0xff}, color.RGBA{}, 1)
	score := "–"
	if c.Agreement >= 0 {
		d.ring(gx, gy, gr+3, gr-3, cardAccent, cardAccent, float64(c.Agreement)/100)
		score = fmt.Sprintf("%d%%", c.Agreement)
	}
	d.text(f.gauge, score, gx-d.width(f.gauge, score)/2, gy+4, cardText)
	d.spaced(f.gaugeLabel, "AGREE", gx-d.spacedWidth(f.gaugeLabel, "AGREE", 0.1)/2, gy+18, cardMuted, 0.1)

	n := thousands(c.Common)
	rest := " shows in common"
	if c.Common == 1 {
		rest = " show in common"
	}
	w := d.width(f.small, n) + d.width(f.small, rest)
	d.text(f.small, n, gx-w/2, 168, cardAccent)
	d.text(f.small, rest, gx-w/2+d.width(f.small, n), 168, cardMuted)

	cols := []struct {
		glyph rune
		title string
		note  string
		shows []tileShow
		empty string
	}{
		{glyphWatching, "BOTH RATED HIGHEST", "you · them", c.Shared, "No show you both scored"},
		{glyphPlay, "THEY LOVED, YOU HAVEN'T SEEN", "", c.Loved, "Nothing new for you"},
	}
	for k, col := range cols {
		x := cardPad + float64(k)*(colW+24)
		d.section(col.glyph, col.title, bodyY, x)
		if col.note != "" {
			d.text(f.legend, col.note, x+colW-d.width(f.legend, col.note), bodyY+11, rgb(0x5d5d68))
		}
		if len(col.shows) == 0 {
			d.text(f.small, col.empty, x, top+20, cardMuted)
		}
		for n, s := range col.shows {
			d.scoreCover(s, x+float64(n)*(coverW+12), top, coverW, coverH)
		}
	}

	note := "otakase · /compare someone"
	d.text(f.legend, note, cardWidth-cardPad-d.width(f.legend, note), foot, cardFaint)

	out := image.NewRGBA(d.img.Bounds())
	b := d.img.Bounds()
	draw.DrawMask(out, b, d.img, image.Point{}, roundRectMask{w: float64(b.Dx()), h: float64(b.Dy()), r: 12 * cardScale}, image.Point{}, draw.Over)
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// avatar draws a round avatar in a Shu ring.
func (d *canvas) avatar(img image.Image, x, y, size float64) {
	d.disc(x+size/2, y+size/2, size/2, cardAccent)
	d.disc(x+size/2, y+size/2, size/2-3, cardBg)
	if img == nil {
		return
	}
	in := size - 12
	r := d.rect(x+6, y+6, in, in)
	src := coverCrop(img, r.Dx(), r.Dy())
	draw.DrawMask(d.img, r, src, image.Point{}, circleMask{cx: float64(r.Min.X) + float64(r.Dx())/2, cy: float64(r.Min.Y) + float64(r.Dy())/2, r: float64(r.Dx()) / 2}, r.Min, draw.Over)
}

// scoreCover is a cover with its scores at the bottom and the title below.
func (d *canvas) scoreCover(s tileShow, x, y, w, h float64) {
	f := d.f
	r := d.rect(x, y, w, h)
	tile := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(tile, tile.Bounds(), image.NewUniform(rgb(0x222222)), image.Point{}, draw.Src)
	if s.Cover != nil {
		draw.Draw(tile, tile.Bounds(), coverCrop(s.Cover, r.Dx(), r.Dy()), image.Point{}, draw.Src)
	}
	th := tile.Bounds().Dy()
	for py := th * 60 / 100; py < th; py++ {
		a := float64(py-th*60/100) / float64(th-th*60/100) * 0.85
		for px := range tile.Bounds().Dx() {
			tile.SetRGBA(px, py, blend(color.RGBA{A: 0xff}, tile.RGBAAt(px, py), a))
		}
	}
	draw.DrawMask(d.img, r, tile, image.Point{}, roundRectMask{x: float64(r.Min.X), y: float64(r.Min.Y), w: float64(r.Dx()), h: float64(r.Dy()), r: 8 * cardScale}, r.Min, draw.Over)
	if s.Chip != "" {
		d.text(f.ep, fit(f.ep, s.Chip, (w-14)*cardScale), x+7, y+h-10, cardText)
	}
	for k, line := range wrap(f.title, s.Title, w*cardScale, 2) {
		d.text(f.title, line, x, y+h+18+float64(k)*16, cardSoft)
	}
}
