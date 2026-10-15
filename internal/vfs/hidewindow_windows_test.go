package vfs

import (
	"strings"
	"testing"
)

// The editor runs git and its other programs from a window that has no
// console of its own, which Windows answers with a console window on the
// desktop for every one of them: the programs are asked for none.
func TestACommandOfTheEditorTakesNoConsoleWindow(t *testing.T) {
	cmd, err := Command(t.TempDir(), nil, nil, "cmd", "/c", "echo", "hi")
	if err != nil {
		t.Fatal(err)
	}
	attr := cmd.SysProcAttr
	if attr == nil || !attr.HideWindow || attr.CreationFlags&createNoWindow == 0 {
		t.Errorf("a command of the editor is left to open a console window: %+v", attr)
	}
}

// Hiding the console window leaves what the program writes alone.
func TestACommandOfTheEditorStillWrites(t *testing.T) {
	cmd, err := Command(t.TempDir(), nil, nil, "cmd", "/c", "echo", "hi")
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(out)), "hi"; got != want {
		t.Errorf("the command wrote %q, want %q", got, want)
	}
}
