package sync

import (
	"github.com/agent-sync/agent-sync/pkg/types"
)

type Provider interface {
	Type() types.ProviderType
	Name() string
	Detect() (bool, error)
	Sync() (*types.SyncStats, error)
	ListSessions() ([]*types.Session, error)
	GetSessionMessages(sessionID string) ([]*types.Message, error)
}
