package tui

import "testing"

func TestMatchingCommands(t *testing.T) {
	if got := matchingCommands("/s"); len(got) != 2 || got[0].value != "/start" || got[1].value != "/switch" {
		t.Fatalf("got %#v, want /start and /switch", got)
	}
	if got := matchingCommands("/r"); len(got) != 1 || got[0].value != "/resume" {
		t.Fatalf("got %#v, want /resume", got)
	}
	if got := matchingCommands("hello"); len(got) != 0 {
		t.Fatalf("got %#v, want no command suggestions", got)
	}
}

func TestIncompleteCommandResolvesToHighlightedMatch(t *testing.T) {
	model := Model{draft: "/res", command: 0}
	matches := matchingCommands(model.draft)
	if len(matches) != 1 || matches[model.command].value != "/resume" {
		t.Fatalf("got %#v, want highlighted /resume", matches)
	}
}
