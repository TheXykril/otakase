package main

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	_ "golang.org/x/image/webp"
)

// The /profile picture card: the bot draws the member's AniList as one PNG
// (avatar, score, stats, what they watch with cover art, favourite genres).
// The top is their AniList banner, or a blurred cover of a show they watch
// when they have none.

//go:embed assets/fonts/*.otf assets/fonts/*.ttf
var cardFonts embed.FS

// The card is landscape so Discord shows it at full width: it caps how tall a
// picture is drawn, which shrank the old portrait card to half size.
// cardScale draws it at twice that, so it stays sharp on high density
// screens. Layout numbers below are in display pixels.
const (
	cardScale = 2
	cardWidth = 760
	cardPad   = 24
	heroH     = 150
	bodyY     = 200                          // where the columns start
	leftW     = 440                          // watching now
	rightX    = cardPad + leftW + 24         // stat tiles
	rightW    = cardWidth - cardPad - rightX // stat tiles
)

var (
	cardBg     = rgb(0x141418)
	cardTile   = rgb(0x1d1d23)
	cardLine   = rgb(0x26262e)
	cardText   = rgb(0xffffff)
	cardSoft   = rgb(0xd6d6de)
	cardMuted  = rgb(0xa2a2ae)
	cardDim    = rgb(0x8a8a96)
	cardFaint  = rgb(0x6d6d78)
	cardAccent = rgb(0xef6a4c)
	cardShu    = rgb(0xd2492f)
	// genreColors colour the favourite genres bar, most watched first.
	genreColors = []color.RGBA{rgb(0xef6a4c), rgb(0xc4513a), rgb(0xe8a25c), rgb(0x8f6be8), rgb(0x4fa3c7)}
)

// Card glyphs from the bundled symbol font (Material Design Icons).
const (
	glyphWatching = '\U000F040D' // play_circle_outline
	glyphEpisodes = '\U000F0199' // counter
	glyphClock    = '\U000F0150' // clock_outline
	glyphPlay     = '\U000F040A' // play
	glyphTag      = '\U000F04FC' // tag_outline
)

type cardShow struct {
	Title    string
	Progress int
	Episodes int // 0 when unknown
	Cover    image.Image
}

type cardGenre struct {
	Name  string
	Count int
}

// profileCard is everything the picture shows, already filtered (no adult
// titles or genres) and with images downloaded.
type profileCard struct {
	Name      string
	Since     int // year joined AniList, 0 when unknown
	Completed int
	Score     float64 // 0-100, 0 hides the gauge
	Anime     int
	Episodes  int
	Days      float64
	Avatar    image.Image
	Banner    image.Image // their AniList banner, behind the name
	Hero      image.Image // blurred there instead when they have no banner; nil draws a plain glow
	Shows     []cardShow
	Hidden    bool // their list is private
	Genres    []cardGenre
	Note      string // bottom right, e.g. how to get your own
	Otakase   string // time watched in the Otakase app, e.g. "42 h"; empty hides it
}

type cardFaces struct {
	name, big, value, title, label, small, legend, ep, epSmall font.Face
	icon, iconSmall, gauge, gaugeLabel                         font.Face
}

var (
	faceOnce sync.Once
	faces    cardFaces
	faceErr  error
)

func loadFaces() (cardFaces, error) {
	faceOnce.Do(func() {
		load := func(name string) *opentype.Font {
			if faceErr != nil {
				return nil
			}
			data, err := cardFonts.ReadFile("assets/fonts/" + name)
			if err != nil {
				faceErr = err
				return nil
			}
			f, err := opentype.Parse(data)
			if err != nil {
				faceErr = fmt.Errorf("%s: %w", name, err)
			}
			return f
		}
		medium, semi, heavy, sym := load("Inter-Medium.otf"), load("Inter-SemiBold.otf"), load("Inter-ExtraBold.otf"), load("OtakaseSymbols.ttf")
		face := func(f *opentype.Font, size float64) font.Face {
			if faceErr != nil {
				return nil
			}
			fc, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size * cardScale, DPI: 72, Hinting: font.HintingNone})
			if err != nil {
				faceErr = err
			}
			return fc
		}
		faces = cardFaces{
			name: face(heavy, 34), big: face(heavy, 22), gauge: face(heavy, 23),
			value: face(semi, 12.5), title: face(semi, 13), label: face(semi, 10.5), gaugeLabel: face(semi, 9.5),
			small: face(medium, 15.5), legend: face(medium, 13), ep: face(heavy, 12.5), epSmall: face(medium, 11),
			icon: face(sym, 22), iconSmall: face(sym, 15),
		}
	})
	return faces, faceErr
}

// renderProfileCard draws the card as a PNG with rounded, transparent corners.
func renderProfileCard(c profileCard) ([]byte, error) {
	f, err := loadFaces()
	if err != nil {
		return nil, err
	}
	d := &canvas{f: f}

	// Two columns under the header: what they watch on the left, stat tiles
	// on the right; then the genres bar across the bottom.
	coverW := (leftW - 4*10) / 5.0
	coverH := coverW * 65 / 46
	tileH, tileGap := 54.0, 9.0
	cols := 3*tileH + 2*tileGap
	if len(c.Shows) > 0 {
		cols = math.Max(cols, 26+coverH+18+16+4)
	}
	y := bodyY + cols + 24
	h := y + 30 // footer
	if len(c.Genres) > 0 {
		h = y + 26 + 10 + 10 + 13 + 22
	}
	d.img = image.NewRGBA(image.Rect(0, 0, cardWidth*cardScale, int(h)*cardScale))
	d.fill(0, 0, cardWidth, h, cardBg)

	d.hero(c.Banner, c.Hero)
	d.header(c)

	// Stat tiles.
	stats := []struct {
		glyph rune
		value string
		label string
	}{
		{glyphWatching, thousands(c.Anime), "ANIME"},
		{glyphEpisodes, thousands(c.Episodes), "EPISODES"},
		{glyphClock, fmt.Sprintf("%.1f", c.Days), "DAYS WATCHED"},
	}
	for k, s := range stats {
		x, ty := float64(rightX), bodyY+float64(k)*(tileH+tileGap)
		d.roundRect(x, ty, rightW, tileH, 10, cardLine)
		d.roundRect(x+1, ty+1, rightW-2, tileH-2, 9, cardTile)
		d.roundRect(x+11, ty+10, 34, 34, 8, blend(cardShu, cardTile, 0.14))
		d.textCentered(f.icon, string(s.glyph), x+28, ty+27, cardAccent)
		d.text(f.big, s.value, x+58, ty+28, cardText)
		d.spaced(f.label, s.label, x+58, ty+44, cardDim, 0.08)
	}

	// Watching now.
	d.section(glyphPlay, "WATCHING NOW", bodyY, cardPad)
	if len(c.Shows) > 0 {
		n := fmt.Sprintf("%d shows", len(c.Shows))
		if len(c.Shows) == 1 {
			n = "1 show"
		}
		d.text(f.legend, n, cardPad+leftW-d.width(f.legend, n), bodyY+11, rgb(0x5d5d68))
	}
	switch {
	case len(c.Shows) > 0:
		for k, s := range c.Shows {
			d.cover(s, cardPad+float64(k)*(coverW+10), bodyY+26, coverW, coverH)
		}
	case c.Hidden:
		d.text(f.small, "Hidden on AniList", cardPad, bodyY+40, cardMuted)
	default:
		d.text(f.small, "Nothing right now", cardPad, bodyY+40, cardMuted)
	}

	// Favourite genres: one bar split by how often each was watched, with
	// the note on the same row as its title.
	if len(c.Genres) > 0 {
		d.section(glyphTag, "FAVOURITE GENRES", y, cardPad)
		y += 26
		total := 0
		for _, g := range c.Genres {
			total += g.Count
		}
		barW := float64(cardWidth - 2*cardPad)
		gaps := float64(len(c.Genres)-1) * 2
		mask := roundRectMask{x: cardPad * cardScale, y: y * cardScale, w: barW * cardScale, h: 10 * cardScale, r: 5 * cardScale}
		x := float64(cardPad)
		for k, g := range c.Genres {
			w := (barW - gaps) * float64(g.Count) / float64(max(total, 1))
			if k == len(c.Genres)-1 {
				w = cardPad + barW - x
			}
			d.masked(x, y, w, 10, genreColors[k%len(genreColors)], mask)
			x += w + 2
		}
		y += 10 + 10
		x = cardPad
		for k, g := range c.Genres {
			name, count := g.Name, " "+fmt.Sprint(g.Count)
			w := 15 + d.width(f.title, name) + d.width(f.legend, count)
			if x+w > cardWidth-cardPad {
				break
			}
			d.roundRect(x, y+3, 10, 10, 3, genreColors[k%len(genreColors)])
			d.text(f.title, name, x+15, y+13, rgb(0xe8e8ee))
			d.text(f.legend, count, x+15+d.width(f.title, name), y+13, cardMuted)
			x += w + 18
		}
		y -= 46
	} else {
		d.text(f.legend, "otakase", cardPad, y+11, cardFaint)
	}
	note := "otakase"
	if c.Note != "" {
		note += " · " + c.Note
	}
	if len(c.Genres) == 0 {
		note = c.Note
	}
	d.text(f.legend, note, cardWidth-cardPad-d.width(f.legend, note), y+11, cardFaint)

	// Round the outer corners.
	out := image.NewRGBA(d.img.Bounds())
	b := d.img.Bounds()
	draw.DrawMask(out, b, d.img, image.Point{}, roundRectMask{w: float64(b.Dx()), h: float64(b.Dy()), r: 12 * cardScale}, image.Point{}, draw.Over)
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// canvas draws in display pixels onto an image cardScale times larger.
type canvas struct {
	img *image.RGBA
	f   cardFaces
}

func (d *canvas) rect(x, y, w, h float64) image.Rectangle {
	return image.Rect(int(x*cardScale), int(y*cardScale), int(math.Ceil((x+w)*cardScale)), int(math.Ceil((y+h)*cardScale)))
}

func (d *canvas) fill(x, y, w, h float64, c color.Color) {
	draw.Draw(d.img, d.rect(x, y, w, h), image.NewUniform(c), image.Point{}, draw.Over)
}

func (d *canvas) roundRect(x, y, w, h, r float64, c color.Color) {
	d.masked(x, y, w, h, c, roundRectMask{x: x * cardScale, y: y * cardScale, w: w * cardScale, h: h * cardScale, r: r * cardScale})
}

// masked fills the box with c where mask (in canvas pixels) lets it through.
func (d *canvas) masked(x, y, w, h float64, c color.Color, mask image.Image) {
	r := d.rect(x, y, w, h)
	draw.DrawMask(d.img, r, image.NewUniform(c), image.Point{}, mask, r.Min, draw.Over)
}

func (d *canvas) text(f font.Face, s string, x, baseline float64, c color.Color) {
	dr := font.Drawer{Dst: d.img, Src: image.NewUniform(c), Face: f,
		Dot: fixed.P(int(x*cardScale), int(baseline*cardScale))}
	dr.DrawString(s)
}

func (d *canvas) width(f font.Face, s string) float64 {
	return float64(font.MeasureString(f, s).Round()) / cardScale
}

// textCentered centres s on (cx, cy), using the glyph's own bounds.
func (d *canvas) textCentered(f font.Face, s string, cx, cy float64, c color.Color) {
	b, _ := font.BoundString(f, s)
	w := float64((b.Max.X - b.Min.X).Round()) / cardScale
	h := float64((b.Max.Y - b.Min.Y).Round()) / cardScale
	d.text(f, s, cx-w/2-float64(b.Min.X.Round())/cardScale, cy+h/2-float64(b.Max.Y.Round())/cardScale, c)
}

// spaced draws capitals labels with extra letter spacing (in em).
func (d *canvas) spaced(f font.Face, s string, x, baseline float64, c color.Color, em float64) {
	gap := em * d.em(f)
	for _, r := range s {
		d.text(f, string(r), x, baseline, c)
		x += d.width(f, string(r)) + gap
	}
}

func (d *canvas) spacedWidth(f font.Face, s string, em float64) float64 {
	w := 0.0
	for _, r := range s {
		w += d.width(f, string(r)) + em*d.em(f)
	}
	return w - em*d.em(f)
}

// em is the face's size in display pixels (Inter's line height is 1.21 em).
func (d *canvas) em(f font.Face) float64 {
	return float64(f.Metrics().Height.Round()) / cardScale / 1.21
}

func (d *canvas) disc(cx, cy, r float64, c color.Color) {
	d.masked(cx-r, cy-r, 2*r, 2*r, c, circleMask{cx: cx * cardScale, cy: cy * cardScale, r: r * cardScale})
}

func (d *canvas) section(glyph rune, title string, y, x float64) {
	d.textCentered(d.f.iconSmall, string(glyph), x+8, y+6, cardAccent)
	d.spaced(d.f.value, title, x+23, y+11, cardDim, 0.12)
}

// hero is the AniList banner (or a blurred cover when there is none) behind
// the name, faded into the card, with a Shu glow from the bottom left.
func (d *canvas) hero(banner, cover image.Image) {
	w, h := cardWidth*cardScale, heroH*cardScale
	img := banner
	switch {
	case banner != nil:
		draw.Draw(d.img, image.Rect(0, 0, w, h), coverCrop(banner, w, h), image.Point{}, draw.Src)
	case cover != nil:
		img = cover
		draw.Draw(d.img, image.Rect(0, 0, w, h), blur(coverCrop(cover, w, h), 16), image.Point{}, draw.Src)
	}
	for py := range h {
		fy := float64(py) / float64(h)
		for px := range w {
			o := d.img.RGBAAt(px, py)
			if img != nil {
				o = saturate(blend(color.RGBA{A: 0xff}, o, 0.15), 1.3)
			}
			// Shu glow from the bottom left
			gx, gy := float64(px)/float64(w)/1.2, (1-fy)/0.9
			if g := 1 - math.Hypot(gx, gy)/0.6; g > 0 {
				o = blend(cardShu, o, 0.35*g)
			}
			// fade to the card colour: clear at the top, solid at the bottom
			fade := fy / 0.6 * 0.55
			if fy > 0.6 {
				fade = 0.55 + (fy-0.6)/0.4*0.45
			}
			o = blend(cardBg, o, fade)
			d.img.SetRGBA(px, py, o)
		}
	}
}

// header draws the avatar with its Shu ring, the name and the score gauge.
func (d *canvas) header(c profileCard) {
	f := d.f
	ax, ay, as := 24.0, 72.0, 110.0
	d.disc(ax+as/2, ay+as/2, as/2, cardAccent)
	d.disc(ax+as/2, ay+as/2, as/2-3, cardBg)
	if c.Avatar != nil {
		in := as - 12
		r := d.rect(ax+6, ay+6, in, in)
		src := coverCrop(c.Avatar, r.Dx(), r.Dy())
		draw.DrawMask(d.img, r, src, image.Point{}, circleMask{cx: float64(r.Min.X) + float64(r.Dx())/2, cy: float64(r.Min.Y) + float64(r.Dy())/2, r: float64(r.Dx()) / 2}, r.Min, draw.Over)
	}

	right := float64(cardWidth - cardPad)
	if c.Score > 0 {
		gx, gy, gr := cardWidth-cardPad-44.0, 108.0, 39.0
		d.ring(gx, gy, gr+3, gr-3, color.RGBA{0x2a, 0x2a, 0x33, 0xff}, color.RGBA{}, 1)
		d.ring(gx, gy, gr+3, gr-3, cardAccent, cardAccent, c.Score/100)
		score := fmt.Sprintf("%.1f", c.Score)
		if c.Score >= 99.95 {
			score = "100"
		}
		d.text(f.gauge, score, gx-d.width(f.gauge, score)/2, gy+4, cardText)
		d.spaced(f.gaugeLabel, "SCORE", gx-d.spacedWidth(f.gaugeLabel, "SCORE", 0.1)/2, gy+18, cardMuted, 0.1)
		right = gx - gr - 14
	}

	name := fit(f.name, c.Name, (right-152)*cardScale)
	d.text(f.name, name, 152, 130, cardText)
	line := "AniList · "
	if c.Since > 0 {
		line = fmt.Sprintf("AniList since %d · ", c.Since)
	}
	x := 152.0
	d.text(f.small, line, x, 158, cardMuted)
	x += d.width(f.small, line)
	n := thousands(c.Completed)
	d.text(f.small, n, x, 158, cardAccent)
	d.text(f.small, " completed", x+d.width(f.small, n), 158, cardMuted)
	if c.Otakase != "" {
		x += d.width(f.small, n+" completed")
		label := " · Watched in Otakase: "
		if x+d.width(f.small, label+c.Otakase) <= right {
			d.text(f.small, label, x, 158, cardMuted)
			d.text(f.small, c.Otakase, x+d.width(f.small, label), 158, cardAccent)
		}
	}
}

// ring fills the part of the annulus between rOut and rIn (display pixels)
// from 12 o'clock clockwise up to share (0-1) of the way round, with round
// ends. from and to colour the start and end of the arc.
func (d *canvas) ring(cx, cy, rOut, rIn float64, from, to color.RGBA, share float64) {
	if share <= 0 {
		return
	}
	if to == (color.RGBA{}) {
		to = from
	}
	s := float64(cardScale)
	cx, cy, rOut, rIn = cx*s, cy*s, rOut*s, rIn*s
	mid, half := (rOut+rIn)/2, (rOut-rIn)/2
	end := share * 2 * math.Pi
	b := image.Rect(int(cx-rOut-1), int(cy-rOut-1), int(cx+rOut+2), int(cy+rOut+2)).Intersect(d.img.Bounds())
	for py := b.Min.Y; py < b.Max.Y; py++ {
		for px := b.Min.X; px < b.Max.X; px++ {
			x, y := float64(px)+0.5-cx, float64(py)+0.5-cy
			dist := math.Hypot(x, y)
			a := math.Atan2(x, -y) // 0 at the top, clockwise
			if a < 0 {
				a += 2 * math.Pi
			}
			var cov float64
			if share >= 1 || a <= end {
				cov = math.Min(clamp01(rOut-dist+0.5), clamp01(dist-rIn+0.5))
			}
			if share < 1 && half > 0 { // round caps
				for _, ca := range []float64{0, end} {
					ex, ey := mid*math.Sin(ca), -mid*math.Cos(ca)
					cov = math.Max(cov, clamp01(half-math.Hypot(x-ex, y-ey)+0.5))
				}
			}
			if cov > 0 {
				col := blend(to, from, a/(2*math.Pi))
				o := d.img.RGBAAt(px, py)
				d.img.SetRGBA(px, py, blend(col, o, cov))
			}
		}
	}
}

// cover draws one show: cover art with the episode and a progress bar on it,
// title (up to two lines) below.
func (d *canvas) cover(s cardShow, x, y, w, h float64) {
	f := d.f
	r := d.rect(x, y, w, h)
	tile := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(tile, tile.Bounds(), image.NewUniform(rgb(0x222222)), image.Point{}, draw.Src)
	if s.Cover != nil {
		draw.Draw(tile, tile.Bounds(), coverCrop(s.Cover, r.Dx(), r.Dy()), image.Point{}, draw.Src)
	}
	// darken the bottom so the episode reads on any cover
	th := tile.Bounds().Dy()
	for py := th * 55 / 100; py < th; py++ {
		a := float64(py-th*55/100) / float64(th-th*55/100) * 0.85
		for px := range tile.Bounds().Dx() {
			tile.SetRGBA(px, py, blend(color.RGBA{A: 0xff}, tile.RGBAAt(px, py), a))
		}
	}
	// progress bar along the bottom edge
	bar := 4 * cardScale
	draw.Draw(tile, image.Rect(0, th-bar, tile.Bounds().Dx(), th), image.NewUniform(color.RGBA{0xff, 0xff, 0xff, 0x2e}), image.Point{}, draw.Over)
	if s.Episodes > 0 && s.Progress > 0 {
		done := int(float64(tile.Bounds().Dx()) * math.Min(float64(s.Progress)/float64(s.Episodes), 1))
		draw.Draw(tile, image.Rect(0, th-bar, done, th), image.NewUniform(cardAccent), image.Point{}, draw.Src)
	}
	draw.DrawMask(d.img, r, tile, image.Point{}, roundRectMask{x: float64(r.Min.X), y: float64(r.Min.Y), w: float64(r.Dx()), h: float64(r.Dy()), r: 8 * cardScale}, r.Min, draw.Over)

	ep := fmt.Sprintf("EP %d", s.Progress)
	d.text(f.ep, ep, x+7, y+h-11, cardText)
	if s.Episodes > 0 {
		d.text(f.epSmall, fmt.Sprintf(" / %d", s.Episodes), x+7+d.width(f.ep, ep), y+h-11, rgb(0xb8b8c2))
	}
	for k, line := range wrap(f.title, s.Title, w*cardScale, 2) {
		d.text(f.title, line, x, y+h+18+float64(k)*16, cardSoft)
	}
}

// coverCrop scales img to fill w×h, cropping the overflow from the centre
// (a bit higher than centre for portraits, where faces usually are).
func coverCrop(img image.Image, w, h int) image.Image {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw == 0 || sh == 0 {
		return image.NewRGBA(image.Rect(0, 0, w, h))
	}
	scale := math.Max(float64(w)/float64(sw), float64(h)/float64(sh))
	cw, ch := int(float64(w)/scale), int(float64(h)/scale)
	x0 := b.Min.X + (sw-cw)/2
	y0 := b.Min.Y + (sh-ch)*35/100
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(out, out.Bounds(), img, image.Rect(x0, y0, x0+cw, y0+ch), draw.Src, nil)
	return out
}

// blur softens img by shrinking it radius times and scaling it back up.
func blur(img image.Image, radius int) *image.RGBA {
	b := img.Bounds()
	small := image.NewRGBA(image.Rect(0, 0, max(b.Dx()/radius, 1), max(b.Dy()/radius, 1)))
	xdraw.ApproxBiLinear.Scale(small, small.Bounds(), img, b, draw.Src, nil)
	tiny := image.NewRGBA(image.Rect(0, 0, max(small.Bounds().Dx()/2, 1), max(small.Bounds().Dy()/2, 1)))
	xdraw.ApproxBiLinear.Scale(tiny, tiny.Bounds(), small, small.Bounds(), draw.Src, nil)
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	xdraw.CatmullRom.Scale(out, out.Bounds(), tiny, tiny.Bounds(), draw.Src, nil)
	return out
}

// fit shortens s with an ellipsis until it is at most w canvas pixels wide.
func fit(f font.Face, s string, w float64) string {
	if float64(font.MeasureString(f, s).Round()) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && float64(font.MeasureString(f, string(r)+"…").Round()) > w {
		r = r[:len(r)-1]
	}
	return strings.TrimSpace(string(r)) + "…"
}

// wrap breaks s into at most lines lines of width w, ending the last with an
// ellipsis when it doesn't fit.
func wrap(f font.Face, s string, w float64, lines int) []string {
	var out []string
	words := strings.Fields(s)
	for len(words) > 0 && len(out) < lines {
		line := words[0]
		n := 1
		for n < len(words) && float64(font.MeasureString(f, line+" "+words[n]).Round()) <= w {
			line += " " + words[n]
			n++
		}
		words = words[n:]
		if len(out) == lines-1 && len(words) > 0 {
			line = fit(f, line+" "+strings.Join(words, " "), w)
			if !strings.HasSuffix(line, "…") {
				line += "…"
			}
		}
		out = append(out, fit(f, line, w))
	}
	return out
}

func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0 && s[i-1] != '-'; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// fetchImage downloads and decodes a cover or avatar (at most 8 MB).
func fetchImage(url string) (image.Image, error) {
	if url == "" {
		return nil, fmt.Errorf("no url")
	}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "otakase-bot (https://github.com/TheXykril/otakase)")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s", req.URL.Host, resp.Status)
	}
	img, _, err := image.Decode(io.LimitReader(resp.Body, 8<<20))
	return img, err
}

// fetchImages downloads urls in parallel; failed ones stay nil.
func fetchImages(urls []string) []image.Image {
	out := make([]image.Image, len(urls))
	var wg sync.WaitGroup
	for k, u := range urls {
		wg.Go(func() {
			if img, err := fetchImage(u); err == nil {
				out[k] = img
			}
		})
	}
	wg.Wait()
	return out
}

// Masks, in canvas pixels, with one pixel of anti-aliasing.

type roundRectMask struct{ x, y, w, h, r float64 }

func (m roundRectMask) ColorModel() color.Model { return color.AlphaModel }
func (m roundRectMask) Bounds() image.Rectangle {
	return image.Rect(int(m.x), int(m.y), int(math.Ceil(m.x+m.w)), int(math.Ceil(m.y+m.h)))
}
func (m roundRectMask) At(px, py int) color.Color {
	x, y := float64(px)+0.5, float64(py)+0.5
	// distance outside the rounded rectangle
	qx := math.Abs(x-(m.x+m.w/2)) - (m.w/2 - m.r)
	qy := math.Abs(y-(m.y+m.h/2)) - (m.h/2 - m.r)
	dist := math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - m.r
	return color.Alpha{uint8(clamp01(0.5-dist) * 255)}
}

type circleMask struct{ cx, cy, r float64 }

func (m circleMask) ColorModel() color.Model { return color.AlphaModel }
func (m circleMask) Bounds() image.Rectangle {
	return image.Rect(int(m.cx-m.r), int(m.cy-m.r), int(math.Ceil(m.cx+m.r)), int(math.Ceil(m.cy+m.r)))
}
func (m circleMask) At(px, py int) color.Color {
	dist := math.Hypot(float64(px)+0.5-m.cx, float64(py)+0.5-m.cy) - m.r
	return color.Alpha{uint8(clamp01(0.5-dist) * 255)}
}

// saturate pushes c away from its grey by k (1 keeps it).
func saturate(c color.RGBA, k float64) color.RGBA {
	grey := 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
	ch := func(v uint8) uint8 { return uint8(math.Round(255 * clamp01((grey+(float64(v)-grey)*k)/255))) }
	return color.RGBA{ch(c.R), ch(c.G), ch(c.B), c.A}
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func rgb(v uint32) color.RGBA {
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xff}
}

// blend mixes a over b: share 1 is all a.
func blend(a, b color.RGBA, share float64) color.RGBA {
	share = clamp01(share)
	mix := func(x, y uint8) uint8 { return uint8(math.Round(float64(x)*share + float64(y)*(1-share))) }
	return color.RGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), 0xff}
}
