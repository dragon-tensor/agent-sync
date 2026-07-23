package groups

import (
	"fmt"
	"time"

	"github.com/agent-sync/agent-sync/internal/db"
	"github.com/agent-sync/agent-sync/pkg/types"
)

type Manager struct {
	db *db.DB
}

func NewManager(database *db.DB) *Manager {
	return &Manager{db: database}
}

func (m *Manager) Create(name, description string, providerIDs []string) (*types.AgentGroup, error) {
	groups, err := m.db.ListAgentGroups()
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		if g.Name == name {
			return nil, fmt.Errorf("group %q already exists", name)
		}
	}

	group := &types.AgentGroup{
		ID:          db.NewID(),
		Name:        name,
		Description: description,
		ProviderIDs: providerIDs,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := m.db.SaveAgentGroup(group); err != nil {
		return nil, err
	}
	return group, nil
}

func (m *Manager) List() ([]types.AgentGroup, error) {
	return m.db.ListAgentGroups()
}

func (m *Manager) Delete(id string) error {
	return m.db.DeleteAgentGroup(id)
}

func (m *Manager) AddContextToGroup(groupID, contextID string) error {
	groups, err := m.db.ListAgentGroups()
	if err != nil {
		return err
	}
	for _, g := range groups {
		if g.ID == groupID {
			for _, cid := range g.ContextIDs {
				if cid == contextID {
					return nil
				}
			}
			g.ContextIDs = append(g.ContextIDs, contextID)
			g.UpdatedAt = time.Now()
			return m.db.SaveAgentGroup(&g)
		}
	}
	return fmt.Errorf("group %q not found", groupID)
}

func (m *Manager) GetSharedContext(groupID string) ([]types.ContextEntry, error) {
	groups, err := m.db.ListAgentGroups()
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		if g.ID == groupID {
			var entries []types.ContextEntry
			for _, cid := range g.ContextIDs {
				all, err := m.db.ListContextEntries(1000, 0)
				if err != nil {
					continue
				}
				for _, e := range all {
					if e.ID == cid {
						entries = append(entries, e)
						break
					}
				}
			}
			return entries, nil
		}
	}
	return nil, fmt.Errorf("group %q not found", groupID)
}
