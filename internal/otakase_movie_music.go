package internal

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand/v2"
)

// The film's soundtrack is the games Activity's dark lo-fi loop ("Abyss", in
// bot/assets/activity/app.js), rendered offline instead of through Web Audio:
// 58 BPM in D Phrygian, warbly electric piano chords over a low drone, a deep
// bass, soft swung drums with brushes and a rim click, a koto-like melody
// after four bars, vinyl crackle, an echo and a dark room. On top of it the
// closing stamp lands with a thud. Nothing is shipped; it takes a moment and
// a temporary file.

const (
	movieSampleRate = 32000

	musicBeat   = 60.0 / 58
	musicEighth = musicBeat / 2
	// musicSwing pushes the off-beats late, as a share of a beat.
	musicSwing = 0.13
)

type musicChord struct {
	bass int
	keys [4]int
}

var musicChords = []musicChord{
	{38, [4]int{50, 53, 57, 62}}, // Dm
	{39, [4]int{51, 55, 58, 62}}, // Ebmaj7
	{38, [4]int{50, 53, 57, 64}}, // Dm(add9)
	{37, [4]int{49, 52, 55, 58}}, // C#dim7
}

// musicMelody is, per bar, the eighth a note starts on and the note.
var musicMelody = [][][2]int{
	{{0, 69}, {3, 70}, {4, 69}},
	{{1, 63}, {2, 62}, {4, 63}},
	{{0, 65}, {2, 63}, {4, 62}, {7, 61}},
	{{0, 62}},
}

var (
	musicKick  = [8]bool{true, false, false, false, false, true, false, false}
	musicSnare = [8]bool{false, false, true, false, false, false, true, false}
)

func midiHz(note int) float64 { return 440 * math.Pow(2, float64(note-69)/12) }

// musicMix is the buffers notes are written into: the dry bus and the echo's
// send.
type musicMix struct {
	bus, echo []float64
	noise     *rand.Rand
}

func (m *musicMix) index(at float64) int { return int(at * movieSampleRate) }

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

// keys is one electric piano note: two soft sines and a faint bell.
func (m *musicMix) keys(freq, at, length, vol float64) {
	start := m.index(at)
	for i := 0; i < int((length+0.05)*movieSampleRate); i++ {
		if start+i >= len(m.bus) {
			return
		}
		t := float64(i) / movieSampleRate
		var g float64
		switch {
		case t < 0.02:
			g = envelope(t, 0.02, 1, vol)
		case t < 0.5:
			g = vol * math.Pow(0.35, (t-0.02)/0.48)
		default:
			g = vol * 0.35 * math.Pow(0.0001/(vol*0.35), (t-0.5)/(length-0.5))
		}
		if t > length {
			g = 0
		}
		v := math.Sin(2*math.Pi*freq*t) + 0.6*math.Sin(2*math.Pi*freq*cents(18)*t) + 0.08*math.Sin(2*math.Pi*freq*4*t)
		m.bus[start+i] += v * g
	}
}

// pluck is the melody's voice: a triangle with a little saw, its brightness
// closing as it rings, sent to the echo as well.
func (m *musicMix) pluck(freq, at, vol float64) {
	start := m.index(at)
	var low float64
	for i := 0; i < int(1.7*movieSampleRate); i++ {
		if start+i >= len(m.bus) {
			return
		}
		t := float64(i) / movieSampleRate
		raw := triangle(freq*t) + 0.25*sawtooth(freq*cents(4)*t)
		cutoff := freq * 8 * math.Pow(1.4/8, math.Min(t, 0.7)/0.7)
		low += (1 - math.Exp(-2*math.Pi*cutoff/movieSampleRate)) * (raw - low)
		v := low * envelope(t, 0.006, 1.6, vol)
		m.bus[start+i] += v
		m.echo[start+i] += v
	}
}

func (m *musicMix) bass(freq, at, length float64) {
	start := m.index(at)
	var low float64
	smooth := 1 - math.Exp(-2*math.Pi*300/movieSampleRate)
	for i := 0; i < int((length+0.05)*movieSampleRate); i++ {
		if start+i >= len(m.bus) {
			return
		}
		t := float64(i) / movieSampleRate
		low += smooth * (triangle(freq*t) - low)
		m.bus[start+i] += low * envelope(t, 0.02, length, 0.4)
	}
}

func (m *musicMix) kick(at, vol float64) {
	start := m.index(at)
	phase := 0.0
	for i := 0; i < int(0.4*movieSampleRate); i++ {
		if start+i >= len(m.bus) {
			return
		}
		t := float64(i) / movieSampleRate
		freq := 110 * math.Pow(42.0/110, math.Min(t, 0.12)/0.12)
		phase += freq / movieSampleRate
		g := vol * math.Pow(0.0001/vol, t/0.35)
		if t > 0.35 {
			g = 0
		}
		m.bus[start+i] += math.Sin(2*math.Pi*phase) * g
	}
}

func (m *musicMix) rim(at float64) {
	for _, part := range [][2]float64{{1700, 0.08}, {520, 0.13}} {
		start := m.index(at)
		for i := 0; i < int(0.06*movieSampleRate); i++ {
			if start+i >= len(m.bus) {
				break
			}
			t := float64(i) / movieSampleRate
			m.bus[start+i] += triangle(part[0]*t) * envelope(t, 0.001, 0.05, part[1])
		}
	}
}

// brush is a short burst of band-passed noise.
func (m *musicMix) brush(at, length, vol float64) {
	start := m.index(at)
	n := int(length * movieSampleRate)
	var low, high float64
	for i := 0; i < n && start+i < len(m.bus); i++ {
		raw := (m.noise.Float64()*2 - 1) * math.Pow(1-float64(i)/float64(n), 3)
		// A band around 3.5 kHz: high-pass then low-pass.
		high += 0.5 * (raw - high)
		low += 0.6 * ((raw - high) - low)
		m.bus[start+i] += low * vol
	}
}

// thud is the stamp landing: a low knock and a dry slap.
func (m *musicMix) thud(at float64) {
	m.kick(at, 0.9)
	m.brush(at, 0.08, 0.5)
}

// movieSoundtrack is seconds of music as a mono 16-bit WAV, with the stamp's
// thud at stampAt.
func movieSoundtrack(seconds, stampAt float64) []byte {
	total := int(seconds * movieSampleRate)
	m := &musicMix{bus: make([]float64, total), echo: make([]float64, total), noise: rand.New(rand.NewPCG(58, 1))}

	for step, at := 0, 0.1; at < seconds; step, at = step+1, at+musicEighth {
		bar := step / 8 % 16
		e := step % 8
		when := at
		if e%2 == 1 {
			when += musicSwing * musicBeat
		}
		chord := musicChords[bar%4]
		if e == 0 {
			for k, note := range chord.keys {
				m.keys(midiHz(note), when+float64(k)*0.03, musicEighth*8, 0.07)
			}
			m.bass(midiHz(chord.bass-12), when, musicEighth*3)
		}
		if e == 5 {
			m.bass(midiHz(chord.bass-12), when, musicEighth*2)
		}
		if musicKick[e] && bar != 15 {
			m.kick(when, 0.55)
		}
		if musicSnare[e] {
			m.rim(when)
		}
		brush := 0.09
		if e%2 == 1 {
			brush = 0.05
		}
		m.brush(when, 0.12, brush)
		switch {
		case bar >= 4 && bar < 12:
			for _, note := range musicMelody[bar%4] {
				if note[0] == e {
					m.pluck(midiHz(note[1]), when+(m.noise.Float64()-0.5)*0.015, 0.11)
				}
			}
		case bar >= 12 && e%3 == 0 && m.noise.Float64() < 0.3:
			m.pluck(midiHz(chord.keys[m.noise.IntN(4)]+12), when, 0.06)
		}
	}
	if stampAt > 0 && stampAt < seconds {
		m.thud(stampAt)
	}

	// The drone: detuned saws on D, filtered dark.
	var droneLow float64
	droneSmooth := 1 - math.Exp(-2*math.Pi*220/movieSampleRate)
	// The echo: a feedback delay of two thirds of a beat.
	delay := int(math.Round(musicBeat * 0.68 * movieSampleRate))
	echoLine := make([]float64, total)
	for i := range m.bus {
		t := float64(i) / movieSampleRate
		var drone float64
		for _, note := range []int{26, 38, 45} {
			for _, detune := range []float64{-8, 8} {
				drone += sawtooth(midiHz(note) * cents(detune) * t)
			}
		}
		droneLow += droneSmooth * (drone - droneLow)
		m.bus[i] += droneLow * 0.05

		fed := m.echo[i]
		if i >= delay {
			fed += 0.3 * echoLine[i-delay]
		}
		echoLine[i] = fed
		if i >= delay {
			m.bus[i] += 0.3 * echoLine[i-delay]
		}

		// Vinyl: a hiss and the odd crackle.
		crackle := (m.noise.Float64()*2 - 1) * 0.02
		if m.noise.Float64() < 0.0004*44100/movieSampleRate {
			crackle += (m.noise.Float64() - 0.5) * 0.9
		}
		m.bus[i] += crackle * 0.18
	}

	out := musicRoom(m.bus)

	// Warm low-pass at 1.9 kHz, a soft limiter, and fades at both ends.
	var low, peak float64
	smooth := 1 - math.Exp(-2*math.Pi*1900/movieSampleRate)
	for i, sample := range out {
		low += smooth * (sample - low)
		t := float64(i) / movieSampleRate
		out[i] = math.Tanh(low*1.6) * math.Min(1, t/2) * math.Min(1, (seconds-t)/3)
		peak = math.Max(peak, math.Abs(out[i]))
	}
	gain := 1.0
	if peak > 0 {
		gain = 0.85 / peak
	}
	return encodeWAV(out, gain)
}

// musicRoom adds a long dark room: a handful of feedback combs into allpass
// filters, the classic cheap reverb, mixed in at about a third.
func musicRoom(dry []float64) []float64 {
	combs := []float64{0.0297, 0.0371, 0.0411, 0.0437}
	wet := make([]float64, len(dry))
	for _, seconds := range combs {
		size := int(seconds * movieSampleRate)
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
		size := int(seconds * movieSampleRate)
		line := make([]float64, size)
		for i, sample := range wet {
			j := i % size
			buffered := line[j]
			out := -sample + buffered
			line[j] = sample + buffered*0.5
			wet[i] = out
		}
	}
	mixed := make([]float64, len(dry))
	for i := range dry {
		mixed[i] = dry[i] + 0.35*wet[i]/float64(len(combs))
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
