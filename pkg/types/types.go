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
	ProviderGemini     ProviderType = "gemini"
	ProviderCopilot    ProviderType = "copilot"
	ProviderGeneric    ProviderType = "generic"
)

type Session struct {
	ID           string       `json:"id"`
	ProviderID   string       `json:"provider_id"`
	Provider     ProviderType `json:"provider"`
	Title        string       `json:"title"`
	Model        string       `json:"model"`
	Workspace    string       `json:"workspace"`
	ProjectDir   string       `json:"project_dir"`
	StartedAt    time.Time    `json:"started_at"`
	EndedAt      *time.Time   `json:"ended_at,omitempty"`
	TokenCount   int          `json:"token_count"`
	MessageCount int          `json:"message_count"`
	Metadata     string       `json:"metadata,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

type Message struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id"`
	ParentID   *string   `json:"parent_id,omitempty"`
	Role       string    `json:"role"`
	Content    string    `json:"content"`
	TokenCount int       `json:"token_count"`
	ToolCalls  string    `json:"tool_calls,omitempty"`
	Metadata   string    `json:"metadata,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type ContextEntry struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Summary   string    `json:"summary,omitempty"`
	Source    string    `json:"source"`
	SourceID  string    `json:"source_id,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
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

type EntityType string

const (
	EntityDecision   EntityType = "decision"
	EntityFact       EntityType = "fact"
	EntityCode       EntityType = "code_pattern"
	EntityPreference EntityType = "preference"
	EntityGoal       EntityType = "goal"
)

type Entity struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	EntityType EntityType `json:"entity_type"`
	Summary    string     `json:"summary"`
	Content    string     `json:"content"`
	Source     string     `json:"source"`
	SourceID   string     `json:"source_id,omitempty"`
	SessionID  string     `json:"session_id,omitempty"`
	Confidence float64    `json:"confidence"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type EntityRelation struct {
	ID             string    `json:"id"`
	SourceEntityID string    `json:"source_entity_id"`
	TargetEntityID string    `json:"target_entity_id"`
	RelationType   string    `json:"relation_type"`
	Weight         float64   `json:"weight"`
	Evidence       string    `json:"evidence,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type Snapshot struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	EntityIDs   []string  `json:"entity_ids"`
	CreatedAt   time.Time `json:"created_at"`
}
