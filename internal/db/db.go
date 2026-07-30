package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/agent-sync/agent-sync/pkg/types"
	"github.com/google/uuid"
)

type DB struct {
	*sql.DB
}

func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?cache=shared&_journal_mode=WAL", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}

	wrapper := &DB{db}
	if err := wrapper.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return wrapper, nil
}

func (db *DB) migrate() error {
	if _, err := db.Exec(SchemaSQL); err != nil {
		return err
	}
	// SchemaSQL creates complete tables for new installs. These additions keep
	// databases made by earlier Dragon Sync builds forward-compatible.
	for _, statement := range []string{
		`ALTER TABLE chat_agent_sessions ADD COLUMN runtime_kind TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE chat_agent_sessions ADD COLUMN runtime_status TEXT NOT NULL DEFAULT 'stopped'`,
		`ALTER TABLE chat_agent_sessions ADD COLUMN capabilities_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE chat_agent_sessions ADD COLUMN commands_json TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE chat_agent_sessions ADD COLUMN config_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE chat_agent_sessions ADD COLUMN last_error TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE chat_messages ADD COLUMN source TEXT NOT NULL DEFAULT 'dragon'`,
		`ALTER TABLE chat_messages ADD COLUMN native_message_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE chat_agent_metrics ADD COLUMN context_used INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := db.Exec(statement); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_messages_native ON chat_messages(chat_id, agent, native_message_id) WHERE native_message_id != ''`); err != nil {
		return err
	}
	db.Exec(`DELETE FROM providers WHERE rowid NOT IN (SELECT MIN(rowid) FROM providers GROUP BY type)`)
	db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_providers_type ON providers(type)`)
	return nil
}

func (db *DB) UpsertProvider(p *types.Provider) error {
	_, err := db.Exec(`INSERT INTO providers (id, name, type, path, config, enabled, last_sync, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, path=excluded.path, config=excluded.config,
			enabled=excluded.enabled, last_sync=excluded.last_sync,
			updated_at=datetime('now')`,
		p.ID, p.Name, string(p.Type), p.Path, p.Config, boolToInt(p.Enabled), nullTime(p.LastSync))
	return err
}

func (db *DB) ListProviders() ([]types.Provider, error) {
	rows, err := db.Query(`SELECT id, name, type, path, config, enabled, last_sync, created_at, updated_at FROM providers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []types.Provider
	for rows.Next() {
		var p types.Provider
		var enabled int
		var lastSync, ca, ua sql.NullString
		if err := rows.Scan(&p.ID, &p.Name, &p.Type, &p.Path, &p.Config, &enabled, &lastSync, &ca, &ua); err != nil {
			return nil, err
		}
		p.Enabled = enabled > 0
		if lastSync.Valid {
			t, _ := time.Parse("2006-01-02 15:04:05", lastSync.String)
			p.LastSync = &t
		}
		if ca.Valid {
			p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca.String)
		}
		if ua.Valid {
			p.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", ua.String)
		}
		providers = append(providers, p)
	}
	if providers == nil {
		providers = []types.Provider{}
	}
	return providers, nil
}

func (db *DB) GetProvider(id string) (*types.Provider, error) {
	var p types.Provider
	var enabled int
	var lastSync, ca, ua sql.NullString
	err := db.QueryRow(`SELECT id, name, type, path, config, enabled, last_sync, created_at, updated_at FROM providers WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.Type, &p.Path, &p.Config, &enabled, &lastSync, &ca, &ua)
	if err != nil {
		return nil, err
	}
	p.Enabled = enabled > 0
	if lastSync.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", lastSync.String)
		p.LastSync = &t
	}
	if ca.Valid {
		p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca.String)
	}
	if ua.Valid {
		p.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", ua.String)
	}
	return &p, nil
}

func (db *DB) GetProviderByType(pt string) (*types.Provider, error) {
	var p types.Provider
	var enabled int
	var lastSync, ca, ua sql.NullString
	err := db.QueryRow(`SELECT id, name, type, path, config, enabled, last_sync, created_at, updated_at FROM providers WHERE type = ? ORDER BY created_at DESC LIMIT 1`, pt).
		Scan(&p.ID, &p.Name, &p.Type, &p.Path, &p.Config, &enabled, &lastSync, &ca, &ua)
	if err != nil {
		return nil, err
	}
	p.Enabled = enabled > 0
	if lastSync.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", lastSync.String)
		p.LastSync = &t
	}
	if ca.Valid {
		p.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca.String)
	}
	if ua.Valid {
		p.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", ua.String)
	}
	return &p, nil
}

func (db *DB) UpsertSession(s *types.Session) error {
	_, err := db.Exec(`INSERT INTO sessions (id, provider_id, provider, title, model, workspace, project_dir, started_at, ended_at, token_count, message_count, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
		ON CONFLICT(id) DO UPDATE SET
			title=excluded.title, token_count=excluded.token_count,
			message_count=excluded.message_count, ended_at=excluded.ended_at,
			metadata=excluded.metadata, updated_at=datetime('now')`,
		s.ID, s.ProviderID, string(s.Provider), s.Title, s.Model, s.Workspace, s.ProjectDir,
		s.StartedAt.Format("2006-01-02 15:04:05"), nullTime(s.EndedAt),
		s.TokenCount, s.MessageCount, s.Metadata)
	return err
}

func (db *DB) InsertMessage(m *types.Message) error {
	_, err := db.Exec(`INSERT INTO messages (id, session_id, parent_id, role, content, token_count, tool_calls, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.SessionID, m.ParentID, m.Role, m.Content, m.TokenCount, m.ToolCalls, m.Metadata, m.CreatedAt.Format("2006-01-02 15:04:05"))
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO messages_fts(rowid, content) VALUES (last_insert_rowid(), ?)`, m.Content)
	return err
}

func (db *DB) ListSessions(provider string, limit, offset int) ([]types.Session, error) {
	query := `SELECT id, provider_id, provider, title, model, workspace, project_dir, started_at, ended_at, token_count, message_count, metadata, created_at, updated_at FROM sessions`
	args := []interface{}{}

	if provider != "" {
		query += " WHERE provider = ?"
		args = append(args, provider)
	}
	query += " ORDER BY started_at DESC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	if offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", offset)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []types.Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	if sessions == nil {
		sessions = []types.Session{}
	}
	return sessions, nil
}

func (db *DB) GetSession(id string) (*types.Session, error) {
	var s types.Session
	var startedAt, endedAt, ca, ua sql.NullString
	err := db.QueryRow(`SELECT id, provider_id, provider, title, model, workspace, project_dir, started_at, ended_at, token_count, message_count, metadata, created_at, updated_at FROM sessions WHERE id = ?`, id).
		Scan(&s.ID, &s.ProviderID, &s.Provider, &s.Title, &s.Model, &s.Workspace, &s.ProjectDir, &startedAt, &endedAt, &s.TokenCount, &s.MessageCount, &s.Metadata, &ca, &ua)
	if err != nil {
		return nil, err
	}
	if startedAt.Valid {
		s.StartedAt, _ = time.Parse("2006-01-02 15:04:05", startedAt.String)
	}
	if endedAt.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", endedAt.String)
		s.EndedAt = &t
	}
	if ca.Valid {
		s.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca.String)
	}
	if ua.Valid {
		s.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", ua.String)
	}
	return &s, nil
}

func (db *DB) GetSessionMessages(sessionID string) ([]types.Message, error) {
	rows, err := db.Query(`SELECT id, session_id, parent_id, role, content, token_count, tool_calls, metadata, created_at FROM messages WHERE session_id = ? ORDER BY created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []types.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	if messages == nil {
		messages = []types.Message{}
	}
	return messages, nil
}

func scanMessage(rows *sql.Rows) (types.Message, error) {
	var m types.Message
	var ca string
	if err := rows.Scan(&m.ID, &m.SessionID, &m.ParentID, &m.Role, &m.Content, &m.TokenCount, &m.ToolCalls, &m.Metadata, &ca); err != nil {
		return m, err
	}
	m.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca)
	return m, nil
}

func (db *DB) SearchMessages(query string, limit int) ([]types.Message, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Query(`SELECT m.id, m.session_id, m.parent_id, m.role, m.content, m.token_count, m.tool_calls, m.metadata, m.created_at
		FROM messages_fts f JOIN messages m ON f.rowid = m.rowid
		WHERE messages_fts MATCH ? ORDER BY rank LIMIT ?`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []types.Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	if messages == nil {
		messages = []types.Message{}
	}
	return messages, nil
}

func (db *DB) SaveContextEntry(e *types.ContextEntry) error {
	tags, _ := json.Marshal(e.Tags)
	_, err := db.Exec(`INSERT INTO context_entries (id, content, summary, source, source_id, tags, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			content=excluded.content, summary=excluded.summary,
			tags=excluded.tags, updated_at=excluded.updated_at`,
		e.ID, e.Content, e.Summary, e.Source, e.SourceID, string(tags),
		e.CreatedAt.Format("2006-01-02 15:04:05"), e.UpdatedAt.Format("2006-01-02 15:04:05"))
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO context_fts(rowid, content, summary) VALUES (last_insert_rowid(), ?, ?)`, e.Content, e.Summary)
	return err
}

func scanContextEntry(rows *sql.Rows) (types.ContextEntry, error) {
	var e types.ContextEntry
	var tags, ca, ua string
	if err := rows.Scan(&e.ID, &e.Content, &e.Summary, &e.Source, &e.SourceID, &tags, &ca, &ua); err != nil {
		return e, err
	}
	e.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca)
	e.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", ua)
	json.Unmarshal([]byte(tags), &e.Tags)
	return e, nil
}

func scanSession(rows *sql.Rows) (types.Session, error) {
	var s types.Session
	var startedAt, endedAt, ca, ua sql.NullString
	if err := rows.Scan(&s.ID, &s.ProviderID, &s.Provider, &s.Title, &s.Model, &s.Workspace, &s.ProjectDir, &startedAt, &endedAt, &s.TokenCount, &s.MessageCount, &s.Metadata, &ca, &ua); err != nil {
		return s, err
	}
	if startedAt.Valid {
		s.StartedAt, _ = time.Parse("2006-01-02 15:04:05", startedAt.String)
	}
	if endedAt.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", endedAt.String)
		s.EndedAt = &t
	}
	return s, nil
}

func (db *DB) SearchContext(query string, limit int) ([]types.ContextEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Query(`SELECT c.id, c.content, c.summary, c.source, c.source_id, c.tags, c.created_at, c.updated_at
		FROM context_fts f JOIN context_entries c ON f.rowid = c.rowid
		WHERE context_fts MATCH ? ORDER BY rank LIMIT ?`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []types.ContextEntry
	for rows.Next() {
		e, err := scanContextEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []types.ContextEntry{}
	}
	return entries, nil
}

func (db *DB) ListContextEntries(limit, offset int) ([]types.ContextEntry, error) {
	rows, err := db.Query(`SELECT id, content, summary, source, source_id, tags, created_at, updated_at FROM context_entries ORDER BY updated_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []types.ContextEntry
	for rows.Next() {
		e, err := scanContextEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []types.ContextEntry{}
	}
	return entries, nil
}

func (db *DB) DeleteContextEntry(id string) error {
	_, err := db.Exec(`DELETE FROM context_entries WHERE id = ?`, id)
	return err
}

func (db *DB) GetConfig(key string) (string, error) {
	var val string
	err := db.QueryRow(`SELECT value FROM config WHERE key = ?`, key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

func (db *DB) SetConfig(key, value string) error {
	_, err := db.Exec(`INSERT INTO config (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (db *DB) SaveEntity(e *types.Entity) error {
	_, err := db.Exec(`INSERT INTO entities (id, name, entity_type, summary, content, source, source_id, session_id, confidence, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, entity_type=excluded.entity_type, summary=excluded.summary,
			content=excluded.content, confidence=excluded.confidence, updated_at=excluded.updated_at`,
		e.ID, e.Name, string(e.EntityType), e.Summary, e.Content, e.Source, e.SourceID,
		e.SessionID, e.Confidence, e.CreatedAt.Format("2006-01-02 15:04:05"), e.UpdatedAt.Format("2006-01-02 15:04:05"))
	return err
}

// FindEntitiesByNameType returns entities with the same canonical name and type (any session/source).
func (db *DB) FindEntitiesByNameType(name, entityType string) ([]types.Entity, error) {
	rows, err := db.Query(`SELECT id, name, entity_type, summary, content, source, source_id, session_id, confidence, created_at, updated_at
		FROM entities WHERE name = ? AND entity_type = ? ORDER BY created_at ASC`, name, entityType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entities []types.Entity
	for rows.Next() {
		e, err := scanEntity(rows)
		if err != nil {
			return nil, err
		}
		entities = append(entities, e)
	}
	if entities == nil {
		entities = []types.Entity{}
	}
	return entities, nil
}

func (db *DB) GetEntity(id string) (*types.Entity, error) {
	rows, err := db.Query(`SELECT id, name, entity_type, summary, content, source, source_id, session_id, confidence, created_at, updated_at
		FROM entities WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	e, err := scanEntity(rows)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// HasConflictPair reports whether a contradicts relation already exists between two entities.
func (db *DB) HasConflictPair(a, b string) bool {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM entity_relations
		WHERE relation_type = 'contradicts'
		  AND ((source_entity_id = ? AND target_entity_id = ?)
		    OR (source_entity_id = ? AND target_entity_id = ?))`, a, b, b, a).Scan(&n)
	return err == nil && n > 0
}

func (db *DB) ListEntities(entityType string, limit, offset int) ([]types.Entity, error) {
	query := `SELECT id, name, entity_type, summary, content, source, source_id, session_id, confidence, created_at, updated_at FROM entities`
	args := []interface{}{}
	if entityType != "" {
		query += " WHERE entity_type = ?"
		args = append(args, entityType)
	}
	query += " ORDER BY updated_at DESC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	if offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", offset)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entities []types.Entity
	for rows.Next() {
		e, err := scanEntity(rows)
		if err != nil {
			return nil, err
		}
		entities = append(entities, e)
	}
	if entities == nil {
		entities = []types.Entity{}
	}
	return entities, nil
}

func (db *DB) SearchEntities(query string, limit int) ([]types.Entity, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Query(`SELECT id, name, entity_type, summary, content, source, source_id, session_id, confidence, created_at, updated_at
		FROM entities WHERE name LIKE ? OR summary LIKE ? OR content LIKE ?
		ORDER BY confidence DESC LIMIT ?`, "%"+query+"%", "%"+query+"%", "%"+query+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entities []types.Entity
	for rows.Next() {
		e, err := scanEntity(rows)
		if err != nil {
			return nil, err
		}
		entities = append(entities, e)
	}
	if entities == nil {
		entities = []types.Entity{}
	}
	return entities, nil
}

func scanEntity(rows *sql.Rows) (types.Entity, error) {
	var e types.Entity
	var eType, ca, ua string
	if err := rows.Scan(&e.ID, &e.Name, &eType, &e.Summary, &e.Content, &e.Source, &e.SourceID, &e.SessionID, &e.Confidence, &ca, &ua); err != nil {
		return e, err
	}
	e.EntityType = types.EntityType(eType)
	e.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca)
	e.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", ua)
	return e, nil
}

func (db *DB) SaveEntityRelation(r *types.EntityRelation) error {
	_, err := db.Exec(`INSERT INTO entity_relations (id, source_entity_id, target_entity_id, relation_type, weight, evidence, created_at)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(id) DO UPDATE SET
			relation_type=excluded.relation_type, weight=excluded.weight, evidence=excluded.evidence`,
		r.ID, r.SourceEntityID, r.TargetEntityID, r.RelationType, r.Weight, r.Evidence)
	return err
}

func (db *DB) ListEntityRelations(entityID string) ([]types.EntityRelation, error) {
	rows, err := db.Query(`SELECT id, source_entity_id, target_entity_id, relation_type, weight, evidence, created_at
		FROM entity_relations WHERE source_entity_id = ? OR target_entity_id = ? ORDER BY weight DESC`, entityID, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var relations []types.EntityRelation
	for rows.Next() {
		var r types.EntityRelation
		var ca string
		if err := rows.Scan(&r.ID, &r.SourceEntityID, &r.TargetEntityID, &r.RelationType, &r.Weight, &r.Evidence, &ca); err != nil {
			return nil, err
		}
		r.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca)
		relations = append(relations, r)
	}
	if relations == nil {
		relations = []types.EntityRelation{}
	}
	return relations, nil
}

func (db *DB) ListConflicts() ([]types.EntityRelation, error) {
	rows, err := db.Query(`SELECT id, source_entity_id, target_entity_id, relation_type, weight, evidence, created_at
		FROM entity_relations WHERE relation_type = 'contradicts' ORDER BY weight DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var relations []types.EntityRelation
	for rows.Next() {
		var r types.EntityRelation
		var ca string
		if err := rows.Scan(&r.ID, &r.SourceEntityID, &r.TargetEntityID, &r.RelationType, &r.Weight, &r.Evidence, &ca); err != nil {
			return nil, err
		}
		r.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca)
		relations = append(relations, r)
	}
	if relations == nil {
		relations = []types.EntityRelation{}
	}
	return relations, nil
}

func (db *DB) SaveSnapshot(s *types.Snapshot) error {
	eIDs, _ := json.Marshal(s.EntityIDs)
	_, err := db.Exec(`INSERT INTO snapshots (id, name, description, entity_ids, created_at)
		VALUES (?, ?, ?, ?, datetime('now'))
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, description=excluded.description, entity_ids=excluded.entity_ids`, s.ID, s.Name, s.Description, string(eIDs))
	return err
}

func (db *DB) ListSnapshots() ([]types.Snapshot, error) {
	rows, err := db.Query(`SELECT id, name, description, entity_ids, created_at FROM snapshots ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []types.Snapshot
	for rows.Next() {
		var s types.Snapshot
		var eIDs, ca string
		if err := rows.Scan(&s.ID, &s.Name, &s.Description, &eIDs, &ca); err != nil {
			return nil, err
		}
		s.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca)
		json.Unmarshal([]byte(eIDs), &s.EntityIDs)
		snapshots = append(snapshots, s)
	}
	if snapshots == nil {
		snapshots = []types.Snapshot{}
	}
	return snapshots, nil
}

func (db *DB) DeleteEntity(id string) error {
	_, err := db.Exec(`DELETE FROM entities WHERE id = ?`, id)
	return err
}

func (db *DB) GetStats() map[string]interface{} {
	stats := make(map[string]interface{})
	var v int64
	db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&v)
	stats["total_sessions"] = v
	db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&v)
	stats["total_messages"] = v
	db.QueryRow(`SELECT COUNT(*) FROM context_entries`).Scan(&v)
	stats["total_context_entries"] = v
	db.QueryRow(`SELECT COUNT(*) FROM providers`).Scan(&v)
	stats["total_providers"] = v
	db.QueryRow(`SELECT COUNT(DISTINCT provider) FROM sessions`).Scan(&v)
	stats["active_providers"] = v
	db.QueryRow(`SELECT COALESCE(SUM(token_count), 0) FROM messages`).Scan(&v)
	stats["total_tokens"] = v
	db.QueryRow(`SELECT COUNT(*) FROM entities`).Scan(&v)
	stats["total_entities"] = v
	db.QueryRow(`SELECT COUNT(*) FROM entity_relations WHERE relation_type = 'contradicts'`).Scan(&v)
	stats["total_conflicts"] = v
	return stats
}

func (db *DB) SaveMerge(m *types.ContextMerge) error {
	pIDs, _ := json.Marshal(m.ParentIDs)
	_, err := db.Exec(`INSERT INTO context_merges (id, name, parent_ids, result_id, strategy, created_at)
		VALUES (?, ?, ?, ?, ?, datetime('now'))`, m.ID, m.Name, string(pIDs), m.ResultID, m.Strategy)
	return err
}

func (db *DB) ListMerges() ([]types.ContextMerge, error) {
	rows, err := db.Query(`SELECT id, name, parent_ids, result_id, strategy, created_at FROM context_merges ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var merges []types.ContextMerge
	for rows.Next() {
		var m types.ContextMerge
		var pIDs, ca string
		if err := rows.Scan(&m.ID, &m.Name, &pIDs, &m.ResultID, &m.Strategy, &ca); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", ca)
		json.Unmarshal([]byte(pIDs), &m.ParentIDs)
		merges = append(merges, m)
	}
	if merges == nil {
		merges = []types.ContextMerge{}
	}
	return merges, nil
}

func NewID() string {
	return uuid.New().String()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.Format("2006-01-02 15:04:05")
}
