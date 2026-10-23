package main

import (
 "fmt"
 "strings"
 "log"
 "sync"
 "time"
)

type watchDiagnostic struct {
 Code string `json:"code"`
 At int64 `json:"at"`
 Show string `json:"show"`
 Episode int `json:"episode"`
 Stage string `json:"stage"`
 Detail string `json:"detail"`
}

var watchDiagnostics = struct {
 sync.Mutex
 next uint64
 entries []watchDiagnostic
}{}

// Recent failures are in-memory only; they are available through the
// authenticated Home Assistant dashboard, never the public Activity API.
func recordWatchFailure(show string, episode int, stage string, err error) string {
 watchDiagnostics.Lock()
 defer watchDiagnostics.Unlock()
 watchDiagnostics.next++
 code := fmt.Sprintf("W-%06d", watchDiagnostics.next)
 entry := watchDiagnostic{Code: code, At: time.Now().UnixMilli(), Show: show, Episode: episode, Stage: stage, Detail: err.Error()}
 watchDiagnostics.entries = append(watchDiagnostics.entries, entry)
 if len(watchDiagnostics.entries) > 30 {
  watchDiagnostics.entries = watchDiagnostics.entries[len(watchDiagnostics.entries)-30:]
 }
 log.Printf("watch room %s: %s episode %d (%s): %v", code, show, episode, stage, err)
 return code
}

func recentWatchDiagnostics() []watchDiagnostic {
 watchDiagnostics.Lock()
 defer watchDiagnostics.Unlock()
 out := make([]watchDiagnostic, len(watchDiagnostics.entries))
 for i, item := range watchDiagnostics.entries {
  out[len(out)-i-1] = item
 }
 return out
}

func publicWatchError(err error) string {
 msg := strings.ToLower(err.Error())
 switch {
 case strings.Contains(msg, "timed out"), strings.Contains(msg, "deadline exceeded"), strings.Contains(msg, "timeout"):
  return "Source lookup or preparation timed out"
 case strings.Contains(msg, "no release"), strings.Contains(msg, "not found"), strings.Contains(msg, "no episode"), strings.Contains(msg, "no playable"), strings.Contains(msg, "no source"), strings.Contains(msg, "no links"):
  return "No playable episode source was found"
 case strings.Contains(msg, "torrent"), strings.Contains(msg, "download"), strings.Contains(msg, "peer"), strings.Contains(msg, "probe"), strings.Contains(msg, "encode"), strings.Contains(msg, "audio"), strings.Contains(msg, "subtitle"):
  return "Torrent or video preparation failed"
 default:
  return "Episode could not start"
 }
}
