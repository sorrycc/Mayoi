package symbols

import "testing"

func TestDeclare(t *testing.T) {
	for _, tc := range []struct {
		line, name, kind, detail string
	}{
		{"func main() {", "main", "function", "()"},
		{"func (a *app) editorView(c *ui.Context, d *document) {", "editorView", "method", "(c *ui.Context, d *document) · app"},
		{"type palette struct {", "palette", "class", "struct"},
		{"export async function load(url: string): Promise<void> {", "load", "function", "(url: string): Promise<void>"},
		{"    def run(self):", "run", "method", "(self)"},
		{"class Foo(Base):", "Foo", "class", "(Base)"},
		{"pub(crate) fn parse<'a>(s: &'a str) -> Result<()> {", "parse", "function", "<'a>(s: &'a str) -> Result<()>"},
		{"interface Props {", "Props", "interface", ""},
	} {
		sym, ok := declare(tc.line)
		if !ok || sym.Name != tc.name || sym.Kind != tc.kind || sym.Detail != tc.detail {
			t.Errorf("%q: %+v %v", tc.line, sym, ok)
		}
	}
	for _, line := range []string{"x := func() {}", "	func() {", "type (", "// func comment()", "	return function() {"} {
		if sym, ok := declare(line); ok {
			t.Errorf("%q declares %+v", line, sym)
		}
	}
}
