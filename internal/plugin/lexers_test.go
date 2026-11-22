package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

const privateGrammar = `<lexer><config><name>Private</name><alias>json</alias></config><rules><state name="root">
<rule pattern="@example"><token type="Keyword"/></rule>
<rule pattern="\{.*\}"><using lexer="json"/></rule>
<rule pattern="\s+"><token type="Text"/></rule>
</state></rules></lexer>`

func TestPluginLexerFile(t *testing.T) {
	r, _, dirs := newRegistry(t)
	original := lexers.Get("json")
	writePlugin(t, dirs.Plugins, "syntax", map[string]string{
		"plugin.json": `{"name":"syntax","contributes":{"languages":[{"id":"example","lexerFile":"grammar.xml"},{"id":"legacy","lexer":"json"}]}}`,
		"grammar.xml": privateGrammar,
	})
	r.Load()
	p := r.Plugin("syntax")
	if p.Stats().Err != "" {
		t.Fatal(p.Stats().Err)
	}
	lexer := p.Lexer("EXAMPLE")
	source := `@example {"n":42}`
	tokens, err := chroma.Tokenise(lexer, nil, source)
	if err != nil {
		t.Fatal(err)
	}
	if chroma.Stringify(tokens...) != source || tokens[0].Type != chroma.Keyword {
		t.Fatal(tokens)
	}
	found := false
	for _, token := range tokens {
		found = found || token.Type == chroma.LiteralNumberInteger && token.Value == "42"
	}
	if !found || lexers.Get("json") != original || p.Lexer("legacy") != original {
		t.Fatal("embedded or legacy lexer changed")
	}
	r.Load()
	if r.Plugin("syntax") != p || p.Lexer("example") != lexer {
		t.Fatal("unchanged grammar was replaced")
	}
	updated := strings.Replace(privateGrammar, `type="Keyword"`, `type="NameFunction"`, 1)
	if err := os.WriteFile(filepath.Join(dirs.Plugins, "syntax", "grammar.xml"), []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	r.Load()
	next := r.Plugin("syntax")
	tokens, err = chroma.Tokenise(next.Lexer("example"), nil, source)
	if err != nil || next == p || tokens[0].Type != chroma.NameFunction {
		t.Fatalf("reload: %v %v", tokens, err)
	}
}

func TestPluginLexerFileErrors(t *testing.T) {
	for _, tc := range []struct{ name, grammar string }{
		{"missing", ""},
		{"xml", `<lexer><config><name>Broken</name></config><rules>`},
		{"regex", strings.Replace(privateGrammar, `pattern="@example"`, `pattern="["`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, dirs := newRegistry(t)
			files := map[string]string{"plugin.json": `{"name":"syntax","contributes":{"languages":[{"id":"example","lexerFile":"grammar.xml"}]}}`}
			if tc.grammar != "" {
				files["grammar.xml"] = tc.grammar
			}
			writePlugin(t, dirs.Plugins, "syntax", files)
			r.Load()
			if err := r.Plugin("syntax").Stats().Err; !strings.Contains(err, "language") || !strings.Contains(err, "example") {
				t.Fatalf("error = %q", err)
			}
			if len(r.Enabled()) != 0 {
				t.Fatal("invalid grammar enabled")
			}
		})
	}
	for _, path := range []string{"../grammar.xml", "/grammar.xml", `dir\grammar.xml`, ".", "C:/grammar.xml"} {
		encoded, err := json.Marshal(path)
		if err != nil {
			t.Fatal(err)
		}
		body := `{"name":"syntax","contributes":{"languages":[{"id":"example","lexerFile":` + string(encoded) + `}]}}`
		if _, err := ParseManifest([]byte(body)); err == nil {
			t.Errorf("accepted lexerFile %q", path)
		}
	}
}
