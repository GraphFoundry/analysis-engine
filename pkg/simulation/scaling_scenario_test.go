package simulation

import (
	"strings"
	"testing"
	"time"
)

// --- helpers ---

func makeScalingRequest(targetID string, currentPods, newPods int) SimulationRequest {
	return SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioScaling,
		SnapshotTimestamp: time.Now().UTC().Format(time.RFC3339),
		ScalingParams: &ScalingParams{
			TargetServiceID: targetID,
			CurrentPods:     currentPods,
			NewPods:         newPods,
		},
	}
}

func makeScalingRequestWithMetric(targetID string, currentPods, newPods int, metric string) SimulationRequest {
	return SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioScaling,
		SnapshotTimestamp: time.Now().UTC().Format(time.RFC3339),
		ScalingParams: &ScalingParams{
			TargetServiceID: targetID,
			CurrentPods:     currentPods,
			NewPods:         newPods,
			LatencyMetric:   metric,
		},
	}
}

func makeScalingContext(req SimulationRequest, snap SimulationSnapshot) ExecutionContext {
	return BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})
}

func makeScalingContextWithInflux(req SimulationRequest, snap SimulationSnapshot) ExecutionContext {
	return BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      true,
		DataSufficient: true,
		Sparse:         false,
	})
}

// --- tests ---

// TestRunScalingScenario_TargetNotInSnapshot verifies that a missing target service returns
// a DEFERRED result with a clear reason and no guessed numeric values.
func TestRunScalingScenario_TargetNotInSnapshot(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-a", Name: "A", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeScalingRequest("svc-missing", 2, 4)
	ctx := makeScalingContext(req, snap)

	resp := RunScalingScenario(ctx)

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

// TestRunScalingScenario_SamePodCount verifies that currentPods==newPods returns DEFERRED
// (no change to simulate).
func TestRunScalingScenario_SamePodCount(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-target", Name: "T", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeScalingRequest("svc-target", 3, 3)
	ctx := makeScalingContext(req, snap)

	resp := RunScalingScenario(ctx)

	if resp.ResultStatus != ResultStatusDeferred {
		t.Errorf("expected DEFERRED for no-change scaling, got %q", resp.ResultStatus)
	}
	if resp.DeferredReason == "" {
		t.Error("expected non-empty DeferredReason for no-change case")
	}
}

// TestRunScalingScenario_ScaleUp_PodCountBAV verifies pod_count before/after/delta for scale-up.
func TestRunScalingScenario_ScaleUp_PodCountBAV(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-target", Name: "T", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeScalingRequest("svc-target", 2, 4)
	ctx := makeScalingContext(req, snap)

	resp := RunScalingScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %q", resp.ResultStatus)
	}

	var podBAV *BeforeAfterValue
	for i := range resp.BeforeAfterValues {
		if resp.BeforeAfterValues[i].FieldRef == "scaling.target.pod_count" {
			podBAV = &resp.BeforeAfterValues[i]
		}
	}
	if podBAV == nil {
		t.Fatal("expected scaling.target.pod_count BeforeAfterValue")
	}
	if podBAV.BeforeValue == nil || *podBAV.BeforeValue != 2.0 {
		t.Errorf("expected BeforeValue=2, got %v", podBAV.BeforeValue)
	}
	if podBAV.AfterValue == nil || *podBAV.AfterValue != 4.0 {
		t.Errorf("expected AfterValue=4, got %v", podBAV.AfterValue)
	}
	if podBAV.DeltaValue == nil || *podBAV.DeltaValue != 2.0 {
		t.Errorf("expected DeltaValue=2, got %v", podBAV.DeltaValue)
	}
}

// TestRunScalingScenario_ScaleDown_PodCountBAV verifies pod_count delta is negative on scale-down.
func TestRunScalingScenario_ScaleDown_PodCountBAV(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-target", Name: "T", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeScalingRequest("svc-target", 6, 3)
	ctx := makeScalingContext(req, snap)

	resp := RunScalingScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %q", resp.ResultStatus)
	}

	var podBAV *BeforeAfterValue
	for i := range resp.BeforeAfterValues {
		if resp.BeforeAfterValues[i].FieldRef == "scaling.target.pod_count" {
			podBAV = &resp.BeforeAfterValues[i]
		}
	}
	if podBAV == nil {
		t.Fatal("expected scaling.target.pod_count BeforeAfterValue")
	}
	if podBAV.DeltaValue == nil || *podBAV.DeltaValue != -3.0 {
		t.Errorf("expected DeltaValue=-3, got %v", podBAV.DeltaValue)
	}
}

// TestRunScalingScenario_RPSCapacityScalesLinearly verifies that RPS capacity projects
// linearly from observed incoming RPS and pod count ratio.
func TestRunScalingScenario_RPSCapacityScalesLinearly(t *testing.T) {
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
	// Double pods: capacity should double.
	req := makeScalingRequest("svc-target", 2, 4)
	ctx := makeScalingContext(req, snap)

	resp := RunScalingScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %q", resp.ResultStatus)
	}

	var rpsBAV *BeforeAfterValue
	for i := range resp.BeforeAfterValues {
		if resp.BeforeAfterValues[i].FieldRef == "scaling.target.rps_capacity" {
			rpsBAV = &resp.BeforeAfterValues[i]
		}
	}
	if rpsBAV == nil {
		t.Fatal("expected scaling.target.rps_capacity BeforeAfterValue")
	}
	if rpsBAV.BeforeValue == nil || *rpsBAV.BeforeValue != 100.0 {
		t.Errorf("expected BeforeValue=100, got %v", rpsBAV.BeforeValue)
	}
	if rpsBAV.AfterValue == nil || *rpsBAV.AfterValue != 200.0 {
		t.Errorf("expected AfterValue=200 (2× scale-up), got %v", rpsBAV.AfterValue)
	}
}

// TestRunScalingScenario_LatencyP95EstimateInverseProp verifies P95 latency is projected
// using inverse proportionality (scale-up → lower latency).
func TestRunScalingScenario_LatencyP95EstimateInverseProp(t *testing.T) {
	p95 := 100.0
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-target", Name: "T", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-target", RateRPS: 50, ErrorRate: 0, P95Ms: &p95},
		},
		nil,
	)
	// Double pods: P95 should halve.
	req := makeScalingRequest("svc-target", 2, 4)
	ctx := makeScalingContext(req, snap)

	resp := RunScalingScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %q", resp.ResultStatus)
	}

	var latBAV *BeforeAfterValue
	for i := range resp.BeforeAfterValues {
		if resp.BeforeAfterValues[i].FieldRef == "scaling.target.latency_p95_ms" {
			latBAV = &resp.BeforeAfterValues[i]
		}
	}
	if latBAV == nil {
		t.Fatal("expected scaling.target.latency_p95_ms BeforeAfterValue")
	}
	if latBAV.BeforeValue == nil || *latBAV.BeforeValue != 100.0 {
		t.Errorf("expected BeforeValue=100, got %v", latBAV.BeforeValue)
	}
	if latBAV.AfterValue == nil || *latBAV.AfterValue != 50.0 {
		t.Errorf("expected AfterValue=50 (halved for 2× pods), got %v", latBAV.AfterValue)
	}
}

// TestRunScalingScenario_LatencyP50Metric verifies the p50 latency metric is used when
// the request specifies LatencyMetric="p50".
func TestRunScalingScenario_LatencyP50Metric(t *testing.T) {
	p50 := 40.0
	p95 := 120.0
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-target", Name: "T", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-target", RateRPS: 50, ErrorRate: 0, P50Ms: &p50, P95Ms: &p95},
		},
		nil,
	)
	req := makeScalingRequestWithMetric("svc-target", 2, 4, "p50")
	ctx := makeScalingContext(req, snap)

	resp := RunScalingScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %q", resp.ResultStatus)
	}

	foundP50 := false
	foundP95 := false
	for _, bav := range resp.BeforeAfterValues {
		if bav.FieldRef == "scaling.target.latency_p50_ms" {
			foundP50 = true
		}
		if bav.FieldRef == "scaling.target.latency_p95_ms" {
			foundP95 = true
		}
	}
	if !foundP50 {
		t.Error("expected scaling.target.latency_p50_ms when LatencyMetric=p50")
	}
	if foundP95 {
		t.Error("did not expect scaling.target.latency_p95_ms when LatencyMetric=p50")
	}
}

// TestRunScalingScenario_NoLatencyFieldWhenNoEdgeData verifies that latency_estimate is
// omitted when snapshot edges carry no relevant latency data.
func TestRunScalingScenario_NoLatencyFieldWhenNoEdgeData(t *testing.T) {
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
	req := makeScalingRequest("svc-target", 2, 4)
	ctx := makeScalingContext(req, snap)

	resp := RunScalingScenario(ctx)

	for _, bav := range resp.BeforeAfterValues {
		if strings.HasPrefix(bav.FieldRef, "scaling.target.latency_") {
			t.Errorf("latency estimate should not be emitted when edges have no latency data, got %q", bav.FieldRef)
		}
	}
}

// TestRunScalingScenario_ImpactedServicesContainTargetAndCallers verifies that the target
// and all direct callers are included in the impacted services list.
func TestRunScalingScenario_ImpactedServicesContainTargetAndCallers(t *testing.T) {
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
	req := makeScalingRequest("svc-target", 2, 6)
	ctx := makeScalingContext(req, snap)

	resp := RunScalingScenario(ctx)

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
	if roles["caller"] != 2 {
		t.Errorf("expected 2 caller services, got %d", roles["caller"])
	}
}

// TestRunScalingScenario_ImpactedPathsIncludeOutgoing verifies that target→downstream paths
// are included alongside caller→target paths.
func TestRunScalingScenario_ImpactedPathsIncludeOutgoing(t *testing.T) {
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
	req := makeScalingRequest("svc-target", 2, 4)
	ctx := makeScalingContext(req, snap)

	resp := RunScalingScenario(ctx)

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

// TestRunScalingScenario_ScaleUpRecommendation verifies that scale-up produces an
// approve_scale_up recommendation citing evidence fields.
func TestRunScalingScenario_ScaleUpRecommendation(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-target", Name: "T", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeScalingRequest("svc-target", 2, 4)
	ctx := makeScalingContext(req, snap)

	resp := RunScalingScenario(ctx)

	if resp.Recommendation.Action != "approve_scale_up" {
		t.Errorf("expected approve_scale_up, got %q", resp.Recommendation.Action)
	}
	if !strings.Contains(resp.Recommendation.Explanation, string(ctx.Evidence.Mode)) {
		t.Errorf("explanation should cite evidence mode %q, got: %s", ctx.Evidence.Mode, resp.Recommendation.Explanation)
	}
	if !strings.Contains(resp.Recommendation.Explanation, string(ctx.Evidence.Confidence)) {
		t.Errorf("explanation should cite confidence %q, got: %s", ctx.Evidence.Confidence, resp.Recommendation.Explanation)
	}
}

// TestRunScalingScenario_ScaleDownSafe verifies that a moderate scale-down returns
// approve_scale_down when projected capacity stays well above observed load.
func TestRunScalingScenario_ScaleDownSafe(t *testing.T) {
	// 100 RPS observed; scale 10→9 pods → projected=90 RPS, well above 80% threshold.
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-caller", Name: "C", Namespace: "default"},
			{ServiceID: "svc-target", Name: "T", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-caller", TargetServiceID: "svc-target", RateRPS: 100, ErrorRate: 0},
		},
		nil,
	)
	req := makeScalingRequest("svc-target", 10, 9)
	ctx := makeScalingContext(req, snap)

	resp := RunScalingScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %q", resp.ResultStatus)
	}
	if resp.Recommendation.Action != "approve_scale_down" {
		t.Errorf("expected approve_scale_down for safe reduction, got %q", resp.Recommendation.Action)
	}
}

// TestRunScalingScenario_ScaleDownRisky verifies that a large scale-down returns
// caution_scale_down when projected capacity drops more than 20% below observed load.
func TestRunScalingScenario_ScaleDownRisky(t *testing.T) {
	// 100 RPS observed; scale 10→1 pod → projected=10 RPS, far below 80% threshold.
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-caller", Name: "C", Namespace: "default"},
			{ServiceID: "svc-target", Name: "T", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-caller", TargetServiceID: "svc-target", RateRPS: 100, ErrorRate: 0},
		},
		nil,
	)
	req := makeScalingRequest("svc-target", 10, 1)
	ctx := makeScalingContext(req, snap)

	resp := RunScalingScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %q", resp.ResultStatus)
	}
	if resp.Recommendation.Action != "caution_scale_down" {
		t.Errorf("expected caution_scale_down for risky reduction, got %q", resp.Recommendation.Action)
	}
}

// TestRunScalingScenario_AssumptionsPresent verifies that the required engine-default
// assumptions are always declared in the response.
func TestRunScalingScenario_AssumptionsPresent(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-target", Name: "T", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeScalingRequest("svc-target", 2, 4)
	ctx := makeScalingContext(req, snap)

	resp := RunScalingScenario(ctx)

	if len(resp.Assumptions) == 0 {
		t.Fatal("expected at least one assumption")
	}
	keys := map[string]bool{}
	for _, a := range resp.Assumptions {
		keys[a.Key] = true
	}
	if !keys["scaling.linear_rps_capacity"] {
		t.Error("expected assumption scaling.linear_rps_capacity")
	}
	if !keys["scaling.inverse_proportional_latency"] {
		t.Error("expected assumption scaling.inverse_proportional_latency")
	}
	if !keys["scaling.direction"] {
		t.Error("expected assumption scaling.direction")
	}
}

// TestRunScalingScenario_EvidenceFieldsPopulated verifies that all base evidence metadata
// fields are propagated from the execution context into the response.
func TestRunScalingScenario_EvidenceFieldsPopulated(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-target", Name: "T", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeScalingRequest("svc-target", 2, 4)
	ctx := makeScalingContext(req, snap)

	resp := RunScalingScenario(ctx)

	if resp.Version != SchemaVersion {
		t.Errorf("expected version %q, got %q", SchemaVersion, resp.Version)
	}
	if resp.ScenarioType != ScenarioScaling {
		t.Errorf("expected scenarioType %q, got %q", ScenarioScaling, resp.ScenarioType)
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

// TestRunScalingScenario_Determinism verifies that two calls with identical ExecutionContext
// produce byte-equal canonical JSON responses.
func TestRunScalingScenario_Determinism(t *testing.T) {
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
	req := makeScalingRequest("svc-target", 3, 6)
	ctx := makeScalingContext(req, snap)

	resp1 := RunScalingScenario(ctx)
	resp2 := RunScalingScenario(ctx)

	b1, err1 := CanonicalizeResponse(resp1)
	b2, err2 := CanonicalizeResponse(resp2)

	if err1 != nil || err2 != nil {
		t.Fatalf("canonicalization failed: %v / %v", err1, err2)
	}
	if string(b1) != string(b2) {
		t.Errorf("responses are not deterministic:\nrun1: %s\nrun2: %s", b1, b2)
	}
}

// TestRunScalingScenario_ResponsePassesValidation checks that the response produced by
// the scenario model is accepted by ValidateSimulationResponse.
func TestRunScalingScenario_ResponsePassesValidation(t *testing.T) {
	p95 := 60.0
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
	req := makeScalingRequest("svc-target", 2, 4)
	ctx := makeScalingContextWithInflux(req, snap)

	resp := RunScalingScenario(ctx)

	if err := ValidateSimulationResponse(resp); err != nil {
		t.Errorf("response failed validation: %v", err)
	}
}

// TestRunScalingScenario_DeferredResponsePassesValidation checks that a DEFERRED response
// (missing target) also passes ValidateSimulationResponse.
func TestRunScalingScenario_DeferredResponsePassesValidation(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{{ServiceID: "svc-other", Name: "Other", Namespace: "default"}},
		nil,
		nil,
	)
	req := makeScalingRequest("svc-missing", 2, 4)
	ctx := makeScalingContext(req, snap)

	resp := RunScalingScenario(ctx)

	if resp.ResultStatus != ResultStatusDeferred {
		t.Fatalf("expected DEFERRED, got %q", resp.ResultStatus)
	}
	if err := ValidateSimulationResponse(resp); err != nil {
		t.Errorf("deferred response failed validation: %v", err)
	}
}
