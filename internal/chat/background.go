package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/agent-sync/agent-sync/internal/agenthost"
	"github.com/google/uuid"
)

// RuntimeController is the richer surface used by the TUI. Runner remains
// deliberately small so tests and alternative non-interactive runners can
// still implement Dragon Sync's canonical turn contract.
type RuntimeController interface {
	Runner
	Ensure(context.Context, RunRequest) (RuntimeState, error)
	NativeCommands(chatID string, agent Agent) []NativeCommand
	RunNative(context.Context, RunRequest) (RunResult, bool, error)
	State(chatID string, agent Agent) RuntimeState
	SendKey(chatID string, agent Agent, value []byte) error
	Resize(chatID string, agent Agent, cols, rows int) error
	LeaveControl(chatID string, agent Agent)
	ResolvePermission(chatID string, agent Agent, requestID, optionID string) error
	Cancel(chatID string, agent Agent) error
	Close() error
}

// BackgroundRunner owns one live runtime per Dragon chat and agent. ACP agents
// stay connected over JSON-RPC. Claude and Codex retain a native PTY control
// tab between reliable structured turns.
type BackgroundRunner struct {
	fallback Runner
	mu       sync.RWMutex
	sessions map[string]*backgroundSession
	closed   bool
}

type backgroundSession struct {
	chatID string
	agent  Agent
	cwd    string

	turnMu sync.Mutex
	mu     sync.RWMutex

	acp          *agenthost.ACPHost
	pty          *agenthost.PTYHost
	id           string
	kind         RuntimeKind
	status       RuntimeStatus
	text         strings.Builder
	metrics      AgentMetrics
	commands     []NativeCommand
	capabilities map[string]any
	config       map[string]any
	lastErr      string

	terminalActive bool
	permission     *PermissionRequest
	permissionWait map[string]chan string
}

func NewBackgroundRunner(fallback Runner) *BackgroundRunner {
	if fallback == nil {
		fallback = CommandRunner{}
	}
	return &BackgroundRunner{fallback: fallback, sessions: map[string]*backgroundSession{}}
}

func (r *BackgroundRunner) Ensure(ctx context.Context, request RunRequest) (RuntimeState, error) {
	session, err := r.session(ctx, request)
	if err != nil {
		return RuntimeState{}, err
	}
	if session.acp == nil {
		session.turnMu.Lock()
		err = session.ensurePTY()
		session.turnMu.Unlock()
		if err != nil {
			session.setStatus(RuntimeCrashed, err.Error())
			return r.State(request.ChatID, request.Agent), err
		}
	}
	return r.State(request.ChatID, request.Agent), nil
}

func (r *BackgroundRunner) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	session, err := r.session(ctx, request)
	if err != nil {
		return RunResult{}, err
	}
	session.turnMu.Lock()
	defer session.turnMu.Unlock()
	if session.acp != nil {
		return session.runACP(ctx, request.Prompt)
	}

	session.stopPTY()
	session.setStatus(RuntimeWorking, "")
	result, err := r.fallback.Run(ctx, request)
	if err != nil {
		if ctx.Err() != nil {
			session.setStatus(RuntimeIdle, "")
		} else {
			session.setStatus(RuntimeCrashed, err.Error())
		}
		return RunResult{}, err
	}
	session.mu.Lock()
	session.id = result.NativeSessionID
	session.metrics = mergeMetrics(session.metrics, result.Metrics)
	session.metrics.Agent = request.Agent
	session.mu.Unlock()
	if err := session.ensurePTY(); err != nil {
		// The structured turn completed successfully. Keep its response and
		// expose the native-control startup error as a degraded runtime state.
		session.setStatus(RuntimeIdle, err.Error())
	} else {
		session.setStatus(RuntimeIdle, "")
	}
	return result, nil
}

func (r *BackgroundRunner) RunNative(ctx context.Context, request RunRequest) (RunResult, bool, error) {
	session, err := r.session(ctx, request)
	if err != nil {
		return RunResult{}, false, err
	}
	session.turnMu.Lock()
	defer session.turnMu.Unlock()
	if session.acp != nil {
		result, err := session.runACP(ctx, request.Prompt)
		return result, false, err
	}
	if err := session.ensurePTY(); err != nil {
		session.setStatus(RuntimeCrashed, err.Error())
		return RunResult{}, false, err
	}
	session.mu.Lock()
	host := session.pty
	session.terminalActive = true
	session.status = RuntimeWaitingForUser
	session.mu.Unlock()
	if err := host.SendText(request.Prompt); err != nil {
		session.setStatus(RuntimeCrashed, err.Error())
		return RunResult{}, false, err
	}
	return RunResult{NativeSessionID: session.nativeID()}, true, nil
}

func (r *BackgroundRunner) NativeCommands(chatID string, agent Agent) []NativeCommand {
	result := append([]NativeCommand(nil), staticCommands(agent)...)
	if session := r.existing(chatID, agent); session != nil {
		session.mu.RLock()
		result = mergeCommands(result, session.commands)
		session.mu.RUnlock()
	}
	return result
}

func (r *BackgroundRunner) State(chatID string, agent Agent) RuntimeState {
	session := r.existing(chatID, agent)
	if session == nil {
		kind := RuntimePTY
		if _, ok := acpKind(agent); ok {
			kind = RuntimeACP
		}
		return RuntimeState{ChatID: chatID, Agent: agent, Kind: kind, Status: RuntimeStopped, Commands: staticCommands(agent), Config: map[string]any{}}
	}
	session.mu.RLock()
	defer session.mu.RUnlock()
	state := RuntimeState{
		ChatID:          chatID,
		Agent:           agent,
		Kind:            session.kind,
		Status:          session.status,
		NativeSessionID: session.id,
		Commands:        mergeCommands(staticCommands(agent), session.commands),
		Capabilities:    cloneMap(session.capabilities),
		Config:          cloneMap(session.config),
		Metrics:         session.metrics,
		TerminalActive:  session.terminalActive,
		LastError:       session.lastErr,
		PendingApproval: clonePermission(session.permission),
	}
	if session.pty != nil {
		state.Terminal = session.pty.Snapshot()
	}
	return state
}

func (r *BackgroundRunner) SendKey(chatID string, agent Agent, value []byte) error {
	session := r.existing(chatID, agent)
	if session == nil {
		return fmt.Errorf("%s runtime is not started", agent)
	}
	session.mu.RLock()
	host := session.pty
	session.mu.RUnlock()
	if host == nil {
		return fmt.Errorf("%s does not have an active native terminal", agent)
	}
	return host.Write(value)
}

func (r *BackgroundRunner) Resize(chatID string, agent Agent, cols, rows int) error {
	session := r.existing(chatID, agent)
	if session == nil {
		return nil
	}
	session.mu.RLock()
	host := session.pty
	session.mu.RUnlock()
	if host == nil {
		return nil
	}
	return host.Resize(cols, rows)
}

func (r *BackgroundRunner) LeaveControl(chatID string, agent Agent) {
	session := r.existing(chatID, agent)
	if session == nil {
		return
	}
	session.mu.Lock()
	session.terminalActive = false
	if session.status == RuntimeWaitingForUser {
		session.status = RuntimeIdle
	}
	session.mu.Unlock()
}

func (r *BackgroundRunner) ResolvePermission(chatID string, agent Agent, requestID, optionID string) error {
	session := r.existing(chatID, agent)
	if session == nil {
		return fmt.Errorf("%s runtime is not started", agent)
	}
	session.mu.Lock()
	wait := session.permissionWait[requestID]
	if wait == nil {
		session.mu.Unlock()
		return fmt.Errorf("permission request is no longer active")
	}
	delete(session.permissionWait, requestID)
	session.permission = nil
	session.status = RuntimeWorking
	session.mu.Unlock()
	wait <- optionID
	return nil
}

func (r *BackgroundRunner) Cancel(chatID string, agent Agent) error {
	session := r.existing(chatID, agent)
	if session == nil {
		return nil
	}
	session.mu.RLock()
	acp, terminal, id := session.acp, session.pty, session.id
	session.mu.RUnlock()
	if acp != nil {
		return acp.Cancel(id)
	}
	if terminal != nil {
		return terminal.Cancel()
	}
	return nil
}

func (r *BackgroundRunner) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	sessions := make([]*backgroundSession, 0, len(r.sessions))
	for _, session := range r.sessions {
		sessions = append(sessions, session)
	}
	r.mu.Unlock()
	var failures []string
	for _, session := range sessions {
		if err := session.close(); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("close agent runtimes: %s", strings.Join(failures, "; "))
	}
	return nil
}

func (r *BackgroundRunner) session(ctx context.Context, request RunRequest) (*backgroundSession, error) {
	key := runtimeKey(request.ChatID, request.Agent)
	r.mu.RLock()
	existing, closed := r.sessions[key], r.closed
	r.mu.RUnlock()
	if closed {
		return nil, fmt.Errorf("agent runtime manager is closed")
	}
	if existing != nil {
		if existing.nativeID() == "" && request.NativeSessionID != "" {
			existing.mu.Lock()
			existing.id = request.NativeSessionID
			existing.mu.Unlock()
		}
		return existing, nil
	}
	session := &backgroundSession{
		chatID:         request.ChatID,
		agent:          request.Agent,
		cwd:            request.ProjectDir,
		id:             request.NativeSessionID,
		status:         RuntimeStarting,
		config:         map[string]any{},
		permissionWait: map[string]chan string{},
	}
	if kind, supported := acpKind(request.Agent); supported {
		session.kind = RuntimeACP
		host, err := agenthost.Start(ctx, kind, request.ProjectDir)
		if err != nil {
			return nil, fmt.Errorf("start %s ACP runtime: %w", request.Agent, err)
		}
		session.acp = host
		_ = json.Unmarshal(host.Capabilities(), &session.capabilities)
		if session.capabilities == nil {
			session.capabilities = map[string]any{}
		}
		host.SetRequestHandler(session.handleACPRequest)
		var created agenthost.Session
		if request.NativeSessionID != "" {
			created, err = host.LoadSession(ctx, request.NativeSessionID, request.ProjectDir)
		} else {
			created, err = host.NewSession(ctx, request.ProjectDir)
		}
		if err != nil {
			_ = host.Close()
			return nil, fmt.Errorf("open %s ACP session: %w", request.Agent, err)
		}
		session.id = created.ID
		session.config = configFromSession(created)
		session.status = RuntimeIdle
		go session.consumeACP()
		go session.watchACP()
	} else {
		session.kind = RuntimePTY
		session.status = RuntimeStopped
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = session.close()
		return nil, fmt.Errorf("agent runtime manager is closed")
	}
	if winner := r.sessions[key]; winner != nil {
		r.mu.Unlock()
		_ = session.close()
		return winner, nil
	}
	r.sessions[key] = session
	r.mu.Unlock()
	return session, nil
}

func (r *BackgroundRunner) existing(chatID string, agent Agent) *backgroundSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sessions[runtimeKey(chatID, agent)]
}

func (s *backgroundSession) runACP(ctx context.Context, prompt string) (RunResult, error) {
	s.mu.Lock()
	s.text.Reset()
	s.metrics.Agent = s.agent
	s.status = RuntimeWorking
	s.lastErr = ""
	host, id := s.acp, s.id
	s.mu.Unlock()
	cancelled := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = host.Cancel(id)
		case <-cancelled:
		}
	}()
	rawResult, err := host.PromptResult(ctx, id, prompt)
	close(cancelled)
	if err != nil {
		if ctx.Err() != nil {
			s.setStatus(RuntimeIdle, "")
		} else {
			s.setStatus(RuntimeCrashed, err.Error())
		}
		return RunResult{}, err
	}
	s.mu.Lock()
	if len(rawResult) > 0 {
		s.metrics = mergeMetrics(s.metrics, findMetricsJSON(rawResult))
	}
	reply := strings.TrimSpace(s.text.String())
	metrics := s.metrics
	s.status = RuntimeIdle
	s.mu.Unlock()
	if reply == "" {
		return RunResult{}, fmt.Errorf("%s returned no readable streamed response", s.agent)
	}
	return RunResult{Reply: reply, NativeSessionID: id, Metrics: metrics}, nil
}

func (s *backgroundSession) consumeACP() {
	for event := range s.acp.Events() {
		if event.SessionID != "" && event.SessionID != s.nativeID() {
			continue
		}
		var value any
		_ = json.Unmarshal(event.Params, &value)
		s.mu.Lock()
		switch event.Method {
		case "agent/stderr":
			if message := findString(value, "message"); message != "" {
				s.lastErr = message
			}
		default:
			updateType := findString(value, "sessionUpdate", "session_update", "type")
			if strings.Contains(updateType, "agent_message") {
				if text := findString(value, "text"); text != "" {
					s.text.WriteString(text)
				}
			}
			if strings.Contains(updateType, "available_commands") {
				s.commands = commandsFrom(value)
			}
			if strings.Contains(updateType, "config_option") {
				s.config = configFrom(value)
			}
			nextMetrics := findMetrics(value)
			if strings.Contains(updateType, "usage_update") {
				nextMetrics.ContextUsed = int(findNumber(value, "used"))
				nextMetrics.ContextWindow = int(findNumber(value, "size"))
			}
			s.metrics = mergeMetrics(s.metrics, nextMetrics)
		}
		s.mu.Unlock()
	}
}

func (s *backgroundSession) watchACP() {
	err, ok := <-s.acp.Done()
	if !ok || err == nil {
		s.setStatus(RuntimeStopped, "")
		return
	}
	s.setStatus(RuntimeCrashed, err.Error())
}

func (s *backgroundSession) handleACPRequest(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if method != "session/request_permission" {
		return nil, fmt.Errorf("unsupported ACP client request %s", method)
	}
	var value any
	if err := json.Unmarshal(params, &value); err != nil {
		return nil, err
	}
	request := permissionFrom(value)
	if request.ID == "" {
		request.ID = uuid.NewString()
	}
	wait := make(chan string, 1)
	s.mu.Lock()
	s.permission = &request
	s.permissionWait[request.ID] = wait
	s.status = RuntimeWaitingForUser
	s.mu.Unlock()
	select {
	case optionID := <-wait:
		if optionID == "" {
			return map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}, nil
		}
		return map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": optionID}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Minute):
		s.mu.Lock()
		delete(s.permissionWait, request.ID)
		s.permission = nil
		s.status = RuntimeWorking
		s.mu.Unlock()
		return map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}, nil
	}
}

func (s *backgroundSession) ensurePTY() error {
	s.mu.RLock()
	if s.pty != nil {
		s.mu.RUnlock()
		return nil
	}
	id := s.id
	s.mu.RUnlock()
	command, args, assignedID, err := ptyCommand(s.agent, id)
	if err != nil {
		return err
	}
	host, err := agenthost.StartPTY(command, args, s.cwd, 100, 30)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.pty != nil {
		s.mu.Unlock()
		_ = host.Close()
		return nil
	}
	s.pty = host
	if s.id == "" {
		s.id = assignedID
	}
	s.status = RuntimeIdle
	s.mu.Unlock()
	go s.watchPTY(host)
	return nil
}

func (s *backgroundSession) watchPTY(host *agenthost.PTYHost) {
	err, ok := <-host.Done()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pty != host {
		return
	}
	s.pty = nil
	s.terminalActive = false
	if ok && err != nil {
		s.status = RuntimeCrashed
		s.lastErr = err.Error()
	} else {
		s.status = RuntimeStopped
	}
}

func (s *backgroundSession) stopPTY() {
	s.mu.Lock()
	host := s.pty
	s.pty = nil
	s.terminalActive = false
	s.mu.Unlock()
	if host != nil {
		_ = host.Close()
	}
}

func (s *backgroundSession) close() error {
	s.mu.Lock()
	acp, terminal := s.acp, s.pty
	s.acp, s.pty = nil, nil
	s.status = RuntimeStopped
	s.mu.Unlock()
	var first error
	if acp != nil {
		first = acp.Close()
	}
	if terminal != nil {
		if err := terminal.Close(); first == nil {
			first = err
		}
	}
	return first
}

func (s *backgroundSession) setStatus(status RuntimeStatus, failure string) {
	s.mu.Lock()
	s.status, s.lastErr = status, failure
	s.mu.Unlock()
}

func (s *backgroundSession) nativeID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.id
}

func ptyCommand(agent Agent, nativeSessionID string) (string, []string, string, error) {
	switch agent {
	case AgentClaude:
		id := nativeSessionID
		if id == "" {
			id = uuid.NewString()
		}
		if nativeSessionID != "" {
			return "claude", []string{"--ax-screen-reader", "--resume", id}, id, nil
		}
		return "claude", []string{"--ax-screen-reader", "--session-id", id}, id, nil
	case AgentCodex:
		if nativeSessionID != "" {
			return "codex", []string{"--no-alt-screen", "resume", nativeSessionID}, nativeSessionID, nil
		}
		return "codex", []string{"--no-alt-screen"}, "", nil
	default:
		return "", nil, "", fmt.Errorf("%s has no PTY fallback", agent)
	}
}

func staticCommands(agent Agent) []NativeCommand {
	names := map[Agent][]NativeCommand{
		AgentClaude: {
			{Name: "/help", Description: "Show Claude Code help"},
			{Name: "/model", Description: "Choose the active model"},
			{Name: "/effort", Description: "Change reasoning effort"},
			{Name: "/permissions", Description: "Review permission mode"},
			{Name: "/compact", Description: "Compact the current context"},
			{Name: "/context", Description: "Inspect context usage"},
			{Name: "/cost", Description: "Show token cost"},
			{Name: "/doctor", Description: "Check Claude Code setup"},
		},
		AgentCodex: {
			{Name: "/help", Description: "Show Codex commands"},
			{Name: "/model", Description: "Choose model and reasoning effort"},
			{Name: "/permissions", Description: "Change approval behavior"},
			{Name: "/status", Description: "Show session configuration and usage"},
			{Name: "/compact", Description: "Compact the current context"},
			{Name: "/review", Description: "Review the working tree"},
			{Name: "/diff", Description: "Show the current diff"},
		},
		AgentOpenCode: {
			{Name: "/help", Description: "Show OpenCode commands"},
			{Name: "/models", Description: "Choose a model"},
			{Name: "/agents", Description: "Choose an agent"},
			{Name: "/compact", Description: "Compact the session"},
			{Name: "/status", Description: "Show session status"},
		},
		AgentGemini: {
			{Name: "/help", Description: "Show Gemini commands"},
			{Name: "/model", Description: "Choose a model"},
			{Name: "/memory", Description: "Manage saved memory"},
			{Name: "/compress", Description: "Compress conversation context"},
			{Name: "/stats", Description: "Show session usage"},
			{Name: "/tools", Description: "Show available tools"},
		},
	}
	return append([]NativeCommand(nil), names[agent]...)
}

func commandsFrom(value any) []NativeCommand {
	var result []NativeCommand
	walkObjects(value, func(object map[string]any) {
		name := stringValue(object, "name", "command")
		if name == "" {
			return
		}
		if !strings.HasPrefix(name, "/") {
			name = "/" + name
		}
		result = append(result, NativeCommand{
			Name:        name,
			Description: stringValue(object, "description"),
			InputHint:   stringValue(object, "inputHint", "input_hint", "hint"),
		})
	})
	return mergeCommands(nil, result)
}

func permissionFrom(value any) PermissionRequest {
	request := PermissionRequest{ID: uuid.NewString(), Title: "Agent requests permission"}
	if title := findString(value, "title", "description", "message"); title != "" {
		request.Title = title
	}
	walkObjects(value, func(object map[string]any) {
		id := stringValue(object, "optionId", "option_id")
		if id == "" {
			return
		}
		label := stringValue(object, "name", "label")
		if label == "" {
			label = id
		}
		request.Options = append(request.Options, PermissionOption{ID: id, Label: label, Kind: stringValue(object, "kind")})
	})
	if len(request.Options) == 0 {
		request.Options = []PermissionOption{{ID: "", Label: "Reject", Kind: "reject"}}
	}
	return request
}

func configFromSession(session agenthost.Session) map[string]any {
	var value any
	if json.Unmarshal(session.ConfigOptions, &value) != nil {
		return map[string]any{}
	}
	return configFrom(value)
}

func configFrom(value any) map[string]any {
	result := map[string]any{}
	walkObjects(value, func(object map[string]any) {
		id := stringValue(object, "id", "configId", "config_id")
		if id == "" {
			return
		}
		for _, key := range []string{"currentValue", "current_value", "value"} {
			if current, ok := object[key]; ok {
				result[id] = current
				return
			}
		}
	})
	return result
}

func walkObjects(value any, visit func(map[string]any)) {
	switch item := value.(type) {
	case map[string]any:
		visit(item)
		for _, child := range item {
			walkObjects(child, visit)
		}
	case []any:
		for _, child := range item {
			walkObjects(child, visit)
		}
	}
}

func stringValue(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if result, ok := value[key].(string); ok {
			return result
		}
	}
	return ""
}

func mergeCommands(groups ...[]NativeCommand) []NativeCommand {
	seen := map[string]NativeCommand{}
	for _, group := range groups {
		for _, command := range group {
			if command.Name == "" {
				continue
			}
			name := command.Name
			if !strings.HasPrefix(name, "/") {
				name = "/" + name
			}
			command.Name = name
			seen[strings.ToLower(name)] = command
		}
	}
	result := make([]NativeCommand, 0, len(seen))
	for _, command := range seen {
		result = append(result, command)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func cloneMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func clonePermission(value *PermissionRequest) *PermissionRequest {
	if value == nil {
		return nil
	}
	result := *value
	result.Options = append([]PermissionOption(nil), value.Options...)
	return &result
}

func runtimeKey(chatID string, agent Agent) string {
	return chatID + ":" + string(agent)
}

var _ RuntimeController = (*BackgroundRunner)(nil)
