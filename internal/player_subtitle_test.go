package internal

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestMPVFileReadyWaitsForProperties(t *testing.T) {
	const stream = "https://cdn.example/episode.m3u8"
	var asked int
	// path is unavailable for the first two polls, as it is while mpv is
	// still opening the file.
	fakeMPV := func(_ string, cmd []interface{}) (interface{}, error) {
		prop, _ := cmd[1].(string)
		if prop == "duration" {
			return 1500.0, nil
		}
		if prop != "path" {
			return nil, fmt.Errorf("fake mpv got %v", cmd)
		}
		if asked++; asked <= 2 {
			return nil, errors.New("property unavailable")
		}
		return stream, nil
	}
	if err := waitForMPVFileReadyWith(fakeMPV, "socket", stream, 100*time.Millisecond, time.Millisecond); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if asked != 3 {
		t.Fatalf("path asked %d times, want 3", asked)
	}
}

func TestClearMPVSubtitleFilesEmptiesTheGlobalList(t *testing.T) {
	var got []interface{}
	send := func(socket string, command []interface{}) (interface{}, error) {
		got = command
		return nil, nil
	}
	clearMPVSubtitleFiles(send, "sock")
	want := []interface{}{"change-list", "sub-files", "clr", ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command = %v, want %v", got, want)
	}
}
