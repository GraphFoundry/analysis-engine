package simulation

// US-021: Validate Scaling up/down scenario on real VMs
//
// This file implements reproducible validation test cases for the Scaling up/down
// scenario model.  The topology reuses the microservice-test-bed cluster defined
// in failure_vm_validation_test.go (buildVMSnapshot):
//
//   api-gateway  ──►  order-service  ──►  payment-service
//                           │         ──►  user-service
//                           │         ──►  inventory-service
//                           └─────────►  notification-service
//
// Primary test case: scale order-service from 5 → 10 pods (scale-up) and verify
// pod_count, rps_capacity, latency_p95_ms BAVs, impacted services/paths, and the
// approve_scale_up recommendation match analytically expected outcomes.
//
// Secondary test case: scale order-service from 5 → 3 pods (significant scale-down)
// and verify the caution_scale_down recommendation and correct BAVs are produced.
//
// Pass/fail criteria are explicit assertions; any divergence from expected outcomes
// marks the scenario as NOT validated.

import (
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// Scaling VM validation case types
// ---------------------------------------------------------------------------

// scalingVMValidationCase captures expected outcomes for a scaling VM test case.
type scalingVMValidationCase struct {
	// Expected impacted service IDs and their roles.
	ExpectedImpactedServices map[string]string // serviceID → role

	// Expected impacted path signatures (service IDs joined by "→").
	ExpectedImpactedPathSigs []string

	// Expected pod_count BAV.
	ExpectedPodCountBefore float64
	ExpectedPodCountAfter  float64
	ExpectedPodCountDelta  float64

	// Expected rps_capacity BAV.
	ExpectedRPSCapacityBefore float64
	ExpectedRPSCapacityAfter  float64
	ExpectedRPSCapacityDelta  float64

	// Expected latency_p95_ms BAV (nil = omitted because no P95 data).
	ExpectedLatencyBefore *float64
	ExpectedLatencyAfter  *float64

	// Expected recommendation action.
	ExpectedRecommendationAction string

	// Expected result status.
	ExpectedResultStatus SimulationResultStatus
}

// ---------------------------------------------------------------------------
// Scale-up case: order-service 5 → 10 pods
// ---------------------------------------------------------------------------

// buildScaleUpRequest builds the deterministic scale-up request for the VM validation case.
func buildScaleUpRequest(snap SimulationSnapshot) SimulationRequest {
	return SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioScaling,
		SnapshotTimestamp: snap.SnapshotTimestamp,
		SnapshotHash:      snap.SnapshotHash,
		ScalingParams: &ScalingParams{
			TargetServiceID: vmTargetService, // svc-order
			CurrentPods:     5,
			NewPods:         10,
			LatencyMetric:   "p95",
		},
	}
}

// buildExpectedScaleUpOutcomes returns the analytically expected outcomes for the
// scale-up VM test case (5 → 10 pods, 2× ratio).
//
// Incoming edge to order-service: api-gw → order-service, RPS=200, P95=45 ms.
//
//   - pod_count: before=5, after=10, delta=+5
//   - rps_capacity: before=200, after=200×2=400, delta=+200
//   - latency_p95_ms: before=45.0, after=45.0×(5/10)=22.5, delta=−22.5
//   - ImpactedServices: svc-order (target), svc-api-gw (caller)
//   - ImpactedPaths: 1 incoming + 4 outgoing = 5 paths
//   - Recommendation: approve_scale_up
func buildExpectedScaleUpOutcomes() scalingVMValidationCase {
	latBefore := 45.0
	latAfter := 22.5 // 45.0 × (5/10) = 22.5

	return scalingVMValidationCase{
		ExpectedImpactedServices: map[string]string{
			vmTargetService: "target",
			vmAPIGateway:    "caller",
		},
		ExpectedImpactedPathSigs: []string{
			"svc-api-gw→svc-order",
			"svc-order→svc-payment",
			"svc-order→svc-user",
			"svc-order→svc-inventory",
			"svc-order→svc-notification",
		},
		ExpectedPodCountBefore:       5,
		ExpectedPodCountAfter:        10,
		ExpectedPodCountDelta:        5,
		ExpectedRPSCapacityBefore:    200,
		ExpectedRPSCapacityAfter:     400,
		ExpectedRPSCapacityDelta:     200,
		ExpectedLatencyBefore:        &latBefore,
		ExpectedLatencyAfter:         &latAfter,
		ExpectedRecommendationAction: "approve_scale_up",
		ExpectedResultStatus:         ResultStatusOK,
	}
}

// ---------------------------------------------------------------------------
// Scale-down (caution) case: order-service 5 → 3 pods
// ---------------------------------------------------------------------------

// buildScaleDownRequest builds the deterministic caution scale-down request.
func buildScaleDownRequest(snap SimulationSnapshot) SimulationRequest {
	return SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioScaling,
		SnapshotTimestamp: snap.SnapshotTimestamp,
		SnapshotHash:      snap.SnapshotHash,
		ScalingParams: &ScalingParams{
			TargetServiceID: vmTargetService, // svc-order
			CurrentPods:     5,
			NewPods:         3,
			LatencyMetric:   "p95",
		},
	}
}

// buildExpectedScaleDownOutcomes returns the expected outcomes for 5 → 3 pods (0.6×).
//
// projectedCapacity = 200 × 0.6 = 120 < 200 × 0.8 = 160  →  caution_scale_down.
func buildExpectedScaleDownOutcomes() scalingVMValidationCase {
	latBefore := 45.0
	latAfter := 75.0 // 45.0 × (5/3) = 75.0

	return scalingVMValidationCase{
		ExpectedImpactedServices: map[string]string{
			vmTargetService: "target",
			vmAPIGateway:    "caller",
		},
		ExpectedImpactedPathSigs: []string{
			"svc-api-gw→svc-order",
			"svc-order→svc-payment",
			"svc-order→svc-user",
			"svc-order→svc-inventory",
			"svc-order→svc-notification",
		},
		ExpectedPodCountBefore:       5,
		ExpectedPodCountAfter:        3,
		ExpectedPodCountDelta:        -2,
		ExpectedRPSCapacityBefore:    200,
		ExpectedRPSCapacityAfter:     120,
		ExpectedRPSCapacityDelta:     -80,
		ExpectedLatencyBefore:        &latBefore,
		ExpectedLatencyAfter:         &latAfter,
		ExpectedRecommendationAction: "caution_scale_down",
		ExpectedResultStatus:         ResultStatusOK,
	}
}

// ---------------------------------------------------------------------------
// US-021 primary VM validation test: scale-up
// ---------------------------------------------------------------------------

// TestUS021_Scaling_ScaleUp_VMValidation is the primary reproducible VM
// validation test case for US-021 covering the scale-up direction.
// It asserts every expected vs observed outcome for panel defensibility.
func TestUS021_Scaling_ScaleUp_VMValidation(t *testing.T) {
	snap := buildVMSnapshot()
	req := buildScaleUpRequest(snap)
	ctx := BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})
	expected := buildExpectedScaleUpOutcomes()

	resp := RunScalingScenario(ctx)

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

	t.Run("BAV_PodCount", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, "scaling.target.pod_count")
		if bav == nil {
			t.Fatal("scaling.target.pod_count not found in BeforeAfterValues")
		}
		if bav.BeforeValue == nil || *bav.BeforeValue != expected.ExpectedPodCountBefore {
			t.Errorf("pod_count before: expected=%.0f, got=%v", expected.ExpectedPodCountBefore, bav.BeforeValue)
		}
		if bav.AfterValue == nil || *bav.AfterValue != expected.ExpectedPodCountAfter {
			t.Errorf("pod_count after: expected=%.0f, got=%v", expected.ExpectedPodCountAfter, bav.AfterValue)
		}
		if bav.DeltaValue == nil || *bav.DeltaValue != expected.ExpectedPodCountDelta {
			t.Errorf("pod_count delta: expected=%.0f, got=%v", expected.ExpectedPodCountDelta, bav.DeltaValue)
		}
	})

	t.Run("BAV_RPSCapacity", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, "scaling.target.rps_capacity")
		if bav == nil {
			t.Fatal("scaling.target.rps_capacity not found in BeforeAfterValues")
		}
		if bav.BeforeValue == nil || *bav.BeforeValue != expected.ExpectedRPSCapacityBefore {
			t.Errorf("rps_capacity before: expected=%.2f, got=%v", expected.ExpectedRPSCapacityBefore, bav.BeforeValue)
		}
		if bav.AfterValue == nil || *bav.AfterValue != expected.ExpectedRPSCapacityAfter {
			t.Errorf("rps_capacity after: expected=%.2f, got=%v", expected.ExpectedRPSCapacityAfter, bav.AfterValue)
		}
		if bav.DeltaValue == nil || *bav.DeltaValue != expected.ExpectedRPSCapacityDelta {
			t.Errorf("rps_capacity delta: expected=%.2f, got=%v", expected.ExpectedRPSCapacityDelta, bav.DeltaValue)
		}
	})

	t.Run("BAV_LatencyP95", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, "scaling.target.latency_p95_ms")
		if expected.ExpectedLatencyBefore == nil {
			// No P95 data — BAV should be absent.
			if bav != nil {
				t.Error("expected latency_p95_ms BAV to be absent when no P95 data, but it was present")
			}
			return
		}
		if bav == nil {
			t.Fatal("scaling.target.latency_p95_ms not found in BeforeAfterValues")
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
			"scaling.linear_rps_capacity",
			"scaling.inverse_proportional_latency",
			"scaling.direction",
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
// US-021 caution scale-down validation test
// ---------------------------------------------------------------------------

// TestUS021_Scaling_ScaleDown_Caution_VMValidation validates the caution_scale_down
// recommendation path when scaling from 5 → 3 pods on the VM topology.
func TestUS021_Scaling_ScaleDown_Caution_VMValidation(t *testing.T) {
	snap := buildVMSnapshot()
	req := buildScaleDownRequest(snap)
	ctx := BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})
	expected := buildExpectedScaleDownOutcomes()

	resp := RunScalingScenario(ctx)

	t.Run("ResultStatus", func(t *testing.T) {
		if resp.ResultStatus != ResultStatusOK {
			t.Errorf("expected ResultStatus=OK, got=%q", resp.ResultStatus)
		}
	})

	t.Run("Recommendation_CautionScaleDown", func(t *testing.T) {
		if resp.Recommendation.Action != expected.ExpectedRecommendationAction {
			t.Errorf("expected recommendation=%q, got=%q",
				expected.ExpectedRecommendationAction, resp.Recommendation.Action)
		}
	})

	t.Run("BAV_RPSCapacity_Reduced", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, "scaling.target.rps_capacity")
		if bav == nil {
			t.Fatal("scaling.target.rps_capacity not found")
		}
		if bav.AfterValue == nil || *bav.AfterValue != expected.ExpectedRPSCapacityAfter {
			t.Errorf("rps_capacity after: expected=%.2f, got=%v",
				expected.ExpectedRPSCapacityAfter, bav.AfterValue)
		}
	})

	t.Run("BAV_LatencyP95_Increased", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, "scaling.target.latency_p95_ms")
		if bav == nil {
			t.Fatal("scaling.target.latency_p95_ms not found")
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
// US-021 determinism test
// ---------------------------------------------------------------------------

// TestUS021_Scaling_Determinism verifies two identical runs produce byte-equivalent
// canonical JSON output — required for panel replay demonstration.
func TestUS021_Scaling_Determinism(t *testing.T) {
	snap := buildVMSnapshot()
	req := buildScaleUpRequest(snap)
	ctx := BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})

	resp1 := RunScalingScenario(ctx)
	resp2 := RunScalingScenario(ctx)

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
// US-021 degraded-mode without Influx test
// ---------------------------------------------------------------------------

// TestUS021_Scaling_DegradedModeWithoutInflux verifies that the scenario produces a
// valid result and a non-none degraded-mode label when InfluxDB is unavailable.
func TestUS021_Scaling_DegradedModeWithoutInflux(t *testing.T) {
	snap := buildVMSnapshot()
	req := buildScaleUpRequest(snap)
	ctx := BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})

	resp := RunScalingScenario(ctx)

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
// US-021 validation report
// ---------------------------------------------------------------------------

// TestUS021_Scaling_ValidationReport logs a structured validation report to test
// output for artifact capture.  The report covers both scale-up and scale-down cases.
func TestUS021_Scaling_ValidationReport(t *testing.T) {
	snap := buildVMSnapshot()

	// --- Scale-up case ---
	reqUp := buildScaleUpRequest(snap)
	ctxUp := BuildExecutionContext(reqUp, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})
	expectedUp := buildExpectedScaleUpOutcomes()
	respUp := RunScalingScenario(ctxUp)

	observedPathSigsUp := make([]string, len(respUp.ImpactedPaths))
	for i, p := range respUp.ImpactedPaths {
		observedPathSigsUp[i] = pathSig(p)
	}
	sort.Strings(observedPathSigsUp)

	t.Logf("=== US-021 VM Validation Report: Scaling up/down ===")
	t.Logf("Snapshot Hash  : %s", snap.SnapshotHash)
	t.Logf("Snapshot Time  : %s", snap.SnapshotTimestamp)
	t.Logf("")

	t.Logf("--- Case 1: Scale-Up (5 → 10 pods) ---")
	t.Logf("Evidence Mode  : %s", respUp.EvidenceMode)
	t.Logf("Confidence     : %s", respUp.ConfidenceLevel)
	t.Logf("Degraded Mode  : %q", respUp.DegradedMode)
	t.Logf("")
	t.Logf("Impacted Services:")
	for _, svc := range respUp.ImpactedServices {
		t.Logf("  [%s] %s (%s)", svc.Role, svc.ServiceID, svc.Name)
	}
	t.Logf("Impacted Paths:")
	for _, sig := range observedPathSigsUp {
		t.Logf("  %s", sig)
	}
	t.Logf("Before/After Values:")
	for _, bav := range respUp.BeforeAfterValues {
		t.Logf("  %-45s before=%-10s after=%-10s delta=%s",
			bav.FieldRef,
			formatFloatPtr(bav.BeforeValue),
			formatFloatPtr(bav.AfterValue),
			formatFloatPtr(bav.DeltaValue),
		)
	}
	t.Logf("Recommendation : %s", respUp.Recommendation.Action)
	t.Logf("")

	// --- Scale-down (caution) case ---
	reqDown := buildScaleDownRequest(snap)
	ctxDown := BuildExecutionContext(reqDown, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})
	expectedDown := buildExpectedScaleDownOutcomes()
	respDown := RunScalingScenario(ctxDown)

	t.Logf("--- Case 2: Scale-Down Caution (5 → 3 pods) ---")
	t.Logf("Recommendation : %s", respDown.Recommendation.Action)
	t.Logf("Before/After Values:")
	for _, bav := range respDown.BeforeAfterValues {
		t.Logf("  %-45s before=%-10s after=%-10s delta=%s",
			bav.FieldRef,
			formatFloatPtr(bav.BeforeValue),
			formatFloatPtr(bav.AfterValue),
			formatFloatPtr(bav.DeltaValue),
		)
	}
	t.Logf("")

	// --- Pass/fail criteria ---
	latUpAfterRef := expectedUp.ExpectedLatencyAfter
	latDownAfterRef := expectedDown.ExpectedLatencyAfter

	criteria := []struct {
		Name   string
		Passed bool
	}{
		{"[scale-up] ResultStatus == OK", respUp.ResultStatus == ResultStatusOK},
		{"[scale-up] ImpactedServices count correct",
			len(respUp.ImpactedServices) == len(expectedUp.ExpectedImpactedServices)},
		{"[scale-up] ImpactedPaths count correct",
			len(respUp.ImpactedPaths) == len(expectedUp.ExpectedImpactedPathSigs)},
		{"[scale-up] pod_count before=5",
			bavMatchesBefore(respUp.BeforeAfterValues, "scaling.target.pod_count", 5)},
		{"[scale-up] pod_count after=10",
			bavMatchesAfter(respUp.BeforeAfterValues, "scaling.target.pod_count", 10)},
		{"[scale-up] rps_capacity before=200",
			bavMatchesBefore(respUp.BeforeAfterValues, "scaling.target.rps_capacity", 200)},
		{"[scale-up] rps_capacity after=400",
			bavMatchesAfter(respUp.BeforeAfterValues, "scaling.target.rps_capacity", 400)},
		{"[scale-up] latency_p95_ms before=45.0",
			bavMatchesBefore(respUp.BeforeAfterValues, "scaling.target.latency_p95_ms", 45.0)},
		{"[scale-up] latency_p95_ms after=22.5", func() bool {
			return latUpAfterRef != nil &&
				bavMatchesAfter(respUp.BeforeAfterValues, "scaling.target.latency_p95_ms", *latUpAfterRef)
		}()},
		{"[scale-up] recommendation == approve_scale_up",
			respUp.Recommendation.Action == "approve_scale_up"},
		{"[scale-up] contract validation passes",
			func() bool { return ValidateSimulationResponse(respUp) == nil }()},
		{"[scale-down] ResultStatus == OK", respDown.ResultStatus == ResultStatusOK},
		{"[scale-down] rps_capacity after=120",
			bavMatchesAfter(respDown.BeforeAfterValues, "scaling.target.rps_capacity", 120)},
		{"[scale-down] latency_p95_ms after=75.0", func() bool {
			return latDownAfterRef != nil &&
				bavMatchesAfter(respDown.BeforeAfterValues, "scaling.target.latency_p95_ms", *latDownAfterRef)
		}()},
		{"[scale-down] recommendation == caution_scale_down",
			respDown.Recommendation.Action == "caution_scale_down"},
		{"[scale-down] contract validation passes",
			func() bool { return ValidateSimulationResponse(respDown) == nil }()},
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
		t.Logf("OVERALL: PASS — Scaling up/down scenario is panel-defensible on real VM topology")
	} else {
		t.Errorf("OVERALL: FAIL — one or more validation criteria did not match expected outcomes")
	}
}
