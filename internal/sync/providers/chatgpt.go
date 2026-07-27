package providers

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/agent-sync/agent-sync/internal/db"
	"github.com/agent-sync/agent-sync/pkg/types"
	"github.com/google/uuid"
)

type ChatGPTProvider struct {
	path string
	db   *db.DB
	cfg  Config
}

type ChatGPTExport struct {
	Title        string                 `json:"title"`
	CreateTime   float64                `json:"create_time"`
	UpdateTime   float64                `json:"update_time"`
	ConversationID string              `json:"conversation_id"`
	Mapping      map[string]ChatGPTNode `json:"mapping"`
	Modelf       string                 `json:"modelf,omitempty"`
	Model        map[string]interface{} `json:"model,omitempty"`
	DefaultModelSlug string            `json:"default_model_slug,omitempty"`
}

type ChatGPTNode struct {
	ID       string          `json:"id"`
	Message  *ChatGPTMessage `json:"message,omitempty"`
	Parent   *string         `json:"parent"`
	Children []string        `json:"children"`
}

type ChatGPTMessage struct {
	Author    ChatGPTAuthor    `json:"author"`
	Content   ChatGPTContent   `json:"content"`
	CreateTime *float64        `json:"create_time"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type ChatGPTAuthor struct {
	Role string `json:"role"`
}

type ChatGPTContent struct {
	ContentType string          `json:"content_type"`
	Parts       []json.RawMessage `json:"parts"`
}

func NewChatGPTProvider(cfg Config, database *db.DB) (*ChatGPTProvider, error) {
	path := cfg.ChatGPTExportPath
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".agent-sync", "imports", "chatgpt")
	}
	return &ChatGPTProvider{path: path, db: database, cfg: cfg}, nil
}

func (p *ChatGPTProvider) Type() types.ProviderType {
	return types.ProviderChatGPT
}

func (p *ChatGPTProvider) Name() string {
	return "ChatGPT"
}

func (p *ChatGPTProvider) Detect() (bool, error) {
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
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".zip") || strings.HasSuffix(e.Name(), ".json")) {
			return true, nil
		}
	}
	return false, nil
}

func (p *ChatGPTProvider) getProvider() (*types.Provider, error) {
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

func (p *ChatGPTProvider) Sync() (*types.SyncStats, error) {
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

func (p *ChatGPTProvider) importZIP(zipPath string, prov *types.Provider, stats *types.SyncStats) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name != "conversations.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open %s in zip: %w", f.Name, err)
		}
		defer rc.Close()

		data, err := io.ReadAll(rc)
		if err != nil {
			return fmt.Errorf("read %s: %w", f.Name, err)
		}
		return p.parseConversationsJSON(data, prov, stats)
	}
	return fmt.Errorf("conversations.json not found in zip")
}

func (p *ChatGPTProvider) importJSONFile(path string, prov *types.Provider, stats *types.SyncStats) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return p.parseConversationsJSON(data, prov, stats)
}

func (p *ChatGPTProvider) parseConversationsJSON(data []byte, prov *types.Provider, stats *types.SyncStats) error {
	var exports []ChatGPTExport
	if err := json.Unmarshal(data, &exports); err != nil {
		var single ChatGPTExport
		if err2 := json.Unmarshal(data, &single); err2 != nil {
			return fmt.Errorf("parse conversations.json: %w", err)
		}
		exports = []ChatGPTExport{single}
	}

	for _, conv := range exports {
		if err := p.importConversation(&conv, prov, stats); err != nil {
			fmt.Printf("  ! conversation %s: %v\n", conv.ConversationID, err)
		}
	}
	return nil
}

func (p *ChatGPTProvider) importConversation(conv *ChatGPTExport, prov *types.Provider, stats *types.SyncStats) error {
	if len(conv.Mapping) == 0 {
		return nil
	}
	sessionID := conv.ConversationID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	startedAt := time.Unix(int64(conv.CreateTime), 0)
	endedAt := time.Unix(int64(conv.UpdateTime), 0)

	model := p.resolveModel(conv)

	title := conv.Title
	if title == "" {
		title = fmt.Sprintf("Chat %s", sessionID[:8])
	}

	nodes := p.linearizeMapping(conv.Mapping)

	session := &types.Session{
		ID:           sessionID,
		ProviderID:   prov.ID,
		Provider:     prov.Type,
		Title:        title,
		Model:        model,
		StartedAt:    startedAt,
		EndedAt:      &endedAt,
		MessageCount: len(nodes),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := p.db.UpsertSession(session); err != nil {
		return fmt.Errorf("upsert session: %w", err)
	}
	stats.SessionsFound++

	nodeMap := make(map[string]*ChatGPTNode)
	for i := range conv.Mapping {
		n := conv.Mapping[i]
		nodeMap[i] = &n
	}

	for _, n := range nodes {
		msg := n.Message
		if msg == nil {
			continue
		}
		content := extractChatGPTContent(&msg.Content)
		if content == "" {
			continue
		}
		role := mapChatGPTRole(msg.Author.Role)

		createdAt := startedAt
		if msg.CreateTime != nil {
			createdAt = time.Unix(int64(*msg.CreateTime), 0)
		}

		var parentID *string
		if n.Parent != nil {
			if parent, ok := nodeMap[*n.Parent]; ok && parent.Message != nil {
				pid := parent.ID
				parentID = &pid
			}
		}

		message := &types.Message{
			ID:        n.ID,
			SessionID: sessionID,
			ParentID:  parentID,
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

func (p *ChatGPTProvider) resolveModel(conv *ChatGPTExport) string {
	if conv.Modelf != "" {
		return conv.Modelf
	}
	if slug, ok := conv.Model["slug"].(string); ok {
		return slug
	}
	if conv.DefaultModelSlug != "" {
		return conv.DefaultModelSlug
	}
	for _, v := range conv.Model {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return "chatgpt"
}

func (p *ChatGPTProvider) linearizeMapping(mapping map[string]ChatGPTNode) []ChatGPTNode {
	var root *ChatGPTNode
	for i := range mapping {
		n := mapping[i]
		if n.Parent == nil || *n.Parent == "" {
			root = &n
			break
		}
	}
	if root == nil {
		for i := range mapping {
			n := mapping[i]
			root = &n
			break
		}
	}

	var ordered []ChatGPTNode
	current := root
	visited := make(map[string]bool)

	for current != nil {
		if visited[current.ID] {
			break
		}
		visited[current.ID] = true
		if current.Message != nil {
			ordered = append(ordered, *current)
		}
		if len(current.Children) == 0 {
			break
		}
		if len(current.Children) == 1 {
			next, ok := mapping[current.Children[0]]
			if !ok {
				break
			}
			current = &next
			continue
		}
		current = pickMainBranch(current.Children, mapping)
	}
	return ordered
}

func pickMainBranch(children []string, mapping map[string]ChatGPTNode) *ChatGPTNode {
	var latest *ChatGPTNode
	var latestTime float64
	for _, cid := range children {
		n, ok := mapping[cid]
		if !ok {
			continue
		}
		if n.Message == nil || n.Message.CreateTime == nil {
			continue
		}
		t := *n.Message.CreateTime
		if latest == nil || t > latestTime {
			latest = &n
			latestTime = t
		}
	}
	if latest == nil {
		n := mapping[children[0]]
		return &n
	}
	return latest
}

func (p *ChatGPTProvider) ListSessions() ([]*types.Session, error) {
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

func (p *ChatGPTProvider) GetSessionMessages(sessionID string) ([]*types.Message, error) {
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

func extractChatGPTContent(c *ChatGPTContent) string {
	switch c.ContentType {
	case "text", "":
		var parts []string
		for _, p := range c.Parts {
			var s string
			if err := json.Unmarshal(p, &s); err == nil {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	case "multimodal_text":
		parts := extractMultimodalParts(c.Parts)
		return strings.Join(parts, "\n")
	case "code":
		var s string
		if len(c.Parts) > 0 {
			json.Unmarshal(c.Parts[0], &s)
		}
		return s
	case "execution_output":
		var s string
		if len(c.Parts) > 0 {
			json.Unmarshal(c.Parts[0], &s)
		}
		return s
	case "tether_browsing":
		p := extractMultimodalParts(c.Parts)
		return strings.Join(p, "\n")
	case "tether_quote":
		var s string
		if len(c.Parts) > 0 {
			json.Unmarshal(c.Parts[0], &s)
		}
		return s
	case "system":
		var parts []string
		for _, p := range c.Parts {
			var s string
			if err := json.Unmarshal(p, &s); err == nil {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	default:
		data, _ := json.Marshal(c.Parts)
		return string(data)
	}
}

func extractMultimodalParts(parts []json.RawMessage) []string {
	var result []string
	for _, p := range parts {
		var obj map[string]interface{}
		if err := json.Unmarshal(p, &obj); err != nil {
			var s string
			if err := json.Unmarshal(p, &s); err == nil {
				result = append(result, s)
			}
			continue
		}
		if t, ok := obj["type"].(string); ok && t == "text" {
			if text, ok := obj["text"].(string); ok {
				result = append(result, text)
			}
		}
		if asset, ok := obj["asset_pointer"].(string); ok {
			result = append(result, fmt.Sprintf("[asset: %s]", asset))
		}
		if url, ok := obj["url"].(string); ok {
			result = append(result, fmt.Sprintf("[image: %s]", url))
		}
	}
	return result
}

func mapChatGPTRole(role string) string {
	switch strings.ToLower(role) {
	case "user":
		return "user"
	case "assistant":
		return "assistant"
	case "system":
		return "system"
	case "tool":
		return "tool"
	default:
		return role
	}
}

func (p *ChatGPTProvider) importConversationsSorted(convs []ChatGPTExport) {
	sort.Slice(convs, func(i, j int) bool {
		return convs[i].CreateTime < convs[j].CreateTime
	})
}
