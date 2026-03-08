package simulation

import (
	"strings"
	"testing"
)

// --- helpers ---

func p64(v float64) *float64 { return &v }

func makeNetworkCutRequest(links []NetworkLink, degradationPct *float64) SimulationRequest {
	return SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioNetworkCut,
		SnapshotTimestamp: "2025-01-01T00:00:00Z",
		NetworkCutParams: &NetworkCutParams{
			AffectedLinks:      links,
			DegradationPercent: degradationPct,
		},
	}
}

func makeNetworkCutContext(req SimulationRequest, snap SimulationSnapshot) ExecutionContext {
	return BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})
}

func makeNetworkCutContextWithInflux(req SimulationRequest, snap SimulationSnapshot) ExecutionContext {
	return BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      true,
		DataSufficient: true,
		Sparse:         false,
	})
}

// twoNodeSnap returns a snapshot with svc-a → svc-b edge at given RPS/error/p95.
func twoNodeSnap(rps, errorRate float64, p95 *float64) SimulationSnapshot {
	return makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-b", Name: "B", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-b", RateRPS: rps, ErrorRate: errorRate, P95Ms: p95},
		},
		nil,
	)
}

// --- DEFERRED cases ---

func TestRunNetworkCutScenario_NoneMatchedIsDeferred(t *testing.T) {
	snap := twoNodeSnap(100, 0.01, nil)
	req := makeNetworkCutRequest([]NetworkLink{
		{SourceServiceID: "svc-x", TargetServiceID: "svc-y"},
	}, nil)
	ctx := makeNetworkCutContext(req, snap)

	resp := RunNetworkCutScenario(ctx)

	if resp.ResultStatus != ResultStatusDeferred {
		t.Fatalf("expected DEFERRED, got %s", resp.ResultStatus)
	}
	if !strings.Contains(resp.DeferredReason, "svc-x") {
		t.Errorf("DeferredReason should mention missing link, got: %s", resp.DeferredReason)
	}
	if len(resp.ImpactedServices) != 0 {
		t.Errorf("ImpactedServices should be empty for DEFERRED, got %d", len(resp.ImpactedServices))
	}
	if len(resp.BeforeAfterValues) != 0 {
		t.Errorf("BeforeAfterValues should be empty for DEFERRED, got %d", len(resp.BeforeAfterValues))
	}
}

func TestRunNetworkCutScenario_PartialMatchDeferred_NoneMatch(t *testing.T) {
	// All links are missing → DEFERRED (even if multiple)
	snap := twoNodeSnap(50, 0.0, nil)
	req := makeNetworkCutRequest([]NetworkLink{
		{SourceServiceID: "svc-missing-1", TargetServiceID: "svc-b"},
		{SourceServiceID: "svc-a", TargetServiceID: "svc-missing-2"},
	}, nil)
	ctx := makeNetworkCutContext(req, snap)

	resp := RunNetworkCutScenario(ctx)
	if resp.ResultStatus != ResultStatusDeferred {
		t.Fatalf("expected DEFERRED when all links missing, got %s", resp.ResultStatus)
	}
}

// --- Full cut (no DegradationPercent) ---

func TestRunNetworkCutScenario_FullCut_NoP95(t *testing.T) {
	snap := twoNodeSnap(200, 0.02, nil)
	req := makeNetworkCutRequest([]NetworkLink{
		{SourceServiceID: "svc-a", TargetServiceID: "svc-b"},
	}, nil) // nil = full cut
	ctx := makeNetworkCutContext(req, snap)

	resp := RunNetworkCutScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %s: %s", resp.ResultStatus, resp.DeferredReason)
	}

	// Find RPS BAV
	var rpsBAV *BeforeAfterValue
	for i := range resp.BeforeAfterValues {
		if strings.HasSuffix(resp.BeforeAfterValues[i].FieldRef, ".rps") {
			rpsBAV = &resp.BeforeAfterValues[i]
		}
	}
	if rpsBAV == nil {
		t.Fatal("expected a .rps BeforeAfterValue")
	}
	if *rpsBAV.BeforeValue != 200 {
		t.Errorf("before RPS = %f, want 200", *rpsBAV.BeforeValue)
	}
	if *rpsBAV.AfterValue != 0 {
		t.Errorf("after RPS = %f, want 0 for full cut", *rpsBAV.AfterValue)
	}

	// Find error_rate BAV
	var errBAV *BeforeAfterValue
	for i := range resp.BeforeAfterValues {
		if strings.HasSuffix(resp.BeforeAfterValues[i].FieldRef, ".error_rate") {
			errBAV = &resp.BeforeAfterValues[i]
		}
	}
	if errBAV == nil {
		t.Fatal("expected a .error_rate BeforeAfterValue")
	}
	if *errBAV.AfterValue != 1.0 {
		t.Errorf("after error rate = %f, want 1.0 for full cut", *errBAV.AfterValue)
	}

	// No latency BAV when P95 is nil
	for _, bav := range resp.BeforeAfterValues {
		if strings.HasSuffix(bav.FieldRef, ".latency_p95_ms") {
			t.Error("did not expect latency BAV when edge has no P95 data")
		}
	}

	// Recommendation action
	if resp.Recommendation.Action != "implement_failover_routing_and_circuit_breakers" {
		t.Errorf("unexpected action: %s", resp.Recommendation.Action)
	}
}

func TestRunNetworkCutScenario_FullCut_100Pct(t *testing.T) {
	snap := twoNodeSnap(50, 0.0, nil)
	req := makeNetworkCutRequest([]NetworkLink{
		{SourceServiceID: "svc-a", TargetServiceID: "svc-b"},
	}, p64(100.0)) // 100% = full cut
	ctx := makeNetworkCutContext(req, snap)

	resp := RunNetworkCutScenario(ctx)
	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK for 100%% degradation, got %s", resp.ResultStatus)
	}

	for _, bav := range resp.BeforeAfterValues {
		if strings.HasSuffix(bav.FieldRef, ".rps") && *bav.AfterValue != 0 {
			t.Errorf("after RPS should be 0 for 100%% cut, got %f", *bav.AfterValue)
		}
	}
}

// --- Partial degradation ---

func TestRunNetworkCutScenario_PartialDegradation_30Pct(t *testing.T) {
	p95 := 100.0
	snap := twoNodeSnap(100, 0.0, &p95)
	req := makeNetworkCutRequest([]NetworkLink{
		{SourceServiceID: "svc-a", TargetServiceID: "svc-b"},
	}, p64(30.0))
	ctx := makeNetworkCutContext(req, snap)

	resp := RunNetworkCutScenario(ctx)
	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %s", resp.ResultStatus)
	}

	// after_rps = 100 × (1 - 0.30) = 70
	for _, bav := range resp.BeforeAfterValues {
		if strings.HasSuffix(bav.FieldRef, ".rps") {
			if *bav.AfterValue != 70.0 {
				t.Errorf("after RPS = %f, want 70.0 for 30%% degradation", *bav.AfterValue)
			}
		}
		// after_latency = 100 × (1 + 0.30) = 130
		if strings.HasSuffix(bav.FieldRef, ".latency_p95_ms") {
			if *bav.AfterValue != 130.0 {
				t.Errorf("after latency = %f, want 130.0 for 30%% degradation", *bav.AfterValue)
			}
		}
	}

	// Recommendation: 30% < 50% → monitor_and_apply_traffic_shaping
	if resp.Recommendation.Action != "monitor_and_apply_traffic_shaping" {
		t.Errorf("unexpected action: %s", resp.Recommendation.Action)
	}
}

func TestRunNetworkCutScenario_PartialDegradation_60Pct_HighSeverity(t *testing.T) {
	snap := twoNodeSnap(200, 0.0, nil)
	req := makeNetworkCutRequest([]NetworkLink{
		{SourceServiceID: "svc-a", TargetServiceID: "svc-b"},
	}, p64(60.0))
	ctx := makeNetworkCutContext(req, snap)

	resp := RunNetworkCutScenario(ctx)
	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %s", resp.ResultStatus)
	}
	// 60% >= 50% → apply_circuit_breaker_with_retry_and_monitor
	if resp.Recommendation.Action != "apply_circuit_breaker_with_retry_and_monitor" {
		t.Errorf("unexpected action: %s", resp.Recommendation.Action)
	}
}

func TestRunNetworkCutScenario_PartialDegradation_NoP95_NoLatencyBAV(t *testing.T) {
	// No P95 data → latency BAV should not appear
	snap := twoNodeSnap(100, 0.0, nil)
	req := makeNetworkCutRequest([]NetworkLink{
		{SourceServiceID: "svc-a", TargetServiceID: "svc-b"},
	}, p64(20.0))
	ctx := makeNetworkCutContext(req, snap)

	resp := RunNetworkCutScenario(ctx)
	for _, bav := range resp.BeforeAfterValues {
		if strings.HasSuffix(bav.FieldRef, ".latency_p95_ms") {
			t.Error("latency BAV should not appear when edge has no P95 data")
		}
	}
}

// --- Impacted services and paths ---

func TestRunNetworkCutScenario_ImpactedServicesRoles(t *testing.T) {
	snap := twoNodeSnap(100, 0.01, nil)
	req := makeNetworkCutRequest([]NetworkLink{
		{SourceServiceID: "svc-a", TargetServiceID: "svc-b"},
	}, nil)
	ctx := makeNetworkCutContext(req, snap)

	resp := RunNetworkCutScenario(ctx)

	roles := map[string]string{}
	for _, svc := range resp.ImpactedServices {
		roles[svc.ServiceID] = svc.Role
	}
	if roles["svc-a"] != "cut_source" {
		t.Errorf("svc-a should be cut_source, got %q", roles["svc-a"])
	}
	if roles["svc-b"] != "cut_target" {
		t.Errorf("svc-b should be cut_target, got %q", roles["svc-b"])
	}
}

func TestRunNetworkCutScenario_ImpactedPaths(t *testing.T) {
	snap := twoNodeSnap(100, 0.0, nil)
	req := makeNetworkCutRequest([]NetworkLink{
		{SourceServiceID: "svc-a", TargetServiceID: "svc-b"},
	}, nil)
	ctx := makeNetworkCutContext(req, snap)

	resp := RunNetworkCutScenario(ctx)

	if len(resp.ImpactedPaths) != 1 {
		t.Fatalf("expected 1 impacted path, got %d", len(resp.ImpactedPaths))
	}
	path := resp.ImpactedPaths[0].Path
	if len(path) != 2 || path[0] != "svc-a" || path[1] != "svc-b" {
		t.Errorf("unexpected path: %v", path)
	}
}

// --- Multiple links ---

func TestRunNetworkCutScenario_MultipleLinks(t *testing.T) {
	snap := makeSnapshotFromInput(
		[]SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "A", Namespace: "default"},
			{ServiceID: "svc-b", Name: "B", Namespace: "default"},
			{ServiceID: "svc-c", Name: "C", Namespace: "default"},
		},
		[]SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-b", RateRPS: 100, ErrorRate: 0.01},
			{SourceServiceID: "svc-b", TargetServiceID: "svc-c", RateRPS: 50, ErrorRate: 0.02},
		},
		nil,
	)
	req := makeNetworkCutRequest([]NetworkLink{
		{SourceServiceID: "svc-a", TargetServiceID: "svc-b"},
		{SourceServiceID: "svc-b", TargetServiceID: "svc-c"},
	}, nil)
	ctx := makeNetworkCutContext(req, snap)

	resp := RunNetworkCutScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK, got %s", resp.ResultStatus)
	}
	if len(resp.ImpactedPaths) != 2 {
		t.Errorf("expected 2 impacted paths, got %d", len(resp.ImpactedPaths))
	}
	// Unique services: svc-a(cut_source), svc-b(cut_source or cut_target), svc-c(cut_target)
	if len(resp.ImpactedServices) != 3 {
		t.Errorf("expected 3 unique impacted services, got %d", len(resp.ImpactedServices))
	}
}

func TestRunNetworkCutScenario_MixedLinksPartialMatch(t *testing.T) {
	snap := twoNodeSnap(100, 0.0, nil)
	req := makeNetworkCutRequest([]NetworkLink{
		{SourceServiceID: "svc-a", TargetServiceID: "svc-b"}, // exists
		{SourceServiceID: "svc-x", TargetServiceID: "svc-y"}, // missing
	}, nil)
	ctx := makeNetworkCutContext(req, snap)

	resp := RunNetworkCutScenario(ctx)
	// Should be OK since at least one link matched
	if resp.ResultStatus != ResultStatusOK {
		t.Fatalf("expected OK for partial match, got %s", resp.ResultStatus)
	}
	// Missing link mentioned in explanation
	if !strings.Contains(resp.Recommendation.Explanation, "1 declared link(s) were not found") {
		t.Errorf("explanation should note missing link count, got: %s", resp.Recommendation.Explanation)
	}
}

// --- Assumptions ---

func TestRunNetworkCutScenario_AssumptionsPresent(t *testing.T) {
	snap := twoNodeSnap(100, 0.0, nil)
	req := makeNetworkCutRequest([]NetworkLink{
		{SourceServiceID: "svc-a", TargetServiceID: "svc-b"},
	}, nil)
	ctx := makeNetworkCutContext(req, snap)

	resp := RunNetworkCutScenario(ctx)
	keys := map[string]bool{}
	for _, a := range resp.Assumptions {
		keys[a.Key] = true
	}
	required := []string{
		"network_cut.degradation_mode",
		"network_cut.error_rate_model",
		"network_cut.latency_model",
		"edge_data.source",
	}
	for _, k := range required {
		if !keys[k] {
			t.Errorf("assumption %q missing from response", k)
		}
	}
}

// --- Determinism ---

func TestRunNetworkCutScenario_Deterministic(t *testing.T) {
	p95 := 80.0
	snap := twoNodeSnap(150, 0.05, &p95)
	req := makeNetworkCutRequest([]NetworkLink{
		{SourceServiceID: "svc-a", TargetServiceID: "svc-b"},
	}, p64(25.0))
	ctx := makeNetworkCutContext(req, snap)

	resp1 := RunNetworkCutScenario(ctx)
	resp2 := RunNetworkCutScenario(ctx)

	c1, err1 := CanonicalizeResponse(resp1)
	c2, err2 := CanonicalizeResponse(resp2)
	if err1 != nil || err2 != nil {
		t.Fatalf("canonicalize errors: %v, %v", err1, err2)
	}
	if string(c1) != string(c2) {
		t.Errorf("responses are not deterministic:\n%s\n---\n%s", c1, c2)
	}
}

// --- Evidence fields ---

func TestRunNetworkCutScenario_EvidenceFieldsPopulated(t *testing.T) {
	snap := twoNodeSnap(100, 0.0, nil)
	req := makeNetworkCutRequest([]NetworkLink{
		{SourceServiceID: "svc-a", TargetServiceID: "svc-b"},
	}, nil)
	ctx := makeNetworkCutContextWithInflux(req, snap)

	resp := RunNetworkCutScenario(ctx)
	if resp.EvidenceMode == "" {
		t.Error("EvidenceMode must be populated")
	}
	if resp.ConfidenceLevel == "" {
		t.Error("ConfidenceLevel must be populated")
	}
	if len(resp.EvidenceSources) == 0 {
		t.Error("EvidenceSources must not be empty")
	}
}

// --- FieldRef format ---

func TestRunNetworkCutScenario_FieldRefFormat(t *testing.T) {
	snap := twoNodeSnap(100, 0.0, nil)
	req := makeNetworkCutRequest([]NetworkLink{
		{SourceServiceID: "svc-a", TargetServiceID: "svc-b"},
	}, nil)
	ctx := makeNetworkCutContext(req, snap)

	resp := RunNetworkCutScenario(ctx)
	for _, bav := range resp.BeforeAfterValues {
		if !strings.HasPrefix(bav.FieldRef, "network.link.svc-a.svc-b.") {
			t.Errorf("unexpected FieldRef prefix: %s", bav.FieldRef)
		}
	}
}
