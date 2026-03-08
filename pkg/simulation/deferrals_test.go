package simulation

import (
	"strings"
	"testing"
	"time"
)

// makeCtxWithMode builds an ExecutionContext with the given evidence mode for testing.
func makeCtxWithMode(mode EvidenceMode, scenarioType ScenarioType) ExecutionContext {
	snap := ComposeSnapshotAt(SnapshotInput{
		Nodes: []SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "svc-a", Namespace: "default"},
		},
	}, time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))

	req := SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      scenarioType,
		SnapshotTimestamp: snap.SnapshotTimestamp,
	}

	// Build a mock evidence result with the desired mode.
	confidence := DetermineConfidenceLevel(mode)
	var degradedMode DegradedMode
	var degradedReason string
	var sources []EvidenceSourceLabel

	switch mode {
	case EvidenceModeFull:
		sources = []EvidenceSourceLabel{
			EvidenceSourceLiveServiceGraph,
			EvidenceSourceLiveK8sRuntime,
			EvidenceSourceHistoricalInfluxDB,
			EvidenceSourceDeterministicFallback,
		}
	case EvidenceModePartial:
		sources = []EvidenceSourceLabel{
			EvidenceSourceLiveServiceGraph,
			EvidenceSourceLiveK8sRuntime,
			EvidenceSourceDeterministicFallback,
		}
		degradedMode = DegradedModeInfluxEmpty
		degradedReason = "InfluxDB returned no data"
	case EvidenceModeDegraded:
		sources = []EvidenceSourceLabel{
			EvidenceSourceLiveServiceGraph,
			EvidenceSourceDeterministicFallback,
		}
		degradedMode = DegradedModeInfluxEmpty
		degradedReason = "InfluxDB returned no data"
	case EvidenceModeFallback:
		sources = []EvidenceSourceLabel{EvidenceSourceDeterministicFallback}
		degradedMode = DegradedModeInfluxEmpty
		degradedReason = "no live data available"
	}

	evidence := EvidenceResolverResult{
		Mode:          mode,
		Sources:       sources,
		DegradedMode:  degradedMode,
		DegradedReason: degradedReason,
		Confidence:    confidence,
	}

	return ExecutionContext{
		Request:  req,
		Snapshot: snap,
		Evidence: evidence,
	}
}

// --- SupportedScenarios ---

func TestSupportedScenarios_ReturnsFiveScenarios(t *testing.T) {
	scenarios := SupportedScenarios()
	if len(scenarios) != 5 {
		t.Errorf("expected 5 supported scenarios, got %d", len(scenarios))
	}
}

func TestSupportedScenarios_ContainsAllLocked(t *testing.T) {
	scenarios := SupportedScenarios()
	required := []ScenarioType{
		ScenarioFailureShutdown,
		ScenarioScaling,
		ScenarioTrafficSpike,
		ScenarioChattyColocation,
		ScenarioNetworkCut,
	}
	set := make(map[ScenarioType]struct{}, len(scenarios))
	for _, s := range scenarios {
		set[s] = struct{}{}
	}
	for _, r := range required {
		if _, ok := set[r]; !ok {
			t.Errorf("SupportedScenarios missing required scenario %q", r)
		}
	}
}

func TestSupportedScenarios_ExcludesUnsupportedPaths(t *testing.T) {
	unsupported := []ScenarioType{
		"auto_remediation",
		"ml_anomaly",
		"capacity_planning",
		"cost_optimization",
		"",
	}
	scenarios := SupportedScenarios()
	set := make(map[ScenarioType]struct{}, len(scenarios))
	for _, s := range scenarios {
		set[s] = struct{}{}
	}
	for _, u := range unsupported {
		if _, ok := set[u]; ok {
			t.Errorf("SupportedScenarios must not include unsupported scenario %q", u)
		}
	}
}

// --- IsScenarioSupported ---

func TestIsScenarioSupported_TrueForAllFive(t *testing.T) {
	for _, s := range SupportedScenarios() {
		if !IsScenarioSupported(s) {
			t.Errorf("IsScenarioSupported(%q) = false, want true", s)
		}
	}
}

func TestIsScenarioSupported_FalseForUnsupported(t *testing.T) {
	unsupported := []ScenarioType{"auto_remediation", "ml_anomaly", ""}
	for _, u := range unsupported {
		if IsScenarioSupported(u) {
			t.Errorf("IsScenarioSupported(%q) = true, want false", u)
		}
	}
}

// --- EvidenceSufficientForScenario ---

func TestEvidenceSufficient_FullMode(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeFull, ScenarioFailureShutdown)
	ok, reason := EvidenceSufficientForScenario(ctx)
	if !ok {
		t.Errorf("FULL mode should be sufficient; got reason: %s", reason)
	}
}

func TestEvidenceSufficient_PartialMode(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModePartial, ScenarioScaling)
	ok, reason := EvidenceSufficientForScenario(ctx)
	if !ok {
		t.Errorf("PARTIAL mode should be sufficient; got reason: %s", reason)
	}
}

func TestEvidenceSufficient_DegradedMode(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeDegraded, ScenarioTrafficSpike)
	ok, reason := EvidenceSufficientForScenario(ctx)
	if !ok {
		t.Errorf("DEGRADED mode should be sufficient; got reason: %s", reason)
	}
}

func TestEvidenceSufficient_FallbackMode_Insufficient(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeFallback, ScenarioFailureShutdown)
	ok, reason := EvidenceSufficientForScenario(ctx)
	if ok {
		t.Error("FALLBACK mode should not be sufficient")
	}
	if reason == "" {
		t.Error("FALLBACK insufficiency must include a non-empty reason")
	}
	if !strings.Contains(reason, "failure_shutdown") {
		t.Errorf("reason should mention scenario type; got %q", reason)
	}
}

func TestEvidenceSufficient_FallbackMode_AllScenarios(t *testing.T) {
	for _, s := range SupportedScenarios() {
		ctx := makeCtxWithMode(EvidenceModeFallback, s)
		ok, reason := EvidenceSufficientForScenario(ctx)
		if ok {
			t.Errorf("scenario %q: FALLBACK should be insufficient", s)
		}
		if reason == "" {
			t.Errorf("scenario %q: FALLBACK insufficiency must provide a reason", s)
		}
	}
}

// --- BuildDeferredResponse ---

func TestBuildDeferredResponse_HasDeferredStatus(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeFallback, ScenarioFailureShutdown)
	resp := BuildDeferredResponse(ctx, "no live data")
	if resp.ResultStatus != ResultStatusDeferred {
		t.Errorf("expected DEFERRED, got %q", resp.ResultStatus)
	}
}

func TestBuildDeferredResponse_HasReason(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeFallback, ScenarioFailureShutdown)
	resp := BuildDeferredResponse(ctx, "no live data")
	if resp.DeferredReason != "no live data" {
		t.Errorf("expected reason %q, got %q", "no live data", resp.DeferredReason)
	}
}

func TestBuildDeferredResponse_NoBeforeAfterValues(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeFallback, ScenarioScaling)
	resp := BuildDeferredResponse(ctx, "insufficient evidence")
	if len(resp.BeforeAfterValues) != 0 {
		t.Errorf("deferred response must not contain BeforeAfterValues, got %d", len(resp.BeforeAfterValues))
	}
}

func TestBuildDeferredResponse_NoImpactedServices(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeFallback, ScenarioFailureShutdown)
	resp := BuildDeferredResponse(ctx, "no live data")
	if len(resp.ImpactedServices) != 0 {
		t.Errorf("deferred response must not contain ImpactedServices, got %d", len(resp.ImpactedServices))
	}
}

func TestBuildDeferredResponse_NoRecommendationAction(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeFallback, ScenarioFailureShutdown)
	resp := BuildDeferredResponse(ctx, "no live data")
	if resp.Recommendation.Action != "" {
		t.Errorf("deferred response must not contain recommendation.action, got %q", resp.Recommendation.Action)
	}
}

func TestBuildDeferredResponse_EvidenceMetadataPreserved(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeFallback, ScenarioTrafficSpike)
	resp := BuildDeferredResponse(ctx, "no live data")
	if resp.EvidenceMode != EvidenceModeFallback {
		t.Errorf("expected evidence mode FALLBACK, got %q", resp.EvidenceMode)
	}
	if resp.SnapshotTimestamp == "" {
		t.Error("deferred response must preserve snapshot timestamp")
	}
	if resp.Version != SchemaVersion {
		t.Errorf("expected version %q, got %q", SchemaVersion, resp.Version)
	}
}

// --- BuildUnsupportedResponse ---

func TestBuildUnsupportedResponse_HasUnsupportedStatus(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeFull, ScenarioFailureShutdown)
	resp := BuildUnsupportedResponse(ctx, "scenario parameters out of contract")
	if resp.ResultStatus != ResultStatusUnsupported {
		t.Errorf("expected UNSUPPORTED, got %q", resp.ResultStatus)
	}
}

func TestBuildUnsupportedResponse_NoNumericOutput(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeFull, ScenarioScaling)
	resp := BuildUnsupportedResponse(ctx, "out of contract")
	if len(resp.BeforeAfterValues) != 0 {
		t.Errorf("unsupported response must not contain BeforeAfterValues, got %d", len(resp.BeforeAfterValues))
	}
	if resp.Recommendation.Action != "" {
		t.Errorf("unsupported response must not contain recommendation.action, got %q", resp.Recommendation.Action)
	}
}

// --- EnforceDeferredConstraints ---

func TestEnforceDeferredConstraints_StripsBAVFromDeferred(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeFallback, ScenarioFailureShutdown)
	resp := BuildBaseResponse(ctx)
	resp.ResultStatus = ResultStatusDeferred
	resp.DeferredReason = "test deferral"
	// Inject guessed values that should be stripped.
	v := 100.0
	resp.BeforeAfterValues = []BeforeAfterValue{
		{FieldRef: "fake", BeforeValue: &v, AfterValue: &v},
	}
	resp.Recommendation = SimulationRecommendation{Action: "scale_up", Explanation: "guessed"}

	EnforceDeferredConstraints(&resp)

	if len(resp.BeforeAfterValues) != 0 {
		t.Errorf("EnforceDeferredConstraints should strip BeforeAfterValues, got %d", len(resp.BeforeAfterValues))
	}
	if resp.Recommendation.Action != "" {
		t.Errorf("EnforceDeferredConstraints should clear recommendation.action, got %q", resp.Recommendation.Action)
	}
}

func TestEnforceDeferredConstraints_StripsFromUnsupported(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeFull, ScenarioScaling)
	resp := BuildBaseResponse(ctx)
	resp.ResultStatus = ResultStatusUnsupported
	resp.DeferredReason = "out of contract"
	v := 5.0
	resp.BeforeAfterValues = []BeforeAfterValue{{FieldRef: "pod_count", BeforeValue: &v, AfterValue: &v}}

	EnforceDeferredConstraints(&resp)

	if len(resp.BeforeAfterValues) != 0 {
		t.Errorf("should strip BeforeAfterValues from UNSUPPORTED response")
	}
}

func TestEnforceDeferredConstraints_DoesNotAffectOK(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeFull, ScenarioScaling)
	resp := BuildBaseResponse(ctx)
	resp.ResultStatus = ResultStatusOK
	v := 5.0
	resp.BeforeAfterValues = []BeforeAfterValue{{FieldRef: "pod_count", BeforeValue: &v, AfterValue: &v}}
	resp.Recommendation = SimulationRecommendation{Action: "approve_scale_up"}

	EnforceDeferredConstraints(&resp)

	if len(resp.BeforeAfterValues) != 1 {
		t.Errorf("EnforceDeferredConstraints must not affect OK responses")
	}
	if resp.Recommendation.Action != "approve_scale_up" {
		t.Errorf("EnforceDeferredConstraints must not clear recommendation for OK response")
	}
}

// --- ValidateDeferredConstraints ---

func TestValidateDeferredConstraints_OKPassesAlways(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeFull, ScenarioScaling)
	resp := BuildBaseResponse(ctx)
	resp.ResultStatus = ResultStatusOK
	v := 5.0
	resp.BeforeAfterValues = []BeforeAfterValue{{FieldRef: "pod_count", BeforeValue: &v, AfterValue: &v}}

	if err := ValidateDeferredConstraints(resp); err != nil {
		t.Errorf("OK response should always pass deferred constraint validation; got: %v", err)
	}
}

func TestValidateDeferredConstraints_DeferredWithBAVFails(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeFallback, ScenarioFailureShutdown)
	resp := BuildBaseResponse(ctx)
	resp.ResultStatus = ResultStatusDeferred
	resp.DeferredReason = "fallback only"
	v := 100.0
	resp.BeforeAfterValues = []BeforeAfterValue{{FieldRef: "fake", BeforeValue: &v, AfterValue: &v}}

	if err := ValidateDeferredConstraints(resp); err == nil {
		t.Error("DEFERRED response with BeforeAfterValues should fail constraint validation")
	}
}

func TestValidateDeferredConstraints_DeferredWithActionFails(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeFallback, ScenarioFailureShutdown)
	resp := BuildDeferredResponse(ctx, "no live data")
	// Manually inject an action.
	resp.Recommendation = SimulationRecommendation{Action: "scale_up"}

	if err := ValidateDeferredConstraints(resp); err == nil {
		t.Error("DEFERRED response with recommendation.action should fail constraint validation")
	}
}

func TestValidateDeferredConstraints_CleanDeferredPasses(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeFallback, ScenarioFailureShutdown)
	resp := BuildDeferredResponse(ctx, "no live data")

	if err := ValidateDeferredConstraints(resp); err != nil {
		t.Errorf("clean DEFERRED response should pass constraint validation; got: %v", err)
	}
}

func TestValidateDeferredConstraints_UnsupportedWithBAVFails(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeFull, ScenarioScaling)
	resp := BuildBaseResponse(ctx)
	resp.ResultStatus = ResultStatusUnsupported
	resp.DeferredReason = "out of contract"
	v := 3.0
	resp.BeforeAfterValues = []BeforeAfterValue{{FieldRef: "pod_count", BeforeValue: &v, AfterValue: &v}}

	if err := ValidateDeferredConstraints(resp); err == nil {
		t.Error("UNSUPPORTED response with BeforeAfterValues should fail constraint validation")
	}
}

// --- Integration: guardrail chain ---

func TestGuardrailChain_FallbackEvidenceProducesDeferredNoNumericValues(t *testing.T) {
	ctx := makeCtxWithMode(EvidenceModeFallback, ScenarioFailureShutdown)

	sufficient, reason := EvidenceSufficientForScenario(ctx)
	if sufficient {
		t.Fatal("FALLBACK should not be sufficient")
	}

	resp := BuildDeferredResponse(ctx, reason)
	EnforceDeferredConstraints(&resp)

	if err := ValidateDeferredConstraints(resp); err != nil {
		t.Errorf("guardrail chain produced invalid response: %v", err)
	}
	if resp.ResultStatus != ResultStatusDeferred {
		t.Errorf("expected DEFERRED, got %q", resp.ResultStatus)
	}
	if resp.DeferredReason == "" {
		t.Error("deferred reason must be non-empty")
	}
	if len(resp.BeforeAfterValues) != 0 {
		t.Error("no numeric values should be present in deferred response")
	}
}
