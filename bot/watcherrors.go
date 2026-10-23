package main

import (
	"fmt"
	"log"
	"strings"
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
 code := fmt.Sprintf("W-%d-%03d", time.Now().Unix(), watchDiagnostics.next%1000)
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
  return "Episode preparation took too long"
 case strings.Contains(msg, "no release"), strings.Contains(msg, "not found"), strings.Contains(msg, "no episode"), strings.Contains(msg, "no playable"), strings.Contains(msg, "no source"), strings.Contains(msg, "no links"):
  return "Episode is not available in the catalog"
 case strings.Contains(msg, "torrent"), strings.Contains(msg, "download"), strings.Contains(msg, "peer"), strings.Contains(msg, "probe"), strings.Contains(msg, "encode"), strings.Contains(msg, "audio"), strings.Contains(msg, "subtitle"):
  return "Playback relay could not prepare the episode"
 default:
  return "Episode could not start"
 }
}
