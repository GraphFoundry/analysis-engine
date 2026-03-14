package simulation

import (
	"strings"
	"testing"
)

// validBaseRequest returns a minimal valid SimulationRequest for failure_shutdown.
func validBaseRequest() SimulationRequest {
	return SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioFailureShutdown,
		SnapshotTimestamp: "2024-01-15T10:00:00Z",
		FailureShutdownParams: &FailureShutdownParams{
			TargetServiceID: "svc-checkout",
		},
	}
}

func TestValidateSimulationRequest_ValidFailureShutdown(t *testing.T) {
	req := validBaseRequest()
	if err := ValidateSimulationRequest(req); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateSimulationRequest_ValidScaling(t *testing.T) {
	req := SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioScaling,
		SnapshotTimestamp: "2024-01-15T10:00:00Z",
		ScalingParams: &ScalingParams{
			TargetServiceID: "svc-payment",
			CurrentPods:     3,
			NewPods:         6,
		},
	}
	if err := ValidateSimulationRequest(req); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateSimulationRequest_ValidTrafficSpike(t *testing.T) {
	req := SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioTrafficSpike,
		SnapshotTimestamp: "2024-01-15T10:00:00Z",
		TrafficSpikeParams: &TrafficSpikeParams{
			TargetServiceID: "svc-frontend",
			LoadMultiplier:  3.0,
		},
	}
	if err := ValidateSimulationRequest(req); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateSimulationRequest_ValidChattyColocation(t *testing.T) {
	req := SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioChattyColocation,
		SnapshotTimestamp: "2024-01-15T10:00:00Z",
		ChattyColocationParams: &ChattyColocationParams{
			SourceServiceID: "svc-a",
			TargetServiceID: "svc-b",
		},
	}
	if err := ValidateSimulationRequest(req); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateSimulationRequest_ValidNetworkCut(t *testing.T) {
	deg := 50.0
	req := SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioNetworkCut,
		SnapshotTimestamp: "2024-01-15T10:00:00Z",
		NetworkCutParams: &NetworkCutParams{
			AffectedLinks: []NetworkLink{
				{SourceServiceID: "svc-a", TargetServiceID: "svc-b"},
			},
			DegradationPercent: &deg,
		},
	}
	if err := ValidateSimulationRequest(req); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateSimulationRequest_MissingVersion(t *testing.T) {
	req := validBaseRequest()
	req.Version = ""
	err := ValidateSimulationRequest(req)
	assertErrorCode(t, err, ErrCodeMissingVersion)
}

func TestValidateSimulationRequest_InvalidVersion(t *testing.T) {
	req := validBaseRequest()
	req.Version = "v99"
	err := ValidateSimulationRequest(req)
	assertErrorCode(t, err, ErrCodeInvalidVersion)
}

func TestValidateSimulationRequest_MissingScenarioType(t *testing.T) {
	req := validBaseRequest()
	req.ScenarioType = ""
	err := ValidateSimulationRequest(req)
	assertErrorCode(t, err, ErrCodeMissingScenarioType)
}

func TestValidateSimulationRequest_UnsupportedScenarioType(t *testing.T) {
	req := validBaseRequest()
	req.ScenarioType = "unsupported_scenario"
	err := ValidateSimulationRequest(req)
	assertErrorCode(t, err, ErrCodeUnsupportedScenario)
}

func TestValidateSimulationRequest_MissingSnapshotTimestamp(t *testing.T) {
	req := validBaseRequest()
	req.SnapshotTimestamp = ""
	err := ValidateSimulationRequest(req)
	assertErrorCode(t, err, ErrCodeMissingSnapshotRef)
}

func TestValidateSimulationRequest_InvalidSnapshotTimestamp(t *testing.T) {
	req := validBaseRequest()
	req.SnapshotTimestamp = "not-a-timestamp"
	err := ValidateSimulationRequest(req)
	assertErrorCode(t, err, ErrCodeInvalidSnapshotTS)
}

func TestValidateSimulationRequest_MissingFailureShutdownParams(t *testing.T) {
	req := validBaseRequest()
	req.FailureShutdownParams = nil
	err := ValidateSimulationRequest(req)
	assertErrorCode(t, err, ErrCodeMissingScenarioParams)
}

func TestValidateSimulationRequest_EmptyTargetServiceID_Failure(t *testing.T) {
	req := validBaseRequest()
	req.FailureShutdownParams.TargetServiceID = ""
	err := ValidateSimulationRequest(req)
	assertErrorCode(t, err, ErrCodeInvalidScenarioParams)
}

func TestValidateSimulationRequest_ScalingCurrentPodsZero(t *testing.T) {
	req := SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioScaling,
		SnapshotTimestamp: "2024-01-15T10:00:00Z",
		ScalingParams: &ScalingParams{
			TargetServiceID: "svc-payment",
			CurrentPods:     0,
			NewPods:         3,
		},
	}
	err := ValidateSimulationRequest(req)
	assertErrorCode(t, err, ErrCodeInvalidScenarioParams)
}

func TestValidateSimulationRequest_TrafficSpikeLoadMultiplierTooLow(t *testing.T) {
	req := SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioTrafficSpike,
		SnapshotTimestamp: "2024-01-15T10:00:00Z",
		TrafficSpikeParams: &TrafficSpikeParams{
			TargetServiceID: "svc-frontend",
			LoadMultiplier:  1.0, // must be > 1.0
		},
	}
	err := ValidateSimulationRequest(req)
	assertErrorCode(t, err, ErrCodeInvalidScenarioParams)
}

func TestValidateSimulationRequest_NetworkCutEmptyLinks(t *testing.T) {
	req := SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioNetworkCut,
		SnapshotTimestamp: "2024-01-15T10:00:00Z",
		NetworkCutParams: &NetworkCutParams{
			AffectedLinks: []NetworkLink{},
		},
	}
	err := ValidateSimulationRequest(req)
	assertErrorCode(t, err, ErrCodeInvalidScenarioParams)
}

func TestValidateSimulationRequest_NetworkCutDegradationOutOfRange(t *testing.T) {
	deg := 150.0
	req := SimulationRequest{
		Version:           SchemaVersion,
		ScenarioType:      ScenarioNetworkCut,
		SnapshotTimestamp: "2024-01-15T10:00:00Z",
		NetworkCutParams: &NetworkCutParams{
			AffectedLinks: []NetworkLink{
				{SourceServiceID: "svc-a", TargetServiceID: "svc-b"},
			},
			DegradationPercent: &deg,
		},
	}
	err := ValidateSimulationRequest(req)
	assertErrorCode(t, err, ErrCodeInvalidScenarioParams)
}

func TestValidateSimulationRequest_DeterministicErrorCodes(t *testing.T) {
	// Same invalid request must always return the same error codes.
	req := SimulationRequest{}
	err1 := ValidateSimulationRequest(req)
	err2 := ValidateSimulationRequest(req)
	if err1.Error() != err2.Error() {
		t.Fatalf("validation errors are not deterministic:\nrun1: %v\nrun2: %v", err1, err2)
	}
}

func TestValidateSimulationRequest_SnapshotHashOptional(t *testing.T) {
	// SnapshotHash is optional; request without it should still pass when all else is valid.
	req := validBaseRequest()
	req.SnapshotHash = ""
	if err := ValidateSimulationRequest(req); err != nil {
		t.Fatalf("expected no error when snapshotHash is absent, got: %v", err)
	}
}

func TestValidateSimulationRequest_SnapshotHashAccepted(t *testing.T) {
	req := validBaseRequest()
	req.SnapshotHash = "sha256:abc123def456"
	if err := ValidateSimulationRequest(req); err != nil {
		t.Fatalf("expected no error with snapshotHash present, got: %v", err)
	}
}

func TestIsValidationErrors(t *testing.T) {
	req := SimulationRequest{}
	err := ValidateSimulationRequest(req)
	ve, ok := IsValidationErrors(err)
	if !ok {
		t.Fatal("expected IsValidationErrors to return true for validation errors")
	}
	if len(ve) == 0 {
		t.Fatal("expected at least one validation error")
	}
}

func TestScenarioTypeConstants(t *testing.T) {
	// Verify the five locked scenario types are defined with expected values.
	scenarios := map[ScenarioType]string{
		ScenarioFailureShutdown:  "failure_shutdown",
		ScenarioScaling:          "scaling",
		ScenarioTrafficSpike:     "traffic_spike",
		ScenarioChattyColocation: "chatty_colocation",
		ScenarioNetworkCut:       "network_cut",
	}
	for k, v := range scenarios {
		if string(k) != v {
			t.Errorf("expected ScenarioType value %q, got %q", v, string(k))
		}
	}
}

// assertErrorCode checks that err contains a ValidationError with the given code.
func assertErrorCode(t *testing.T, err error, expectedCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %q, got nil", expectedCode)
	}
	ve, ok := IsValidationErrors(err)
	if !ok {
		t.Fatalf("expected ValidationErrors, got: %T: %v", err, err)
	}
	for _, e := range ve {
		if e.Code == expectedCode {
			return
		}
	}
	var codes []string
	for _, e := range ve {
		codes = append(codes, e.Code)
	}
	t.Fatalf("expected error code %q, got codes: %s", expectedCode, strings.Join(codes, ", "))
}
