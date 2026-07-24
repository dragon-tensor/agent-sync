package providers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agent-sync/agent-sync/internal/db"
	"github.com/agent-sync/agent-sync/pkg/types"
	"github.com/google/uuid"
)

type CodexProvider struct {
	path string
	db   *db.DB
}

type CodexSession struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Model     string          `json:"model"`
	Messages  []CodexMessage  `json:"messages"`
	CreatedAt string          `json:"created_at"`
	UpdatedAt string          `json:"updated_at"`
}

type CodexMessage struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

func NewCodexProvider(cfg Config, database *db.DB) (*CodexProvider, error) {
	path := cfg.CodexPath
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".codex")
	}
	return &CodexProvider{path: path, db: database}, nil
}

func (p *CodexProvider) Type() types.ProviderType {
	return types.ProviderCodex
}

func (p *CodexProvider) Name() string {
	return "Codex"
}

func (p *CodexProvider) Detect() (bool, error) {
	info, err := os.Stat(p.path)
	if err != nil {
		return false, nil
	}
	if !info.IsDir() {
		return false, nil
	}
	entries, err := os.ReadDir(p.path)
	if err != nil {
		return false, nil
	}
	for _, e := range entries {
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".jsonl") || strings.HasSuffix(e.Name(), ".json")) {
			if strings.HasPrefix(e.Name(), "history") || strings.Contains(e.Name(), "session") {
				return true, nil
			}
		}
	}
	return false, nil
}

func (p *CodexProvider) getProvider() (*types.Provider, error) {
	prov, err := p.db.GetProvider(string(p.Type()))
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

func (p *CodexProvider) Sync() (*types.SyncStats, error) {
	prov, err := p.getProvider()
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}
	stats := &types.SyncStats{ProviderID: prov.ID}

	if err := p.scanHistoryJSONL(prov, stats); err != nil {
		fmt.Printf("  ! history: %v\n", err)
	}
	if err := p.scanSessionFiles(prov, stats); err != nil {
		fmt.Printf("  ! sessions: %v\n", err)
	}

	now := time.Now()
	prov.LastSync = &now
	p.db.UpsertProvider(prov)
	return stats, nil
}

func (p *CodexProvider) scanHistoryJSONL(prov *types.Provider, stats *types.SyncStats) error {
	historyPath := filepath.Join(p.path, "history.jsonl")
	data, err := os.ReadFile(historyPath)
	if err != nil {
		return nil
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		p.importRawEntry(raw, prov, stats)
	}
	return nil
}

func (p *CodexProvider) scanSessionFiles(prov *types.Provider, stats *types.SyncStats) error {
	entries, err := os.ReadDir(p.path)
	if err != nil {
		return nil
	}

	for _, e := range entries {
		if e.IsDir() {
			sessionDir := filepath.Join(p.path, e.Name())
			subFiles, err := os.ReadDir(sessionDir)
			if err != nil {
				continue
			}
			for _, sf := range subFiles {
				if strings.HasSuffix(sf.Name(), ".json") || strings.HasSuffix(sf.Name(), ".jsonl") {
					fpath := filepath.Join(sessionDir, sf.Name())
					p.importSessionFile(fpath, prov, stats)
				}
			}
			continue
		}
		if strings.HasSuffix(e.Name(), ".json") && !strings.HasSuffix(e.Name(), ".jsonl") {
			fpath := filepath.Join(p.path, e.Name())
			p.importSessionFile(fpath, prov, stats)
		}
	}
	return nil
}

func (p *CodexProvider) importSessionFile(fpath string, prov *types.Provider, stats *types.SyncStats) {
	data, err := os.ReadFile(fpath)
	if err != nil {
		return
	}

	var session CodexSession
	if err := json.Unmarshal(data, &session); err != nil {
		var sessions []CodexSession
		if err2 := json.Unmarshal(data, &sessions); err2 != nil {
			return
		}
		for i := range sessions {
			p.importCodexSession(&sessions[i], prov, stats)
		}
		return
	}
	p.importCodexSession(&session, prov, stats)
}

func (p *CodexProvider) importCodexSession(s *CodexSession, prov *types.Provider, stats *types.SyncStats) {
	id := s.ID
	if id == "" {
		id = uuid.New().String()
	}

	title := s.Title
	if title == "" {
		title = fmt.Sprintf("Codex %s", id[:8])
	}

	startedAt := parseCodexTime(s.CreatedAt, time.Now())
	endedAt := parseCodexTime(s.UpdatedAt, startedAt)

	model := s.Model
	if model == "" {
		model = "codex"
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

	var prevID string
	for i, msg := range s.Messages {
		if msg.Content == "" {
			continue
		}
		role := mapCodexRole(msg.Role)
		createdAt := parseCodexTime(msg.CreatedAt, startedAt)

		msgID := msg.ID
		if msgID == "" {
			msgID = uuid.New().String()
		}

		var parentID *string
		if i > 0 && prevID != "" {
			parentID = &prevID
		}

		message := &types.Message{
			ID:        msgID,
			SessionID: id,
			ParentID:  parentID,
			Role:      role,
			Content:   msg.Content,
			CreatedAt: createdAt,
		}
		if err := p.db.InsertMessage(message); err == nil {
			stats.MessagesNew++
			prevID = msgID
		}
	}
}

func (p *CodexProvider) importRawEntry(raw map[string]interface{}, prov *types.Provider, stats *types.SyncStats) {
	id, _ := raw["id"].(string)
	if id == "" {
		if m, ok := raw["message_id"].(string); ok {
			id = m
		} else {
			id = uuid.New().String()
		}
	}

	sessionID := id

	role, _ := raw["role"].(string)
	if role == "" {
		role, _ = raw["sender"].(string)
	}

	content, _ := raw["content"].(string)
	if content == "" {
		content, _ = raw["text"].(string)
	}
	if content == "" {
		return
	}

	title, _ := raw["title"].(string)
	if title == "" {
		if c, ok := raw["conversation_name"].(string); ok {
			title = c
		} else {
			title = fmt.Sprintf("Codex %s", sessionID[:8])
		}
	}

	model, _ := raw["model"].(string)
	if model == "" {
		model = "codex"
	}

	existing, err := p.db.GetSession(sessionID)
	if err == nil && existing != nil {
		message := &types.Message{
			ID:        uuid.New().String(),
			SessionID: sessionID,
			Role:      mapCodexRole(role),
			Content:   content,
			CreatedAt: time.Now(),
		}
		if p.db.InsertMessage(message) == nil {
			stats.MessagesNew++
		}
		return
	}

	session := &types.Session{
		ID:           sessionID,
		ProviderID:   prov.ID,
		Provider:     prov.Type,
		Title:        title,
		Model:        model,
		StartedAt:    time.Now(),
		MessageCount: 1,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := p.db.UpsertSession(session); err != nil {
		return
	}
	stats.SessionsFound++

	message := &types.Message{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Role:      mapCodexRole(role),
		Content:   content,
		CreatedAt: time.Now(),
	}
	if p.db.InsertMessage(message) == nil {
		stats.MessagesNew++
	}
}

func parseCodexTime(s string, fallback time.Time) time.Time {
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

func mapCodexRole(role string) string {
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

func (p *CodexProvider) ListSessions() ([]*types.Session, error) {
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

func (p *CodexProvider) GetSessionMessages(sessionID string) ([]*types.Message, error) {
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
