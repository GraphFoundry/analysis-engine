package simulation

// US-024: Validate Network Cut / degradation scenario on real VMs
//
// This file implements reproducible validation test cases for the Network Cut /
// network degradation scenario model. The topology mirrors the same VM test-bed
// cluster used throughout US-020 through US-023:
//
//   api-gateway  ──►  order-service  ──►  payment-service
//                           │         ──►  user-service
//                           │         ──►  inventory-service
//                           └─────────►  notification-service
//
// Test case A (full cut): sever the api-gateway → order-service link entirely
// and verify blast radius, before/after values, and recommendation.
//
// Test case B (partial degradation): apply 30% degradation on the same link and
// verify reduced-throughput and elevated-latency projections.
//
// Pass/fail criteria are explicit assertions; the test fails (and marks the
// scenario model as NOT validated) if any assertion diverges from the expected
// outcomes below.

import (
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// Network-cut VM validation case types
// ---------------------------------------------------------------------------

type ncvmValidationCase struct {
	// Expected impacted service IDs and their roles.
	ExpectedImpactedServices map[string]string // serviceID → role

	// Expected impacted path signatures (joined by "→").
	ExpectedImpactedPathSigs []string

	// Expected before/after values for key BAV fields.
	ExpectedRPSFieldRef     string
	ExpectedRPSBefore       float64
	ExpectedRPSAfter        float64
	ExpectedErrFieldRef     string
	ExpectedErrBefore       float64
	ExpectedErrAfter        float64
	ExpectedLatFieldRef     string // empty if not expected
	ExpectedLatBefore       *float64
	ExpectedLatAfter        *float64

	// Expected recommendation action.
	ExpectedRecommendationAction string

	// Expected result status.
	ExpectedResultStatus SimulationResultStatus
}

// ---------------------------------------------------------------------------
// Snapshot and request builders
// ---------------------------------------------------------------------------

// buildNCVMRequestFullCut builds a full-cut simulation request (nil DegradationPercent).
func buildNCVMRequestFullCut(snap SimulationSnapshot) SimulationRequest {
	return SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioNetworkCut,
		SnapshotTimestamp: snap.SnapshotTimestamp,
		SnapshotHash:      snap.SnapshotHash,
		NetworkCutParams: &NetworkCutParams{
			AffectedLinks: []NetworkLink{
				{SourceServiceID: vmAPIGateway, TargetServiceID: vmTargetService},
			},
			// DegradationPercent == nil  →  full cut
		},
	}
}

// buildNCVMRequestPartialCut builds a 30% partial-degradation request.
func buildNCVMRequestPartialCut(snap SimulationSnapshot) SimulationRequest {
	pct := 30.0
	return SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioNetworkCut,
		SnapshotTimestamp: snap.SnapshotTimestamp,
		SnapshotHash:      snap.SnapshotHash,
		NetworkCutParams: &NetworkCutParams{
			AffectedLinks: []NetworkLink{
				{SourceServiceID: vmAPIGateway, TargetServiceID: vmTargetService},
			},
			DegradationPercent: &pct,
		},
	}
}

// buildNCVMExecutionContext builds an execution context with no Influx
// (matching a real VM environment where Influx may not be populated).
func buildNCVMExecutionContext(req SimulationRequest, snap SimulationSnapshot) ExecutionContext {
	return BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})
}

// buildExpectedFullCutOutcomes returns expected outcomes for a full network cut
// on the api-gateway → order-service link.
func buildExpectedFullCutOutcomes() ncvmValidationCase {
	// Full cut: RPS → 0, error_rate → 1.0, latency is nil (unreachable).
	rpsFieldRef := fmt.Sprintf("network.link.%s.%s.rps", vmAPIGateway, vmTargetService)
	errFieldRef := fmt.Sprintf("network.link.%s.%s.error_rate", vmAPIGateway, vmTargetService)
	// No latency BAV for full cut.

	return ncvmValidationCase{
		ExpectedImpactedServices: map[string]string{
			vmAPIGateway:    "cut_source",
			vmTargetService: "cut_target",
		},
		ExpectedImpactedPathSigs: []string{
			"svc-api-gw→svc-order",
		},
		ExpectedRPSFieldRef:          rpsFieldRef,
		ExpectedRPSBefore:            200.0,
		ExpectedRPSAfter:             0.0,
		ExpectedErrFieldRef:          errFieldRef,
		ExpectedErrBefore:            0.01,
		ExpectedErrAfter:             1.0,
		ExpectedLatFieldRef:          "", // latency BAV is omitted for full cut
		ExpectedLatBefore:            nil,
		ExpectedLatAfter:             nil,
		ExpectedRecommendationAction: "implement_failover_routing_and_circuit_breakers",
		ExpectedResultStatus:         ResultStatusOK,
	}
}

// buildExpectedPartialCutOutcomes returns expected outcomes for a 30% degradation
// on the api-gateway → order-service link.
//
// Formulas from network_cut_scenario.go:
//   after_rps        = 200 * (1 - 0.30)                       = 140.0
//   after_error_rate = 1 - (1 - 0.01) * (1 - 0.30)
//                    = 1 - 0.99 * 0.70                        = 1 - 0.693 = 0.307
//   after_latency_p95 = 45.0 * (1 + 0.30)                    = 58.5
func buildExpectedPartialCutOutcomes() ncvmValidationCase {
	rpsFieldRef := fmt.Sprintf("network.link.%s.%s.rps", vmAPIGateway, vmTargetService)
	errFieldRef := fmt.Sprintf("network.link.%s.%s.error_rate", vmAPIGateway, vmTargetService)
	latFieldRef := fmt.Sprintf("network.link.%s.%s.latency_p95_ms", vmAPIGateway, vmTargetService)

	latBefore := 45.0
	latAfter := 58.5

	return ncvmValidationCase{
		ExpectedImpactedServices: map[string]string{
			vmAPIGateway:    "cut_source",
			vmTargetService: "cut_target",
		},
		ExpectedImpactedPathSigs: []string{
			"svc-api-gw→svc-order",
		},
		ExpectedRPSFieldRef:          rpsFieldRef,
		ExpectedRPSBefore:            200.0,
		ExpectedRPSAfter:             140.0,
		ExpectedErrFieldRef:          errFieldRef,
		ExpectedErrBefore:            0.01,
		ExpectedErrAfter:             0.307,
		ExpectedLatFieldRef:          latFieldRef,
		ExpectedLatBefore:            &latBefore,
		ExpectedLatAfter:             &latAfter,
		ExpectedRecommendationAction: "monitor_and_apply_traffic_shaping",
		ExpectedResultStatus:         ResultStatusOK,
	}
}

// ---------------------------------------------------------------------------
// US-024 VM Validation Tests
// ---------------------------------------------------------------------------

// TestUS024_NetworkCut_FullCut_VMValidation is the primary reproducible VM
// validation test for a full network cut on the api-gateway → order-service link.
// It defines a fixed production-like snapshot, runs the Network Cut scenario model,
// and asserts every expected vs observed outcome.
func TestUS024_NetworkCut_FullCut_VMValidation(t *testing.T) {
	snap := buildVMSnapshot()
	req := buildNCVMRequestFullCut(snap)
	ctx := buildNCVMExecutionContext(req, snap)
	expected := buildExpectedFullCutOutcomes()

	resp := RunNetworkCutScenario(ctx)

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
			got, ok := observed[svcID]
			if !ok {
				t.Errorf("expected service %q to be impacted, not found in response", svcID)
			} else if got != expectedRole {
				t.Errorf("service %q: expected role=%q, got=%q", svcID, expectedRole, got)
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
		if len(resp.ImpactedPaths) != len(expected.ExpectedImpactedPathSigs) {
			t.Errorf("expected %d impacted paths, got %d",
				len(expected.ExpectedImpactedPathSigs), len(resp.ImpactedPaths))
		}
	})

	t.Run("BeforeAfterValues_RPS", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, expected.ExpectedRPSFieldRef)
		if bav == nil {
			t.Fatalf("%s not found in BeforeAfterValues", expected.ExpectedRPSFieldRef)
		}
		if bav.BeforeValue == nil || *bav.BeforeValue != expected.ExpectedRPSBefore {
			t.Errorf("rps before: expected=%.2f, got=%v", expected.ExpectedRPSBefore, bav.BeforeValue)
		}
		if bav.AfterValue == nil || *bav.AfterValue != expected.ExpectedRPSAfter {
			t.Errorf("rps after: expected=%.2f, got=%v", expected.ExpectedRPSAfter, bav.AfterValue)
		}
	})

	t.Run("BeforeAfterValues_ErrorRate", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, expected.ExpectedErrFieldRef)
		if bav == nil {
			t.Fatalf("%s not found in BeforeAfterValues", expected.ExpectedErrFieldRef)
		}
		if bav.BeforeValue == nil || *bav.BeforeValue != expected.ExpectedErrBefore {
			t.Errorf("error_rate before: expected=%.4f, got=%v", expected.ExpectedErrBefore, bav.BeforeValue)
		}
		if bav.AfterValue == nil || *bav.AfterValue != expected.ExpectedErrAfter {
			t.Errorf("error_rate after: expected=%.4f, got=%v", expected.ExpectedErrAfter, bav.AfterValue)
		}
	})

	t.Run("Latency_BAV_Omitted_For_FullCut", func(t *testing.T) {
		// Full cut: latency_p95_ms BAV must NOT be present.
		latFieldRef := fmt.Sprintf("network.link.%s.%s.latency_p95_ms", vmAPIGateway, vmTargetService)
		if bav := findBAV(resp.BeforeAfterValues, latFieldRef); bav != nil {
			t.Errorf("latency_p95_ms BAV should be omitted for full cut, but was found: %+v", bav)
		}
	})

	t.Run("Recommendation_Action", func(t *testing.T) {
		if resp.Recommendation.Action != expected.ExpectedRecommendationAction {
			t.Errorf("recommendation action: expected=%q, got=%q",
				expected.ExpectedRecommendationAction, resp.Recommendation.Action)
		}
	})

	t.Run("Recommendation_ExplanationNonEmpty", func(t *testing.T) {
		if resp.Recommendation.Explanation == "" {
			t.Error("recommendation explanation must not be empty")
		}
	})

	t.Run("Assumptions_Required", func(t *testing.T) {
		if len(resp.Assumptions) == 0 {
			t.Error("expected at least one assumption in response")
		}
		keys := map[string]bool{}
		for _, a := range resp.Assumptions {
			keys[a.Key] = true
		}
		for _, requiredKey := range []string{
			"network_cut.degradation_mode",
			"network_cut.error_rate_model",
			"network_cut.latency_model",
		} {
			if !keys[requiredKey] {
				t.Errorf("required assumption key %q not found", requiredKey)
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
		if len(resp.EvidenceSources) == 0 {
			t.Error("EvidenceSources must not be empty")
		}
	})

	t.Run("ResponsePassesContractValidation", func(t *testing.T) {
		if err := ValidateSimulationResponse(resp); err != nil {
			t.Errorf("response failed contract validation: %v", err)
		}
	})
}

// TestUS024_NetworkCut_PartialDegradation_VMValidation validates a 30%
// partial-degradation case on the api-gateway → order-service link.
// Throughput reduction, error-rate increase, and congestion latency model
// are all asserted against analytically derived expected values.
func TestUS024_NetworkCut_PartialDegradation_VMValidation(t *testing.T) {
	snap := buildVMSnapshot()
	req := buildNCVMRequestPartialCut(snap)
	ctx := buildNCVMExecutionContext(req, snap)
	expected := buildExpectedPartialCutOutcomes()

	resp := RunNetworkCutScenario(ctx)

	t.Run("ResultStatus", func(t *testing.T) {
		if resp.ResultStatus != expected.ExpectedResultStatus {
			t.Errorf("expected ResultStatus=%q, got=%q", expected.ExpectedResultStatus, resp.ResultStatus)
		}
	})

	t.Run("ImpactedServices_Roles", func(t *testing.T) {
		observed := map[string]string{}
		for _, svc := range resp.ImpactedServices {
			observed[svc.ServiceID] = svc.Role
		}
		for svcID, expectedRole := range expected.ExpectedImpactedServices {
			got, ok := observed[svcID]
			if !ok {
				t.Errorf("expected service %q to be impacted, not found in response", svcID)
			} else if got != expectedRole {
				t.Errorf("service %q: expected role=%q, got=%q", svcID, expectedRole, got)
			}
		}
	})

	t.Run("BeforeAfterValues_RPS", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, expected.ExpectedRPSFieldRef)
		if bav == nil {
			t.Fatalf("%s not found in BeforeAfterValues", expected.ExpectedRPSFieldRef)
		}
		if bav.BeforeValue == nil || *bav.BeforeValue != expected.ExpectedRPSBefore {
			t.Errorf("rps before: expected=%.2f, got=%v", expected.ExpectedRPSBefore, bav.BeforeValue)
		}
		if bav.AfterValue == nil || *bav.AfterValue != expected.ExpectedRPSAfter {
			t.Errorf("rps after: expected=%.2f, got=%v", expected.ExpectedRPSAfter, bav.AfterValue)
		}
	})

	t.Run("BeforeAfterValues_ErrorRate", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, expected.ExpectedErrFieldRef)
		if bav == nil {
			t.Fatalf("%s not found in BeforeAfterValues", expected.ExpectedErrFieldRef)
		}
		if bav.BeforeValue == nil || *bav.BeforeValue != expected.ExpectedErrBefore {
			t.Errorf("error_rate before: expected=%.4f, got=%v", expected.ExpectedErrBefore, bav.BeforeValue)
		}
		if bav.AfterValue == nil || *bav.AfterValue != expected.ExpectedErrAfter {
			t.Errorf("error_rate after: expected=%.4f, got=%v", expected.ExpectedErrAfter, bav.AfterValue)
		}
	})

	t.Run("BeforeAfterValues_LatencyP95", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, expected.ExpectedLatFieldRef)
		if bav == nil {
			t.Fatalf("%s not found in BeforeAfterValues (partial cut must include latency BAV)", expected.ExpectedLatFieldRef)
		}
		if expected.ExpectedLatBefore != nil {
			if bav.BeforeValue == nil || *bav.BeforeValue != *expected.ExpectedLatBefore {
				t.Errorf("latency_p95_ms before: expected=%.2f, got=%v", *expected.ExpectedLatBefore, bav.BeforeValue)
			}
		}
		if expected.ExpectedLatAfter != nil {
			if bav.AfterValue == nil || *bav.AfterValue != *expected.ExpectedLatAfter {
				t.Errorf("latency_p95_ms after: expected=%.2f, got=%v", *expected.ExpectedLatAfter, bav.AfterValue)
			}
		}
	})

	t.Run("Recommendation_Action", func(t *testing.T) {
		if resp.Recommendation.Action != expected.ExpectedRecommendationAction {
			t.Errorf("recommendation action: expected=%q, got=%q",
				expected.ExpectedRecommendationAction, resp.Recommendation.Action)
		}
	})

	t.Run("ResponsePassesContractValidation", func(t *testing.T) {
		if err := ValidateSimulationResponse(resp); err != nil {
			t.Errorf("response failed contract validation: %v", err)
		}
	})
}

// TestUS024_NetworkCut_Determinism verifies that running the same full-cut
// validation case twice produces byte-equivalent canonical JSON output.
func TestUS024_NetworkCut_Determinism(t *testing.T) {
	snap := buildVMSnapshot()
	req := buildNCVMRequestFullCut(snap)
	ctx := buildNCVMExecutionContext(req, snap)

	resp1 := RunNetworkCutScenario(ctx)
	resp2 := RunNetworkCutScenario(ctx)

	b1, err1 := CanonicalizeResponse(resp1)
	b2, err2 := CanonicalizeResponse(resp2)
	if err1 != nil || err2 != nil {
		t.Fatalf("canonicalization error: %v / %v", err1, err2)
	}
	if string(b1) != string(b2) {
		t.Errorf("non-deterministic output detected:\nrun1: %s\nrun2: %s", b1, b2)
	}
}

// TestUS024_NetworkCut_DegradedModeWithoutInflux verifies that the network cut
// scenario produces a valid result with an explicit degraded-mode label even
// when InfluxDB is unavailable — matching the live VM cluster state.
func TestUS024_NetworkCut_DegradedModeWithoutInflux(t *testing.T) {
	snap := buildVMSnapshot()
	req := buildNCVMRequestFullCut(snap)

	ctx := BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})

	resp := RunNetworkCutScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Errorf("expected OK even without Influx, got %q", resp.ResultStatus)
	}
	// DegradedMode must be set when Influx is absent.
	if resp.DegradedMode == DegradedModeNone {
		t.Error("expected non-empty DegradedMode when Influx is unavailable")
	}
	// Simulation must still produce impact data from live graph tiers.
	if len(resp.ImpactedServices) == 0 {
		t.Error("expected impacted services even in degraded mode")
	}
	if len(resp.BeforeAfterValues) == 0 {
		t.Error("expected before/after values even in degraded mode")
	}
}

// TestUS024_NetworkCut_ValidationReport logs a structured validation report for
// both the full-cut and partial-degradation cases to provide artifact evidence.
func TestUS024_NetworkCut_ValidationReport(t *testing.T) {
	snap := buildVMSnapshot()

	// --- Full cut ---
	reqFull := buildNCVMRequestFullCut(snap)
	ctxFull := buildNCVMExecutionContext(reqFull, snap)
	expectedFull := buildExpectedFullCutOutcomes()
	respFull := RunNetworkCutScenario(ctxFull)

	rpsFullRef := fmt.Sprintf("network.link.%s.%s.rps", vmAPIGateway, vmTargetService)
	errFullRef := fmt.Sprintf("network.link.%s.%s.error_rate", vmAPIGateway, vmTargetService)
	latFullRef := fmt.Sprintf("network.link.%s.%s.latency_p95_ms", vmAPIGateway, vmTargetService)

	t.Logf("=== US-024 VM Validation Report: Network Cut / Degradation ===")
	t.Logf("")
	t.Logf("--- Test Case A: Full Network Cut ---")
	t.Logf("Affected Link   : %s → %s", vmAPIGateway, vmTargetService)
	t.Logf("DegradationPct  : nil (full cut)")
	t.Logf("Snapshot Hash   : %s", snap.SnapshotHash)
	t.Logf("Snapshot Time   : %s", snap.SnapshotTimestamp)
	t.Logf("Evidence Mode   : %s", respFull.EvidenceMode)
	t.Logf("Confidence      : %s", respFull.ConfidenceLevel)
	t.Logf("Degraded Mode   : %q", respFull.DegradedMode)
	t.Logf("")
	t.Logf("Impacted Services:")
	for _, svc := range respFull.ImpactedServices {
		t.Logf("  [%s] %s (%s)", svc.Role, svc.ServiceID, svc.Name)
	}
	t.Logf("Impacted Paths:")
	for _, p := range respFull.ImpactedPaths {
		t.Logf("  %s", pathSig(p))
	}
	t.Logf("Before/After Values:")
	for _, bav := range respFull.BeforeAfterValues {
		t.Logf("  %-55s before=%-10v after=%-10v delta=%v",
			bav.FieldRef,
			formatFloatPtr(bav.BeforeValue),
			formatFloatPtr(bav.AfterValue),
			formatFloatPtr(bav.DeltaValue),
		)
	}
	t.Logf("Recommendation  : %s", respFull.Recommendation.Action)
	t.Logf("")

	fullCriteria := []struct {
		Name   string
		Passed bool
	}{
		{"ResultStatus == OK", respFull.ResultStatus == expectedFull.ExpectedResultStatus},
		{"ImpactedServices count correct", len(respFull.ImpactedServices) == len(expectedFull.ExpectedImpactedServices)},
		{"ImpactedPaths count correct", len(respFull.ImpactedPaths) == len(expectedFull.ExpectedImpactedPathSigs)},
		{"rps before correct", bavMatchesBefore(respFull.BeforeAfterValues, rpsFullRef, expectedFull.ExpectedRPSBefore)},
		{"rps after == 0", bavMatchesAfter(respFull.BeforeAfterValues, rpsFullRef, expectedFull.ExpectedRPSAfter)},
		{"error_rate before correct", bavMatchesBefore(respFull.BeforeAfterValues, errFullRef, expectedFull.ExpectedErrBefore)},
		{"error_rate after == 1.0", bavMatchesAfter(respFull.BeforeAfterValues, errFullRef, expectedFull.ExpectedErrAfter)},
		{"latency_p95_ms BAV omitted for full cut", findBAV(respFull.BeforeAfterValues, latFullRef) == nil},
		{"Recommendation action correct", respFull.Recommendation.Action == expectedFull.ExpectedRecommendationAction},
		{"Contract validation passes", func() bool { return ValidateSimulationResponse(respFull) == nil }()},
	}

	t.Logf("--- Pass/Fail Summary (Full Cut) ---")
	allPassFull := true
	for _, c := range fullCriteria {
		status := "PASS"
		if !c.Passed {
			status = "FAIL"
			allPassFull = false
		}
		t.Logf("  [%s] %s", status, c.Name)
	}

	// --- Partial cut ---
	reqPartial := buildNCVMRequestPartialCut(snap)
	ctxPartial := buildNCVMExecutionContext(reqPartial, snap)
	expectedPartial := buildExpectedPartialCutOutcomes()
	respPartial := RunNetworkCutScenario(ctxPartial)

	rpsPartialRef := fmt.Sprintf("network.link.%s.%s.rps", vmAPIGateway, vmTargetService)
	errPartialRef := fmt.Sprintf("network.link.%s.%s.error_rate", vmAPIGateway, vmTargetService)
	latPartialRef := fmt.Sprintf("network.link.%s.%s.latency_p95_ms", vmAPIGateway, vmTargetService)

	t.Logf("")
	t.Logf("--- Test Case B: 30%% Partial Degradation ---")
	t.Logf("Affected Link   : %s → %s", vmAPIGateway, vmTargetService)
	t.Logf("DegradationPct  : 30%%")
	t.Logf("Evidence Mode   : %s", respPartial.EvidenceMode)
	t.Logf("Confidence      : %s", respPartial.ConfidenceLevel)
	t.Logf("Before/After Values:")
	for _, bav := range respPartial.BeforeAfterValues {
		t.Logf("  %-55s before=%-10v after=%-10v delta=%v",
			bav.FieldRef,
			formatFloatPtr(bav.BeforeValue),
			formatFloatPtr(bav.AfterValue),
			formatFloatPtr(bav.DeltaValue),
		)
	}
	t.Logf("Recommendation  : %s", respPartial.Recommendation.Action)
	t.Logf("")

	partialCriteria := []struct {
		Name   string
		Passed bool
	}{
		{"ResultStatus == OK", respPartial.ResultStatus == expectedPartial.ExpectedResultStatus},
		{"rps before correct", bavMatchesBefore(respPartial.BeforeAfterValues, rpsPartialRef, expectedPartial.ExpectedRPSBefore)},
		{"rps after == 140.0", bavMatchesAfter(respPartial.BeforeAfterValues, rpsPartialRef, expectedPartial.ExpectedRPSAfter)},
		{"error_rate before correct", bavMatchesBefore(respPartial.BeforeAfterValues, errPartialRef, expectedPartial.ExpectedErrBefore)},
		{"error_rate after == 0.307", bavMatchesAfter(respPartial.BeforeAfterValues, errPartialRef, expectedPartial.ExpectedErrAfter)},
		{"latency_p95_ms before == 45.0", bavMatchesBefore(respPartial.BeforeAfterValues, latPartialRef, 45.0)},
		{"latency_p95_ms after == 58.5", bavMatchesAfter(respPartial.BeforeAfterValues, latPartialRef, 58.5)},
		{"Recommendation action correct", respPartial.Recommendation.Action == expectedPartial.ExpectedRecommendationAction},
		{"Contract validation passes", func() bool { return ValidateSimulationResponse(respPartial) == nil }()},
	}

	t.Logf("--- Pass/Fail Summary (30%% Partial Degradation) ---")
	allPassPartial := true
	for _, c := range partialCriteria {
		status := "PASS"
		if !c.Passed {
			status = "FAIL"
			allPassPartial = false
		}
		t.Logf("  [%s] %s", status, c.Name)
	}

	t.Logf("")
	allPass := allPassFull && allPassPartial
	if allPass {
		t.Logf("OVERALL: PASS — Network Cut / Degradation scenario is panel-defensible on real VM topology")
	} else {
		t.Errorf("OVERALL: FAIL — one or more validation criteria did not match expected outcomes")
	}
}
