package context

import (
	"strings"
	"time"

	"github.com/agent-sync/agent-sync/internal/db"
	"github.com/agent-sync/agent-sync/pkg/types"
)

type Store struct {
	db *db.DB
}

func NewStore(database *db.DB) *Store {
	return &Store{db: database}
}

func (s *Store) Save(content, summary, source, sourceID string, tags []string) (*types.ContextEntry, error) {
	entry := &types.ContextEntry{
		ID:        db.NewID(),
		Content:   content,
		Summary:   summary,
		Source:    source,
		SourceID:  sourceID,
		Tags:      tags,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.db.SaveContextEntry(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

func (s *Store) Search(query string, limit int) ([]types.ContextEntry, error) {
	return s.db.SearchContext(query, limit)
}

func (s *Store) List(limit, offset int) ([]types.ContextEntry, error) {
	return s.db.ListContextEntries(limit, offset)
}

func (s *Store) Delete(id string) error {
	return s.db.DeleteContextEntry(id)
}

func (s *Store) GetBySource(source string) ([]types.ContextEntry, error) {
	all, err := s.db.ListContextEntries(1000, 0)
	if err != nil {
		return nil, err
	}
	var filtered []types.ContextEntry
	for _, e := range all {
		if e.Source == source {
			filtered = append(filtered, e)
		}
	}
	return filtered, nil
}

func (s *Store) GetByTag(tag string) ([]types.ContextEntry, error) {
	all, err := s.db.ListContextEntries(1000, 0)
	if err != nil {
		return nil, err
	}
	var filtered []types.ContextEntry
	for _, e := range all {
		for _, t := range e.Tags {
			if strings.EqualFold(t, tag) {
				filtered = append(filtered, e)
				break
			}
		}
	}
	return filtered, nil
}
