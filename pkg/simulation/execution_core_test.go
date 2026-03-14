package simulation

import (
	"bytes"
	"testing"
	"time"
)

// ---- helpers ----------------------------------------------------------------

func makeTestSnapshot() SimulationSnapshot {
	ts := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	input := SnapshotInput{
		Nodes: []SnapshotServiceNode{
			{ServiceID: "svc-c", Name: "service-c", Namespace: "default"},
			{ServiceID: "svc-a", Name: "service-a", Namespace: "default"},
			{ServiceID: "svc-b", Name: "service-b", Namespace: "default"},
		},
		Edges: []SnapshotServiceEdge{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-c", RateRPS: 10},
			{SourceServiceID: "svc-a", TargetServiceID: "svc-b", RateRPS: 5},
		},
		RuntimeServices: []SnapshotRuntimeService{
			{ServiceID: "svc-b", PodCount: 2, ReadyPods: 2},
			{ServiceID: "svc-a", PodCount: 3, ReadyPods: 3},
		},
	}
	return ComposeSnapshotAt(input, ts)
}

func makeTestRequest(scenario ScenarioType) SimulationRequest {
	req := SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      scenario,
		SnapshotTimestamp: "2024-01-15T10:00:00Z",
	}
	switch scenario {
	case ScenarioFailureShutdown:
		req.FailureShutdownParams = &FailureShutdownParams{TargetServiceID: "svc-a"}
	case ScenarioScaling:
		req.ScalingParams = &ScalingParams{TargetServiceID: "svc-a", CurrentPods: 2, NewPods: 4}
	case ScenarioTrafficSpike:
		req.TrafficSpikeParams = &TrafficSpikeParams{TargetServiceID: "svc-a", LoadMultiplier: 3.0}
	case ScenarioChattyColocation:
		req.ChattyColocationParams = &ChattyColocationParams{SourceServiceID: "svc-a", TargetServiceID: "svc-b"}
	case ScenarioNetworkCut:
		req.NetworkCutParams = &NetworkCutParams{AffectedLinks: []NetworkLink{
			{SourceServiceID: "svc-a", TargetServiceID: "svc-b"},
		}}
	}
	return req
}

func noInflux() InfluxCheckResult {
	return InfluxCheckResult{Reachable: false}
}

func fullInflux() InfluxCheckResult {
	return InfluxCheckResult{Reachable: true, DataSufficient: true, Sparse: false}
}

// ---- BuildExecutionContext --------------------------------------------------

func TestBuildExecutionContext_PopulatesAllFields(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeTestRequest(ScenarioFailureShutdown)
	ctx := BuildExecutionContext(req, snap, noInflux())

	if ctx.Request.ScenarioType != ScenarioFailureShutdown {
		t.Errorf("expected ScenarioType %q, got %q", ScenarioFailureShutdown, ctx.Request.ScenarioType)
	}
	if ctx.Snapshot.SnapshotHash == "" {
		t.Error("snapshot hash must not be empty")
	}
	if ctx.Evidence.Mode == "" {
		t.Error("evidence mode must be resolved")
	}
	if ctx.Evidence.Confidence == "" {
		t.Error("confidence level must be resolved")
	}
}

func TestBuildExecutionContext_SameInputsSameEvidence(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeTestRequest(ScenarioScaling)
	ctx1 := BuildExecutionContext(req, snap, noInflux())
	ctx2 := BuildExecutionContext(req, snap, noInflux())

	if ctx1.Evidence.Mode != ctx2.Evidence.Mode {
		t.Errorf("evidence mode not deterministic: %q vs %q", ctx1.Evidence.Mode, ctx2.Evidence.Mode)
	}
	if ctx1.Evidence.Confidence != ctx2.Evidence.Confidence {
		t.Errorf("confidence not deterministic: %q vs %q", ctx1.Evidence.Confidence, ctx2.Evidence.Confidence)
	}
}

func TestBuildExecutionContext_LiveTiersInferred(t *testing.T) {
	snap := makeTestSnapshot() // has nodes and runtime services
	ctx := BuildExecutionContext(makeTestRequest(ScenarioTrafficSpike), snap, noInflux())

	// snapshot has nodes → LiveServiceGraph, has RuntimeServices → LiveK8sRuntime
	if !ctx.Evidence.Availability.LiveServiceGraph {
		t.Error("expected LiveServiceGraph=true from snapshot nodes")
	}
	if !ctx.Evidence.Availability.LiveK8sRuntime {
		t.Error("expected LiveK8sRuntime=true from snapshot runtime services")
	}
}

func TestBuildExecutionContext_FullEvidence(t *testing.T) {
	snap := makeTestSnapshot()
	ctx := BuildExecutionContext(makeTestRequest(ScenarioNetworkCut), snap, fullInflux())

	if ctx.Evidence.Mode != EvidenceModeFull {
		t.Errorf("expected FULL mode, got %q", ctx.Evidence.Mode)
	}
	if ctx.Evidence.Confidence != ConfidenceHigh {
		t.Errorf("expected HIGH confidence, got %q", ctx.Evidence.Confidence)
	}
}

// ---- SortImpactedServices --------------------------------------------------

func TestSortImpactedServices_ByServiceID(t *testing.T) {
	services := []ImpactedService{
		{ServiceID: "svc-c", Name: "c"},
		{ServiceID: "svc-a", Name: "a"},
		{ServiceID: "svc-b", Name: "b"},
	}
	sorted := SortImpactedServices(services)
	ids := []string{sorted[0].ServiceID, sorted[1].ServiceID, sorted[2].ServiceID}
	expected := []string{"svc-a", "svc-b", "svc-c"}
	for i, id := range ids {
		if id != expected[i] {
			t.Errorf("position %d: expected %q, got %q", i, expected[i], id)
		}
	}
}

func TestSortImpactedServices_TieBreakByName(t *testing.T) {
	services := []ImpactedService{
		{ServiceID: "svc-a", Name: "zebra"},
		{ServiceID: "svc-a", Name: "alpha"},
	}
	sorted := SortImpactedServices(services)
	if sorted[0].Name != "alpha" {
		t.Errorf("expected tiebreak by name to put 'alpha' first, got %q", sorted[0].Name)
	}
}

func TestSortImpactedServices_Idempotent(t *testing.T) {
	services := []ImpactedService{
		{ServiceID: "svc-b"}, {ServiceID: "svc-a"}, {ServiceID: "svc-c"},
	}
	SortImpactedServices(services)
	firstPass := make([]string, len(services))
	for i, s := range services {
		firstPass[i] = s.ServiceID
	}
	SortImpactedServices(services)
	for i, s := range services {
		if s.ServiceID != firstPass[i] {
			t.Errorf("sort not idempotent at position %d: expected %q got %q", i, firstPass[i], s.ServiceID)
		}
	}
}

// ---- SortImpactedPaths -----------------------------------------------------

func TestSortImpactedPaths_Lexicographic(t *testing.T) {
	paths := []ImpactedPath{
		{Path: []string{"svc-c", "svc-a"}},
		{Path: []string{"svc-a", "svc-b"}},
		{Path: []string{"svc-a", "svc-a"}},
	}
	sorted := SortImpactedPaths(paths)
	if sorted[0].Path[0] != "svc-a" || sorted[0].Path[1] != "svc-a" {
		t.Errorf("expected first path [svc-a, svc-a], got %v", sorted[0].Path)
	}
	if sorted[1].Path[0] != "svc-a" || sorted[1].Path[1] != "svc-b" {
		t.Errorf("expected second path [svc-a, svc-b], got %v", sorted[1].Path)
	}
	if sorted[2].Path[0] != "svc-c" {
		t.Errorf("expected third path starting with svc-c, got %v", sorted[2].Path)
	}
}

func TestSortImpactedPaths_ShorterFirst(t *testing.T) {
	paths := []ImpactedPath{
		{Path: []string{"svc-a", "svc-b", "svc-c"}},
		{Path: []string{"svc-a", "svc-b"}},
	}
	sorted := SortImpactedPaths(paths)
	if len(sorted[0].Path) != 2 {
		t.Errorf("expected shorter path first, got length %d", len(sorted[0].Path))
	}
}

// ---- SortBeforeAfterValues -------------------------------------------------

func TestSortBeforeAfterValues_ByFieldRef(t *testing.T) {
	values := []BeforeAfterValue{
		{FieldRef: "latency.p99"},
		{FieldRef: "latency.p50"},
		{FieldRef: "error_rate"},
	}
	sorted := SortBeforeAfterValues(values)
	expected := []string{"error_rate", "latency.p50", "latency.p99"}
	for i, v := range sorted {
		if v.FieldRef != expected[i] {
			t.Errorf("position %d: expected %q got %q", i, expected[i], v.FieldRef)
		}
	}
}

// ---- SortAssumptions -------------------------------------------------------

func TestSortAssumptions_ByKey(t *testing.T) {
	assumptions := []SimulationAssumption{
		{Key: "pod_overhead"},
		{Key: "baseline_latency"},
		{Key: "error_propagation"},
	}
	sorted := SortAssumptions(assumptions)
	expected := []string{"baseline_latency", "error_propagation", "pod_overhead"}
	for i, a := range sorted {
		if a.Key != expected[i] {
			t.Errorf("position %d: expected %q got %q", i, expected[i], a.Key)
		}
	}
}

// ---- NormalizeResponse -----------------------------------------------------

func TestNormalizeResponse_SortsAllSlices(t *testing.T) {
	resp := SimulationResponse{
		ImpactedServices: []ImpactedService{
			{ServiceID: "svc-c"}, {ServiceID: "svc-a"}, {ServiceID: "svc-b"},
		},
		ImpactedPaths: []ImpactedPath{
			{Path: []string{"svc-c"}}, {Path: []string{"svc-a"}},
		},
		BeforeAfterValues: []BeforeAfterValue{
			{FieldRef: "z_field"}, {FieldRef: "a_field"},
		},
		Assumptions: []SimulationAssumption{
			{Key: "z_assumption"}, {Key: "a_assumption"},
		},
	}
	NormalizeResponse(&resp)

	if resp.ImpactedServices[0].ServiceID != "svc-a" {
		t.Errorf("ImpactedServices not sorted: first is %q", resp.ImpactedServices[0].ServiceID)
	}
	if resp.ImpactedPaths[0].Path[0] != "svc-a" {
		t.Errorf("ImpactedPaths not sorted: first path starts with %q", resp.ImpactedPaths[0].Path[0])
	}
	if resp.BeforeAfterValues[0].FieldRef != "a_field" {
		t.Errorf("BeforeAfterValues not sorted: first is %q", resp.BeforeAfterValues[0].FieldRef)
	}
	if resp.Assumptions[0].Key != "a_assumption" {
		t.Errorf("Assumptions not sorted: first is %q", resp.Assumptions[0].Key)
	}
}

// ---- CanonicalizeResponse + determinism ------------------------------------

func TestCanonicalizeResponse_SameInputsByteEqual(t *testing.T) {
	before := 100.0
	after := 80.0
	delta := -20.0

	resp := SimulationResponse{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioFailureShutdown,
		SnapshotTimestamp: "2024-01-15T10:00:00Z",
		SnapshotHash:      "abc123",
		ResultStatus:      ResultStatusOK,
		EvidenceSources:   []string{"live_service_graph", "deterministic_fallback"},
		EvidenceMode:      EvidenceModeDegraded,
		ConfidenceLevel:   ConfidenceLow,
		ImpactedServices: []ImpactedService{
			{ServiceID: "svc-b", Name: "service-b", Namespace: "default", Role: "downstream"},
		},
		ImpactedPaths: []ImpactedPath{
			{Path: []string{"svc-a", "svc-b"}},
		},
		BeforeAfterValues: []BeforeAfterValue{
			{FieldRef: "path.latency.p95", BeforeValue: &before, AfterValue: &after, DeltaValue: &delta},
		},
		Assumptions: []SimulationAssumption{
			{Key: "latency_model", Description: "linear degradation", Source: "engine_default"},
		},
		Recommendation: SimulationRecommendation{
			Action:      "failover",
			Explanation: "svc-a shutdown causes svc-b to lose its primary call path",
		},
	}

	NormalizeResponse(&resp)

	b1, err1 := CanonicalizeResponse(resp)
	b2, err2 := CanonicalizeResponse(resp)

	if err1 != nil || err2 != nil {
		t.Fatalf("CanonicalizeResponse error: %v / %v", err1, err2)
	}
	if !bytes.Equal(b1, b2) {
		t.Error("two CanonicalizeResponse calls on same value produced different bytes")
	}
}

func TestCanonicalizeResponse_DifferentInputsDifferentBytes(t *testing.T) {
	resp1 := SimulationResponse{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioScaling,
		SnapshotTimestamp: "2024-01-15T10:00:00Z",
		ResultStatus:      ResultStatusDeferred,
		DeferredReason:    "insufficient evidence",
		EvidenceSources:   []string{"deterministic_fallback"},
		EvidenceMode:      EvidenceModeFallback,
		ConfidenceLevel:   ConfidenceLow,
	}
	resp2 := resp1
	resp2.ResultStatus = ResultStatusOK
	resp2.DeferredReason = ""
	resp2.Recommendation = SimulationRecommendation{Action: "scale_up", Explanation: "pods added"}

	NormalizeResponse(&resp1)
	NormalizeResponse(&resp2)

	b1, _ := CanonicalizeResponse(resp1)
	b2, _ := CanonicalizeResponse(resp2)

	if bytes.Equal(b1, b2) {
		t.Error("different responses produced identical canonical bytes")
	}
}

// ---- EvidenceSourcesToStrings ----------------------------------------------

func TestEvidenceSourcesToStrings_Conversion(t *testing.T) {
	labels := []EvidenceSourceLabel{
		EvidenceSourceLiveServiceGraph,
		EvidenceSourceLiveK8sRuntime,
		EvidenceSourceDeterministicFallback,
	}
	strs := EvidenceSourcesToStrings(labels)
	if len(strs) != 3 {
		t.Fatalf("expected 3 strings, got %d", len(strs))
	}
	if strs[0] != "live_service_graph" {
		t.Errorf("expected 'live_service_graph', got %q", strs[0])
	}
	if strs[1] != "live_k8s_runtime" {
		t.Errorf("expected 'live_k8s_runtime', got %q", strs[1])
	}
	if strs[2] != "deterministic_fallback" {
		t.Errorf("expected 'deterministic_fallback', got %q", strs[2])
	}
}

func TestEvidenceSourcesToStrings_Empty(t *testing.T) {
	strs := EvidenceSourcesToStrings(nil)
	if len(strs) != 0 {
		t.Errorf("expected empty slice for nil input, got %v", strs)
	}
}

// ---- BuildBaseResponse -----------------------------------------------------

func TestBuildBaseResponse_RequiredFieldsPopulated(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeTestRequest(ScenarioTrafficSpike)
	ctx := BuildExecutionContext(req, snap, noInflux())

	resp := BuildBaseResponse(ctx)

	if resp.Version != SchemaVersion {
		t.Errorf("version: expected %q, got %q", SchemaVersion, resp.Version)
	}
	if resp.ScenarioType != ScenarioTrafficSpike {
		t.Errorf("scenarioType: expected %q, got %q", ScenarioTrafficSpike, resp.ScenarioType)
	}
	if resp.SnapshotTimestamp == "" {
		t.Error("snapshotTimestamp must not be empty")
	}
	if resp.SnapshotHash == "" {
		t.Error("snapshotHash must not be empty")
	}
	if len(resp.EvidenceSources) == 0 {
		t.Error("evidenceSources must not be empty")
	}
	if resp.EvidenceMode == "" {
		t.Error("evidenceMode must not be empty")
	}
	if resp.ConfidenceLevel == "" {
		t.Error("confidenceLevel must not be empty")
	}
}

func TestBuildBaseResponse_DeterministicForSameContext(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeTestRequest(ScenarioChattyColocation)
	ctx := BuildExecutionContext(req, snap, noInflux())

	r1 := BuildBaseResponse(ctx)
	r2 := BuildBaseResponse(ctx)

	NormalizeResponse(&r1)
	NormalizeResponse(&r2)

	b1, _ := CanonicalizeResponse(r1)
	b2, _ := CanonicalizeResponse(r2)

	if !bytes.Equal(b1, b2) {
		t.Error("BuildBaseResponse is not deterministic for same context")
	}
}

func TestBuildBaseResponse_SnapshotHashMatchesSnapshot(t *testing.T) {
	snap := makeTestSnapshot()
	ctx := BuildExecutionContext(makeTestRequest(ScenarioNetworkCut), snap, fullInflux())
	resp := BuildBaseResponse(ctx)

	if resp.SnapshotHash != snap.SnapshotHash {
		t.Errorf("response hash %q does not match snapshot hash %q", resp.SnapshotHash, snap.SnapshotHash)
	}
}

// ---- End-to-end determinism: same snapshot + request → byte-equal JSON ----

func TestEndToEnd_SameSnapshotAndRequest_ByteEqualJSON(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeTestRequest(ScenarioScaling)
	influx := noInflux()

	buildResult := func() []byte {
		ctx := BuildExecutionContext(req, snap, influx)
		resp := BuildBaseResponse(ctx)
		// Simulate scenario model output (deterministic values)
		before := 120.0
		after := 80.0
		delta := -40.0
		resp.ResultStatus = ResultStatusOK
		resp.ImpactedServices = []ImpactedService{
			{ServiceID: "svc-b", Name: "service-b", Namespace: "default", Role: "downstream"},
			{ServiceID: "svc-c", Name: "service-c", Namespace: "default", Role: "downstream"},
		}
		resp.ImpactedPaths = []ImpactedPath{
			{Path: []string{"svc-a", "svc-c"}},
			{Path: []string{"svc-a", "svc-b"}},
		}
		resp.BeforeAfterValues = []BeforeAfterValue{
			{FieldRef: "path.latency.p95", BeforeValue: &before, AfterValue: &after, DeltaValue: &delta, Unit: "ms"},
		}
		resp.Assumptions = []SimulationAssumption{
			{Key: "scaling_model", Description: "Amdahl approximation", Source: "engine_default"},
		}
		resp.Recommendation = SimulationRecommendation{
			Action:      "scale_up",
			Explanation: "increasing pods reduces per-pod load on svc-a",
		}

		NormalizeResponse(&resp)
		b, err := CanonicalizeResponse(resp)
		if err != nil {
			t.Fatalf("CanonicalizeResponse failed: %v", err)
		}
		return b
	}

	run1 := buildResult()
	run2 := buildResult()
	run3 := buildResult()

	if !bytes.Equal(run1, run2) {
		t.Error("run1 and run2 produced different bytes")
	}
	if !bytes.Equal(run2, run3) {
		t.Error("run2 and run3 produced different bytes")
	}
}
