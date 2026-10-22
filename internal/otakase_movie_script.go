package internal

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strings"

	"github.com/thexykril/otakase/internal/theme"
)

// The film's picture, as one ASS subtitle over the lavfi background. It runs
// in parts, each placed after the last on a single clock, and each part also
// books the sound effects that go with it, so the soundtrack lands on the
// picture's beats:
//
//	presents · logo · I the numbers · II the conversation · III favourites ·
//	IV the crew · the seal

// movieTimeline is when the film's parts happen and what the soundtrack has
// to play under them.
type movieTimeline struct {
	WallStart, WallEnd float64
	End                float64
	Score              movieScore
}

// filmWriter writes the events and keeps the clock and the cues.
type filmWriter struct {
	script  strings.Builder
	palette theme.Palette
	rng     *rand.Rand
	cues    []movieCue
	drums   [][2]float64

	accent, text, muted, second, dark string
}

const (
	filmCentreX = movieWidth / 2
	filmCentreY = movieHeight / 2
)

func (w *filmWriter) event(layer int, start, end float64, style, body string) {
	fmt.Fprintf(&w.script, "Dialogue: %d,%s,%s,%s,,0,0,0,,%s\n", layer, assTime(start), assTime(end), style, body)
}

func (w *filmWriter) cue(at float64, kind string, length float64) {
	w.cues = append(w.cues, movieCue{At: at, Kind: kind, Length: length})
}

// typed shows text at (x, y) as if typed: a clip opening from the left at a
// steady speed, with a key tick every few letters. It returns when typing
// finishes.
func (w *filmWriter) typed(layer int, start, end float64, x, y, size int, colour, text string, ticks bool) float64 {
	runes := len([]rune(text))
	width := int(float64(runes*size) * 0.56)
	left := x - width/2 - 20
	typing := math.Max(0.4, float64(runes)/34)
	w.event(layer, start, end, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs%d\c%s\clip(%d,%d,%d,%d)\t(0,%d,\clip(%d,%d,%d,%d))\fad(0,500)}%s`,
		x, y, size, colour, left, y-size, left, y+size, int(typing*1000), left, y-size, left+width+40, y+size, assText(text)))
	if ticks {
		for i := 0; i < runes; i += 3 {
			w.cue(start+typing*float64(i)/float64(runes), cueTick, 0)
		}
	}
	return start + typing
}

// chapter is a title card between parts: a numeral, a name and a rule
// sweeping under them. It returns when the next part may start.
func (w *filmWriter) chapter(at float64, numeral, name string) float64 {
	end := at + 2.6
	w.cue(at-0.15, cueWhoosh, 0.9)
	w.event(2, at, end, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs26\fsp22\c%s\alpha&HFF&\t(0,300,\alpha&H00&)\fad(0,500)}%s`,
		filmCentreX, filmCentreY-50, w.accent, assText("CHAPTER "+numeral)))
	w.event(2, at+0.2, end, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs64\fsp30\c%s\t(0,1600,\fsp8)\fad(500,500)}%s`,
		filmCentreX, filmCentreY+20, w.text, assText(strings.ToUpper(name))))
	w.event(2, at+0.4, end, "Draw", fmt.Sprintf(`{\an5\pos(%d,%d)\c%s\fscx0\t(0,700,\fscx100)\fad(0,500)\p1}m 0 0 l 520 0 520 3 0 3{\p0}`,
		filmCentreX, filmCentreY+80, w.accent))
	return end + 0.2
}

// presents is the opening line, under a shimmer.
func (w *filmWriter) presents(presenter string) float64 {
	if presenter == "" {
		presenter = "You"
	}
	w.cue(0.7, cueShimmer, 1.6)
	w.event(2, 0.8, 3.9, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs30\fsp14\c%s\t(0,3000,\fsp6)\fad(700,600)}%s`,
		filmCentreX, filmCentreY, w.muted, assText(strings.ToUpper(presenter)+"  presents")))
	return 4.2
}

// logo lands the block-letter logo: each row wipes in from the left, a
// colour-split glitch flickers as it lands, the glow pulses once, and the
// tagline settles beneath a rule.
func (w *filmWriter) logo(at float64, tagline string) float64 {
	end := at + 6.4
	rows := otakaseLogo()
	const size = 34
	lineHeight := size * 122 / 100
	top := filmCentreY - 40 - lineHeight*len(rows)/2 + lineHeight/2
	width := int(float64(len([]rune(rows[0]))*size) * 0.62)
	left := filmCentreX - width/2 - 10

	w.cue(at-1.3, cueRiser, 1.3)
	w.cue(at, cueImpact, 0)
	for i, row := range rows {
		y := top + i*lineHeight
		delay := i * 60
		wipe := fmt.Sprintf(`\clip(%d,%d,%d,%d)\t(%d,%d,\clip(%d,%d,%d,%d))`,
			left, y-lineHeight, left, y+lineHeight, delay, delay+380, left, y-lineHeight, left+width+20, y+lineHeight)
		body := assText(row)
		w.event(3, at, end, "Logo", fmt.Sprintf(`{\an5\pos(%d,%d)%s\fad(0,700)}%s`, filmCentreX, y, wipe, body))
		// The glow behind, swelling once after the landing.
		w.event(2, at, end, "Logo", fmt.Sprintf(`{\an5\pos(%d,%d)%s\blur16\alpha&H90&\t(500,900,\alpha&H40&)\t(900,1900,\alpha&H90&)\fad(0,700)}%s`, filmCentreX, y, wipe, body))
		// The glitch: cyan and red copies pulled apart, flickering out.
		for _, split := range []struct {
			dx     int
			colour string
		}{{-9, assColor(w.palette.Cyan)}, {9, assColor(w.palette.Red)}} {
			w.event(4, at+0.05, at+0.7, "Logo", fmt.Sprintf(`{\an5\pos(%d,%d)\c%s\alpha&H50&\t(0,80,\alpha&HD0&)\t(80,160,\alpha&H60&)\t(160,320,\alpha&HFF&)\t(320,380,\alpha&H80&)\t(380,600,\alpha&HFF&)}%s`,
				filmCentreX+split.dx, y, split.colour, body))
		}
	}
	w.event(3, at+1.1, end, "Draw", fmt.Sprintf(`{\an5\pos(%d,%d)\c%s\fscx0\t(0,800,\fscx100)\fad(0,700)\p1}m 0 0 l 1000 0 1000 4 0 4{\p0}`,
		filmCentreX, filmCentreY+95, w.accent))
	w.event(3, at+1.6, end, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs34\fsp44\c%s\t(0,1400,\fsp18)\fad(700,700)}%s`,
		filmCentreX, filmCentreY+150, w.text, assText(strings.ToUpper(tagline))))
	w.cue(at+1.55, cueShimmer, 1)
	return end + 0.3
}

// statCards counts the numbers up, one card at a time: the label, the
// number racing to its value with the counter ticking, what it counts, and
// the line about it typed in after.
func (w *filmWriter) statCards(at float64, cards []statCard) float64 {
	const hold = 5.0
	for _, card := range cards {
		end := at + hold
		w.cue(at-0.1, cueWhoosh, 0.7)
		w.event(2, at, end, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs28\fsp16\c%s\move(%d,%d,%d,%d,0,400)\fad(300,400)}%s`,
			filmCentreX, filmCentreY-170, w.muted, filmCentreX, filmCentreY-150, filmCentreX, filmCentreY-170, assText(strings.ToUpper(card.Label))))

		// The count: a run of short events, easing out, each a tick.
		const steps = 28
		countFor := 1.5
		for i := 0; i <= steps; i++ {
			p := float64(i) / steps
			value := int(math.Round(float64(card.Value) * (1 - math.Pow(1-p, 3))))
			start := at + 0.3 + countFor*p
			stop := start + countFor/steps
			pop := ""
			if i == steps {
				stop = end
				// The final value lands with a little punch.
				pop = `\fscx112\fscy112\t(0,160,\fscx100\fscy100)\fad(0,400)`
				w.cue(start, cueThud, 0)
			} else if i%2 == 0 {
				w.cue(start, cueTick, 0)
			}
			w.event(3, start, stop, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs150\b1\c%s%s}%s`,
				filmCentreX, filmCentreY-40, w.text, pop, assText(formatStatsCount(value))))
		}
		w.event(3, at+0.3, end, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs150\b1\c%s\blur20\alpha&HB0&\fad(300,400)}%s`,
			filmCentreX, filmCentreY-40, w.accent, assText(formatStatsCount(card.Value))))
		w.event(2, at+1.9, end, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs34\c%s\fad(400,400)}%s`,
			filmCentreX, filmCentreY+70, w.accent, assText(card.Unit)))
		w.typed(2, at+2.4, end, filmCentreX, filmCentreY+150, 32, w.muted, card.Quip, true)
		at = end + 0.2
	}
	return at
}

// poem plays the conversation a few lines at a time, each line typed in the
// colour of its voice, the page fading before the next.
func (w *filmWriter) poem(at float64, lines []poemLine) float64 {
	const (
		perPage = 4
		size    = 38
		gap     = 86
	)
	for first := 0; first < len(lines); first += perPage {
		page := lines[first:min(first+perPage, len(lines))]
		top := filmCentreY - (len(page)-1)*gap/2
		// Lay the page out first, to know when it has all been typed.
		t := at
		starts := make([]float64, len(page))
		for i, line := range page {
			starts[i] = t
			t += math.Max(0.4, float64(len([]rune(line.Text)))/34) + 0.9
		}
		end := t + 1.4
		for i, line := range page {
			colour := w.accent
			if line.Voice == 1 {
				colour = w.second
			}
			w.typed(2, starts[i], end, filmCentreX, top+i*gap, size, colour, line.Text, true)
		}
		at = end + 0.5
	}
	return at
}

// wall is the favourites: the covers come from lavfi (movieFilterGraph);
// this draws the heading, a frame that springs out round each cover as it
// pops in, and its title.
func (w *filmWriter) wall(at float64, wall []movieFavourite) (float64, float64) {
	end := at + movieWallSeconds
	w.event(2, at, end, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs30\fsp14\c%s\fad(600,700)}%s`, filmCentreX, movieCoverTop-62, w.accent, assText("YOUR FAVOURITES")))
	for i, favourite := range wall {
		x := movieCoverX(i, len(wall))
		in := at + 0.6 + float64(i)*0.35
		w.cue(in, cuePop, 0)
		w.event(3, in, end, "Draw", fmt.Sprintf(`{\an5\pos(%d,%d)\1a&HFF&\bord2\3c%s\fscx86\fscy86\t(0,220,\fscx104\fscy104)\t(220,380,\fscx100\fscy100)\fad(200,700)\p1}m 0 0 l %d 0 %d %d 0 %d{\p0}`,
			x+movieCoverWidth/2, movieCoverTop+movieCoverHeight/2, w.accent, movieCoverWidth, movieCoverWidth, movieCoverHeight, movieCoverHeight))
		w.event(2, in+0.2, end, "Text", fmt.Sprintf(`{\an8\pos(%d,%d)\fs22\c%s\fad(500,700)}%s`,
			x+movieCoverWidth/2, movieCoverTop+movieCoverHeight+24, w.text, assText(ellipsize(favourite.Title, 21))))
	}
	return at, end
}

// crew rolls the closing credits: roles right-aligned against the centre,
// names left-aligned after it, rising at a steady speed.
func (w *filmWriter) crew(at float64, credits []creditLine) float64 {
	const (
		rowHeight = 74
		gutter    = 36
		speed     = 130
	)
	height := len(credits) * rowHeight
	end := at + float64(movieHeight+height+80)/speed
	rise := movieHeight + height + 200
	y := movieHeight + 60
	for _, credit := range credits {
		if credit.Role != "" {
			w.event(2, at, end, "Text", fmt.Sprintf(`{\an6\move(%d,%d,%d,%d)\fs30\c%s}%s`,
				filmCentreX-gutter, y, filmCentreX-gutter, y-rise, w.muted, assText(ellipsize(credit.Role, movieValueRunes))))
		}
		w.event(2, at, end, "Text", fmt.Sprintf(`{\an4\move(%d,%d,%d,%d)\fs44\c%s}%s`,
			filmCentreX+gutter, y, filmCentreX+gutter, y-rise, w.text, assText(ellipsize(credit.Name, movieValueRunes))))
		y += rowHeight
	}
	return end
}

// seal stamps 終, "the end", the way anime end cards close: a filled square
// in the accent with the character cut out of it, slammed down with a thud
// and a bell, the ink spreading, OTAKASE and a goodbye beneath.
func (w *filmWriter) seal(at float64, farewell string) float64 {
	end := at + movieEndCardSeconds
	stamp := at + 1.2
	y := filmCentreY - 70
	w.cue(stamp-1.1, cueRiser, 1.1)
	w.cue(stamp, cueThud, 0)
	w.cue(stamp, cueImpact, 0)
	w.cue(stamp+0.05, cueBell, 0)
	land := `\frz-6\fscx260\fscy260\alpha&HFF&\t(0,40,\alpha&H00&)\t(0,170,\fscx100\fscy100)\fad(0,1200)`
	w.event(3, stamp, end, "Draw", fmt.Sprintf(`{\an5\pos(%d,%d)\c%s\blur0.8%s\p1}m 12 0 l 318 0 b 325 0 330 5 330 12 l 330 318 b 330 325 325 330 318 330 l 12 330 b 5 330 0 325 0 318 l 0 12 b 0 5 5 0 12 0{\p0}`,
		filmCentreX, y, w.accent, land))
	w.event(4, stamp, end, "Seal", fmt.Sprintf(`{\pos(%d,%d)\c%s%s}終`, filmCentreX, y+6, w.dark, land))
	// The shake of the landing, felt in a ring of ink spreading out.
	w.event(2, stamp+0.12, end, "Draw", fmt.Sprintf(`{\an5\pos(%d,%d)\c%s\alpha&HA0&\blur22\frz-6\t(0,700,\fscx124\fscy124\alpha&HF0&)\fad(0,1200)\p1}m 0 0 l 330 0 330 330 0 330{\p0}`,
		filmCentreX, y, w.accent))
	w.event(2, stamp+0.9, end, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs30\fsp30\c%s\t(0,1500,\fsp20)\fad(700,1200)}%s`, filmCentreX, filmCentreY+170, w.text, assText("OTAKASE")))
	w.event(2, stamp+1.5, end, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs26\c%s\fad(700,1200)}%s`, filmCentreX, filmCentreY+222, w.muted, assText(farewell)))
	return end
}

// petals drift down through the whole film, behind everything.
func (w *filmWriter) petals(end float64) {
	petals := rand.New(rand.NewPCG(4, 12))
	for i := 0; i < int(end/2.4); i++ {
		start := petals.Float64() * (end - 4)
		life := 11 + petals.Float64()*8
		x := petals.IntN(movieWidth)
		drift := petals.IntN(500) - 150
		scale := 60 + petals.IntN(80)
		spin := petals.IntN(720) - 360
		w.event(0, start, math.Min(end, start+life), "Draw", fmt.Sprintf(`{\an5\move(%d,%d,%d,%d)\fscx%d\fscy%d\frz%d\t(\frz%d)\c%s\alpha&H%02X&\blur1.5\fad(800,800)\p1}m 0 0 b 10 -14 26 -10 30 0 b 26 10 10 14 0 0{\p0}`,
			x, movieBars-40, x+drift, movieHeight-movieBars+40, scale, scale, petals.IntN(360), spin, w.accent, 0x70+petals.IntN(0x50)))
	}
}

// movieScript writes the whole film's picture, less the covers themselves,
// and when its parts happen.
func movieScript(facts movieFacts, tagline string, wall []movieFavourite, palette theme.Palette, logoFont, textFont string, rng *rand.Rand) (string, movieTimeline) {
	w := &filmWriter{
		palette: palette, rng: rng,
		accent: assColor(palette.Accent), text: assColor(palette.BrightForeground), muted: assColor(palette.Muted),
		second: assColor(palette.Cyan), dark: assColor(palette.Background),
	}
	fmt.Fprintf(&w.script, "[Script Info]\nScriptType: v4.00+\nPlayResX: %d\nPlayResY: %d\nWrapStyle: 2\nScaledBorderAndShadow: yes\n\n", movieWidth, movieHeight)
	w.script.WriteString("[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	fmt.Fprintf(&w.script, "Style: Logo,%s,34,%s,%s,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,5,0,0,0,1\n", logoFont, w.accent, w.accent)
	fmt.Fprintf(&w.script, "Style: Text,%s,44,%s,%s,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,5,0,0,0,1\n", textFont, w.text, w.text)
	fmt.Fprintf(&w.script, "Style: Seal,serif,250,%s,%s,&H00000000,&H00000000,1,0,0,0,100,100,0,0,1,0,0,5,0,0,0,1\n", w.accent, w.accent)
	w.script.WriteString("Style: Draw,Arial,20,&H00FFFFFF,&H00FFFFFF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,7,0,0,0,1\n\n")
	w.script.WriteString("[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")

	var timeline movieTimeline
	at := w.presents(facts.Viewer)
	at = w.logo(at+0.2, tagline)

	numerals := []string{"I", "II", "III", "IV"}
	chapter := 0
	next := func() string { chapter++; return numerals[chapter-1] }

	if cards := movieStatCards(facts, rng); len(cards) > 0 {
		start := w.chapter(at, next(), "The numbers")
		at = w.statCards(start, cards)
		w.drums = append(w.drums, [2]float64{start - 0.4, at})
	}
	at = w.poem(w.chapter(at, next(), "The conversation"), moviePoem(facts, rng))
	timeline.WallStart, timeline.WallEnd = at, at
	if len(wall) > 0 {
		start := w.chapter(at, next(), "Your favourites")
		timeline.WallStart, timeline.WallEnd = w.wall(start, wall)
		w.drums = append(w.drums, [2]float64{start - 0.4, timeline.WallEnd})
		at = timeline.WallEnd + 0.4
	}
	start := w.chapter(at, next(), "The crew")
	at = w.crew(start, movieCrew(facts, rng))
	w.drums = append(w.drums, [2]float64{start - 0.4, at})
	timeline.End = w.seal(at, movieFarewell(rng))

	w.petals(timeline.End)
	// Letterbox bars, over everything.
	bar := fmt.Sprintf(`m 0 0 l %d 0 %d %d 0 %d`, movieWidth, movieWidth, movieBars, movieBars)
	w.event(9, 0, timeline.End, "Draw", `{\pos(0,0)\c&H000000&\p1}`+bar+`{\p0}`)
	w.event(9, 0, timeline.End, "Draw", fmt.Sprintf(`{\pos(0,%d)\c&H000000&\p1}%s{\p0}`, movieHeight-movieBars, bar))

	timeline.Score = movieScore{Length: timeline.End, Drums: w.drums, Cues: w.cues}
	return w.script.String(), timeline
}
