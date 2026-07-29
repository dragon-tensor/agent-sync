package chat

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/agent-sync/agent-sync/internal/db"
)

type Store struct{ db *db.DB }

func NewStore(database *db.DB) *Store { return &Store{db: database} }

func (s *Store) Create(projectDir string, agent Agent) (*Chat, error) {
	chat := &Chat{ID: db.NewID(), Title: "New chat", ProjectDir: projectDir, ActiveAgent: agent, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	_, err := s.db.Exec(`INSERT INTO chats (id, title, project_dir, active_agent, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		chat.ID, chat.Title, chat.ProjectDir, chat.ActiveAgent, stamp(chat.CreatedAt), stamp(chat.UpdatedAt))
	if err != nil {
		return nil, fmt.Errorf("create chat: %w", err)
	}
	return chat, nil
}

func (s *Store) SetTitle(chatID, title string) error {
	_, err := s.db.Exec(`UPDATE chats SET title = ?, updated_at = ? WHERE id = ?`, title, stamp(time.Now()), chatID)
	return err
}

func (s *Store) Get(id string) (*Chat, error) {
	var chat Chat
	var created, updated string
	err := s.db.QueryRow(`SELECT id, title, project_dir, active_agent, created_at, updated_at FROM chats WHERE id = ?`, id).
		Scan(&chat.ID, &chat.Title, &chat.ProjectDir, &chat.ActiveAgent, &created, &updated)
	if err != nil {
		return nil, err
	}
	chat.CreatedAt = parseStamp(created)
	chat.UpdatedAt = parseStamp(updated)
	return &chat, nil
}

func (s *Store) List() ([]Chat, error) {
	rows, err := s.db.Query(`SELECT id, title, project_dir, active_agent, created_at, updated_at FROM chats ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Chat
	for rows.Next() {
		var chat Chat
		var created, updated string
		if err := rows.Scan(&chat.ID, &chat.Title, &chat.ProjectDir, &chat.ActiveAgent, &created, &updated); err != nil {
			return nil, err
		}
		chat.CreatedAt = parseStamp(created)
		chat.UpdatedAt = parseStamp(updated)
		result = append(result, chat)
	}
	if result == nil {
		result = []Chat{}
	}
	return result, rows.Err()
}

func (s *Store) SetActiveAgent(chatID string, agent Agent) error {
	_, err := s.db.Exec(`UPDATE chats SET active_agent = ?, updated_at = ? WHERE id = ?`, agent, stamp(time.Now()), chatID)
	return err
}

func (s *Store) GetOrCreateAgentSession(chatID string, agent Agent) (*AgentSession, error) {
	var session AgentSession
	var lastActive sql.NullString
	err := s.db.QueryRow(`SELECT id, chat_id, agent, native_session_id, last_delivered_sequence, last_active_at FROM chat_agent_sessions WHERE chat_id = ? AND agent = ?`, chatID, agent).
		Scan(&session.ID, &session.ChatID, &session.Agent, &session.NativeSessionID, &session.LastDeliveredSequence, &lastActive)
	if err == nil {
		if lastActive.Valid {
			t := parseStamp(lastActive.String)
			session.LastActiveAt = &t
		}
		return &session, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	session = AgentSession{ID: db.NewID(), ChatID: chatID, Agent: agent}
	_, err = s.db.Exec(`INSERT INTO chat_agent_sessions (id, chat_id, agent) VALUES (?, ?, ?)`, session.ID, chatID, agent)
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *Store) UpdateAgentSession(session *AgentSession) error {
	now := time.Now()
	session.LastActiveAt = &now
	_, err := s.db.Exec(`UPDATE chat_agent_sessions SET native_session_id = ?, last_delivered_sequence = ?, last_active_at = ?, updated_at = ? WHERE id = ?`,
		session.NativeSessionID, session.LastDeliveredSequence, stamp(now), stamp(now), session.ID)
	return err
}

func (s *Store) ListAgentSessions(chatID string) ([]AgentSession, error) {
	rows, err := s.db.Query(`SELECT id, chat_id, agent, native_session_id, last_delivered_sequence, last_active_at FROM chat_agent_sessions WHERE chat_id = ? ORDER BY last_active_at DESC`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []AgentSession
	for rows.Next() {
		var x AgentSession
		var active sql.NullString
		if err := rows.Scan(&x.ID, &x.ChatID, &x.Agent, &x.NativeSessionID, &x.LastDeliveredSequence, &active); err != nil {
			return nil, err
		}
		if active.Valid {
			t := parseStamp(active.String)
			x.LastActiveAt = &t
		}
		result = append(result, x)
	}
	if result == nil {
		result = []AgentSession{}
	}
	return result, rows.Err()
}

func (s *Store) SaveMetrics(metrics AgentMetrics) error {
	metrics.UpdatedAt = time.Now()
	_, err := s.db.Exec(`INSERT INTO chat_agent_metrics (chat_id, agent, model, effort, input_tokens, output_tokens, context_window, cost_usd, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(chat_id, agent) DO UPDATE SET model=excluded.model, effort=excluded.effort, input_tokens=excluded.input_tokens, output_tokens=excluded.output_tokens, context_window=excluded.context_window, cost_usd=excluded.cost_usd, updated_at=excluded.updated_at`,
		metrics.ChatID, metrics.Agent, metrics.Model, metrics.Effort, metrics.InputTokens, metrics.OutputTokens, metrics.ContextWindow, metrics.CostUSD, stamp(metrics.UpdatedAt))
	return err
}

func (s *Store) Metrics(chatID string) ([]AgentMetrics, error) {
	rows, err := s.db.Query(`SELECT chat_id, agent, model, effort, input_tokens, output_tokens, context_window, cost_usd, updated_at FROM chat_agent_metrics WHERE chat_id = ?`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []AgentMetrics
	for rows.Next() {
		var metrics AgentMetrics
		var updated string
		if err := rows.Scan(&metrics.ChatID, &metrics.Agent, &metrics.Model, &metrics.Effort, &metrics.InputTokens, &metrics.OutputTokens, &metrics.ContextWindow, &metrics.CostUSD, &updated); err != nil {
			return nil, err
		}
		metrics.UpdatedAt = parseStamp(updated)
		result = append(result, metrics)
	}
	if result == nil {
		result = []AgentMetrics{}
	}
	return result, rows.Err()
}

func (s *Store) AddMessage(chatID, role, content string, agent Agent) (*Message, error) {
	var sequence int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(sequence), 0) + 1 FROM chat_messages WHERE chat_id = ?`, chatID).Scan(&sequence); err != nil {
		return nil, err
	}
	m := &Message{ID: db.NewID(), ChatID: chatID, Sequence: sequence, Role: role, Content: content, Agent: agent, CreatedAt: time.Now()}
	_, err := s.db.Exec(`INSERT INTO chat_messages (id, chat_id, sequence, role, content, agent, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, m.ID, m.ChatID, m.Sequence, m.Role, m.Content, m.Agent, stamp(m.CreatedAt))
	if err != nil {
		return nil, err
	}
	if role == "user" {
		_, _ = s.db.Exec(`UPDATE chats SET title = CASE WHEN title = 'New chat' THEN substr(?, 1, 60) ELSE title END, updated_at = ? WHERE id = ?`, content, stamp(m.CreatedAt), chatID)
	}
	return m, nil
}

func (s *Store) MessagesAfter(chatID string, sequence int) ([]Message, error) {
	return s.messagesAfter(chatID, sequence, false)
}

func (s *Store) TransferMessagesAfter(chatID string, sequence int) ([]Message, error) {
	return s.messagesAfter(chatID, sequence, true)
}

func (s *Store) messagesAfter(chatID string, sequence int, excludeSystem bool) ([]Message, error) {
	query := `SELECT id, chat_id, sequence, role, content, agent, created_at FROM chat_messages WHERE chat_id = ? AND sequence > ?`
	if excludeSystem {
		query += ` AND role != 'system'`
	}
	query += ` ORDER BY sequence`
	rows, err := s.db.Query(query, chatID, sequence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Message
	for rows.Next() {
		var m Message
		var created string
		if err := rows.Scan(&m.ID, &m.ChatID, &m.Sequence, &m.Role, &m.Content, &m.Agent, &created); err != nil {
			return nil, err
		}
		m.CreatedAt = parseStamp(created)
		result = append(result, m)
	}
	if result == nil {
		result = []Message{}
	}
	return result, rows.Err()
}

func (s *Store) Messages(chatID string) ([]Message, error) { return s.MessagesAfter(chatID, 0) }

func (s *Store) RecordHandoff(chatID string, handoff Handoff) error {
	if handoff.To < handoff.From {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO chat_handoffs (id, chat_id, target_agent, from_sequence, to_sequence) VALUES (?, ?, ?, ?, ?)`, db.NewID(), chatID, handoff.Target, handoff.From, handoff.To)
	return err
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func parseStamp(value string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, value)
	if t.IsZero() {
		t, _ = time.Parse("2006-01-02 15:04:05", value)
	}
	return t
}
