package providers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/agent-sync/agent-sync/internal/db"
	"github.com/agent-sync/agent-sync/pkg/types"
	"github.com/google/uuid"
)

type GeminiProvider struct {
	path string
	db   *db.DB
	cfg  Config
}

type GeminiActivity struct {
	Conversations []GeminiActivityConv `json:"conversations"`
}

type GeminiActivityConv struct {
	ID        string        `json:"id,omitempty"`
	Name      string        `json:"name,omitempty"`
	Messages  []GeminiActMsg `json:"messages,omitempty"`
	CreatedAt string        `json:"created_at,omitempty"`
	UpdatedAt string        `json:"updated_at,omitempty"`
}

type GeminiActMsg struct {
	Author  string `json:"author,omitempty"`
	Content string `json:"content,omitempty"`
	Time    string `json:"time,omitempty"`
}

type GeminiAIConversation struct {
	ConversationID string           `json:"conversationId"`
	Title          string           `json:"title"`
	CreateTime     string           `json:"createTime"`
	UpdateTime     string           `json:"updateTime"`
	Model          string           `json:"model"`
	Messages       []GeminiAIMessage `json:"messages"`
}

type GeminiAIMessage struct {
	Author     string `json:"author"`
	Content    string `json:"content"`
	CreateTime string `json:"createTime"`
}

func NewGeminiProvider(cfg Config, database *db.DB) (*GeminiProvider, error) {
	path := cfg.GeminiExportPath
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".agent-sync", "imports", "gemini")
	}
	return &GeminiProvider{path: path, db: database, cfg: cfg}, nil
}

func (p *GeminiProvider) Type() types.ProviderType {
	return types.ProviderGemini
}

func (p *GeminiProvider) Name() string {
	return "Gemini"
}

func (p *GeminiProvider) Detect() (bool, error) {
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
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			return true, nil
		}
	}
	return false, nil
}

func (p *GeminiProvider) getProvider() (*types.Provider, error) {
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

func (p *GeminiProvider) Sync() (*types.SyncStats, error) {
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
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		fpath := filepath.Join(p.path, entry.Name())
		if err := p.importJSONFile(fpath, prov, stats); err != nil {
			fmt.Printf("  ! error importing %s: %v\n", entry.Name(), err)
		}
	}
	now := time.Now()
	prov.LastSync = &now
	p.db.UpsertProvider(prov)
	return stats, nil
}

func (p *GeminiProvider) importJSONFile(path string, prov *types.Provider, stats *types.SyncStats) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var aiConvs []GeminiAIConversation
	if err := json.Unmarshal(data, &aiConvs); err == nil && len(aiConvs) > 0 {
		if aiConvs[0].ConversationID != "" || aiConvs[0].Title != "" {
			for i := range aiConvs {
				p.importAIConversation(&aiConvs[i], prov, stats)
			}
			return nil
		}
	}

	var act GeminiActivity
	if err := json.Unmarshal(data, &act); err == nil && len(act.Conversations) > 0 {
		for i := range act.Conversations {
			p.importActivityConv(&act.Conversations[i], prov, stats)
		}
		return nil
	}

	var rawConvs []map[string]interface{}
	if err := json.Unmarshal(data, &rawConvs); err == nil {
		for _, raw := range rawConvs {
			p.importRawConv(raw, prov, stats)
		}
		return nil
	}

	return fmt.Errorf("unknown gemini export format")
}

func (p *GeminiProvider) importAIConversation(conv *GeminiAIConversation, prov *types.Provider, stats *types.SyncStats) {
	id := conv.ConversationID
	if id == "" {
		id = uuid.New().String()
	}

	title := conv.Title
	if title == "" {
		title = fmt.Sprintf("Chat %s", id[:8])
	}

	startedAt := parseGeminiTime(conv.CreateTime, time.Now())
	endedAt := parseGeminiTime(conv.UpdateTime, time.Now())

	model := conv.Model
	if model == "" {
		model = "gemini"
	}

	msgCount := 0
	for _, m := range conv.Messages {
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

	for _, msg := range conv.Messages {
		if msg.Content == "" {
			continue
		}
		role := mapGeminiRole(msg.Author)
		createdAt := parseGeminiTime(msg.CreateTime, startedAt)

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

func (p *GeminiProvider) importActivityConv(conv *GeminiActivityConv, prov *types.Provider, stats *types.SyncStats) {
	id := conv.ID
	if id == "" {
		id = uuid.New().String()
	}

	title := conv.Name
	if title == "" {
		title = fmt.Sprintf("Chat %s", id[:8])
	}

	startedAt := parseGeminiTime(conv.CreatedAt, time.Now())
	endedAt := parseGeminiTime(conv.UpdatedAt, time.Now())

	session := &types.Session{
		ID:           id,
		ProviderID:   prov.ID,
		Provider:     prov.Type,
		Title:        title,
		Model:        "gemini",
		StartedAt:    startedAt,
		EndedAt:      &endedAt,
		MessageCount: len(conv.Messages),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := p.db.UpsertSession(session); err != nil {
		return
	}
	stats.SessionsFound++

	for _, msg := range conv.Messages {
		if msg.Content == "" {
			continue
		}
		role := mapGeminiRole(msg.Author)
		createdAt := parseGeminiTime(msg.Time, startedAt)

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

func (p *GeminiProvider) importRawConv(raw map[string]interface{}, prov *types.Provider, stats *types.SyncStats) {
	id := getString(raw, "id", "conversationId", "conversation_id", "uuid")
	if id == "" {
		id = uuid.New().String()
	}

	title := getString(raw, "name", "title", "Name", "conversationName")
	if title == "" {
		title = fmt.Sprintf("Chat %s", id[:8])
	}

	startedAt := parseAnyTime(raw, time.Now(), "createTime", "created_at", "createdAt", "create_time")
	endedAt := parseAnyTime(raw, startedAt, "updateTime", "updated_at", "updatedAt", "update_time")

	model := getString(raw, "model", "model_slug", "Model")

	session := &types.Session{
		ID:           id,
		ProviderID:   prov.ID,
		Provider:     prov.Type,
		Title:        title,
		Model:        model,
		StartedAt:    startedAt,
		EndedAt:      &endedAt,
		MessageCount: 0,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := p.db.UpsertSession(session); err != nil {
		return
	}
	stats.SessionsFound++

	messages := extractMessages(raw)
	for _, msg := range messages {
		message := &types.Message{
			ID:        uuid.New().String(),
			SessionID: id,
			Role:      msg.role,
			Content:   msg.content,
			CreatedAt: msg.time,
		}
		if err := p.db.InsertMessage(message); err == nil {
			stats.MessagesNew++
		}
	}
}

type rawMessage struct {
	role    string
	content string
	time    time.Time
}

func extractMessages(raw map[string]interface{}) []rawMessage {
	var msgs []rawMessage

	for _, key := range []string{"messages", "chat_messages", "Message", "ChatMessage"} {
		if raw[key] == nil {
			continue
		}
		list, ok := raw[key].([]interface{})
		if !ok {
			continue
		}
		for _, m := range list {
			obj, ok := m.(map[string]interface{})
			if !ok {
				continue
			}
			role := getString(obj, "author", "sender", "role", "Role", "Author")
			content := getString(obj, "content", "text", "Text", "Content", "message")
			createdAt := parseAnyTime(obj, time.Now(), "createTime", "created_at", "createdAt", "time")
			if content != "" {
				msgs = append(msgs, rawMessage{role: mapGeminiRole(role), content: content, time: createdAt})
			}
		}
	}
	return msgs
}

func getString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func parseAnyTime(m map[string]interface{}, fallback time.Time, keys ...string) time.Time {
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if t := parseGeminiTime(v, time.Time{}); !t.IsZero() {
				return t
			}
		case float64:
			return time.Unix(int64(v), 0)
		}
	}
	return fallback
}

func parseGeminiTime(s string, fallback time.Time) time.Time {
	if s == "" {
		return fallback
	}
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
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

var geminiModelRe = regexp.MustCompile(`(?i)(gemini[\w.\-]*)`)

func mapGeminiRole(author string) string {
	switch strings.ToLower(author) {
	case "user", "human":
		return "user"
	case "assistant", "model", "bot", "ai":
		return "assistant"
	case "system":
		return "system"
	default:
		if author == "" {
			return "user"
		}
		return author
	}
}

func (p *GeminiProvider) ListSessions() ([]*types.Session, error) {
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

func (p *GeminiProvider) GetSessionMessages(sessionID string) ([]*types.Message, error) {
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
