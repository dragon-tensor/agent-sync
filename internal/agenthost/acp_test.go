package agenthost

import "testing"

func TestCommandForSupportedAgents(t *testing.T) {
	command, args, err := commandFor(OpenCode)
	if err != nil || command != "opencode" || len(args) != 1 || args[0] != "acp" {
		t.Fatalf("got %q %q %v", command, args, err)
	}
	command, args, err = commandFor(Gemini)
	if err != nil || command != "gemini" || len(args) != 1 || args[0] != "--acp" {
		t.Fatalf("got %q %q %v", command, args, err)
	}
}
