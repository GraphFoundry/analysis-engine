package simulation

import (
	"testing"
)

// --- IsValidEvidenceSourceLabel ---

func TestIsValidEvidenceSourceLabel_AllDefined(t *testing.T) {
	labels := []EvidenceSourceLabel{
		EvidenceSourceLiveServiceGraph,
		EvidenceSourceLiveK8sRuntime,
		EvidenceSourceHistoricalInfluxDB,
		EvidenceSourceDeterministicFallback,
	}
	for _, l := range labels {
		if !IsValidEvidenceSourceLabel(l) {
			t.Errorf("expected %q to be valid", l)
		}
	}
}

func TestIsValidEvidenceSourceLabel_Unknown(t *testing.T) {
	if IsValidEvidenceSourceLabel("unknown_source") {
		t.Error("expected unknown label to be invalid")
	}
}

func TestIsValidEvidenceSourceLabel_Empty(t *testing.T) {
	if IsValidEvidenceSourceLabel("") {
		t.Error("expected empty label to be invalid")
	}
}

// --- ResolveEvidenceMode ---

func TestResolveEvidenceMode_Full(t *testing.T) {
	avail := EvidenceTierAvailability{
		LiveServiceGraph:   true,
		LiveK8sRuntime:     true,
		HistoricalInfluxDB: true,
	}
	if got := ResolveEvidenceMode(avail); got != EvidenceModeFull {
		t.Errorf("expected FULL, got %q", got)
	}
}

func TestResolveEvidenceMode_Partial_NoInflux(t *testing.T) {
	avail := EvidenceTierAvailability{
		LiveServiceGraph:   true,
		LiveK8sRuntime:     true,
		HistoricalInfluxDB: false,
	}
	if got := ResolveEvidenceMode(avail); got != EvidenceModePartial {
		t.Errorf("expected PARTIAL, got %q", got)
	}
}

func TestResolveEvidenceMode_Degraded_OnlyLiveGraph(t *testing.T) {
	avail := EvidenceTierAvailability{
		LiveServiceGraph:   true,
		LiveK8sRuntime:     false,
		HistoricalInfluxDB: false,
	}
	if got := ResolveEvidenceMode(avail); got != EvidenceModeDegraded {
		t.Errorf("expected DEGRADED, got %q", got)
	}
}

func TestResolveEvidenceMode_Degraded_OnlyK8sRuntime(t *testing.T) {
	avail := EvidenceTierAvailability{
		LiveServiceGraph:   false,
		LiveK8sRuntime:     true,
		HistoricalInfluxDB: false,
	}
	if got := ResolveEvidenceMode(avail); got != EvidenceModeDegraded {
		t.Errorf("expected DEGRADED, got %q", got)
	}
}

func TestResolveEvidenceMode_Fallback_NoneAvailable(t *testing.T) {
	avail := EvidenceTierAvailability{
		LiveServiceGraph:   false,
		LiveK8sRuntime:     false,
		HistoricalInfluxDB: false,
	}
	if got := ResolveEvidenceMode(avail); got != EvidenceModeFallback {
		t.Errorf("expected FALLBACK, got %q", got)
	}
}

// InfluxDB alone (without live tiers) is not a valid "partial" — it must still be FALLBACK
// because the tier order requires live graph before runtime before Influx.
func TestResolveEvidenceMode_InfluxOnlyIsNotPartial(t *testing.T) {
	avail := EvidenceTierAvailability{
		LiveServiceGraph:   false,
		LiveK8sRuntime:     false,
		HistoricalInfluxDB: true,
	}
	// Without live tiers, result should not be FULL or PARTIAL.
	got := ResolveEvidenceMode(avail)
	if got == EvidenceModeFull || got == EvidenceModePartial {
		t.Errorf("Influx-only should not produce FULL or PARTIAL, got %q", got)
	}
}

// --- ResolveEvidenceSources ---

func TestResolveEvidenceSources_AllTiers(t *testing.T) {
	avail := EvidenceTierAvailability{
		LiveServiceGraph:   true,
		LiveK8sRuntime:     true,
		HistoricalInfluxDB: true,
	}
	sources := ResolveEvidenceSources(avail)
	// Must include all four labels (live graph, live runtime, influx, deterministic fallback).
	if len(sources) != 4 {
		t.Fatalf("expected 4 sources, got %d: %v", len(sources), sources)
	}
	assertContainsSource(t, sources, EvidenceSourceLiveServiceGraph)
	assertContainsSource(t, sources, EvidenceSourceLiveK8sRuntime)
	assertContainsSource(t, sources, EvidenceSourceHistoricalInfluxDB)
	assertContainsSource(t, sources, EvidenceSourceDeterministicFallback)
}

func TestResolveEvidenceSources_NoLiveTiers_FallbackAlwaysPresent(t *testing.T) {
	avail := EvidenceTierAvailability{}
	sources := ResolveEvidenceSources(avail)
	assertContainsSource(t, sources, EvidenceSourceDeterministicFallback)
	if len(sources) != 1 {
		t.Errorf("expected only fallback source, got %v", sources)
	}
}

func TestResolveEvidenceSources_PartialTiers_OrderPreserved(t *testing.T) {
	avail := EvidenceTierAvailability{
		LiveServiceGraph:   true,
		LiveK8sRuntime:     true,
		HistoricalInfluxDB: false,
	}
	sources := ResolveEvidenceSources(avail)
	// Must start with live graph, then runtime, then fallback.
	if sources[0] != EvidenceSourceLiveServiceGraph {
		t.Errorf("first source must be live_service_graph, got %q", sources[0])
	}
	if sources[1] != EvidenceSourceLiveK8sRuntime {
		t.Errorf("second source must be live_k8s_runtime, got %q", sources[1])
	}
	// Fallback must be last.
	last := sources[len(sources)-1]
	if last != EvidenceSourceDeterministicFallback {
		t.Errorf("last source must be deterministic_fallback, got %q", last)
	}
}

// --- DetermineConfidenceLevel ---

func TestDetermineConfidenceLevel_Full_IsHigh(t *testing.T) {
	if got := DetermineConfidenceLevel(EvidenceModeFull); got != ConfidenceHigh {
		t.Errorf("expected HIGH for FULL, got %q", got)
	}
}

func TestDetermineConfidenceLevel_Partial_IsMedium(t *testing.T) {
	if got := DetermineConfidenceLevel(EvidenceModePartial); got != ConfidenceMedium {
		t.Errorf("expected MEDIUM for PARTIAL, got %q", got)
	}
}

func TestDetermineConfidenceLevel_Degraded_IsLow(t *testing.T) {
	if got := DetermineConfidenceLevel(EvidenceModeDegraded); got != ConfidenceLow {
		t.Errorf("expected LOW for DEGRADED, got %q", got)
	}
}

func TestDetermineConfidenceLevel_Fallback_IsLow(t *testing.T) {
	if got := DetermineConfidenceLevel(EvidenceModeFallback); got != ConfidenceLow {
		t.Errorf("expected LOW for FALLBACK, got %q", got)
	}
}

// DetermineConfidenceLevel must be deterministic: same mode always yields same level.
func TestDetermineConfidenceLevel_Deterministic(t *testing.T) {
	modes := []EvidenceMode{EvidenceModeFull, EvidenceModePartial, EvidenceModeDegraded, EvidenceModeFallback}
	for _, mode := range modes {
		first := DetermineConfidenceLevel(mode)
		for i := 0; i < 10; i++ {
			if got := DetermineConfidenceLevel(mode); got != first {
				t.Errorf("non-deterministic result for mode %q: first=%q, got=%q", mode, first, got)
			}
		}
	}
}

// ResolveEvidenceMode must be deterministic: same availability always yields same mode.
func TestResolveEvidenceMode_Deterministic(t *testing.T) {
	cases := []EvidenceTierAvailability{
		{true, true, true},
		{true, true, false},
		{true, false, false},
		{false, true, false},
		{false, false, false},
	}
	for _, avail := range cases {
		first := ResolveEvidenceMode(avail)
		for i := 0; i < 10; i++ {
			if got := ResolveEvidenceMode(avail); got != first {
				t.Errorf("non-deterministic result for avail %+v: first=%q, got=%q", avail, first, got)
			}
		}
	}
}

// --- EvidenceModeToTierDescription ---

func TestEvidenceModeToTierDescription_AllModesReturnNonEmpty(t *testing.T) {
	modes := []EvidenceMode{EvidenceModeFull, EvidenceModePartial, EvidenceModeDegraded, EvidenceModeFallback}
	for _, mode := range modes {
		desc := EvidenceModeToTierDescription(mode)
		if desc == "" {
			t.Errorf("expected non-empty description for mode %q", mode)
		}
	}
}

// --- helpers ---

func assertContainsSource(t *testing.T, sources []EvidenceSourceLabel, want EvidenceSourceLabel) {
	t.Helper()
	for _, s := range sources {
		if s == want {
			return
		}
	}
	t.Errorf("expected source %q in %v", want, sources)
}
