package context

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/agent-sync/agent-sync/internal/db"
	"github.com/agent-sync/agent-sync/pkg/types"
	"github.com/google/uuid"
)

var (
	decisionRe = regexp.MustCompile(`(?i)(we\s+)?(decided|chose|elect(ed|ing)?|settled|opted|going\s+with|let'?s\s+use|let'?s\s+go\s+with|pick(ed|ing)?|selected)\s+(.{10,200})`)
	factRe     = regexp.MustCompile(`(?i)(uses|runs?\s+on|depends?\s+on|built\s+(with|using)|powered\s+by|based\s+on|written\s+in|configured?\s+(with|as)|deployed?\s+(to|on|with)|migrated?\s+(to|from))\s+(.{10,200})`)
	prefRe     = regexp.MustCompile(`(?i)(i\s+|we\s+)?(prefer|like|rather\s+use|recommend|suggest(ed|ing)?|think\s+we\s+should|believe)\s+(.{10,200})`)
	goalRe     = regexp.MustCompile(`(?i)(TODO|FIXME|HACK|XXX|NOTE|next\s+step|next\s+task|we\s+need\s+to|we\s+should|goal\s+is\s+to|aim\s+is\s+to|plan\s+is\s+to|objective)\s*[::\-]?\s*(.{10,200})`)
	codeBlockRe = regexp.MustCompile("```(\\w+)?\n([\\s\\S]*?)```")
	pascalCase  = regexp.MustCompile(`[A-Z][a-z]+(?:[A-Z][a-z]+)+`)
	snakeCase   = regexp.MustCompile(`[a-z]+(?:_[a-z]+)+`)
)

type Extractor struct {
	db *db.DB
}

func NewExtractor(database *db.DB) *Extractor {
	return &Extractor{db: database}
}

type ExtractResult struct {
	Entities     []types.Entity
	Relations    []types.EntityRelation
	Conflicts    []types.EntityRelation
}

func (e *Extractor) ExtractFromMessages(session *types.Session, messages []types.Message) (*ExtractResult, error) {
	result := &ExtractResult{}
	seenEntities := make(map[string]bool)

	fullContent := ""
	for _, msg := range messages {
		if msg.Role == "assistant" || msg.Role == "user" {
			fullContent += msg.Content + "\n"
		}
	}

	decisions := e.extractDecisions(fullContent, session)
	for i := range decisions {
		key := decisions[i].Name + string(decisions[i].EntityType)
		if !seenEntities[key] {
			decisions[i].SessionID = session.ID
			decisions[i].Source = string(session.Provider)
			if err := e.db.SaveEntity(&decisions[i]); err == nil {
				result.Entities = append(result.Entities, decisions[i])
				seenEntities[key] = true
			}
		}
	}

	facts := e.extractFacts(fullContent, session)
	for i := range facts {
		key := facts[i].Name + string(facts[i].EntityType)
		if !seenEntities[key] {
			facts[i].SessionID = session.ID
			facts[i].Source = string(session.Provider)
			if err := e.db.SaveEntity(&facts[i]); err == nil {
				result.Entities = append(result.Entities, facts[i])
				seenEntities[key] = true
			}
		}
	}

	prefs := e.extractPreferences(fullContent, session)
	for i := range prefs {
		key := prefs[i].Name + string(prefs[i].EntityType)
		if !seenEntities[key] {
			prefs[i].SessionID = session.ID
			prefs[i].Source = string(session.Provider)
			if err := e.db.SaveEntity(&prefs[i]); err == nil {
				result.Entities = append(result.Entities, prefs[i])
				seenEntities[key] = true
			}
		}
	}

	goals := e.extractGoals(fullContent, session)
	for i := range goals {
		key := goals[i].Name + string(goals[i].EntityType)
		if !seenEntities[key] {
			goals[i].SessionID = session.ID
			goals[i].Source = string(session.Provider)
			if err := e.db.SaveEntity(&goals[i]); err == nil {
				result.Entities = append(result.Entities, goals[i])
				seenEntities[key] = true
			}
		}
	}

	codePats := e.extractCodePatterns(fullContent, session)
	for i := range codePats {
		key := codePats[i].Name + string(codePats[i].EntityType)
		if !seenEntities[key] {
			codePats[i].SessionID = session.ID
			codePats[i].Source = string(session.Provider)
			if err := e.db.SaveEntity(&codePats[i]); err == nil {
				result.Entities = append(result.Entities, codePats[i])
				seenEntities[key] = true
			}
		}
	}

	rels := e.buildCoOccurrenceEdges(result.Entities)
	for i := range rels {
		if err := e.db.SaveEntityRelation(&rels[i]); err == nil {
			result.Relations = append(result.Relations, rels[i])
		}
	}

	conflicts := e.detectConflicts(result.Entities)
	for i := range conflicts {
		if err := e.db.SaveEntityRelation(&conflicts[i]); err == nil {
			result.Conflicts = append(result.Conflicts, conflicts[i])
		}
	}

	return result, nil
}

func (e *Extractor) extractDecisions(content string, session *types.Session) []types.Entity {
	matches := decisionRe.FindAllStringSubmatch(content, -1)
	var entities []types.Entity
	for _, m := range matches {
		text := strings.TrimSpace(m[len(m)-1])
		if len(text) < 10 || len(text) > 300 {
			continue
		}
		name := toEntityName(text)
		entities = append(entities, types.Entity{
			ID:         uuid.New().String(),
			Name:       name,
			EntityType: types.EntityDecision,
			Summary:    truncate(text, 200),
			Content:    text,
			Confidence: 0.75,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		})
	}
	return entities
}

func (e *Extractor) extractFacts(content string, session *types.Session) []types.Entity {
	matches := factRe.FindAllStringSubmatch(content, -1)
	var entities []types.Entity
	for _, m := range matches {
		text := strings.TrimSpace(m[len(m)-1])
		if len(text) < 10 || len(text) > 300 {
			continue
		}
		name := toEntityName(text)
		entities = append(entities, types.Entity{
			ID:         uuid.New().String(),
			Name:       name,
			EntityType: types.EntityFact,
			Summary:    truncate(text, 200),
			Content:    text,
			Confidence: 0.7,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		})
	}
	return entities
}

func (e *Extractor) extractPreferences(content string, session *types.Session) []types.Entity {
	matches := prefRe.FindAllStringSubmatch(content, -1)
	var entities []types.Entity
	for _, m := range matches {
		text := strings.TrimSpace(m[len(m)-1])
		if len(text) < 10 || len(text) > 300 {
			continue
		}
		name := toEntityName(text)
		entities = append(entities, types.Entity{
			ID:         uuid.New().String(),
			Name:       name,
			EntityType: types.EntityPreference,
			Summary:    truncate(text, 200),
			Content:    text,
			Confidence: 0.6,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		})
	}
	return entities
}

func (e *Extractor) extractGoals(content string, session *types.Session) []types.Entity {
	matches := goalRe.FindAllStringSubmatch(content, -1)
	var entities []types.Entity
	for _, m := range matches {
		text := strings.TrimSpace(m[len(m)-1])
		if len(text) < 10 || len(text) > 300 {
			continue
		}
		name := toEntityName(text)
		entities = append(entities, types.Entity{
			ID:         uuid.New().String(),
			Name:       name,
			EntityType: types.EntityGoal,
			Summary:    truncate(text, 200),
			Content:    text,
			Confidence: 0.8,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		})
	}
	return entities
}

func (e *Extractor) extractCodePatterns(content string, session *types.Session) []types.Entity {
	matches := codeBlockRe.FindAllStringSubmatch(content, -1)
	var entities []types.Entity
	seen := make(map[string]bool)

	for _, m := range matches {
		lang := m[1]
		code := strings.TrimSpace(m[2])
		if len(code) < 20 || len(code) > 2000 || code == "" {
			continue
		}

		idents := extractIdentifiers(code)
		for _, ident := range idents {
			if seen[ident] {
				continue
			}
			seen[ident] = true
			entities = append(entities, types.Entity{
				ID:         uuid.New().String(),
				Name:       ident,
				EntityType: types.EntityCode,
				Summary:    fmt.Sprintf("Code pattern: %s in %s", ident, lang),
				Content:    code[:min(len(code), 500)],
				Confidence: 0.65,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			})
		}

		if lang != "" {
			langEntity := fmt.Sprintf("language:%s", lang)
			if !seen[langEntity] {
				seen[langEntity] = true
				entities = append(entities, types.Entity{
					ID:         uuid.New().String(),
					Name:       langEntity,
					EntityType: types.EntityFact,
					Summary:    fmt.Sprintf("Uses %s", lang),
					Content:    fmt.Sprintf("Code written in %s", lang),
					Confidence: 0.9,
					CreatedAt:  time.Now(),
					UpdatedAt:  time.Now(),
				})
			}
		}
	}
	return entities
}

func (e *Extractor) buildCoOccurrenceEdges(entities []types.Entity) []types.EntityRelation {
	var relations []types.EntityRelation
	for i := 0; i < len(entities); i++ {
		for j := i + 1; j < len(entities); j++ {
			wordOverlap := countWordOverlap(entities[i].Content, entities[j].Content)
			if wordOverlap > 3 {
				relations = append(relations, types.EntityRelation{
					ID:             uuid.New().String(),
					SourceEntityID: entities[i].ID,
					TargetEntityID: entities[j].ID,
					RelationType:   "related",
					Weight:         float64(wordOverlap) / 10.0,
					Evidence:       fmt.Sprintf("%s <-> %s", entities[i].Name, entities[j].Name),
				})
			}
		}
	}
	return relations
}

func (e *Extractor) detectConflicts(entities []types.Entity) []types.EntityRelation {
	var conflicts []types.EntityRelation
	byName := make(map[string][]types.Entity)
	for _, ent := range entities {
		byName[ent.Name] = append(byName[ent.Name], ent)
	}

	for name, ents := range byName {
		if len(ents) < 2 {
			continue
		}
		for i := 0; i < len(ents); i++ {
			for j := i + 1; j < len(ents); j++ {
				overlap := wordOverlapRatio(ents[i].Summary, ents[j].Summary)
				if overlap < 0.4 && ents[i].EntityType == ents[j].EntityType {
					conflicts = append(conflicts, types.EntityRelation{
						ID:             uuid.New().String(),
						SourceEntityID: ents[i].ID,
						TargetEntityID: ents[j].ID,
						RelationType:   "contradicts",
						Weight:         1.0 - overlap,
						Evidence:       fmt.Sprintf("%s: %q vs %q", name, ents[i].Summary, ents[j].Summary),
					})
				}
			}
		}
	}
	return conflicts
}

func toEntityName(text string) string {
	lower := strings.ToLower(text)
	lower = strings.TrimSpace(lower)
	if len(lower) > 80 {
		words := strings.Fields(lower)
		if len(words) > 8 {
			words = words[:8]
		}
		lower = strings.Join(words, " ")
	}
	replacer := strings.NewReplacer(
		"'", "", "\"", "", "`", "",
		",", "", ".", "", "!", "", "?", "",
	)
	return replacer.Replace(lower)
}

func extractIdentifiers(code string) []string {
	var idents []string
	seen := make(map[string]bool)

	for _, m := range pascalCase.FindAllString(code, -1) {
		if len(m) > 3 && !seen[m] {
			seen[m] = true
			idents = append(idents, m)
		}
	}
	for _, m := range snakeCase.FindAllString(code, -1) {
		if len(m) > 3 && !seen[m] {
			seen[m] = true
			idents = append(idents, m)
		}
	}
	if len(idents) > 10 {
		idents = idents[:10]
	}
	return idents
}

func countWordOverlap(a, b string) int {
	wordsA := wordSet(a)
	wordsB := wordSet(b)
	count := 0
	for w := range wordsA {
		if wordsB[w] {
			count++
		}
	}
	return count
}

func wordOverlapRatio(a, b string) float64 {
	wordsA := wordSet(a)
	wordsB := wordSet(b)
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0
	}
	intersection := 0
	for w := range wordsA {
		if wordsB[w] {
			intersection++
		}
	}
	union := len(wordsA) + len(wordsB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func wordSet(s string) map[string]bool {
	words := make(map[string]bool)
	for _, w := range strings.Fields(strings.ToLower(s)) {
		w = strings.Trim(w, ".,!?;:\"'()[]{}")
		if len(w) > 2 {
			words[w] = true
		}
	}
	return words
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
