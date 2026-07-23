package types

import "time"

type ProviderType string

const (
	ProviderClaudeCode ProviderType = "claude-code"
	ProviderOpenCode   ProviderType = "opencode"
	ProviderCursor     ProviderType = "cursor"
	ProviderCodex      ProviderType = "codex"
	ProviderChatGPT    ProviderType = "chatgpt"
	ProviderClaudeWeb  ProviderType = "claude-web"
)

type Session struct {
	ID         string       `json:"id"`
	ProviderID string       `json:"provider_id"`
	Provider   ProviderType `json:"provider"`
	Title      string       `json:"title"`
	Model      string       `json:"model"`
	Workspace  string       `json:"workspace"`
	ProjectDir string       `json:"project_dir"`
	StartedAt  time.Time    `json:"started_at"`
	EndedAt    *time.Time   `json:"ended_at,omitempty"`
	TokenCount int          `json:"token_count"`
	MessageCount int        `json:"message_count"`
	Metadata   string       `json:"metadata,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
}

type Message struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	ParentID  *string   `json:"parent_id,omitempty"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	TokenCount int      `json:"token_count"`
	ToolCalls string    `json:"tool_calls,omitempty"`
	Metadata  string    `json:"metadata,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type ContextEntry struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Summary   string    `json:"summary,omitempty"`
	Source    string    `json:"source"`
	SourceID  string    `json:"source_id,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	GroupIDs  []string  `json:"group_ids,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ContextMerge struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ParentIDs []string  `json:"parent_ids"`
	ResultID  string    `json:"result_id"`
	Strategy  string    `json:"strategy"`
	CreatedAt time.Time `json:"created_at"`
}

type AgentGroup struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	ProviderIDs []string `json:"provider_ids"`
	ContextIDs  []string `json:"context_ids,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Provider struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Type      ProviderType `json:"type"`
	Path      string       `json:"path"`
	Config    string       `json:"config,omitempty"`
	Enabled   bool         `json:"enabled"`
	LastSync  *time.Time   `json:"last_sync,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type ExportFormat string

const (
	ExportJSON     ExportFormat = "json"
	ExportMarkdown ExportFormat = "markdown"
	ExportCSV      ExportFormat = "csv"
	ExportHTML     ExportFormat = "html"
)

type SyncStats struct {
	ProviderID    string `json:"provider_id"`
	SessionsFound int    `json:"sessions_found"`
	SessionsNew   int    `json:"sessions_new"`
	MessagesNew   int    `json:"messages_new"`
	Errors        int    `json:"errors"`
}
