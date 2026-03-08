package simulation

import (
	"testing"
)

// validBaseResponse returns a minimal valid SimulationResponse for an OK result.
func validBaseResponse() SimulationResponse {
	before := 50.0
	after := 100.0
	delta := 50.0
	return SimulationResponse{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioFailureShutdown,
		SnapshotTimestamp: "2024-01-15T10:00:00Z",
		SnapshotHash:      "sha256:abc123",
		ResultStatus:      ResultStatusOK,
		EvidenceSources:   []string{"live_service_graph", "live_k8s_runtime"},
		EvidenceMode:      EvidenceModePartial,
		ConfidenceLevel:   ConfidenceMedium,
		Assumptions: []SimulationAssumption{
			{Key: "latency_baseline", Description: "baseline latency from graph edge p95", Source: "live_service_graph"},
		},
		ImpactedServices: []ImpactedService{
			{ServiceID: "svc-checkout", Name: "checkout", Namespace: "default", Role: "target"},
		},
		ImpactedPaths: []ImpactedPath{
			{Path: []string{"svc-frontend", "svc-checkout"}},
		},
		BeforeAfterValues: []BeforeAfterValue{
			{
				FieldRef:    "path_latency_p95_ms",
				Description: "p95 latency for affected path",
				Unit:        "ms",
				BeforeValue: &before,
				AfterValue:  &after,
				DeltaValue:  &delta,
			},
		},
		Recommendation: SimulationRecommendation{
			Action:      "failover",
			Explanation: "Downstream callers of svc-checkout will lose traffic; failover to backup recommended.",
		},
	}
}

func TestValidateSimulationResponse_ValidOK(t *testing.T) {
	resp := validBaseResponse()
	if err := ValidateSimulationResponse(resp); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateSimulationResponse_ValidFullEvidenceMode(t *testing.T) {
	resp := validBaseResponse()
	resp.EvidenceMode = EvidenceModeFull
	resp.EvidenceSources = []string{"live_service_graph", "live_k8s_runtime", "historical_influxdb"}
	resp.ConfidenceLevel = ConfidenceHigh
	if err := ValidateSimulationResponse(resp); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateSimulationResponse_ValidDegradedMode(t *testing.T) {
	resp := validBaseResponse()
	resp.EvidenceMode = EvidenceModeDegraded
	resp.DegradedMode = DegradedModeInfluxEmpty
	resp.DegradedModeReason = "InfluxDB returned no historical data for the snapshot window."
	resp.ConfidenceLevel = ConfidenceLow
	if err := ValidateSimulationResponse(resp); err != nil {
		t.Fatalf("expected no error for degraded mode, got: %v", err)
	}
}

func TestValidateSimulationResponse_ValidDeferredStatus(t *testing.T) {
	resp := SimulationResponse{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioScaling,
		SnapshotTimestamp: "2024-01-15T10:00:00Z",
		ResultStatus:      ResultStatusDeferred,
		DeferredReason:    "Insufficient evidence to produce a defensible scaling estimate.",
		EvidenceSources:   []string{"deterministic_fallback"},
		EvidenceMode:      EvidenceModeFallback,
		ConfidenceLevel:   ConfidenceLow,
		Assumptions:       []SimulationAssumption{},
		ImpactedServices:  []ImpactedService{},
		ImpactedPaths:     []ImpactedPath{},
		BeforeAfterValues: []BeforeAfterValue{},
		Recommendation:    SimulationRecommendation{},
	}
	if err := ValidateSimulationResponse(resp); err != nil {
		t.Fatalf("expected no error for deferred status, got: %v", err)
	}
}

func TestValidateSimulationResponse_ValidUnsupportedStatus(t *testing.T) {
	resp := SimulationResponse{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioNetworkCut,
		SnapshotTimestamp: "2024-01-15T10:00:00Z",
		ResultStatus:      ResultStatusUnsupported,
		DeferredReason:    "Scenario parameters do not identify any known service edges.",
		EvidenceSources:   []string{"live_service_graph"},
		EvidenceMode:      EvidenceModePartial,
		ConfidenceLevel:   ConfidenceLow,
		Assumptions:       []SimulationAssumption{},
		ImpactedServices:  []ImpactedService{},
		ImpactedPaths:     []ImpactedPath{},
		BeforeAfterValues: []BeforeAfterValue{},
		Recommendation:    SimulationRecommendation{},
	}
	if err := ValidateSimulationResponse(resp); err != nil {
		t.Fatalf("expected no error for unsupported status, got: %v", err)
	}
}

func TestValidateSimulationResponse_MissingVersion(t *testing.T) {
	resp := validBaseResponse()
	resp.Version = ""
	err := ValidateSimulationResponse(resp)
	assertErrorCode(t, err, ErrRespCodeMissingVersion)
}

func TestValidateSimulationResponse_InvalidVersion(t *testing.T) {
	resp := validBaseResponse()
	resp.Version = "v99"
	err := ValidateSimulationResponse(resp)
	assertErrorCode(t, err, ErrRespCodeInvalidVersion)
}

func TestValidateSimulationResponse_MissingScenarioType(t *testing.T) {
	resp := validBaseResponse()
	resp.ScenarioType = ""
	err := ValidateSimulationResponse(resp)
	assertErrorCode(t, err, ErrRespCodeMissingScenarioType)
}

func TestValidateSimulationResponse_InvalidScenarioType(t *testing.T) {
	resp := validBaseResponse()
	resp.ScenarioType = "unsupported_scenario"
	err := ValidateSimulationResponse(resp)
	assertErrorCode(t, err, ErrRespCodeInvalidScenarioType)
}

func TestValidateSimulationResponse_MissingSnapshotTimestamp(t *testing.T) {
	resp := validBaseResponse()
	resp.SnapshotTimestamp = ""
	err := ValidateSimulationResponse(resp)
	assertErrorCode(t, err, ErrRespCodeMissingSnapshotTimestamp)
}

func TestValidateSimulationResponse_MissingResultStatus(t *testing.T) {
	resp := validBaseResponse()
	resp.ResultStatus = ""
	err := ValidateSimulationResponse(resp)
	assertErrorCode(t, err, ErrRespCodeMissingResultStatus)
}

func TestValidateSimulationResponse_InvalidResultStatus(t *testing.T) {
	resp := validBaseResponse()
	resp.ResultStatus = "UNKNOWN_STATUS"
	err := ValidateSimulationResponse(resp)
	assertErrorCode(t, err, ErrRespCodeInvalidResultStatus)
}

func TestValidateSimulationResponse_EmptyEvidenceSources(t *testing.T) {
	resp := validBaseResponse()
	resp.EvidenceSources = nil
	err := ValidateSimulationResponse(resp)
	assertErrorCode(t, err, ErrRespCodeMissingEvidenceSources)
}

func TestValidateSimulationResponse_MissingEvidenceMode(t *testing.T) {
	resp := validBaseResponse()
	resp.EvidenceMode = ""
	err := ValidateSimulationResponse(resp)
	assertErrorCode(t, err, ErrRespCodeMissingEvidenceMode)
}

func TestValidateSimulationResponse_InvalidEvidenceMode(t *testing.T) {
	resp := validBaseResponse()
	resp.EvidenceMode = "UNKNOWN_MODE"
	err := ValidateSimulationResponse(resp)
	assertErrorCode(t, err, ErrRespCodeInvalidEvidenceMode)
}

func TestValidateSimulationResponse_MissingConfidenceLevel(t *testing.T) {
	resp := validBaseResponse()
	resp.ConfidenceLevel = ""
	err := ValidateSimulationResponse(resp)
	assertErrorCode(t, err, ErrRespCodeMissingConfidenceLevel)
}

func TestValidateSimulationResponse_InvalidConfidenceLevel(t *testing.T) {
	resp := validBaseResponse()
	resp.ConfidenceLevel = "VERY_HIGH"
	err := ValidateSimulationResponse(resp)
	assertErrorCode(t, err, ErrRespCodeInvalidConfidenceLevel)
}

func TestValidateSimulationResponse_DegradedModeWithoutReason(t *testing.T) {
	resp := validBaseResponse()
	resp.DegradedMode = DegradedModeInfluxEmpty
	resp.DegradedModeReason = "" // missing reason
	err := ValidateSimulationResponse(resp)
	assertErrorCode(t, err, ErrRespCodeMissingDegradedReason)
}

func TestValidateSimulationResponse_DeferredWithoutReason(t *testing.T) {
	resp := validBaseResponse()
	resp.ResultStatus = ResultStatusDeferred
	resp.DeferredReason = "" // missing
	err := ValidateSimulationResponse(resp)
	assertErrorCode(t, err, ErrRespCodeMissingDeferredReason)
}

func TestValidateSimulationResponse_UnsupportedWithoutReason(t *testing.T) {
	resp := validBaseResponse()
	resp.ResultStatus = ResultStatusUnsupported
	resp.DeferredReason = "" // missing
	err := ValidateSimulationResponse(resp)
	assertErrorCode(t, err, ErrRespCodeMissingDeferredReason)
}

func TestValidateSimulationResponse_OKWithoutRecommendationAction(t *testing.T) {
	resp := validBaseResponse()
	resp.Recommendation.Action = ""
	err := ValidateSimulationResponse(resp)
	assertErrorCode(t, err, ErrRespCodeMissingRecommendationAction)
}

func TestValidateSimulationResponse_SnapshotHashOptional(t *testing.T) {
	resp := validBaseResponse()
	resp.SnapshotHash = ""
	if err := ValidateSimulationResponse(resp); err != nil {
		t.Fatalf("expected no error when snapshotHash is absent, got: %v", err)
	}
}

func TestValidateSimulationResponse_DegradedModeNoneNoReasonRequired(t *testing.T) {
	resp := validBaseResponse()
	resp.DegradedMode = DegradedModeNone
	resp.DegradedModeReason = ""
	if err := ValidateSimulationResponse(resp); err != nil {
		t.Fatalf("expected no error when degradedMode is not set, got: %v", err)
	}
}

func TestValidateSimulationResponse_AllSupportedScenarioTypes(t *testing.T) {
	scenarios := []ScenarioType{
		ScenarioFailureShutdown,
		ScenarioScaling,
		ScenarioTrafficSpike,
		ScenarioChattyColocation,
		ScenarioNetworkCut,
	}
	for _, sc := range scenarios {
		resp := validBaseResponse()
		resp.ScenarioType = sc
		if err := ValidateSimulationResponse(resp); err != nil {
			t.Errorf("scenario %q: expected no error, got: %v", sc, err)
		}
	}
}

func TestValidateSimulationResponse_AllEvidenceModes(t *testing.T) {
	modes := []EvidenceMode{
		EvidenceModeFull,
		EvidenceModePartial,
		EvidenceModeDegraded,
		EvidenceModeFallback,
	}
	for _, mode := range modes {
		resp := validBaseResponse()
		resp.EvidenceMode = mode
		if mode == EvidenceModeDegraded {
			resp.DegradedMode = DegradedModeInfluxSparse
			resp.DegradedModeReason = "sparse data"
		}
		if err := ValidateSimulationResponse(resp); err != nil {
			t.Errorf("evidenceMode %q: expected no error, got: %v", mode, err)
		}
	}
}

func TestValidateSimulationResponse_AllConfidenceLevels(t *testing.T) {
	levels := []ConfidenceLevel{ConfidenceHigh, ConfidenceMedium, ConfidenceLow}
	for _, level := range levels {
		resp := validBaseResponse()
		resp.ConfidenceLevel = level
		if err := ValidateSimulationResponse(resp); err != nil {
			t.Errorf("confidenceLevel %q: expected no error, got: %v", level, err)
		}
	}
}

func TestValidateSimulationResponse_DeterministicValidation(t *testing.T) {
	// Same invalid response must always produce the same error codes.
	resp := SimulationResponse{}
	err1 := ValidateSimulationResponse(resp)
	err2 := ValidateSimulationResponse(resp)
	if err1 == nil || err2 == nil {
		t.Fatal("expected validation errors for empty response")
	}
	if err1.Error() != err2.Error() {
		t.Fatalf("validation is not deterministic:\nrun1: %v\nrun2: %v", err1, err2)
	}
}

func TestSimulationResponseFields_BeforeAfterValue(t *testing.T) {
	before := 10.0
	after := 20.0
	delta := 10.0
	bav := BeforeAfterValue{
		FieldRef:    "path_latency_p95_ms",
		Description: "p95 path latency",
		Unit:        "ms",
		BeforeValue: &before,
		AfterValue:  &after,
		DeltaValue:  &delta,
	}
	if bav.FieldRef != "path_latency_p95_ms" {
		t.Errorf("unexpected FieldRef: %s", bav.FieldRef)
	}
	if *bav.DeltaValue != 10.0 {
		t.Errorf("unexpected DeltaValue: %f", *bav.DeltaValue)
	}
}

func TestSimulationResponseFields_ImpactedServiceAndPath(t *testing.T) {
	svc := ImpactedService{ServiceID: "svc-a", Name: "a", Namespace: "default", Role: "caller"}
	path := ImpactedPath{Path: []string{"svc-a", "svc-b", "svc-c"}}
	if svc.Role != "caller" {
		t.Errorf("unexpected role: %s", svc.Role)
	}
	if len(path.Path) != 3 {
		t.Errorf("expected 3 path elements, got %d", len(path.Path))
	}
}
