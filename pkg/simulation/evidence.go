package simulation

// EvidenceSourceLabel identifies a specific evidence tier used during simulation.
type EvidenceSourceLabel string

const (
	// EvidenceSourceLiveServiceGraph is the live service graph (primary, highest-fidelity source).
	EvidenceSourceLiveServiceGraph EvidenceSourceLabel = "live_service_graph"

	// EvidenceSourceLiveK8sRuntime is live Kubernetes/runtime metadata (pods, replicas, resources).
	EvidenceSourceLiveK8sRuntime EvidenceSourceLabel = "live_k8s_runtime"

	// EvidenceSourceHistoricalInfluxDB is historical time-series data from InfluxDB.
	EvidenceSourceHistoricalInfluxDB EvidenceSourceLabel = "historical_influxdb"

	// EvidenceSourceDeterministicFallback is the deterministic rule-based fallback used when
	// live or historical sources are unavailable or insufficient.
	EvidenceSourceDeterministicFallback EvidenceSourceLabel = "deterministic_fallback"
)

// validEvidenceSourceLabels is the authoritative set of defined source labels.
var validEvidenceSourceLabels = map[EvidenceSourceLabel]struct{}{
	EvidenceSourceLiveServiceGraph:      {},
	EvidenceSourceLiveK8sRuntime:        {},
	EvidenceSourceHistoricalInfluxDB:    {},
	EvidenceSourceDeterministicFallback: {},
}

// IsValidEvidenceSourceLabel returns true if the label is one of the four defined source labels.
func IsValidEvidenceSourceLabel(l EvidenceSourceLabel) bool {
	_, ok := validEvidenceSourceLabels[l]
	return ok
}

// EvidenceTierAvailability captures which evidence tiers are available for a given simulation run.
// Tiers are resolved in mandatory order: live graph -> live runtime -> Influx history -> fallback.
type EvidenceTierAvailability struct {
	// LiveServiceGraph indicates the live service graph tier is reachable and returned data.
	LiveServiceGraph bool
	// LiveK8sRuntime indicates the live Kubernetes/runtime tier is reachable and returned data.
	LiveK8sRuntime bool
	// HistoricalInfluxDB indicates InfluxDB is reachable and returned sufficient historical data.
	HistoricalInfluxDB bool
}

// ResolveEvidenceMode maps available evidence tiers to the canonical EvidenceMode following the
// mandatory tier ordering: live graph -> live runtime -> Influx history -> deterministic fallback.
//
// Tier ordering rules (applied in strict priority order):
//   - FULL:     live graph AND live runtime AND Influx history are all available.
//   - PARTIAL:  live graph AND live runtime are available, but Influx history is not.
//   - DEGRADED: live graph OR live runtime is available, but Influx history is not.
//   - FALLBACK: none of the live/historical tiers are available; deterministic fallback only.
func ResolveEvidenceMode(avail EvidenceTierAvailability) EvidenceMode {
	switch {
	case avail.LiveServiceGraph && avail.LiveK8sRuntime && avail.HistoricalInfluxDB:
		return EvidenceModeFull
	case avail.LiveServiceGraph && avail.LiveK8sRuntime && !avail.HistoricalInfluxDB:
		return EvidenceModePartial
	case (avail.LiveServiceGraph || avail.LiveK8sRuntime) && !avail.HistoricalInfluxDB:
		return EvidenceModeDegraded
	default:
		return EvidenceModeFallback
	}
}

// ResolveEvidenceSources returns the ordered list of evidence source labels that correspond to
// the available tiers. The order follows the mandatory tier priority.
func ResolveEvidenceSources(avail EvidenceTierAvailability) []EvidenceSourceLabel {
	sources := make([]EvidenceSourceLabel, 0, 4)
	if avail.LiveServiceGraph {
		sources = append(sources, EvidenceSourceLiveServiceGraph)
	}
	if avail.LiveK8sRuntime {
		sources = append(sources, EvidenceSourceLiveK8sRuntime)
	}
	if avail.HistoricalInfluxDB {
		sources = append(sources, EvidenceSourceHistoricalInfluxDB)
	}
	// Deterministic fallback is always included as the final safety tier.
	sources = append(sources, EvidenceSourceDeterministicFallback)
	return sources
}

// DetermineConfidenceLevel returns the deterministic confidence level for a given evidence mode.
//
// Confidence rubric (no random weighting; derived solely from evidence mode):
//   - HIGH:   EvidenceModeFull   — all three live+historical tiers available.
//   - MEDIUM: EvidenceModePartial — live tiers available, Influx history absent.
//   - LOW:    EvidenceModeDegraded or EvidenceModeFallback — limited or no live data.
func DetermineConfidenceLevel(mode EvidenceMode) ConfidenceLevel {
	switch mode {
	case EvidenceModeFull:
		return ConfidenceHigh
	case EvidenceModePartial:
		return ConfidenceMedium
	default:
		// EvidenceModeDegraded and EvidenceModeFallback both yield LOW confidence.
		return ConfidenceLow
	}
}

// EvidenceModeToTierDescription returns a human-readable description of what the evidence mode
// means in terms of which tiers were active. Intended for degraded-mode reason strings.
func EvidenceModeToTierDescription(mode EvidenceMode) string {
	switch mode {
	case EvidenceModeFull:
		return "live service graph, live Kubernetes runtime, and historical InfluxDB data all available"
	case EvidenceModePartial:
		return "live service graph and live Kubernetes runtime available; InfluxDB history absent or sparse"
	case EvidenceModeDegraded:
		return "partial live data available; InfluxDB history absent or sparse; deterministic fallback applied"
	case EvidenceModeFallback:
		return "no live or historical data available; deterministic fallback only"
	default:
		return "unknown evidence mode"
	}
}
