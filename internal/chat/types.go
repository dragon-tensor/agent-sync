package chat

import "time"

type Agent string

const (
	AgentClaude   Agent = "claude"
	AgentCodex    Agent = "codex"
	AgentOpenCode Agent = "opencode"
	AgentGemini   Agent = "gemini"
)

type Chat struct {
	ID          string
	Title       string
	ProjectDir  string
	ActiveAgent Agent
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Message struct {
	ID        string
	ChatID    string
	Sequence  int
	Role      string
	Content   string
	Agent     Agent
	CreatedAt time.Time
}

type AgentSession struct {
	ID                    string
	ChatID                string
	Agent                 Agent
	NativeSessionID       string
	LastDeliveredSequence int
	LastActiveAt          *time.Time
}

// AgentMetrics is deliberately a common, optional view. Different CLIs expose
// different fields, so a zero value means that provider did not report it.
type AgentMetrics struct {
	ChatID        string
	Agent         Agent
	Model         string
	Effort        string
	InputTokens   int
	OutputTokens  int
	ContextWindow int
	CostUSD       float64
	UpdatedAt     time.Time
}

type Handoff struct {
	Target Agent
	From   int
	To     int
}
