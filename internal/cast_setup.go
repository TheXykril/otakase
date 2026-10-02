package internal

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/thexykril/otakase/internal/cast"
)

// Ports -cast-setup fixes when the config leaves them random. 8010 is what the
// README and the wiki already use for CastPort; discovery sits next to it.
const (
	castSetupStreamPort    = 8010
	castSetupDiscoveryPort = cast.DefaultDiscoveryPort
)

// CastSetup gets casting through this machine's firewall in one go: it fixes
// the ports a firewall rule needs, shows the rules, runs them with sudo once
// the viewer says yes, and then searches so they see the result.
//
// It is a command of its own, run on purpose. Casting itself still never
// touches a firewall -- changing one as a side effect of watching an episode
// is not something a media player should do -- but a viewer who asked for the
// fix should not have to copy it out of an error message.
func CastSetup(config *Config) error {
	if config == nil {
		return fmt.Errorf("no config")
	}
	if err := castSetupFixPorts(os.Stdout, config, GlobalConfigPath); err != nil {
		return err
	}
	fmt.Printf("Casting uses UDP %d (TV answers), UDP 5353 (mDNS) and TCP %d (the stream).\n",
		config.CastDiscoveryPort, config.CastPort)

	firewall := ""
	if runtime.GOOS == "linux" {
		firewall = castDetectFirewall()
	}
	if firewall == "" {
		fmt.Println("No ufw or firewalld found running, so there is nothing to allow.")
	} else {
		subnet := castLocalSubnet(cast.LocalAddress())
		if subnet == "" {
			subnet = "192.168.0.0/24"
		}
		rules, _ := castDiscoveryFirewallRules(config, subnet, firewall)
		fmt.Printf("%s is on. These rules let devices on %s through:\n", firewall, subnet)
		for _, rule := range rules {
			fmt.Println("  " + castShellCommand([][]string{rule}))
		}
		if !castSetupConfirm(os.Stdout, "Run them now? You will be asked for your password. [Y/n] ") {
			fmt.Println("Nothing changed. Run the lines above yourself when ready.")
			return nil
		}
		if err := castSetupApply(rules); err != nil {
			return err
		}
		fmt.Println("Firewall updated.")
	}

	fmt.Println("Looking for cast devices...")
	cast.SetKodi(cast.KodiSettings{
		Hosts:    strings.Split(config.KodiHost, ","),
		User:     config.KodiUser,
		Password: config.KodiPassword,
	})
	cast.SetDiscoveryPort(config.CastDiscoveryPort)
	devices, err := cast.Discover(context.Background(), cast.DefaultDiscoveryTimeout)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		fmt.Println("Still no devices. Check the TV is on, on this network, and has its DLNA or media renderer setting on.")
		fmt.Println("More help: https://github.com/TheXykril/otakase/wiki/Casting-Problems")
		return nil
	}
	fmt.Println("Found:")
	for _, device := range devices {
		fmt.Println("  " + device.String())
	}
	fmt.Println("Cast with otakase -cast.")
	return nil
}

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

// castSetupConfirm asks a yes/no question that defaults to yes. Ended stdin is
// a no: nobody answered.
func castSetupConfirm(out io.Writer, question string) bool {
	fmt.Fprint(out, question)
	line, err := stdinLineReader().ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(out)
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "", "y", "yes":
		return true
	}
	return false
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
var castRescanOption = SelectionOption{Key: castRescanKey, Label: "↻ Rescan for devices"}

// castNoDevicesMenu asks, from the menu the search was started from, what to
// do about an empty search: look again (a TV just turned on), fix the
// firewall when one is on, or give up. It reports whether a second search is
// worth it.
//
// A rofi cast's own terminal cannot show a menu, so it is not asked there;
// the error still carries the rules and the pointer to -cast-setup.
func castNoDevicesMenu(config *Config, firewall string) bool {
	if config == nil || config.CastNonInteractive {
		return false
	}
	options := []SelectionOption{castRescanOption}
	if firewall != "" {
		options = append(options, SelectionOption{Key: "fix", Label: "Fix the firewall now (" + firewall + " may be blocking devices)"})
	}
	options = append(options, SelectionOption{Key: "cancel", Label: "Cancel"})
	Out("No cast devices found.")
	selected, err := DynamicSelectPreserveOrder(options)
	if err != nil {
		return false
	}
	switch selected.Key {
	case castRescanKey:
		return true
	case "fix":
		return castFixFirewallNow(config, firewall)
	}
	return false
}

// castFixFirewallNow fixes the cast ports and allows them through the
// firewall, asking for the password first. It reports whether the rules went
// in.
func castFixFirewallNow(config *Config, firewall string) bool {
	if err := castSetupFixPorts(io.Discard, config, GlobalConfigPath); err != nil {
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
