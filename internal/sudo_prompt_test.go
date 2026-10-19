package internal

import "testing"

func TestPreferGUIPasswordPrompt(t *testing.T) {
	saved := GetGlobalConfig()
	t.Cleanup(func() { SetGlobalConfig(saved) })

	SetGlobalConfig(&Config{RofiSelection: true})
	if !preferGUIPasswordPrompt() {
		t.Fatal("rofi mode should ask in a desktop dialog")
	}

	// Without rofi a terminal session asks in the terminal, display or not.
	SetGlobalConfig(&Config{RofiSelection: false})
	if stdinIsTerminal() && preferGUIPasswordPrompt() {
		t.Fatal("a terminal session should not get a desktop dialog")
	}
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	if preferGUIPasswordPrompt() {
		t.Fatal("without rofi or a display there is no dialog to show")
	}
}

func TestPasswordDialogsOrder(t *testing.T) {
	dialogs := passwordDialogs("Otakase Update", "Password:")
	var names []string
	for _, d := range dialogs {
		names = append(names, d.name)
	}
	if len(names) != 3 || names[0] != "zenity" || names[1] != "yad" || names[2] != "kdialog" {
		t.Fatalf("got %v", names)
	}
}
