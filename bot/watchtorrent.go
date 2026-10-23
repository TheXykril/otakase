package main

import (
 "bufio"
 "errors"
 "fmt"
 "io"
 "net/url"
 "os"
 "os/exec"
 "strings"
 "sync"
 "time"
)

const watchTorrentStartWait = 65 * time.Second

type watchTorrent struct {
 cmd *exec.Cmd
 once sync.Once
}

func (t *watchTorrent) stop() {
 if t != nil {
  t.once.Do(func() { if t.cmd.Process != nil { _ = t.cmd.Process.Kill() } })
 }
}

var watchStartTorrent = startWatchTorrent

func startWatchTorrent(cli, hash string) (*watchTorrent, string, error) {
 if len(hash) != 40 || strings.Trim(hash, "0123456789abcdefABCDEF") != "" {
  return nil, "", errors.New("invalid torrent infohash")
 }
 cmd := exec.Command(cli, "-torrent-serve", hash)
 stdout, err := cmd.StdoutPipe()
 if err != nil { return nil, "", err }
 stdin, err := cmd.StdinPipe()
 if err != nil { return nil, "", err }
 cmd.Stderr = io.Discard
 home, err := os.MkdirTemp("", "otakase-room-torrent-")
 if err != nil { return nil, "", err }
 cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+home)
 if err := cmd.Start(); err != nil {
  _ = os.RemoveAll(home)
  return nil, "", err
 }
 t := &watchTorrent{cmd: cmd}
 ready := make(chan string, 1)
 go func() {
  line, _ := bufio.NewReader(stdout).ReadString('\n')
  ready <- strings.TrimSpace(line)
 }()
 go func() {
  _ = cmd.Wait()
  _ = stdin.Close()
  _ = os.RemoveAll(home)
 }()
 timer := time.NewTimer(watchTorrentStartWait)
 defer timer.Stop()
 select {
 case link := <-ready:
  u, err := url.Parse(link)
  if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || strings.TrimPrefix(u.Path, "/") != strings.ToLower(hash) {
   t.stop()
   return nil, "", fmt.Errorf("torrent helper returned invalid address")
  }
  return t, link, nil
 case <-timer.C:
  t.stop()
  return nil, "", errors.New("torrent source did not start in time")
 }
}
