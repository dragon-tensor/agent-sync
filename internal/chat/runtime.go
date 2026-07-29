package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/agent-sync/agent-sync/internal/agenthost"
	"github.com/google/uuid"
)

type RunRequest struct {
	ChatID          string
	Agent           Agent
	ProjectDir      string
	Prompt          string
	NativeSessionID string
}

type RunResult struct {
	Reply           string
	NativeSessionID string
	Metrics         AgentMetrics
}

type Runner interface {
	Run(context.Context, RunRequest) (RunResult, error)
}

type CommandRunner struct{}

// BackgroundRunner keeps ACP-capable agents alive behind Dragon Sync.  A
// process is scoped to one Dragon chat + one agent, so switching away does not
// discard the tool's own active context.
type BackgroundRunner struct {
	fallback Runner
	mu       sync.Mutex
	sessions map[string]*backgroundSession
}

type backgroundSession struct {
	host    *agenthost.ACPHost
	id      string
	chatID  string
	agent   Agent
	mu      sync.Mutex
	text    strings.Builder
	metrics AgentMetrics
}

func NewBackgroundRunner(fallback Runner) *BackgroundRunner {
	if fallback == nil {
		fallback = CommandRunner{}
	}
	return &BackgroundRunner{fallback: fallback, sessions: map[string]*backgroundSession{}}
}

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
	reply, nativeID, metrics := parseOutput(request.Agent, output.String())
	if nativeID == "" {
		nativeID = expectedSessionID
	}
	if reply == "" {
		return RunResult{}, fmt.Errorf("%s returned no readable response", request.Agent)
	}
	metrics.Agent = request.Agent
	return RunResult{Reply: reply, NativeSessionID: nativeID, Metrics: metrics}, nil
}

func (r *BackgroundRunner) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	kind, supported := acpKind(request.Agent)
	if !supported {
		return r.fallback.Run(ctx, request)
	}
	session, err := r.getSession(kind, request)
	if err != nil {
		return r.fallback.Run(ctx, request)
	}
	session.mu.Lock()
	session.text.Reset()
	session.metrics = AgentMetrics{Agent: request.Agent}
	session.mu.Unlock()

	cancelled := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = session.host.Cancel(session.id)
		case <-cancelled:
		}
	}()
	err = session.host.Prompt(ctx, session.id, request.Prompt)
	close(cancelled)
	if err != nil {
		return RunResult{}, err
	}
	session.mu.Lock()
	reply, metrics := strings.TrimSpace(session.text.String()), session.metrics
	session.mu.Unlock()
	if reply == "" {
		return RunResult{}, fmt.Errorf("%s returned no readable streamed response", request.Agent)
	}
	return RunResult{Reply: reply, NativeSessionID: session.id, Metrics: metrics}, nil
}

func (r *BackgroundRunner) getSession(kind agenthost.Kind, request RunRequest) (*backgroundSession, error) {
	key := request.ChatID + ":" + string(request.Agent)
	r.mu.Lock()
	if session := r.sessions[key]; session != nil {
		r.mu.Unlock()
		return session, nil
	}
	r.mu.Unlock()
	host, err := agenthost.Start(context.Background(), kind, request.ProjectDir)
	if err != nil {
		return nil, err
	}
	created, err := host.NewSession(context.Background(), request.ProjectDir)
	if err != nil {
		_ = host.Close()
		return nil, err
	}
	session := &backgroundSession{host: host, id: created.ID, chatID: request.ChatID, agent: request.Agent}
	go session.consume()
	r.mu.Lock()
	if existing := r.sessions[key]; existing != nil {
		r.mu.Unlock()
		_ = host.Close()
		return existing, nil
	}
	r.sessions[key] = session
	r.mu.Unlock()
	return session, nil
}

func (s *backgroundSession) consume() {
	for event := range s.host.Events() {
		if id := findStringJSON(event.Params, "sessionId", "session_id"); id != "" && id != s.id {
			continue
		}
		s.mu.Lock()
		if text := findAgentTextJSON(event.Params); text != "" {
			s.text.WriteString(text)
		}
		s.metrics = mergeMetrics(s.metrics, findMetricsJSON(event.Params))
		s.mu.Unlock()
	}
}

func acpKind(agent Agent) (agenthost.Kind, bool) {
	switch agent {
	case AgentOpenCode:
		return agenthost.OpenCode, true
	case AgentGemini:
		return agenthost.Gemini, true
	default:
		return "", false
	}
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

func parseOutput(agent Agent, raw string) (string, string, AgentMetrics) {
	if agent == AgentCodex || agent == AgentOpenCode {
		return parseJSONLines(raw)
	}
	var value any
	if json.Unmarshal([]byte(raw), &value) == nil {
		return findReply(value), findSessionID(value), findMetrics(value)
	}
	return strings.TrimSpace(raw), "", AgentMetrics{}
}

func parseJSONLines(raw string) (string, string, AgentMetrics) {
	var replies []string
	var sessionID string
	var metrics AgentMetrics
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
		metrics = mergeMetrics(metrics, findMetrics(value))
	}
	return strings.TrimSpace(strings.Join(replies, "\n")), sessionID, metrics
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

func findMetrics(value any) AgentMetrics {
	return AgentMetrics{
		Model:         findString(value, "model", "model_name"),
		Effort:        findString(value, "effort", "reasoning_effort", "reasoningEffort"),
		InputTokens:   int(findNumber(value, "input_tokens", "inputTokens", "prompt_tokens", "promptTokens")),
		OutputTokens:  int(findNumber(value, "output_tokens", "outputTokens", "completion_tokens", "completionTokens")),
		ContextWindow: int(findNumber(value, "context_window", "contextWindow", "context_length", "contextLength")),
		CostUSD:       findNumber(value, "total_cost_usd", "cost_usd", "costUSD"),
	}
}

func findMetricsJSON(raw json.RawMessage) AgentMetrics {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return AgentMetrics{}
	}
	return findMetrics(value)
}
func findStringJSON(raw json.RawMessage, keys ...string) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return findString(value, keys...)
}
func findAgentTextJSON(raw json.RawMessage) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return findAgentReply(value)
}

func mergeMetrics(current, next AgentMetrics) AgentMetrics {
	if next.Model != "" {
		current.Model = next.Model
	}
	if next.Effort != "" {
		current.Effort = next.Effort
	}
	if next.InputTokens != 0 {
		current.InputTokens = next.InputTokens
	}
	if next.OutputTokens != 0 {
		current.OutputTokens = next.OutputTokens
	}
	if next.ContextWindow != 0 {
		current.ContextWindow = next.ContextWindow
	}
	if next.CostUSD != 0 {
		current.CostUSD = next.CostUSD
	}
	return current
}

func findString(value any, keys ...string) string {
	switch x := value.(type) {
	case map[string]any:
		for _, key := range keys {
			if candidate, ok := x[key].(string); ok && candidate != "" {
				return candidate
			}
		}
		for _, candidate := range x {
			if result := findString(candidate, keys...); result != "" {
				return result
			}
		}
	case []any:
		for _, candidate := range x {
			if result := findString(candidate, keys...); result != "" {
				return result
			}
		}
	}
	return ""
}

func findNumber(value any, keys ...string) float64 {
	switch x := value.(type) {
	case map[string]any:
		for _, key := range keys {
			if candidate, ok := x[key].(float64); ok {
				return candidate
			}
		}
		for _, candidate := range x {
			if result := findNumber(candidate, keys...); result != 0 {
				return result
			}
		}
	case []any:
		for _, candidate := range x {
			if result := findNumber(candidate, keys...); result != 0 {
				return result
			}
		}
	}
	return 0
}
