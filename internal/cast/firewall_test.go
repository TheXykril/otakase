package cast

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestFirewallCommand(t *testing.T) {
	ports := []FirewallPort{{8010, "tcp"}, {8011, "udp"}}
	name, args, ok := FirewallCommand("ufw", "192.168.0.0/24", ports)
	script := strings.Join(args, " ")
	if !ok || name != "pkexec" ||
		!strings.Contains(script, "ufw allow from 192.168.0.0/24 to any port 8010 proto tcp comment 'otakase cast'") ||
		!strings.Contains(script, "port 8011 proto udp") {
		t.Fatalf("ufw: %s %q", name, args)
	}
	name, args, ok = FirewallCommand("firewalld", "", ports)
	script = strings.Join(args, " ")
	if !ok || name != "pkexec" || !strings.Contains(script, "--add-port=8010/tcp") ||
		!strings.Contains(script, "--add-port=8011/udp") || !strings.HasSuffix(script, "firewall-cmd --reload") {
		t.Fatalf("firewalld: %s %q", name, args)
	}
	name, args, ok = FirewallCommand("windows", "", ports)
	script = strings.Join(args, " ")
	if !ok || name != "powershell" || !strings.Contains(script, "-Verb RunAs") ||
		!strings.Contains(script, "protocol=TCP localport=8010") || !strings.Contains(script, "protocol=UDP localport=8011") {
		t.Fatalf("windows: %s %q", name, args)
	}
	if _, _, ok := FirewallCommand("", "", ports); ok {
		t.Fatal("no firewall still gave a command")
	}
}

func TestAllowThroughFirewall(t *testing.T) {
	firewall, run := Firewall, runFirewall
	defer func() { Firewall, runFirewall = firewall, run }()
	Firewall = func() string { return "" }
	if err := AllowThroughFirewall(context.Background()); !errors.Is(err, ErrNoFirewall) {
		t.Fatalf("no firewall: %v", err)
	}
	var ran []string
	Firewall = func() string { return "ufw" }
	runFirewall = func(_ context.Context, name string, args []string) error {
		ran = append([]string{name}, args...)
		return nil
	}
	if err := AllowThroughFirewall(context.Background()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(ran, " ")
	for _, p := range FirewallPorts() {
		if !strings.Contains(joined, " "+strconv.Itoa(p.Port)+" proto "+p.Proto) {
			t.Fatalf("port %d/%s missing from %s", p.Port, p.Proto, joined)
		}
	}
}

func TestSetDiscoveryInterface(t *testing.T) {
	defer SetDiscoveryInterface("", 0)
	SetDiscoveryInterface("wlan0", 30)
	iface := currentDiscoveryInterface()
	if iface == nil || iface.Name != "wlan0" || iface.Index != 30 {
		t.Fatalf("interface %+v", iface)
	}
	if len(zeroconfOptions()) != 2 {
		t.Fatal("interface not passed to mDNS")
	}
	SetDiscoveryInterface("", 0)
	if currentDiscoveryInterface() != nil || len(zeroconfOptions()) != 1 {
		t.Fatal("interface not cleared")
	}
}
