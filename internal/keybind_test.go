package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hyprHome(t *testing.T, name, body string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "hypr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

func readBindings(t *testing.T, home, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(home, ".config", "hypr", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// Omarchy configures Hyprland in Lua. Writing Hyprland's .conf syntax into a Lua
// config is a parse error, not a keybinding, so the dialect has to be chosen
// from the file that is actually there.
func TestKeybindUsesLuaWhenOmarchyBindingsExist(t *testing.T) {
	home := hyprHome(t, "bindings.lua", "-- my bindings\n")

	notes, err := InstallHyprlandKeybind(home, false)
	if err != nil {
		t.Fatalf("install failed: %v", err)
	}
	body := readBindings(t, home, "bindings.lua")

	if !strings.Contains(body, `o.bind("SUPER + SHIFT + A"`) {
		t.Errorf("no Lua binding was written:\n%s", body)
	}
	if !strings.Contains(body, `hl.unbind("SUPER + SHIFT + A")`) {
		t.Error("the combination must be unbound first, or an Omarchy default keeps it")
	}
	if !strings.Contains(body, "otakase -rofi -image-preview") {
		t.Error("the binding should open the rofi menu with previews")
	}
	if !strings.Contains(body, "-- my bindings") {
		t.Error("the user's existing bindings were lost")
	}
	if !strings.Contains(strings.Join(notes, "\n"), "backed up to") {
		t.Error("a file the user owns was edited without a backup")
	}
}

// A stock Hyprland install has no bindings.lua.
func TestKeybindFallsBackToHyprlandConf(t *testing.T) {
	home := hyprHome(t, "hyprland.conf", "# stock config\n")

	if _, err := InstallHyprlandKeybind(home, false); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	body := readBindings(t, home, "hyprland.conf")
	if !strings.Contains(body, "bind = SUPER SHIFT, A, exec, otakase -rofi -image-preview") {
		t.Errorf("no conf-syntax binding was written:\n%s", body)
	}
	if strings.Contains(body, "o.bind") {
		t.Error("Lua syntax leaked into a .conf file")
	}
}

// Running it twice must not stack two copies of the binding.
func TestKeybindIsIdempotent(t *testing.T) {
	home := hyprHome(t, "bindings.lua", "-- mine\n")

	for i := 0; i < 3; i++ {
		if _, err := InstallHyprlandKeybind(home, false); err != nil {
			t.Fatalf("install %d failed: %v", i, err)
		}
	}
	body := readBindings(t, home, "bindings.lua")
	if got := strings.Count(body, "o.bind(\"SUPER + SHIFT + A\""); got != 1 {
		t.Errorf("expected exactly one binding, found %d:\n%s", got, body)
	}
	if got := strings.Count(body, keybindBlockStart); got != 1 {
		t.Errorf("expected one managed block, found %d", got)
	}
}

// Somebody else's binding on the same keys is theirs. Silently stealing it is
// how a tool earns a reputation for wrecking configs.
func TestKeybindRefusesToStealAnExistingBinding(t *testing.T) {
	home := hyprHome(t, "bindings.lua",
		"o.bind(\"SUPER + SHIFT + A\", \"Something else\", \"exec important-thing\")\n")

	_, err := InstallHyprlandKeybind(home, false)
	if err == nil {
		t.Fatal("it overwrote a binding the user already had")
	}
	if !strings.Contains(err.Error(), "important-thing") {
		t.Errorf("the error should say what holds the key, got: %v", err)
	}
	if body := readBindings(t, home, "bindings.lua"); !strings.Contains(body, "important-thing") {
		t.Error("the existing binding was modified despite the refusal")
	}

	// ...unless the user says to.
	if _, err := InstallHyprlandKeybind(home, true); err != nil {
		t.Fatalf("forced install failed: %v", err)
	}
	if body := readBindings(t, home, "bindings.lua"); !strings.Contains(body, "otakase -rofi") {
		t.Error("the forced install did not write the binding")
	}
}

// Removing takes out what was added and leaves the rest untouched.
func TestKeybindRemovalLeavesEverythingElse(t *testing.T) {
	home := hyprHome(t, "bindings.lua", "-- keep me\no.bind(\"SUPER + B\", \"Browser\", \"exec firefox\")\n")

	if _, err := InstallHyprlandKeybind(home, false); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveHyprlandKeybind(home); err != nil {
		t.Fatalf("removal failed: %v", err)
	}
	body := readBindings(t, home, "bindings.lua")
	if strings.Contains(body, "SUPER + SHIFT + A") || strings.Contains(body, keybindBlockStart) {
		t.Errorf("the binding survived removal:\n%s", body)
	}
	if !strings.Contains(body, "-- keep me") || !strings.Contains(body, "firefox") {
		t.Errorf("removal took other bindings with it:\n%s", body)
	}
}

// A machine with no Hyprland at all should say so, not panic or write files.
func TestKeybindReportsWhenHyprlandIsAbsent(t *testing.T) {
	_, err := InstallHyprlandKeybind(t.TempDir(), false)
	if err == nil {
		t.Fatal("expected an error when there is no Hyprland config")
	}
	if !strings.Contains(err.Error(), "no Hyprland config found") {
		t.Errorf("unhelpful error: %v", err)
	}
}

// Forcing must retire the old binding, not leave two bindings fighting over one
// combination.
func TestKeybindForceRetiresTheOldBinding(t *testing.T) {
	home := hyprHome(t, "bindings.lua",
		"o.bind(\"SUPER + SHIFT + A\", \"otakase\", \"exec otakase\")\n")

	if _, err := InstallHyprlandKeybind(home, true); err != nil {
		t.Fatalf("forced install failed: %v", err)
	}
	body := readBindings(t, home, "bindings.lua")

	active := 0
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		if strings.Contains(trimmed, "o.bind") && strings.Contains(trimmed, "SUPER + SHIFT + A") {
			active++
		}
	}
	if active != 1 {
		t.Errorf("expected exactly one active binding, found %d:\n%s", active, body)
	}
	if !strings.Contains(body, "-- o.bind(\"SUPER + SHIFT + A\", \"otakase\"") {
		t.Errorf("the old binding should be commented out, not deleted:\n%s", body)
	}
}

// A package upgrade refreshes a binding it already owns. It must not re-add one
// the user deliberately removed -- an upgrade that argues with you is worse than
// an upgrade that does nothing.
func TestKeybindRefreshOnlyTouchesItsOwnBlock(t *testing.T) {
	home := hyprHome(t, "bindings.lua", "-- nothing of mine here\n")

	// Never having had one is not the same as having removed one: a reinstall
	// or an upgrade from a version without the feature must still set it up.
	if _, err := RefreshHyprlandKeybind(home); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if body := readBindings(t, home, "bindings.lua"); !strings.Contains(body, "SUPER + SHIFT + A") {
		t.Errorf("a first upgrade should install the binding:\n%s", body)
	}
	stale := strings.Replace(readBindings(t, home, "bindings.lua"),
		"otakase -rofi -image-preview", "otakase -old-flags", 1)
	if err := os.WriteFile(filepath.Join(home, ".config", "hypr", "bindings.lua"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RefreshHyprlandKeybind(home); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	body := readBindings(t, home, "bindings.lua")
	if strings.Contains(body, "-old-flags") {
		t.Errorf("refresh did not update the existing block:\n%s", body)
	}
	if got := strings.Count(body, "o.bind(\"SUPER + SHIFT + A\""); got != 1 {
		t.Errorf("refresh should leave exactly one binding, found %d", got)
	}
}

// Removing the binding is a decision. A later upgrade must not undo it, which
// means removal has to leave something behind that says so.
func TestKeybindRemovalSurvivesAnUpgrade(t *testing.T) {
	home := hyprHome(t, "bindings.lua", "-- mine\n")

	if _, err := InstallHyprlandKeybind(home, false); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveHyprlandKeybind(home); err != nil {
		t.Fatal(err)
	}

	// This is what a package upgrade runs.
	notes, err := RefreshHyprlandKeybind(home)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if body := readBindings(t, home, "bindings.lua"); strings.Contains(body, "SUPER + SHIFT + A") {
		t.Errorf("an upgrade put back a binding the user removed:\n%s", body)
	}
	if !strings.Contains(strings.Join(notes, " "), "removed previously") {
		t.Errorf("it should say why it left things alone, got %v", notes)
	}

	// Asking for it explicitly overrides that.
	if _, err := InstallHyprlandKeybind(home, false); err != nil {
		t.Fatal(err)
	}
	if body := readBindings(t, home, "bindings.lua"); !strings.Contains(body, "SUPER + SHIFT + A") {
		t.Error("asking for the binding back did not restore it")
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "otakase", "keybind-optout")); err == nil {
		t.Error("asking for it back should clear the opt-out")
	}
}
