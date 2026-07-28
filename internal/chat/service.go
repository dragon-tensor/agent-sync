package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/agent-sync/agent-sync/internal/db"
	providersync "github.com/agent-sync/agent-sync/internal/sync"
	"github.com/agent-sync/agent-sync/internal/sync/providers"
	"github.com/agent-sync/agent-sync/pkg/types"
)

type Service struct {
	store    *Store
	database *db.DB
	runner   Runner
}

func NewService(database *db.DB, runner Runner) *Service {
	if runner == nil {
		runner = CommandRunner{}
	}
	return &Service{store: NewStore(database), database: database, runner: runner}
}

func (s *Service) Start(projectDir string, agent Agent) (*Chat, error) {
	if !isKnownAgent(agent) {
		return nil, fmt.Errorf("unknown agent %q", agent)
	}
	return s.store.Create(projectDir, agent)
}

func (s *Service) Resume(chatID string) (*Chat, error)       { return s.store.Get(chatID) }
func (s *Service) ListChats() ([]Chat, error)                { return s.store.List() }
func (s *Service) Messages(chatID string) ([]Message, error) { return s.store.Messages(chatID) }
func (s *Service) AgentSessions(chatID string) ([]AgentSession, error) {
	return s.store.ListAgentSessions(chatID)
}

func (s *Service) Switch(chatID string, agent Agent) (*Chat, error) {
	if !isKnownAgent(agent) {
		return nil, fmt.Errorf("unknown agent %q", agent)
	}
	if _, err := s.store.Get(chatID); err != nil {
		return nil, err
	}
	if err := s.store.SetActiveAgent(chatID, agent); err != nil {
		return nil, err
	}
	return s.store.Get(chatID)
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
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("message is empty")
	}
	chat, err := s.store.Get(chatID)
	if err != nil {
		return nil, err
	}
	session, err := s.store.GetOrCreateAgentSession(chat.ID, chat.ActiveAgent)
	if err != nil {
		return nil, err
	}

	// Read the target session's unseen messages before recording the new user
	// message.  This gives a resumed agent exactly the work done elsewhere,
	// then presents the current user instruction as the active request.
	transfer, err := s.store.MessagesAfter(chat.ID, session.LastDeliveredSequence)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.AddMessage(chat.ID, "user", input, chat.ActiveAgent); err != nil {
		return nil, err
	}

	prompt := buildPrompt(chat.ActiveAgent, transfer, input)
	result, err := s.runner.Run(ctx, RunRequest{Agent: chat.ActiveAgent, ProjectDir: chat.ProjectDir, Prompt: prompt, NativeSessionID: session.NativeSessionID})
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(result.Reply) == "" {
		return nil, fmt.Errorf("%s returned an empty response", chat.ActiveAgent)
	}
	reply, err := s.store.AddMessage(chat.ID, "assistant", strings.TrimSpace(result.Reply), chat.ActiveAgent)
	if err != nil {
		return nil, err
	}
	if len(transfer) > 0 {
		_ = s.store.RecordHandoff(chat.ID, Handoff{Target: chat.ActiveAgent, From: transfer[0].Sequence, To: transfer[len(transfer)-1].Sequence})
	}
	if result.NativeSessionID != "" {
		session.NativeSessionID = result.NativeSessionID
	}
	session.LastDeliveredSequence = reply.Sequence
	if err := s.store.UpdateAgentSession(session); err != nil {
		return nil, err
	}
	return reply, nil
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

func isAvailable(agent Agent) bool {
	for _, candidate := range AvailableAgents() {
		if candidate == agent {
			return true
		}
	}
	return false
}
