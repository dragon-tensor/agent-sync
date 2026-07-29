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

func TestACPAgentMapping(t *testing.T) {
	if kind, ok := acpKind(AgentOpenCode); !ok || kind != "opencode" {
		t.Fatalf("got %q %t", kind, ok)
	}
	if _, ok := acpKind(AgentClaude); ok {
		t.Fatal("claude must not use ACP")
	}
}
