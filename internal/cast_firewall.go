package internal

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/atotto/clipboard"
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
	message, _ := castFirewallHintWithCommand(config, serverAddr, firewall)
	return message
}

// castFirewallHintWithCommand is castFirewallHint, plus the bare command by
// itself. Nothing can paste "sudo ufw allow from ..." into a terminal without
// also pasting the sentence in front of it, so the two are kept apart here and
// joined only for what gets printed.
//
// command is "" when there is nothing to run yet -- the CastPort-unset case,
// where the fix is a config edit rather than a shell command.
func castFirewallHintWithCommand(config *Config, serverAddr, firewall string) (message, command string) {
	subnet := castLocalSubnet(serverAddr)
	if subnet == "" {
		subnet = "192.168.0.0/24"
	}

	var b strings.Builder
	b.WriteString("The device never connected to this machine, which usually means a firewall is dropping it.\n")

	if config == nil || config.CastPort == 0 {
		b.WriteString("Set CastPort in the config to a fixed port (otakase -e), then allow that one port.\n")
		b.WriteString("A random port cannot be allowed without opening the whole ephemeral range.")
		return b.String(), ""
	}

	switch firewall {
	case "firewalld":
		command = fmt.Sprintf("sudo firewall-cmd --add-port=%d/tcp && sudo firewall-cmd --runtime-to-permanent", config.CastPort)
		b.WriteString(fmt.Sprintf("Allow the port this cast used:\n  sudo firewall-cmd --add-port=%d/tcp\n", config.CastPort))
		b.WriteString("  sudo firewall-cmd --runtime-to-permanent")
	default:
		command = fmt.Sprintf("sudo ufw allow from %s to any port %d proto tcp comment 'otakase cast'", subnet, config.CastPort)
		b.WriteString("Allow the port this cast used:\n  " + command)
	}
	return b.String(), command
}

// castCopyFirewallCommand puts command on the clipboard and says so in
// message, so the fix survives however long the viewer actually gets to read
// the screen before it moves on.
//
// This is the reason the command is copied at all rather than just printed
// bigger: the terminal this prints into is the one the failing cast is about
// to close, whether that terminal came from a rofi launch or an ordinary one.
// The message alone would flash and be gone; the command in the clipboard
// survives into whatever terminal the viewer opens next.
//
// A failed copy is not worth failing the cast over -- a headless session with
// no clipboard to reach is the ordinary case on a server, not an error -- so it
// only changes which sentence gets appended.
//
// A variable so a test can fail the copy deterministically: whether the real
// one succeeds depends on this machine having a clipboard tool installed at
// all, which the test runner cannot assume either way.
var clipboardWriteAll = clipboard.WriteAll

func castCopyFirewallCommand(command, message string) string {
	if command == "" {
		return message
	}
	if err := clipboardWriteAll(command); err != nil {
		Log(fmt.Sprintf("cast: could not copy the firewall command to the clipboard: %v", err))
		return message + "\n(Copy the line above and run it.)"
	}
	return message + "\n(Copied to your clipboard -- paste it into a terminal and run it.)"
}

// castWaitingFirewallHint is the panel message for a device that has accepted
// the load and still not asked this machine for anything.
//
// It is shown while the cast is still waiting, well before castStartupGrace
// gives up, because the failure itself used to be the first word of it: ninety
// seconds of "Waiting for X to start…" and then a window that closed. The fix
// is copied now rather than at the failure, so it is on the clipboard even if
// the viewer closes the window instead of waiting the grace out.
func castWaitingFirewallHint(config *Config, serverAddr, firewall, device string) string {
	if config == nil || config.CastPort == 0 {
		return fmt.Sprintf("No request from %s yet -- a firewall may be blocking it.\n", device) +
			"Set CastPort (otakase -e) to a fixed port, then allow that port."
	}
	lead := fmt.Sprintf("No request from %s yet -- a firewall may be blocking port %d.\n", device, config.CastPort)
	_, command := castFirewallHintWithCommand(config, serverAddr, firewall)
	if err := clipboardWriteAll(command); err != nil {
		Log(fmt.Sprintf("cast: could not copy the firewall command to the clipboard: %v", err))
		return lead + "Run: " + command
	}
	return lead + "The fix is on your clipboard -- paste it into a terminal."
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
