package main

import (
	"strings"
	"testing"
)

func TestCommentJSXChildren(t *testing.T) {
	for _, file := range []string{"App.tsx", "App.jsx"} {
		t.Run(file, func(t *testing.T) {
			src := "const view = (\n  <section>\n        <G3Alert type=\"info\" showIcon message={LEVEL_ROLE_HINT} />\n  </section>\n);\n"
			hs, d := editHarness(t, file, src)
			hs.place(d, strings.Index(src, "<G3Alert"), strings.Index(src, "<G3Alert"))
			hs.a.run("sel.comment")
			hs.settle()
			want := strings.Replace(src, "<G3Alert type=\"info\" showIcon message={LEVEL_ROLE_HINT} />", "{/* <G3Alert type=\"info\" showIcon message={LEVEL_ROLE_HINT} /> */}", 1)
			expectText(t, d, want)
			hs.a.run("sel.comment")
			hs.settle()
			expectText(t, d, src)
		})
	}
}

func TestCommentJSXChildrenBlock(t *testing.T) {
	src := "const view = (\n  <section>\n    <h1>Hello</h1>\n    <p>World</p>\n  </section>\n);\n"
	hs, d := editHarness(t, "App.tsx", src)
	start := strings.Index(src, "<h1>")
	end := strings.Index(src, "</p>") + len("</p>")
	hs.place(d, start, end)
	hs.a.run("sel.comment")
	hs.settle()
	want := strings.Replace(src, "<h1>Hello</h1>\n    <p>World</p>", "{/* <h1>Hello</h1>\n    <p>World</p> */}", 1)
	expectText(t, d, want)
	hs.a.run("sel.comment")
	hs.settle()
	expectText(t, d, src)
}

func TestCommentJSXKeepsLineCommentsInTags(t *testing.T) {
	src := "const view = (\n  <Card\n    title=\"Hello\"\n  />\n);\n"
	hs, d := editHarness(t, "App.tsx", src)
	at := strings.Index(src, `title="Hello"`)
	hs.place(d, at, at)
	hs.a.run("sel.comment")
	hs.settle()
	want := strings.Replace(src, "    title=\"Hello\"", "    // title=\"Hello\"", 1)
	expectText(t, d, want)
	hs.a.run("sel.comment")
	hs.settle()
	expectText(t, d, src)
}

func TestCommentJSXExpressionUsesLineComments(t *testing.T) {
	src := "const view = (\n  <section>\n    {\n      value\n    }\n  </section>\n);\n"
	hs, d := editHarness(t, "App.tsx", src)
	at := strings.Index(src, "      value")
	hs.place(d, at, at)
	hs.a.run("sel.comment")
	hs.settle()
	want := strings.Replace(src, "      value", "      // value", 1)
	expectText(t, d, want)
	hs.a.run("sel.comment")
	hs.settle()
	expectText(t, d, src)
}

func TestCommentJSXUnwrapsAJSXComment(t *testing.T) {
	src := "const view = (\n  <section>\n    {/* <G3Alert /> */}\n  </section>\n);\n"
	hs, d := editHarness(t, "App.tsx", src)
	at := strings.Index(src, "    {/* <G3Alert /> */}")
	hs.place(d, at+5, at+5)
	hs.a.run("sel.comment")
	hs.settle()
	want := strings.Replace(src, "    {/* <G3Alert /> */}", "    <G3Alert />", 1)
	expectText(t, d, want)
}

func TestCommentJSXLeavesTypeScriptGenericsAlone(t *testing.T) {
	src := "const identity = <T,>(value: T) => value;\n" +
		"const typed = <T extends Foo>(value: T) => value;\n" +
		"const xs: Array<string> = [];\n" +
		"const c: React.FC<Props> = (props) => null;\n" +
		"new Map<string, Set<number>>();\n" +
		"const z = y as Array<number>;\n" +
		"const g = foo<T>(x);\n" +
		"const x = 1;\n"
	hs, d := editHarness(t, "App.tsx", src)
	at := strings.Index(src, "const x = 1")
	hs.place(d, at, at)
	hs.a.run("sel.comment")
	hs.settle()
	want := strings.Replace(src, "const x = 1", "// const x = 1", 1)
	expectText(t, d, want)
	hs.a.run("sel.comment")
	hs.settle()
	expectText(t, d, src)
}

func TestCommentJSXFragmentChildren(t *testing.T) {
	src := "const view = (\n  <>\n    <Child />\n  </>\n);\n"
	hs, d := editHarness(t, "App.tsx", src)
	at := strings.Index(src, "<Child />")
	hs.place(d, at, at)
	hs.a.run("sel.comment")
	hs.settle()
	want := strings.Replace(src, "    <Child />", "    {/* <Child /> */}", 1)
	expectText(t, d, want)
	hs.a.run("sel.comment")
	hs.settle()
	expectText(t, d, src)
}

func TestCommentJSXTextChild(t *testing.T) {
	src := "const view = (\n  <p>\n    Hello {name}\n  </p>\n);\n"
	hs, d := editHarness(t, "App.tsx", src)
	at := strings.Index(src, "Hello")
	hs.place(d, at, at)
	hs.a.run("sel.comment")
	hs.settle()
	want := strings.Replace(src, "    Hello {name}", "    {/* Hello {name} */}", 1)
	expectText(t, d, want)
	hs.a.run("sel.comment")
	hs.settle()
	expectText(t, d, src)
}

func TestCommentJSXTagAfterText(t *testing.T) {
	src := "const view = (\n  <p>\n    Click<b>here</b>\n    more\n  </p>\n);\n"
	hs, d := editHarness(t, "App.tsx", src)
	at := strings.Index(src, "more")
	hs.place(d, at, at)
	hs.a.run("sel.comment")
	hs.settle()
	want := strings.Replace(src, "    more", "    {/* more */}", 1)
	expectText(t, d, want)
	hs.a.run("sel.comment")
	hs.settle()
	expectText(t, d, src)
}

func TestCommentJSXDoesNotNestAComment(t *testing.T) {
	src := "const view = (\n  <section>\n    <A />\n    {/* note */}\n    <B />\n  </section>\n);\n"
	hs, d := editHarness(t, "App.tsx", src)
	start := strings.Index(src, "<A />")
	end := strings.Index(src, "<B />") + len("<B />")
	hs.place(d, start, end)
	hs.a.run("sel.comment")
	hs.settle()
	expectText(t, d, src)
}

func TestCommentJSXTagAfterAParen(t *testing.T) {
	src := "render(<App>\n  <Child />\n</App>);\n"
	hs, d := editHarness(t, "main.tsx", src)
	at := strings.Index(src, "<Child />")
	hs.place(d, at, at)
	hs.a.run("sel.comment")
	hs.settle()
	want := strings.Replace(src, "  <Child />", "  {/* <Child /> */}", 1)
	expectText(t, d, want)
	hs.a.run("sel.comment")
	hs.settle()
	expectText(t, d, src)
}

func TestCommentJSXLeavesValueComparisonsAlone(t *testing.T) {
	src := "const s = a?.<string>();\nif (f(x)<limit && y>2) go();\nconst ok = xs[i]<n && n>0;\nconst x = 1;\n"
	hs, d := editHarness(t, "App.tsx", src)
	at := strings.Index(src, "const x = 1")
	hs.place(d, at, at)
	hs.a.run("sel.comment")
	hs.settle()
	want := strings.Replace(src, "const x = 1", "// const x = 1", 1)
	expectText(t, d, want)
}

func TestCommentJSXFollowsAChosenLanguage(t *testing.T) {
	src := "const view = (\n  <section>\n    <G3Alert />\n  </section>\n);\n"
	for _, c := range []struct{ file, language, want string }{
		{"App.tsx", "html", "    <!-- <G3Alert /> -->"},
		{"App.tsx", "typescript", "    {/* <G3Alert /> */}"},
		{"App.ts", "react", "    {/* <G3Alert /> */}"},
	} {
		t.Run(c.file+" as "+c.language, func(t *testing.T) {
			hs, d := editHarness(t, c.file, src)
			hs.a.setDocumentLanguage(d.document, nil, c.language)
			at := strings.Index(src, "<G3Alert")
			hs.place(d, at, at)
			hs.a.run("sel.comment")
			hs.settle()
			expectText(t, d, strings.Replace(src, "    <G3Alert />", c.want, 1))
		})
	}
}
