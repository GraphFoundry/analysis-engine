package simulation

import (
	"strings"
	"testing"
	"time"
)

// --- helpers ---

func makeSpikeRequest(targetID string, loadMultiplier float64) SimulationRequest {
	return SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioTrafficSpike,
		SnapshotTimestamp: time.Now().UTC().Format(time.RFC3339),
		TrafficSpikeParams: &TrafficSpikeParams{
			TargetServiceID: targetID,
			LoadMultiplier:  loadMultiplier,
		},
	}
}

func makeSpikeContext(req SimulationRequest, snap SimulationSnapshot) ExecutionContext {
	return BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})
}

func makeSpikeContextWithInflux(req SimulationRequest, snap SimulationSnapshot) ExecutionContext {
	return BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      true,
		DataSufficient: true,
		Sparse:         false,
	})
}

// --- tests ---

// TestRunTrafficSpikeScenario_TargetNotInSnapshot verifies that a missing target service
// returns DEFERRED with a clear reason and no guessed numeric values.
func TestRunTrafficSpikeScenario_TargetNotInSnapshot(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-a", Name: "A", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeSpikeRequest("svc-missing", 3.0)
	ctx := makeSpikeContext(req, snap)

	resp := RunTrafficSpikeScenario(ctx)

	if resp.ResultStatus != ResultStatusDeferred {
		t.Errorf("expected DEFERRED, got %q", resp.ResultStatus)
	}
	if resp.DeferredReason == "" {
		t.Error("expected non-empty DeferredReason")
	}
	if !strings.Contains(resp.DeferredReason, "svc-missing") {
		t.Errorf("DeferredReason should mention target service ID, got %q", resp.DeferredReason)
	}
	if len(resp.BeforeAfterValues) != 0 {
		t.Errorf("expected no BeforeAfterValues for DEFERRED result, got %d", len(resp.BeforeAfterValues))
	}
	if len(resp.ImpactedServices) != 0 {
		t.Errorf("expected no ImpactedServices for DEFERRED result, got %d", len(resp.ImpactedServices))
	}
}

// TestRunTrafficSpikeScenario_RPSScalesWithMultiplier verifies that projected spike RPS equals
// observed RPS × LoadMultiplier.
func TestRunTrafficSpikeScenario_RPSScalesWithMultiplier(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-caller", Name: "Caller", Namespace: "default"},
			{ServiceID: "svc-target", Name: "Target", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-caller", TargetServiceID: "svc-target", RateRPS: 100, ErrorRate: 0},
		},
		nil,
	)
	req := makeSpikeRequest("svc-target", 3.0)
	ctx := makeSpikeContext(req, snap)

	resp := RunTrafficSpikeScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %q", resp.ResultStatus)
	}

	var rpsBAV *BeforeAfterValue
	for i := range resp.BeforeAfterValues {
		if resp.BeforeAfterValues[i].FieldRef == "spike.target.incoming_rps" {
			rpsBAV = &resp.BeforeAfterValues[i]
		}
	}
	if rpsBAV == nil {
		t.Fatal("expected spike.target.incoming_rps BeforeAfterValue")
	}
	if rpsBAV.BeforeValue == nil || *rpsBAV.BeforeValue != 100.0 {
		t.Errorf("expected BeforeValue=100, got %v", rpsBAV.BeforeValue)
	}
	if rpsBAV.AfterValue == nil || *rpsBAV.AfterValue != 300.0 {
		t.Errorf("expected AfterValue=300 (3× spike), got %v", rpsBAV.AfterValue)
	}
	if rpsBAV.DeltaValue == nil || *rpsBAV.DeltaValue != 200.0 {
		t.Errorf("expected DeltaValue=200, got %v", rpsBAV.DeltaValue)
	}
}

// TestRunTrafficSpikeScenario_LatencyP95ScalesLinearly verifies that P95 latency is
// projected linearly with the load multiplier.
func TestRunTrafficSpikeScenario_LatencyP95ScalesLinearly(t *testing.T) {
	p95 := 50.0
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-target", Name: "T", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-target", RateRPS: 80, ErrorRate: 0, P95Ms: &p95},
		},
		nil,
	)
	req := makeSpikeRequest("svc-target", 2.0)
	ctx := makeSpikeContext(req, snap)

	resp := RunTrafficSpikeScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %q", resp.ResultStatus)
	}

	var latBAV *BeforeAfterValue
	for i := range resp.BeforeAfterValues {
		if resp.BeforeAfterValues[i].FieldRef == "spike.target.latency_p95_ms" {
			latBAV = &resp.BeforeAfterValues[i]
		}
	}
	if latBAV == nil {
		t.Fatal("expected spike.target.latency_p95_ms BeforeAfterValue")
	}
	if latBAV.BeforeValue == nil || *latBAV.BeforeValue != 50.0 {
		t.Errorf("expected BeforeValue=50, got %v", latBAV.BeforeValue)
	}
	// 2× multiplier → projected latency = 50 × 2 = 100
	if latBAV.AfterValue == nil || *latBAV.AfterValue != 100.0 {
		t.Errorf("expected AfterValue=100 (linear 2× projection), got %v", latBAV.AfterValue)
	}
}

// TestRunTrafficSpikeScenario_NoLatencyFieldWhenNoEdgeData verifies that latency_p95_ms is
// omitted when snapshot edges carry no P95 data.
func TestRunTrafficSpikeScenario_NoLatencyFieldWhenNoEdgeData(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-target", Name: "T", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			// P95Ms is nil — no latency data.
			{SourceServiceID: "svc-a", TargetServiceID: "svc-target", RateRPS: 50, ErrorRate: 0},
		},
		nil,
	)
	req := makeSpikeRequest("svc-target", 2.0)
	ctx := makeSpikeContext(req, snap)

	resp := RunTrafficSpikeScenario(ctx)

	for _, bav := range resp.BeforeAfterValues {
		if bav.FieldRef == "spike.target.latency_p95_ms" {
			t.Errorf("latency_p95_ms should not be emitted when edges have no latency data")
		}
	}
}

// TestRunTrafficSpikeScenario_ImpactedServicesIncludeCallerAndDownstream verifies that
// the target, callers, and downstream services are all included with correct roles.
func TestRunTrafficSpikeScenario_ImpactedServicesIncludeCallerAndDownstream(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-caller", Name: "Caller", Namespace: "default"},
			{ServiceID: "svc-target", Name: "Target", Namespace: "default"},
			{ServiceID: "svc-db", Name: "DB", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-caller", TargetServiceID: "svc-target", RateRPS: 60, ErrorRate: 0},
			{SourceServiceID: "svc-target", TargetServiceID: "svc-db", RateRPS: 60, ErrorRate: 0},
		},
		nil,
	)
	req := makeSpikeRequest("svc-target", 2.0)
	ctx := makeSpikeContext(req, snap)

	resp := RunTrafficSpikeScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %q", resp.ResultStatus)
	}

	roles := map[string]int{}
	for _, s := range resp.ImpactedServices {
		roles[s.Role]++
	}
	if roles["target"] != 1 {
		t.Errorf("expected 1 target service, got %d", roles["target"])
	}
	if roles["caller"] != 1 {
		t.Errorf("expected 1 caller service, got %d", roles["caller"])
	}
	if roles["downstream"] != 1 {
		t.Errorf("expected 1 downstream service, got %d", roles["downstream"])
	}
}

// TestRunTrafficSpikeScenario_ImpactedPathsIncludeIncomingAndOutgoing verifies that both
// caller→target and target→downstream paths appear in ImpactedPaths.
func TestRunTrafficSpikeScenario_ImpactedPathsIncludeIncomingAndOutgoing(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-caller", Name: "Caller", Namespace: "default"},
			{ServiceID: "svc-target", Name: "Target", Namespace: "default"},
			{ServiceID: "svc-db", Name: "DB", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-caller", TargetServiceID: "svc-target", RateRPS: 50, ErrorRate: 0},
			{SourceServiceID: "svc-target", TargetServiceID: "svc-db", RateRPS: 50, ErrorRate: 0},
		},
		nil,
	)
	req := makeSpikeRequest("svc-target", 2.0)
	ctx := makeSpikeContext(req, snap)

	resp := RunTrafficSpikeScenario(ctx)

	foundIncoming := false
	foundOutgoing := false
	for _, p := range resp.ImpactedPaths {
		if len(p.Path) == 2 && p.Path[0] == "svc-caller" && p.Path[1] == "svc-target" {
			foundIncoming = true
		}
		if len(p.Path) == 2 && p.Path[0] == "svc-target" && p.Path[1] == "svc-db" {
			foundOutgoing = true
		}
	}
	if !foundIncoming {
		t.Error("expected caller→target path in ImpactedPaths")
	}
	if !foundOutgoing {
		t.Error("expected target→downstream path in ImpactedPaths")
	}
}

// TestRunTrafficSpikeScenario_HighMultiplierRecommendation verifies that a ≥3× load
// multiplier produces a pre_emptive_scale_up_required recommendation.
func TestRunTrafficSpikeScenario_HighMultiplierRecommendation(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-target", Name: "T", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeSpikeRequest("svc-target", 5.0)
	ctx := makeSpikeContext(req, snap)

	resp := RunTrafficSpikeScenario(ctx)

	if resp.Recommendation.Action != "pre_emptive_scale_up_required" {
		t.Errorf("expected pre_emptive_scale_up_required for 5× spike, got %q", resp.Recommendation.Action)
	}
}

// TestRunTrafficSpikeScenario_ModerateMultiplierRecommendation verifies that a <3× load
// multiplier produces a monitor_and_prepare_rate_limits recommendation.
func TestRunTrafficSpikeScenario_ModerateMultiplierRecommendation(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-target", Name: "T", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeSpikeRequest("svc-target", 2.0)
	ctx := makeSpikeContext(req, snap)

	resp := RunTrafficSpikeScenario(ctx)

	if resp.Recommendation.Action != "monitor_and_prepare_rate_limits" {
		t.Errorf("expected monitor_and_prepare_rate_limits for 2× spike, got %q", resp.Recommendation.Action)
	}
}

// TestRunTrafficSpikeScenario_RecommendationCitesEvidenceFields verifies that the recommendation
// explanation references evidence mode and confidence from the context.
func TestRunTrafficSpikeScenario_RecommendationCitesEvidenceFields(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-target", Name: "T", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeSpikeRequest("svc-target", 2.0)
	ctx := makeSpikeContext(req, snap)

	resp := RunTrafficSpikeScenario(ctx)

	if !strings.Contains(resp.Recommendation.Explanation, string(ctx.Evidence.Mode)) {
		t.Errorf("explanation should cite evidence mode %q, got: %s", ctx.Evidence.Mode, resp.Recommendation.Explanation)
	}
	if !strings.Contains(resp.Recommendation.Explanation, string(ctx.Evidence.Confidence)) {
		t.Errorf("explanation should cite confidence %q, got: %s", ctx.Evidence.Confidence, resp.Recommendation.Explanation)
	}
}

// TestRunTrafficSpikeScenario_AssumptionsPresent verifies that required engine-default
// assumptions are declared in the response.
func TestRunTrafficSpikeScenario_AssumptionsPresent(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-target", Name: "T", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeSpikeRequest("svc-target", 2.0)
	ctx := makeSpikeContext(req, snap)

	resp := RunTrafficSpikeScenario(ctx)

	if len(resp.Assumptions) == 0 {
		t.Fatal("expected at least one assumption")
	}
	keys := map[string]bool{}
	for _, a := range resp.Assumptions {
		keys[a.Key] = true
	}
	if !keys["spike.linear_latency_model"] {
		t.Error("expected assumption spike.linear_latency_model")
	}
	if !keys["spike.rps_linear_scale"] {
		t.Error("expected assumption spike.rps_linear_scale")
	}
	if !keys["edge_data.source"] {
		t.Error("expected assumption edge_data.source")
	}
}

// TestRunTrafficSpikeScenario_EvidenceFieldsPopulated verifies that all base evidence
// metadata fields are propagated from the execution context into the response.
func TestRunTrafficSpikeScenario_EvidenceFieldsPopulated(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-target", Name: "T", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeSpikeRequest("svc-target", 2.0)
	ctx := makeSpikeContext(req, snap)

	resp := RunTrafficSpikeScenario(ctx)

	if resp.Version != SchemaVersion {
		t.Errorf("expected version %q, got %q", SchemaVersion, resp.Version)
	}
	if resp.ScenarioType != ScenarioTrafficSpike {
		t.Errorf("expected scenarioType %q, got %q", ScenarioTrafficSpike, resp.ScenarioType)
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

// TestRunTrafficSpikeScenario_Determinism verifies that two calls with identical
// ExecutionContext produce byte-equal canonical JSON responses.
func TestRunTrafficSpikeScenario_Determinism(t *testing.T) {
	p95 := 60.0
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
	req := makeSpikeRequest("svc-target", 3.0)
	ctx := makeSpikeContext(req, snap)

	resp1 := RunTrafficSpikeScenario(ctx)
	resp2 := RunTrafficSpikeScenario(ctx)

	b1, err1 := CanonicalizeResponse(resp1)
	b2, err2 := CanonicalizeResponse(resp2)

	if err1 != nil || err2 != nil {
		t.Fatalf("canonicalization failed: %v / %v", err1, err2)
	}
	if string(b1) != string(b2) {
		t.Errorf("responses are not deterministic:\nrun1: %s\nrun2: %s", b1, b2)
	}
}

// TestRunTrafficSpikeScenario_ResponsePassesValidation checks that the response produced
// by the scenario model is accepted by ValidateSimulationResponse.
func TestRunTrafficSpikeScenario_ResponsePassesValidation(t *testing.T) {
	p95 := 40.0
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-caller", Name: "C", Namespace: "default"},
			{ServiceID: "svc-target", Name: "T", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-caller", TargetServiceID: "svc-target", RateRPS: 80, ErrorRate: 0, P95Ms: &p95},
		},
		nil,
	)
	req := makeSpikeRequest("svc-target", 2.5)
	ctx := makeSpikeContextWithInflux(req, snap)

	resp := RunTrafficSpikeScenario(ctx)

	if err := ValidateSimulationResponse(resp); err != nil {
		t.Errorf("response failed validation: %v", err)
	}
}

// TestRunTrafficSpikeScenario_DeferredResponsePassesValidation checks that a DEFERRED
// response (missing target) also passes ValidateSimulationResponse.
func TestRunTrafficSpikeScenario_DeferredResponsePassesValidation(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-other", Name: "Other", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeSpikeRequest("svc-missing", 3.0)
	ctx := makeSpikeContext(req, snap)

	resp := RunTrafficSpikeScenario(ctx)

	if resp.ResultStatus != ResultStatusDeferred {
		t.Fatalf("expected DEFERRED, got %q", resp.ResultStatus)
	}
	if err := ValidateSimulationResponse(resp); err != nil {
		t.Errorf("deferred response failed validation: %v", err)
	}
}

// TestRunTrafficSpikeScenario_MultipleCallers verifies correct handling of multiple
// callers contributing to aggregate RPS.
func TestRunTrafficSpikeScenario_MultipleCallers(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-b", Name: "B", Namespace: "default"},
			{ServiceID: "svc-target", Name: "Target", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-target", RateRPS: 60, ErrorRate: 0},
			{SourceServiceID: "svc-b", TargetServiceID: "svc-target", RateRPS: 40, ErrorRate: 0},
		},
		nil,
	)
	req := makeSpikeRequest("svc-target", 2.0)
	ctx := makeSpikeContext(req, snap)

	resp := RunTrafficSpikeScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %q", resp.ResultStatus)
	}

	var rpsBAV *BeforeAfterValue
	for i := range resp.BeforeAfterValues {
		if resp.BeforeAfterValues[i].FieldRef == "spike.target.incoming_rps" {
			rpsBAV = &resp.BeforeAfterValues[i]
		}
	}
	if rpsBAV == nil {
		t.Fatal("expected spike.target.incoming_rps BeforeAfterValue")
	}
	// Total baseline: 60 + 40 = 100 RPS; 2× spike = 200 RPS
	if rpsBAV.BeforeValue == nil || *rpsBAV.BeforeValue != 100.0 {
		t.Errorf("expected BeforeValue=100 (sum of callers), got %v", rpsBAV.BeforeValue)
	}
	if rpsBAV.AfterValue == nil || *rpsBAV.AfterValue != 200.0 {
		t.Errorf("expected AfterValue=200 (2× spike), got %v", rpsBAV.AfterValue)
	}
}

// TestRunTrafficSpikeScenario_ExactBoundaryMultiplier verifies behavior at the 3.0×
// boundary (should be pre_emptive_scale_up_required, not moderate).
func TestRunTrafficSpikeScenario_ExactBoundaryMultiplier(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-target", Name: "T", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeSpikeRequest("svc-target", 3.0)
	ctx := makeSpikeContext(req, snap)

	resp := RunTrafficSpikeScenario(ctx)

	if resp.Recommendation.Action != "pre_emptive_scale_up_required" {
		t.Errorf("expected pre_emptive_scale_up_required at exactly 3.0× boundary, got %q", resp.Recommendation.Action)
	}
}
