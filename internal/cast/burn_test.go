package cast

import (
	"strings"
	"testing"
)

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

// A restart with nothing to burn still goes through the encoder, and draws
// nothing: an empty subtitles filter is an error that fails the whole stream.
func TestBuildBurnArgsWithoutSubtitlesDrawsNothing(t *testing.T) {
	software := strings.Join(BuildBurnArgsFrom("https://host.test/master.m3u8", "", "", "/tmp/out", Encoder{Name: "libx264"}, 90, nil, nil), " ")
	if strings.Contains(software, "subtitles=") {
		t.Errorf("a software re-encode with no subtitles still draws some:\n%s", software)
	}
	if !strings.Contains(software, "-c:v libx264") {
		t.Errorf("the re-encode lost its encoder:\n%s", software)
	}

	hardware := strings.Join(BuildBurnArgsFrom("https://host.test/master.m3u8", "", "", "/tmp/out", Encoder{Name: "h264_vaapi", Device: "/dev/dri/renderD128"}, 90, nil, nil), " ")
	if strings.Contains(hardware, "subtitles=") || !strings.HasSuffix(strings.Split(strings.SplitN(hardware, "-vf ", 2)[1], " ")[0], "format=nv12,hwupload") {
		t.Errorf("a hardware re-encode with no subtitles should end its filters on the upload:\n%s", hardware)
	}
}

// A seek is taken short of the target and trimmed to it, both tracks by the
// same amount, and the trim comes before the subtitles: they were shifted by
// the whole target, so they have to see the target as zero.
func TestBuildBurnArgsSeeksShortAndTrimsToTheTarget(t *testing.T) {
	args := strings.Join(BuildBurnArgsFrom("https://host.test/master.m3u8", "", "/tmp/subs.ass", "/tmp/out", Encoder{Name: "libx264"}, 493, nil, nil), " ")

	for _, want := range []string{
		"-ss 478.000 -i",
		"-vf trim=start=15.000,setpts=PTS-15.000/TB,fps=source_fps:start_time=0,subtitles=",
		"-af atrim=start=15.000,asetpts=PTS-15.000/TB,aresample=async=1:first_pts=0",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q:\n%s", want, args)
		}
	}

	// Near the start there is not a full preroll to take: the input is read
	// from zero and trimmed to the target.
	early := strings.Join(BuildBurnArgsFrom("https://host.test/master.m3u8", "", "", "/tmp/out", Encoder{Name: "libx264"}, 6, nil, nil), " ")
	if strings.Contains(early, "-ss ") || !strings.Contains(early, "trim=start=6.000") {
		t.Errorf("a seek to 6s should read from zero and trim 6s:\n%s", early)
	}
	start := strings.Join(BuildBurnArgsFrom("https://host.test/master.m3u8", "", "", "/tmp/out", Encoder{Name: "libx264"}, 0, nil, nil), " ")
	if strings.Contains(start, "trim=") {
		t.Errorf("a stream from zero has nothing to trim:\n%s", start)
	}
}
