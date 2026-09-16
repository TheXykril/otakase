package internal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"time"
)

// listenMPVClientMessages calls handler for every script-message the player
// sends, until the player goes away or done is closed.
//
// This reads the socket a line at a time rather than a buffer at a time: mpv
// writes one JSON object per line and several can arrive in a single read, so
// parsing whatever a read happened to return drops every message but the first.
func listenMPVClientMessages(socket string, done <-chan struct{}, handler func(args []string)) {
	if socket == "" || socket == "android-intent" || handler == nil {
		return
	}

	conn, err := connectToPipe(socket)
	if err != nil {
		Log(fmt.Sprintf("skip marker: could not listen to the player: %v", err))
		return
	}

	go func() {
		<-done
		conn.Close()
	}()
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		select {
		case <-done:
			return
		default:
		}

		var message struct {
			Event string        `json:"event"`
			Args  []interface{} `json:"args"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			continue
		}
		if message.Event != "client-message" {
			continue
		}

		args := make([]string, 0, len(message.Args))
		for _, arg := range message.Args {
			if text, ok := arg.(string); ok {
				args = append(args, text)
			}
		}
		if len(args) > 0 {
			handler(args)
		}
	}
}

// mpvFloatProperty reads a numeric property, which is how the marker learns
// where the viewer is in the episode.
func mpvFloatProperty(socket, name string) (float64, error) {
	response, err := MPVSendCommand(socket, []interface{}{"get_property", name})
	if err != nil {
		return 0, err
	}
	value, ok := response.(float64)
	if !ok {
		return 0, fmt.Errorf("the player did not answer with a number for %s", name)
	}
	return value, nil
}

// mpvShowText puts a line on the player's own screen.
//
// Everything this feature says has to go here: mpv is what the viewer is
// looking at, and a message printed to the terminal behind it is a message
// nobody reads.
func mpvShowText(socket, text string, duration time.Duration) {
	milliseconds := int(duration / time.Millisecond)
	if _, err := MPVSendCommand(socket, []interface{}{"show-text", text, milliseconds}); err != nil {
		Log(fmt.Sprintf("skip marker: could not show %q: %v", text, err))
	}
}
