package cast

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Encoder is how the video is re-encoded when subtitles are burned into it.
//
// Burning is the one thing the copy path cannot do: the subtitles are drawn
// onto the frames, so the frames have to be written again. Everything else
// about the stream -- the audio, the segmenting, the playlist -- is unchanged.
type Encoder struct {
	// Name is the ffmpeg encoder, e.g. "libx264" or "h264_vaapi".
	Name string
	// Device is the render node a hardware encoder works through, empty for
	// software encoders.
	Device string
}

// Hardware reports whether this encoder works on the GPU.
func (e Encoder) Hardware() bool { return e.Device != "" }

// castBurnLevel is the H.264 level the burned stream is held to.
//
// A Chromecast decodes H.264 High up to Level 4.1 and refuses anything above
// it, and re-encoding is the moment to state that rather than rewrite it
// afterwards the way the copy path has to.
const castBurnLevel = "4.1"

// BuildBurnArgs is the ffmpeg command that draws subtitles into the video and
// segments the result for the device.
//
// It mirrors BuildRemuxArgs everywhere the stream is concerned -- the same
// referrer handling, the same tolerance for disguised segments, the same event
// playlist -- and differs only in re-encoding the video instead of copying it.
func BuildBurnArgs(streamURL, referrer, subtitlePath, outDir string, enc Encoder) []string {
	return BuildBurnArgsFrom(streamURL, referrer, subtitlePath, outDir, enc, 0)
}

// BuildBurnArgsFrom is BuildBurnArgs starting the stream at an offset.
//
// The subtitle file passed here must already be shifted by the same offset --
// see ShiftWebVTT. The filter reads cue timings as they are written, and a
// stream restarted at the target plays from zero, so an unshifted file would
// show every line the seek distance too late.
func BuildBurnArgsFrom(streamURL, referrer, subtitlePath, outDir string, enc Encoder, startAt float64) []string {
	args := []string{"-hide_banner"}

	// Named before the input, because it sets up the device the filter chain
	// uploads to.
	if enc.Hardware() {
		args = append(args, "-vaapi_device", enc.Device)
	}

	// Providers routinely disguise HLS segments as images, and ffmpeg's HLS
	// demuxer rejects an extension it does not recognise.
	args = append(args, "-allowed_extensions", "ALL", "-extension_picky", "0")

	// -headers is a per-input option: it applies only to the next -i.
	if referrer = strings.TrimSpace(referrer); referrer != "" {
		args = append(args, "-headers", "Referer: "+referrer+"\r\n")
	}
	args = append(args, seekArgs(startAt)...)
	args = append(args, "-i", streamURL)

	filter := "subtitles=" + escapeFilterPath(subtitlePath)
	if enc.Hardware() {
		// The subtitles are drawn on the CPU, then the frames go to the GPU in
		// the pixel format the encoder takes.
		filter += ",format=nv12,hwupload"
	}
	args = append(args, "-vf", filter)

	args = append(args,
		"-c:v", enc.Name,
		"-profile:v", "high",
		"-level", castBurnLevel,
		// Without this, ffmpeg's default frame-rate handling can duplicate or
		// drop video frames to force a constant rate while the audio track
		// below is copied verbatim with its original timestamps untouched --
		// the two drift apart, worst right after a seek, where the encoder
		// restarts its rate assumption from a fresh, likely irregular, point
		// in the source. Passthrough keeps every input frame's own
		// timestamp, exactly matching what -c:a copy already does for audio.
		"-fps_mode", "passthrough",
	)
	if !enc.Hardware() {
		// Fast enough to stay well ahead of playback, which is the only
		// deadline that matters: the viewer is watching the front of this.
		args = append(args, "-preset", "veryfast")
	}

	// Nothing is drawn on the audio, so it is still copied.
	args = append(args, "-c:a", "copy")

	args = append(args,
		"-f", "hls",
		"-hls_time", "4",
		"-hls_playlist_type", "event",
		"-hls_flags", "independent_segments",
		"-hls_segment_filename", filepath.Join(outDir, "seg%05d.ts"),
	)

	args = append(args, "-nostdin", "-y", filepath.Join(outDir, PlaylistName))
	return args
}

// escapeFilterPath makes a path safe to embed in an ffmpeg filter argument.
//
// Filter syntax gives \ : ' and [ ] their own meanings, so a storage path
// containing any of them -- and a person's home directory may well -- would
// otherwise be parsed as filter syntax rather than read as a filename.
func escapeFilterPath(path string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`:`, `\:`,
		`'`, `\'`,
		`[`, `\[`,
		`]`, `\]`,
		`,`, `\,`,
	)
	return "'" + replacer.Replace(path) + "'"
}

// errWorkingProbe is only used by tests, to stand in for an encoder that will
// not run on this machine.
var errWorkingProbe = fmt.Errorf("cast: probe failed")

// ChooseEncoder picks how to re-encode, preferring the GPU when it actually
// works.
//
// The hardware encoder is probed rather than assumed: a render node can exist
// while h264_vaapi refuses to initialise -- a headless session, a driver
// without the entrypoint, a container without permission -- and without a
// probe that failure arrives when ffmpeg is already meant to be streaming, as
// a cast that never starts.
//
// probe and exists are injected so the choice is testable without a GPU.
func ChooseEncoder(ffmpegPath, renderNode string, probe func(Encoder) error, exists func(string) bool) Encoder {
	software := Encoder{Name: "libx264"}

	if renderNode == "" || !exists(renderNode) {
		return software
	}
	hardware := Encoder{Name: "h264_vaapi", Device: renderNode}
	if err := probe(hardware); err != nil {
		Log(fmt.Sprintf("cast: hardware encoding unavailable, falling back to libx264: %v", err))
		return software
	}
	return hardware
}

// ProbeEncoder runs a one-frame encode to find out whether this encoder works
// on this machine, which is the only way to know short of trying.
func ProbeEncoder(ffmpegPath string, enc Encoder) error {
	args := []string{"-hide_banner", "-loglevel", "error"}
	if enc.Hardware() {
		args = append(args, "-vaapi_device", enc.Device)
	}
	args = append(args,
		"-f", "lavfi", "-i", "color=c=black:s=320x240:d=0.1",
	)
	if enc.Hardware() {
		args = append(args, "-vf", "format=nv12,hwupload")
	}
	args = append(args, "-c:v", enc.Name, "-frames:v", "1", "-f", "null", "-")

	out, err := exec.Command(ffmpegPath, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// DefaultRenderNode is where a GPU usually appears on Linux.
const DefaultRenderNode = "/dev/dri/renderD128"
