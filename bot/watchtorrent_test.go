package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestWatchTorrentCancellationAndCleanup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	helper := filepath.Join(root, "helper")
	// Simulate a helper still buffering, before it announces an address.
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nread line\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _, err := startWatchTorrent(ctx, helper, "0123456789abcdef0123456789abcdef01234567", "sub", nil)
	if err == nil {
		t.Fatal("cancelled startup succeeded")
	}
	dirs, _ := filepath.Glob(filepath.Join(root, "otakase-room-torrent-*"))
	if len(dirs) != 0 {
		t.Fatalf("leaked directories: %v", dirs)
	}
	if len(watchTorrentSlot) != 0 {
		t.Fatal("helper slot leaked")
	}
}

func TestWatchTorrentStopAndCrash(t *testing.T) {
	for _, crash := range []bool{false, true} {
		t.Run(map[bool]string{false: "stop", true: "crash"}[crash], func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("TMPDIR", root)
			helper := filepath.Join(root, "helper")
			os.WriteFile(helper, []byte("#!/bin/sh\necho http://127.0.0.1:12345/playlist.m3u8\nread line\n"), 0700)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			tor, _, err := startWatchTorrent(ctx, helper, "0123456789abcdef0123456789abcdef01234567", "dub", nil)
			if err != nil {
				t.Fatal(err)
			}
			if crash {
				tor.cmd.Process.Kill()
			} else {
				tor.stop()
			}
			select {
			case <-tor.done:
			case <-time.After(2 * time.Second):
				t.Fatal("helper did not exit")
			}
			dirs, _ := filepath.Glob(filepath.Join(root, "otakase-room-torrent-*"))
			if len(dirs) != 0 {
				t.Fatalf("leaked directories: %v", dirs)
			}
		})
	}
}

func TestWatchTorrentReclaimsOnlyUnlockedDirs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	stale, _ := os.MkdirTemp(root, "otakase-room-torrent-")
	live, _ := os.MkdirTemp(root, "otakase-room-torrent-")
	os.WriteFile(filepath.Join(stale, ".lock"), nil, 0600)
	lock, err := os.OpenFile(filepath.Join(live, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	cleanWatchTorrentDirs()
	if _, err = os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale directory retained")
	}
	if _, err = os.Stat(live); err != nil {
		t.Fatal("live directory removed")
	}
}

func TestWatchTorrentReportsProgress(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	helper := filepath.Join(root, "helper")
	script := "#!/bin/sh\necho 'progress Connecting to source'\nprintf 'progress Reading the video \\033[31m(4%%)\\n'\necho http://127.0.0.1:12345/playlist.m3u8\nread line\n"
	os.WriteFile(helper, []byte(script), 0700)
	var steps []string
	tor, link, err := startWatchTorrent(context.Background(), helper, "0123456789abcdef0123456789abcdef01234567", "sub", func(step string) { steps = append(steps, step) })
	if err != nil {
		t.Fatal(err)
	}
	defer tor.stop()
	if link != "http://127.0.0.1:12345/playlist.m3u8" {
		t.Fatalf("link %q", link)
	}
	if len(steps) != 2 || steps[0] != "Connecting to source" || steps[1] != "Reading the video [31m(4%)" {
		t.Fatalf("steps %q", steps)
	}
}

func TestWatchHelperNotesReachLog(t *testing.T) {
	l := &botLimitedLog{limit: 64}
	l.Write([]byte("note before the playlist\n"))
	var lines []string
	l.follow(func(line string) { lines = append(lines, line) })
	l.Write([]byte("[client] a peer went away\nnote segment 3 (sub) fai"))
	l.Write([]byte("led after 45s\n\x1b[31mnote x\nnote download 4%\n"))
	for i := 0; i < botEchoLines+5; i++ {
		l.Write([]byte("note again\n"))
	}
	if len(lines) != botEchoLines || lines[0] != "segment 3 (sub) failed after 45s" || lines[1] != "download 4%" {
		t.Fatalf("%d lines: %q", len(lines), lines[:min(3, len(lines))])
	}
	if !strings.Contains(l.String(), "note again") || len(l.String()) > 64 {
		t.Fatalf("tail %q", l.String())
	}
}
