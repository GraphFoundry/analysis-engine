package storage

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestNewDecisionStore_MigratesDrillRunValidationColumns(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "decisions.db")
	seedOldSchema(t, dbPath)

	store, err := NewDecisionStore(dbPath)
	if err != nil {
		t.Fatalf("NewDecisionStore() failed: %v", err)
	}
	defer store.Close()

	columns := readTableColumns(t, store.db, "drill_runs")
	expected := []string{
		"scenario_id",
		"validation_status",
		"rollback_verified_at",
		"banner_verified",
	}
	for _, column := range expected {
		if _, ok := columns[column]; !ok {
			t.Fatalf("expected migrated column %q to exist, got columns: %#v", column, columns)
		}
	}
}

func seedOldSchema(t *testing.T, dbPath string) {
	t.Helper()

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()

	oldSchema := `
		CREATE TABLE drill_runs (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			target TEXT NOT NULL,
			status TEXT NOT NULL,
			start_time TEXT NOT NULL,
			end_time TEXT,
			config TEXT NOT NULL,
			pre_snapshot TEXT,
			post_snapshot TEXT,
			verdict TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		);
	`
	if _, err := db.Exec(oldSchema); err != nil {
		t.Fatalf("create old drill_runs schema: %v", err)
	}
}

func readTableColumns(t *testing.T, db *sql.DB, table string) map[string]struct{} {
	t.Helper()

	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("query table_info for %s: %v", table, err)
	}
	defer rows.Close()

	columns := make(map[string]struct{})
	for rows.Next() {
		var cid int
		var name string
		var colType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan table_info row: %v", err)
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table_info rows: %v", err)
	}

	return columns
}
