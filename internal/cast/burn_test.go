package cast

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func burnArgs(enc Encoder) string {
	return strings.Join(BuildBurnArgs("https://host.test/master.m3u8", "https://ref.test/", "/tmp/scratch/subs.ass", "/tmp/out", enc), " ")
}

// Burning subtitles means re-encoding the video, which is the one thing the
// copy path never does. The audio is still copied: nothing is drawn on it.
func TestBuildBurnArgsReencodesVideoAndCopiesAudio(t *testing.T) {
	args := burnArgs(Encoder{Name: "libx264"})

	if !strings.Contains(args, "-c:v libx264") {
		t.Errorf("video is not being re-encoded:\n%s", args)
	}
	if !strings.Contains(args, "-c:a copy") {
		t.Errorf("audio is being re-encoded for no reason:\n%s", args)
	}
	if strings.Contains(args, "-c copy") {
		t.Errorf("the blanket copy survived, which cannot burn anything:\n%s", args)
	}
}

// A 1st-generation Chromecast decodes H.264 High up to Level 4.1. Re-encoding
// is the one chance to state that rather than rewrite it afterwards, and an
// encoder left to its own devices will happily emit something the device
// refuses.
func TestBuildBurnArgsStaysWithinTheDeviceProfile(t *testing.T) {
	args := burnArgs(Encoder{Name: "libx264"})

	for _, want := range []string{"-profile:v high", "-level"} {
		if !strings.Contains(args, want) {
			t.Errorf("the encode does not constrain %q:\n%s", want, args)
		}
	}
}

// Without an explicit rate-passthrough, ffmpeg's default frame-rate handling
// can duplicate or drop re-encoded video frames to force a constant rate,
// while the copied audio track keeps its original timestamps untouched --
// drifting the two apart, worst right after a seek restarts the encoder's
// rate assumption from a fresh point in the source.
func TestBuildBurnArgsKeepsVideoAndAudioTimestampsInSync(t *testing.T) {
	args := burnArgs(Encoder{Name: "libx264"})

	if !strings.Contains(args, "-fps_mode passthrough") {
		t.Errorf("video frames are not passed through as-is, which can drift out of sync with the copied audio:\n%s", args)
	}
}

// The subtitles filter takes a path, and ffmpeg's filter syntax gives : and \
// and ' their own meanings -- a storage path containing any of them would
// otherwise be read as filter syntax rather than as a filename.
func TestBuildBurnArgsEscapesTheSubtitlePath(t *testing.T) {
	args := BuildBurnArgs("https://host.test/s.m3u8", "", `/tmp/it's:odd/subs.ass`, "/tmp/out", Encoder{Name: "libx264"})

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, `subtitles=`) {
		t.Fatalf("no subtitles filter:\n%s", joined)
	}
	if strings.Contains(joined, `subtitles=/tmp/it's:odd/subs.ass`) {
		t.Errorf("the path went in raw, so : and ' will be read as filter syntax:\n%s", joined)
	}
}

// Hardware encoding needs the frames uploaded to the GPU after the subtitles
// are drawn on them, and the device named up front.
func TestBuildBurnArgsUsesTheHardwareChain(t *testing.T) {
	args := burnArgs(Encoder{Name: "h264_vaapi", Device: "/dev/dri/renderD128"})

	for _, want := range []string{"-vaapi_device /dev/dri/renderD128", "hwupload", "-c:v h264_vaapi"} {
		if !strings.Contains(args, want) {
			t.Errorf("the hardware chain is missing %q:\n%s", want, args)
		}
	}
	if !strings.Contains(args, "format=nv12,hwupload") {
		t.Errorf("frames are uploaded without the pixel format the encoder needs:\n%s", args)
	}
}

// Everything the copy path learned the hard way still applies: the referrer is
// a per-input option and has to precede -i, providers disguise segments, and
// the device seeks through an event playlist.
func TestBuildBurnArgsKeepsTheStreamWorkarounds(t *testing.T) {
	args := BuildBurnArgs("https://host.test/master.m3u8", "https://ref.test/", "/tmp/subs.ass", "/tmp/out", Encoder{Name: "libx264"})

	joined := strings.Join(args, " ")
	for _, want := range []string{"-allowed_extensions ALL", "-extension_picky 0", "-hls_playlist_type event"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q:\n%s", want, joined)
		}
	}

	headers, input := -1, -1
	for i, a := range args {
		if a == "-headers" {
			headers = i
		}
		if a == "-i" && input < 0 {
			input = i
		}
	}
	if headers < 0 || headers > input {
		t.Errorf("the referrer does not precede -i, so it applies to nothing: %v", args)
	}
}

// Hardware encoding is worth having and cannot be assumed: a render node can
// exist while the encoder refuses to run, and the failure arrives only when
// ffmpeg is already streaming. The probe settles it in advance.
func TestChooseEncoderFallsBackWhenTheProbeFails(t *testing.T) {
	chosen := ChooseEncoder("ffmpeg", "/dev/dri/renderD128",
		func(Encoder) error { return errWorkingProbe },
		func(string) bool { return true })

	if chosen.Hardware() {
		t.Errorf("a failing probe still yielded a hardware encoder: %+v", chosen)
	}
	if chosen.Name != "libx264" {
		t.Errorf("fell back to %q, want libx264", chosen.Name)
	}
}

func TestChooseEncoderUsesHardwareWhenItWorks(t *testing.T) {
	chosen := ChooseEncoder("ffmpeg", "/dev/dri/renderD128",
		func(Encoder) error { return nil },
		func(string) bool { return true })

	if !chosen.Hardware() || chosen.Name != "h264_vaapi" {
		t.Errorf("a working probe did not yield hardware encoding: %+v", chosen)
	}
}

// No render node, no probe: a machine without a GPU should not be made to wait
// for ffmpeg to tell it so.
func TestChooseEncoderSkipsTheProbeWithoutARenderNode(t *testing.T) {
	probed := false
	chosen := ChooseEncoder("ffmpeg", "/dev/dri/renderD128",
		func(Encoder) error { probed = true; return nil },
		func(string) bool { return false })

	if probed {
		t.Error("the hardware encoder was probed on a machine with no render node")
	}
	if chosen.Name != "libx264" {
		t.Errorf("chose %q, want libx264", chosen.Name)
	}
}

// The argument tests assert strings; this one runs them. Burning is the only
// part of casting that rewrites the picture, so "the flags look right" is not
// evidence that anything was drawn -- the frames have to be checked.
//
// The check is a comparison rather than a threshold: the same black source is
// encoded with and without the subtitles filter, and the burned one must be
// measurably brighter. Six characters on a small frame move the average luma
// very little, so any fixed number here would be a number chosen to pass.
func TestBurnedStreamActuallyContainsTheSubtitles(t *testing.T) {
	ffmpegBin, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}

	dir := t.TempDir()
	subs := filepath.Join(dir, "subs.srt")
	if err := os.WriteFile(subs, []byte("1\n00:00:00,200 --> 00:00:04,000\nBURNED SUBTITLE TEXT\n"), 0o644); err != nil {
		t.Fatalf("writing subtitles: %v", err)
	}

	// A black source, so anything not black in the output was drawn by us.
	source := filepath.Join(dir, "source.m3u8")
	build := exec.Command(ffmpegBin, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=c=black:s=320x240:r=10:d=4",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-f", "hls", "-hls_time", "2", "-hls_playlist_type", "event", source)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the fixture: %v\n%s", err, out)
	}

	luma := func(name string, args []string) float64 {
		t.Helper()
		outDir := filepath.Join(dir, name)
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if out, err := exec.Command(ffmpegBin, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}

		statsFile := filepath.Join(dir, name+"-stats.txt")
		probe := exec.Command(ffmpegBin, "-hide_banner", "-v", "error",
			"-allowed_extensions", "ALL", "-i", filepath.Join(outDir, PlaylistName),
			"-vf", "signalstats,metadata=print:key=lavfi.signalstats.YAVG:file="+statsFile,
			"-f", "null", "-")
		if out, err := probe.CombinedOutput(); err != nil {
			t.Fatalf("probing %s: %v\n%s", name, err, out)
		}

		raw, err := os.ReadFile(statsFile)
		if err != nil {
			t.Fatalf("reading %s statistics: %v", name, err)
		}
		brightest := 0.0
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.Contains(line, "YAVG") {
				continue
			}
			var value float64
			if _, err := fmt.Sscanf(line[strings.LastIndex(line, "=")+1:], "%f", &value); err == nil && value > brightest {
				brightest = value
			}
		}
		if brightest == 0 {
			t.Fatalf("no luma statistics for %s", name)
		}
		return brightest
	}

	burned := luma("burned", BuildBurnArgs(source, "", subs, filepath.Join(dir, "burned"), Encoder{Name: "libx264"}))
	plain := luma("plain", BuildRemuxArgs(source, "", filepath.Join(dir, "plain")))

	t.Logf("brightest frame: burned %.2f, plain %.2f", burned, plain)
	if burned <= plain {
		t.Errorf("the burned stream is no brighter than the untouched one (%.2f vs %.2f): the subtitles filter drew nothing", burned, plain)
	}
}
