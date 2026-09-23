package internal

import (
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
