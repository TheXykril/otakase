package cast

import (
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Encoder is how the video is re-encoded when subtitles are burned into it.
//
// Burning is the one thing the copy path cannot do: the subtitles are drawn
// onto the frames, so the frames have to be written again. The segmenting and
// the playlist are unchanged.
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
	return BuildBurnArgsFrom(streamURL, referrer, subtitlePath, outDir, enc, 0, nil, nil)
}

// BuildBurnArgsFrom is BuildBurnArgs starting the stream at an offset.
//
// The subtitle file passed here must already be shifted by the same offset --
// see ShiftSubtitles. The filter reads cue timings as they are written, and a
// stream restarted at the target plays from zero, so an unshifted file would
// show every line the seek distance too late.
//
// An empty subtitlePath re-encodes without drawing anything. That is how a
// stream with no subtitles to burn is restarted at an offset: see
// BuildRemuxArgsFrom for why copying cannot start it there in sync.
func BuildBurnArgsFrom(streamURL, referrer, subtitlePath, outDir string, enc Encoder, startAt float64, maps []string, headers map[string]string) []string {
	args := []string{"-hide_banner"}

	// Named before the input, because it sets up the device the filter chain
	// uploads to.
	if enc.Hardware() {
		args = append(args, "-vaapi_device", enc.Device)
	}

	// Providers routinely disguise HLS segments as images, and ffmpeg's HLS
	// demuxer rejects an extension it does not recognise.
	args = append(args, HLSInputArgs()...)

	// -headers is a per-input option: it applies only to the next -i.
	args = append(args, inputHeaderArgs(referrer, headers)...)
	// The input is seeked short of the target and the rest is trimmed away
	// frame by frame, below. See castSeekPreroll.
	preroll := math.Min(startAt, castSeekPreroll)
	args = append(args, seekArgs(startAt-preroll)...)
	args = append(args, "-i", streamURL)
	args = append(args, maps...)

	var filters []string
	if preroll > 0 {
		// First, so everything after it -- the subtitles above all, which were
		// shifted by the whole target -- sees the target as zero.
		filters = append(filters, fmt.Sprintf("trim=start=%s,setpts=PTS-%s/TB", seconds(preroll), seconds(preroll)))
	}
	// The backstop for a keyframe further past the seek point than the
	// preroll reaches: the first frame is held from zero until the next one,
	// so the picture still starts where the sound does. With the preroll
	// doing its job this holds nothing.
	filters = append(filters, "fps=source_fps:start_time=0")
	if subtitlePath != "" {
		filters = append(filters, "subtitles="+escapeFilterPath(subtitlePath))
	}
	if enc.Hardware() {
		// The subtitles are drawn on the CPU, then the frames go to the GPU in
		// the pixel format the encoder takes.
		filters = append(filters, "format=nv12,hwupload")
	}
	args = append(args, "-vf", strings.Join(filters, ","))

	args = append(args,
		"-c:v", enc.Name,
		"-profile:v", "high",
		"-level", castBurnLevel,
		// Without this, ffmpeg's default frame-rate handling can duplicate or
		// drop video frames to force a constant rate while the audio keeps
		// its source timestamps -- the two drift apart, worst right after a
		// seek, where the encoder restarts its rate assumption from a fresh,
		// likely irregular, point in the source. Passthrough keeps the
		// timestamps the filters above settled.
		"-fps_mode", "passthrough",
	)
	if !enc.Hardware() {
		// Fast enough to stay well ahead of playback, which is the only
		// deadline that matters: the viewer is watching the front of this.
		args = append(args, "-preset", "veryfast")
	}

	// Nothing is drawn on the audio, but it is encoded anyway. A copied track
	// cannot be cut mid-packet, so after a seek it starts at the keyframe
	// before the target while the re-encoded video starts at the target
	// itself. The audio then leads the video by up to a segment, and the
	// receiver plays each from its first sample, so the sound runs that far
	// behind the picture. Encoding the audio trims it at the target too.
	//
	// aresample then holds the audio to its timestamps: first_pts=0 starts it
	// at the same zero as the video, padding with silence when the source's
	// audio begins later, and async fills any gap the source leaves between
	// frames. A receiver that counts samples rather than reading every
	// timestamp -- and one that plays each track from its first sample -- then
	// hears the audio exactly where the timestamps put it.
	audioFilters := []string{"aresample=async=1:first_pts=0"}
	if preroll > 0 {
		audioFilters = append([]string{fmt.Sprintf("atrim=start=%s,asetpts=PTS-%s/TB", seconds(preroll), seconds(preroll))}, audioFilters...)
	}
	args = append(args, "-af", strings.Join(audioFilters, ","))
	args = append(args, "-c:a", "aac", "-b:a", "192k")

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

// castSeekPreroll is how far short of a seek target the input is seeked.
//
// ffmpeg's HLS reader starts audio at the target but drops video up to the
// next keyframe, and a host may space its keyframes ten seconds apart:
// anizone's are at 490.4s and 496.4s, so a resume at 493s began the picture
// 3.4s after the sound. The receiver plays each track from its first sample,
// so the voice ran 3.4s behind the mouths for the whole episode. Seeking this
// far early lands on a keyframe before the target, and the trim filters then
// cut both tracks at the target itself, to the frame and to the sample.
const castSeekPreroll = 15.0

// seconds formats a filter argument in seconds, to the millisecond.
func seconds(value float64) string {
	return strconv.FormatFloat(value, 'f', 3, 64)
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
