package internal

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/thexykril/otakase/internal/cast"
	"github.com/thexykril/otakase/internal/icons"
)

// Ports -cast-setup fixes when the config leaves them random. 8010 is what the
// README and the wiki already use for CastPort; discovery sits next to it.
const (
	castSetupStreamPort    = 8010
	castSetupDiscoveryPort = cast.DefaultDiscoveryPort
)

// castSetupFixPorts gives CastPort and CastDiscoveryPort fixed values when
// they are random, in memory and in the config file, because a random port is
// the one thing no firewall rule can cover.
func castSetupFixPorts(out io.Writer, config *Config, configPath string) error {
	changed := map[string]string{}
	if config.CastPort == 0 {
		config.CastPort = castSetupStreamPort
		changed["CastPort"] = strconv.Itoa(castSetupStreamPort)
	}
	if config.CastDiscoveryPort == 0 {
		config.CastDiscoveryPort = castSetupDiscoveryPort
		changed["CastDiscoveryPort"] = strconv.Itoa(castSetupDiscoveryPort)
	}
	if len(changed) == 0 || strings.TrimSpace(configPath) == "" {
		return nil
	}
	configMap, err := LoadConfigFromFile(configPath)
	if err != nil {
		return err
	}
	for _, key := range []string{"CastPort", "CastDiscoveryPort"} {
		if value, ok := changed[key]; ok {
			configMap[key] = value
			fmt.Fprintf(out, "Set %s=%s in your config.\n", key, value)
		}
	}
	return SaveConfigToFile(configPath, configMap)
}

// castSetupApply asks for the sudo password the way otakase -u does -- a
// desktop dialog from rofi, the terminal otherwise -- and runs the rules
// with it.
func castSetupApply(rules [][]string) error {
	prompt := castSetupPasswordPrompt(rules)
	if preferGUIPasswordPrompt() {
		// zenity's password dialog shows no text of its own, so the commands
		// also go out as a notification ahead of it.
		Out(strings.TrimSuffix(prompt, "Administrator password (sudo): "))
	}
	password, err := promptSudoPasswordTitled(DisplayName+" Cast Setup", prompt)
	if err != nil {
		return fmt.Errorf("read sudo password: %w", err)
	}
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("empty sudo password")
	}
	return castSetupRun(rules, password, exec.Command)
}

// castSetupPasswordPrompt says what the password is for and lists every
// command it will run, so nobody types a sudo password for something they
// have not read. The last line is the ask itself, which the terminal prompt
// ends on.
func castSetupPasswordPrompt(rules [][]string) string {
	var b strings.Builder
	b.WriteString("To let cast devices through the firewall, otakase will run:\n")
	for _, rule := range rules {
		b.WriteString("  " + castShellCommand([][]string{rule}) + "\n")
	}
	b.WriteString("Only devices on your local network are allowed.\n")
	b.WriteString("Administrator password (sudo): ")
	return b.String()
}

// castSetupRun runs each rule with the password on sudo's stdin, and stops at
// the first one that fails. command is a parameter so tests can run something
// harmless in place of sudo.
func castSetupRun(rules [][]string, password string, command func(string, ...string) *exec.Cmd) error {
	for _, rule := range rules {
		argv := rule
		if len(argv) > 0 && argv[0] == "sudo" {
			// -S: password from stdin; -p '': no prompt of sudo's own.
			argv = append([]string{"sudo", "-S", "-p", ""}, argv[1:]...)
		}
		cmd := command(argv[0], argv[1:]...)
		cmd.Stdin = strings.NewReader(password + "\n")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s failed: %w", castShellCommand([][]string{rule}), err)
		}
	}
	return nil
}

// castRescanKey is the menu key of the entry that searches again, in the
// device picker and in the menu shown when nothing was found.
const castRescanKey = "rescan"

// castRescanOption is that entry, kept last so the devices come first.
var castRescanOption = SelectionOption{Key: castRescanKey, Label: "Rescan for devices", Icon: icons.Refresh}

// castNoDevicesMenu asks, from the menu the search was started from, what to
// do about an empty search: cast to a browser instead, look again (a TV just
// turned on), fix the firewall when one is on, or give up. It returns the key
// of what was picked -- castRescanKey or castBrowserOption.Key -- or "" to give
// up.
//
// A rofi cast's own terminal cannot show a menu, so it is not asked there;
// the error still carries the rules and the pointer to -cast-setup.
func castNoDevicesMenu(config *Config, firewall string, browser bool) string {
	if config == nil || config.CastNonInteractive {
		return ""
	}
	options := []SelectionOption{}
	if browser {
		options = append(options, castBrowserOption)
	}
	options = append(options, castRescanOption)
	if firewall != "" {
		options = append(options, SelectionOption{Key: "fix", Label: "Fix the firewall now (" + firewall + " may be blocking devices)", Icon: icons.Firewall})
	}
	options = append(options, SelectionOption{Key: "cancel", Label: "Cancel", Icon: icons.No})
	Out("No cast devices found.")
	selected, err := DynamicSelectPreserveOrder(options)
	if err != nil {
		return ""
	}
	switch selected.Key {
	case castRescanKey, castBrowserOption.Key:
		return selected.Key
	case "fix":
		if castFixFirewallNow(config, firewall) {
			return castRescanKey
		}
	}
	return ""
}

// castFixFirewallNow fixes the cast ports and allows them through the
// firewall, asking for the password first. It reports whether the rules went
// in.
func castFixFirewallNow(config *Config, firewall string) bool {
	if err := castSetupFixPorts(io.Discard, config, ConfigFilePath); err != nil {
		Out("Could not save the cast ports: " + err.Error())
		return false
	}
	subnet := castLocalSubnet(cast.LocalAddress())
	if subnet == "" {
		subnet = "192.168.0.0/24"
	}
	rules, _ := castDiscoveryFirewallRules(config, subnet, firewall)
	if err := castSetupApply(rules); err != nil {
		Log(fmt.Sprintf("cast: firewall fix failed: %v", err))
		Out("Firewall not changed: " + err.Error())
		return false
	}
	Out("Firewall updated.")
	return true
}
