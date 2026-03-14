package simulation

import (
	"errors"
	"testing"
)

// allTiersAvailable returns an EvidenceResolverInput where all tiers report full availability.
func allTiersAvailable() EvidenceResolverInput {
	return EvidenceResolverInput{
		HasLiveServiceGraph: true,
		HasLiveK8sRuntime:   true,
		InfluxResult: InfluxCheckResult{
			Reachable:      true,
			DataSufficient: true,
			Sparse:         false,
			Err:            nil,
		},
	}
}

// --- Tier ordering: full availability ---

func TestResolveEvidenceTiers_AllAvailable_FullMode(t *testing.T) {
	result := ResolveEvidenceTiers(allTiersAvailable())
	if result.Mode != EvidenceModeFull {
		t.Errorf("expected EvidenceModeFull, got %q", result.Mode)
	}
}

func TestResolveEvidenceTiers_AllAvailable_HighConfidence(t *testing.T) {
	result := ResolveEvidenceTiers(allTiersAvailable())
	if result.Confidence != ConfidenceHigh {
		t.Errorf("expected ConfidenceHigh, got %q", result.Confidence)
	}
}

func TestResolveEvidenceTiers_AllAvailable_NoDegradedMode(t *testing.T) {
	result := ResolveEvidenceTiers(allTiersAvailable())
	if result.DegradedMode != DegradedModeNone {
		t.Errorf("expected DegradedModeNone, got %q", result.DegradedMode)
	}
	if result.DegradedReason != "" {
		t.Errorf("expected empty DegradedReason, got %q", result.DegradedReason)
	}
}

func TestResolveEvidenceTiers_AllAvailable_SourcesIncludeAllThreePlusFallback(t *testing.T) {
	result := ResolveEvidenceTiers(allTiersAvailable())
	want := []EvidenceSourceLabel{
		EvidenceSourceLiveServiceGraph,
		EvidenceSourceLiveK8sRuntime,
		EvidenceSourceHistoricalInfluxDB,
		EvidenceSourceDeterministicFallback,
	}
	if len(result.Sources) != len(want) {
		t.Fatalf("expected %d sources, got %d: %v", len(want), len(result.Sources), result.Sources)
	}
	for i, s := range result.Sources {
		if s != want[i] {
			t.Errorf("sources[%d]: expected %q, got %q", i, want[i], s)
		}
	}
}

// --- InfluxDB absent: partial mode ---

func TestResolveEvidenceTiers_NoInflux_PartialMode(t *testing.T) {
	input := EvidenceResolverInput{
		HasLiveServiceGraph: true,
		HasLiveK8sRuntime:   true,
		InfluxResult:        InfluxCheckResult{Reachable: false},
	}
	result := ResolveEvidenceTiers(input)
	if result.Mode != EvidenceModePartial {
		t.Errorf("expected EvidenceModePartial, got %q", result.Mode)
	}
}

func TestResolveEvidenceTiers_NoInflux_MediumConfidence(t *testing.T) {
	input := EvidenceResolverInput{
		HasLiveServiceGraph: true,
		HasLiveK8sRuntime:   true,
		InfluxResult:        InfluxCheckResult{Reachable: false},
	}
	result := ResolveEvidenceTiers(input)
	if result.Confidence != ConfidenceMedium {
		t.Errorf("expected ConfidenceMedium, got %q", result.Confidence)
	}
}

func TestResolveEvidenceTiers_NoInflux_DegradedModeEmpty(t *testing.T) {
	input := EvidenceResolverInput{
		HasLiveServiceGraph: true,
		HasLiveK8sRuntime:   true,
		InfluxResult:        InfluxCheckResult{Reachable: false},
	}
	result := ResolveEvidenceTiers(input)
	if result.DegradedMode != DegradedModeInfluxEmpty {
		t.Errorf("expected DegradedModeInfluxEmpty, got %q", result.DegradedMode)
	}
	if result.DegradedReason == "" {
		t.Error("expected non-empty DegradedReason when InfluxDB unreachable")
	}
}

func TestResolveEvidenceTiers_SimulationNeverBlocksOnInflux(t *testing.T) {
	// Core acceptance criterion: resolver must return a usable result even when Influx is down.
	input := EvidenceResolverInput{
		HasLiveServiceGraph: true,
		HasLiveK8sRuntime:   true,
		InfluxResult: InfluxCheckResult{
			Reachable: false,
			Err:       errors.New("connection refused"),
		},
	}
	result := ResolveEvidenceTiers(input)
	// Must return a result; Mode must not be empty; simulation can proceed.
	if result.Mode == "" {
		t.Error("resolver returned empty Mode; simulation would be blocked")
	}
	// Must not claim InfluxDB was available.
	if result.Availability.HistoricalInfluxDB {
		t.Error("HistoricalInfluxDB availability must be false when probe failed")
	}
}

// --- InfluxDB sparse ---

func TestResolveEvidenceTiers_InfluxSparse_DegradedModeSparse(t *testing.T) {
	input := EvidenceResolverInput{
		HasLiveServiceGraph: true,
		HasLiveK8sRuntime:   true,
		InfluxResult: InfluxCheckResult{
			Reachable:      true,
			DataSufficient: false,
			Sparse:         true,
		},
	}
	result := ResolveEvidenceTiers(input)
	if result.DegradedMode != DegradedModeInfluxSparse {
		t.Errorf("expected DegradedModeInfluxSparse, got %q", result.DegradedMode)
	}
}

func TestResolveEvidenceTiers_InfluxSparse_DoesNotBlockSimulation(t *testing.T) {
	input := EvidenceResolverInput{
		HasLiveServiceGraph: true,
		HasLiveK8sRuntime:   true,
		InfluxResult: InfluxCheckResult{
			Reachable:      true,
			DataSufficient: false,
			Sparse:         true,
		},
	}
	result := ResolveEvidenceTiers(input)
	if result.Mode == "" {
		t.Error("resolver returned empty Mode when Influx is sparse")
	}
	if !result.Availability.LiveServiceGraph || !result.Availability.LiveK8sRuntime {
		t.Error("live tiers must remain available when Influx is only sparse")
	}
}

// --- InfluxDB error ---

func TestResolveEvidenceTiers_InfluxError_DegradedModeError(t *testing.T) {
	input := EvidenceResolverInput{
		HasLiveServiceGraph: true,
		HasLiveK8sRuntime:   true,
		InfluxResult: InfluxCheckResult{
			Reachable: true,
			Err:       errors.New("timeout"),
		},
	}
	result := ResolveEvidenceTiers(input)
	if result.DegradedMode != DegradedModeInfluxError {
		t.Errorf("expected DegradedModeInfluxError, got %q", result.DegradedMode)
	}
	if result.DegradedReason == "" {
		t.Error("expected non-empty DegradedReason for InfluxDB error")
	}
}

// --- No live tiers at all: fallback mode ---

func TestResolveEvidenceTiers_NoLiveTiers_FallbackMode(t *testing.T) {
	input := EvidenceResolverInput{
		HasLiveServiceGraph: false,
		HasLiveK8sRuntime:   false,
		InfluxResult:        InfluxCheckResult{Reachable: false},
	}
	result := ResolveEvidenceTiers(input)
	if result.Mode != EvidenceModeFallback {
		t.Errorf("expected EvidenceModeFallback, got %q", result.Mode)
	}
}

func TestResolveEvidenceTiers_NoLiveTiers_LowConfidence(t *testing.T) {
	input := EvidenceResolverInput{
		HasLiveServiceGraph: false,
		HasLiveK8sRuntime:   false,
		InfluxResult:        InfluxCheckResult{Reachable: false},
	}
	result := ResolveEvidenceTiers(input)
	if result.Confidence != ConfidenceLow {
		t.Errorf("expected ConfidenceLow, got %q", result.Confidence)
	}
}

func TestResolveEvidenceTiers_NoLiveTiers_SourcesContainFallback(t *testing.T) {
	input := EvidenceResolverInput{
		HasLiveServiceGraph: false,
		HasLiveK8sRuntime:   false,
		InfluxResult:        InfluxCheckResult{Reachable: false},
	}
	result := ResolveEvidenceTiers(input)
	hasFallback := false
	for _, s := range result.Sources {
		if s == EvidenceSourceDeterministicFallback {
			hasFallback = true
		}
	}
	if !hasFallback {
		t.Error("deterministic_fallback source must always be present")
	}
}

// --- Only one live tier: degraded mode ---

func TestResolveEvidenceTiers_OnlyServiceGraph_DegradedMode(t *testing.T) {
	input := EvidenceResolverInput{
		HasLiveServiceGraph: true,
		HasLiveK8sRuntime:   false,
		InfluxResult:        InfluxCheckResult{Reachable: false},
	}
	result := ResolveEvidenceTiers(input)
	if result.Mode != EvidenceModeDegraded {
		t.Errorf("expected EvidenceModeDegraded, got %q", result.Mode)
	}
}

// --- Determinism ---

func TestResolveEvidenceTiers_Deterministic_SameInputSameOutput(t *testing.T) {
	input := allTiersAvailable()
	r1 := ResolveEvidenceTiers(input)
	r2 := ResolveEvidenceTiers(input)

	if r1.Mode != r2.Mode {
		t.Errorf("mode differs: %q vs %q", r1.Mode, r2.Mode)
	}
	if r1.Confidence != r2.Confidence {
		t.Errorf("confidence differs: %q vs %q", r1.Confidence, r2.Confidence)
	}
	if r1.DegradedMode != r2.DegradedMode {
		t.Errorf("degraded mode differs: %q vs %q", r1.DegradedMode, r2.DegradedMode)
	}
	if len(r1.Sources) != len(r2.Sources) {
		t.Errorf("sources length differs: %d vs %d", len(r1.Sources), len(r2.Sources))
	}
}

// --- ResolveEvidenceTiersFromSnapshot ---

func TestResolveEvidenceTiersFromSnapshot_NonEmptySnapshot_GraphAndRuntimeAvailable(t *testing.T) {
	snap := SimulationSnapshot{
		ServiceNodes:    []SnapshotServiceNode{{ServiceID: "svc-a", Name: "a", Namespace: "default"}},
		RuntimeServices: []SnapshotRuntimeService{{ServiceID: "svc-a", PodCount: 2}},
	}
	influx := InfluxCheckResult{Reachable: true, DataSufficient: true}
	result := ResolveEvidenceTiersFromSnapshot(snap, influx)

	if !result.Availability.LiveServiceGraph {
		t.Error("expected LiveServiceGraph available for non-empty ServiceNodes")
	}
	if !result.Availability.LiveK8sRuntime {
		t.Error("expected LiveK8sRuntime available for non-empty RuntimeServices")
	}
}

func TestResolveEvidenceTiersFromSnapshot_EmptySnapshot_LiveTiersUnavailable(t *testing.T) {
	snap := SimulationSnapshot{}
	influx := InfluxCheckResult{Reachable: false}
	result := ResolveEvidenceTiersFromSnapshot(snap, influx)

	if result.Availability.LiveServiceGraph {
		t.Error("expected LiveServiceGraph unavailable for empty snapshot")
	}
	if result.Availability.LiveK8sRuntime {
		t.Error("expected LiveK8sRuntime unavailable for empty snapshot")
	}
	if result.Mode != EvidenceModeFallback {
		t.Errorf("expected EvidenceModeFallback for fully empty snapshot, got %q", result.Mode)
	}
}

func TestResolveEvidenceTiersFromSnapshot_WithInfluxSparse_ReturnsDegradedLabel(t *testing.T) {
	snap := SimulationSnapshot{
		ServiceNodes:    []SnapshotServiceNode{{ServiceID: "x", Name: "x", Namespace: "ns"}},
		RuntimeServices: []SnapshotRuntimeService{{ServiceID: "x", PodCount: 1}},
	}
	influx := InfluxCheckResult{Reachable: true, DataSufficient: false, Sparse: true}
	result := ResolveEvidenceTiersFromSnapshot(snap, influx)

	if result.DegradedMode != DegradedModeInfluxSparse {
		t.Errorf("expected DegradedModeInfluxSparse, got %q", result.DegradedMode)
	}
}

// --- Availability struct is correctly populated ---

func TestResolveEvidenceTiers_AvailabilityMatchesInput(t *testing.T) {
	input := EvidenceResolverInput{
		HasLiveServiceGraph: true,
		HasLiveK8sRuntime:   false,
		InfluxResult:        InfluxCheckResult{Reachable: true, DataSufficient: true},
	}
	result := ResolveEvidenceTiers(input)

	if !result.Availability.LiveServiceGraph {
		t.Error("expected LiveServiceGraph true")
	}
	if result.Availability.LiveK8sRuntime {
		t.Error("expected LiveK8sRuntime false")
	}
	if !result.Availability.HistoricalInfluxDB {
		t.Error("expected HistoricalInfluxDB true when reachable and sufficient")
	}
}

func TestResolveEvidenceTiers_InfluxReachableButNotSufficient_NotCountedAsAvailable(t *testing.T) {
	input := EvidenceResolverInput{
		HasLiveServiceGraph: true,
		HasLiveK8sRuntime:   true,
		InfluxResult: InfluxCheckResult{
			Reachable:      true,
			DataSufficient: false,
			Sparse:         false,
		},
	}
	result := ResolveEvidenceTiers(input)
	if result.Availability.HistoricalInfluxDB {
		t.Error("HistoricalInfluxDB must be false when DataSufficient is false")
	}
}
