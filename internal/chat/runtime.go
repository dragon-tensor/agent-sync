package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

type RunRequest struct {
	Agent           Agent
	ProjectDir      string
	Prompt          string
	NativeSessionID string
}

type RunResult struct {
	Reply           string
	NativeSessionID string
}

type Runner interface {
	Run(context.Context, RunRequest) (RunResult, error)
}

type CommandRunner struct{}

func AvailableAgents() []Agent {
	candidates := []Agent{AgentClaude, AgentCodex, AgentOpenCode, AgentGemini}
	available := make([]Agent, 0, len(candidates))
	for _, agent := range candidates {
		if _, err := exec.LookPath(string(agent)); err == nil {
			available = append(available, agent)
		}
	}
	return available
}

func (CommandRunner) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	command, args, expectedSessionID, err := commandFor(request)
	if err != nil {
		return RunResult{}, err
	}
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = request.ProjectDir
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return RunResult{}, fmt.Errorf("%s: %w\n%s", request.Agent, err, strings.TrimSpace(output.String()))
	}
	reply, nativeID := parseOutput(request.Agent, output.String())
	if nativeID == "" {
		nativeID = expectedSessionID
	}
	if reply == "" {
		return RunResult{}, fmt.Errorf("%s returned no readable response", request.Agent)
	}
	return RunResult{Reply: reply, NativeSessionID: nativeID}, nil
}

func commandFor(request RunRequest) (string, []string, string, error) {
	prompt := request.Prompt
	if strings.TrimSpace(prompt) == "" {
		return "", nil, "", fmt.Errorf("empty agent prompt")
	}
	project := request.ProjectDir
	if project == "" {
		project = "."
	}
	if absolute, err := filepath.Abs(project); err == nil {
		project = absolute
	}

	switch request.Agent {
	case AgentClaude:
		if request.NativeSessionID != "" {
			return "claude", []string{"-p", "--output-format", "json", "--resume", request.NativeSessionID, prompt}, request.NativeSessionID, nil
		}
		id := uuid.NewString()
		return "claude", []string{"-p", "--output-format", "json", "--session-id", id, prompt}, id, nil
	case AgentCodex:
		if request.NativeSessionID != "" {
			return "codex", []string{"exec", "resume", "--json", "--skip-git-repo-check", request.NativeSessionID, prompt}, request.NativeSessionID, nil
		}
		return "codex", []string{"exec", "--json", "--skip-git-repo-check", prompt}, "", nil
	case AgentOpenCode:
		if request.NativeSessionID != "" {
			return "opencode", []string{"run", "--format", "json", "--dir", project, "--session", request.NativeSessionID, prompt}, request.NativeSessionID, nil
		}
		return "opencode", []string{"run", "--format", "json", "--dir", project, prompt}, "", nil
	case AgentGemini:
		if request.NativeSessionID != "" {
			return "gemini", []string{"--prompt", prompt, "--output-format", "json", "--resume", request.NativeSessionID}, request.NativeSessionID, nil
		}
		id := uuid.NewString()
		return "gemini", []string{"--prompt", prompt, "--output-format", "json", "--session-id", id}, id, nil
	default:
		return "", nil, "", fmt.Errorf("unsupported local agent %q", request.Agent)
	}
}

func parseOutput(agent Agent, raw string) (string, string) {
	if agent == AgentCodex || agent == AgentOpenCode {
		return parseJSONLines(raw)
	}
	var value any
	if json.Unmarshal([]byte(raw), &value) == nil {
		return findReply(value), findSessionID(value)
	}
	return strings.TrimSpace(raw), ""
}

func parseJSONLines(raw string) (string, string) {
	var replies []string
	var sessionID string
	for _, line := range strings.Split(raw, "\n") {
		var value any
		if json.Unmarshal([]byte(line), &value) != nil {
			continue
		}
		if id := findSessionID(value); id != "" && sessionID == "" {
			sessionID = id
		}
		if reply := findAgentReply(value); reply != "" {
			replies = append(replies, reply)
		}
	}
	return strings.TrimSpace(strings.Join(replies, "\n")), sessionID
}

func findAgentReply(value any) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if item, ok := object["item"].(map[string]any); ok {
		kind, _ := item["type"].(string)
		if kind == "agent_message" || kind == "text" {
			return findReply(item)
		}
	}
	if part, ok := object["part"].(map[string]any); ok {
		kind, _ := part["type"].(string)
		if kind == "text" {
			return findReply(part)
		}
	}
	if typ, _ := object["type"].(string); strings.Contains(typ, "message") {
		return findReply(object)
	}
	return ""
}

func findReply(value any) string {
	switch x := value.(type) {
	case map[string]any:
		for _, key := range []string{"result", "response", "text", "content", "message"} {
			if candidate, ok := x[key]; ok {
				if text := findReply(candidate); text != "" {
					return text
				}
			}
		}
	case []any:
		var parts []string
		for _, candidate := range x {
			if text := findReply(candidate); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case string:
		return strings.TrimSpace(x)
	}
	return ""
}

func findSessionID(value any) string {
	switch x := value.(type) {
	case map[string]any:
		for _, key := range []string{"session_id", "sessionID", "thread_id", "threadId"} {
			if id, ok := x[key].(string); ok && id != "" {
				return id
			}
		}
		for _, candidate := range x {
			if id := findSessionID(candidate); id != "" {
				return id
			}
		}
	case []any:
		for _, candidate := range x {
			if id := findSessionID(candidate); id != "" {
				return id
			}
		}
	}
	return ""
}
