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

func TestNewDecisionStore_ExistingDrillRunsRemainReadableAfterMigration(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "decisions.db")
	seedOldSchema(t, dbPath)
	seedLegacyDrillRun(t, dbPath)

	store, err := NewDecisionStore(dbPath)
	if err != nil {
		t.Fatalf("NewDecisionStore() failed: %v", err)
	}
	defer store.Close()

	run, err := store.GetDrillRun("legacy-run-1")
	if err != nil {
		t.Fatalf("GetDrillRun() failed: %v", err)
	}
	if run == nil {
		t.Fatalf("expected migrated legacy drill run to be readable")
	}
	if run.ScenarioID != "" || run.ValidationStatus != "" {
		t.Fatalf("expected unset migrated validation metadata, got scenarioId=%q validationStatus=%q", run.ScenarioID, run.ValidationStatus)
	}
	if run.RollbackVerifiedAt != nil || run.BannerVerified != nil {
		t.Fatalf("expected nullable migrated metadata to remain nil, got rollbackVerifiedAt=%v bannerVerified=%v", run.RollbackVerifiedAt, run.BannerVerified)
	}
	if string(run.Config) != `{"mode":"legacy"}` {
		t.Fatalf("expected legacy config to remain readable, got %s", string(run.Config))
	}
	if run.Verdict != "success" {
		t.Fatalf("expected verdict to remain readable, got %q", run.Verdict)
	}

	runs, err := store.ListDrillRuns(10)
	if err != nil {
		t.Fatalf("ListDrillRuns() failed: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected one legacy drill run in list, got %d", len(runs))
	}
	if runs[0].ID != "legacy-run-1" {
		t.Fatalf("expected listed legacy run id to match, got %q", runs[0].ID)
	}
	if runs[0].RollbackVerifiedAt != nil || runs[0].BannerVerified != nil {
		t.Fatalf("expected nil nullable metadata in listed legacy run, got rollbackVerifiedAt=%v bannerVerified=%v", runs[0].RollbackVerifiedAt, runs[0].BannerVerified)
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

func seedLegacyDrillRun(t *testing.T, dbPath string) {
	t.Helper()

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()

	const query = `
		INSERT INTO drill_runs (
			id, type, target, status, start_time, end_time, config, pre_snapshot, post_snapshot, verdict, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err = db.Exec(
		query,
		"legacy-run-1",
		"network",
		"payments-service",
		"completed",
		"2026-01-10T00:00:00Z",
		"2026-01-10T00:02:00Z",
		`{"mode":"legacy"}`,
		`{"before":"ok"}`,
		`{"after":"ok"}`,
		"success",
		"2026-01-10T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("seed legacy drill run: %v", err)
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
