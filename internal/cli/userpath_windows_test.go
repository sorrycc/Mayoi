package cli

import (
	"strings"
	"testing"
)

func TestEditPath(t *testing.T) {
	dir := `C:\Users\Me\AppData\Local\Mayoi\bin`
	duplicate := `%USERPROFILE%\bin;` + strings.ToUpper(dir)
	for _, tt := range []struct {
		name, value string
		add         bool
		want        string
	}{
		{"add to empty", "", true, dir},
		{"append without expanding", `%USERPROFILE%\bin;C:\Tools`, true, `%USERPROFILE%\bin;C:\Tools;` + dir},
		{"existing is unchanged", duplicate, true, duplicate},
		{"remove ignores case", `C:\Tools;` + strings.ToUpper(dir) + `;%LOCALAPPDATA%\npm`, false, `C:\Tools;%LOCALAPPDATA%\npm`},
		{"remove when absent keeps quotes", `"C:\A;B";C:\Tools`, false, `"C:\A;B";C:\Tools`},
		{"remove every copy", dir + `;C:\Tools;` + dir, false, `C:\Tools`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := editPath(tt.value, dir, tt.add); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
