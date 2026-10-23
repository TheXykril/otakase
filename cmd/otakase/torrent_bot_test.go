package main

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBotTrackSelection(t *testing.T) {
	var p botProbe
	if err := json.Unmarshal([]byte(`{"format":{"duration":"12"},"streams":[{"index":0,"codec_type":"video"},{"index":1,"codec_type":"audio","tags":{"language":"eng"}},{"index":2,"codec_type":"audio","tags":{"language":"jpn"}},{"index":3,"codec_type":"subtitle","codec_name":"ass","tags":{"language":"eng","title":"Signs"},"disposition":{"default":1}},{"index":4,"codec_type":"subtitle","codec_name":"ass","tags":{"language":"eng","title":"Dialogue"}}]}`), &p); err != nil {
		t.Fatal(err)
	}
	sub, err := chooseBotMedia(p, "sub")
	if err != nil || sub.audio != 2 || sub.subtitle != 1 {
		t.Fatalf("sub: %+v %v", sub, err)
	}
	dub, err := chooseBotMedia(p, "dub")
	if err != nil || dub.audio != 1 || dub.subtitle != 0 {
		t.Fatalf("dub: %+v %v", dub, err)
	}
	p.Streams = p.Streams[:4]
	if _, err := chooseBotMedia(p, "sub"); err == nil {
		t.Fatal("signs-only subtitles accepted as dialogue")
	}
	p.Streams = p.Streams[:1]
	if _, err := chooseBotMedia(p, "dub"); err == nil {
		t.Fatal("missing audio accepted")
	}
	p.Format.Duration = "NaN"
	if _, err := chooseBotMedia(p, "sub"); err == nil {
		t.Fatal("invalid duration accepted")
	}
}

func TestBotSegmentsIntegration(t *testing.T) {
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skip(name + " unavailable")
		}
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mkv")
	subtitles := filepath.Join(dir, "dialogue.srt")
	os.WriteFile(subtitles, []byte("1\n00:00:08,000 --> 00:00:11,500\nLate subtitle\n"), 0600)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	args := []string{"-v", "error", "-f", "lavfi", "-i", "color=black:s=320x180:r=24:d=12", "-f", "lavfi", "-i", "sine=frequency=440:duration=12", "-f", "lavfi", "-i", "sine=frequency=880:duration=12", "-i", subtitles, "-map", "0:v", "-map", "1:a", "-map", "2:a", "-map", "3:s", "-c:v", "libx264", "-threads", "1", "-c:a", "aac", "-c:s", "srt", "-metadata:s:a:0", "language=eng", "-metadata:s:a:1", "language=jpn", "-metadata:s:s:0", "language=eng", source}
	if out, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	m, err := probeBotMedia(ctx, source, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if m.audio != 2 || m.subtitle != 0 {
		t.Fatalf("wrong tracks: %+v", m)
	}
	s := &botSegments{ctx: ctx, source: source, dir: dir, media: m, gate: make(chan struct{}, 1)}
	if !strings.Contains(s.playlist(), "#EXT-X-ENDLIST") || !strings.Contains(s.playlist(), "seg2.ts") {
		t.Fatal("incomplete VOD playlist")
	}
	// Request the final segment first, then seek backward.
	for _, n := range []int{2, 0, 1, 2} {
		data, err := s.segment(ctx, n)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "segment.ts")
		os.WriteFile(path, data, 0600)
		out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=start_time,duration", "-of", "json", path).Output()
		if err != nil {
			t.Fatal(err)
		}
		var probe struct {
			Format struct {
				Start    string `json:"start_time"`
				Duration string `json:"duration"`
			} `json:"format"`
		}
		json.Unmarshal(out, &probe)
		start, err := strconv.ParseFloat(probe.Format.Start, 64)
		if err != nil || math.Abs(start-float64(n*4)) > 0.1 {
			t.Fatalf("segment %d timestamp: %s", n, out)
		}
		duration, err := strconv.ParseFloat(probe.Format.Duration, 64)
		if err != nil || math.Abs(duration-4) > 0.1 {
			t.Fatalf("segment duration: %s", out)
		}
		if n == 2 {
			// A white subtitle must be present on the otherwise black frame after seeking.
			pixels, err := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-i", path, "-ss", "1", "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "gray", "pipe:1").Output()
			if err != nil {
				t.Fatal(err)
			}
			bright := 0
			for _, v := range pixels {
				if v > 150 {
					bright++
				}
			}
			if bright < 20 {
				t.Fatal("embedded subtitle missing after seek")
			}
		}
	}
	if _, err := s.segment(ctx, 100); err == nil {
		t.Fatal("out-of-range segment accepted")
	}
}

func TestBotOutputLimit(t *testing.T) {
	output := &botLimitedOutput{limit: 4}
	if _, err := io.Copy(output, strings.NewReader("oversized")); err == nil {
		t.Fatal("output limit bypassed")
	}
	if output.buffer.Len() > 4 {
		t.Fatal("oversized output retained")
	}
}
