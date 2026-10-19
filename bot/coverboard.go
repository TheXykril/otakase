package main

import (
	"image"
	"image/color"
	"image/draw"
)

// A picture in the airing board's style for the season preview,
// /server-top and /recommend: a label and title on top with a count on the
// right, the first show big as the spotlight and up to six covers beside
// it, each with a short chip.

type coverBoard struct {
	Label     string // "SEASON PREVIEW"
	Title     string // "Fall 2026"
	Count     string // "48"
	Unit      string // " new shows"
	SpotLabel string // above the spotlight's title
	Empty     string // drawn when there are no shows
	Shows     []tileShow
}

// tileShow is one show on the picture, with its images downloaded.
type tileShow struct {
	Title  string
	Sub    string // under the spotlight's title
	Chip   string // on a cover, empty for none
	Cover  image.Image
	Banner image.Image
}

// tileImages downloads covers (and the first show's banner) for tiles.
func tileImages(tiles []tileShow, covers []string, banner string) {
	imgs := fetchImages(append(append([]string(nil), covers...), banner))
	for k := range tiles {
		if k < len(covers) {
			tiles[k].Cover = imgs[k]
		}
	}
	if len(tiles) > 0 {
		tiles[0].Banner = imgs[len(imgs)-1]
	}
}

func renderCoverBoard(c coverBoard) ([]byte, error) {
	f, err := loadBoardFaces()
	if err != nil {
		return nil, err
	}
	d := &canvas{img: image.NewRGBA(image.Rect(0, 0, boardW*cardScale, boardH*cardScale))}
	d.fill(0, 0, boardW, boardH, cardBg)

	d.spaced(f.label, c.Label, boardPad, 44, cardAccent, 0.18)
	x := boardPad + d.spacedWidth(f.label, c.Label, 0.18) + 14
	right := float64(boardW - boardPad)
	countW := d.width(f.unit, c.Unit) + d.width(f.count, c.Count)
	d.text(f.unit, c.Unit, right-d.width(f.unit, c.Unit), 45, cardMuted)
	d.text(f.count, c.Count, right-countW, 45, cardText)
	d.text(f.date, fit(f.date, c.Title, (right-countW-16-x)*cardScale), x, 46, cardText)

	if len(c.Shows) == 0 {
		d.roundRect(boardPad, boardTop, boardW-2*boardPad, spotH, 10, cardTile)
		d.text(f.empty, c.Empty, (boardW-d.width(f.empty, c.Empty))/2, boardTop+spotH/2+6, cardMuted)
		return encodeRounded(d.img)
	}
	rest := c.Shows[1:]
	if len(rest) > boardCovers {
		rest = rest[:boardCovers]
	}
	sw := float64(spotW)
	if len(rest) == 0 {
		sw = boardW - 2*boardPad
	}
	d.tileSpotlight(c.Shows[0], c.SpotLabel, boardPad, boardTop, sw, spotH)

	gx := float64(boardPad + spotW + 16)
	gap := 8.0
	cw := (boardW - boardPad - gx - 2*gap) / 3
	ch := (spotH - gap) / 2
	for k, s := range rest {
		d.tileCover(s, gx+float64(k%3)*(cw+gap), boardTop+float64(k/3)*(ch+gap), cw, ch)
	}
	return encodeRounded(d.img)
}

// tileSpotlight is the airing board's spotlight with a label and a sub
// line of our own.
func (d *canvas) tileSpotlight(s tileShow, label string, x, y, w, h float64) {
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
	th := r.Dy()
	for py := range th {
		a := 0.92 * clamp01((float64(py)/float64(th)-0.35)/0.65)
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
	d.spaced(f.spotLabel, label, x+16, base-32, cardAccent, 0.14)
	for k, line := range lines {
		d.text(f.spotTitle, line, x+16, base+float64(k)*30, cardText)
	}
	d.text(f.spotEp, fit(f.spotEp, s.Sub, textW*cardScale), x+16, y+h-16, cardSoft)
}

// tileCover is a cover with its chip in the corner.
func (d *canvas) tileCover(s tileShow, x, y, w, h float64) {
	f := bFaces
	r := d.rect(x, y, w, h)
	tile := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(tile, tile.Bounds(), image.NewUniform(cardTile), image.Point{}, draw.Src)
	if s.Cover != nil {
		draw.Draw(tile, tile.Bounds(), coverCrop(s.Cover, r.Dx(), r.Dy()), image.Point{}, draw.Src)
	}
	draw.DrawMask(d.img, r, tile, image.Point{}, roundRectMask{x: float64(r.Min.X), y: float64(r.Min.Y), w: float64(r.Dx()), h: float64(r.Dy()), r: 6 * cardScale}, r.Min, draw.Over)
	if s.Chip == "" {
		return
	}
	chip := fit(f.chip, s.Chip, (w-22)*cardScale)
	cw := d.width(f.chip, chip) + 12
	d.roundRect(x+5, y+h-23, cw, 18, 4, color.RGBA{0x14, 0x14, 0x18, 0xe0})
	d.text(f.chip, chip, x+11, y+h-10, cardText)
}
