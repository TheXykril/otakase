package internal

import (
	"os"
	"runtime/debug"
	"sync"
)

// Noticing a crash. Every interactive run leaves a small "still running"
// marker in the storage directory, and Go writes a crash's stack beside it;
// a clean exit removes both. A marker whose process is gone at the next
// launch means that run did not end well (a panic, or the process was
// killed), and that launch asks once whether to send a report to the
// Otakase Discord, through the usual report flow with the crash filled in.

const ()

var crashWatch struct {
	sync.Mutex
	marker string
	output *os.File
}

// EndCrashWatch marks this run as ended well. It runs on every exit.
func EndCrashWatch() {
	crashWatch.Lock()
	defer crashWatch.Unlock()
	if crashWatch.output != nil {
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
		name := crashWatch.output.Name()
		_ = crashWatch.output.Close()
		_ = os.Remove(name)
		crashWatch.output = nil
	}
	if crashWatch.marker != "" {
		_ = os.Remove(crashWatch.marker)
		crashWatch.marker = ""
	}
}
