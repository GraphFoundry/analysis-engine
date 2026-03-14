package simulation

import (
	"strings"
	"testing"
	"time"
)

// --- helpers ---

func makeChattyRequest(sourceID, targetID string) SimulationRequest {
	return SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioChattyColocation,
		SnapshotTimestamp: time.Now().UTC().Format(time.RFC3339),
		ChattyColocationParams: &ChattyColocationParams{
			SourceServiceID: sourceID,
			TargetServiceID: targetID,
		},
	}
}

func makeChattyContext(req SimulationRequest, snap SimulationSnapshot) ExecutionContext {
	return BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})
}

func makeChattyContextWithInflux(req SimulationRequest, snap SimulationSnapshot) ExecutionContext {
	return BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      true,
		DataSufficient: true,
		Sparse:         false,
	})
}

// --- tests ---

// TestRunChattyColocationScenario_SourceNotInSnapshot verifies that a missing source service
// returns DEFERRED with a clear reason and no guessed numeric values.
func TestRunChattyColocationScenario_SourceNotInSnapshot(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-b", Name: "B", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeChattyRequest("svc-missing", "svc-b")
	ctx := makeChattyContext(req, snap)

	resp := RunChattyColocationScenario(ctx)

	if resp.ResultStatus != ResultStatusDeferred {
		t.Errorf("expected DEFERRED, got %q", resp.ResultStatus)
	}
	if resp.DeferredReason == "" {
		t.Error("expected non-empty DeferredReason")
	}
	if !strings.Contains(resp.DeferredReason, "svc-missing") {
		t.Errorf("DeferredReason should mention source service ID, got %q", resp.DeferredReason)
	}
	if len(resp.BeforeAfterValues) != 0 {
		t.Errorf("expected no BeforeAfterValues for DEFERRED result, got %d", len(resp.BeforeAfterValues))
	}
	if len(resp.ImpactedServices) != 0 {
		t.Errorf("expected no ImpactedServices for DEFERRED result, got %d", len(resp.ImpactedServices))
	}
}

// TestRunChattyColocationScenario_TargetNotInSnapshot verifies that a missing target service
// returns DEFERRED with a clear reason.
func TestRunChattyColocationScenario_TargetNotInSnapshot(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-a", Name: "A", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeChattyRequest("svc-a", "svc-missing")
	ctx := makeChattyContext(req, snap)

	resp := RunChattyColocationScenario(ctx)

	if resp.ResultStatus != ResultStatusDeferred {
		t.Errorf("expected DEFERRED, got %q", resp.ResultStatus)
	}
	if !strings.Contains(resp.DeferredReason, "svc-missing") {
		t.Errorf("DeferredReason should mention target service ID, got %q", resp.DeferredReason)
	}
}

// TestRunChattyColocationScenario_NoDirectEdgeDeferred verifies that when both services
// are in the snapshot but no direct edge exists, the result is DEFERRED.
func TestRunChattyColocationScenario_NoDirectEdgeDeferred(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-b", Name: "B", Namespace: "default"},
		},
		// No edges at all.
		nil,
		nil,
	)
	req := makeChattyRequest("svc-a", "svc-b")
	ctx := makeChattyContext(req, snap)

	resp := RunChattyColocationScenario(ctx)

	if resp.ResultStatus != ResultStatusDeferred {
		t.Errorf("expected DEFERRED when no direct edge exists, got %q", resp.ResultStatus)
	}
	if resp.DeferredReason == "" {
		t.Error("expected non-empty DeferredReason for missing edge")
	}
}

// TestRunChattyColocationScenario_HighRPSSameNamespaceCoLocate verifies that a chatty
// pair in the same namespace receives a co_locate recommendation.
func TestRunChattyColocationScenario_HighRPSSameNamespaceCoLocate(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-b", Name: "B", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-b", RateRPS: 100, ErrorRate: 0},
		},
		nil,
	)
	req := makeChattyRequest("svc-a", "svc-b")
	ctx := makeChattyContext(req, snap)

	resp := RunChattyColocationScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %q", resp.ResultStatus)
	}
	if resp.Recommendation.Action != "co_locate" {
		t.Errorf("expected co_locate for high-RPS same-namespace pair, got %q", resp.Recommendation.Action)
	}
}

// TestRunChattyColocationScenario_HighRPSDifferentNamespaceMigrate verifies that a chatty
// pair in different namespaces receives a migrate recommendation.
func TestRunChattyColocationScenario_HighRPSDifferentNamespaceMigrate(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "ns-frontend"},
			{ServiceID: "svc-b", Name: "B", Namespace: "ns-backend"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-b", RateRPS: 200, ErrorRate: 0},
		},
		nil,
	)
	req := makeChattyRequest("svc-a", "svc-b")
	ctx := makeChattyContext(req, snap)

	resp := RunChattyColocationScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %q", resp.ResultStatus)
	}
	if resp.Recommendation.Action != "migrate" {
		t.Errorf("expected migrate for high-RPS cross-namespace pair, got %q", resp.Recommendation.Action)
	}
}

// TestRunChattyColocationScenario_LowRPSNoChange verifies that a below-threshold RPS pair
// receives a no_change recommendation.
func TestRunChattyColocationScenario_LowRPSNoChange(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-b", Name: "B", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-b", RateRPS: 5, ErrorRate: 0},
		},
		nil,
	)
	req := makeChattyRequest("svc-a", "svc-b")
	ctx := makeChattyContext(req, snap)

	resp := RunChattyColocationScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %q", resp.ResultStatus)
	}
	if resp.Recommendation.Action != "no_change" {
		t.Errorf("expected no_change for low-RPS pair, got %q", resp.Recommendation.Action)
	}
}

// TestRunChattyColocationScenario_ExactThresholdBoundary verifies that RPS exactly at
// chattyRPSThreshold (50.0) is classified as chatty (co_locate for same namespace).
func TestRunChattyColocationScenario_ExactThresholdBoundary(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-b", Name: "B", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-b", RateRPS: chattyRPSThreshold, ErrorRate: 0},
		},
		nil,
	)
	req := makeChattyRequest("svc-a", "svc-b")
	ctx := makeChattyContext(req, snap)

	resp := RunChattyColocationScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %q", resp.ResultStatus)
	}
	if resp.Recommendation.Action != "co_locate" {
		t.Errorf("expected co_locate at exactly chattyRPSThreshold boundary, got %q", resp.Recommendation.Action)
	}
}

// TestRunChattyColocationScenario_RPSBAVIsUnchanged verifies that the colocation.edge.rps
// before and after values are equal (co-location does not change call frequency).
func TestRunChattyColocationScenario_RPSBAVIsUnchanged(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-b", Name: "B", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-b", RateRPS: 80, ErrorRate: 0},
		},
		nil,
	)
	req := makeChattyRequest("svc-a", "svc-b")
	ctx := makeChattyContext(req, snap)

	resp := RunChattyColocationScenario(ctx)

	var rpsBAV *BeforeAfterValue
	for i := range resp.BeforeAfterValues {
		if resp.BeforeAfterValues[i].FieldRef == "colocation.edge.rps" {
			rpsBAV = &resp.BeforeAfterValues[i]
		}
	}
	if rpsBAV == nil {
		t.Fatal("expected colocation.edge.rps BeforeAfterValue")
	}
	if rpsBAV.BeforeValue == nil || *rpsBAV.BeforeValue != 80.0 {
		t.Errorf("expected BeforeValue=80, got %v", rpsBAV.BeforeValue)
	}
	if rpsBAV.AfterValue == nil || *rpsBAV.AfterValue != 80.0 {
		t.Errorf("expected AfterValue=80 (unchanged by co-location), got %v", rpsBAV.AfterValue)
	}
	if rpsBAV.DeltaValue == nil || *rpsBAV.DeltaValue != 0.0 {
		t.Errorf("expected DeltaValue=0 for RPS, got %v", rpsBAV.DeltaValue)
	}
}

// TestRunChattyColocationScenario_LatencyBAVAppliesReductionFactor verifies that the
// colocation.edge.latency_p95_ms after value applies colocationLatencyFactor to the before value.
func TestRunChattyColocationScenario_LatencyBAVAppliesReductionFactor(t *testing.T) {
	p95 := 100.0
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-b", Name: "B", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-b", RateRPS: 80, P95Ms: &p95},
		},
		nil,
	)
	req := makeChattyRequest("svc-a", "svc-b")
	ctx := makeChattyContext(req, snap)

	resp := RunChattyColocationScenario(ctx)

	var latBAV *BeforeAfterValue
	for i := range resp.BeforeAfterValues {
		if resp.BeforeAfterValues[i].FieldRef == "colocation.edge.latency_p95_ms" {
			latBAV = &resp.BeforeAfterValues[i]
		}
	}
	if latBAV == nil {
		t.Fatal("expected colocation.edge.latency_p95_ms BeforeAfterValue")
	}
	if latBAV.BeforeValue == nil || *latBAV.BeforeValue != 100.0 {
		t.Errorf("expected BeforeValue=100, got %v", latBAV.BeforeValue)
	}
	// 100 × 0.60 = 60 ms
	expectedAfter := 100.0 * colocationLatencyFactor
	if latBAV.AfterValue == nil || *latBAV.AfterValue != expectedAfter {
		t.Errorf("expected AfterValue=%.2f (factor %.2f), got %v", expectedAfter, colocationLatencyFactor, latBAV.AfterValue)
	}
}

// TestRunChattyColocationScenario_NoLatencyBAVWhenNoEdgeP95 verifies that latency_p95_ms
// is omitted when the edge carries no P95 data.
func TestRunChattyColocationScenario_NoLatencyBAVWhenNoEdgeP95(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-b", Name: "B", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			// P95Ms is nil — no latency data.
			{SourceServiceID: "svc-a", TargetServiceID: "svc-b", RateRPS: 80},
		},
		nil,
	)
	req := makeChattyRequest("svc-a", "svc-b")
	ctx := makeChattyContext(req, snap)

	resp := RunChattyColocationScenario(ctx)

	for _, bav := range resp.BeforeAfterValues {
		if bav.FieldRef == "colocation.edge.latency_p95_ms" {
			t.Error("latency_p95_ms should not be emitted when edge has no P95 data")
		}
	}
}

// TestRunChattyColocationScenario_ImpactedServicesRoles verifies the source and target
// services carry the correct chatty_source and chatty_target roles.
func TestRunChattyColocationScenario_ImpactedServicesRoles(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-b", Name: "B", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-b", RateRPS: 80},
		},
		nil,
	)
	req := makeChattyRequest("svc-a", "svc-b")
	ctx := makeChattyContext(req, snap)

	resp := RunChattyColocationScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %q", resp.ResultStatus)
	}

	roles := map[string]string{}
	for _, s := range resp.ImpactedServices {
		roles[s.ServiceID] = s.Role
	}
	if roles["svc-a"] != "chatty_source" {
		t.Errorf("expected svc-a role=chatty_source, got %q", roles["svc-a"])
	}
	if roles["svc-b"] != "chatty_target" {
		t.Errorf("expected svc-b role=chatty_target, got %q", roles["svc-b"])
	}
}

// TestRunChattyColocationScenario_ImpactedPathIsSourceToTarget verifies the impacted path
// is exactly [sourceID, targetID].
func TestRunChattyColocationScenario_ImpactedPathIsSourceToTarget(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-b", Name: "B", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-b", RateRPS: 80},
		},
		nil,
	)
	req := makeChattyRequest("svc-a", "svc-b")
	ctx := makeChattyContext(req, snap)

	resp := RunChattyColocationScenario(ctx)

	if len(resp.ImpactedPaths) != 1 {
		t.Fatalf("expected 1 impacted path, got %d", len(resp.ImpactedPaths))
	}
	p := resp.ImpactedPaths[0].Path
	if len(p) != 2 || p[0] != "svc-a" || p[1] != "svc-b" {
		t.Errorf("expected path [svc-a svc-b], got %v", p)
	}
}

// TestRunChattyColocationScenario_AssumptionsPresent verifies that required engine-default
// assumptions are declared in the response.
func TestRunChattyColocationScenario_AssumptionsPresent(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-b", Name: "B", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-b", RateRPS: 80},
		},
		nil,
	)
	req := makeChattyRequest("svc-a", "svc-b")
	ctx := makeChattyContext(req, snap)

	resp := RunChattyColocationScenario(ctx)

	if len(resp.Assumptions) == 0 {
		t.Fatal("expected at least one assumption")
	}
	keys := map[string]bool{}
	for _, a := range resp.Assumptions {
		keys[a.Key] = true
	}
	if !keys["colocation.latency_reduction_factor"] {
		t.Error("expected assumption colocation.latency_reduction_factor")
	}
	if !keys["colocation.rps_unchanged"] {
		t.Error("expected assumption colocation.rps_unchanged")
	}
	if !keys["edge_data.source"] {
		t.Error("expected assumption edge_data.source")
	}
}

// TestRunChattyColocationScenario_RecommendationCitesEvidenceFields verifies that the
// recommendation explanation references evidence mode and confidence.
func TestRunChattyColocationScenario_RecommendationCitesEvidenceFields(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-b", Name: "B", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-b", RateRPS: 80},
		},
		nil,
	)
	req := makeChattyRequest("svc-a", "svc-b")
	ctx := makeChattyContext(req, snap)

	resp := RunChattyColocationScenario(ctx)

	if !strings.Contains(resp.Recommendation.Explanation, string(ctx.Evidence.Mode)) {
		t.Errorf("explanation should cite evidence mode %q, got: %s", ctx.Evidence.Mode, resp.Recommendation.Explanation)
	}
	if !strings.Contains(resp.Recommendation.Explanation, string(ctx.Evidence.Confidence)) {
		t.Errorf("explanation should cite confidence %q, got: %s", ctx.Evidence.Confidence, resp.Recommendation.Explanation)
	}
}

// TestRunChattyColocationScenario_EvidenceFieldsPopulated verifies that all base evidence
// metadata fields are propagated from the execution context into the response.
func TestRunChattyColocationScenario_EvidenceFieldsPopulated(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-b", Name: "B", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-b", RateRPS: 80},
		},
		nil,
	)
	req := makeChattyRequest("svc-a", "svc-b")
	ctx := makeChattyContext(req, snap)

	resp := RunChattyColocationScenario(ctx)

	if resp.Version != SchemaVersion {
		t.Errorf("expected version %q, got %q", SchemaVersion, resp.Version)
	}
	if resp.ScenarioType != ScenarioChattyColocation {
		t.Errorf("expected scenarioType %q, got %q", ScenarioChattyColocation, resp.ScenarioType)
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

// TestRunChattyColocationScenario_Determinism verifies that two identical runs produce
// byte-equal canonical JSON responses.
func TestRunChattyColocationScenario_Determinism(t *testing.T) {
	p95 := 80.0
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "ns1"},
			{ServiceID: "svc-b", Name: "B", Namespace: "ns1"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-b", RateRPS: 120, ErrorRate: 0.01, P95Ms: &p95},
		},
		nil,
	)
	req := makeChattyRequest("svc-a", "svc-b")
	ctx := makeChattyContext(req, snap)

	resp1 := RunChattyColocationScenario(ctx)
	resp2 := RunChattyColocationScenario(ctx)

	b1, err1 := CanonicalizeResponse(resp1)
	b2, err2 := CanonicalizeResponse(resp2)

	if err1 != nil || err2 != nil {
		t.Fatalf("canonicalization failed: %v / %v", err1, err2)
	}
	if string(b1) != string(b2) {
		t.Errorf("responses are not deterministic:\nrun1: %s\nrun2: %s", b1, b2)
	}
}

// TestRunChattyColocationScenario_ResponsePassesValidation checks that the response
// produced by the scenario model is accepted by ValidateSimulationResponse.
func TestRunChattyColocationScenario_ResponsePassesValidation(t *testing.T) {
	p95 := 40.0
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-b", Name: "B", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-b", RateRPS: 80, ErrorRate: 0, P95Ms: &p95},
		},
		nil,
	)
	req := makeChattyRequest("svc-a", "svc-b")
	ctx := makeChattyContextWithInflux(req, snap)

	resp := RunChattyColocationScenario(ctx)

	if err := ValidateSimulationResponse(resp); err != nil {
		t.Errorf("response failed validation: %v", err)
	}
}

// TestRunChattyColocationScenario_DeferredResponsePassesValidation checks that a DEFERRED
// response also passes ValidateSimulationResponse.
func TestRunChattyColocationScenario_DeferredResponsePassesValidation(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-other", Name: "Other", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeChattyRequest("svc-missing", "svc-other")
	ctx := makeChattyContext(req, snap)

	resp := RunChattyColocationScenario(ctx)

	if resp.ResultStatus != ResultStatusDeferred {
		t.Fatalf("expected DEFERRED, got %q", resp.ResultStatus)
	}
	if err := ValidateSimulationResponse(resp); err != nil {
		t.Errorf("deferred response failed validation: %v", err)
	}
}

// TestRunChattyColocationScenario_ReverseEdgeNotUsed verifies that a target→source edge
// (opposite direction) does not satisfy the source→target requirement.
func TestRunChattyColocationScenario_ReverseEdgeNotUsed(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-b", Name: "B", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			// Reversed: b→a, not a→b.
			{SourceServiceID: "svc-b", TargetServiceID: "svc-a", RateRPS: 200},
		},
		nil,
	)
	req := makeChattyRequest("svc-a", "svc-b")
	ctx := makeChattyContext(req, snap)

	resp := RunChattyColocationScenario(ctx)

	// Since the direct a→b edge doesn't exist, the result must be DEFERRED.
	if resp.ResultStatus != ResultStatusDeferred {
		t.Errorf("expected DEFERRED when only reverse edge exists, got %q", resp.ResultStatus)
	}
}
