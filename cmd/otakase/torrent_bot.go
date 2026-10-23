package main

import (
 "fmt"
 "io"
 "net"
 "net/http"
 "os"
 "os/exec"
 "path/filepath"
 "regexp"
 "time"

 "github.com/thexykril/otakase/internal/torrentstream"
)

var torrentBotHash = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

// serveTorrentForBot keeps the torrent client and its browser-compatible HLS
// transcode running while the parent bot holds stdin open.
func serveTorrentForBot(hash string) error {
 if !torrentBotHash.MatchString(hash) { return fmt.Errorf("invalid torrent infohash") }
 defer torrentstream.Shutdown()
 source, err := torrentstream.Stream(hash)
 if err != nil { return err }
 dir, err := os.MkdirTemp("", "otakase-room-hls-")
 if err != nil { return err }
 defer os.RemoveAll(dir)
 listener, err := net.Listen("tcp", "127.0.0.1:0")
 if err != nil { return err }
 defer listener.Close()
 server := &http.Server{Handler: http.FileServer(http.Dir(dir))}
 go func() { _ = server.Serve(listener) }()
 defer server.Close()
 args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-i", source,
  "-map", "0:v:0", "-map", "0:a:0?", "-vf", "scale=w=min(1280\\,iw):h=min(720\\,ih):force_original_aspect_ratio=decrease",
  "-c:v", "libx264", "-preset", "veryfast", "-crf", "26", "-c:a", "aac", "-b:a", "128k",
  "-f", "hls", "-hls_time", "4", "-hls_list_size", "0", "-hls_playlist_type", "event",
  "-hls_segment_filename", filepath.Join(dir, "seg%05d.ts"), filepath.Join(dir, "playlist.m3u8")}
 cmd := exec.Command("ffmpeg", args...)
 cmd.Stderr = io.Discard
 if err := cmd.Start(); err != nil { return fmt.Errorf("torrent video requires ffmpeg: %w", err) }
 defer func() { if cmd.Process != nil { _ = cmd.Process.Kill() }; _ = cmd.Wait() }()
 deadline := time.NewTimer(90 * time.Second)
 defer deadline.Stop()
 tick := time.NewTicker(200 * time.Millisecond)
 defer tick.Stop()
 for {
  select {
  case <-deadline.C: return fmt.Errorf("torrent video did not start in time")
  case <-tick.C:
   if stat, err := os.Stat(filepath.Join(dir, "playlist.m3u8")); err == nil && stat.Size() > 0 {
    fmt.Printf("http://127.0.0.1:%d/playlist.m3u8\n", listener.Addr().(*net.TCPAddr).Port)
    _, _ = io.Copy(io.Discard, os.Stdin)
    return nil
   }
  }
 }
}
