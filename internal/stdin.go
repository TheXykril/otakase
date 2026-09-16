package internal

import (
	"bufio"
	"os"
	"sync"
)

// stdinLines is kept between calls. A reader built for a single read throws
// away whatever it buffered past the line it returned, so a second read taken
// straight after the first would lose an answer already typed -- pasting two
// lines, or answering ahead of the question.
var (
	stdinLinesMu sync.Mutex
	stdinLines   *bufio.Reader
	stdinSource  *os.File
)

func stdinLineReader() *bufio.Reader {
	stdinLinesMu.Lock()
	defer stdinLinesMu.Unlock()
	// os.Stdin is replaced in tests, so the reader is rebuilt when it changes
	// rather than being bound once to whatever it was at startup.
	if stdinLines == nil || stdinSource != os.Stdin {
		stdinLines = bufio.NewReader(os.Stdin)
		stdinSource = os.Stdin
	}
	return stdinLines
}

// AwaitEnter waits for enter to be pressed, and reports whether it was.
//
// It returns false when stdin has ended, which is not someone pressing enter.
// The waits that use this sit in loops that start the next episode each time
// round, and a stdin that answers instantly and forever would spin them --
// opening a player per turn, with nobody there to watch it.
func AwaitEnter() bool {
	_, err := stdinLineReader().ReadString('\n')
	return err == nil
}
