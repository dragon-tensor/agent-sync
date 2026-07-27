package providers

import (
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

type ClaudeCodeProvider struct {
	path string
	db   *db.DB
	cfg  Config
}

type ClaudeCodeSession struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Model     string            `json:"model"`
	Messages  []ClaudeCodeMessage `json:"messages"`
	CreatedAt string            `json:"created_at"`
	UpdatedAt string            `json:"updated_at"`
	Metadata  map[string]interface{} `json:"metadata"`
}

type ClaudeCodeMessage struct {
	Role       string                   `json:"role"`
	Content    []ClaudeCodeContentBlock `json:"content"`
	ToolUseID  string                   `json:"tool_use_id,omitempty"`
	Name       string                   `json:"name,omitempty"`
	Input      interface{}              `json:"input,omitempty"`
	ToolResult interface{}              `json:"tool_result,omitempty"`
	IsError    bool                     `json:"is_error,omitempty"`
	CreatedAt  string                   `json:"created_at,omitempty"`
}

type ClaudeCodeContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"`
	ID   string `json:"id,omitempty"`
}

func NewClaudeCodeProvider(cfg Config, database *db.DB) (*ClaudeCodeProvider, error) {
	path := cfg.ClaudeCodePath
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".claude", "projects")
	}
	return &ClaudeCodeProvider{path: path, db: database, cfg: cfg}, nil
}

func (p *ClaudeCodeProvider) Type() types.ProviderType {
	return types.ProviderClaudeCode
}

func (p *ClaudeCodeProvider) Name() string {
	return "Claude Code"
}

func (p *ClaudeCodeProvider) Detect() (bool, error) {
	info, err := os.Stat(p.path)
	if err != nil {
		return false, nil
	}
	return info.IsDir(), nil
}

func (p *ClaudeCodeProvider) getProvider() (*types.Provider, error) {
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

func (p *ClaudeCodeProvider) Sync() (*types.SyncStats, error) {
	prov, err := p.getProvider()
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}

	stats := &types.SyncStats{ProviderID: prov.ID}

	entries, err := os.ReadDir(p.path)
	if err != nil {
		return stats, nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		projectDir := filepath.Join(p.path, entry.Name())
		sessionsDir := filepath.Join(projectDir, "sessions")
		sessInfo, err := os.Stat(sessionsDir)
		if err != nil || !sessInfo.IsDir() {
			continue
		}

		sessionFiles, err := os.ReadDir(sessionsDir)
		if err != nil {
			continue
		}

		for _, sf := range sessionFiles {
			if !strings.HasSuffix(sf.Name(), ".json") {
				continue
			}
			sessionPath := filepath.Join(sessionsDir, sf.Name())
			if err := p.importSessionFile(sessionPath, prov, projectDir, stats); err != nil {
				fmt.Printf("  ! error importing %s: %v\n", sf.Name(), err)
			}
		}
	}

	now := time.Now()
	prov.LastSync = &now
	p.db.UpsertProvider(prov)
	return stats, nil
}

func (p *ClaudeCodeProvider) importSessionFile(path string, prov *types.Provider, projectDir string, stats *types.SyncStats) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var cs ClaudeCodeSession
	if err := json.Unmarshal(data, &cs); err != nil {
		raw := make(map[string]interface{})
		if err2 := json.Unmarshal(data, &raw); err2 != nil {
			return fmt.Errorf("unknown format: %w", err)
		}
		return p.importRawSession(raw, prov, projectDir, stats)
	}

	startedAt := time.Now()
	if cs.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, cs.CreatedAt); err == nil {
			startedAt = t
		}
	}

	endedAt := startedAt
	if cs.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, cs.UpdatedAt); err == nil {
			endedAt = t
		}
	}

	model := cs.Model
	if model == "" {
		if m, ok := cs.Metadata["model"].(string); ok {
			model = m
		}
	}

	workspace := filepath.Base(projectDir)

	title := cs.Name
	if title == "" {
		title = fmt.Sprintf("Session %s", cs.ID[:8])
	}

	session := &types.Session{
		ID:           cs.ID,
		ProviderID:   prov.ID,
		Provider:     prov.Type,
		Title:        title,
		Model:        model,
		Workspace:    workspace,
		ProjectDir:   projectDir,
		StartedAt:    startedAt,
		EndedAt:      &endedAt,
		MessageCount: len(cs.Messages),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := p.db.UpsertSession(session); err != nil {
		return fmt.Errorf("upsert session: %w", err)
	}
	stats.SessionsFound++

	for _, msg := range cs.Messages {
		content := extractContent(msg.Content)
		msgID := uuid.New().String()

		message := &types.Message{
			ID:        msgID,
			SessionID: session.ID,
			Role:      msg.Role,
			Content:   content,
			CreatedAt: time.Now(),
		}

		if msg.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339, msg.CreatedAt); err == nil {
				message.CreatedAt = t
			}
		}

		if err := p.db.InsertMessage(message); err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
		stats.MessagesNew++
	}

	return nil
}

func (p *ClaudeCodeProvider) importRawSession(raw map[string]interface{}, prov *types.Provider, projectDir string, stats *types.SyncStats) error {
	id, _ := raw["id"].(string)
	if id == "" {
		id = db.NewID()
	}

	session := &types.Session{
		ID:           id,
		ProviderID:   prov.ID,
		Provider:     prov.Type,
		Title:        fmt.Sprintf("Session %s", id[:8]),
		Workspace:    filepath.Base(projectDir),
		ProjectDir:   projectDir,
		StartedAt:    time.Now(),
		MessageCount: 0,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := p.db.UpsertSession(session); err != nil {
		return err
	}
	stats.SessionsFound++
	return nil
}

func (p *ClaudeCodeProvider) ListSessions() ([]*types.Session, error) {
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

func (p *ClaudeCodeProvider) GetSessionMessages(sessionID string) ([]*types.Message, error) {
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

func extractContent(blocks []ClaudeCodeContentBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}
