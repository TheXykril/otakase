package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The keybinding this installs. Super+Shift+A opens the rofi menu with poster
// previews, which is the launcher-style way to use the program: no terminal,
// pick a show, watch it.
const (
	keybindCombo   = "SUPER + SHIFT + A"
	keybindCommand = AppName + " -rofi -image-preview"

	keybindBlockStart = "-- >>> " + AppName + " keybinding (managed; remove with -remove-keybind) >>>"
	keybindBlockEnd   = "-- <<< " + AppName + " keybinding <<<"

	confBlockStart = "# >>> " + AppName + " keybinding (managed; remove with -remove-keybind) >>>"
	confBlockEnd   = "# <<< " + AppName + " keybinding <<<"
)

// hyprlandConfig describes where a keybinding should be written and in which
// dialect. Omarchy configures Hyprland in Lua and loads user files after its own
// defaults; a stock Hyprland install uses hyprland.conf.
type hyprlandConfig struct {
	path       string
	lua        bool
	blockStart string
	blockEnd   string
}

// findHyprlandConfig picks the file a user's own bindings belong in. Omarchy's
// bindings.lua is preferred when present: writing Hyprland's .conf syntax into a
// Lua config produces a parse error rather than a keybinding.
func findHyprlandConfig(home string) (hyprlandConfig, error) {
	lua := filepath.Join(home, ".config", "hypr", "bindings.lua")
	if _, err := os.Stat(lua); err == nil {
		return hyprlandConfig{path: lua, lua: true, blockStart: keybindBlockStart, blockEnd: keybindBlockEnd}, nil
	}
	conf := filepath.Join(home, ".config", "hypr", "hyprland.conf")
	if _, err := os.Stat(conf); err == nil {
		return hyprlandConfig{path: conf, blockStart: confBlockStart, blockEnd: confBlockEnd}, nil
	}
	return hyprlandConfig{}, fmt.Errorf("no Hyprland config found: looked for %s and %s", lua, conf)
}

// keybindBlock renders the managed block for a config dialect.
func (c hyprlandConfig) keybindBlock() string {
	var body string
	if c.lua {
		// Unbinding first is required to take a combination Omarchy already uses;
		// it is harmless when nothing holds it.
		body = fmt.Sprintf("hl.unbind(%q)\no.bind(%q, %q, %q)",
			keybindCombo, keybindCombo, DisplayName, "exec "+keybindCommand)
	} else {
		body = fmt.Sprintf("bind = SUPER SHIFT, A, exec, %s", keybindCommand)
	}
	return c.blockStart + "\n" + body + "\n" + c.blockEnd
}

// InstallHyprlandKeybind adds Super+Shift+A to the user's Hyprland config.
//
// A package cannot do this at install time: pacman runs as root with no user
// context, and writing into somebody's home directory from a package hook is
// both impossible to do correctly and rude to attempt. So this is a command the
// user runs once, for themselves.
//
// It is idempotent -- a block it wrote before is replaced rather than appended
// to -- and it refuses to touch a binding somebody else owns unless forced.
func InstallHyprlandKeybind(home string, force bool) ([]string, error) {
	cfg, err := findHyprlandConfig(home)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(cfg.path)
	if err != nil {
		return nil, err
	}
	body := string(raw)
	notes := []string{}

	if existing, found := managedBlockRange(body, cfg); found {
		updated := body[:existing[0]] + cfg.keybindBlock() + body[existing[1]:]
		if updated == body {
			return []string{fmt.Sprintf("%s already binds %s; nothing to do", cfg.path, keybindCombo)}, nil
		}
		if err := writeConfig(cfg.path, updated, &notes); err != nil {
			return notes, err
		}
		return append(notes, fmt.Sprintf("updated the existing %s binding in %s", keybindCombo, cfg.path)), nil
	}

	// Somebody else's binding on the same keys is theirs to keep.
	if owner := conflictingBinding(body, cfg); owner != "" && !force {
		return nil, fmt.Errorf(
			"%s is already bound in %s:\n    %s\nleaving it alone -- pass -force-keybind to replace it, or edit that line yourself",
			keybindCombo, cfg.path, owner)
	} else if owner != "" {
		// Appending without retiring the old line would leave two bindings on
		// one combination. Comment it out rather than delete it, so the user can
		// see what was there and put it back.
		body = commentOutBinding(body, owner, cfg)
		notes = append(notes, fmt.Sprintf("commented out the previous binding: %s", owner))
	}

	separator := "\n"
	if !strings.HasSuffix(body, "\n") {
		separator = "\n\n"
	} else if !strings.HasSuffix(body, "\n\n") {
		separator = "\n"
	}
	updated := body + separator + cfg.keybindBlock() + "\n"
	if err := writeConfig(cfg.path, updated, &notes); err != nil {
		return notes, err
	}
	return append(notes,
		fmt.Sprintf("added %s -> %s in %s", keybindCombo, keybindCommand, cfg.path),
		"run `hyprctl reload` if Hyprland has not picked it up already",
	), nil
}

// RemoveHyprlandKeybind takes back out exactly what was added, and nothing else.
func RemoveHyprlandKeybind(home string) ([]string, error) {
	cfg, err := findHyprlandConfig(home)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(cfg.path)
	if err != nil {
		return nil, err
	}
	body := string(raw)
	span, found := managedBlockRange(body, cfg)
	if !found {
		return []string{fmt.Sprintf("no %s binding found in %s", AppName, cfg.path)}, nil
	}
	updated := strings.TrimRight(body[:span[0]], "\n") + "\n" + strings.TrimLeft(body[span[1]:], "\n")
	notes := []string{}
	if err := writeConfig(cfg.path, updated, &notes); err != nil {
		return notes, err
	}
	return append(notes, fmt.Sprintf("removed the %s binding from %s", keybindCombo, cfg.path)), nil
}

// managedBlockRange locates a block this program wrote, so a second run replaces
// it instead of stacking another copy underneath.
func managedBlockRange(body string, cfg hyprlandConfig) ([2]int, bool) {
	start := strings.Index(body, cfg.blockStart)
	if start < 0 {
		return [2]int{}, false
	}
	end := strings.Index(body[start:], cfg.blockEnd)
	if end < 0 {
		return [2]int{}, false
	}
	return [2]int{start, start + end + len(cfg.blockEnd)}, true
}

// conflictingBinding returns the line that already binds the combination, if any
// line outside a managed block does.
func conflictingBinding(body string, cfg hyprlandConfig) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if cfg.lua {
			if strings.Contains(trimmed, "o.bind") && strings.Contains(trimmed, keybindCombo) {
				return trimmed
			}
			continue
		}
		// Hyprland's own syntax is "bind = MOD KEY, key, dispatcher, args".
		if strings.HasPrefix(trimmed, "bind") && strings.Contains(strings.ToUpper(trimmed), "SUPER SHIFT, A,") {
			return trimmed
		}
	}
	return ""
}

// writeConfig backs the file up before replacing it, because this edits
// something the user did not write and may care about a great deal.
func writeConfig(path, contents string, notes *[]string) error {
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	backup := fmt.Sprintf("%s.bak.%d", path, time.Now().Unix())
	if err := os.WriteFile(backup, original, 0o644); err != nil {
		return fmt.Errorf("could not back up %s: %w", path, err)
	}
	*notes = append(*notes, "backed up to "+backup)
	return os.WriteFile(path, []byte(contents), 0o644)
}

// commentOutBinding disables one line in place, marked so its fate is obvious.
func commentOutBinding(body, target string, cfg hyprlandConfig) string {
	prefix := "# "
	if cfg.lua {
		prefix = "-- "
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != target {
			continue
		}
		lines[i] = prefix + line + "  " + prefix + "replaced by " + AppName
		break
	}
	return strings.Join(lines, "\n")
}
