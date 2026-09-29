package internal

import (
	"errors"
	"strings"
	"testing"
)

// When the device never fetched anything, the cause is almost always a host
// firewall, and the fix is one command. Printing it beats describing it.
func TestCastFirewallHintNamesThePortAndSubnet(t *testing.T) {
	hint := castFirewallHint(&Config{CastPort: 8010}, "192.168.0.115", "ufw")

	for _, want := range []string{"8010", "192.168.0.0/24", "ufw allow"} {
		if !strings.Contains(hint, want) {
			t.Errorf("the hint does not contain %q:\n%s", want, hint)
		}
	}
}

// With no fixed port there is no single port to allow, so the hint has to ask
// for CastPort first rather than print a command that cannot be written.
func TestCastFirewallHintAsksForAPortWhenThereIsNone(t *testing.T) {
	hint := castFirewallHint(&Config{CastPort: 0}, "192.168.0.115", "ufw")

	if !strings.Contains(hint, "CastPort") {
		t.Errorf("the hint does not mention CastPort:\n%s", hint)
	}
	if strings.Contains(hint, "port 0") {
		t.Errorf("the hint offers a command for port 0:\n%s", hint)
	}
}

// firewalld takes a different command, and a ufw command would not work there.
func TestCastFirewallHintMatchesTheFirewall(t *testing.T) {
	hint := castFirewallHint(&Config{CastPort: 8010}, "192.168.0.115", "firewalld")

	if strings.Contains(hint, "ufw") {
		t.Errorf("a firewalld host was given a ufw command:\n%s", hint)
	}
	if !strings.Contains(hint, "firewall-cmd") {
		t.Errorf("the hint does not use firewall-cmd:\n%s", hint)
	}
}

// The command handed to the clipboard has to be the runnable line by itself --
// pasting a sentence in front of "sudo ufw allow ..." into a terminal would
// either fail or, with the wrong sentence, run something unintended.
func TestCastFirewallCommandIsBareAndRunnable(t *testing.T) {
	_, command := castFirewallHintWithCommand(&Config{CastPort: 8010}, "192.168.0.115", "ufw")

	if !strings.HasPrefix(command, "sudo ufw allow") {
		t.Errorf("the bare command is not runnable on its own: %q", command)
	}
	if strings.Contains(command, "\n") {
		t.Errorf("the bare command spans lines, which a paste cannot run as one: %q", command)
	}
}

// The unset-CastPort case has no command to run yet -- the fix is a config
// edit -- and nothing should be offered to copy for it.
func TestCastFirewallCommandIsEmptyWithNoFixedPort(t *testing.T) {
	_, command := castFirewallHintWithCommand(&Config{CastPort: 0}, "192.168.0.115", "ufw")

	if command != "" {
		t.Errorf("a command was offered with no fixed port: %q", command)
	}
}

// firewalld's fix is two commands. Both have to survive into one clipboard
// paste, joined so a shell runs them in order rather than only the first line.
func TestCastFirewallCommandJoinsBothFirewalldSteps(t *testing.T) {
	_, command := castFirewallHintWithCommand(&Config{CastPort: 8010}, "192.168.0.115", "firewalld")

	for _, want := range []string{"--add-port=8010", "--runtime-to-permanent"} {
		if !strings.Contains(command, want) {
			t.Errorf("the firewalld command is missing %q: %q", want, command)
		}
	}
}

// With no command to run, there is nothing to say about the clipboard either --
// appending a copy note to the CastPort-unset message would promise something
// that did not happen.
func TestCastCopyFirewallCommandSkipsTheNoteWithNoCommand(t *testing.T) {
	got := castCopyFirewallCommand("", "Set CastPort first.")
	if got != "Set CastPort first." {
		t.Errorf("a message with no command was changed: %q", got)
	}
}

// A failed copy -- no clipboard on a headless machine, the ordinary case on a
// server -- must not lose the command, and must not be reported as done when
// it was not.
func TestCastCopyFirewallCommandFailsSafelyToACopyableLine(t *testing.T) {
	previous := clipboardWriteAll
	clipboardWriteAll = func(string) error { return errors.New("no clipboard on this machine") }
	defer func() { clipboardWriteAll = previous }()

	command := "sudo ufw allow from 192.168.0.0/24 to any port 8010"
	got := castCopyFirewallCommand(command, "Allow the port:\n  "+command)

	// The command itself must still be in the text somewhere -- the one thing a
	// viewer with no working clipboard still needs to be able to read and type.
	if !strings.Contains(got, command) {
		t.Errorf("the command did not survive: %q", got)
	}
	if strings.Contains(got, "Copied to your clipboard") {
		t.Errorf("a copy that failed was reported as done: %q", got)
	}
}

// The success path is the one worth checking against the real package too,
// once, so the two never quietly drift apart.
func TestCastCopyFirewallCommandReportsSuccess(t *testing.T) {
	previous := clipboardWriteAll
	written := ""
	clipboardWriteAll = func(text string) error { written = text; return nil }
	defer func() { clipboardWriteAll = previous }()

	command := "sudo ufw allow from 192.168.0.0/24 to any port 8010"
	got := castCopyFirewallCommand(command, "Allow the port:\n  "+command)

	if written != command {
		t.Errorf("wrote %q to the clipboard, want the bare command %q", written, command)
	}
	if !strings.Contains(got, "Copied to your clipboard") {
		t.Errorf("a successful copy did not say so: %q", got)
	}
}

// A /24 is the common case but not the only one; the subnet comes from the
// address the server actually bound.
func TestCastLocalSubnet(t *testing.T) {
	for _, tc := range []struct{ addr, want string }{
		{"192.168.0.115", "192.168.0.0/24"},
		{"10.1.2.3", "10.1.2.0/24"},
		{"", ""},
		{"not-an-address", ""},
	} {
		if got := castLocalSubnet(tc.addr); got != tc.want {
			t.Errorf("castLocalSubnet(%q) = %q, want %q", tc.addr, got, tc.want)
		}
	}
}

// The hint shown while a cast is still waiting copies the fix at once, so it is
// on the clipboard even if the viewer closes the window before the grace runs
// out, and it names the port on its first line, which is the one a narrow
// panel keeps.
func TestCastWaitingFirewallHintCopiesTheFix(t *testing.T) {
	previous := clipboardWriteAll
	written := ""
	clipboardWriteAll = func(text string) error { written = text; return nil }
	defer func() { clipboardWriteAll = previous }()

	got := castWaitingFirewallHint(&Config{CastPort: 8010}, "192.168.1.20", "ufw", "Living Room")

	if !strings.Contains(written, "port 8010") {
		t.Errorf("wrote %q to the clipboard, want the ufw command for port 8010", written)
	}
	first, rest, _ := strings.Cut(got, "\n")
	if !strings.Contains(first, "Living Room") || !strings.Contains(first, "8010") {
		t.Errorf("the first line does not name the device and port: %q", first)
	}
	if !strings.Contains(rest, "clipboard") {
		t.Errorf("a successful copy did not say so: %q", got)
	}
}

func TestCastWaitingFirewallHintWithoutCopyShowsTheCommand(t *testing.T) {
	previous := clipboardWriteAll
	clipboardWriteAll = func(string) error { return errors.New("no clipboard on this machine") }
	defer func() { clipboardWriteAll = previous }()

	got := castWaitingFirewallHint(&Config{CastPort: 8010}, "192.168.1.20", "ufw", "Living Room")

	if !strings.Contains(got, "sudo ufw allow") || strings.Contains(got, "clipboard") {
		t.Errorf("a failed copy should leave the command readable and claim nothing: %q", got)
	}
}

// With no fixed port there is nothing to allow, so nothing is copied.
func TestCastWaitingFirewallHintWithoutAPortAsksForOne(t *testing.T) {
	previous := clipboardWriteAll
	copied := false
	clipboardWriteAll = func(string) error { copied = true; return nil }
	defer func() { clipboardWriteAll = previous }()

	got := castWaitingFirewallHint(&Config{}, "192.168.1.20", "ufw", "Living Room")

	if copied {
		t.Error("copied a command with no port to allow")
	}
	if !strings.Contains(got, "CastPort") {
		t.Errorf("the hint does not say to set CastPort: %q", got)
	}
}
