package cast

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// DefaultStreamPort is the port a cast device fetches the stream from.
// Fixed, so a firewall needs one rule for it: a random port cannot be allowed
// without opening the whole ephemeral range. It is the port the old command
// line app used, so a rule made for that still lets casts through.
const DefaultStreamPort = 8010

// DefaultRebuildPort is where a stream rebuilt by ffmpeg is served from when
// the relay already holds DefaultStreamPort.
const DefaultRebuildPort = 8013

// FirewallPort is one port a cast needs open.
type FirewallPort struct {
	Port  int
	Proto string
}

// FirewallPorts are the ports casting needs reachable from the LAN: the
// stream, the rebuilt stream, the browser player page, the DLNA search
// answers and mDNS.
func FirewallPorts() []FirewallPort {
	return []FirewallPort{
		{DefaultStreamPort, "tcp"},
		{DefaultRebuildPort, "tcp"},
		{DefaultBrowserPort, "tcp"},
		{currentDiscoveryPortOr(DefaultDiscoveryPort), "udp"},
		{5353, "udp"},
	}
}

func currentDiscoveryPortOr(fallback int) int {
	if p := currentDiscoveryPort(); p > 0 {
		return p
	}
	return fallback
}

// Firewall names the host firewall that can be opened for casting: "ufw",
// "firewalld", "windows", or "" when none is known to block casts.
//
// Read without root: ufw's own config says whether it is on, firewalld is a
// service. macOS's firewall asks by itself when the app first listens.
var Firewall = func() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "linux":
		if data, err := os.ReadFile("/etc/ufw/ufw.conf"); err == nil {
			if strings.Contains(strings.ToUpper(string(data)), "ENABLED=YES") {
				return "ufw"
			}
		}
		if _, err := exec.LookPath("firewall-cmd"); err == nil {
			out, err := exec.Command("systemctl", "is-active", "firewalld").Output()
			if err == nil && strings.TrimSpace(string(out)) == "active" {
				return "firewalld"
			}
		}
	}
	return ""
}

// LocalSubnet is the /24 this machine's LAN address is in, or "".
func LocalSubnet() string {
	ip, err := outboundIP()
	if err != nil {
		return ""
	}
	v4 := ip.To4()
	if v4 == nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d.0/24", v4[0], v4[1], v4[2])
}

// FirewallCommand is the program and arguments that open ports on firewall,
// run through the system's own password or admin prompt. ok is false when
// there is nothing this app can open.
func FirewallCommand(firewall, subnet string, ports []FirewallPort) (name string, args []string, ok bool) {
	if subnet == "" {
		subnet = "192.168.0.0/16"
	}
	switch firewall {
	case "ufw":
		lines := make([]string, 0, len(ports))
		for _, p := range ports {
			lines = append(lines, fmt.Sprintf("ufw allow from %s to any port %d proto %s comment 'otakase cast'", subnet, p.Port, p.Proto))
		}
		return "pkexec", []string{"/bin/sh", "-c", strings.Join(lines, " && ")}, true
	case "firewalld":
		lines := make([]string, 0, len(ports)+1)
		for _, p := range ports {
			lines = append(lines, fmt.Sprintf("firewall-cmd --permanent --add-port=%d/%s", p.Port, p.Proto))
		}
		lines = append(lines, "firewall-cmd --reload")
		return "pkexec", []string{"/bin/sh", "-c", strings.Join(lines, " && ")}, true
	case "windows":
		var tcp, udp []string
		for _, p := range ports {
			if p.Proto == "udp" {
				udp = append(udp, strconv.Itoa(p.Port))
			} else {
				tcp = append(tcp, strconv.Itoa(p.Port))
			}
		}
		rule := func(proto string, list []string) string {
			return fmt.Sprintf("netsh advfirewall firewall add rule name=\"Otakase cast %s\" dir=in action=allow protocol=%s localport=%s remoteip=localsubnet profile=any", strings.ToUpper(proto), strings.ToUpper(proto), strings.Join(list, ","))
		}
		script := "/c " + rule("tcp", tcp) + " & " + rule("udp", udp)
		// One elevated cmd runs both rules, so Windows asks once.
		return "powershell", []string{
			"-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command",
			"Start-Process -FilePath cmd.exe -Verb RunAs -Wait -WindowStyle Hidden -ArgumentList '" + strings.ReplaceAll(script, "'", "''") + "'",
		}, true
	}
	return "", nil, false
}

// ErrNoFirewall means there is no firewall this app knows how to open.
var ErrNoFirewall = errors.New("cast: no firewall to open on this computer")

// runFirewall runs the opening command. A variable so tests never prompt.
var runFirewall = func(ctx context.Context, name string, args []string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text != "" {
			return fmt.Errorf("cast: could not open the firewall: %s", text)
		}
		return fmt.Errorf("cast: could not open the firewall: %w", err)
	}
	return nil
}

// AllowThroughFirewall opens the cast ports on this computer's firewall. It
// asks for the password or admin approval the system wants for that.
func AllowThroughFirewall(ctx context.Context) error {
	name, args, ok := FirewallCommand(Firewall(), LocalSubnet(), FirewallPorts())
	if !ok {
		return ErrNoFirewall
	}
	if err := runFirewall(ctx, name, args); err != nil {
		return err
	}
	Log("cast: opened the firewall for casting")
	return nil
}
