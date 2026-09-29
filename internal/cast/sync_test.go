package cast

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture for the tests below: an HLS master whose audio and video are
// separate playlists cut on different boundaries -- video every 10s, on a
// keyframe and with none between, audio every 4s -- with a smaller variant
// listed first, the way anizone serves its episodes. Every 5s the picture
// flashes white for one frame and a beep sounds, at the same instant, so how
// far apart the two land in an output is how far out of sync that output is.
func buildDemuxedSyncFixture(t *testing.T, ffmpegBin string) string {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"v", "small", "a"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command(ffmpegBin, append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("building the fixture: %v\n%s", err, out)
		}
	}
	run("-f", "lavfi", "-i", "color=c=black:s=160x90:r=24000/1001:d=40,drawbox=c=white:t=fill:enable='lt(mod(t,5),0.04)'",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "240", "-keyint_min", "240", "-sc_threshold", "0", "-bf", "2",
		"-f", "hls", "-hls_time", "10", "-hls_playlist_type", "vod",
		"-hls_segment_filename", filepath.Join(dir, "v", "%03d.ts"), filepath.Join(dir, "v", "playlist.m3u8"))
	run("-f", "lavfi", "-i", "color=c=black:s=64x36:r=24000/1001:d=40",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "240", "-keyint_min", "240", "-sc_threshold", "0",
		"-f", "hls", "-hls_time", "10", "-hls_playlist_type", "vod",
		"-hls_segment_filename", filepath.Join(dir, "small", "%03d.ts"), filepath.Join(dir, "small", "playlist.m3u8"))
	run("-f", "lavfi", "-i", "sine=f=1000:d=40:r=48000,volume=enable='gte(mod(t,5),0.04)':volume=0",
		"-c:a", "aac", "-f", "hls", "-hls_time", "4", "-hls_playlist_type", "vod",
		"-hls_segment_filename", filepath.Join(dir, "a", "%03d.ts"), filepath.Join(dir, "a", "playlist.m3u8"))

	master := filepath.Join(dir, "master.m3u8")
	body := "#EXTM3U\n" +
		`#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="Japanese",LANGUAGE="ja",DEFAULT=YES,AUTOSELECT=YES,URI="a/playlist.m3u8"` + "\n" +
		`#EXT-X-STREAM-INF:BANDWIDTH=100000,RESOLUTION=64x36,AUDIO="aud"` + "\nsmall/playlist.m3u8\n" +
		`#EXT-X-STREAM-INF:BANDWIDTH=500000,RESOLUTION=160x90,AUDIO="aud"` + "\nv/playlist.m3u8\n"
	if err := os.WriteFile(master, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return master
}

// syncMarks decodes a stream and reports, on the one clock both tracks share,
// when the video and the audio each begin and when the first flash and the
// first beep arrive, in the video stream named by video.
type syncMarks struct {
	videoStart, audioStart float64
	flash, beep            float64
}

func measureSync(t *testing.T, ffmpegBin, input, video string) syncMarks {
	t.Helper()
	dir := t.TempDir()
	videoStats := filepath.Join(dir, "video.txt")
	audioStats := filepath.Join(dir, "audio.txt")
	// 480-sample audio frames: 10ms resolution for the beep.
	cmd := exec.Command(ffmpegBin, "-hide_banner", "-v", "error", "-allowed_extensions", "ALL", "-i", input,
		"-map", video, "-vf", "signalstats,metadata=print:key=lavfi.signalstats.YAVG:file="+videoStats, "-f", "null", "-",
		"-map", "0:a:0", "-af", "asetnsamples=480,astats=metadata=1:reset=1,ametadata=print:key=lavfi.astats.Overall.RMS_level:file="+audioStats, "-f", "null", "-")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("measuring %s: %v\n%s", input, err, out)
	}

	// Each frame is a "frame:N pts:P pts_time:T" line, then its value.
	read := func(path string, marked func(float64) bool) (start, mark float64) {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		start, mark = math.NaN(), math.NaN()
		current := math.NaN()
		for _, line := range strings.Split(string(raw), "\n") {
			if i := strings.Index(line, "pts_time:"); i >= 0 {
				if _, err := fmt.Sscanf(line[i+len("pts_time:"):], "%g", &current); err == nil && math.IsNaN(start) {
					start = current
				}
				continue
			}
			if eq := strings.LastIndex(line, "="); eq >= 0 && math.IsNaN(mark) {
				var value float64
				if _, err := fmt.Sscanf(line[eq+1:], "%g", &value); err == nil && marked(value) {
					mark = current
				}
			}
		}
		if math.IsNaN(start) || math.IsNaN(mark) {
			t.Fatalf("no marks found in %s:\n%s", path, raw)
		}
		return start, mark
	}

	var m syncMarks
	m.videoStart, m.flash = read(videoStats, func(yavg float64) bool { return yavg > 100 })
	m.audioStart, m.beep = read(audioStats, func(rms float64) bool { return rms > -40 })
	return m
}

// A restart has to land both tracks at the target together. ffmpeg's HLS
// reader starts the audio at the target and the video at the next keyframe,
// and on anizone that put the picture 3.4s after the sound -- which a
// receiver playing each track from its first sample turns into the voice
// running 3.4s behind the mouths.
//
// That drop happens reading anizone and does not happen reading this local
// fixture, however it is encoded, so this cannot catch it coming back; the
// argument tests pin the preroll that fixes it. What this does check, on the
// decoded output of the command a seek actually runs: both tracks begin
// together, the flash and the beep still coincide the way they do in the
// source, and the stream begins at the target -- which a trim at the wrong
// point, or subtitles shifted against the wrong zero, would each break.
func TestARestartedStreamStartsAudioAndVideoTogether(t *testing.T) {
	ffmpegBin, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	master := buildDemuxedSyncFixture(t, ffmpegBin)
	// The sharp variant, which is the one a cast plays.
	source := measureSync(t, ffmpegBin, master, "0:v:m:variant_bitrate:500000")

	// Between the keyframes at 20s and 30s, and between audio segment
	// boundaries at 20s and 24s. The next flash is at 25s: 3s in.
	const target = 22.0
	outDir := filepath.Join(t.TempDir(), "restart")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	args := BuildBurnArgsFrom(master, "", "", outDir, Encoder{Name: "libx264"}, target, nil, nil)
	if out, err := exec.Command(ffmpegBin, args...).CombinedOutput(); err != nil {
		t.Fatalf("restarting the stream: %v\n%s", err, out)
	}
	got := measureSync(t, ffmpegBin, filepath.Join(outDir, PlaylistName), "0:v:0")
	t.Logf("source: %+v", source)
	t.Logf("restart at %.0fs: %+v", target, got)

	// Encoding costs a few tens of milliseconds -- the video's first frame is
	// the one after the target, and each encoder adds its own start delay --
	// which is below what a viewer can see. The failures this guards are
	// seconds.
	const tolerance = 0.1
	if gap := math.Abs(got.videoStart - got.audioStart); gap > tolerance {
		t.Errorf("the video starts %.3fs from the audio, want them together", gap)
	}
	sourceOffset := source.flash - source.beep
	if drift := (got.flash - got.beep) - sourceOffset; math.Abs(drift) > tolerance {
		t.Errorf("the flash and the beep are %.3fs further apart than in the source", drift)
	}
	// Read off the audio: the video's own start carries the encoder's frame
	// reordering delay, which says nothing about where the stream begins.
	if lead := got.beep - got.audioStart; math.Abs(lead-3) > tolerance {
		t.Errorf("the first beep is %.3fs in, want 3s: the stream did not start at the target", lead)
	}
}
