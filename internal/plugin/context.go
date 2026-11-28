package plugin

// DocumentRef identifies an open document for the lifetime of its window.
type DocumentRef struct {
	WindowID   uint64 `json:"windowId"`
	DocumentID int    `json:"documentId"`
}

// CommandContext describes the action that invoked a plugin command.
type CommandContext struct {
	Source   string       `json:"source"`
	WindowID uint64       `json:"windowId"`
	Document *DocumentRef `json:"document,omitempty"`
	Version  *uint64      `json:"version,omitempty"`
	Position *struct {
		Line      int `json:"line"`
		Character int `json:"character"`
	} `json:"position,omitempty"`
	// Cwd is the folder of the terminal a clicked link came from.
	Cwd string `json:"cwd,omitempty"`
}
