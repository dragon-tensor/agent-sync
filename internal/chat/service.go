package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/agent-sync/agent-sync/internal/db"
	providersync "github.com/agent-sync/agent-sync/internal/sync"
	"github.com/agent-sync/agent-sync/internal/sync/providers"
	"github.com/agent-sync/agent-sync/pkg/types"
)

type Service struct {
	store    *Store
	database *db.DB
	runner   Runner
	runtime  RuntimeController
}

func NewService(database *db.DB, runner Runner) *Service {
	if runner == nil {
		runner = NewBackgroundRunner(CommandRunner{})
	}
	store := NewStore(database)
	_ = store.RecoverQueue()
	service := &Service{store: store, database: database, runner: runner}
	service.runtime, _ = runner.(RuntimeController)
	return service
}

func (s *Service) Start(projectDir string, agent Agent) (*Chat, error) {
	if !isKnownAgent(agent) {
		return nil, fmt.Errorf("unknown agent %q", agent)
	}
	conversation, err := s.store.Create(projectDir, agent)
	if err != nil {
		return nil, err
	}
	s.ensureRuntime(conversation)
	return conversation, nil
}

func (s *Service) Resume(chatID string) (*Chat, error) {
	conversation, err := s.store.Get(chatID)
	if err == nil {
		s.ensureRuntime(conversation)
	}
	return conversation, err
}
func (s *Service) ListChats() ([]Chat, error)                { return s.store.List() }
func (s *Service) Messages(chatID string) ([]Message, error) { return s.store.Messages(chatID) }
func (s *Service) AgentSessions(chatID string) ([]AgentSession, error) {
	return s.store.ListAgentSessions(chatID)
}
func (s *Service) AgentMetrics(chatID string) ([]AgentMetrics, error) { return s.store.Metrics(chatID) }
func (s *Service) Queue(chatID string) ([]QueueItem, error)           { return s.store.Queue(chatID, true) }

func (s *Service) Close() error {
	if s.runtime != nil {
		return s.runtime.Close()
	}
	return nil
}

func (s *Service) Switch(chatID string, agent Agent) (*Chat, error) {
	if !isKnownAgent(agent) {
		return nil, fmt.Errorf("unknown agent %q", agent)
	}
	conversation, err := s.store.Get(chatID)
	if err != nil {
		return nil, err
	}
	if err := s.store.SetActiveAgent(chatID, agent); err != nil {
		return nil, err
	}
	if conversation.ActiveAgent != agent {
		if _, err := s.store.AddMessage(chatID, "system", agentBanner(agent), agent); err != nil {
			return nil, err
		}
	}
	updated, err := s.store.Get(chatID)
	if err == nil {
		s.ensureRuntime(updated)
	}
	return updated, err
}

// ScanImportableSessions refreshes local copies of histories. It does not
// modify the original agent sessions.
func (s *Service) ScanImportableSessions() ([]types.Session, error) {
	registry := providersync.NewRegistry(s.database)
	registry.InitDefaultProviders(providers.Config{})
	for _, provider := range registry.DetectAll() {
		_, _ = provider.Sync()
	}
	return s.database.ListSessions("", 200, 0)
}

func (s *Service) ImportLegacySession(sessionID string, fallback Agent) (*Chat, error) {
	source, err := s.database.GetSession(sessionID)
	if err != nil {
		return nil, err
	}
	messages, err := s.database.GetSessionMessages(sessionID)
	if err != nil {
		return nil, err
	}
	agent := importedAgent(source.Provider, fallback)
	if !isAvailable(agent) {
		agent = fallback
	}
	conversation, err := s.store.Create(source.ProjectDir, agent)
	if err != nil {
		return nil, err
	}
	if source.Title != "" {
		if err := s.store.SetTitle(conversation.ID, source.Title); err != nil {
			return nil, err
		}
	}
	for _, message := range messages {
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		if _, err := s.store.AddMessage(conversation.ID, message.Role, message.Content, conversation.ActiveAgent); err != nil {
			return nil, err
		}
	}
	return s.store.Get(conversation.ID)
}

func (s *Service) Send(ctx context.Context, chatID, input string) (*Message, error) {
	item, _, err := s.Enqueue(chatID, input)
	if err != nil {
		return nil, err
	}
	return s.RunQueueItem(ctx, item.ID)
}

func (s *Service) Enqueue(chatID, input string) (*QueueItem, *Message, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil, fmt.Errorf("message is empty")
	}
	chat, err := s.store.Get(chatID)
	if err != nil {
		return nil, nil, err
	}
	return s.store.Enqueue(chat.ID, chat.ActiveAgent, input)
}

func (s *Service) RunQueueItem(ctx context.Context, queueID string) (*Message, error) {
	item, err := s.store.QueueItem(queueID)
	if err != nil {
		return nil, err
	}
	if item.Status != QueueQueued {
		return nil, fmt.Errorf("queue item is %s", item.Status)
	}
	if err := s.store.BeginQueueItem(item.ID); err != nil {
		return nil, err
	}
	session, err := s.store.GetOrCreateAgentSession(item.ChatID, item.Agent)
	if err != nil {
		_ = s.store.FinishQueueItem(item.ID, QueueFailed, err.Error())
		return nil, err
	}

	// The queued user message already exists in the canonical ledger. Transfer
	// only earlier unseen work, then present this item's content as the active
	// request. Later queued messages are deliberately excluded.
	transfer, err := s.store.TransferMessagesBefore(item.ChatID, session.LastDeliveredSequence, item.UserSequence)
	if err != nil {
		_ = s.store.FinishQueueItem(item.ID, QueueFailed, err.Error())
		return nil, err
	}
	chat, err := s.store.Get(item.ChatID)
	if err != nil {
		_ = s.store.FinishQueueItem(item.ID, QueueFailed, err.Error())
		return nil, err
	}
	prompt := buildPrompt(item.Agent, transfer, item.Content)
	result, err := s.runner.Run(ctx, RunRequest{ChatID: item.ChatID, Agent: item.Agent, ProjectDir: chat.ProjectDir, Prompt: prompt, NativeSessionID: session.NativeSessionID})
	if err != nil {
		status := QueueFailed
		if ctx.Err() != nil {
			status = QueueCancelled
		}
		_ = s.store.FinishQueueItem(item.ID, status, err.Error())
		s.persistRuntime(item.ChatID, item.Agent, session)
		return nil, err
	}
	if strings.TrimSpace(result.Reply) == "" {
		err := fmt.Errorf("%s returned an empty response", item.Agent)
		_ = s.store.FinishQueueItem(item.ID, QueueFailed, err.Error())
		return nil, err
	}
	reply, err := s.store.AddMessage(item.ChatID, "assistant", strings.TrimSpace(result.Reply), item.Agent)
	if err != nil {
		_ = s.store.FinishQueueItem(item.ID, QueueFailed, err.Error())
		return nil, err
	}
	if len(transfer) > 0 {
		_ = s.store.RecordHandoff(item.ChatID, Handoff{Target: item.Agent, From: transfer[0].Sequence, To: transfer[len(transfer)-1].Sequence})
	}
	if result.NativeSessionID != "" {
		session.NativeSessionID = result.NativeSessionID
	}
	result.Metrics.ChatID, result.Metrics.Agent = item.ChatID, item.Agent
	if err := s.store.SaveMetrics(result.Metrics); err != nil {
		_ = s.store.FinishQueueItem(item.ID, QueueFailed, err.Error())
		return nil, err
	}
	session.LastDeliveredSequence = reply.Sequence
	state := s.runtimeState(item.ChatID, item.Agent)
	s.copyRuntimeState(session, state)
	if err := s.store.SaveRuntimeState(session, state); err != nil {
		_ = s.store.FinishQueueItem(item.ID, QueueFailed, err.Error())
		return nil, err
	}
	if err := s.store.FinishQueueItem(item.ID, QueueCompleted, ""); err != nil {
		return nil, err
	}
	return reply, nil
}

func (s *Service) RunNativeCommand(ctx context.Context, chatID, command string) (NativeCommandResult, error) {
	if s.runtime == nil {
		return NativeCommandResult{}, fmt.Errorf("the configured runner does not expose native commands")
	}
	command = strings.TrimSpace(command)
	if command == "" || !strings.HasPrefix(command, "/") {
		return NativeCommandResult{}, fmt.Errorf("native command must start with /")
	}
	conversation, err := s.store.Get(chatID)
	if err != nil {
		return NativeCommandResult{}, err
	}
	session, err := s.store.GetOrCreateAgentSession(chatID, conversation.ActiveAgent)
	if err != nil {
		return NativeCommandResult{}, err
	}
	user, err := s.store.AddMessage(chatID, "user", command, conversation.ActiveAgent)
	if err != nil {
		return NativeCommandResult{}, err
	}
	result, control, err := s.runtime.RunNative(ctx, RunRequest{
		ChatID:          chatID,
		Agent:           conversation.ActiveAgent,
		ProjectDir:      conversation.ProjectDir,
		Prompt:          command,
		NativeSessionID: session.NativeSessionID,
	})
	if err != nil {
		s.persistRuntime(chatID, conversation.ActiveAgent, session)
		return NativeCommandResult{}, err
	}
	if result.NativeSessionID != "" {
		session.NativeSessionID = result.NativeSessionID
	}
	var reply *Message
	if strings.TrimSpace(result.Reply) != "" {
		reply, err = s.store.AddMessage(chatID, "assistant", strings.TrimSpace(result.Reply), conversation.ActiveAgent)
		if err != nil {
			return NativeCommandResult{}, err
		}
		session.LastDeliveredSequence = reply.Sequence
	} else {
		session.LastDeliveredSequence = user.Sequence
	}
	result.Metrics.ChatID, result.Metrics.Agent = chatID, conversation.ActiveAgent
	if result.Metrics.Model != "" || result.Metrics.InputTokens != 0 || result.Metrics.OutputTokens != 0 {
		_ = s.store.SaveMetrics(result.Metrics)
	}
	state := s.runtimeState(chatID, conversation.ActiveAgent)
	s.copyRuntimeState(session, state)
	if err := s.store.SaveRuntimeState(session, state); err != nil {
		return NativeCommandResult{}, err
	}
	return NativeCommandResult{Reply: reply, AgentControl: control}, nil
}

func (s *Service) NativeCommands(chatID string, agent Agent) []NativeCommand {
	if s.runtime != nil {
		return s.runtime.NativeCommands(chatID, agent)
	}
	return nil
}

func (s *Service) RuntimeStates(chatID string) ([]RuntimeState, error) {
	conversation, err := s.store.Get(chatID)
	if err != nil {
		return nil, err
	}
	sessions, err := s.store.ListAgentSessions(chatID)
	if err != nil {
		return nil, err
	}
	seen := map[Agent]bool{}
	var states []RuntimeState
	for _, session := range sessions {
		state := s.runtimeState(chatID, session.Agent)
		if state.Status == "" || (state.Status == RuntimeStopped && state.NativeSessionID == "") {
			persisted := persistedRuntimeState(session)
			persisted.Commands = s.NativeCommands(chatID, session.Agent)
			state = persisted
		}
		states = append(states, state)
		seen[session.Agent] = true
	}
	if !seen[conversation.ActiveAgent] {
		states = append(states, s.runtimeState(chatID, conversation.ActiveAgent))
	}
	return states, nil
}

func (s *Service) SendRuntimeKey(chatID string, agent Agent, value []byte) error {
	if s.runtime == nil {
		return fmt.Errorf("native agent control is unavailable")
	}
	return s.runtime.SendKey(chatID, agent, value)
}

func (s *Service) ResizeRuntime(chatID string, agent Agent, cols, rows int) error {
	if s.runtime == nil {
		return nil
	}
	return s.runtime.Resize(chatID, agent, cols, rows)
}

func (s *Service) LeaveAgentControl(chatID string, agent Agent) {
	if s.runtime == nil {
		return
	}
	s.runtime.LeaveControl(chatID, agent)
	session, err := s.store.GetOrCreateAgentSession(chatID, agent)
	if err == nil {
		s.persistRuntime(chatID, agent, session)
	}
}

func (s *Service) ResolvePermission(chatID string, agent Agent, requestID, optionID string) error {
	if s.runtime == nil {
		return fmt.Errorf("agent permissions are unavailable")
	}
	return s.runtime.ResolvePermission(chatID, agent, requestID, optionID)
}

func (s *Service) CancelRuntime(chatID string, agent Agent) error {
	if s.runtime == nil {
		return nil
	}
	return s.runtime.Cancel(chatID, agent)
}

func (s *Service) runtimeState(chatID string, agent Agent) RuntimeState {
	if s.runtime == nil {
		return RuntimeState{ChatID: chatID, Agent: agent, Status: RuntimeStopped}
	}
	return s.runtime.State(chatID, agent)
}

func (s *Service) ensureRuntime(conversation *Chat) {
	if s.runtime == nil || conversation == nil {
		return
	}
	session, err := s.store.GetOrCreateAgentSession(conversation.ID, conversation.ActiveAgent)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	state, err := s.runtime.Ensure(ctx, RunRequest{
		ChatID:          conversation.ID,
		Agent:           conversation.ActiveAgent,
		ProjectDir:      conversation.ProjectDir,
		NativeSessionID: session.NativeSessionID,
	})
	if err != nil {
		state = RuntimeState{
			ChatID:          conversation.ID,
			Agent:           conversation.ActiveAgent,
			Kind:            state.Kind,
			Status:          RuntimeCrashed,
			NativeSessionID: session.NativeSessionID,
			LastError:       err.Error(),
		}
	}
	s.copyRuntimeState(session, state)
	_ = s.store.SaveRuntimeState(session, state)
}

func (s *Service) persistRuntime(chatID string, agent Agent, session *AgentSession) {
	state := s.runtimeState(chatID, agent)
	s.copyRuntimeState(session, state)
	_ = s.store.SaveRuntimeState(session, state)
}

func (s *Service) copyRuntimeState(session *AgentSession, state RuntimeState) {
	if state.NativeSessionID != "" {
		session.NativeSessionID = state.NativeSessionID
	}
	session.RuntimeKind = state.Kind
	session.Status = state.Status
	session.LastError = state.LastError
}

func persistedRuntimeState(session AgentSession) RuntimeState {
	state := RuntimeState{
		ChatID:          session.ChatID,
		Agent:           session.Agent,
		Kind:            session.RuntimeKind,
		Status:          session.Status,
		NativeSessionID: session.NativeSessionID,
		LastError:       session.LastError,
	}
	_ = json.Unmarshal([]byte(session.CommandsJSON), &state.Commands)
	_ = json.Unmarshal([]byte(session.CapabilitiesJSON), &state.Capabilities)
	_ = json.Unmarshal([]byte(session.ConfigJSON), &state.Config)
	if state.Capabilities == nil {
		state.Capabilities = map[string]any{}
	}
	if state.Config == nil {
		state.Config = map[string]any{}
	}
	return state
}

func buildPrompt(target Agent, transfer []Message, input string) string {
	if len(transfer) == 0 {
		return input
	}
	var b strings.Builder
	b.WriteString("You are continuing a Dragon Sync workspace. The text below is work completed while your ")
	b.WriteString(string(target))
	b.WriteString(" session was inactive. Treat it as context, inspect the current project directory when relevant, and do not repeat it unless asked.\n\n## Transferred conversation\n")
	for _, message := range transfer {
		label := "User"
		if message.Role == "assistant" {
			label = strings.ToUpper(string(message.Agent))
		}
		fmt.Fprintf(&b, "[%s]\n%s\n\n", label, message.Content)
	}
	b.WriteString("## Current user message\n")
	b.WriteString(input)
	return b.String()
}

func isKnownAgent(agent Agent) bool {
	return agent == AgentClaude || agent == AgentCodex || agent == AgentOpenCode || agent == AgentGemini
}

func importedAgent(provider types.ProviderType, fallback Agent) Agent {
	switch provider {
	case types.ProviderClaudeCode:
		return AgentClaude
	case types.ProviderCodex:
		return AgentCodex
	case types.ProviderOpenCode:
		return AgentOpenCode
	case types.ProviderGemini:
		return AgentGemini
	default:
		return fallback
	}
}

func agentBanner(agent Agent) string {
	name := strings.ToUpper(string(agent))
	line := strings.Repeat("=", len(name)+16)
	return "+" + line + "+\n|      ACTIVE AGENT: " + name + "      |\n+" + line + "+"
}

func isAvailable(agent Agent) bool {
	for _, candidate := range AvailableAgents() {
		if candidate == agent {
			return true
		}
	}
	return false
}
