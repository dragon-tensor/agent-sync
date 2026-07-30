package tui

import (
	"path/filepath"
	"testing"

	"github.com/agent-sync/agent-sync/internal/chat"
	"github.com/agent-sync/agent-sync/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

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

func TestAgentCommandsJoinSuggestionsAndCanBeForced(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "dragon-sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	service := chat.NewService(database, nil)
	t.Cleanup(func() { service.Close() })
	model := NewModel(service)
	model.current = &chat.Chat{ID: "suggestions", ActiveAgent: chat.AgentClaude, ProjectDir: t.TempDir()}
	matches := model.commandMatches("/mo")
	if len(matches) != 1 || matches[0].value != "/model" {
		t.Fatalf("native commands were not suggested: %+v", matches)
	}
	matches = model.commandMatches("//mo")
	if len(matches) != 1 || matches[0].value != "//model" {
		t.Fatalf("forced native command was not suggested: %+v", matches)
	}
}

func TestTerminalKeysPreserveTextAndNavigation(t *testing.T) {
	if got := string(terminalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})); got != "j" {
		t.Fatalf("got %q", got)
	}
	if got := string(terminalKey(tea.KeyMsg{Type: tea.KeyUp})); got != "\x1b[A" {
		t.Fatalf("got %q", got)
	}
}

func TestDoubleEscapeCancelsActiveTurn(t *testing.T) {
	cancelled := 0
	model := Model{busy: true, cancel: func() { cancelled++ }}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if cancelled != 0 || model.escapeAt.IsZero() {
		t.Fatalf("first escape should only arm cancellation: cancelled=%d", cancelled)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if cancelled != 1 || !model.cancelling {
		t.Fatalf("second escape did not cancel: cancelled=%d", cancelled)
	}
}
