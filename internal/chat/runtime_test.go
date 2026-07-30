package chat

import "testing"

func TestCodexResumeCommandUsesPersistedSession(t *testing.T) {
	command, args, sessionID, err := commandFor(RunRequest{Agent: AgentCodex, ProjectDir: ".", Prompt: "continue", NativeSessionID: "thread-123"})
	if err != nil {
		t.Fatal(err)
	}
	if command != "codex" || sessionID != "thread-123" {
		t.Fatalf("got command=%q session=%q", command, sessionID)
	}
	if args[0] != "exec" || args[1] != "resume" {
		t.Fatalf("want codex exec resume, got %q", args)
	}
	for _, arg := range args {
		if arg == "-C" {
			t.Fatalf("resume command must rely on the process directory, got %q", args)
		}
	}
}

func TestParseCodexOutput(t *testing.T) {
	reply, sessionID, _ := parseOutput(AgentCodex, "{\"type\":\"thread.started\",\"thread_id\":\"thread-123\"}\n{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"Done\"}}\n")
	if reply != "Done" || sessionID != "thread-123" {
		t.Fatalf("got reply=%q session=%q", reply, sessionID)
	}
}

func TestParseMetricsFromAgentEvents(t *testing.T) {
	_, _, metrics := parseOutput(AgentCodex, "{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":12,\"output_tokens\":34},\"model\":\"gpt-5\",\"reasoning_effort\":\"high\"}\n")
	if metrics.InputTokens != 12 || metrics.OutputTokens != 34 || metrics.Model != "gpt-5" || metrics.Effort != "high" {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
}

func TestParseACPUsageCost(t *testing.T) {
	value := map[string]any{
		"sessionUpdate": "usage_update",
		"used":          float64(2048),
		"size":          float64(8192),
		"cost":          map[string]any{"amount": 0.25, "currency": "USD"},
	}
	metrics := findMetrics(value)
	metrics.ContextUsed = int(findNumber(value, "used"))
	metrics.ContextWindow = int(findNumber(value, "size"))
	if metrics.ContextUsed != 2048 || metrics.ContextWindow != 8192 || metrics.CostUSD != 0.25 {
		t.Fatalf("unexpected ACP usage: %+v", metrics)
	}
}

func TestACPAgentMapping(t *testing.T) {
	if kind, ok := acpKind(AgentOpenCode); !ok || kind != "opencode" {
		t.Fatalf("got %q %t", kind, ok)
	}
	if _, ok := acpKind(AgentClaude); ok {
		t.Fatal("claude must not use ACP")
	}
}

func TestPTYResumeCommandsUseNativeSession(t *testing.T) {
	command, args, id, err := ptyCommand(AgentClaude, "claude-session")
	if err != nil {
		t.Fatal(err)
	}
	if command != "claude" || id != "claude-session" || !containsPair(args, "--resume", "claude-session") {
		t.Fatalf("unexpected Claude PTY command: %q %q %q", command, args, id)
	}
	command, args, id, err = ptyCommand(AgentCodex, "codex-thread")
	if err != nil {
		t.Fatal(err)
	}
	if command != "codex" || id != "codex-thread" || !containsPair(args, "resume", "codex-thread") {
		t.Fatalf("unexpected Codex PTY command: %q %q %q", command, args, id)
	}
}

func TestACPAvailableCommandsAreNormalizedAndDeduplicated(t *testing.T) {
	value := map[string]any{
		"sessionUpdate": "available_commands_update",
		"availableCommands": []any{
			map[string]any{"name": "model", "description": "Choose model"},
			map[string]any{"name": "/model", "description": "Latest description"},
		},
	}
	commands := commandsFrom(value)
	if len(commands) != 1 || commands[0].Name != "/model" || commands[0].Description != "Latest description" {
		t.Fatalf("unexpected commands: %+v", commands)
	}
}

func containsPair(values []string, first, second string) bool {
	for index := 0; index+1 < len(values); index++ {
		if values[index] == first && values[index+1] == second {
			return true
		}
	}
	return false
}
