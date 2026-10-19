package internal

import (
	"errors"
	"testing"
)

func TestPrepareLinkLaunch(t *testing.T) {
	found := func(string) (string, error) { return "/usr/bin/rofi", nil }
	missing := func(string) (string, error) { return "", errors.New("not found") }

	cases := []struct {
		name         string
		goos         string
		terminal     bool
		linkMenus    string
		lookPath     func(string) (string, error)
		relaunchErr  error
		wantRofi     bool
		wantRelaunch bool
		wantExit     bool
	}{
		{name: "rofi installed uses rofi", goos: "linux", linkMenus: "rofi", lookPath: found, wantRofi: true},
		{name: "unset uses rofi", goos: "linux", lookPath: found, wantRofi: true},
		{name: "no rofi reopens in a terminal", goos: "linux", linkMenus: "rofi", lookPath: missing, wantRelaunch: true, wantExit: true},
		{name: "terminal chosen reopens in a terminal", goos: "linux", linkMenus: "Terminal", lookPath: found, wantRelaunch: true, wantExit: true},
		{name: "already in a terminal stays", goos: "linux", terminal: true, linkMenus: "rofi", lookPath: found},
		{name: "no terminal emulator carries on", goos: "linux", linkMenus: "terminal", lookPath: found, relaunchErr: errors.New("none"), wantRelaunch: true},
		{name: "other systems are left alone", goos: "windows", lookPath: found},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			old := linkLaunchHasTerminal
			linkLaunchHasTerminal = func() bool { return tc.terminal }
			defer func() { linkLaunchHasTerminal = old }()

			config := &Config{LinkMenus: tc.linkMenus}
			relaunched := false
			exit := prepareLinkLaunch(config, tc.goos, tc.lookPath, func(*Config) error {
				relaunched = true
				return tc.relaunchErr
			})
			if config.RofiSelection != tc.wantRofi || relaunched != tc.wantRelaunch || exit != tc.wantExit {
				t.Fatalf("rofi=%v relaunch=%v exit=%v, want %v %v %v", config.RofiSelection, relaunched, exit, tc.wantRofi, tc.wantRelaunch, tc.wantExit)
			}
		})
	}
}
