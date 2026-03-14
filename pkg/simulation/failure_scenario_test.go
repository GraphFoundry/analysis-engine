package simulation

import (
	"strings"
	"testing"
	"time"
)

// --- helpers ---

func makeFailureRequest(targetServiceID string) SimulationRequest {
	return SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioFailureShutdown,
		SnapshotTimestamp: time.Now().UTC().Format(time.RFC3339),
		FailureShutdownParams: &FailureShutdownParams{
			TargetServiceID: targetServiceID,
		},
	}
}

func makeSnapshotFromInput(nodes []SnapshotServiceNode, edges []SnapshotServiceEdge, runtime []SnapshotRuntimeService) SimulationSnapshot {
	return ComposeSnapshotAt(SnapshotInput{
		Nodes:           nodes,
		Edges:           edges,
		RuntimeServices: runtime,
	}, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
}

func makeFailureContext(req SimulationRequest, snap SimulationSnapshot) ExecutionContext {
	return BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})
}

func makeFailureContextWithInflux(req SimulationRequest, snap SimulationSnapshot) ExecutionContext {
	return BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      true,
		DataSufficient: true,
		Sparse:         false,
	})
}

// --- tests ---

// TestRunFailureShutdownScenario_TargetNotInSnapshot verifies that when the target service
// is not present in the snapshot, the response is DEFERRED with a clear reason.
func TestRunFailureShutdownScenario_TargetNotInSnapshot(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-a", Name: "A", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeFailureRequest("svc-missing")
	ctx := makeFailureContext(req, snap)

	resp := RunFailureShutdownScenario(ctx)

	if resp.ResultStatus != ResultStatusDeferred {
		t.Errorf("expected DEFERRED, got %q", resp.ResultStatus)
	}
	if resp.DeferredReason == "" {
		t.Error("expected non-empty DeferredReason")
	}
	if !strings.Contains(resp.DeferredReason, "svc-missing") {
		t.Errorf("DeferredReason should mention target service ID, got %q", resp.DeferredReason)
	}
	// No guessed values in deferred response.
	if len(resp.BeforeAfterValues) != 0 {
		t.Errorf("expected no BeforeAfterValues for DEFERRED result, got %d", len(resp.BeforeAfterValues))
	}
	if len(resp.ImpactedServices) != 0 {
		t.Errorf("expected no ImpactedServices for DEFERRED result, got %d", len(resp.ImpactedServices))
	}
}

// TestRunFailureShutdownScenario_NoCallersNoDownstream verifies the case where the target
// service exists but has no incoming or outgoing edges.
func TestRunFailureShutdownScenario_NoCallersNoDownstream(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-isolated", Name: "isolated", Namespace: "default"},
		},
		nil,
		nil,
	)
	req := makeFailureRequest("svc-isolated")
	ctx := makeFailureContext(req, snap)

	resp := RunFailureShutdownScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Errorf("expected OK, got %q", resp.ResultStatus)
	}
	if resp.Recommendation.Action != "no_action_needed" {
		t.Errorf("expected no_action_needed, got %q", resp.Recommendation.Action)
	}
	// Target itself must be in impacted services.
	if len(resp.ImpactedServices) != 1 {
		t.Errorf("expected 1 impacted service (target only), got %d", len(resp.ImpactedServices))
	}
	if resp.ImpactedServices[0].Role != "target" {
		t.Errorf("expected role=target, got %q", resp.ImpactedServices[0].Role)
	}
	// Paths: none (no edges).
	if len(resp.ImpactedPaths) != 0 {
		t.Errorf("expected 0 impacted paths, got %d", len(resp.ImpactedPaths))
	}
}

// TestRunFailureShutdownScenario_WithCallers verifies impacted services and paths for a
// target that has direct callers in the snapshot.
func TestRunFailureShutdownScenario_WithCallers(t *testing.T) {
	p95 := 50.0
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-b", Name: "B", Namespace: "default"},
			{ServiceID: "svc-target", Name: "Target", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-target", RateRPS: 100, ErrorRate: 0.01, P95Ms: &p95},
			{SourceServiceID: "svc-b", TargetServiceID: "svc-target", RateRPS: 50, ErrorRate: 0.02, P95Ms: &p95},
		},
		nil,
	)
	req := makeFailureRequest("svc-target")
	ctx := makeFailureContext(req, snap)

	resp := RunFailureShutdownScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %q", resp.ResultStatus)
	}

	// Impacted services: target + 2 callers = 3.
	if len(resp.ImpactedServices) != 3 {
		t.Errorf("expected 3 impacted services, got %d", len(resp.ImpactedServices))
	}
	roles := map[string]int{}
	for _, s := range resp.ImpactedServices {
		roles[s.Role]++
	}
	if roles["target"] != 1 {
		t.Errorf("expected 1 target service, got %d", roles["target"])
	}
	if roles["caller"] != 2 {
		t.Errorf("expected 2 caller services, got %d", roles["caller"])
	}

	// Impacted paths: 2 caller→target 1-hop paths.
	foundCallerTarget := 0
	for _, p := range resp.ImpactedPaths {
		if len(p.Path) == 2 && p.Path[1] == "svc-target" {
			foundCallerTarget++
		}
	}
	if foundCallerTarget != 2 {
		t.Errorf("expected 2 caller→target paths, got %d", foundCallerTarget)
	}

	// BeforeAfterValues: incoming_rps drops from 150 to 0.
	var rpsBAV *BeforeAfterValue
	for i := range resp.BeforeAfterValues {
		if resp.BeforeAfterValues[i].FieldRef == "failure.target.incoming_rps" {
			rpsBAV = &resp.BeforeAfterValues[i]
		}
	}
	if rpsBAV == nil {
		t.Fatal("expected failure.target.incoming_rps BeforeAfterValue")
	}
	if rpsBAV.BeforeValue == nil || *rpsBAV.BeforeValue != 150.0 {
		t.Errorf("expected BeforeValue=150, got %v", rpsBAV.BeforeValue)
	}
	if rpsBAV.AfterValue == nil || *rpsBAV.AfterValue != 0.0 {
		t.Errorf("expected AfterValue=0, got %v", rpsBAV.AfterValue)
	}

	// Recommendation must advocate circuit breakers for callers.
	if resp.Recommendation.Action != "implement_circuit_breaker_and_failover" {
		t.Errorf("expected implement_circuit_breaker_and_failover, got %q", resp.Recommendation.Action)
	}
	if resp.Recommendation.Explanation == "" {
		t.Error("expected non-empty recommendation Explanation")
	}
}

// TestRunFailureShutdownScenario_WithDownstream verifies that downstream services are
// included in impacted services and paths when the target has outgoing edges.
func TestRunFailureShutdownScenario_WithDownstream(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-target", Name: "Target", Namespace: "default"},
			{ServiceID: "svc-db", Name: "DB", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-target", TargetServiceID: "svc-db", RateRPS: 200, ErrorRate: 0.0},
		},
		nil,
	)
	req := makeFailureRequest("svc-target")
	ctx := makeFailureContext(req, snap)

	resp := RunFailureShutdownScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %q", resp.ResultStatus)
	}

	roles := map[string]int{}
	for _, s := range resp.ImpactedServices {
		roles[s.Role]++
	}
	if roles["downstream"] != 1 {
		t.Errorf("expected 1 downstream service, got %d", roles["downstream"])
	}

	// Path: target→db.
	foundTargetDown := false
	for _, p := range resp.ImpactedPaths {
		if len(p.Path) == 2 && p.Path[0] == "svc-target" && p.Path[1] == "svc-db" {
			foundTargetDown = true
		}
	}
	if !foundTargetDown {
		t.Error("expected target→downstream path in ImpactedPaths")
	}
}

// TestRunFailureShutdownScenario_CrossPaths verifies that 2-hop caller→target→downstream
// paths are emitted for a service with both callers and downstream.
func TestRunFailureShutdownScenario_CrossPaths(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-caller", Name: "Caller", Namespace: "default"},
			{ServiceID: "svc-target", Name: "Target", Namespace: "default"},
			{ServiceID: "svc-down", Name: "Down", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-caller", TargetServiceID: "svc-target", RateRPS: 10, ErrorRate: 0},
			{SourceServiceID: "svc-target", TargetServiceID: "svc-down", RateRPS: 10, ErrorRate: 0},
		},
		nil,
	)
	req := makeFailureRequest("svc-target")
	ctx := makeFailureContext(req, snap)

	resp := RunFailureShutdownScenario(ctx)

	// Expect 3-element cross-path.
	foundCross := false
	for _, p := range resp.ImpactedPaths {
		if len(p.Path) == 3 && p.Path[0] == "svc-caller" && p.Path[1] == "svc-target" && p.Path[2] == "svc-down" {
			foundCross = true
		}
	}
	if !foundCross {
		t.Error("expected caller→target→downstream 2-hop cross-path in ImpactedPaths")
	}
}

// TestRunFailureShutdownScenario_BeforeAfterErrorRate verifies the error_rate field:
// before = weighted average from snapshot, after = 1.0.
func TestRunFailureShutdownScenario_BeforeAfterErrorRate(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-caller", Name: "Caller", Namespace: "default"},
			{ServiceID: "svc-target", Name: "Target", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-caller", TargetServiceID: "svc-target", RateRPS: 100, ErrorRate: 0.05},
		},
		nil,
	)
	req := makeFailureRequest("svc-target")
	ctx := makeFailureContext(req, snap)

	resp := RunFailureShutdownScenario(ctx)

	var errBAV *BeforeAfterValue
	for i := range resp.BeforeAfterValues {
		if resp.BeforeAfterValues[i].FieldRef == "failure.target.error_rate" {
			errBAV = &resp.BeforeAfterValues[i]
		}
	}
	if errBAV == nil {
		t.Fatal("expected failure.target.error_rate BeforeAfterValue")
	}
	if errBAV.BeforeValue == nil || *errBAV.BeforeValue != 0.05 {
		t.Errorf("expected BeforeValue=0.05, got %v", errBAV.BeforeValue)
	}
	if errBAV.AfterValue == nil || *errBAV.AfterValue != 1.0 {
		t.Errorf("expected AfterValue=1.0, got %v", errBAV.AfterValue)
	}
}

// TestRunFailureShutdownScenario_P95LatencyField verifies that avg_p95_ms is emitted when
// snapshot edges carry P95 data, and AfterValue is nil (latency undefined post-shutdown).
func TestRunFailureShutdownScenario_P95LatencyField(t *testing.T) {
	p95 := 120.0
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-target", Name: "T", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-target", RateRPS: 10, ErrorRate: 0, P95Ms: &p95},
		},
		nil,
	)
	req := makeFailureRequest("svc-target")
	ctx := makeFailureContext(req, snap)

	resp := RunFailureShutdownScenario(ctx)

	var p95BAV *BeforeAfterValue
	for i := range resp.BeforeAfterValues {
		if resp.BeforeAfterValues[i].FieldRef == "failure.target.avg_p95_ms" {
			p95BAV = &resp.BeforeAfterValues[i]
		}
	}
	if p95BAV == nil {
		t.Fatal("expected failure.target.avg_p95_ms BeforeAfterValue when edges carry P95 data")
	}
	if p95BAV.BeforeValue == nil || *p95BAV.BeforeValue != 120.0 {
		t.Errorf("expected BeforeValue=120.0, got %v", p95BAV.BeforeValue)
	}
	if p95BAV.AfterValue != nil {
		t.Errorf("expected nil AfterValue (undefined post-shutdown), got %v", p95BAV.AfterValue)
	}
}

// TestRunFailureShutdownScenario_NoP95FieldWhenNoEdgeData verifies that avg_p95_ms is
// omitted when no snapshot edges carry P95 values.
func TestRunFailureShutdownScenario_NoP95FieldWhenNoEdgeData(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-target", Name: "T", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			// P95Ms is nil (not provided).
			{SourceServiceID: "svc-a", TargetServiceID: "svc-target", RateRPS: 10, ErrorRate: 0},
		},
		nil,
	)
	req := makeFailureRequest("svc-target")
	ctx := makeFailureContext(req, snap)

	resp := RunFailureShutdownScenario(ctx)

	for _, bav := range resp.BeforeAfterValues {
		if bav.FieldRef == "failure.target.avg_p95_ms" {
			t.Error("avg_p95_ms should not be emitted when edges have no P95 data")
		}
	}
}

// TestRunFailureShutdownScenario_AssumptionsPresent verifies that at least the engine-default
// assumptions are always declared in the response.
func TestRunFailureShutdownScenario_AssumptionsPresent(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-target", Name: "T", Namespace: "default"},
		},
		nil,
		nil,
	)
	req := makeFailureRequest("svc-target")
	ctx := makeFailureContext(req, snap)

	resp := RunFailureShutdownScenario(ctx)

	if len(resp.Assumptions) == 0 {
		t.Error("expected at least one assumption in response")
	}
	keys := map[string]bool{}
	for _, a := range resp.Assumptions {
		keys[a.Key] = true
	}
	if !keys["shutdown.complete_traffic_loss"] {
		t.Error("expected assumption shutdown.complete_traffic_loss")
	}
	if !keys["shutdown.callers_error_rate_one"] {
		t.Error("expected assumption shutdown.callers_error_rate_one")
	}
}

// TestRunFailureShutdownScenario_EvidenceFieldsPopulated verifies that the base evidence
// metadata is propagated into the response.
func TestRunFailureShutdownScenario_EvidenceFieldsPopulated(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-target", Name: "T", Namespace: "default"},
		},
		nil,
		nil,
	)
	req := makeFailureRequest("svc-target")
	ctx := makeFailureContext(req, snap)

	resp := RunFailureShutdownScenario(ctx)

	if resp.Version != SchemaVersion {
		t.Errorf("expected version %q, got %q", SchemaVersion, resp.Version)
	}
	if resp.ScenarioType != ScenarioFailureShutdown {
		t.Errorf("expected scenarioType %q, got %q", ScenarioFailureShutdown, resp.ScenarioType)
	}
	if resp.SnapshotTimestamp == "" {
		t.Error("expected non-empty SnapshotTimestamp")
	}
	if resp.SnapshotHash == "" {
		t.Error("expected non-empty SnapshotHash")
	}
	if len(resp.EvidenceSources) == 0 {
		t.Error("expected non-empty EvidenceSources")
	}
	if resp.EvidenceMode == "" {
		t.Error("expected non-empty EvidenceMode")
	}
	if resp.ConfidenceLevel == "" {
		t.Error("expected non-empty ConfidenceLevel")
	}
}

// TestRunFailureShutdownScenario_Determinism verifies that two calls with the same
// ExecutionContext return byte-equal canonical JSON.
func TestRunFailureShutdownScenario_Determinism(t *testing.T) {
	p95 := 80.0
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "ns1"},
			{ServiceID: "svc-b", Name: "B", Namespace: "ns1"},
			{ServiceID: "svc-target", Name: "Target", Namespace: "ns1"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-target", RateRPS: 100, ErrorRate: 0.01, P95Ms: &p95},
			{SourceServiceID: "svc-b", TargetServiceID: "svc-target", RateRPS: 50, ErrorRate: 0.02},
		},
		nil,
	)
	req := makeFailureRequest("svc-target")
	ctx := makeFailureContext(req, snap)

	resp1 := RunFailureShutdownScenario(ctx)
	resp2 := RunFailureShutdownScenario(ctx)

	b1, err1 := CanonicalizeResponse(resp1)
	b2, err2 := CanonicalizeResponse(resp2)

	if err1 != nil || err2 != nil {
		t.Fatalf("canonicalization failed: %v / %v", err1, err2)
	}
	if string(b1) != string(b2) {
		t.Errorf("responses are not deterministic:\nrun1: %s\nrun2: %s", b1, b2)
	}
}

// TestRunFailureShutdownScenario_ResponsePassesValidation checks that the response
// produced by the scenario model is accepted by ValidateSimulationResponse.
func TestRunFailureShutdownScenario_ResponsePassesValidation(t *testing.T) {
	p95 := 30.0
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-caller", Name: "C", Namespace: "default"},
			{ServiceID: "svc-target", Name: "T", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-caller", TargetServiceID: "svc-target", RateRPS: 20, ErrorRate: 0, P95Ms: &p95},
		},
		nil,
	)
	req := makeFailureRequest("svc-target")
	ctx := makeFailureContextWithInflux(req, snap)

	resp := RunFailureShutdownScenario(ctx)

	if err := ValidateSimulationResponse(resp); err != nil {
		t.Errorf("response failed validation: %v", err)
	}
}

// TestRunFailureShutdownScenario_DeferredResponsePassesValidation checks that a DEFERRED
// response also passes ValidateSimulationResponse.
func TestRunFailureShutdownScenario_DeferredResponsePassesValidation(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-other", Name: "Other", Namespace: "default"},
		},
		nil,
		nil,
	)
	req := makeFailureRequest("svc-missing")
	ctx := makeFailureContext(req, snap)

	resp := RunFailureShutdownScenario(ctx)

	if resp.ResultStatus != ResultStatusDeferred {
		t.Fatalf("expected DEFERRED, got %q", resp.ResultStatus)
	}
	if err := ValidateSimulationResponse(resp); err != nil {
		t.Errorf("deferred response failed validation: %v", err)
	}
}

// TestRunFailureShutdownScenario_RecommendationExplanationCitesEvidence checks that the
// recommendation explanation contains evidence mode and confidence references.
func TestRunFailureShutdownScenario_RecommendationExplanationCitesEvidence(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-target", Name: "T", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-target", RateRPS: 10, ErrorRate: 0},
		},
		nil,
	)
	req := makeFailureRequest("svc-target")
	ctx := makeFailureContext(req, snap)

	resp := RunFailureShutdownScenario(ctx)

	exp := resp.Recommendation.Explanation
	if !strings.Contains(exp, string(ctx.Evidence.Mode)) {
		t.Errorf("explanation should reference evidence mode %q, got: %s", ctx.Evidence.Mode, exp)
	}
	if !strings.Contains(exp, string(ctx.Evidence.Confidence)) {
		t.Errorf("explanation should reference confidence level %q, got: %s", ctx.Evidence.Confidence, exp)
	}
}
