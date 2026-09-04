package internal

import (
	"reflect"
	"strings"
	"testing"
)

// EDITOR almost always carries flags. Passing the whole string to exec.Command
// as the executable name made `curd -e` fail outright.
func TestSplitEditorCommand(t *testing.T) {
	cases := []struct {
		value string
		want  []string
	}{
		{"nvim", []string{"nvim"}},
		{"omarchy-launch-editor --inline", []string{"omarchy-launch-editor", "--inline"}},
		{"code --wait", []string{"code", "--wait"}},
		{"subl -w", []string{"subl", "-w"}},
		{"  vim   -u   NONE  ", []string{"vim", "-u", "NONE"}},
		// A quoted path containing spaces must survive as one argument.
		{`"/usr/local/my editor/bin" --flag`, []string{"/usr/local/my editor/bin", "--flag"}},
		{`'/opt/my editor' -w`, []string{"/opt/my editor", "-w"}},
		{`/opt/my\ editor -w`, []string{"/opt/my editor", "-w"}},
		{"", nil},
		{"   ", nil},
	}

	for _, tc := range cases {
		got := splitEditorCommand(tc.value)
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("splitEditorCommand(%q) = %#v, want %#v", tc.value, got, tc.want)
		}
	}
}

func TestResolveEditorCommandPutsPathLast(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "code --wait")

	name, args, err := resolveEditorCommand("/home/u/.config/curd/curd.conf")
	if err != nil {
		t.Fatal(err)
	}
	if name != "code" {
		t.Fatalf("name = %q, want code", name)
	}
	want := []string{"--wait", "/home/u/.config/curd/curd.conf"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
}

// VISUAL wins over EDITOR by convention.
func TestResolveEditorCommandPrefersVisual(t *testing.T) {
	t.Setenv("EDITOR", "ed")
	t.Setenv("VISUAL", "nvim -R")

	name, args, err := resolveEditorCommand("/tmp/c.conf")
	if err != nil {
		t.Fatal(err)
	}
	if name != "nvim" {
		t.Fatalf("name = %q, want nvim", name)
	}
	if !reflect.DeepEqual(args, []string{"-R", "/tmp/c.conf"}) {
		t.Fatalf("args = %#v", args)
	}
}

func TestResolveEditorCommandFallsBackWhenUnset(t *testing.T) {
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	name, args, err := resolveEditorCommand("/tmp/c.conf")
	if err != nil {
		t.Fatal(err)
	}
	if name == "" {
		t.Fatal("expected a default editor")
	}
	if len(args) == 0 || args[len(args)-1] != "/tmp/c.conf" {
		t.Fatalf("expected the path last, got %#v", args)
	}
}

// Whitespace-only EDITOR must not produce an empty executable name.
func TestResolveEditorCommandRejectsBlankEditor(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", `""`)

	name, _, err := resolveEditorCommand("/tmp/c.conf")
	if err == nil && strings.TrimSpace(name) == "" {
		t.Fatal("expected an error or a usable editor name, got an empty name")
	}
}
