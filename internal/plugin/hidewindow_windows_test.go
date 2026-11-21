package plugin

import (
	"context"
	"testing"
)

// createNoWindow is CREATE_NO_WINDOW, as vfs has it.
const createNoWindow = 0x08000000

// Mayoi checks each Git-installed plugin for an update an hour apart from
// a window that has no console, which Windows answers with a console
// window on the desktop for each git it starts: git is asked for none.
func TestGitOfAPluginTakesNoConsoleWindow(t *testing.T) {
	cmd := gitCommand(context.Background(), t.TempDir(), "ls-remote", "--", "https://example.com/x.git")
	attr := cmd.SysProcAttr
	if attr == nil || !attr.HideWindow || attr.CreationFlags&createNoWindow == 0 {
		t.Errorf("git of a plugin is left to open a console window: %+v", attr)
	}
}
