package lsptest

import "strings"

func fakeSourceActions(text, uri string, version int, kind string, diagnosed bool, mode string) []any {
	edit := func(line, start, end int, text string) map[string]any {
		return map[string]any{"range": map[string]any{
			"start": map[string]int{"line": line, "character": start}, "end": map[string]int{"line": line, "character": end}}, "newText": text}
	}
	var edits []any
	if kind == "source.organizeImports" && strings.Contains(text, "needimports") && !strings.Contains(text, "// organized\n") {
		edits = append(edits, edit(0, 0, 0, "// organized\n"))
	}
	if kind == "source.fixAll" && diagnosed {
		for line, text := range strings.Split(text, "\n") {
			if at := strings.Index(text, "bad"); at >= 0 {
				edits = append(edits, edit(line, at, at+3, "good"))
			}
		}
	}
	if len(edits) == 0 {
		return nil
	}
	action := map[string]any{"title": "Fix source", "kind": kind}
	switch mode {
	case "command":
		action["command"] = map[string]any{"title": "Fix", "command": "fake.fix"}
	case "foreign":
		uri = "file:///another.fake"
	case "outdated":
		version--
	case "invalid":
		edits = []any{edit(0, 0, 10000, "invalid")}
	case "overlap":
		edits = []any{edit(0, 0, 2, "first"), edit(0, 1, 3, "second")}
	case "disabled":
		action["disabled"] = map[string]string{"reason": "Disabled"}
	}
	if kind == "source.organizeImports" && mode != "outdated" {
		action["edit"] = map[string]any{"changes": map[string]any{uri: edits}}
	} else {
		action["edit"] = map[string]any{"documentChanges": []any{map[string]any{
			"textDocument": map[string]any{"uri": uri, "version": version}, "edits": edits}}}
	}
	return []any{action}
}
