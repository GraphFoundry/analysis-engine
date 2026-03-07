package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"predictive-analysis-engine/pkg/storage"

	"github.com/go-chi/chi/v5"
)

func TestListScenarioCatalogMarksResponseNoStore(t *testing.T) {
	handler := &DrillsHandler{}
	req := httptest.NewRequest(http.MethodGet, "/drills/catalog", nil)
	rec := httptest.NewRecorder()

	handler.ListScenarioCatalog(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.StatusCode)
	}

	if got := res.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected Cache-Control no-store, got %q", got)
	}

	if got := res.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("expected json content type, got %q", got)
	}

	var body drillScenarioCatalogResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("expected valid json response: %v", err)
	}

	if len(body.Scenarios) != 0 {
		t.Fatalf("expected empty scenario list when engine is nil, got %d scenarios", len(body.Scenarios))
	}
}

func TestGetDrillRunSnapshotReturnsCrossLayerFields(t *testing.T) {
	store := newTestDecisionStore(t)
	handler := &DrillsHandler{Store: store}

	run := storage.DrillRun{
		ID:        "run-1",
		Type:      "ServiceBrownout",
		Target:    "checkoutservice",
		Status:    "Completed",
		StartTime: "2026-03-07T10:00:00Z",
		Config:    json.RawMessage(`{"namespace":"default","observeTokens":15}`),
		Verdict:   "Success",
	}
	if err := store.InsertDrillRun(run); err != nil {
		t.Fatalf("InsertDrillRun() failed: %v", err)
	}

	run.PreSnapshot = json.RawMessage(`{
		"timestamp":"2026-03-07T10:00:30Z",
		"window":"5m",
		"services":[
			{"name":"checkoutservice","namespace":"default","rps":22.5,"errorRate":0.02,"p95":180,"podCount":2,"availability":0.98}
		],
		"edges":[
			{"from":"frontend","to":"checkoutservice","namespace":"default","rps":22.5,"errorRate":0.02,"p95":180}
		]
	}`)
	run.PostSnapshot = json.RawMessage(`{
		"timestamp":"2026-03-07T10:03:00Z",
		"window":"5m",
		"services":[
			{"name":"checkoutservice","namespace":"default","rps":18.0,"errorRate":0.01,"p95":150,"podCount":2,"availability":0.99},
			{"name":"frontend","namespace":"default","rps":35.0,"errorRate":0.00,"p95":120,"podCount":3,"availability":1.00}
		],
		"edges":[
			{"from":"frontend","to":"checkoutservice","namespace":"default","rps":18.0,"errorRate":0.01,"p95":150},
			{"from":"checkoutservice","to":"paymentservice","namespace":"default","rps":12.0,"errorRate":0.01,"p95":90}
		]
	}`)
	if err := store.UpdateDrillRun(run); err != nil {
		t.Fatalf("UpdateDrillRun() failed: %v", err)
	}

	req := drillRunRequestWithID(http.MethodGet, "/drills/runs/run-1/snapshot", "run-1")
	rec := httptest.NewRecorder()

	handler.GetDrillRunSnapshot(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.StatusCode)
	}
	if got := res.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected Cache-Control no-store, got %q", got)
	}

	var body drillRunSnapshotResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("expected valid json response: %v", err)
	}

	if body.RunID != "run-1" {
		t.Fatalf("expected runId run-1, got %q", body.RunID)
	}
	if _, err := time.Parse(time.RFC3339, body.SnapshotTimestamp); err != nil {
		t.Fatalf("expected snapshot timestamp in RFC3339, got %q", body.SnapshotTimestamp)
	}
	if body.VMState.Status != "Completed" {
		t.Fatalf("expected VM state status Completed, got %q", body.VMState.Status)
	}
	if body.VMState.SourceTimestamp == nil || *body.VMState.SourceTimestamp != "2026-03-07T10:00:00Z" {
		t.Fatalf("expected vm source timestamp 2026-03-07T10:00:00Z, got %v", body.VMState.SourceTimestamp)
	}
	if body.BackendMetrics.TargetService != "checkoutservice" {
		t.Fatalf("expected target service checkoutservice, got %q", body.BackendMetrics.TargetService)
	}
	if body.BackendMetrics.Baseline == nil || body.BackendMetrics.Final == nil {
		t.Fatalf("expected baseline and final backend metrics, got baseline=%v final=%v", body.BackendMetrics.Baseline, body.BackendMetrics.Final)
	}
	if body.BackendMetrics.SourceTimestamp == nil || *body.BackendMetrics.SourceTimestamp != "2026-03-07T10:03:00Z" {
		t.Fatalf("expected backend source timestamp 2026-03-07T10:03:00Z, got %v", body.BackendMetrics.SourceTimestamp)
	}
	if body.DashboardMetrics.Source != "drill_run_snapshots" {
		t.Fatalf("expected dashboard source drill_run_snapshots, got %q", body.DashboardMetrics.Source)
	}
	if body.DashboardMetrics.SourceTimestamp == nil || *body.DashboardMetrics.SourceTimestamp != "2026-03-07T10:03:00Z" {
		t.Fatalf("expected dashboard source timestamp 2026-03-07T10:03:00Z, got %v", body.DashboardMetrics.SourceTimestamp)
	}
	if body.GraphSummary.ServiceCount != 2 || body.GraphSummary.EdgeCount != 2 {
		t.Fatalf("expected graph summary counts 2 services/2 edges, got %d/%d", body.GraphSummary.ServiceCount, body.GraphSummary.EdgeCount)
	}
	if body.GraphSummary.SourceTimestamp == nil || *body.GraphSummary.SourceTimestamp != "2026-03-07T10:03:00Z" {
		t.Fatalf("expected graph source timestamp 2026-03-07T10:03:00Z, got %v", body.GraphSummary.SourceTimestamp)
	}
	if body.GraphSummary.Target == nil {
		t.Fatalf("expected graph target metrics to be present")
	}
}

func TestGetDrillRunSnapshotReturnsNotFoundForUnknownRun(t *testing.T) {
	store := newTestDecisionStore(t)
	handler := &DrillsHandler{Store: store}

	req := drillRunRequestWithID(http.MethodGet, "/drills/runs/missing/snapshot", "missing")
	rec := httptest.NewRecorder()

	handler.GetDrillRunSnapshot(rec, req)

	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, res.StatusCode)
	}
}

func newTestDecisionStore(t *testing.T) *storage.DecisionStore {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "decisions.db")
	store, err := storage.NewDecisionStore(dbPath)
	if err != nil {
		t.Fatalf("NewDecisionStore() failed: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func drillRunRequestWithID(method, path, id string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", id)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx)
	return req.WithContext(ctx)
}
