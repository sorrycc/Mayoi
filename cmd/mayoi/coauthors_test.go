package main

import (
	"testing"

	"mayoi/internal/agents"
	"mayoi/internal/git"
)

func TestWithoutCoAuthorLines(t *testing.T) {
	body := "Keep this.\n\nCo-authored-by: Claude <noreply@anthropic.com>\nco-authored-by: Jane <j@x.io>"
	if got, want := withoutCoAuthorLines(body), "Keep this."; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCoAuthorAgent(t *testing.T) {
	cs := []git.Person{{Name: "Jane", Email: "j@x.io"}, {Name: "Claude", Email: "noreply@anthropic.com"}}
	if h, ok := coAuthorAgent(cs); !ok || h != agents.Claude {
		t.Fatalf("got %q, %v; want claude", h, ok)
	}
	if _, ok := coAuthorAgent(cs[:1]); ok {
		t.Fatal("a person taken for an agent")
	}
	if got := coAuthorNames(cs); got != "Jane, Claude" {
		t.Fatalf("got %q", got)
	}
}
