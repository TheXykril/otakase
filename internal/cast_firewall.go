package internal

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
)

// castDetectFirewall names the host firewall that is running, or "" when none
// is found.
//
// Detection is by configuration rather than by asking the firewall, because
// asking needs root and a media player has no business holding it. ufw's own
// config says whether it is enabled; firewalld is a service, so its presence
// on PATH plus a running unit is the closest read available unprivileged.
func castDetectFirewall() string {
	if data, err := os.ReadFile("/etc/ufw/ufw.conf"); err == nil {
		if strings.Contains(strings.ToUpper(string(data)), "ENABLED=YES") {
			return "ufw"
		}
	}
	if _, err := exec.LookPath("firewall-cmd"); err == nil {
		if out, err := exec.Command("systemctl", "is-active", "firewalld").Output(); err == nil {
			if strings.TrimSpace(string(out)) == "active" {
				return "firewalld"
			}
		}
	}
	return ""
}

// castLocalSubnet turns this machine's address into the /24 its devices are on,
// or "" if it cannot.
//
// A /24 is an assumption, and a safe one to print: it is what home networks
// use, and the viewer reads the command before running it.
func castLocalSubnet(addr string) string {
	ip := net.ParseIP(strings.TrimSpace(addr))
	if ip == nil {
		return ""
	}
	v4 := ip.To4()
	if v4 == nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d.0/24", v4[0], v4[1], v4[2])
}

// castFirewallHint is what to tell a viewer whose device never connected.
//
// It prints the command rather than running it. Allowing a port needs root,
// and a program that plays anime should not be able to change a firewall as a
// side effect of watching an episode -- nor should it prompt for a password to
// do so. The viewer runs one line they can read first.
func castFirewallHint(config *Config, serverAddr, firewall string) string {
	subnet := castLocalSubnet(serverAddr)
	if subnet == "" {
		subnet = "192.168.0.0/24"
	}

	var b strings.Builder
	b.WriteString("The device never connected to this machine, which usually means a firewall is dropping it.\n")

	if config == nil || config.CastPort == 0 {
		b.WriteString("Set CastPort in the config to a fixed port (otakase -e), then allow that one port.\n")
		b.WriteString("A random port cannot be allowed without opening the whole ephemeral range.")
		return b.String()
	}

	switch firewall {
	case "firewalld":
		b.WriteString(fmt.Sprintf("Allow the port this cast used:\n  sudo firewall-cmd --add-port=%d/tcp\n", config.CastPort))
		b.WriteString("  sudo firewall-cmd --runtime-to-permanent")
	default:
		b.WriteString(fmt.Sprintf("Allow the port this cast used:\n  sudo ufw allow from %s to any port %d proto tcp comment 'otakase cast'",
			subnet, config.CastPort))
	}
	return b.String()
}

// castServerHost is the address out of a server URL, for building a firewall
// hint that names this machine's own subnet.
func castServerHost(rawURL string) string {
	trimmed := strings.TrimPrefix(rawURL, "http://")
	if i := strings.IndexByte(trimmed, '/'); i >= 0 {
		trimmed = trimmed[:i]
	}
	host, _, err := net.SplitHostPort(trimmed)
	if err != nil {
		return trimmed
	}
	return host
}
