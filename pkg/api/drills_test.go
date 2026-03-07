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

	"predictive-analysis-engine/pkg/drills"
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

func TestRunDrillReturnsConflictWhenRollbackVerificationIsMissing(t *testing.T) {
	store := newTestDecisionStore(t)
	engine := drills.NewEngine(store, nil, nil)
	handler := &DrillsHandler{Engine: engine, Store: store}

	previous := storage.DrillRun{
		ID:        "run-prev",
		Type:      "UnsupportedType",
		Target:    "default/checkoutservice",
		Status:    drills.StatusCompleted,
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
		Status:    drills.StatusPlanned,
		StartTime: "2026-03-07T10:05:00Z",
		Config:    json.RawMessage(`{"namespace":"default"}`),
		Verdict:   "Pending",
	}
	if err := store.InsertDrillRun(next); err != nil {
		t.Fatalf("InsertDrillRun(next) failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/drills/run", strings.NewReader(`{"runId":"run-next"}`))
	rec := httptest.NewRecorder()

	handler.RunDrill(rec, req)

	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, res.StatusCode)
	}

	body := rec.Body.String()
	if !strings.Contains(body, drills.ErrRollbackGateBlocked.Error()) {
		t.Fatalf("expected clear rollback gate error in response body, got %q", body)
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
	if err := store.AddDrillStep(storage.DrillStep{
		RunID:     "run-1",
		Timestamp: "2026-03-07T10:01:00Z",
		Phase:     "Observe",
		Message:   "Scenario checks passed",
		Status:    "Ok",
	}); err != nil {
		t.Fatalf("AddDrillStep() failed: %v", err)
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
	if body.Comparison.VM.Status != "match" {
		t.Fatalf("expected vm comparison status match, got %q", body.Comparison.VM.Status)
	}
	if body.Comparison.API.Status != "match" {
		t.Fatalf("expected api comparison status match, got %q", body.Comparison.API.Status)
	}
	if body.Comparison.UIMetrics.Status != "match" {
		t.Fatalf("expected ui metrics comparison status match, got %q", body.Comparison.UIMetrics.Status)
	}
	if body.Comparison.Graph.Status != "match" {
		t.Fatalf("expected graph comparison status match, got %q", body.Comparison.Graph.Status)
	}
	if body.Comparison.ScenarioVerdict != "passed" {
		t.Fatalf("expected scenario verdict passed, got %q", body.Comparison.ScenarioVerdict)
	}
	if body.Comparison.FailureReason != "" {
		t.Fatalf("expected empty failure reason for passed scenario, got %q", body.Comparison.FailureReason)
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

func TestGetDrillRunSnapshotComparisonIncludesMismatchAndMissingStatuses(t *testing.T) {
	store := newTestDecisionStore(t)
	handler := &DrillsHandler{Store: store}

	run := storage.DrillRun{
		ID:        "run-failed",
		Type:      "ServiceBrownout",
		Target:    "checkoutservice",
		Status:    "Failed",
		StartTime: "2026-03-07T10:00:00Z",
		Config:    json.RawMessage(`{"namespace":"default"}`),
		Verdict:   "Failure",
	}
	if err := store.InsertDrillRun(run); err != nil {
		t.Fatalf("InsertDrillRun() failed: %v", err)
	}
	if err := store.AddDrillStep(storage.DrillStep{
		RunID:     "run-failed",
		Timestamp: "2026-03-07T10:00:30Z",
		Phase:     "Execute",
		Message:   "Action failed",
		Status:    "Error",
	}); err != nil {
		t.Fatalf("AddDrillStep() failed: %v", err)
	}

	req := drillRunRequestWithID(http.MethodGet, "/drills/runs/run-failed/snapshot", "run-failed")
	rec := httptest.NewRecorder()

	handler.GetDrillRunSnapshot(rec, req)

	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.StatusCode)
	}

	var body drillRunSnapshotResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("expected valid json response: %v", err)
	}

	if body.Comparison.VM.Status != "mismatch" {
		t.Fatalf("expected vm comparison mismatch for failed run, got %q", body.Comparison.VM.Status)
	}
	if body.Comparison.API.Status != "mismatch" {
		t.Fatalf("expected api comparison mismatch for failed run, got %q", body.Comparison.API.Status)
	}
	if body.Comparison.UIMetrics.Status != "missing" {
		t.Fatalf("expected ui metrics comparison missing without snapshots, got %q", body.Comparison.UIMetrics.Status)
	}
	if body.Comparison.Graph.Status != "missing" {
		t.Fatalf("expected graph comparison missing without snapshots, got %q", body.Comparison.Graph.Status)
	}
	if body.Comparison.ScenarioVerdict != "failed" {
		t.Fatalf("expected scenario verdict failed, got %q", body.Comparison.ScenarioVerdict)
	}
	expectedReason := "vm mismatch on status (expected Completed, actual Failed)"
	if body.Comparison.FailureReason != expectedReason {
		t.Fatalf("expected failure reason %q, got %q", expectedReason, body.Comparison.FailureReason)
	}
}

func TestGetDrillRunSnapshotComparisonIncludesFieldLevelMismatches(t *testing.T) {
	store := newTestDecisionStore(t)
	handler := &DrillsHandler{Store: store}

	run := storage.DrillRun{
		ID:        "run-mismatch-fields",
		Type:      "ServiceBrownout",
		Target:    "checkoutservice",
		Status:    "Failed",
		StartTime: "2026-03-07T10:00:00Z",
		Config:    json.RawMessage(`{"namespace":"default"}`),
		Verdict:   "Failure",
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
			{"name":"checkoutservice","namespace":"default","rps":18.0,"errorRate":0.05,"p95":220,"podCount":1,"availability":0.90}
		],
		"edges":[
			{"from":"frontend","to":"checkoutservice","namespace":"default","rps":18.0,"errorRate":0.05,"p95":220}
		]
	}`)
	if err := store.UpdateDrillRun(run); err != nil {
		t.Fatalf("UpdateDrillRun() failed: %v", err)
	}
	if err := store.AddDrillStep(storage.DrillStep{
		RunID:     run.ID,
		Timestamp: "2026-03-07T10:01:00Z",
		Phase:     "Execute",
		Message:   "Action failed",
		Status:    "Error",
	}); err != nil {
		t.Fatalf("AddDrillStep() failed: %v", err)
	}

	req := drillRunRequestWithID(http.MethodGet, "/drills/runs/run-mismatch-fields/snapshot", run.ID)
	rec := httptest.NewRecorder()

	handler.GetDrillRunSnapshot(rec, req)

	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.StatusCode)
	}

	var body drillRunSnapshotResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("expected valid json response: %v", err)
	}

	if body.Comparison.VM.Status != "mismatch" {
		t.Fatalf("expected vm comparison mismatch, got %q", body.Comparison.VM.Status)
	}
	if body.Comparison.API.Status != "mismatch" {
		t.Fatalf("expected api comparison mismatch, got %q", body.Comparison.API.Status)
	}
	if body.Comparison.UIMetrics.Status != "mismatch" {
		t.Fatalf("expected ui metrics comparison mismatch, got %q", body.Comparison.UIMetrics.Status)
	}
	if body.Comparison.Graph.Status != "mismatch" {
		t.Fatalf("expected graph comparison mismatch, got %q", body.Comparison.Graph.Status)
	}

	vmStatus, ok := findDrillMismatch(body.Comparison.VM.Mismatches, "status")
	if !ok {
		t.Fatalf("expected vm mismatch for status, got %+v", body.Comparison.VM.Mismatches)
	}
	if vmStatus.ExpectedValue != "Completed" || vmStatus.ActualValue != "Failed" {
		t.Fatalf("expected vm status mismatch Completed->Failed, got %+v", vmStatus)
	}

	apiErrors, ok := findDrillMismatch(body.Comparison.API.Mismatches, "timeline.errorSteps")
	if !ok {
		t.Fatalf("expected api mismatch for timeline.errorSteps, got %+v", body.Comparison.API.Mismatches)
	}
	if apiErrors.ExpectedValue != "0" || apiErrors.ActualValue != "1" {
		t.Fatalf("expected api timeline.errorSteps mismatch 0->1, got %+v", apiErrors)
	}

	uiRPS, ok := findDrillMismatch(body.Comparison.UIMetrics.Mismatches, "uiMetrics.rps")
	if !ok {
		t.Fatalf("expected ui mismatch for rps, got %+v", body.Comparison.UIMetrics.Mismatches)
	}
	if uiRPS.ExpectedValue != "22.5" || uiRPS.ActualValue != "18" {
		t.Fatalf("expected ui rps mismatch 22.5->18, got %+v", uiRPS)
	}

	graphPods, ok := findDrillMismatch(body.Comparison.Graph.Mismatches, "graph.target.podCount")
	if !ok {
		t.Fatalf("expected graph mismatch for podCount, got %+v", body.Comparison.Graph.Mismatches)
	}
	if graphPods.ExpectedValue != "2" || graphPods.ActualValue != "1" {
		t.Fatalf("expected graph podCount mismatch 2->1, got %+v", graphPods)
	}
	if body.Comparison.ScenarioVerdict != "failed" {
		t.Fatalf("expected scenario verdict failed, got %q", body.Comparison.ScenarioVerdict)
	}
	expectedReason := "vm mismatch on status (expected Completed, actual Failed)"
	if body.Comparison.FailureReason != expectedReason {
		t.Fatalf("expected failure reason %q, got %q", expectedReason, body.Comparison.FailureReason)
	}
}

func findDrillMismatch(mismatches []drillRunFieldMismatch, metricName string) (drillRunFieldMismatch, bool) {
	for _, mismatch := range mismatches {
		if mismatch.MetricName == metricName {
			return mismatch, true
		}
	}
	return drillRunFieldMismatch{}, false
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
