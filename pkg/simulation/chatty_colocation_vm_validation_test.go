package simulation

// US-023: Validate Chatty-service co-location / migration scenario on real VMs
//
// This file implements reproducible validation test cases for the Chatty-service
// co-location / migration scenario model. The topology reuses the microservice-test-bed
// cluster defined in failure_vm_validation_test.go (buildVMSnapshot):
//
//   api-gateway  ──►  order-service  ──►  payment-service
//                           │         ──►  user-service
//                           │         ──►  inventory-service
//                           └─────────►  notification-service
//
// Primary test case: simulate co-location recommendation for api-gateway → order-service
// (same namespace "production", RPS=200 ≥ chattyRPSThreshold=50 → co_locate).
// Expected BAVs: edge.rps before=200/after=200/delta=0;
//               edge.latency_p95_ms before=45.0/after=27.0/delta=-18.0.
//
// Secondary test case: simulate migration recommendation for a cross-namespace pair
// (api-gateway in "gateway" ns → order-service in "production" ns, RPS=200 → migrate).
//
// Pass/fail criteria are explicit assertions; any divergence from expected outcomes
// marks the scenario as NOT validated.

import (
	"sort"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Chatty co-location VM validation case type
// ---------------------------------------------------------------------------

// chattyColocationVMValidationCase captures expected outcomes for a chatty
// co-location / migration VM test case.
type chattyColocationVMValidationCase struct {
	// Expected impacted service IDs and their roles.
	ExpectedImpactedServices map[string]string // serviceID → role

	// Expected impacted path signatures (service IDs joined by "→").
	ExpectedImpactedPathSigs []string

	// Expected colocation.edge.rps BAV.
	ExpectedEdgeRPSBefore float64
	ExpectedEdgeRPSAfter  float64
	ExpectedEdgeRPSDelta  float64

	// Expected colocation.edge.latency_p95_ms BAV (nil = omitted because no P95 data).
	ExpectedLatencyBefore *float64
	ExpectedLatencyAfter  *float64
	ExpectedLatencyDelta  *float64

	// Expected recommendation action.
	ExpectedRecommendationAction string

	// Expected result status.
	ExpectedResultStatus SimulationResultStatus
}

// ---------------------------------------------------------------------------
// Primary case: co_locate (same namespace, high RPS)
// ---------------------------------------------------------------------------

// buildChattyColocateRequest builds the deterministic co-location request for the VM test.
// The pair api-gateway → order-service are both in "production" namespace with RPS=200.
func buildChattyColocateRequest(snap SimulationSnapshot) SimulationRequest {
	return SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioChattyColocation,
		SnapshotTimestamp: snap.SnapshotTimestamp,
		SnapshotHash:      snap.SnapshotHash,
		ChattyColocationParams: &ChattyColocationParams{
			SourceServiceID: vmAPIGateway,  // svc-api-gw (production)
			TargetServiceID: vmTargetService, // svc-order (production)
		},
	}
}

// buildExpectedColocateOutcomes returns the analytically expected outcomes for the
// co-locate case.
//
// Edge: api-gateway → order-service, RPS=200, P95=45ms, same namespace.
// - RPS: before=200, after=200 (co-location does not change call frequency), delta=0
// - latency_p95_ms: before=45.0, after=45.0×0.60=27.0, delta=-18.0
// - Recommendation: co_locate (same namespace, RPS ≥ 50)
func buildExpectedColocateOutcomes() chattyColocationVMValidationCase {
	latBefore := 45.0
	latAfter := 27.0  // 45.0 × 0.60
	latDelta := -18.0 // 27.0 - 45.0

	return chattyColocationVMValidationCase{
		ExpectedImpactedServices: map[string]string{
			vmAPIGateway:    "chatty_source",
			vmTargetService: "chatty_target",
		},
		ExpectedImpactedPathSigs: []string{
			"svc-api-gw→svc-order",
		},
		ExpectedEdgeRPSBefore:        200.0,
		ExpectedEdgeRPSAfter:         200.0,
		ExpectedEdgeRPSDelta:         0.0,
		ExpectedLatencyBefore:        &latBefore,
		ExpectedLatencyAfter:         &latAfter,
		ExpectedLatencyDelta:         &latDelta,
		ExpectedRecommendationAction: "co_locate",
		ExpectedResultStatus:         ResultStatusOK,
	}
}

// ---------------------------------------------------------------------------
// Secondary case: migrate (cross-namespace, high RPS)
// ---------------------------------------------------------------------------

// buildChattyMigrateSnapshot builds a modified VM snapshot where api-gateway is placed
// in a separate "gateway" namespace, making it cross-namespace with order-service ("production").
// All edges and runtime services are preserved; only the api-gateway namespace changes.
func buildChattyMigrateSnapshot() SimulationSnapshot {
	p95GwOrder := 45.0

	nodes := []SnapshotServiceNode{
		{ServiceID: vmAPIGateway, Name: "API Gateway", Namespace: "gateway"},    // different namespace
		{ServiceID: vmTargetService, Name: "Order Service", Namespace: "production"},
		{ServiceID: vmPaymentService, Name: "Payment Service", Namespace: "production"},
		{ServiceID: vmUserService, Name: "User Service", Namespace: "production"},
		{ServiceID: vmInventoryService, Name: "Inventory Service", Namespace: "production"},
		{ServiceID: vmNotificationService, Name: "Notification Service", Namespace: "production"},
	}

	edges := []SnapshotServiceEdge{
		{SourceServiceID: vmAPIGateway, TargetServiceID: vmTargetService, RateRPS: 200, ErrorRate: 0.01, P95Ms: &p95GwOrder},
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

// buildChattyMigrateRequest builds the deterministic migrate request for the VM test.
func buildChattyMigrateRequest(snap SimulationSnapshot) SimulationRequest {
	return SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioChattyColocation,
		SnapshotTimestamp: snap.SnapshotTimestamp,
		SnapshotHash:      snap.SnapshotHash,
		ChattyColocationParams: &ChattyColocationParams{
			SourceServiceID: vmAPIGateway,
			TargetServiceID: vmTargetService,
		},
	}
}

// buildExpectedMigrateOutcomes returns the expected outcomes for the cross-namespace migrate case.
//
// api-gateway (gateway ns) → order-service (production ns), RPS=200, P95=45ms.
// Different namespaces + RPS ≥ 50 → migrate recommendation.
func buildExpectedMigrateOutcomes() chattyColocationVMValidationCase {
	latBefore := 45.0
	latAfter := 27.0
	latDelta := -18.0

	return chattyColocationVMValidationCase{
		ExpectedImpactedServices: map[string]string{
			vmAPIGateway:    "chatty_source",
			vmTargetService: "chatty_target",
		},
		ExpectedImpactedPathSigs: []string{
			"svc-api-gw→svc-order",
		},
		ExpectedEdgeRPSBefore:        200.0,
		ExpectedEdgeRPSAfter:         200.0,
		ExpectedEdgeRPSDelta:         0.0,
		ExpectedLatencyBefore:        &latBefore,
		ExpectedLatencyAfter:         &latAfter,
		ExpectedLatencyDelta:         &latDelta,
		ExpectedRecommendationAction: "migrate",
		ExpectedResultStatus:         ResultStatusOK,
	}
}

// ---------------------------------------------------------------------------
// US-023 primary VM validation test: co-locate (same namespace)
// ---------------------------------------------------------------------------

// TestUS023_ChattyColocation_CoLocate_VMValidation is the primary reproducible VM
// validation test case for US-023. It asserts every expected vs observed outcome
// for the co_locate recommendation path.
func TestUS023_ChattyColocation_CoLocate_VMValidation(t *testing.T) {
	snap := buildVMSnapshot()
	req := buildChattyColocateRequest(snap)
	ctx := BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})
	expected := buildExpectedColocateOutcomes()

	resp := RunChattyColocationScenario(ctx)

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

	t.Run("BAV_EdgeRPS", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, "colocation.edge.rps")
		if bav == nil {
			t.Fatal("colocation.edge.rps not found in BeforeAfterValues")
		}
		if bav.BeforeValue == nil || *bav.BeforeValue != expected.ExpectedEdgeRPSBefore {
			t.Errorf("edge.rps before: expected=%.2f, got=%v", expected.ExpectedEdgeRPSBefore, bav.BeforeValue)
		}
		if bav.AfterValue == nil || *bav.AfterValue != expected.ExpectedEdgeRPSAfter {
			t.Errorf("edge.rps after: expected=%.2f, got=%v", expected.ExpectedEdgeRPSAfter, bav.AfterValue)
		}
		if bav.DeltaValue == nil || *bav.DeltaValue != expected.ExpectedEdgeRPSDelta {
			t.Errorf("edge.rps delta: expected=%.2f, got=%v", expected.ExpectedEdgeRPSDelta, bav.DeltaValue)
		}
	})

	t.Run("BAV_LatencyP95", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, "colocation.edge.latency_p95_ms")
		if expected.ExpectedLatencyBefore == nil {
			if bav != nil {
				t.Error("expected latency_p95_ms BAV to be absent when no P95 data, but it was present")
			}
			return
		}
		if bav == nil {
			t.Fatal("colocation.edge.latency_p95_ms not found in BeforeAfterValues")
		}
		if bav.BeforeValue == nil || *bav.BeforeValue != *expected.ExpectedLatencyBefore {
			t.Errorf("latency_p95_ms before: expected=%.2f, got=%v", *expected.ExpectedLatencyBefore, bav.BeforeValue)
		}
		if bav.AfterValue == nil || *bav.AfterValue != *expected.ExpectedLatencyAfter {
			t.Errorf("latency_p95_ms after: expected=%.2f, got=%v", *expected.ExpectedLatencyAfter, bav.AfterValue)
		}
		if bav.DeltaValue == nil || *bav.DeltaValue != *expected.ExpectedLatencyDelta {
			t.Errorf("latency_p95_ms delta: expected=%.2f, got=%v", *expected.ExpectedLatencyDelta, bav.DeltaValue)
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
			"colocation.latency_reduction_factor",
			"colocation.rps_unchanged",
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
// US-023 secondary VM validation test: migrate (cross-namespace)
// ---------------------------------------------------------------------------

// TestUS023_ChattyColocation_Migrate_VMValidation validates the migrate recommendation
// path when the chatty pair spans different namespaces.
func TestUS023_ChattyColocation_Migrate_VMValidation(t *testing.T) {
	snap := buildChattyMigrateSnapshot()
	req := buildChattyMigrateRequest(snap)
	ctx := BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})
	expected := buildExpectedMigrateOutcomes()

	resp := RunChattyColocationScenario(ctx)

	t.Run("ResultStatus", func(t *testing.T) {
		if resp.ResultStatus != ResultStatusOK {
			t.Errorf("expected ResultStatus=OK, got=%q", resp.ResultStatus)
		}
	})

	t.Run("Recommendation_Migrate", func(t *testing.T) {
		if resp.Recommendation.Action != expected.ExpectedRecommendationAction {
			t.Errorf("expected recommendation=%q, got=%q",
				expected.ExpectedRecommendationAction, resp.Recommendation.Action)
		}
	})

	t.Run("BAV_EdgeRPS_Migrate", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, "colocation.edge.rps")
		if bav == nil {
			t.Fatal("colocation.edge.rps not found")
		}
		if bav.AfterValue == nil || *bav.AfterValue != expected.ExpectedEdgeRPSAfter {
			t.Errorf("edge.rps after: expected=%.2f, got=%v",
				expected.ExpectedEdgeRPSAfter, bav.AfterValue)
		}
	})

	t.Run("BAV_LatencyP95_Migrate", func(t *testing.T) {
		bav := findBAV(resp.BeforeAfterValues, "colocation.edge.latency_p95_ms")
		if bav == nil {
			t.Fatal("colocation.edge.latency_p95_ms not found")
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
// US-023 determinism test
// ---------------------------------------------------------------------------

// TestUS023_ChattyColocation_Determinism verifies two identical runs produce byte-equivalent
// canonical JSON output — required for panel replay demonstration.
func TestUS023_ChattyColocation_Determinism(t *testing.T) {
	snap := buildVMSnapshot()
	req := buildChattyColocateRequest(snap)
	ctx := BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})

	resp1 := RunChattyColocationScenario(ctx)
	resp2 := RunChattyColocationScenario(ctx)

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
// US-023 degraded-mode without Influx test
// ---------------------------------------------------------------------------

// TestUS023_ChattyColocation_DegradedModeWithoutInflux verifies that the scenario
// produces a valid result and a non-none degraded-mode label when InfluxDB is unavailable.
func TestUS023_ChattyColocation_DegradedModeWithoutInflux(t *testing.T) {
	snap := buildVMSnapshot()
	req := buildChattyColocateRequest(snap)
	ctx := BuildExecutionContext(req, snap, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})

	resp := RunChattyColocationScenario(ctx)

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
// US-023 validation report
// ---------------------------------------------------------------------------

// TestUS023_ChattyColocation_ValidationReport logs a structured validation report to test
// output for artifact capture. The report covers both co-locate and migrate cases.
func TestUS023_ChattyColocation_ValidationReport(t *testing.T) {
	// --- Co-locate case (same namespace) ---
	snapColoc := buildVMSnapshot()
	reqColoc := buildChattyColocateRequest(snapColoc)
	ctxColoc := BuildExecutionContext(reqColoc, snapColoc, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})
	expectedColoc := buildExpectedColocateOutcomes()
	respColoc := RunChattyColocationScenario(ctxColoc)

	observedPathSigsColoc := make([]string, len(respColoc.ImpactedPaths))
	for i, p := range respColoc.ImpactedPaths {
		observedPathSigsColoc[i] = pathSig(p)
	}
	sort.Strings(observedPathSigsColoc)

	t.Logf("=== US-023 VM Validation Report: Chatty-service Co-location / Migration ===")
	t.Logf("Snapshot Hash  : %s", snapColoc.SnapshotHash)
	t.Logf("Snapshot Time  : %s", snapColoc.SnapshotTimestamp)
	t.Logf("")

	t.Logf("--- Case 1: Co-locate (same namespace, api-gw → order, RPS=200) ---")
	t.Logf("Evidence Mode  : %s", respColoc.EvidenceMode)
	t.Logf("Confidence     : %s", respColoc.ConfidenceLevel)
	t.Logf("Degraded Mode  : %q", respColoc.DegradedMode)
	t.Logf("")
	t.Logf("Impacted Services:")
	for _, svc := range respColoc.ImpactedServices {
		t.Logf("  [%s] %s (%s)", svc.Role, svc.ServiceID, svc.Name)
	}
	t.Logf("Impacted Paths:")
	for _, sig := range observedPathSigsColoc {
		t.Logf("  %s", sig)
	}
	t.Logf("Before/After Values:")
	for _, bav := range respColoc.BeforeAfterValues {
		t.Logf("  %-45s before=%-10s after=%-10s delta=%s",
			bav.FieldRef,
			formatFloatPtr(bav.BeforeValue),
			formatFloatPtr(bav.AfterValue),
			formatFloatPtr(bav.DeltaValue),
		)
	}
	t.Logf("Recommendation : %s", respColoc.Recommendation.Action)
	t.Logf("")

	// --- Migrate case (cross-namespace) ---
	snapMig := buildChattyMigrateSnapshot()
	reqMig := buildChattyMigrateRequest(snapMig)
	ctxMig := BuildExecutionContext(reqMig, snapMig, InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	})
	expectedMig := buildExpectedMigrateOutcomes()
	respMig := RunChattyColocationScenario(ctxMig)

	t.Logf("--- Case 2: Migrate (cross-namespace, api-gw[gateway] → order[production], RPS=200) ---")
	t.Logf("Recommendation : %s", respMig.Recommendation.Action)
	t.Logf("Before/After Values:")
	for _, bav := range respMig.BeforeAfterValues {
		t.Logf("  %-45s before=%-10s after=%-10s delta=%s",
			bav.FieldRef,
			formatFloatPtr(bav.BeforeValue),
			formatFloatPtr(bav.AfterValue),
			formatFloatPtr(bav.DeltaValue),
		)
	}
	t.Logf("")

	// --- Pass/fail criteria ---
	latColocAfterRef := expectedColoc.ExpectedLatencyAfter
	latMigAfterRef := expectedMig.ExpectedLatencyAfter

	criteria := []struct {
		Name   string
		Passed bool
	}{
		{"[co-locate] ResultStatus == OK", respColoc.ResultStatus == ResultStatusOK},
		{"[co-locate] ImpactedServices count correct",
			len(respColoc.ImpactedServices) == len(expectedColoc.ExpectedImpactedServices)},
		{"[co-locate] ImpactedPaths count correct",
			len(respColoc.ImpactedPaths) == len(expectedColoc.ExpectedImpactedPathSigs)},
		{"[co-locate] edge.rps before=200",
			bavMatchesBefore(respColoc.BeforeAfterValues, "colocation.edge.rps", 200)},
		{"[co-locate] edge.rps after=200 (unchanged)",
			bavMatchesAfter(respColoc.BeforeAfterValues, "colocation.edge.rps", 200)},
		{"[co-locate] latency_p95_ms before=45.0",
			bavMatchesBefore(respColoc.BeforeAfterValues, "colocation.edge.latency_p95_ms", 45.0)},
		{"[co-locate] latency_p95_ms after=27.0", func() bool {
			return latColocAfterRef != nil &&
				bavMatchesAfter(respColoc.BeforeAfterValues, "colocation.edge.latency_p95_ms", *latColocAfterRef)
		}()},
		{"[co-locate] recommendation == co_locate",
			respColoc.Recommendation.Action == "co_locate"},
		{"[co-locate] contract validation passes",
			func() bool { return ValidateSimulationResponse(respColoc) == nil }()},
		{"[migrate] ResultStatus == OK", respMig.ResultStatus == ResultStatusOK},
		{"[migrate] edge.rps after=200 (unchanged)",
			bavMatchesAfter(respMig.BeforeAfterValues, "colocation.edge.rps", 200)},
		{"[migrate] latency_p95_ms after=27.0", func() bool {
			return latMigAfterRef != nil &&
				bavMatchesAfter(respMig.BeforeAfterValues, "colocation.edge.latency_p95_ms", *latMigAfterRef)
		}()},
		{"[migrate] recommendation == migrate",
			respMig.Recommendation.Action == "migrate"},
		{"[migrate] contract validation passes",
			func() bool { return ValidateSimulationResponse(respMig) == nil }()},
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
		t.Logf("OVERALL: PASS — Chatty-service Co-location/Migration scenario is panel-defensible on real VM topology")
	} else {
		t.Errorf("OVERALL: FAIL — one or more validation criteria did not match expected outcomes")
	}
}
