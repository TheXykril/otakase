package internal

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand/v2"
)

// The film's soundtrack is written for it: a dark lo-fi piece in the games
// Activity's style (soft electric piano, a low drone, a round bass, brushed
// half-time drums, vinyl, a dark room) but its own tune, in E minor at 66 BPM.
// It follows the film: the drums only play through the busy chapters and drop
// out for the conversation, and the picture's moments get their own sound
// effects (movieCue): whooshes between scenes, an impact as the logo lands,
// ticks as numbers count and words type, pops as covers appear, a bell and a
// thud for the closing seal.
//
// Nothing is shipped. The whole thing is rendered offline in a moment, into a
// temporary WAV that mpv plays beside the picture.

const (
	movieSampleRate = 32000

	musicBeat   = 60.0 / 66
	musicEighth = musicBeat / 2
	// musicSwing pushes the off-beats late, as a share of a beat.
	musicSwing = 0.11
)

type musicChord struct {
	bass int
	keys [4]int
}

// musicChords is Em9, Cmaj7(#11), G6, D/F#.
var musicChords = []musicChord{
	{40, [4]int{55, 59, 62, 66}},
	{36, [4]int{52, 55, 59, 66}},
	{43, [4]int{55, 59, 62, 64}},
	{42, [4]int{54, 57, 62, 64}},
}

// musicMelody is, per bar of the four, the eighth a note starts on and the
// note: a slow line up and down the E minor pentatonic.
var musicMelody = [][][2]int{
	{{0, 71}, {2, 74}, {4, 76}, {6, 74}},
	{{1, 71}, {4, 67}, {5, 69}},
	{{0, 72}, {3, 71}, {4, 69}, {6, 67}},
	{{0, 66}, {4, 69}, {6, 71}},
}

// Half-time: the kick on one and the and-of-two, a rim on three.
var (
	musicKick = [8]bool{true, false, false, true, false, false, false, false}
	musicRim  = [8]bool{false, false, false, false, true, false, false, false}
)

// movieCue is a sound effect at a moment of the picture.
type movieCue struct {
	At     float64
	Kind   string
	Length float64
}

const (
	cueWhoosh  = "whoosh"
	cueImpact  = "impact"
	cueRiser   = "riser"
	cueShimmer = "shimmer"
	cueTick    = "tick"
	cuePop     = "pop"
	cueBell    = "bell"
	cueThud    = "thud"
)

// movieScore is what the soundtrack needs to know about the film.
type movieScore struct {
	Length float64
	// Drums are the stretches the beat plays through.
	Drums [][2]float64
	Cues  []movieCue
}

func midiHz(note int) float64 { return 440 * math.Pow(2, float64(note-69)/12) }

// musicMix is the buffers sound is written into: the music bus, the echo's
// send, and the effects, which skip the music's low-pass so they stay crisp.
type musicMix struct {
	bus, echo, fx []float64
	noise         *rand.Rand
	// curves keeps each envelope shape once worked out: thousands of notes
	// share a handful of shapes, and math.Pow per sample was most of the time
	// the score took to render.
	curves map[[3]float64][]float64
}

// curve is envelope(t, attack, length, peak) for every sample of length.
func (m *musicMix) curve(attack, length, peak float64) []float64 {
	key := [3]float64{attack, length, peak}
	if c, ok := m.curves[key]; ok {
		return c
	}
	c := make([]float64, samples(length)+1)
	for i := range c {
		c[i] = envelope(float64(i)/movieSampleRate, attack, length, peak)
	}
	m.curves[key] = c
	return c
}

// curveAt reads a curve at sample i, silence past its end.
func curveAt(curve []float64, i int) float64 {
	if i < len(curve) {
		return curve[i]
	}
	return 0
}

func (m *musicMix) index(at float64) int { return int(at * movieSampleRate) }

// write adds v to buffer at sample i, ignoring anything past either end.
func write(buffer []float64, i int, v float64) {
	if i >= 0 && i < len(buffer) {
		buffer[i] += v
	}
}

// envelope is the Web Audio shape the loop uses: an exponential rise from
// silence to peak over attack, then a fall back to silence by length.
func envelope(t, attack, length, peak float64) float64 {
	const floor = 0.0001
	switch {
	case t < 0 || t > length:
		return 0
	case t < attack:
		return floor * math.Pow(peak/floor, t/attack)
	default:
		return peak * math.Pow(floor/peak, (t-attack)/(length-attack))
	}
}

func triangle(phase float64) float64 {
	phase -= math.Floor(phase)
	return 4*math.Abs(phase-0.5) - 1
}

func sawtooth(phase float64) float64 {
	phase -= math.Floor(phase)
	return 2*phase - 1
}

func cents(value float64) float64 { return math.Pow(2, value/1200) }

func samples(seconds float64) int { return int(seconds * movieSampleRate) }

// keys is one electric piano note: two soft sines, the second detuned for the
// warble, and a faint bell an octave and a fifth up.
func (m *musicMix) keys(freq, at0, length, vol float64) {
	key := [3]float64{-1, length, vol}
	shape, ok := m.curves[key]
	if !ok {
		shape = make([]float64, samples(length+0.05))
		for i := range shape {
			t := float64(i) / movieSampleRate
			switch {
			case t > length:
			case t < 0.02:
				shape[i] = envelope(t, 0.02, 1, vol)
			case t < 0.6:
				shape[i] = vol * math.Pow(0.4, (t-0.02)/0.58)
			default:
				shape[i] = vol * 0.4 * math.Pow(0.0001/(vol*0.4), (t-0.6)/(length-0.6))
			}
		}
		m.curves[key] = shape
	}
	start := m.index(at0)
	detuned := freq * cents(14)
	for i, g := range shape {
		t := float64(i) / movieSampleRate
		wobble := 1 + 0.002*math.Sin(2*math.Pi*0.7*t)
		v := math.Sin(2*math.Pi*freq*wobble*t) + 0.5*math.Sin(2*math.Pi*detuned*t) + 0.06*math.Sin(2*math.Pi*freq*3*t)
		write(m.bus, start+i, v*g)
	}
}

// pluck is the melody's voice, a muted koto: a triangle with a little saw,
// its brightness closing as it rings, sent to the echo as well.
func (m *musicMix) pluck(freq, at0, vol float64) {
	start := m.index(at0)
	shape := m.curve(0.005, 1.8, vol)
	detuned := freq * cents(4)
	var low float64
	for i := 0; i < samples(1.9); i++ {
		t := float64(i) / movieSampleRate
		raw := triangle(freq*t) + 0.25*sawtooth(detuned*t)
		cutoff := freq * 7 * math.Pow(1.5/7, math.Min(t, 0.8)/0.8)
		low += (1 - math.Exp(-2*math.Pi*cutoff/movieSampleRate)) * (raw - low)
		v := low * curveAt(shape, i)
		write(m.bus, start+i, v)
		write(m.echo, start+i, v)
	}
}

func (m *musicMix) bass(freq, at0, length float64) {
	start := m.index(at0)
	shape := m.curve(0.02, length, 0.42)
	var low float64
	smooth := 1 - math.Exp(-2*math.Pi*260/movieSampleRate)
	for i := 0; i < samples(length+0.05); i++ {
		t := float64(i) / movieSampleRate
		low += smooth * (triangle(freq*t) - low)
		write(m.bus, start+i, low*curveAt(shape, i))
	}
}

// kick is a sine falling from 110 Hz to 42 Hz, into buffer.
func (m *musicMix) kick(buffer []float64, at, vol float64) {
	start := m.index(at)
	phase := 0.0
	for i := 0; i < samples(0.4); i++ {
		t := float64(i) / movieSampleRate
		freq := 110 * math.Pow(42.0/110, math.Min(t, 0.12)/0.12)
		phase += freq / movieSampleRate
		if t <= 0.35 {
			write(buffer, start+i, math.Sin(2*math.Pi*phase)*vol*math.Pow(0.0001/vol, t/0.35))
		}
	}
}

func (m *musicMix) rim(at float64) {
	for _, part := range [][2]float64{{1600, 0.08}, {480, 0.12}} {
		start := m.index(at)
		for i := 0; i < samples(0.06); i++ {
			t := float64(i) / movieSampleRate
			write(m.bus, start+i, triangle(part[0]*t)*envelope(t, 0.001, 0.05, part[1]))
		}
	}
}

// noiseBurst is a short burst of filtered noise, fading out: a brush, a hat.
// brightness, 0 to 1, is how much of the top end gets through.
func (m *musicMix) noiseBurst(buffer []float64, at, length, vol, brightness float64) {
	start := m.index(at)
	n := samples(length)
	var low, high float64
	for i := 0; i < n; i++ {
		raw := (m.noise.Float64()*2 - 1) * math.Pow(1-float64(i)/float64(n), 3)
		high += 0.5 * (raw - high)
		low += brightness * ((raw - high) - low)
		write(buffer, start+i, low*vol)
	}
}

// whoosh is air rushing past: noise through a band that sweeps up and back
// down, swelling and fading over length.
func (m *musicMix) whoosh(at, length, vol float64) {
	start := m.index(at)
	n := samples(length)
	var low, band float64
	for i := 0; i < n; i++ {
		p := float64(i) / float64(n)
		cutoff := 300 + 4200*math.Sin(math.Pi*p)
		a := 1 - math.Exp(-2*math.Pi*cutoff/movieSampleRate)
		raw := m.noise.Float64()*2 - 1
		low += a * (raw - low)
		band += 0.3 * (low - band)
		write(m.fx, start+i, (low-band)*vol*math.Pow(math.Sin(math.Pi*p), 2))
	}
}

// riser climbs into a moment: a sine gliding up an octave and a half, and
// noise thickening under it.
func (m *musicMix) riser(at, length, vol float64) {
	start := m.index(at)
	n := samples(length)
	phase := 0.0
	var low float64
	for i := 0; i < n; i++ {
		p := float64(i) / float64(n)
		phase += 220 * math.Pow(2, 1.5*p) / movieSampleRate
		low += 0.08 * (m.noise.Float64()*2 - 1 - low)
		swell := math.Pow(p, 2.2)
		write(m.fx, start+i, (0.5*math.Sin(2*math.Pi*phase)+1.4*low)*vol*swell)
	}
}

// impact is a landing: a long low boom under a short crack.
func (m *musicMix) impact(at, vol float64) {
	start := m.index(at)
	phase := 0.0
	for i := 0; i < samples(2.2); i++ {
		t := float64(i) / movieSampleRate
		freq := 30 + 40*math.Exp(-t*6)
		phase += freq / movieSampleRate
		write(m.fx, start+i, math.Sin(2*math.Pi*phase)*envelope(t, 0.004, 2.1, vol))
	}
	m.noiseBurst(m.fx, at, 0.25, vol*0.7, 0.7)
	m.kick(m.fx, at, vol*0.8)
}

// shimmer is a quick glittering run up the chord, three octaves high.
func (m *musicMix) shimmer(at, length, vol float64) {
	chord := musicChords[0].keys
	step := 0.045
	for k := 0; float64(k)*step < length; k++ {
		note := chord[k%4] + 24 + 12*(k/8%2)
		freq := midiHz(note)
		start := m.index(at + float64(k)*step)
		fade := 1 - float64(k)*step/length
		for i := 0; i < samples(0.5); i++ {
			t := float64(i) / movieSampleRate
			write(m.fx, start+i, math.Sin(2*math.Pi*freq*t)*envelope(t, 0.003, 0.45, vol*fade))
		}
	}
}

// tick is the faint click of a counter or a key.
func (m *musicMix) tick(at, vol float64) {
	start := m.index(at)
	pitch := 2200 + m.noise.Float64()*500
	for i := 0; i < samples(0.02); i++ {
		t := float64(i) / movieSampleRate
		write(m.fx, start+i, triangle(pitch*t)*envelope(t, 0.0005, 0.018, vol))
	}
}

// pop is a soft round blip, rising.
func (m *musicMix) pop(at, vol float64) {
	start := m.index(at)
	phase := 0.0
	for i := 0; i < samples(0.12); i++ {
		t := float64(i) / movieSampleRate
		phase += (520 + 900*t/0.12) / movieSampleRate
		write(m.fx, start+i, math.Sin(2*math.Pi*phase)*envelope(t, 0.003, 0.11, vol))
	}
}

// bell is a temple bell: inharmonic partials ringing out slowly.
func (m *musicMix) bell(at, vol float64) {
	start := m.index(at)
	partials := [][3]float64{{1, 1, 4.5}, {2.76, 0.5, 2.8}, {5.4, 0.28, 1.6}, {8.93, 0.15, 0.9}}
	base := midiHz(52)
	for i := 0; i < samples(4.6); i++ {
		t := float64(i) / movieSampleRate
		var v float64
		for _, p := range partials {
			v += p[1] * math.Sin(2*math.Pi*base*p[0]*t) * envelope(t, 0.002, p[2], 1)
		}
		write(m.fx, start+i, v*vol)
	}
}

func inRanges(t float64, ranges [][2]float64) bool {
	for _, r := range ranges {
		if t >= r[0] && t < r[1] {
			return true
		}
	}
	return false
}

// movieSoundtrack renders the score as a mono 16-bit WAV.
func movieSoundtrack(score movieScore) []byte {
	total := samples(score.Length)
	m := &musicMix{
		bus: make([]float64, total), echo: make([]float64, total), fx: make([]float64, total),
		noise:  rand.New(rand.NewPCG(66, 4)),
		curves: map[[3]float64][]float64{},
	}

	for step, at := 0, 0.2; at < score.Length; step, at = step+1, at+musicEighth {
		bar := step / 8
		e := step % 8
		when := at
		if e%2 == 1 {
			when += musicSwing * musicBeat
		}
		chord := musicChords[bar%4]
		if e == 0 {
			for k, note := range chord.keys {
				m.keys(midiHz(note), when+float64(k)*0.035, musicEighth*8, 0.065)
			}
			m.bass(midiHz(chord.bass-12), when, musicEighth*3.5)
		}
		if e == 3 {
			m.bass(midiHz(chord.bass-12+7), when, musicEighth*1.5)
		}
		if inRanges(when, score.Drums) {
			if musicKick[e] {
				m.kick(m.bus, when, 0.5)
			}
			if musicRim[e] {
				m.rim(when)
			}
			hat := 0.035
			if e%2 == 0 {
				hat = 0.06
			}
			m.noiseBurst(m.bus, when, 0.09, hat, 0.6)
		}
		// The melody comes in after the opening and rests every fourth
		// cycle, so it never wears out.
		if bar >= 3 && bar/4%4 != 3 {
			for _, note := range musicMelody[bar%4] {
				if note[0] == e {
					m.pluck(midiHz(note[1]), when+(m.noise.Float64()-0.5)*0.012, 0.1)
				}
			}
		}
	}

	for _, cue := range score.Cues {
		switch cue.Kind {
		case cueWhoosh:
			m.whoosh(cue.At, math.Max(cue.Length, 0.6), 0.5)
		case cueRiser:
			m.riser(cue.At, math.Max(cue.Length, 0.8), 0.22)
		case cueImpact:
			m.impact(cue.At, 0.85)
		case cueShimmer:
			m.shimmer(cue.At, math.Max(cue.Length, 0.8), 0.06)
		case cueTick:
			m.tick(cue.At, 0.09)
		case cuePop:
			m.pop(cue.At, 0.18)
		case cueBell:
			m.bell(cue.At, 0.22)
		case cueThud:
			m.kick(m.fx, cue.At, 1)
			m.noiseBurst(m.fx, cue.At, 0.09, 0.6, 0.5)
		}
	}

	// The drone: detuned saws on E, filtered dark. The echo: a feedback delay
	// of two thirds of a beat. Then the vinyl.
	var droneFreqs []float64
	for _, note := range []int{28, 40, 47} {
		for _, detune := range []float64{-7, 7} {
			droneFreqs = append(droneFreqs, midiHz(note)*cents(detune))
		}
	}
	var droneLow float64
	droneSmooth := 1 - math.Exp(-2*math.Pi*200/movieSampleRate)
	delay := int(math.Round(musicBeat * 0.66 * movieSampleRate))
	echoLine := make([]float64, total)
	for i := range m.bus {
		t := float64(i) / movieSampleRate
		var drone float64
		for _, freq := range droneFreqs {
			drone += sawtooth(freq * t)
		}
		droneLow += droneSmooth * (drone - droneLow)
		m.bus[i] += droneLow * 0.045

		fed := m.echo[i]
		if i >= delay {
			fed += 0.32 * echoLine[i-delay]
			m.bus[i] += 0.3 * echoLine[i-delay]
		}
		echoLine[i] = fed

		crackle := (m.noise.Float64()*2 - 1) * 0.02
		if m.noise.Float64() < 0.0005*44100/movieSampleRate {
			crackle += (m.noise.Float64() - 0.5) * 0.9
		}
		m.bus[i] += crackle * 0.16
	}

	music := musicRoom(m.bus, 0.35)
	effects := musicRoom(m.fx, 0.25)

	// The music gets the warm low-pass; the effects go on top of it; a soft
	// limiter holds both, with fades at either end.
	var low, peak float64
	smooth := 1 - math.Exp(-2*math.Pi*1900/movieSampleRate)
	out := make([]float64, total)
	for i := range out {
		low += smooth * (music[i] - low)
		t := float64(i) / movieSampleRate
		out[i] = math.Tanh((low*1.5+effects[i])*1.2) * math.Min(1, t/1.5) * math.Min(1, (score.Length-t)/3)
		peak = math.Max(peak, math.Abs(out[i]))
	}
	gain := 1.0
	if peak > 0 {
		gain = 0.88 / peak
	}
	return encodeWAV(out, gain)
}

// musicRoom adds a room: a handful of damped feedback combs into allpass
// filters, the classic cheap reverb, mixed in at amount.
func musicRoom(dry []float64, amount float64) []float64 {
	combs := []float64{0.0297, 0.0371, 0.0411, 0.0437}
	wet := make([]float64, len(dry))
	for _, seconds := range combs {
		size := samples(seconds)
		line := make([]float64, size)
		var damp float64
		for i, sample := range dry {
			j := i % size
			out := line[j]
			damp += 0.4 * (out - damp)
			line[j] = sample + damp*0.86
			wet[i] += out
		}
	}
	for _, seconds := range []float64{0.005, 0.0017} {
		size := samples(seconds)
		line := make([]float64, size)
		for i, sample := range wet {
			j := i % size
			buffered := line[j]
			line[j] = sample + buffered*0.5
			wet[i] = buffered - sample
		}
	}
	mixed := make([]float64, len(dry))
	for i := range dry {
		mixed[i] = dry[i] + amount*wet[i]/float64(len(combs))
	}
	return mixed
}

func encodeWAV(samples []float64, gain float64) []byte {
	var wav bytes.Buffer
	dataSize := uint32(len(samples) * 2)
	wav.WriteString("RIFF")
	binary.Write(&wav, binary.LittleEndian, 36+dataSize)
	wav.WriteString("WAVEfmt ")
	for _, field := range []any{
		uint32(16), uint16(1), uint16(1), uint32(movieSampleRate),
		uint32(movieSampleRate * 2), uint16(2), uint16(16),
	} {
		binary.Write(&wav, binary.LittleEndian, field)
	}
	wav.WriteString("data")
	binary.Write(&wav, binary.LittleEndian, dataSize)
	pcm := make([]int16, len(samples))
	for i, sample := range samples {
		pcm[i] = int16(math.Max(-1, math.Min(1, sample*gain)) * math.MaxInt16)
	}
	binary.Write(&wav, binary.LittleEndian, pcm)
	return wav.Bytes()
}
