package providers

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agent-sync/agent-sync/internal/db"
	"github.com/agent-sync/agent-sync/pkg/types"
	"github.com/google/uuid"
)

type ClaudeWebProvider struct {
	path string
	db   *db.DB
	cfg  Config
}

type ClaudeWebConversation struct {
	UUID         string              `json:"uuid"`
	Name         string              `json:"name"`
	ModelSlug    string              `json:"model_slug"`
	ChatMessages []ClaudeWebMessage  `json:"chat_messages"`
	CreatedAt    string              `json:"created_at"`
	UpdatedAt    string              `json:"updated_at"`
	ProjectID    string              `json:"project_id,omitempty"`
	WorkspaceID  string              `json:"workspace_id,omitempty"`
}

type ClaudeWebMessage struct {
	UUID      string          `json:"uuid"`
	Sender    string          `json:"sender"`
	Text      string          `json:"text,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	CreatedAt string          `json:"created_at"`
	Files     []interface{}   `json:"files,omitempty"`
}

func NewClaudeWebProvider(cfg Config, database *db.DB) (*ClaudeWebProvider, error) {
	path := cfg.ClaudeWebExportPath
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".agent-sync", "imports", "claude-web")
	}
	return &ClaudeWebProvider{path: path, db: database, cfg: cfg}, nil
}

func (p *ClaudeWebProvider) Type() types.ProviderType {
	return types.ProviderClaudeWeb
}

func (p *ClaudeWebProvider) Name() string {
	return "Claude Web"
}

func (p *ClaudeWebProvider) Detect() (bool, error) {
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
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".zip")) {
			return true, nil
		}
	}
	return false, nil
}

func (p *ClaudeWebProvider) getProvider() (*types.Provider, error) {
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

func (p *ClaudeWebProvider) Sync() (*types.SyncStats, error) {
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
		if entry.IsDir() {
			continue
		}
		fpath := filepath.Join(p.path, entry.Name())
		if strings.HasSuffix(entry.Name(), ".zip") {
			if err := p.importZIP(fpath, prov, stats); err != nil {
				fmt.Printf("  ! error importing %s: %v\n", entry.Name(), err)
			}
		} else if strings.HasSuffix(entry.Name(), ".json") {
			if err := p.importJSONFile(fpath, prov, stats); err != nil {
				fmt.Printf("  ! error importing %s: %v\n", entry.Name(), err)
			}
		}
	}

	now := time.Now()
	prov.LastSync = &now
	p.db.UpsertProvider(prov)
	return stats, nil
}

func (p *ClaudeWebProvider) importZIP(zipPath string, prov *types.Provider, stats *types.SyncStats) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if !strings.HasSuffix(f.Name, ".json") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			continue
		}
		return p.parseConversationsJSON(data, prov, stats)
	}
	return fmt.Errorf("no JSON files found in zip")
}

func (p *ClaudeWebProvider) importJSONFile(path string, prov *types.Provider, stats *types.SyncStats) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return p.parseConversationsJSON(data, prov, stats)
}

func (p *ClaudeWebProvider) parseConversationsJSON(data []byte, prov *types.Provider, stats *types.SyncStats) error {
	var convs []ClaudeWebConversation
	if err := json.Unmarshal(data, &convs); err != nil {
		wrapped := struct {
			Conversations []ClaudeWebConversation `json:"conversations"`
			Accounts     []struct {
				Conversations []ClaudeWebConversation `json:"conversations"`
			} `json:"accounts"`
		}{}
		if err2 := json.Unmarshal(data, &wrapped); err2 != nil {
			return fmt.Errorf("parse claude web export: %w", err)
		}
		if wrapped.Conversations != nil {
			convs = wrapped.Conversations
		} else if len(wrapped.Accounts) > 0 {
			for _, a := range wrapped.Accounts {
				convs = append(convs, a.Conversations...)
			}
		}
	}

	for _, conv := range convs {
		if err := p.importConversation(&conv, prov, stats); err != nil {
			fmt.Printf("  ! conversation %s: %v\n", conv.UUID, err)
		}
	}
	return nil
}

func (p *ClaudeWebProvider) importConversation(conv *ClaudeWebConversation, prov *types.Provider, stats *types.SyncStats) error {
	if conv.UUID == "" {
		conv.UUID = uuid.New().String()
	}

	startedAt := parseClaudeTime(conv.CreatedAt, time.Now())
	endedAt := parseClaudeTime(conv.UpdatedAt, time.Now())

	model := conv.ModelSlug
	if model == "" {
		model = "claude"
	}

	title := conv.Name
	if title == "" {
		title = fmt.Sprintf("Chat %s", conv.UUID[:8])
	}

	messageCount := 0
	for _, m := range conv.ChatMessages {
		if m.Text != "" || m.Content != nil {
			messageCount++
		}
	}

	session := &types.Session{
		ID:           conv.UUID,
		ProviderID:   prov.ID,
		Provider:     prov.Type,
		Title:        title,
		Model:        model,
		StartedAt:    startedAt,
		EndedAt:      &endedAt,
		MessageCount: messageCount,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := p.db.UpsertSession(session); err != nil {
		return fmt.Errorf("upsert session: %w", err)
	}
	stats.SessionsFound++

	for _, msg := range conv.ChatMessages {
		content := msg.Text
		if content == "" && msg.Content != nil {
			content = extractClaudeContent(msg.Content)
		}
		if content == "" {
			continue
		}
		role := mapClaudeSender(msg.Sender)

		createdAt := parseClaudeTime(msg.CreatedAt, startedAt)

		message := &types.Message{
			ID:        msg.UUID,
			SessionID: conv.UUID,
			Role:      role,
			Content:   content,
			CreatedAt: createdAt,
		}
		if err := p.db.InsertMessage(message); err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
		stats.MessagesNew++
	}
	return nil
}

func extractClaudeContent(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	var obj struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Text
	}
	return ""
}

func parseClaudeTime(s string, fallback time.Time) time.Time {
	if s == "" {
		return fallback
	}
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
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

func mapClaudeSender(sender string) string {
	switch strings.ToLower(sender) {
	case "human", "user":
		return "user"
	case "assistant":
		return "assistant"
	default:
		return sender
	}
}

func (p *ClaudeWebProvider) ListSessions() ([]*types.Session, error) {
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

func (p *ClaudeWebProvider) GetSessionMessages(sessionID string) ([]*types.Message, error) {
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
