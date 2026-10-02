package internal

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Random ports are the one thing a rule cannot cover, so setup fixes them in
// the config file as well as in memory.
func TestCastSetupFixPortsWritesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "otakase.conf")
	if err := os.WriteFile(path, []byte("CastPort=0\nCastDiscoveryPort=0\nPlayer=mpv\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := &Config{}
	if err := castSetupFixPorts(io.Discard, config, path); err != nil {
		t.Fatal(err)
	}
	if config.CastPort != 8010 || config.CastDiscoveryPort != 8011 {
		t.Errorf("ports %d/%d, want 8010/8011", config.CastPort, config.CastDiscoveryPort)
	}
	saved, err := LoadConfigFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved["CastPort"] != "8010" || saved["CastDiscoveryPort"] != "8011" || saved["Player"] != "mpv" {
		t.Errorf("saved config: %v", saved)
	}
}

// Ports the viewer chose are theirs: setup leaves them, and the file alone.
func TestCastSetupFixPortsKeepsChosenPorts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.conf")
	config := &Config{CastPort: 9000, CastDiscoveryPort: 9001}
	if err := castSetupFixPorts(io.Discard, config, path); err != nil {
		t.Fatal(err)
	}
	if config.CastPort != 9000 || config.CastDiscoveryPort != 9001 {
		t.Errorf("ports changed to %d/%d", config.CastPort, config.CastDiscoveryPort)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("wrote a config with nothing to change")
	}
}

func TestCastSetupConfirm(t *testing.T) {
	for input, want := range map[string]bool{"\n": true, "y\n": true, "YES\n": true, "n\n": false, "no\n": false, "": false} {
		stdinFromString(t, input)
		if got := castSetupConfirm(io.Discard, "? "); got != want {
			t.Errorf("answer %q: got %v, want %v", input, got, want)
		}
	}
}

// The rules run in order and stop at the first failure, naming it.
func TestCastSetupRunStopsAtFirstFailure(t *testing.T) {
	var ran []string
	fake := func(name string, args ...string) *exec.Cmd {
		ran = append(ran, strings.Join(append([]string{name}, args...), " "))
		if args[len(args)-1] == "bad" {
			return exec.Command("false")
		}
		return exec.Command("true")
	}
	err := castSetupRun([][]string{{"sudo", "ok"}, {"sudo", "bad"}, {"sudo", "never"}}, "pw", fake)
	if err == nil || !strings.Contains(err.Error(), "sudo bad") {
		t.Errorf("error %v, want one naming sudo bad", err)
	}
	if len(ran) != 2 || ran[0] != "sudo -S -p  ok" {
		t.Errorf("ran %v, want to stop after the failure", ran)
	}
}

// The printed line pastes into a shell as the argv setup runs.
func TestCastShellCommandQuotes(t *testing.T) {
	got := castShellCommand([][]string{{"sudo", "ufw", "allow", "comment", "otakase cast"}, {"sudo", "x"}})
	want := "sudo ufw allow comment 'otakase cast' && sudo x"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A rofi cast's own terminal has no menu to ask in, so it is never offered.
func TestCastNoDevicesMenuSkipsNonInteractive(t *testing.T) {
	if castNoDevicesMenu(&Config{CastNonInteractive: true}, "ufw") {
		t.Error("asked with no menu to ask in")
	}
}

// The password prompt lists every command it will run, before the ask.
func TestCastSetupPasswordPromptListsCommands(t *testing.T) {
	rules, _ := castDiscoveryFirewallRules(&Config{CastPort: 8010, CastDiscoveryPort: 8011}, "192.168.0.0/24", "ufw")
	got := castSetupPasswordPrompt(rules)
	for _, rule := range rules {
		if !strings.Contains(got, castShellCommand([][]string{rule})) {
			t.Errorf("prompt lacks %v:\n%s", rule, got)
		}
	}
	if !strings.HasSuffix(got, "(sudo): ") {
		t.Errorf("prompt does not end on the ask:\n%s", got)
	}
}
