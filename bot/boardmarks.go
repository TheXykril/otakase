package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Discord can't colour message text, so the airing board writes AIRING NOW
// and UP NEXT as Shu pictures: app emojis the bot uploads itself (they work
// in every server without a server emoji slot). Emojis show as squares, so
// each label is cut into pieces of two letters, spaced like the labels on
// the picture. Aired hours get a grey tick.

// boardMarks are the emojis as message text, empty when not uploaded.
type boardMarks struct{ Live, Next, Aired string }

type boardMarkCache struct {
	mu    sync.Mutex
	marks boardMarks
	tried time.Time
}

// boardMarkArt is one emoji: its name and its picture.
type boardMarkArt struct {
	name string
	png  func() ([]byte, error)
}

// Shu labels in pieces: otk_an1… spells AIRING NOW, otk_un1… UP NEXT.
var boardLabels = []struct{ prefix, text string }{{"otk_an", "AIRING NOW"}, {"otk_un", "UP NEXT"}}

func boardMarkArts() []boardMarkArt {
	var list []boardMarkArt
	for _, l := range boardLabels {
		for k := 0; k*2 < len(l.text); k++ {
			pair := l.text[k*2 : min(k*2+2, len(l.text))]
			list = append(list, boardMarkArt{fmt.Sprintf("%s%d", l.prefix, k+1), func() ([]byte, error) { return boardLetters(pair) }})
		}
	}
	return append(list, boardMarkArt{"otk_aired", func() ([]byte, error) {
		return boardMarkPNG(func(x, y float64) color.NRGBA {
			d := math.Min(segDist(x, y, -0.62, 0.02, -0.2, 0.44), segDist(x, y, -0.2, 0.44, 0.66, -0.42))
			return color.NRGBA{0x80, 0x84, 0x8e, uint8(255 * clamp01((0.13-d)/0.03))}
		}), nil
	}})
}

// segDist is the distance from (x, y) to the segment a-b.
func segDist(x, y, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	t := clamp01(((x-ax)*dx + (y-ay)*dy) / (dx*dx + dy*dy))
	return math.Hypot(x-ax-t*dx, y-ay-t*dy)
}

const markSize = 128

// boardMarkPNG draws one mark at 128×128 on a clear background.
func boardMarkPNG(draw func(x, y float64) color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, markSize, markSize))
	for py := range markSize {
		for px := range markSize {
			img.SetNRGBA(px, py, draw((float64(px)+0.5)/markSize*2-1, (float64(py)+0.5)/markSize*2-1))
		}
	}
	return encodePNG(img)
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

var (
	markFaceOnce sync.Once
	markFace     font.Face
	markFaceErr  error
)

// boardLetters draws up to two Shu letters, each centred in its half. The
// halves sit a little apart so the gap Discord leaves between emojis
// matches the gap inside one.
func boardLetters(pair string) ([]byte, error) {
	markFaceOnce.Do(func() {
		data, err := cardFonts.ReadFile("assets/fonts/Inter-ExtraBold.otf")
		var f *opentype.Font
		if err == nil {
			f, err = opentype.Parse(data)
		}
		if err == nil {
			markFace, err = opentype.NewFace(f, &opentype.FaceOptions{Size: 66, DPI: 72, Hinting: font.HintingNone})
		}
		markFaceErr = err
	})
	if markFaceErr != nil {
		return nil, markFaceErr
	}
	img := image.NewNRGBA(image.Rect(0, 0, markSize, markSize))
	d := &font.Drawer{Dst: img, Src: image.NewUniform(color.NRGBA{0xd2, 0x49, 0x2f, 0xff}), Face: markFace}
	for k, centre := range []float64{29.75, 98.25} {
		if k >= len(pair) {
			break
		}
		ch := pair[k : k+1]
		w := float64(d.MeasureString(ch)) / 64
		x := math.Max(1, math.Min(centre-w/2, markSize-1-w)) // a wide W stays inside
		d.Dot = fixed.Point26_6{X: fixed.Int26_6(x * 64), Y: fixed.I(86)}
		d.DrawString(ch)
	}
	return encodePNG(img), nil
}

// boardMarks returns the marks, uploading any that are missing. A failure
// leaves them out and is retried after an hour.
func (b *bot) boardMarks() boardMarks {
	c := &b.marks
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.marks.Live != "" || time.Since(c.tried) < time.Hour || b.s.State.User == nil {
		return c.marks
	}
	c.tried = time.Now()
	app := b.s.State.User.ID
	if b.s.State.Application != nil {
		app = b.s.State.Application.ID
	}
	have, err := b.s.ApplicationEmojis(app)
	if err != nil {
		log.Printf("airing marks: %v", err)
		return c.marks
	}
	found := map[string]string{}
	for _, e := range have {
		found[e.Name] = e.MessageFormat()
	}
	for _, art := range boardMarkArts() {
		if found[art.name] != "" {
			continue
		}
		data, err := art.png()
		if err == nil {
			var e *discordgo.Emoji
			e, err = b.s.ApplicationEmojiCreate(app, &discordgo.EmojiParams{Name: art.name,
				Image: "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)})
			if err == nil {
				found[art.name] = e.MessageFormat()
			}
		}
		if err != nil {
			log.Printf("airing marks: uploading %s: %v", art.name, err)
			return c.marks
		}
	}
	label := func(prefix, text string) string {
		var s strings.Builder
		for k := 1; k*2 <= len(text)+1; k++ {
			s.WriteString(found[fmt.Sprintf("%s%d", prefix, k)])
		}
		return s.String()
	}
	c.marks = boardMarks{Live: label("otk_an", "AIRING NOW"), Next: label("otk_un", "UP NEXT"), Aired: found["otk_aired"]}
	return c.marks
}
