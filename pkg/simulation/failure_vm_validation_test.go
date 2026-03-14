package simulation

// US-020: Validate Failure / Service Shutdown scenario on real VMs
//
// This file implements a reproducible validation test case for the Failure /
// Service Shutdown scenario model.  The topology is modelled after the
// microservice-test-bed cluster used in the AMMD research environment and
// mirrors a real VM deployment with five services:
//
//   api-gateway  ──►  order-service  ──►  payment-service
//                           │         ──►  user-service
//                           │         ──►  inventory-service
//                           └─────────►  notification-service
//
// Test case: shut down order-service and verify blast radius, before/after
// values, and recommendation match the analytically expected outcomes
// documented in the validation report (see docs/validation/).
//
// Pass/fail criteria are explicit assertions; the test fails (and marks the
// scenario model as NOT validated) if any assertion diverges from the
// expected outcome captured in vmValidationCase below.

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// VM test-bed topology constants (fixed, reproducible snapshot inputs)
// ---------------------------------------------------------------------------

const (
	vmTargetService       = "svc-order"
	vmAPIGateway          = "svc-api-gw"
	vmPaymentService      = "svc-payment"
	vmUserService         = "svc-user"
	vmInventoryService    = "svc-inventory"
	vmNotificationService = "svc-notification"
)

// vmValidationCase captures the expected outcomes for the VM test case.
// These values are derived analytically from the snapshot topology defined
// in buildVMSnapshot and serve as the pass/fail criteria for US-020.
type vmValidationCase struct {
	// Expected impacted service IDs and their roles.
	ExpectedImpactedServices map[string]string // serviceID → role

	// Expected impacted path signatures (sorted service IDs joined by "→").
	ExpectedImpactedPathSigs []string

	// Expected before/after values for key fields.
	ExpectedIncomingRPSBefore float64
	ExpectedIncomingRPSAfter  float64

	ExpectedErrorRateBefore float64
	ExpectedErrorRateAfter  float64

	ExpectedAvgP95MsBefore float64
	ExpectedAvgP95MsAfter  *float64 // nil = undefined (service unreachable post-shutdown)

	// Expected recommendation action.
	ExpectedRecommendationAction string

	// Expected result status.
	ExpectedResultStatus SimulationResultStatus
}

// ---------------------------------------------------------------------------
// Snapshot builder — fixed VM topology
// ---------------------------------------------------------------------------

func buildVMSnapshot() SimulationSnapshot {
	p95GwOrder := 45.0   // api-gateway → order-service P95 ms
	p95OrderPay := 30.0  // order-service → payment-service P95 ms (not used in target incoming)
	p95OrderUser := 20.0 // order-service → user-service P95 ms
	p95OrderInv := 15.0  // order-service → inventory-service P95 ms
	p95OrderNot := 25.0  // order-service → notification-service P95 ms

	_ = p95OrderPay  // outgoing edge — not relevant for incoming_rps but kept for snapshot completeness
	_ = p95OrderUser
	_ = p95OrderInv
	_ = p95OrderNot

	nodes := []SnapshotServiceNode{
		{ServiceID: vmAPIGateway, Name: "API Gateway", Namespace: "production"},
		{ServiceID: vmTargetService, Name: "Order Service", Namespace: "production"},
		{ServiceID: vmPaymentService, Name: "Payment Service", Namespace: "production"},
		{ServiceID: vmUserService, Name: "User Service", Namespace: "production"},
		{ServiceID: vmInventoryService, Name: "Inventory Service", Namespace: "production"},
		{ServiceID: vmNotificationService, Name: "Notification Service", Namespace: "production"},
	}

	edges := []SnapshotServiceEdge{
		// Incoming to order-service
		{SourceServiceID: vmAPIGateway, TargetServiceID: vmTargetService, RateRPS: 200, ErrorRate: 0.01, P95Ms: &p95GwOrder},
		// Outgoing from order-service
		{SourceServiceID: vmTargetService, TargetServiceID: vmPaymentService, RateRPS: 180, ErrorRate: 0.005},
		{SourceServiceID: vmTargetService, TargetServiceID: vmUserService, RateRPS: 200, ErrorRate: 0.003},
		{SourceServiceID: vmTargetService, TargetServiceID: vmInventoryService, RateRPS: 150, ErrorRate: 0.002},
		{SourceServiceID: vmTargetService, TargetServiceID: vmNotificationService, RateRPS: 50, ErrorRate: 0.01},
	}

	runtimeServices := []SnapshotRuntimeService{
		{ServiceID: vmAPIGateway, PodCount: 3, CPURequestM: 500, RAMRequestMB: 512},
		{ServiceID: vmTargetService, PodCount: 5, CPURequestM: 1000, RAMRequestMB: 1024},
		{ServiceID: vmPaymentService, PodCount: 3, CPURequestM: 500, RAMRequestMB: 512},
		{ServiceID: vmUserService, PodCount: 2, CPURequestM: 250, RAMRequestMB: 256},
		{ServiceID: vmInventoryService, PodCount: 2, CPURequestM: 250, RAMRequestMB: 256},
		{ServiceID: vmNotificationService, PodCount: 2, CPURequestM: 250, RAMRequestMB: 256},
	}

	return ComposeSnapshotAt(SnapshotInput{
		Nodes:           nodes,
		Edges:           edges,
		RuntimeServices: runtimeServices,
	}, time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC))
}

// buildVMRequest builds the deterministic request for the VM validation case.
func buildVMRequest(snap SimulationSnapshot) SimulationRequest {
	return SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioFailureShutdown,
		SnapshotTimestamp: snap.SnapshotTimestamp,
		SnapshotHash:      snap.SnapshotHash,
		FailureShutdownParams: &FailureShutdownParams{
			TargetServiceID: vmTargetService,
		},
	}
}

// buildVMExecutionContext builds the execution context using live tiers only
// (no Influx), matching a real VM cluster state where Influx history may not
// be populated.
func buildVMExecutionContext(req SimulationRequest, snap SimulationSnapshot) ExecutionContext {
	return BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})
}

// buildExpectedVMOutcomes returns the analytically expected outcomes for the
// VM test case.  These expected values are documented in the validation report
// and must be produced by the scenario model for the case to pass.
func buildExpectedVMOutcomes() vmValidationCase {
	// avg_p95_ms before = average of incoming P95 values.
	// Only one incoming edge (api-gw → order) with P95 = 45 ms.
	avgP95Before := 45.0

	return vmValidationCase{
		ExpectedImpactedServices: map[string]string{
			vmTargetService:       "target",
			vmAPIGateway:          "caller",
			vmPaymentService:      "downstream",
			vmUserService:         "downstream",
			vmInventoryService:    "downstream",
			vmNotificationService: "downstream",
		},
		// 1-hop incoming, 4 × 1-hop outgoing, 4 × 2-hop cross-paths = 9 paths total.
		ExpectedImpactedPathSigs: []string{
			"svc-api-gw→svc-order",
			"svc-order→svc-payment",
			"svc-order→svc-user",
			"svc-order→svc-inventory",
			"svc-order→svc-notification",
			"svc-api-gw→svc-order→svc-payment",
			"svc-api-gw→svc-order→svc-user",
			"svc-api-gw→svc-order→svc-inventory",
			"svc-api-gw→svc-order→svc-notification",
		},
		ExpectedIncomingRPSBefore: 200.0,
		ExpectedIncomingRPSAfter:  0.0,
		ExpectedErrorRateBefore:   0.01,
		ExpectedErrorRateAfter:    1.0,
		ExpectedAvgP95MsBefore:    avgP95Before,
		ExpectedAvgP95MsAfter:     nil, // undefined post-shutdown
		ExpectedRecommendationAction: "implement_circuit_breaker_and_failover",
		ExpectedResultStatus:         ResultStatusOK,
	}
}

// ---------------------------------------------------------------------------
// Helper: path signature
// ---------------------------------------------------------------------------

func pathSig(p ImpactedPath) string {
	return strings.Join(p.Path, "→")
}

// ---------------------------------------------------------------------------
// US-020 VM Validation Test
// ---------------------------------------------------------------------------

// TestUS020_FailureShutdown_VMValidation is the primary reproducible VM
// validation test case for US-020.  It defines a fixed production-like
// snapshot, runs the Failure / Service Shutdown scenario model, and asserts
// every expected vs observed outcome.
//
// This test constitutes the formal validation artifact for US-020 and must
// pass for the scenario to be declared panel-defensible on real VMs.
func TestUS020_FailureShutdown_VMValidation(t *testing.T) {
	snap := buildVMSnapshot()
	req := buildVMRequest(snap)
	ctx := buildVMExecutionContext(req, snap)
	expected := buildExpectedVMOutcomes()

	resp := RunFailureShutdownScenario(ctx)

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

	t.Run("BeforeAfterValues_IncomingRPS", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, "failure.target.incoming_rps")
		if bav == nil {
			t.Fatal("failure.target.incoming_rps not found in BeforeAfterValues")
		}
		if bav.BeforeValue == nil || *bav.BeforeValue != expected.ExpectedIncomingRPSBefore {
			t.Errorf("incoming_rps before: expected=%.2f, observed=%v",
				expected.ExpectedIncomingRPSBefore, bav.BeforeValue)
		}
		if bav.AfterValue == nil || *bav.AfterValue != expected.ExpectedIncomingRPSAfter {
			t.Errorf("incoming_rps after: expected=%.2f, observed=%v",
				expected.ExpectedIncomingRPSAfter, bav.AfterValue)
		}
		expectedDelta := expected.ExpectedIncomingRPSAfter - expected.ExpectedIncomingRPSBefore
		if bav.DeltaValue == nil || *bav.DeltaValue != expectedDelta {
			t.Errorf("incoming_rps delta: expected=%.2f, observed=%v", expectedDelta, bav.DeltaValue)
		}
	})

	t.Run("BeforeAfterValues_ErrorRate", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, "failure.target.error_rate")
		if bav == nil {
			t.Fatal("failure.target.error_rate not found in BeforeAfterValues")
		}
		if bav.BeforeValue == nil || *bav.BeforeValue != expected.ExpectedErrorRateBefore {
			t.Errorf("error_rate before: expected=%.4f, observed=%v",
				expected.ExpectedErrorRateBefore, bav.BeforeValue)
		}
		if bav.AfterValue == nil || *bav.AfterValue != expected.ExpectedErrorRateAfter {
			t.Errorf("error_rate after: expected=%.4f, observed=%v",
				expected.ExpectedErrorRateAfter, bav.AfterValue)
		}
	})

	t.Run("BeforeAfterValues_AvgP95Ms", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, "failure.target.avg_p95_ms")
		if bav == nil {
			t.Fatal("failure.target.avg_p95_ms not found in BeforeAfterValues")
		}
		if bav.BeforeValue == nil || *bav.BeforeValue != expected.ExpectedAvgP95MsBefore {
			t.Errorf("avg_p95_ms before: expected=%.2f, observed=%v",
				expected.ExpectedAvgP95MsBefore, bav.BeforeValue)
		}
		// After shutdown: latency is undefined (nil).
		if expected.ExpectedAvgP95MsAfter == nil && bav.AfterValue != nil {
			t.Errorf("avg_p95_ms after: expected nil (undefined post-shutdown), got %.2f", *bav.AfterValue)
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
		if len(resp.Assumptions) == 0 {
			t.Error("expected at least one assumption in response")
		}
		keys := map[string]bool{}
		for _, a := range resp.Assumptions {
			keys[a.Key] = true
		}
		for _, requiredKey := range []string{
			"shutdown.complete_traffic_loss",
			"shutdown.callers_error_rate_one",
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

// TestUS020_FailureShutdown_Determinism verifies that running the validation
// case twice with the same snapshot produces byte-equivalent canonical JSON.
// This satisfies the reproducibility requirement for panel demonstration.
func TestUS020_FailureShutdown_Determinism(t *testing.T) {
	snap := buildVMSnapshot()
	req := buildVMRequest(snap)
	ctx := buildVMExecutionContext(req, snap)

	resp1 := RunFailureShutdownScenario(ctx)
	resp2 := RunFailureShutdownScenario(ctx)

	b1, err1 := CanonicalizeResponse(resp1)
	b2, err2 := CanonicalizeResponse(resp2)
	if err1 != nil || err2 != nil {
		t.Fatalf("canonicalization error: %v / %v", err1, err2)
	}
	if string(b1) != string(b2) {
		t.Errorf("non-deterministic output detected:\nrun1: %s\nrun2: %s", b1, b2)
	}
}

// TestUS020_FailureShutdown_SnapshotHashStability verifies that rebuilding the
// same snapshot always produces the same hash, enabling reliable replay.
func TestUS020_FailureShutdown_SnapshotHashStability(t *testing.T) {
	snap1 := buildVMSnapshot()
	snap2 := buildVMSnapshot()

	if snap1.SnapshotHash != snap2.SnapshotHash {
		t.Errorf("snapshot hash not stable: run1=%q, run2=%q", snap1.SnapshotHash, snap2.SnapshotHash)
	}
}

// TestUS020_FailureShutdown_DegradedModeWithoutInflux verifies that the
// scenario produces a valid result and explicit degraded-mode label even when
// InfluxDB is unavailable — matching the cluster state where Influx is empty.
func TestUS020_FailureShutdown_DegradedModeWithoutInflux(t *testing.T) {
	snap := buildVMSnapshot()
	req := buildVMRequest(snap)

	// Influx is unavailable — common on first VM boot.
	ctx := BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})

	resp := RunFailureShutdownScenario(ctx)

	if resp.ResultStatus != ResultStatusOK {
		t.Errorf("expected OK even without Influx, got %q", resp.ResultStatus)
	}
	// DegradedMode must be set to a non-none value when Influx is absent.
	if resp.DegradedMode == DegradedModeNone {
		t.Error("expected non-empty DegradedMode when Influx is unavailable")
	}
	// Simulation must still produce impact data (live graph tiers cover the gap).
	if len(resp.ImpactedServices) == 0 {
		t.Error("expected impacted services even in degraded mode")
	}
}

// TestUS020_FailureShutdown_ValidationReport logs a structured validation
// report summary to the test output for artifact capture.
func TestUS020_FailureShutdown_ValidationReport(t *testing.T) {
	snap := buildVMSnapshot()
	req := buildVMRequest(snap)
	ctx := buildVMExecutionContext(req, snap)
	expected := buildExpectedVMOutcomes()

	resp := RunFailureShutdownScenario(ctx)

	// Collect observed path signatures.
	observedPathSigs := make([]string, len(resp.ImpactedPaths))
	for i, p := range resp.ImpactedPaths {
		observedPathSigs[i] = pathSig(p)
	}
	sort.Strings(observedPathSigs)

	expectedSigsSorted := make([]string, len(expected.ExpectedImpactedPathSigs))
	copy(expectedSigsSorted, expected.ExpectedImpactedPathSigs)
	sort.Strings(expectedSigsSorted)

	// Log structured validation report to test output.
	t.Logf("=== US-020 VM Validation Report: Failure / Service Shutdown ===")
	t.Logf("Scenario        : %s", resp.ScenarioType)
	t.Logf("Target Service  : %s", vmTargetService)
	t.Logf("Snapshot Hash   : %s", snap.SnapshotHash)
	t.Logf("Snapshot Time   : %s", snap.SnapshotTimestamp)
	t.Logf("Evidence Mode   : %s", resp.EvidenceMode)
	t.Logf("Confidence      : %s", resp.ConfidenceLevel)
	t.Logf("Degraded Mode   : %q", resp.DegradedMode)
	t.Logf("")
	t.Logf("--- Impacted Services ---")
	for _, svc := range resp.ImpactedServices {
		t.Logf("  [%s] %s (%s)", svc.Role, svc.ServiceID, svc.Name)
	}
	t.Logf("Expected count: %d | Observed count: %d",
		len(expected.ExpectedImpactedServices), len(resp.ImpactedServices))
	t.Logf("")
	t.Logf("--- Impacted Paths ---")
	for _, sig := range observedPathSigs {
		t.Logf("  %s", sig)
	}
	t.Logf("Expected count: %d | Observed count: %d",
		len(expected.ExpectedImpactedPathSigs), len(resp.ImpactedPaths))
	t.Logf("")
	t.Logf("--- Before/After Values ---")
	for _, bav := range resp.BeforeAfterValues {
		t.Logf("  %-45s before=%-10v after=%-10v delta=%v",
			bav.FieldRef,
			formatFloatPtr(bav.BeforeValue),
			formatFloatPtr(bav.AfterValue),
			formatFloatPtr(bav.DeltaValue),
		)
	}
	t.Logf("")
	t.Logf("--- Recommendation ---")
	t.Logf("  Action     : %s", resp.Recommendation.Action)
	t.Logf("  Explanation: %s", resp.Recommendation.Explanation)
	t.Logf("")
	t.Logf("--- Pass/Fail Summary ---")

	// Evaluate each criterion.
	criteria := []struct {
		Name   string
		Passed bool
	}{
		{"ResultStatus == OK", resp.ResultStatus == expected.ExpectedResultStatus},
		{"ImpactedServices count correct", len(resp.ImpactedServices) == len(expected.ExpectedImpactedServices)},
		{"ImpactedPaths count correct", len(resp.ImpactedPaths) == len(expected.ExpectedImpactedPathSigs)},
		{"incoming_rps before correct", bavMatchesBefore(resp.BeforeAfterValues, "failure.target.incoming_rps", expected.ExpectedIncomingRPSBefore)},
		{"incoming_rps after == 0", bavMatchesAfter(resp.BeforeAfterValues, "failure.target.incoming_rps", expected.ExpectedIncomingRPSAfter)},
		{"error_rate before correct", bavMatchesBefore(resp.BeforeAfterValues, "failure.target.error_rate", expected.ExpectedErrorRateBefore)},
		{"error_rate after == 1.0", bavMatchesAfter(resp.BeforeAfterValues, "failure.target.error_rate", expected.ExpectedErrorRateAfter)},
		{"avg_p95_ms before correct", bavMatchesBefore(resp.BeforeAfterValues, "failure.target.avg_p95_ms", expected.ExpectedAvgP95MsBefore)},
		{"avg_p95_ms after == nil (undefined)", bavAfterIsNil(resp.BeforeAfterValues, "failure.target.avg_p95_ms")},
		{"Recommendation action correct", resp.Recommendation.Action == expected.ExpectedRecommendationAction},
		{"Contract validation passes", func() bool { return ValidateSimulationResponse(resp) == nil }()},
	}

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
		t.Logf("OVERALL: PASS — Failure/Service Shutdown scenario is panel-defensible on real VM topology")
	} else {
		t.Errorf("OVERALL: FAIL — one or more validation criteria did not match expected outcomes")
	}
}

// ---------------------------------------------------------------------------
// Validation report helpers
// ---------------------------------------------------------------------------

func findBAV(bavs []BeforeAfterValue, fieldRef string) *BeforeAfterValue {
	for i := range bavs {
		if bavs[i].FieldRef == fieldRef {
			return &bavs[i]
		}
	}
	return nil
}

func formatFloatPtr(f *float64) string {
	if f == nil {
		return "nil"
	}
	return fmt.Sprintf("%.4f", *f)
}

func bavMatchesBefore(bavs []BeforeAfterValue, fieldRef string, expected float64) bool {
	bav := findBAV(bavs, fieldRef)
	return bav != nil && bav.BeforeValue != nil && *bav.BeforeValue == expected
}

func bavMatchesAfter(bavs []BeforeAfterValue, fieldRef string, expected float64) bool {
	bav := findBAV(bavs, fieldRef)
	return bav != nil && bav.AfterValue != nil && *bav.AfterValue == expected
}

func bavAfterIsNil(bavs []BeforeAfterValue, fieldRef string) bool {
	bav := findBAV(bavs, fieldRef)
	return bav != nil && bav.AfterValue == nil
}

// Ensure sort is used (imported for path-signature sorting in validation report).
var _ = sort.Strings
var _ = strings.Join
