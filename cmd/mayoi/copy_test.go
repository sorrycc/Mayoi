package main

// The words Mayoi shows follow a few rules in every language, checked here
// over the catalogs rather than in review: the same placeholders in each
// language, a count's noun that agrees with it, an ellipsis that is one
// character, and no words that plead or pad.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// catalog reads the messages of one language, by key.
func catalog(t *testing.T, lang string) map[string]string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "locales", lang+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(src, &m); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for k, v := range m {
		if s, ok := v.(string); ok && !strings.HasPrefix(k, "@") {
			out[k] = s
		}
	}
	return out
}

// argsOf returns the names of a message's arguments, as {name} or
// {name, plural, …}, in the branches of a plural or a select too, whose
// keys and bodies are not arguments.
func argsOf(s string) []string {
	var out []string
	var message func(i int) int
	// argument reads one from the { at i, returning the index after its }.
	argument := func(i int) int {
		j := i + 1
		for j < len(s) && s[j] != ',' && s[j] != '}' {
			j++
		}
		if name := strings.TrimSpace(s[i+1 : j]); !slices.Contains(out, name) {
			out = append(out, name)
		}
		if j >= len(s) || s[j] == '}' {
			return j + 1
		}
		// {name, type, …}: a plural's or a select's branches are a key
		// and a message in braces each.
		k := strings.IndexAny(s[j+1:], ",}")
		if k < 0 {
			return len(s)
		}
		j += 1 + k
		if s[j] == '}' {
			return j + 1
		}
		for j++; j < len(s); {
			switch s[j] {
			case '}':
				return j + 1
			case '{':
				j = message(j+1) + 1
			default:
				j++
			}
		}
		return j
	}
	// message reads a message from i to the } that ends it, or the end,
	// returning that }'s index.
	message = func(i int) int {
		for i < len(s) {
			switch s[i] {
			case '{':
				i = argument(i)
			case '}':
				return i
			default:
				i++
			}
		}
		return i
	}
	message(0)
	slices.Sort(out)
	return out
}

func TestCopyRules(t *testing.T) {
	en := catalog(t, "en")
	for _, lang := range []string{"ja", "zh-Hans"} {
		tr := catalog(t, lang)
		for k, s := range en {
			got, ok := tr[k]
			if !ok {
				t.Errorf("%s: %s is missing", lang, k)
				continue
			}
			if !slices.Equal(argsOf(s), argsOf(got)) {
				t.Errorf("%s: %s takes %v, en %v", lang, k, argsOf(got), argsOf(s))
			}
			if strings.Contains(got, "...") {
				t.Errorf("%s: %s writes ... where … is one character: %q", lang, k, got)
			}
		}
	}
	// A count before a plural noun, as "{n} files", reads "1 files": it
	// needs {n, plural, …}.
	count := regexp.MustCompile(`\{\w+\} (files|lines|commits|changes|matches|items|results|seconds|minutes|hours|days|tabs|folders|projects|terminals|plugins|errors|warnings|problems)\b`)
	// Words that plead, pad, or ask again what was asked.
	padding := regexp.MustCompile(`\b(Please|please|[Ss]uccessfully|Click here|click here|Are you sure)\b|\(s\)`)
	for k, s := range en {
		if strings.Contains(s, "...") {
			t.Errorf("en: %s writes ... where … is one character: %q", k, s)
		}
		if m := count.FindString(s); m != "" {
			t.Errorf("en: %s puts a count before a plural noun, %q: use {…, plural, …}", k, m)
		}
		if m := padding.FindString(s); m != "" {
			t.Errorf("en: %s says %q: say what happens instead", k, m)
		}
	}
}

func TestAgoTurnsToADate(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local)
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{now.Add(-3 * 24 * time.Hour), "3 days ago"},
		{time.Date(2026, 3, 4, 9, 0, 0, 0, time.Local), "Mar 4"},
		{time.Date(2024, 3, 4, 9, 0, 0, 0, time.Local), "Mar 4, 2024"},
	} {
		if got := ago(c.t, now); got != c.want {
			t.Errorf("ago(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestArgsOf(t *testing.T) {
	for s, want := range map[string][]string{
		"{name} is a binary file": {"name"},
		"{tag, select, true {Creating tag {name}…} other {Creating branch {name}…}}": {"name", "tag"},
		"{op, select, merge {merge} rebase {rebase} other {{op}}}":                   {"op"},
		"{count, plural, one {# day ago} other {# days ago}}":                        {"count"},
	} {
		if got := argsOf(s); !slices.Equal(got, want) {
			t.Errorf("argsOf(%q) = %v, want %v", s, got, want)
		}
	}
}
