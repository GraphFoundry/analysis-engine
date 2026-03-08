package simulation

// US-022: Validate Traffic Spike / targeted load scenario on real VMs
//
// This file implements reproducible validation test cases for the Traffic Spike /
// targeted load scenario model. The topology reuses the microservice-test-bed cluster
// defined in failure_vm_validation_test.go (buildVMSnapshot):
//
//   api-gateway  ──►  order-service  ──►  payment-service
//                           │         ──►  user-service
//                           │         ──►  inventory-service
//                           └─────────►  notification-service
//
// Primary test case: simulate a 2× load spike on order-service and verify
// incoming_rps, latency_p95_ms BAVs, impacted services/paths, and the
// monitor_and_prepare_rate_limits recommendation match analytically expected outcomes.
//
// Secondary test case: simulate a 4× load spike on order-service (high severity)
// and verify the pre_emptive_scale_up_required recommendation is returned.
//
// Pass/fail criteria are explicit assertions; any divergence from expected outcomes
// marks the scenario as NOT validated.

import (
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// Traffic Spike VM validation case types
// ---------------------------------------------------------------------------

// trafficSpikeVMValidationCase captures expected outcomes for a traffic spike VM test case.
type trafficSpikeVMValidationCase struct {
	// Expected impacted service IDs and their roles.
	ExpectedImpactedServices map[string]string // serviceID → role

	// Expected impacted path signatures (service IDs joined by "→").
	ExpectedImpactedPathSigs []string

	// Expected incoming_rps BAV.
	ExpectedIncomingRPSBefore float64
	ExpectedIncomingRPSAfter  float64
	ExpectedIncomingRPSDelta  float64

	// Expected latency_p95_ms BAV (nil = omitted because no P95 data).
	ExpectedLatencyBefore *float64
	ExpectedLatencyAfter  *float64
	ExpectedLatencyDelta  *float64

	// Expected recommendation action.
	ExpectedRecommendationAction string

	// Expected result status.
	ExpectedResultStatus SimulationResultStatus
}

// ---------------------------------------------------------------------------
// Moderate spike case: order-service 2× load multiplier
// ---------------------------------------------------------------------------

// buildModSpikeRequest builds the deterministic 2× spike request for the VM validation case.
func buildModSpikeRequest(snap SimulationSnapshot) SimulationRequest {
	return SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioTrafficSpike,
		SnapshotTimestamp: snap.SnapshotTimestamp,
		SnapshotHash:      snap.SnapshotHash,
		TrafficSpikeParams: &TrafficSpikeParams{
			TargetServiceID: vmTargetService, // svc-order
			LoadMultiplier:  2.0,
		},
	}
}

// buildExpectedModSpikeOutcomes returns the analytically expected outcomes for the
// 2× traffic spike VM test case on order-service.
//
// Incoming edge to order-service: api-gw → order-service, RPS=200, P95=45 ms.
//
//   - incoming_rps: before=200, after=200×2=400, delta=+200
//   - latency_p95_ms: before=45.0, after=45.0×2=90.0, delta=+45.0
//   - ImpactedServices: svc-order (target), svc-api-gw (caller), 4 downstreams
//   - ImpactedPaths: 1 incoming + 4 outgoing = 5 paths
//   - Recommendation: monitor_and_prepare_rate_limits (2× < 3× threshold)
func buildExpectedModSpikeOutcomes() trafficSpikeVMValidationCase {
	latBefore := 45.0
	latAfter := 90.0 // 45.0 × 2.0 = 90.0
	latDelta := 45.0

	return trafficSpikeVMValidationCase{
		ExpectedImpactedServices: map[string]string{
			vmTargetService:       "target",
			vmAPIGateway:          "caller",
			vmPaymentService:      "downstream",
			vmUserService:         "downstream",
			vmInventoryService:    "downstream",
			vmNotificationService: "downstream",
		},
		ExpectedImpactedPathSigs: []string{
			"svc-api-gw→svc-order",
			"svc-order→svc-payment",
			"svc-order→svc-user",
			"svc-order→svc-inventory",
			"svc-order→svc-notification",
		},
		ExpectedIncomingRPSBefore:    200,
		ExpectedIncomingRPSAfter:     400,
		ExpectedIncomingRPSDelta:     200,
		ExpectedLatencyBefore:        &latBefore,
		ExpectedLatencyAfter:         &latAfter,
		ExpectedLatencyDelta:         &latDelta,
		ExpectedRecommendationAction: "monitor_and_prepare_rate_limits",
		ExpectedResultStatus:         ResultStatusOK,
	}
}

// ---------------------------------------------------------------------------
// High-severity spike case: order-service 4× load multiplier
// ---------------------------------------------------------------------------

// buildHighSpikeRequest builds the deterministic 4× spike request for the VM validation case.
func buildHighSpikeRequest(snap SimulationSnapshot) SimulationRequest {
	return SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioTrafficSpike,
		SnapshotTimestamp: snap.SnapshotTimestamp,
		SnapshotHash:      snap.SnapshotHash,
		TrafficSpikeParams: &TrafficSpikeParams{
			TargetServiceID: vmTargetService, // svc-order
			LoadMultiplier:  4.0,
		},
	}
}

// buildExpectedHighSpikeOutcomes returns the expected outcomes for 4× load multiplier.
//
// 4.0 >= 3.0 threshold → pre_emptive_scale_up_required.
// incoming_rps: before=200, after=200×4=800, delta=+600.
// latency_p95_ms: before=45.0, after=45.0×4=180.0, delta=+135.0.
func buildExpectedHighSpikeOutcomes() trafficSpikeVMValidationCase {
	latBefore := 45.0
	latAfter := 180.0 // 45.0 × 4.0 = 180.0
	latDelta := 135.0

	return trafficSpikeVMValidationCase{
		ExpectedImpactedServices: map[string]string{
			vmTargetService:       "target",
			vmAPIGateway:          "caller",
			vmPaymentService:      "downstream",
			vmUserService:         "downstream",
			vmInventoryService:    "downstream",
			vmNotificationService: "downstream",
		},
		ExpectedImpactedPathSigs: []string{
			"svc-api-gw→svc-order",
			"svc-order→svc-payment",
			"svc-order→svc-user",
			"svc-order→svc-inventory",
			"svc-order→svc-notification",
		},
		ExpectedIncomingRPSBefore:    200,
		ExpectedIncomingRPSAfter:     800,
		ExpectedIncomingRPSDelta:     600,
		ExpectedLatencyBefore:        &latBefore,
		ExpectedLatencyAfter:         &latAfter,
		ExpectedLatencyDelta:         &latDelta,
		ExpectedRecommendationAction: "pre_emptive_scale_up_required",
		ExpectedResultStatus:         ResultStatusOK,
	}
}

// ---------------------------------------------------------------------------
// US-022 primary VM validation test: moderate spike (2×)
// ---------------------------------------------------------------------------

// TestUS022_TrafficSpike_Moderate_VMValidation is the primary reproducible VM
// validation test case for US-022 covering the moderate (2×) traffic spike.
// It asserts every expected vs observed outcome for panel defensibility.
func TestUS022_TrafficSpike_Moderate_VMValidation(t *testing.T) {
	snap := buildVMSnapshot()
	req := buildModSpikeRequest(snap)
	ctx := BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})
	expected := buildExpectedModSpikeOutcomes()

	resp := RunTrafficSpikeScenario(ctx)

	t.Run("ResultStatus", func(t *testing.T) {
		if resp.ResultStatus != expected.ExpectedResultStatus {
			t.Errorf("expected ResultStatus=%q, got=%q", expected.ExpectedResultStatus, resp.ResultStatus)
		}
	})

	t.Run("ImpactedServices_Count", func(t *testing.T) {
		if len(resp.ImpactedServices) != len(expected.ExpectedImpactedServices) {
			t.Errorf("expected %d impacted services, got %d: %v",
				len(expected.ExpectedImpactedServices),
				len(resp.ImpactedServices),
				resp.ImpactedServices,
			)
		}
	})

	t.Run("ImpactedServices_Roles", func(t *testing.T) {
		observed := map[string]string{}
		for _, svc := range resp.ImpactedServices {
			observed[svc.ServiceID] = svc.Role
		}
		for svcID, expectedRole := range expected.ExpectedImpactedServices {
			if got, ok := observed[svcID]; !ok {
				t.Errorf("expected service %q to be impacted, but not found in response", svcID)
			} else if got != expectedRole {
				t.Errorf("service %q: expected role=%q, got=%q", svcID, expectedRole, got)
			}
		}
	})

	t.Run("ImpactedPaths_Count", func(t *testing.T) {
		if len(resp.ImpactedPaths) != len(expected.ExpectedImpactedPathSigs) {
			t.Errorf("expected %d impacted paths, got %d",
				len(expected.ExpectedImpactedPathSigs),
				len(resp.ImpactedPaths),
			)
			for _, p := range resp.ImpactedPaths {
				t.Logf("  observed path: %s", pathSig(p))
			}
		}
	})

	t.Run("ImpactedPaths_Signatures", func(t *testing.T) {
		observedSigs := map[string]bool{}
		for _, p := range resp.ImpactedPaths {
			observedSigs[pathSig(p)] = true
		}
		for _, sig := range expected.ExpectedImpactedPathSigs {
			if !observedSigs[sig] {
				t.Errorf("expected path signature %q not found in response", sig)
			}
		}
	})

	t.Run("BAV_IncomingRPS", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, "spike.target.incoming_rps")
		if bav == nil {
			t.Fatal("spike.target.incoming_rps not found in BeforeAfterValues")
		}
		if bav.BeforeValue == nil || *bav.BeforeValue != expected.ExpectedIncomingRPSBefore {
			t.Errorf("incoming_rps before: expected=%.2f, got=%v", expected.ExpectedIncomingRPSBefore, bav.BeforeValue)
		}
		if bav.AfterValue == nil || *bav.AfterValue != expected.ExpectedIncomingRPSAfter {
			t.Errorf("incoming_rps after: expected=%.2f, got=%v", expected.ExpectedIncomingRPSAfter, bav.AfterValue)
		}
		if bav.DeltaValue == nil || *bav.DeltaValue != expected.ExpectedIncomingRPSDelta {
			t.Errorf("incoming_rps delta: expected=%.2f, got=%v", expected.ExpectedIncomingRPSDelta, bav.DeltaValue)
		}
	})

	t.Run("BAV_LatencyP95", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, "spike.target.latency_p95_ms")
		if expected.ExpectedLatencyBefore == nil {
			if bav != nil {
				t.Error("expected latency_p95_ms BAV to be absent when no P95 data, but it was present")
			}
			return
		}
		if bav == nil {
			t.Fatal("spike.target.latency_p95_ms not found in BeforeAfterValues")
		}
		if bav.BeforeValue == nil || *bav.BeforeValue != *expected.ExpectedLatencyBefore {
			t.Errorf("latency_p95_ms before: expected=%.2f, got=%v", *expected.ExpectedLatencyBefore, bav.BeforeValue)
		}
		if bav.AfterValue == nil || *bav.AfterValue != *expected.ExpectedLatencyAfter {
			t.Errorf("latency_p95_ms after: expected=%.2f, got=%v", *expected.ExpectedLatencyAfter, bav.AfterValue)
		}
	})

	t.Run("Recommendation_Action", func(t *testing.T) {
		if resp.Recommendation.Action != expected.ExpectedRecommendationAction {
			t.Errorf("recommendation action: expected=%q, observed=%q",
				expected.ExpectedRecommendationAction,
				resp.Recommendation.Action,
			)
		}
	})

	t.Run("Recommendation_ExplanationNonEmpty", func(t *testing.T) {
		if resp.Recommendation.Explanation == "" {
			t.Error("recommendation explanation must not be empty")
		}
	})

	t.Run("Assumptions_Required", func(t *testing.T) {
		keys := map[string]bool{}
		for _, a := range resp.Assumptions {
			keys[a.Key] = true
		}
		for _, required := range []string{
			"spike.linear_latency_model",
			"spike.rps_linear_scale",
			"edge_data.source",
		} {
			if !keys[required] {
				t.Errorf("required assumption key %q not found", required)
			}
		}
	})

	t.Run("EvidenceFields_Populated", func(t *testing.T) {
		if resp.SnapshotHash == "" {
			t.Error("SnapshotHash must not be empty")
		}
		if resp.SnapshotTimestamp == "" {
			t.Error("SnapshotTimestamp must not be empty")
		}
		if resp.EvidenceMode == "" {
			t.Error("EvidenceMode must not be empty")
		}
		if resp.ConfidenceLevel == "" {
			t.Error("ConfidenceLevel must not be empty")
		}
	})

	t.Run("ResponsePassesContractValidation", func(t *testing.T) {
		if err := ValidateSimulationResponse(resp); err != nil {
			t.Errorf("response failed contract validation: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// US-022 high-severity spike validation test (4×)
// ---------------------------------------------------------------------------

// TestUS022_TrafficSpike_HighSeverity_VMValidation validates the pre_emptive_scale_up_required
// recommendation path when load multiplier is 4× on the VM topology.
func TestUS022_TrafficSpike_HighSeverity_VMValidation(t *testing.T) {
	snap := buildVMSnapshot()
	req := buildHighSpikeRequest(snap)
	ctx := BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})
	expected := buildExpectedHighSpikeOutcomes()

	resp := RunTrafficSpikeScenario(ctx)

	t.Run("ResultStatus", func(t *testing.T) {
		if resp.ResultStatus != ResultStatusOK {
			t.Errorf("expected ResultStatus=OK, got=%q", resp.ResultStatus)
		}
	})

	t.Run("Recommendation_PreEmptiveScaleUp", func(t *testing.T) {
		if resp.Recommendation.Action != expected.ExpectedRecommendationAction {
			t.Errorf("expected recommendation=%q, got=%q",
				expected.ExpectedRecommendationAction, resp.Recommendation.Action)
		}
	})

	t.Run("BAV_IncomingRPS_HighSpike", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, "spike.target.incoming_rps")
		if bav == nil {
			t.Fatal("spike.target.incoming_rps not found")
		}
		if bav.AfterValue == nil || *bav.AfterValue != expected.ExpectedIncomingRPSAfter {
			t.Errorf("incoming_rps after: expected=%.2f, got=%v",
				expected.ExpectedIncomingRPSAfter, bav.AfterValue)
		}
	})

	t.Run("BAV_LatencyP95_HighSpike", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, "spike.target.latency_p95_ms")
		if bav == nil {
			t.Fatal("spike.target.latency_p95_ms not found")
		}
		if bav.AfterValue == nil || *bav.AfterValue != *expected.ExpectedLatencyAfter {
			t.Errorf("latency_p95_ms after: expected=%.2f, got=%v",
				*expected.ExpectedLatencyAfter, bav.AfterValue)
		}
	})

	t.Run("ContractValidation", func(t *testing.T) {
		if err := ValidateSimulationResponse(resp); err != nil {
			t.Errorf("response failed contract validation: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// US-022 determinism test
// ---------------------------------------------------------------------------

// TestUS022_TrafficSpike_Determinism verifies two identical runs produce byte-equivalent
// canonical JSON output — required for panel replay demonstration.
func TestUS022_TrafficSpike_Determinism(t *testing.T) {
	snap := buildVMSnapshot()
	req := buildModSpikeRequest(snap)
	ctx := BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})

	resp1 := RunTrafficSpikeScenario(ctx)
	resp2 := RunTrafficSpikeScenario(ctx)

	b1, err1 := CanonicalizeResponse(resp1)
	b2, err2 := CanonicalizeResponse(resp2)
	if err1 != nil || err2 != nil {
		t.Fatalf("canonicalization error: %v / %v", err1, err2)
	}
	if string(b1) != string(b2) {
		t.Errorf("non-deterministic output detected:\nrun1: %s\nrun2: %s", b1, b2)
	}
}

// ---------------------------------------------------------------------------
// US-022 degraded-mode without Influx test
// ---------------------------------------------------------------------------

// TestUS022_TrafficSpike_DegradedModeWithoutInflux verifies that the scenario produces a
// valid result and a non-none degraded-mode label when InfluxDB is unavailable.
func TestUS022_TrafficSpike_DegradedModeWithoutInflux(t *testing.T) {
	snap := buildVMSnapshot()
	req := buildModSpikeRequest(snap)
	ctx := BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})

	resp := RunTrafficSpikeScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Errorf("expected OK even without Influx, got %q", resp.ResultStatus)
	}
	if resp.DegradedMode == DegradedModeNone {
		t.Error("expected non-empty DegradedMode when Influx is unavailable")
	}
	if len(resp.ImpactedServices) == 0 {
		t.Error("expected impacted services even in degraded mode")
	}
}

// ---------------------------------------------------------------------------
// US-022 validation report
// ---------------------------------------------------------------------------

// TestUS022_TrafficSpike_ValidationReport logs a structured validation report to test
// output for artifact capture. The report covers both moderate and high-severity spike cases.
func TestUS022_TrafficSpike_ValidationReport(t *testing.T) {
	snap := buildVMSnapshot()

	// --- Moderate spike case (2×) ---
	reqMod := buildModSpikeRequest(snap)
	ctxMod := BuildExecutionContext(reqMod, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})
	expectedMod := buildExpectedModSpikeOutcomes()
	respMod := RunTrafficSpikeScenario(ctxMod)

	observedPathSigsMod := make([]string, len(respMod.ImpactedPaths))
	for i, p := range respMod.ImpactedPaths {
		observedPathSigsMod[i] = pathSig(p)
	}
	sort.Strings(observedPathSigsMod)

	t.Logf("=== US-022 VM Validation Report: Traffic Spike / targeted load ===")
	t.Logf("Snapshot Hash  : %s", snap.SnapshotHash)
	t.Logf("Snapshot Time  : %s", snap.SnapshotTimestamp)
	t.Logf("")

	t.Logf("--- Case 1: Moderate Spike (2×) ---")
	t.Logf("Evidence Mode  : %s", respMod.EvidenceMode)
	t.Logf("Confidence     : %s", respMod.ConfidenceLevel)
	t.Logf("Degraded Mode  : %q", respMod.DegradedMode)
	t.Logf("")
	t.Logf("Impacted Services:")
	for _, svc := range respMod.ImpactedServices {
		t.Logf("  [%s] %s (%s)", svc.Role, svc.ServiceID, svc.Name)
	}
	t.Logf("Impacted Paths:")
	for _, sig := range observedPathSigsMod {
		t.Logf("  %s", sig)
	}
	t.Logf("Before/After Values:")
	for _, bav := range respMod.BeforeAfterValues {
		t.Logf("  %-45s before=%-10s after=%-10s delta=%s",
			bav.FieldRef,
			formatFloatPtr(bav.BeforeValue),
			formatFloatPtr(bav.AfterValue),
			formatFloatPtr(bav.DeltaValue),
		)
	}
	t.Logf("Recommendation : %s", respMod.Recommendation.Action)
	t.Logf("")

	// --- High-severity spike case (4×) ---
	reqHigh := buildHighSpikeRequest(snap)
	ctxHigh := BuildExecutionContext(reqHigh, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})
	expectedHigh := buildExpectedHighSpikeOutcomes()
	respHigh := RunTrafficSpikeScenario(ctxHigh)

	t.Logf("--- Case 2: High-Severity Spike (4×) ---")
	t.Logf("Recommendation : %s", respHigh.Recommendation.Action)
	t.Logf("Before/After Values:")
	for _, bav := range respHigh.BeforeAfterValues {
		t.Logf("  %-45s before=%-10s after=%-10s delta=%s",
			bav.FieldRef,
			formatFloatPtr(bav.BeforeValue),
			formatFloatPtr(bav.AfterValue),
			formatFloatPtr(bav.DeltaValue),
		)
	}
	t.Logf("")

	// --- Pass/fail criteria ---
	latModAfterRef := expectedMod.ExpectedLatencyAfter
	latHighAfterRef := expectedHigh.ExpectedLatencyAfter

	criteria := []struct {
		Name   string
		Passed bool
	}{
		{"[mod-spike] ResultStatus == OK", respMod.ResultStatus == ResultStatusOK},
		{"[mod-spike] ImpactedServices count correct",
			len(respMod.ImpactedServices) == len(expectedMod.ExpectedImpactedServices)},
		{"[mod-spike] ImpactedPaths count correct",
			len(respMod.ImpactedPaths) == len(expectedMod.ExpectedImpactedPathSigs)},
		{"[mod-spike] incoming_rps before=200",
			bavMatchesBefore(respMod.BeforeAfterValues, "spike.target.incoming_rps", 200)},
		{"[mod-spike] incoming_rps after=400",
			bavMatchesAfter(respMod.BeforeAfterValues, "spike.target.incoming_rps", 400)},
		{"[mod-spike] latency_p95_ms before=45.0",
			bavMatchesBefore(respMod.BeforeAfterValues, "spike.target.latency_p95_ms", 45.0)},
		{"[mod-spike] latency_p95_ms after=90.0", func() bool {
			return latModAfterRef != nil &&
				bavMatchesAfter(respMod.BeforeAfterValues, "spike.target.latency_p95_ms", *latModAfterRef)
		}()},
		{"[mod-spike] recommendation == monitor_and_prepare_rate_limits",
			respMod.Recommendation.Action == "monitor_and_prepare_rate_limits"},
		{"[mod-spike] contract validation passes",
			func() bool { return ValidateSimulationResponse(respMod) == nil }()},
		{"[high-spike] ResultStatus == OK", respHigh.ResultStatus == ResultStatusOK},
		{"[high-spike] incoming_rps after=800",
			bavMatchesAfter(respHigh.BeforeAfterValues, "spike.target.incoming_rps", 800)},
		{"[high-spike] latency_p95_ms after=180.0", func() bool {
			return latHighAfterRef != nil &&
				bavMatchesAfter(respHigh.BeforeAfterValues, "spike.target.latency_p95_ms", *latHighAfterRef)
		}()},
		{"[high-spike] recommendation == pre_emptive_scale_up_required",
			respHigh.Recommendation.Action == "pre_emptive_scale_up_required"},
		{"[high-spike] contract validation passes",
			func() bool { return ValidateSimulationResponse(respHigh) == nil }()},
	}

	t.Logf("--- Pass/Fail Summary ---")
	allPass := true
	for _, c := range criteria {
		status := "PASS"
		if !c.Passed {
			status = "FAIL"
			allPass = false
		}
		t.Logf("  [%s] %s", status, c.Name)
	}

	t.Logf("")
	if allPass {
		t.Logf("OVERALL: PASS — Traffic Spike scenario is panel-defensible on real VM topology")
	} else {
		t.Errorf("OVERALL: FAIL — one or more validation criteria did not match expected outcomes")
	}
}
