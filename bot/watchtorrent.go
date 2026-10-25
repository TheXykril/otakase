package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const watchTorrentStartWait = 11 * time.Minute

type watchTorrent struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	done  chan struct{}
	once  sync.Once
}

func (t *watchTorrent) stop() {
	if t != nil {
		t.once.Do(func() {
			_ = t.stdin.Close()
			select {
			case <-t.done:
			case <-time.After(5 * time.Second):
				if t.cmd.Process != nil {
					_ = syscall.Kill(-t.cmd.Process.Pid, syscall.SIGKILL)
				}
				<-t.done
			}
		})
	}
}

var watchStartTorrent = startWatchTorrent
var watchTorrentSlot = make(chan struct{}, 1)

func startWatchTorrent(ctx context.Context, cli, hash, mode string) (*watchTorrent, string, error) {
	if len(hash) != 40 || strings.Trim(hash, "0123456789abcdefABCDEF") != "" {
		return nil, "", errors.New("invalid torrent infohash")
	}
	select {
	case watchTorrentSlot <- struct{}{}:
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
	released := false
	defer func() {
		if !released {
			<-watchTorrentSlot
		}
	}()
	cmd := exec.Command(cli, "-torrent-serve", hash, mode)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, "", err
	}
	stderr := &botLimitedLog{limit: 8192}
	cmd.Stderr = stderr
	cleanWatchTorrentDirs()
	home, err := os.MkdirTemp("", "otakase-room-torrent-")
	if err != nil {
		return nil, "", err
	}
	lock, err := os.OpenFile(filepath.Join(home, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		os.RemoveAll(home)
		return nil, "", err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		os.RemoveAll(home)
		return nil, "", err
	}
	cmd.ExtraFiles = []*os.File{lock}
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+home, "TMPDIR="+home, "GOMAXPROCS=2", "OTAKASE_ROOM_DIR="+home)
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = os.RemoveAll(home)
		return nil, "", err
	}
	released = true
	t := &watchTorrent{cmd: cmd, stdin: stdin, done: make(chan struct{})}
	ready := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(stdout).ReadString('\n')
		ready <- strings.TrimSpace(line)
	}()
	go func() {
		_ = cmd.Wait()
		// Kill any encoder left behind if the helper crashed.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = stdin.Close()
		_ = os.RemoveAll(home)
		<-watchTorrentSlot
		close(t.done)
	}()
	timer := time.NewTimer(watchTorrentStartWait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.stop()
		return nil, "", ctx.Err()
	case link := <-ready:
		u, err := url.Parse(link)
		if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Path != "/playlist.m3u8" {
			t.stop()
			if detail := strings.TrimSpace(stderr.String()); detail != "" {
				lines := strings.Split(detail, "\n")
				return nil, "", fmt.Errorf("torrent preparation failed: %s", strings.TrimSpace(lines[len(lines)-1]))
			}
			return nil, "", fmt.Errorf("torrent helper exited without a valid playback address")
		}
		go func() {
			select {
			case <-ctx.Done():
				t.stop()
			case <-t.done:
			}
		}()
		return t, link, nil
	case <-timer.C:
		t.stop()
		return nil, "", errors.New("torrent source did not start in time")
	}
}

// Locks are inherited by helpers, so restarting the bot never removes a live
// room's files. Dead process groups leave an unlocked directory to reclaim.
func cleanWatchTorrentDirs() {
	dirs, _ := filepath.Glob(filepath.Join(os.TempDir(), "otakase-room-torrent-*"))
	for _, dir := range dirs {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		lock, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_RDWR, 0600)
		if err != nil {
			continue
		}
		if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
			_ = os.RemoveAll(dir)
		}
		lock.Close()
	}
}

type botLimitedLog struct {
	mu    sync.Mutex
	data  []byte
	limit int
}

func (l *botLimitedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(p) >= l.limit {
		l.data = append(l.data[:0], p[len(p)-l.limit:]...)
	} else {
		excess := len(l.data) + len(p) - l.limit
		if excess > 0 {
			l.data = append(l.data[:0], l.data[excess:]...)
		}
		l.data = append(l.data, p...)
	}
	return len(p), nil
}

func (l *botLimitedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return string(l.data)
}
