package internal

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand/v2"
)

// The film's soundtrack is synthesised when it plays, the same way its picture
// is: a slow lo-fi loop of four chords, a soft pad under a plucked arpeggio,
// with a kick and a brushed snare once the credits start rolling, and a little
// vinyl crackle on top. Nothing is shipped; it costs a few milliseconds and a
// temporary file.

const (
	movieSampleRate = 22050
	// movieBeat is one beat at 80 BPM; a chord lasts a bar of four.
	movieBeat = 60.0 / 80
	movieBar  = 4 * movieBeat
)

// movieChords is Am7, Fmaj7, Cmaj7, Em7, as frequencies in Hz.
var movieChords = [][]float64{
	{220.00, 261.63, 329.63, 392.00},
	{174.61, 220.00, 261.63, 329.63},
	{130.81, 164.81, 196.00, 246.94},
	{164.81, 196.00, 246.94, 293.66},
}

// movieSoundtrack is seconds of music as a mono 16-bit WAV. drumsFrom is when
// the beat comes in, so the logo opens on the pad alone.
func movieSoundtrack(seconds, drumsFrom float64) []byte {
	total := int(seconds * movieSampleRate)
	samples := make([]float64, total)
	noise := rand.New(rand.NewPCG(7, 12))
	snareNoise := make([]float64, int(0.2*movieSampleRate))
	for i := range snareNoise {
		snareNoise[i] = noise.Float64()*2 - 1
	}

	for i := range samples {
		t := float64(i) / movieSampleRate
		bar := int(t / movieBar)
		inBar := t - float64(bar)*movieBar
		chord := movieChords[bar%len(movieChords)]
		next := movieChords[(bar+1)%len(movieChords)]

		// Pad: each chord swells in and hands over to the next across the
		// last beat, two slightly detuned voices per note.
		var pad float64
		blend := math.Max(0, (inBar-(movieBar-movieBeat))/movieBeat)
		for n := range chord {
			for _, detune := range []float64{0.997, 1.003} {
				pad += (1-blend)*math.Sin(2*math.Pi*chord[n]*detune*t) + blend*math.Sin(2*math.Pi*next[n]*detune*t)
			}
		}
		pad *= 0.022 * math.Min(1, t/2)

		// Arpeggio: eighth notes up the chord an octave higher, each a pluck
		// that dies away.
		eighth := movieBeat / 2
		step := int(inBar / eighth)
		since := inBar - float64(step)*eighth
		pattern := []int{0, 1, 2, 3, 2, 1, 3, 2}
		note := chord[pattern[step%len(pattern)]] * 2
		pluck := (math.Sin(2*math.Pi*note*t) + 0.3*math.Sin(4*math.Pi*note*t)) * math.Exp(-since*6)
		pluck *= 0.11 * math.Min(1, math.Max(0, (t-2)/2))

		// Bass: the root, two octaves down, on beats one and three.
		halfBar := 2 * movieBeat
		sinceBass := math.Mod(inBar, halfBar)
		bass := math.Sin(2*math.Pi*chord[0]/2*t) * math.Exp(-sinceBass*1.6) * 0.16

		var drums float64
		if t >= drumsFrom {
			beat := math.Mod(t-drumsFrom, movieBeat)
			beatIndex := int((t - drumsFrom) / movieBeat)
			if beatIndex%2 == 0 {
				// Kick: a sine falling from 110 Hz to 45 Hz.
				freq := 45 + 65*math.Exp(-beat*30)
				drums += math.Sin(2*math.Pi*freq*beat) * math.Exp(-beat*9) * 0.42
			} else if j := int(beat * movieSampleRate); j < len(snareNoise) {
				drums += snareNoise[j] * math.Exp(-beat*22) * 0.07
			}
		}

		// Vinyl: a quiet hiss and the odd crackle.
		vinyl := (noise.Float64()*2 - 1) * 0.004
		if noise.Float64() < 4.0/movieSampleRate {
			vinyl += (noise.Float64()*2 - 1) * 0.12
		}

		samples[i] = pad + pluck + bass + drums + vinyl
	}

	// A gentle low-pass keeps it warm, then fade in and out.
	const smoothing = 0.35
	var last, peak float64
	for i, sample := range samples {
		last += smoothing * (sample - last)
		t := float64(i) / movieSampleRate
		samples[i] = last * math.Min(1, t/1.5) * math.Min(1, (seconds-t)/3)
		peak = math.Max(peak, math.Abs(samples[i]))
	}
	gain := 1.0
	if peak > 0 {
		gain = 0.8 / peak
	}

	var wav bytes.Buffer
	dataSize := uint32(total * 2)
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
	pcm := make([]int16, total)
	for i, sample := range samples {
		pcm[i] = int16(math.Max(-1, math.Min(1, sample*gain)) * math.MaxInt16)
	}
	binary.Write(&wav, binary.LittleEndian, pcm)
	return wav.Bytes()
}
