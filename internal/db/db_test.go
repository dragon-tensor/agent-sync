package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrationAddsBackgroundRuntimeColumnsToExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE chats (id TEXT PRIMARY KEY, title TEXT, project_dir TEXT, active_agent TEXT, created_at TEXT, updated_at TEXT)`,
		`CREATE TABLE chat_agent_sessions (
			id TEXT PRIMARY KEY, chat_id TEXT, agent TEXT, native_session_id TEXT DEFAULT '',
			last_delivered_sequence INTEGER DEFAULT 0, last_active_at TEXT, created_at TEXT, updated_at TEXT,
			UNIQUE(chat_id, agent)
		)`,
		`CREATE TABLE chat_messages (
			id TEXT PRIMARY KEY, chat_id TEXT, sequence INTEGER, role TEXT, content TEXT, agent TEXT,
			created_at TEXT, UNIQUE(chat_id, sequence)
		)`,
		`CREATE TABLE chat_agent_metrics (
			chat_id TEXT, agent TEXT, model TEXT DEFAULT '', effort TEXT DEFAULT '',
			input_tokens INTEGER DEFAULT 0, output_tokens INTEGER DEFAULT 0,
			context_window INTEGER DEFAULT 0, cost_usd REAL DEFAULT 0, updated_at TEXT,
			PRIMARY KEY(chat_id, agent)
		)`,
	} {
		if _, err := legacy.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	assertColumn(t, database.DB, "chat_agent_sessions", "runtime_kind")
	assertColumn(t, database.DB, "chat_agent_sessions", "commands_json")
	assertColumn(t, database.DB, "chat_messages", "native_message_id")
	assertColumn(t, database.DB, "chat_agent_metrics", "context_used")
	assertColumn(t, database.DB, "chat_message_queue", "status")
}

func assertColumn(t *testing.T, database *sql.DB, table, expected string) {
	t.Helper()
	rows, err := database.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == expected {
			return
		}
	}
	t.Fatalf("column %s.%s was not created", table, expected)
}
