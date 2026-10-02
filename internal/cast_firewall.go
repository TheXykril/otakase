package internal

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
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

// castDiscoveryFirewallRules are the commands that let a cast through the
// firewall, each as its argv, and the config keys left random that no rule
// can cover.
//
// The rules are the three ports a cast uses: the SSDP answers (DLNA), mDNS
// (Chromecast, Kodi), and the stream server the device fetches from. Each is
// limited to the LAN and, on ufw, tagged so it is easy to find and delete. A
// port left random is named in unset instead, since it cannot be allowed.
func castDiscoveryFirewallRules(config *Config, subnet, firewall string) (rules [][]string, unset []string) {
	discoveryPort, castPort := 0, 0
	if config != nil {
		discoveryPort, castPort = config.CastDiscoveryPort, config.CastPort
	}
	add := func(port int, proto string) {
		if firewall == "firewalld" {
			rules = append(rules, []string{"sudo", "firewall-cmd", "--permanent", fmt.Sprintf("--add-port=%d/%s", port, proto)})
			return
		}
		rules = append(rules, []string{"sudo", "ufw", "allow", "from", subnet, "to", "any", "port", strconv.Itoa(port), "proto", proto, "comment", "otakase cast"})
	}
	if discoveryPort > 0 {
		add(discoveryPort, "udp")
	} else {
		unset = append(unset, "CastDiscoveryPort")
	}
	add(5353, "udp")
	if castPort > 0 {
		add(castPort, "tcp")
	} else {
		unset = append(unset, "CastPort")
	}
	if firewall == "firewalld" {
		rules = append(rules, []string{"sudo", "firewall-cmd", "--reload"})
	}
	return rules, unset
}

// castShellCommand joins argvs into one line a shell runs as they were meant,
// quoting only the words that need it.
func castShellCommand(rules [][]string) string {
	lines := make([]string, 0, len(rules))
	for _, argv := range rules {
		words := make([]string, len(argv))
		for i, word := range argv {
			if strings.ContainsAny(word, " '\"$\\") {
				word = "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
			}
			words[i] = word
		}
		lines = append(lines, strings.Join(words, " "))
	}
	return strings.Join(lines, " && ")
}

// castDiscoveryFirewallHint is what to add to "no devices found" when a host
// firewall is on, plus the bare command to put on the clipboard. Both are ""
// when there is no firewall to blame.
//
// Discovery answers are inbound UDP that ufw and firewalld drop by default,
// which looks exactly like an empty network.
//
// It goes into the error rather than out on its own: the search runs before
// the cast panel opens, from a terminal menu or from rofi, and the error is
// the one message every one of those shows.
func castDiscoveryFirewallHint(config *Config, localAddr, firewall string) (message, command string) {
	if firewall == "" {
		return "", ""
	}
	subnet := castLocalSubnet(localAddr)
	if subnet == "" {
		subnet = "192.168.0.0/24"
	}
	rules, unset := castDiscoveryFirewallRules(config, subnet, firewall)
	command = castShellCommand(rules)
	message = fmt.Sprintf("%s is on and may be dropping their answers. Run otakase -cast-setup to fix it, or allow them yourself:\n  %s", firewall, command)
	if len(unset) > 0 {
		message += fmt.Sprintf("\nAlso set %s to a fixed port (otakase -e).", strings.Join(unset, " and "))
	}
	return message, command
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
