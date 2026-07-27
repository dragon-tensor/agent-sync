package providers

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/agent-sync/agent-sync/internal/db"
	"github.com/agent-sync/agent-sync/pkg/types"
)

type CursorProvider struct {
	path string
	db   *db.DB
}

func NewCursorProvider(cfg Config, database *db.DB) (*CursorProvider, error) {
	path := cfg.CursorPath
	if path == "" {
		path = detectCursorPath()
	}
	return &CursorProvider{path: path, db: database}, nil
}

func detectCursorPath() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "linux":
		return filepath.Join(home, ".config", "Cursor", "User", "workspaceStorage")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Cursor", "User", "workspaceStorage")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Cursor", "User", "workspaceStorage")
	}
	return filepath.Join(home, ".config", "Cursor", "User", "workspaceStorage")
}

func (p *CursorProvider) Type() types.ProviderType {
	return types.ProviderCursor
}

func (p *CursorProvider) Name() string {
	return "Cursor"
}

func (p *CursorProvider) Detect() (bool, error) {
	info, err := os.Stat(p.path)
	if err != nil {
		return false, nil
	}
	return info.IsDir(), nil
}

func (p *CursorProvider) getProvider() (*types.Provider, error) {
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

func (p *CursorProvider) Sync() (*types.SyncStats, error) {
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
		stateDB := filepath.Join(p.path, entry.Name(), "state.vscdb")
		if _, err := os.Stat(stateDB); err != nil {
			continue
		}
		stats.SessionsFound += 1
	}

	now := time.Now()
	prov.LastSync = &now
	p.db.UpsertProvider(prov)
	return stats, nil
}

func (p *CursorProvider) ListSessions() ([]*types.Session, error) {
	sessions, err := p.db.ListSessions(string(p.Type()), 100, 0)
	if err != nil {
		return nil, err
	}
	var result []*types.Session
	for i := range sessions {
		result = append(result, &sessions[i])
	}
	return result, nil
}

func (p *CursorProvider) GetSessionMessages(sessionID string) ([]*types.Message, error) {
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
