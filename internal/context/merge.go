package context

import (
	"fmt"
	"strings"
	"time"

	"github.com/agent-sync/agent-sync/internal/db"
	"github.com/agent-sync/agent-sync/pkg/types"
)

type MergeEngine struct {
	db    *db.DB
	store *Store
}

func NewMergeEngine(database *db.DB, store *Store) *MergeEngine {
	return &MergeEngine{db: database, store: store}
}

func (m *MergeEngine) Merge(entryIDs []string, name string, strategy string) (*types.ContextEntry, error) {
	if len(entryIDs) < 2 {
		return nil, fmt.Errorf("need at least 2 entries to merge")
	}

	var entries []types.ContextEntry
	for _, id := range entryIDs {
		all, err := m.store.List(1000, 0)
		if err != nil {
			return nil, err
		}
		for _, e := range all {
			if e.ID == id || strings.HasPrefix(e.ID, id) {
				entries = append(entries, e)
				break
			}
		}
	}

	if len(entries) < 2 {
		return nil, fmt.Errorf("found %d of %d requested entries", len(entries), len(entryIDs))
	}

	var mergedContent string
	var mergedTags []string
	var sources []string
	tagSet := make(map[string]bool)

	switch strategy {
	case "append":
		mergedContent = m.mergeAppend(entries)
	case "dedup":
		mergedContent = m.mergeDedup(entries)
	case "summarize":
		mergedContent = m.mergeSummarize(entries)
	default:
		mergedContent = m.mergeAppend(entries)
	}

	for _, e := range entries {
		for _, t := range e.Tags {
			if !tagSet[t] {
				tagSet[t] = true
				mergedTags = append(mergedTags, t)
			}
		}
		if e.Source != "" {
			sources = append(sources, e.Source)
		}
	}

	summary := fmt.Sprintf("Merge of %d entries (%s)", len(entries), strategy)

	merged := &types.ContextEntry{
		ID:        db.NewID(),
		Content:   mergedContent,
		Summary:   summary,
		Source:    fmt.Sprintf("merge:%s", strings.Join(uniqueStrings(sources), "+")),
		Tags:      mergedTags,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := m.db.SaveContextEntry(merged); err != nil {
		return nil, fmt.Errorf("save merged entry: %w", err)
	}

	merge := &types.ContextMerge{
		ID:        db.NewID(),
		Name:      name,
		ParentIDs: entryIDs,
		ResultID:  merged.ID,
		Strategy:  strategy,
		CreatedAt: time.Now(),
	}

	if err := m.db.SaveMerge(merge); err != nil {
		return nil, fmt.Errorf("save merge record: %w", err)
	}

	return merged, nil
}

func (m *MergeEngine) mergeAppend(entries []types.ContextEntry) string {
	var parts []string
	for i, e := range entries {
		parts = append(parts, fmt.Sprintf("--- Entry %d: %s ---\n%s", i+1, e.Summary, e.Content))
	}
	return strings.Join(parts, "\n\n")
}

func (m *MergeEngine) mergeDedup(entries []types.ContextEntry) string {
	seen := make(map[string]bool)
	var parts []string
	for i, e := range entries {
		normalized := strings.TrimSpace(e.Content)
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		parts = append(parts, fmt.Sprintf("--- Entry %d: %s ---\n%s", i+1, e.Summary, e.Content))
	}
	return strings.Join(parts, "\n\n")
}

func (m *MergeEngine) mergeSummarize(entries []types.ContextEntry) string {
	var parts []string
	for i, e := range entries {
		if e.Summary != "" {
			parts = append(parts, fmt.Sprintf("• %s", e.Summary))
		} else {
			lines := strings.Split(strings.TrimSpace(e.Content), "\n")
			summaryLine := lines[0]
			if len(summaryLine) > 200 {
				summaryLine = summaryLine[:200] + "..."
			}
			parts = append(parts, fmt.Sprintf("• (%d) %s", i+1, summaryLine))
		}
	}
	return strings.Join(parts, "\n")
}

func (m *MergeEngine) ListMerges() ([]types.ContextMerge, error) {
	return m.db.ListMerges()
}

func uniqueStrings(s []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}
