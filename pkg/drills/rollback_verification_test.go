package drills

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"predictive-analysis-engine/pkg/storage"
)

func TestRecordRollbackVerificationPersistsTimestampAndSource(t *testing.T) {
	store := newRollbackMetadataDecisionStore(t)
	engine := &Engine{store: store}

	run := storage.DrillRun{
		ID:        "run-rollback-metadata",
		Type:      "UnsupportedType",
		Target:    "default/checkoutservice",
		Status:    StatusAwaitingRecovery,
		StartTime: "2026-03-07T10:00:00Z",
		Config:    json.RawMessage(`{"namespace":"default"}`),
		Verdict:   "Success",
	}
	if err := store.InsertDrillRun(run); err != nil {
		t.Fatalf("InsertDrillRun() failed: %v", err)
	}

	if err := engine.recordRollbackVerification(&run, "manual"); err != nil {
		t.Fatalf("recordRollbackVerification() failed: %v", err)
	}

	persisted, err := store.GetDrillRun(run.ID)
	if err != nil {
		t.Fatalf("GetDrillRun() failed: %v", err)
	}
	if persisted == nil {
		t.Fatalf("expected persisted drill run")
	}
	if persisted.RollbackVerifiedAt == nil {
		t.Fatalf("expected rollbackVerifiedAt to be persisted")
	}
	if _, err := time.Parse(time.RFC3339, *persisted.RollbackVerifiedAt); err != nil {
		t.Fatalf("expected rollbackVerifiedAt RFC3339 timestamp, got %q", *persisted.RollbackVerifiedAt)
	}
	if persisted.RollbackVerificationSource != "manual" {
		t.Fatalf("expected rollbackVerificationSource manual, got %q", persisted.RollbackVerificationSource)
	}
}

func newRollbackMetadataDecisionStore(t *testing.T) *storage.DecisionStore {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "rollback-metadata-tests.db")
	store, err := storage.NewDecisionStore(dbPath)
	if err != nil {
		t.Fatalf("NewDecisionStore() failed: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}
