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
	yaml "gopkg.in/yaml.v2"
)

type GenericProvider struct {
	path string
	db   *db.DB
}

type genericMsg struct {
	Role    string
	Content string
}

type GenericAdapter struct {
	provider *GenericProvider
}

func NewGenericProvider(cfg Config, database *db.DB) (*GenericProvider, error) {
	path := cfg.GenericImportPath
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".agent-sync", "imports", "generic")
	}
	return &GenericProvider{path: path, db: database}, nil
}

func (p *GenericProvider) Type() types.ProviderType {
	return types.ProviderGeneric
}

func (p *GenericProvider) Name() string {
	return "Generic Import"
}

func (p *GenericProvider) Detect() (bool, error) {
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
		if !e.IsDir() {
			ext := filepath.Ext(e.Name())
			if ext == ".json" || ext == ".jsonl" || ext == ".md" || ext == ".txt" {
				return true, nil
			}
		}
	}
	return false, nil
}

func (p *GenericProvider) getProvider() (*types.Provider, error) {
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

func (p *GenericProvider) Sync() (*types.SyncStats, error) {
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
		ext := filepath.Ext(entry.Name())

		switch ext {
		case ".json":
			if err := p.importJSON(fpath, prov, stats); err != nil {
				fmt.Printf("  ! %s: %v\n", entry.Name(), err)
			}
		case ".jsonl":
			if err := p.importJSONL(fpath, prov, stats); err != nil {
				fmt.Printf("  ! %s: %v\n", entry.Name(), err)
			}
		case ".md", ".txt":
			if err := p.importMarkdown(fpath, prov, stats); err != nil {
				fmt.Printf("  ! %s: %v\n", entry.Name(), err)
			}
		}
	}

	now := time.Now()
	prov.LastSync = &now
	p.db.UpsertProvider(prov)
	return stats, nil
}

func (p *GenericProvider) importJSON(fpath string, prov *types.Provider, stats *types.SyncStats) error {
	data, err := os.ReadFile(fpath)
	if err != nil {
		return err
	}

	var sessions []struct {
		ID        string `json:"id,omitempty"`
		Title     string `json:"title,omitempty"`
		Model     string `json:"model,omitempty"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
			Time    string `json:"time,omitempty"`
		} `json:"messages,omitempty"`
		CreatedAt string `json:"created_at,omitempty"`
		UpdatedAt string `json:"updated_at,omitempty"`
	}
	if err := json.Unmarshal(data, &sessions); err != nil {
		var single struct {
			ID        string `json:"id,omitempty"`
			Title     string `json:"title,omitempty"`
			Model     string `json:"model,omitempty"`
			Messages  []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
				Time    string `json:"time,omitempty"`
			} `json:"messages,omitempty"`
			CreatedAt string `json:"created_at,omitempty"`
			UpdatedAt string `json:"updated_at,omitempty"`
		}
		if err2 := json.Unmarshal(data, &single); err2 != nil {
			var rawMsgs []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}
			if err3 := json.Unmarshal(data, &rawMsgs); err3 == nil && len(rawMsgs) > 0 {
				id := uuid.New().String()
				title := strings.TrimSuffix(filepath.Base(fpath), filepath.Ext(fpath))
				var raw []genericMsg
				for _, m := range rawMsgs {
					raw = append(raw, genericMsg{m.Role, m.Content})
				}
				p.importRawMessages(id, title, "", raw, prov, stats)
				return nil
			}
			return fmt.Errorf("cannot parse JSON: %w", err)
		}
		sessions = append(sessions, single)
	}

	for _, s := range sessions {
		id := s.ID
		if id == "" {
			id = uuid.New().String()
		}
		title := s.Title
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(fpath), filepath.Ext(fpath))
		}
		var msgs []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
			Time    string `json:"time,omitempty"`
		}
		msgs = s.Messages
		if len(msgs) == 0 {
			continue
		}

		var raw []genericMsg
		for _, m := range msgs {
			raw = append(raw, genericMsg{m.Role, m.Content})
		}
		p.importRawMessages(id, title, s.Model, raw, prov, stats)
	}
	return nil
}

func (p *GenericProvider) importJSONL(fpath string, prov *types.Provider, stats *types.SyncStats) error {
	f, err := os.Open(fpath)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var msgs []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
		Time    string `json:"time,omitempty"`
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var msg struct {
			Role    string `json:"role"`
			Content string `json:"content"`
			Time    string `json:"time,omitempty"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg.Role != "" && msg.Content != "" {
			msgs = append(msgs, msg)
		}
	}

	if len(msgs) > 0 {
		id := uuid.New().String()
		title := strings.TrimSuffix(filepath.Base(fpath), filepath.Ext(fpath))
		var raw []genericMsg
		for _, m := range msgs {
			raw = append(raw, genericMsg{m.Role, m.Content})
		}
		p.importRawMessages(id, title, "", raw, prov, stats)
	}
	return nil
}

type markdownFrontmatter struct {
	Title   string `yaml:"title"`
	Model   string `yaml:"model"`
	Date    string `yaml:"date"`
	Created string `yaml:"created_at"`
}

func (p *GenericProvider) importMarkdown(fpath string, prov *types.Provider, stats *types.SyncStats) error {
	data, err := os.ReadFile(fpath)
	if err != nil {
		return err
	}

	content := string(data)
	fm := markdownFrontmatter{}
	body := content

	if strings.HasPrefix(strings.TrimSpace(content), "---") {
		parts := strings.SplitN(content[3:], "---", 2)
		if len(parts) == 2 {
			if err := yaml.Unmarshal([]byte(parts[0]), &fm); err == nil {
				body = strings.TrimSpace(parts[1])
			}
		}
	}

	id := uuid.New().String()
	title := fm.Title
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(fpath), filepath.Ext(fpath))
	}

	model := fm.Model
	createdStr := fm.Date
	if createdStr == "" {
		createdStr = fm.Created
	}
	startedAt := parseGenericTime(createdStr, time.Now())

	var msgs []genericMsg
	lines := strings.Split(body, "\n")
	var currentRole string
	var currentContent []string
	rolePrefix := map[string]string{
		"## User:":       "user",
		"## User":        "user",
		"**User:**":      "user",
		"**User**:":      "user",
		"**Assistant:**": "assistant",
		"**Assistant**:": "assistant",
		"## Assistant:":  "assistant",
		"## Assistant":   "assistant",
		"### User":       "user",
		"### Assistant":  "assistant",
		"> **User**":     "user",
		"> **Assistant**": "assistant",
	}

	flush := func() {
		if currentRole != "" && len(currentContent) > 0 {
		msgs = append(msgs, genericMsg{
			Role:    currentRole,
			Content: strings.TrimSpace(strings.Join(currentContent, "\n")),
		})
		}
		currentContent = nil
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		found := false
		for prefix, role := range rolePrefix {
			if strings.HasPrefix(trimmed, prefix) {
				flush()
				currentRole = role
				rest := strings.TrimPrefix(trimmed, prefix)
				rest = strings.TrimSpace(rest)
				if rest != "" {
					currentContent = append(currentContent, rest)
				}
				found = true
				break
			}
		}
		if found {
			continue
		}
		if currentRole != "" {
			currentContent = append(currentContent, line)
		}
	}
	flush()

	if len(msgs) == 0 {
		msgs = append(msgs, genericMsg{"user", "see body"}, genericMsg{"assistant", body})
	}

	p.importRawMessagesAt(id, title, model, msgs, prov, stats, startedAt)
	return nil
}

func (p *GenericProvider) importRawMessages(id, title, model string, msgs []genericMsg, prov *types.Provider, stats *types.SyncStats) {
	p.importRawMessagesAt(id, title, model, msgs, prov, stats, time.Now())
}

func (p *GenericProvider) importRawMessagesAt(id, title, model string, msgs []genericMsg, prov *types.Provider, stats *types.SyncStats, startedAt time.Time) {
	if model == "" {
		model = "generic"
	}

	session := &types.Session{
		ID:           id,
		ProviderID:   prov.ID,
		Provider:     prov.Type,
		Title:        title,
		Model:        model,
		StartedAt:    startedAt,
		MessageCount: len(msgs),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := p.db.UpsertSession(session); err != nil {
		return
	}
	stats.SessionsFound++

	for _, m := range msgs {
		if m.Content == "" {
			continue
		}
		message := &types.Message{
			ID:        uuid.New().String(),
			SessionID: id,
			Role:      m.Role,
			Content:   m.Content,
			CreatedAt: startedAt,
		}
		if err := p.db.InsertMessage(message); err == nil {
			stats.MessagesNew++
		}
	}
}

func parseGenericTime(s string, fallback time.Time) time.Time {
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

func (p *GenericProvider) ListSessions() ([]*types.Session, error) {
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

func (p *GenericProvider) GetSessionMessages(sessionID string) ([]*types.Message, error) {
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
