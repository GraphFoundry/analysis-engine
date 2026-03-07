package drills

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"predictive-analysis-engine/pkg/storage"
)

func TestExecuteDrillBlocksWhenPreviousRunRollbackIsUnverified(t *testing.T) {
	store := newEngineTestDecisionStore(t)
	engine := &Engine{store: store}

	previous := storage.DrillRun{
		ID:        "run-prev",
		Type:      "UnsupportedType",
		Target:    "default/checkoutservice",
		Status:    StatusCompleted,
		StartTime: "2026-03-07T10:00:00Z",
		Config:    json.RawMessage(`{"namespace":"default"}`),
		Verdict:   "Success",
	}
	if err := store.InsertDrillRun(previous); err != nil {
		t.Fatalf("InsertDrillRun(previous) failed: %v", err)
	}

	next := storage.DrillRun{
		ID:        "run-next",
		Type:      "UnsupportedType",
		Target:    "default/paymentservice",
		Status:    StatusPlanned,
		StartTime: "2026-03-07T10:05:00Z",
		Config:    json.RawMessage(`{"namespace":"default"}`),
		Verdict:   "Pending",
	}
	if err := store.InsertDrillRun(next); err != nil {
		t.Fatalf("InsertDrillRun(next) failed: %v", err)
	}

	err := engine.ExecuteDrill(next.ID)
	if !errors.Is(err, ErrRollbackGateBlocked) {
		t.Fatalf("expected rollback gate error %v, got %v", ErrRollbackGateBlocked, err)
	}
}

func TestEnforceRollbackTransitionGateUsesLatestStartedRunOnly(t *testing.T) {
	store := newEngineTestDecisionStore(t)
	engine := &Engine{store: store}

	verifiedAt := "2026-03-07T10:06:00Z"
	seeds := []storage.DrillRun{
		{
			ID:        "run-old-unverified",
			Type:      "UnsupportedType",
			Target:    "default/checkoutservice",
			Status:    StatusCompleted,
			StartTime: "2026-03-07T10:00:00Z",
			Config:    json.RawMessage(`{"namespace":"default"}`),
			Verdict:   "Success",
		},
		{
			ID:                 "run-latest-verified",
			Type:               "UnsupportedType",
			Target:             "default/checkoutservice",
			Status:             StatusCompleted,
			StartTime:          "2026-03-07T10:05:00Z",
			Config:             json.RawMessage(`{"namespace":"default"}`),
			Verdict:            "Success",
			RollbackVerifiedAt: &verifiedAt,
		},
		{
			ID:        "run-later-planned",
			Type:      "UnsupportedType",
			Target:    "default/checkoutservice",
			Status:    StatusPlanned,
			StartTime: "2026-03-07T10:10:00Z",
			Config:    json.RawMessage(`{"namespace":"default"}`),
			Verdict:   "Pending",
		},
		{
			ID:        "run-next",
			Type:      "UnsupportedType",
			Target:    "default/checkoutservice",
			Status:    StatusPlanned,
			StartTime: "2026-03-07T10:15:00Z",
			Config:    json.RawMessage(`{"namespace":"default"}`),
			Verdict:   "Pending",
		},
	}

	for _, run := range seeds {
		if err := store.InsertDrillRun(run); err != nil {
			t.Fatalf("InsertDrillRun(%s) failed: %v", run.ID, err)
		}
	}

	if err := engine.enforceRollbackTransitionGate("run-next"); err != nil {
		t.Fatalf("expected rollback gate to pass when latest started run is verified, got %v", err)
	}
}

func newEngineTestDecisionStore(t *testing.T) *storage.DecisionStore {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "engine-tests.db")
	store, err := storage.NewDecisionStore(dbPath)
	if err != nil {
		t.Fatalf("NewDecisionStore() failed: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}
