package providers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/agent-sync/agent-sync/internal/db"
	"github.com/agent-sync/agent-sync/pkg/types"
	"github.com/google/uuid"

	_ "modernc.org/sqlite"
)

type CopilotProvider struct {
	path string
	db   *db.DB
}

type CopilotChatSession struct {
	ID        string             `json:"id"`
	Title     string             `json:"title,omitempty"`
	Model     string             `json:"modelSlug,omitempty"`
	Messages  []CopilotChatMsg   `json:"messages"`
	CreatedAt string             `json:"createdAt,omitempty"`
	UpdatedAt string             `json:"updatedAt,omitempty"`
}

type CopilotChatMsg struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	CreatedAt  string `json:"createdAt,omitempty"`
	CopilotID  string `json:"copilotId,omitempty"`
}

type CopilotAgentSession struct {
	ID         string     `json:"id,omitempty"`
	Title      string     `json:"title,omitempty"`
	Model      string     `json:"model,omitempty"`
	Messages   []CopilotChatMsg `json:"messages"`
	StartTime  string     `json:"startTime,omitempty"`
	EndTime    string     `json:"endTime,omitempty"`
}

func NewCopilotProvider(cfg Config, database *db.DB) (*CopilotProvider, error) {
	path := cfg.CopilotPath
	if path == "" {
		home, _ := os.UserHomeDir()
		switch runtime.GOOS {
		case "linux":
			path = filepath.Join(home, ".config", "Code", "User", "workspaceStorage")
		case "darwin":
			path = filepath.Join(home, "Library", "Application Support", "Code", "User", "workspaceStorage")
		case "windows":
			path = filepath.Join(os.Getenv("APPDATA"), "Code", "User", "workspaceStorage")
		default:
			path = filepath.Join(home, ".config", "Code", "User", "workspaceStorage")
		}
	}
	return &CopilotProvider{path: path, db: database}, nil
}

func (p *CopilotProvider) Type() types.ProviderType {
	return types.ProviderCopilot
}

func (p *CopilotProvider) Name() string {
	return "GitHub Copilot"
}

func (p *CopilotProvider) Detect() (bool, error) {
	chatDir := filepath.Join(p.chatSessionsDir())
	if info, err := os.Stat(chatDir); err == nil && info.IsDir() {
		return true, nil
	}
	if info, err := os.Stat(p.path); err == nil && info.IsDir() {
		return true, nil
	}
	return false, nil
}

func (p *CopilotProvider) chatSessionsDir() string {
	base := filepath.Dir(filepath.Dir(p.path))
	return filepath.Join(base, "globalStorage", "github.copilot-chat", "chatSessions")
}

func (p *CopilotProvider) agentSessionsDir() string {
	return filepath.Join(p.chatSessionsDir(), "agentSessions")
}

func (p *CopilotProvider) getProvider() (*types.Provider, error) {
	prov, err := p.db.GetProviderByType(string(p.Type()))
	if err != nil {
		prov = &types.Provider{
			ID:        db.NewID(),
			Name:      p.Name(),
			Type:      p.Type(),
			Path:      p.path,
			Enabled:   true,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := p.db.UpsertProvider(prov); err != nil {
			return nil, err
		}
		return prov, nil
	}
	return prov, nil
}

func (p *CopilotProvider) Sync() (*types.SyncStats, error) {
	prov, err := p.getProvider()
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}
	stats := &types.SyncStats{ProviderID: prov.ID}

	if err := p.scanChatSessions(prov, stats); err != nil {
		fmt.Printf("  ! chat sessions: %v\n", err)
	}
	if err := p.scanAgentSessions(prov, stats); err != nil {
		fmt.Printf("  ! agent sessions: %v\n", err)
	}
	if err := p.scanWorkspaceDBs(prov, stats); err != nil {
		fmt.Printf("  ! workspace dbs: %v\n", err)
	}

	now := time.Now()
	prov.LastSync = &now
	p.db.UpsertProvider(prov)
	return stats, nil
}

func (p *CopilotProvider) scanChatSessions(prov *types.Provider, stats *types.SyncStats) error {
	dir := p.chatSessionsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		fpath := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(fpath)
		if err != nil {
			continue
		}
		var session CopilotChatSession
		if err := json.Unmarshal(data, &session); err != nil {
			continue
		}
		p.importChatSession(&session, prov, stats)
	}
	return nil
}

func (p *CopilotProvider) scanAgentSessions(prov *types.Provider, stats *types.SyncStats) error {
	dir := p.agentSessionsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		fpath := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(fpath)
		if err != nil {
			continue
		}
		var session CopilotAgentSession
		if err := json.Unmarshal(data, &session); err != nil {
			continue
		}
		p.importAgentSession(&session, prov, stats)
	}
	return nil
}

func (p *CopilotProvider) scanWorkspaceDBs(prov *types.Provider, stats *types.SyncStats) error {
	entries, err := os.ReadDir(p.path)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dbfile := filepath.Join(p.path, e.Name(), "state.vscdb")
		if _, err := os.Stat(dbfile); err != nil {
			continue
		}
		if err := p.importWorkspaceDB(dbfile, prov, stats); err != nil {
			fmt.Printf("  ! workspace db %s: %v\n", e.Name(), err)
		}
	}
	return nil
}

func (p *CopilotProvider) importChatSession(s *CopilotChatSession, prov *types.Provider, stats *types.SyncStats) {
	id := s.ID
	if id == "" {
		id = uuid.New().String()
	}

	title := s.Title
	if title == "" {
		title = fmt.Sprintf("Copilot Chat %s", id[:8])
	}

	startedAt := parseCopilotTime(s.CreatedAt, time.Now())
	endedAt := parseCopilotTime(s.UpdatedAt, startedAt)

	model := s.Model
	if model == "" {
		model = "copilot"
	}

	msgCount := 0
	for _, m := range s.Messages {
		if m.Content != "" {
			msgCount++
		}
	}

	session := &types.Session{
		ID:           id,
		ProviderID:   prov.ID,
		Provider:     prov.Type,
		Title:        title,
		Model:        model,
		StartedAt:    startedAt,
		EndedAt:      &endedAt,
		MessageCount: msgCount,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := p.db.UpsertSession(session); err != nil {
		return
	}
	stats.SessionsFound++

	for _, msg := range s.Messages {
		if msg.Content == "" {
			continue
		}
		role := mapCopilotRole(msg.Role)
		createdAt := parseCopilotTime(msg.CreatedAt, startedAt)

		message := &types.Message{
			ID:        uuid.New().String(),
			SessionID: id,
			Role:      role,
			Content:   msg.Content,
			CreatedAt: createdAt,
		}
		if err := p.db.InsertMessage(message); err == nil {
			stats.MessagesNew++
		}
	}
}

func (p *CopilotProvider) importAgentSession(s *CopilotAgentSession, prov *types.Provider, stats *types.SyncStats) {
	id := s.ID
	if id == "" {
		id = uuid.New().String()
	}

	title := s.Title
	if title == "" {
		title = fmt.Sprintf("Copilot Agent %s", id[:8])
	}

	startedAt := parseCopilotTime(s.StartTime, time.Now())
	endedAt := parseCopilotTime(s.EndTime, startedAt)

	model := s.Model
	if model == "" {
		model = "copilot-agent"
	}

	session := &types.Session{
		ID:           id,
		ProviderID:   prov.ID,
		Provider:     prov.Type,
		Title:        title,
		Model:        model,
		StartedAt:    startedAt,
		EndedAt:      &endedAt,
		MessageCount: len(s.Messages),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := p.db.UpsertSession(session); err != nil {
		return
	}
	stats.SessionsFound++

	for _, msg := range s.Messages {
		if msg.Content == "" {
			continue
		}
		role := mapCopilotRole(msg.Role)
		message := &types.Message{
			ID:        uuid.New().String(),
			SessionID: id,
			Role:      role,
			Content:   msg.Content,
			CreatedAt: startedAt,
		}
		if err := p.db.InsertMessage(message); err == nil {
			stats.MessagesNew++
		}
	}
}

func (p *CopilotProvider) importWorkspaceDB(dbfile string, prov *types.Provider, stats *types.SyncStats) error {
	dsn := fmt.Sprintf("file:%s?mode=ro&cache=shared", dbfile)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer conn.Close()

	rows, err := conn.Query(`SELECT key, value FROM ItemTable WHERE key LIKE 'github.copilot.chat.%'`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		var rawSession map[string]interface{}
		if err := json.Unmarshal([]byte(value), &rawSession); err != nil {
			continue
		}
		p.importRawSession(rawSession, prov, stats)
	}
	return nil
}

func (p *CopilotProvider) importRawSession(raw map[string]interface{}, prov *types.Provider, stats *types.SyncStats) {
	id, _ := raw["id"].(string)
	if id == "" {
		id = uuid.New().String()
	}

	title, _ := raw["title"].(string)
	if title == "" {
		if t, ok := raw["conversationName"].(string); ok {
			title = t
		} else {
			title = fmt.Sprintf("Copilot %s", id[:8])
		}
	}

	messages, ok := raw["messages"].([]interface{})
	if !ok {
		return
	}

	session := &types.Session{
		ID:           id,
		ProviderID:   prov.ID,
		Provider:     prov.Type,
		Title:        title,
		Model:        "copilot",
		StartedAt:    time.Now(),
		MessageCount: len(messages),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := p.db.UpsertSession(session); err != nil {
		return
	}
	stats.SessionsFound++

	for _, m := range messages {
		obj, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := obj["role"].(string)
		content, _ := obj["content"].(string)
		if content == "" {
			content, _ = obj["text"].(string)
		}
		if content == "" {
			continue
		}

		message := &types.Message{
			ID:        uuid.New().String(),
			SessionID: id,
			Role:      mapCopilotRole(role),
			Content:   content,
			CreatedAt: time.Now(),
		}
		if err := p.db.InsertMessage(message); err == nil {
			stats.MessagesNew++
		}
	}
}

func parseCopilotTime(s string, fallback time.Time) time.Time {
	if s == "" {
		return fallback
	}
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z07:00",
		time.RFC3339Nano,
		"2006-01-02",
	}
	for _, f := range formats {
		t, err := time.Parse(f, s)
		if err == nil {
			return t
		}
	}
	return fallback
}

func mapCopilotRole(role string) string {
	switch strings.ToLower(role) {
	case "user", "human":
		return "user"
	case "assistant", "model", "bot", "ai":
		return "assistant"
	case "system":
		return "system"
	case "tool":
		return "tool"
	default:
		return role
	}
}

func (p *CopilotProvider) ListSessions() ([]*types.Session, error) {
	prov, err := p.getProvider()
	if err != nil {
		return nil, err
	}
	sessions, err := p.db.ListSessions(string(prov.Type), 100, 0)
	if err != nil {
		return nil, err
	}
	var result []*types.Session
	for i := range sessions {
		result = append(result, &sessions[i])
	}
	return result, nil
}

func (p *CopilotProvider) GetSessionMessages(sessionID string) ([]*types.Message, error) {
	messages, err := p.db.GetSessionMessages(sessionID)
	if err != nil {
		return nil, err
	}
	var result []*types.Message
	for i := range messages {
		result = append(result, &messages[i])
	}
	return result, nil
}
