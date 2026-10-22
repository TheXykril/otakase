package internal

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"

	"github.com/thexykril/otakase/internal/theme"
)

// The film's picture, as one ASS subtitle over the lavfi background. It runs
// in parts, each placed after the last on a single clock, and each part also
// books the sound effects that go with it, so the soundtrack lands on the
// picture's beats:
//
//	presents · logo · I counting the stars (the numbers) · II the voices
//	(the conversation) · III memories that never fade (favourites) ·
//	IV legendary conquests (achievements) · the journey continues (next
//	time) · the seal

// movieTimeline is when the film's parts happen and what the soundtrack has
// to play under them.
type movieTimeline struct {
	// Pictures are where and when each of the wall's covers shows.
	Pictures []moviePicture
	End      float64
	Score    movieScore
}

// filmWriter writes the events and keeps the clock and the cues.
type filmWriter struct {
	script  strings.Builder
	palette theme.Palette
	rng     *rand.Rand
	cues    []movieCue
	drums   [][2]float64
	images  []moviePicture

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
				// The landing sends a ring out behind the number.
				w.event(1, start, start+1.6, "Draw", fmt.Sprintf(`{\an5\pos(%d,%d)\1a&HFF&\bord3\3c%s\3a&H60&\fscx30\fscy30\t(0,1600,\fscx260\fscy260\bord1\3a&HFF&)\p1}%s{\p0}`,
					filmCentreX, filmCentreY-40, w.accent, circleShape(160)))
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

// poem plays the conversation under a night sky. The two voices are two
// lights, one either side, that brighten while they speak; each line writes
// itself on word by word in its voice's colour with a soft glow, and the
// lines before it rise and dim, so the talk scrolls like a conversation
// rather than turning pages. It returns when the last line has faded.
func (w *filmWriter) poem(at float64, lines []poemLine) float64 {
	const (
		bottom  = filmCentreY + 150
		gap     = 92
		visible = 4
	)
	// When each line arrives: after the one before has been written and
	// had time to be read.
	arrive := make([]float64, len(lines)+1)
	writing := make([]float64, len(lines))
	t := at + 0.6
	for i, line := range lines {
		arrive[i] = t
		runes := float64(len([]rune(line.Text)))
		writing[i] = math.Max(0.6, runes/26)
		t += writing[i] + 1.0 + runes/55
	}
	arrive[len(lines)] = t
	end := t + 1.2

	w.nightSky(at, end)
	// Each voice's words are the colour of its light: a warm rose for the
	// first, cyan for the second.
	colours := []string{assColor(mixColor(w.palette.Accent, w.palette.BrightForeground, 0.35)), w.second}
	voiceX := []int{filmCentreX - 800, filmCentreX + 800}
	speaking := [2][][3]float64{}
	for i, line := range lines {
		speaking[line.Voice%2] = append(speaking[line.Voice%2], [3]float64{arrive[i], arrive[i] + writing[i], float64(i)})
	}
	for voice := range 2 {
		var words []float64
		for _, span := range speaking[voice] {
			words = append(words, wordStarts(lines[int(span[2])].Text, span[0], writing[int(span[2])])...)
		}
		w.voiceLight(voiceX[voice], filmCentreY, colours[voice], 1-2*voice, at, end, speaking[voice], words)
	}

	alphas := []int{0x00, 0x60, 0xA0, 0xD0}
	for i, line := range lines {
		voice := line.Voice % 2
		glow := colours[voice]
		runes := len([]rune(line.Text))
		size := 50
		// Long lines shrink to keep clear of the lights.
		if runes > 50 {
			size = max(34, 50*50/runes)
		}
		// The line itself: written on word by word, then rising and dimming
		// a step each time a new line arrives, gone after four.
		karaoke := writeOn(line.Text, writing[i])
		for step := 0; step < visible; step++ {
			k := i + step
			if k >= len(lines) && step > 0 {
				break
			}
			start := arrive[k]
			stop := arrive[min(k+1, len(lines))]
			fade := ""
			last := k >= len(lines)-1 || step == visible-1
			if last {
				stop = math.Max(stop, start+0.6)
				if k == len(lines)-1 {
					stop = end
				}
				fade = `\fad(0,600)`
			}
			y := bottom - step*gap
			from, fromAlpha := y+24, 0xFF
			if step > 0 {
				from, fromAlpha = y+gap, alphas[step-1]
			}
			// moving rises the line into place while its alpha settles; the
			// glow behind follows at a fainter alpha.
			moving := func(alpha func(int) int) string {
				return fmt.Sprintf(`\an5\move(%d,%d,%d,%d,0,600)\fs%d\alpha&H%02X&\t(0,600,\alpha&H%02X&)%s`,
					filmCentreX, from, filmCentreX, y, size, alpha(fromAlpha), alpha(alphas[step]), fade)
			}
			solid := func(a int) int { return a }
			faint := func(a int) int { return min(0xFF, 0xA0+a/2) }
			if step == 0 {
				// Written on: the fill sweeps each word in turn (the unfilled
				// part is the transparent secondary colour), the glow fading
				// up behind it.
				rise := fmt.Sprintf(`\an5\move(%d,%d,%d,%d,0,600)\fs%d%s`, filmCentreX, from, filmCentreX, y, size, fade)
				w.event(4, start, stop, "Voice", fmt.Sprintf(`{%s\c%s\2a&HFF&\blur0.6}`, rise, colours[voice])+karaoke)
				w.event(3, start, stop, "Voice", fmt.Sprintf(`{%s\c%s\blur10\alpha&HFF&\t(%d,%d,\alpha&HA0&)}`, rise, glow, int(writing[i]*1000), int(writing[i]*1000)+700)+assText(line.Text))
				continue
			}
			w.event(4, start, stop, "Voice", fmt.Sprintf(`{%s\c%s\blur0.6}`, moving(solid), colours[voice])+assText(line.Text))
			w.event(3, start, stop, "Voice", fmt.Sprintf(`{%s\c%s\blur10}`, moving(faint), glow)+assText(line.Text))
		}
	}
	return end
}

// voiceLight is one voice of the conversation: a small bright core in a
// soft halo, a ring of motes slowly circling it, all breathing quietly the
// whole time. While the voice speaks the core swells and the halo
// brightens, and every word it says sends a ripple out. spin is 1 or -1,
// so the two voices turn opposite ways; speaking holds [start, end, line]
// for each of its lines, words when each of its words starts.
func (w *filmWriter) voiceLight(x, y int, colour string, spin int, start, end float64, speaking [][3]float64, words []float64) {
	const motes, orbit = 14, 92
	span := int((end - start) * 1000)
	fade := `\fad(1500,1500)`
	at := func(layer int, from, to float64, tags, shape string) {
		w.event(layer, from, to, "Draw", fmt.Sprintf(`{\an5\pos(%d,%d)\c%s%s\p1}%s{\p0}`, x, y, colour, tags, shape))
	}
	// The halo: rings of fainter light standing in for a gradient.
	for ring, halo := range []struct{ radius, alpha int }{{22, 0xC0}, {48, 0xDC}, {80, 0xEE}} {
		at(1, start, end, fmt.Sprintf(`\alpha&H%02X&\blur%d%s`, halo.alpha, 12+ring*10, fade), circleShape(halo.radius))
	}
	at(2, start, end, `\alpha&H10&\blur2.5`+fade, circleShape(8))
	// The motes, circling.
	var ring strings.Builder
	for i := range motes {
		angle := 2 * math.Pi * float64(i) / motes
		size := 2 + i%2
		fmt.Fprintf(&ring, "%s ", circleAt(orbit+3+int(math.Round(orbit*math.Cos(angle))), orbit+3+int(math.Round(orbit*math.Sin(angle))), size))
	}
	w.event(2, start, end, "Draw", fmt.Sprintf(`{\an5\pos(%d,%d)\org(%d,%d)\c%s\alpha&H80&\blur1\t(0,%d,\frz%d)%s\p1}%s{\p0}`,
		x, y, x, y, colour, span, spin*int(float64(span)/1000*14), fade, ring.String()))
	at(1, start, end, `\1a&HFF&\bord1\3a&HD8&\blur1`+fade, circleShape(orbit))

	for _, talk := range speaking {
		talking := int((talk[1]-talk[0])*1000) + 400
		at(2, talk[0], talk[1]+1, fmt.Sprintf(`\alpha&HFF&\blur32\t(0,300,\alpha&HC0&)\t(%d,%d,\alpha&HFF&)`, talking, talking+600), circleShape(60))
		at(3, talk[0], talk[1]+1, fmt.Sprintf(`\alpha&HFF&\blur3\t(0,250,\alpha&H00&)\t(%d,%d,\alpha&HFF&)`, talking, talking+600), circleShape(14))
	}
	for _, word := range words {
		at(1, word, word+1.2, `\1a&HFF&\bord2\3a&H70&\blur1\fscx100\fscy100\t(0,1200,\fscx300\fscy300\3a&HFF&)`, circleShape(40))
	}
}

// wordStarts is when each word of text begins, written on over seconds
// from start the way writeOn spreads it.
func wordStarts(text string, start, seconds float64) []float64 {
	runes := float64(max(len([]rune(text)), 1))
	var starts []float64
	t := start
	for j, word := range strings.Fields(text) {
		starts = append(starts, t)
		width := len([]rune(word))
		if j > 0 {
			width++
		}
		t += seconds * float64(width) / runes
	}
	return starts
}

// writeOn is text that fills in word by word over seconds, ASS karaoke,
// for an event whose secondary colour is transparent.
func writeOn(text string, seconds float64) string {
	runes := len([]rune(text))
	var out strings.Builder
	for j, word := range strings.Fields(text) {
		if j > 0 {
			word = " " + word
		}
		share := seconds * float64(len([]rune(word))) / float64(max(runes, 1))
		fmt.Fprintf(&out, `{\kf%d}%s`, int(math.Round(share*100)), assText(word))
	}
	return out.String()
}

// nightSky is the backdrop the voices talk under: a slow field of stars
// twinkling and drifting a little.
func (w *filmWriter) nightSky(start, end float64) {
	r := rand.New(rand.NewPCG(9, 9))
	drift := int((end - start) * 6)
	for range 150 {
		x, y := r.IntN(movieWidth), movieBars+r.IntN(movieHeight-2*movieBars)
		bright := 0x50 + r.IntN(0x70)
		period := 1500 + r.IntN(3000)
		var twinkle strings.Builder
		for t := r.IntN(period); t < int((end-start)*1000); t += period {
			fmt.Fprintf(&twinkle, `\t(%d,%d,\alpha&HE0&)\t(%d,%d,\alpha&H%02X&)`, t, t+period/2, t+period/2, t+period, bright)
		}
		w.event(0, start+r.Float64()*1.5, end, "Draw", fmt.Sprintf(`{\an5\move(%d,%d,%d,%d)\c%s\alpha&H%02X&\blur0.8%s\fad(1500,1500)\p1}%s{\p0}`,
			x, y, x-drift, y, w.text, bright, twinkle.String(), circleShape(1+r.IntN(3))))
	}
}

// wall is the favourites: the covers come from lavfi (movieFilterGraph);
// this draws the heading, a frame that springs out round each cover as it
// pops in, and its title.
func (w *filmWriter) wall(at float64, wall []movieFavourite) float64 {
	end := at + movieWallSeconds
	w.event(2, at, end, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs30\fsp14\c%s\fad(600,700)}%s`, filmCentreX, movieCoverTop-62, w.accent, assText("YOUR FAVOURITES")))
	for i, favourite := range wall {
		x := movieCoverX(i, len(wall))
		in := at + 0.6 + float64(i)*0.35
		w.images = append(w.images, moviePicture{X: x, Y: movieCoverTop, Width: movieCoverWidth, Height: movieCoverHeight, In: in, Out: end - 0.1})
		w.cue(in, cuePop, 0)
		w.event(3, in, end, "Draw", fmt.Sprintf(`{\an5\pos(%d,%d)\1a&HFF&\bord2\3c%s\fscx86\fscy86\t(0,220,\fscx104\fscy104)\t(220,380,\fscx100\fscy100)\fad(200,700)\p1}m 0 0 l %d 0 %d %d 0 %d{\p0}`,
			x+movieCoverWidth/2, movieCoverTop+movieCoverHeight/2, w.accent, movieCoverWidth, movieCoverWidth, movieCoverHeight, movieCoverHeight))
		w.event(2, in+0.2, end, "Text", fmt.Sprintf(`{\an8\pos(%d,%d)\fs22\c%s\fad(500,700)}%s`,
			x+movieCoverWidth/2, movieCoverTop+movieCoverHeight+24, w.text, assText(ellipsize(favourite.Title, 21))))
	}
	return end
}

// achievements pops the viewer's badges into a grid, two columns, one at a
// time: a card sliding in with a bounce, a medal with the badge's initial,
// its name and what earned it. Rare badges are gold and ring a bell.
func (w *filmWriter) achievements(at float64, badges []achievement) float64 {
	const (
		cardWidth  = 780
		cardHeight = 124
		columnGap  = 40
		rowGap     = 26
		medal      = 44
	)
	rows := (len(badges) + 1) / 2
	gridTop := filmCentreY + 40 - (rows*cardHeight+(rows-1)*rowGap)/2
	end := at + 1.6 + float64(len(badges))*0.75 + 3.4
	gold := assColor(w.palette.Yellow)

	w.event(2, at, end, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs30\fsp14\c%s\fad(500,700)}%s`, filmCentreX, gridTop-70, w.muted, assText("ACHIEVEMENTS UNLOCKED")))
	for i, badge := range badges {
		in := at + 0.8 + float64(i)*0.75
		column, row := i%2, i/2
		x := filmCentreX - columnGap/2 - cardWidth/2
		if column == 1 {
			x = filmCentreX + columnGap/2 + cardWidth/2
		}
		if len(badges)%2 == 1 && i == len(badges)-1 {
			x = filmCentreX
		}
		y := gridTop + row*(cardHeight+rowGap) + cardHeight/2
		colour, ring := w.muted, w.accent
		if badge.Rare {
			colour, ring = gold, gold
			w.cue(in+0.05, cueBell, 0)
			w.cue(in, cueShimmer, 0.8)
		}
		w.cue(in, cuePop, 0)
		slide := fmt.Sprintf(`\move(%d,%d,%d,%d,0,260)\alpha&HFF&\t(0,200,\alpha&H00&)\fad(0,600)`, x+60, y, x, y)
		card := fmt.Sprintf(`m 0 0 l %d 0 %d %d 0 %d`, cardWidth, cardWidth, cardHeight, cardHeight)
		w.event(2, in, end, "Draw", fmt.Sprintf(`{\an5%s\c%s\1a&HD8&\bord2\3c%s\p1}%s{\p0}`, slide, w.dark, colour, card))
		if badge.Rare {
			// A gold sheen sweeping across the card once.
			w.event(3, in+0.4, in+1.4, "Draw", fmt.Sprintf(`{\an5\move(%d,%d,%d,%d)\c%s\alpha&HC0&\blur12\frz-20\clip(%d,%d,%d,%d)\p1}m 0 0 l 40 0 40 %d 0 %d{\p0}`,
				x-cardWidth/2, y, x+cardWidth/2, y, gold, x-cardWidth/2, y-cardHeight/2, x+cardWidth/2, y+cardHeight/2, cardHeight*2, cardHeight*2))
		}
		medalX := x - cardWidth/2 + 30 + medal
		bounce := `\fscx40\fscy40\t(120,300,\fscx112\fscy112)\t(300,420,\fscx100\fscy100)\fad(0,600)`
		w.event(3, in, end, "Draw", fmt.Sprintf(`{\an5\pos(%d,%d)\c%s\1a&H30&\bord3\3c%s%s\p1}%s{\p0}`, medalX, y, w.dark, ring, bounce, circleShape(medal)))
		initial := string([]rune(badge.Name)[0])
		w.event(4, in, end, "Text", fmt.Sprintf(`{\an5\pos(%d,%d)\fs44\b1\c%s%s}%s`, medalX, y, ring, bounce, assText(initial)))
		textX := medalX + medal + 30
		w.event(3, in+0.1, end, "Text", fmt.Sprintf(`{\an1\pos(%d,%d)\fs40\c%s\fad(250,600)}%s`, textX, y+2, w.text, assText(badge.Name)))
		w.event(3, in+0.2, end, "Text", fmt.Sprintf(`{\an7\pos(%d,%d)\fs26\c%s\fad(250,600)}%s`, textX, y+10, colour, assText(ellipsize(badge.Detail, 40))))
	}
	return end
}

// nextTime is the "next episode" preview anime close on, framed like a
// viewfinder: corner marks closing in, the show the viewer is in the middle
// of written on large with a rule sweeping under it, the episode they are up
// to, and a teaser in the voices' hand, with 次回予告 ("next time") standing
// down the right side.
func (w *filmWriter) nextTime(at float64, title, episode, teaser string) float64 {
	const (
		frameWidth  = 1240
		frameHeight = 560
		arm         = 70
	)
	end := at + 8
	w.cue(at, cueWhoosh, 0.8)
	w.cue(at+0.9, cueShimmer, 1.2)
	w.cue(at+2.1, cuePop, 0)

	// The corner marks, closing in from a little wider. Each is an L drawn
	// in its own box, the box's top left at (left, top).
	for _, corner := range [][2]int{{-1, -1}, {1, -1}, {-1, 1}, {1, 1}} {
		left := filmCentreX + corner[0]*frameWidth/2
		top := filmCentreY + corner[1]*frameHeight/2
		barY, barX := 0, 0
		if corner[0] == 1 {
			left -= arm
			barX = arm - 3
		}
		if corner[1] == 1 {
			top -= arm
			barY = arm - 3
		}
		mark := fmt.Sprintf(`m 0 %d l %d %d %d %d 0 %d m %d 0 l %d 0 %d %d %d %d`,
			barY, arm, barY, arm, barY+3, barY+3, barX, barX+3, barX+3, arm, barX, arm)
		w.event(2, at, end, "Draw", fmt.Sprintf(`{\an7\move(%d,%d,%d,%d,0,700)\c%s\fad(400,700)\p1}%s{\p0}`,
			left+corner[0]*40, top+corner[1]*40, left, top, w.muted, mark))
	}
	w.event(2, at+0.3, end, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs28\fsp18\c%s\t(0,1600,\fsp10)\fad(600,700)}%s`,
		filmCentreX, filmCentreY-170, w.accent, assText("THE JOURNEY CONTINUES")))
	for i, kanji := range []rune("次回予告") {
		w.event(2, at+0.5+float64(i)*0.12, end, "Seal", fmt.Sprintf(`{\an5\pos(%d,%d)\fs52\b0\c%s\alpha&HB0&\fad(400,700)}%c`,
			filmCentreX+frameWidth/2-60, filmCentreY-150+i*68, w.muted, kanji))
	}
	// The title, written on, with its glow.
	write := math.Max(0.8, float64(len([]rune(title)))/22)
	name := ellipsize(title, 30)
	w.event(4, at+0.9, end, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs78\b1\c%s\2a&HFF&\fad(0,700)}`, filmCentreX, filmCentreY-60, w.text)+writeOn(name, write))
	w.event(3, at+0.9, end, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs78\b1\c%s\blur14\alpha&HFF&\t(%d,%d,\alpha&HA8&)\fad(0,700)}%s`,
		filmCentreX, filmCentreY-60, w.accent, int(write*500), int(write*1000)+400, assText(name)))
	w.event(2, at+1.2, end, "Draw", fmt.Sprintf(`{\an5\pos(%d,%d)\c%s\fscx0\t(0,900,\fscx100)\fad(0,700)\p1}m 0 0 l 640 0 640 3 0 3{\p0}`,
		filmCentreX, filmCentreY+10, w.accent))
	w.event(2, at+2.1, end, "Text", fmt.Sprintf(`{\pos(%d,%d)\fs34\fsp22\c%s\fscx80\fscy80\t(0,200,\fscx100\fscy100)\fad(300,700)}%s`,
		filmCentreX, filmCentreY+62, w.text, assText(strings.ToUpper(episode))))
	w.event(2, at+2.8, end, "Voice", fmt.Sprintf(`{\pos(%d,%d)\fs40\c%s\2a&HFF&\fad(0,700)}`, filmCentreX, filmCentreY+160, w.second)+writeOn(teaser, math.Max(0.8, float64(len([]rune(teaser)))/26)))
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

// petals drift through the film on one gentle wind from the left, at two
// depths: small, dim and slow far away, larger and tumbling nearer. None
// fall through a skipped span: the night sky, so the voices talk under
// stars only, and the wall, since the covers sit under the subtitle and a
// petal there would cross in front of them. It
// uses its own random source so the film's backdrop is the same however its
// story varies.
func (w *filmWriter) petals(end float64, skip ...[2]float64) {
	r := rand.New(rand.NewPCG(4, 12))
	petal := `m 0 0 b 10 -14 26 -10 30 0 b 26 10 10 14 0 0`
	layers := []struct {
		every, life, blur float64
		scale, spread     int
		alpha             int
	}{
		{every: 1.6, life: 18, blur: 2.5, scale: 30, spread: 25, alpha: 0xC0},
		{every: 3.0, life: 12, blur: 1.2, scale: 70, spread: 60, alpha: 0x70},
	}
	for depth, layer := range layers {
		for i := 0; i < int(end/layer.every); i++ {
			start := r.Float64() * (end - 4)
			life := layer.life * (0.85 + r.Float64()*0.3)
			if slices.ContainsFunc(skip, func(span [2]float64) bool { return start+life > span[0]-1 && start < span[1] }) {
				continue
			}
			x := r.IntN(movieWidth+400) - 400
			scale := layer.scale + r.IntN(layer.spread)
			// Wind from the left: every petal drifts right as it falls.
			wind := 260 + depth*200 + r.IntN(120)
			w.event(1, start, math.Min(end, start+life), "Draw", fmt.Sprintf(`{\an5\move(%d,%d,%d,%d)\fscx%d\fscy%d\frz%d\t(\frz%d\frx%d)\c%s\alpha&H%02X&\blur%.1f\fad(1000,1000)\p1}%s{\p0}`,
				x, movieBars-40, x+wind, movieHeight-movieBars+40, scale, scale, r.IntN(360), r.IntN(240)-300, 360*(depth+1), w.accent, layer.alpha+r.IntN(0x20), layer.blur, petal))
		}
	}
}

// spotlight is a soft cone of light from above, behind the favourites wall.
func (w *filmWriter) spotlight(start, end float64) {
	w.event(0, start, end, "Draw", fmt.Sprintf(`{\an8\pos(%d,%d)\c%s\alpha&HF4&\blur60\fad(900,900)\p1}m 380 0 l 620 0 1000 820 0 820{\p0}`,
		filmCentreX, movieBars-40, w.text))
}

// rays are slow beams of gold light turning behind the achievements.
func (w *filmWriter) rays(start, end float64) {
	gold := assColor(w.palette.Yellow)
	for i := range 8 {
		angle := i * 45
		w.event(0, start, end, "Draw", fmt.Sprintf(`{\an7\org(%d,%d)\pos(%d,%d)\frz%d\t(\frz%d)\c%s\alpha&HF2&\blur24\fad(1200,1200)\p1}m 0 0 l 1400 -90 1400 90{\p0}`,
			filmCentreX, filmCentreY+40, filmCentreX, filmCentreY+40, angle, angle+40, gold))
	}
}

// circleAt draws a circle of radius r centred on (cx, cy), all in positive
// coordinates.
func circleAt(cx, cy, r int) string {
	k := int(math.Round(float64(r) * 0.5523))
	return fmt.Sprintf("m %d %d b %d %d %d %d %d %d b %d %d %d %d %d %d b %d %d %d %d %d %d b %d %d %d %d %d %d",
		cx, cy-r, cx+k, cy-r, cx+r, cy-k, cx+r, cy, cx+r, cy+k, cx+k, cy+r, cx, cy+r, cx-k, cy+r, cx-r, cy+k, cx-r, cy, cx-r, cy-k, cx-k, cy-r, cx, cy-r)
}

// circleShape draws a circle of radius r inside the box from the origin to
// (2r, 2r), which libass centres on \pos under \an5.
func circleShape(r int) string {
	k := int(math.Round(float64(r) * 0.5523))
	return fmt.Sprintf("m %d 0 b %d 0 %d %d %d %d b %d %d %d %d %d %d b %d %d 0 %d 0 %d b 0 %d %d 0 %d 0",
		r, r+k, 2*r, r-k, 2*r, r, 2*r, r+k, r+k, 2*r, r, 2*r, r-k, 2*r, r+k, r, r-k, r-k, r)
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
	fmt.Fprintf(&w.script, "Style: Voice,serif,50,%s,%s,&H00000000,&H00000000,0,1,0,0,100,100,1,0,1,0,0,5,0,0,0,1\n", w.text, w.text)
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
		start := w.chapter(at, next(), "Counting the Stars")
		at = w.statCards(start, cards)
		w.drums = append(w.drums, [2]float64{start - 0.4, at})
	}
	voicesStart := w.chapter(at, next(), "The Voices")
	at = w.poem(voicesStart, moviePoem(facts, rng))
	sky := [2]float64{voicesStart, at}
	var wallSpan [2]float64
	if len(wall) > 0 {
		start := w.chapter(at, next(), "Memories That Never Fade")
		at = w.wall(start, wall)
		w.spotlight(start, at)
		wallSpan = [2]float64{start, at}
		w.drums = append(w.drums, [2]float64{start - 0.4, at})
		at += 0.4
	}
	start := w.chapter(at, next(), "Legendary Conquests")
	at = w.achievements(start, movieAchievements(facts, rng))
	w.rays(start, at)
	w.drums = append(w.drums, [2]float64{start - 0.4, at})
	title, episode, teaser := movieNextTime(facts, rng)
	at = w.nextTime(at+0.4, title, episode, teaser)
	timeline.End = w.seal(at+0.2, movieFarewell(rng))

	w.petals(timeline.End, sky, wallSpan)
	// Letterbox bars, over everything.
	bar := fmt.Sprintf(`m 0 0 l %d 0 %d %d 0 %d`, movieWidth, movieWidth, movieBars, movieBars)
	w.event(9, 0, timeline.End, "Draw", `{\pos(0,0)\c&H000000&\p1}`+bar+`{\p0}`)
	w.event(9, 0, timeline.End, "Draw", fmt.Sprintf(`{\pos(0,%d)\c&H000000&\p1}%s{\p0}`, movieHeight-movieBars, bar))

	timeline.Pictures = w.images
	timeline.Score = movieScore{Length: timeline.End, Drums: w.drums, Cues: w.cues}
	return w.script.String(), timeline
}
