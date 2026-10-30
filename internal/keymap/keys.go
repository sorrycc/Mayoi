package keymap

import (
	"errors"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// keyNames are the keys by the names VS Code gives them.
var keyNames = map[string]ui.Key{
	"enter": ui.KeyEnter, "escape": ui.KeyEscape, "backspace": ui.KeyBackspace, "tab": ui.KeyTab,
	"space": ui.KeySpace, "delete": ui.KeyDelete, "insert": ui.KeyInsert, "home": ui.KeyHome, "end": ui.KeyEnd,
	"pageup": ui.KeyPageUp, "pagedown": ui.KeyPageDown,
	"left": ui.KeyLeft, "right": ui.KeyRight, "up": ui.KeyUp, "down": ui.KeyDown,
	"-": ui.KeyMinus, "=": ui.KeyEqual, ",": ui.KeyComma, ".": ui.KeyPeriod, "/": ui.KeySlash,
	";": ui.KeySemicolon, "'": ui.KeyQuote, "[": ui.KeyBracketLeft, "]": ui.KeyBracketRight,
	"\\": ui.KeyBackslash, "`": ui.KeyBackquote,
	"f1": ui.KeyF1, "f2": ui.KeyF2, "f3": ui.KeyF3, "f4": ui.KeyF4, "f5": ui.KeyF5, "f6": ui.KeyF6,
	"f7": ui.KeyF7, "f8": ui.KeyF8, "f9": ui.KeyF9, "f10": ui.KeyF10, "f11": ui.KeyF11, "f12": ui.KeyF12,
}

func init() {
	for k := ui.KeyA; k <= ui.KeyZ; k++ {
		keyNames[string(rune('a'+k-ui.KeyA))] = k
	}
	for k := ui.Key0; k <= ui.Key9; k++ {
		keyNames[string(rune('0'+k-ui.Key0))] = k
	}
}

// ParseKeys reads a key or a chord as VS Code writes it: strokes apart by
// spaces, each its modifiers and its key joined by "+", as "cmd+k cmd+s".
func ParseKeys(s string) ([]Stroke, error) {
	fields := strings.Fields(strings.ToLower(s))
	if len(fields) == 0 {
		return nil, errors.New("no key")
	}
	var keys []Stroke
	for _, f := range fields {
		st, err := ParseStroke(f)
		if err != nil {
			return nil, err
		}
		keys = append(keys, st)
	}
	return keys, nil
}

// ParseStroke reads a stroke as "shift+cmd+p".
func ParseStroke(s string) (Stroke, error) {
	var st Stroke
	parts := strings.Split(strings.ToLower(s), "+")
	// The key may be "+" itself, after the modifiers.
	if strings.HasSuffix(s, "++") {
		parts = append(parts[:len(parts)-2], "+")
	}
	for i, p := range parts {
		if i == len(parts)-1 {
			k, ok := keyNames[p]
			if !ok {
				return Stroke{}, fmt.Errorf("unknown key %q", p)
			}
			st.Key = k
			break
		}
		switch p {
		case "cmd", "meta", "win", "super":
			st.Mods |= ui.Super
		case "ctrl":
			st.Mods |= ui.Ctrl
		case "alt", "option":
			st.Mods |= ui.Alt
		case "shift":
			st.Mods |= ui.Shift
		case "cmdorctrl", "commandorcontrol":
			st.Mods |= ui.Cmd
		default:
			return Stroke{}, fmt.Errorf("unknown modifier %q", p)
		}
	}
	return st, nil
}

// ParseAccel reads an accelerator of the menus, as "Shift+CmdOrCtrl+S".
func ParseAccel(accel string) (Stroke, bool) {
	if accel == "" {
		return Stroke{}, false
	}
	low := strings.ToLower(accel)
	low = strings.ReplaceAll(low, "command", "cmd")
	low = strings.ReplaceAll(low, "control", "ctrl")
	st, err := ParseStroke(low)
	return st, err == nil
}

// Accel is the stroke as the menus write an accelerator, as
// "Ctrl+Shift+P", for their caps.
func (s Stroke) Accel() string {
	var parts []string
	if s.Mods&ui.Ctrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if s.Mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	if s.Mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	if s.Mods&ui.Super != 0 {
		parts = append(parts, "Cmd")
	}
	return strings.Join(append(parts, s.keyName()), "+")
}

func (s Stroke) keyName() string {
	switch {
	case s.Key >= ui.KeyA && s.Key <= ui.KeyZ:
		return string(rune('A' + s.Key - ui.KeyA))
	case s.Key == ui.KeyEscape:
		return "Escape"
	case s.Key == ui.KeySpace:
		return "Space"
	}
	for name, k := range keyNames {
		if k == s.Key {
			if len(name) > 1 {
				return strings.ToUpper(name[:1]) + name[1:]
			}
			return name
		}
	}
	return "?"
}

// KnownKey reports whether the file can name k, as a key of a shortcut.
func KnownKey(k ui.Key) bool {
	for _, n := range keyNames {
		if n == k {
			return true
		}
	}
	return false
}
