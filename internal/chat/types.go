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
	ID              string
	ChatID          string
	Sequence        int
	Role            string
	Content         string
	Agent           Agent
	Source          string
	NativeMessageID string
	CreatedAt       time.Time
}

type AgentSession struct {
	ID                    string
	ChatID                string
	Agent                 Agent
	NativeSessionID       string
	LastDeliveredSequence int
	LastActiveAt          *time.Time
	RuntimeKind           RuntimeKind
	Status                RuntimeStatus
	CapabilitiesJSON      string
	CommandsJSON          string
	ConfigJSON            string
	LastError             string
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
	ContextUsed   int
	ContextWindow int
	CostUSD       float64
	UpdatedAt     time.Time
}

type Handoff struct {
	Target Agent
	From   int
	To     int
}

type RuntimeKind string

const (
	RuntimeACP RuntimeKind = "acp"
	RuntimePTY RuntimeKind = "pty"
)

type RuntimeStatus string

const (
	RuntimeStopped        RuntimeStatus = "stopped"
	RuntimeStarting       RuntimeStatus = "starting"
	RuntimeIdle           RuntimeStatus = "idle"
	RuntimeWorking        RuntimeStatus = "working"
	RuntimeWaitingForUser RuntimeStatus = "waiting"
	RuntimeCrashed        RuntimeStatus = "crashed"
)

type NativeCommand struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputHint   string `json:"input_hint,omitempty"`
}

type PermissionOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind,omitempty"`
}

type PermissionRequest struct {
	ID      string             `json:"id"`
	Title   string             `json:"title"`
	Options []PermissionOption `json:"options"`
}

type RuntimeState struct {
	ChatID          string
	Agent           Agent
	Kind            RuntimeKind
	Status          RuntimeStatus
	NativeSessionID string
	Commands        []NativeCommand
	Capabilities    map[string]any
	Config          map[string]any
	Metrics         AgentMetrics
	Terminal        string
	TerminalActive  bool
	LastError       string
	PendingApproval *PermissionRequest
}

type QueueStatus string

const (
	QueueQueued    QueueStatus = "queued"
	QueueRunning   QueueStatus = "running"
	QueueCompleted QueueStatus = "completed"
	QueueCancelled QueueStatus = "cancelled"
	QueueFailed    QueueStatus = "failed"
)

type QueueItem struct {
	ID            string
	ChatID        string
	Agent         Agent
	UserMessageID string
	UserSequence  int
	Content       string
	Status        QueueStatus
	Error         string
	CreatedAt     time.Time
	StartedAt     *time.Time
	CompletedAt   *time.Time
}

type NativeCommandResult struct {
	Reply        *Message
	AgentControl bool
}
