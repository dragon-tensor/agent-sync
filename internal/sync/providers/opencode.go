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

type OpenCodeProvider struct {
	path string
	db   *db.DB
}

type OpenCodeSession struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Model     string                 `json:"model"`
	Messages  []OpenCodeMessage      `json:"messages"`
	CreatedAt string                 `json:"created_at"`
	UpdatedAt string                 `json:"updated_at"`
	Metadata  map[string]interface{} `json:"metadata"`
}

type OpenCodeMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

func NewOpenCodeProvider(cfg Config, database *db.DB) (*OpenCodeProvider, error) {
	path := cfg.OpenCodePath
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".local", "share", "opencode")
	}
	return &OpenCodeProvider{path: path, db: database}, nil
}

func (p *OpenCodeProvider) Type() types.ProviderType {
	return types.ProviderOpenCode
}

func (p *OpenCodeProvider) Name() string {
	return "OpenCode"
}

func (p *OpenCodeProvider) Detect() (bool, error) {
	info, err := os.Stat(p.path)
	if err != nil {
		return false, nil
	}
	return info.IsDir(), nil
}

func (p *OpenCodeProvider) getProvider() (*types.Provider, error) {
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

func (p *OpenCodeProvider) Sync() (*types.SyncStats, error) {
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
		sessionDir := filepath.Join(p.path, entry.Name())
		files, err := os.ReadDir(sessionDir)
		if err != nil {
			continue
		}

		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".json") && !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			sessionPath := filepath.Join(sessionDir, f.Name())
			if err := p.importFile(sessionPath, prov, stats); err != nil {
				fmt.Printf("  ! error importing %s: %v\n", f.Name(), err)
			}
		}
	}

	now := time.Now()
	prov.LastSync = &now
	p.db.UpsertProvider(prov)
	return stats, nil
}

func (p *OpenCodeProvider) importFile(path string, prov *types.Provider, stats *types.SyncStats) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var cs OpenCodeSession
	if err := json.Unmarshal(data, &cs); err != nil {
		return nil
	}

	startedAt := time.Now()
	if cs.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, cs.CreatedAt); err == nil {
			startedAt = t
		}
	}

	session := &types.Session{
		ID:           cs.ID,
		ProviderID:   prov.ID,
		Provider:     prov.Type,
		Title:        cs.Name,
		Model:        cs.Model,
		StartedAt:    startedAt,
		MessageCount: len(cs.Messages),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if session.Title == "" {
		session.Title = fmt.Sprintf("Session %s", cs.ID[:8])
	}
	if len(cs.Messages) > 0 {
		t := startedAt
		session.EndedAt = &t
	}

	if err := p.db.UpsertSession(session); err != nil {
		return err
	}
	stats.SessionsFound++

	for _, msg := range cs.Messages {
		message := &types.Message{
			ID:        uuid.New().String(),
			SessionID: session.ID,
			Role:      msg.Role,
			Content:   msg.Content,
			CreatedAt: time.Now(),
		}
		if msg.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339, msg.CreatedAt); err == nil {
				message.CreatedAt = t
			}
		}
		if err := p.db.InsertMessage(message); err != nil {
			return err
		}
		stats.MessagesNew++
	}

	return nil
}

func (p *OpenCodeProvider) ListSessions() ([]*types.Session, error) {
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

func (p *OpenCodeProvider) GetSessionMessages(sessionID string) ([]*types.Message, error) {
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
