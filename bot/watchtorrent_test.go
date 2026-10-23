package main

import (
	"context"
	"os"
	"path/filepath"
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
	_, _, err := startWatchTorrent(ctx, helper, "0123456789abcdef0123456789abcdef01234567", "sub")
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
			tor, _, err := startWatchTorrent(ctx, helper, "0123456789abcdef0123456789abcdef01234567", "dub")
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
