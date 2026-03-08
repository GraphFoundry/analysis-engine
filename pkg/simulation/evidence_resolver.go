package simulation

// InfluxCheckResult captures the outcome of probing the InfluxDB historical data tier.
// The resolver uses this to determine effective InfluxDB availability without blocking.
type InfluxCheckResult struct {
	// Reachable indicates InfluxDB was contactable (network/auth succeeded).
	Reachable bool
	// DataSufficient indicates returned data is non-empty and adequate for analysis.
	DataSufficient bool
	// Sparse is true when data was returned but the volume or time-range is too thin
	// for full confidence analysis.
	Sparse bool
	// Err is non-nil when the InfluxDB probe encountered a hard error.
	// A non-nil Err records DegradedModeInfluxError; simulation continues without Influx.
	Err error
}

// EvidenceResolverInput bundles all tier availability signals into the resolver.
// Callers populate this from live probes immediately before running a simulation.
type EvidenceResolverInput struct {
	// HasLiveServiceGraph is true when the live service graph tier returned data.
	HasLiveServiceGraph bool
	// HasLiveK8sRuntime is true when the live Kubernetes/runtime tier returned data.
	HasLiveK8sRuntime bool
	// InfluxResult is the probe outcome for the InfluxDB historical tier.
	// The resolver degrades gracefully and never blocks if InfluxResult signals unavailability.
	InfluxResult InfluxCheckResult
}

// EvidenceResolverResult is the fully resolved outcome of the tiered evidence resolution pass.
// All fields are populated deterministically from EvidenceResolverInput; no randomness is used.
type EvidenceResolverResult struct {
	// Availability records which tiers were effectively usable for this resolution pass.
	Availability EvidenceTierAvailability
	// Mode is the canonical evidence mode resolved from the tier availability.
	Mode EvidenceMode
	// Sources is the ordered list of active evidence source labels (tier priority order).
	Sources []EvidenceSourceLabel
	// Confidence is the deterministic confidence level derived from Mode.
	Confidence ConfidenceLevel
	// DegradedMode is non-empty when the simulation is running in a degraded or fallback state.
	// It is DegradedModeNone when all tiers including InfluxDB are available and sufficient.
	DegradedMode DegradedMode
	// DegradedReason is a human-readable explanation of why degraded mode is active.
	// Empty when DegradedMode is DegradedModeNone.
	DegradedReason string
}

// influxEffective returns true when the InfluxDB check result represents a tier that is
// fully usable: reachable, no error, non-sparse, and data sufficient.
func influxEffective(r InfluxCheckResult) bool {
	return r.Reachable && r.DataSufficient && !r.Sparse && r.Err == nil
}

// classifyInfluxDegradation maps an unusable InfluxCheckResult to a DegradedMode constant
// and a human-readable reason string. It is only called when influxEffective returns false.
func classifyInfluxDegradation(r InfluxCheckResult) (DegradedMode, string) {
	if r.Err != nil {
		return DegradedModeInfluxError, "InfluxDB query failed: " + r.Err.Error()
	}
	if r.Reachable && r.Sparse {
		return DegradedModeInfluxSparse, "InfluxDB data is present but insufficient for full confidence analysis"
	}
	if r.Reachable && !r.DataSufficient {
		return DegradedModeInfluxEmpty, "InfluxDB returned no usable historical data points"
	}
	// Not reachable at all (network/auth failure with no wrapped error).
	return DegradedModeInfluxEmpty, "InfluxDB historical data tier is unreachable"
}

// ResolveEvidenceTiers runs the mandatory tier-ordering algorithm and returns a fully populated
// EvidenceResolverResult. The function never returns an error; when InfluxDB is unavailable
// or sparse it degrades gracefully and records why, so simulation can always proceed.
//
// Tier resolution order (mandatory): live graph -> live runtime -> Influx history -> fallback.
func ResolveEvidenceTiers(input EvidenceResolverInput) EvidenceResolverResult {
	influxOK := influxEffective(input.InfluxResult)

	avail := EvidenceTierAvailability{
		LiveServiceGraph:   input.HasLiveServiceGraph,
		LiveK8sRuntime:     input.HasLiveK8sRuntime,
		HistoricalInfluxDB: influxOK,
	}

	mode := ResolveEvidenceMode(avail)
	sources := ResolveEvidenceSources(avail)
	confidence := DetermineConfidenceLevel(mode)

	var degradedMode DegradedMode
	var degradedReason string

	if !influxOK {
		degradedMode, degradedReason = classifyInfluxDegradation(input.InfluxResult)
	}

	return EvidenceResolverResult{
		Availability:   avail,
		Mode:           mode,
		Sources:        sources,
		Confidence:     confidence,
		DegradedMode:   degradedMode,
		DegradedReason: degradedReason,
	}
}

// ResolveEvidenceTiersFromSnapshot derives a best-effort EvidenceResolverInput from an
// existing SimulationSnapshot and an InfluxCheckResult. Live tier availability is inferred
// from snapshot content: non-empty ServiceNodes implies live graph data was captured, and
// non-empty RuntimeServices implies live Kubernetes runtime data was captured.
//
// Use this helper when the snapshot is already composed and no separate live probes are needed.
func ResolveEvidenceTiersFromSnapshot(snap SimulationSnapshot, influx InfluxCheckResult) EvidenceResolverResult {
	return ResolveEvidenceTiers(EvidenceResolverInput{
		HasLiveServiceGraph: len(snap.ServiceNodes) > 0,
		HasLiveK8sRuntime:   len(snap.RuntimeServices) > 0,
		InfluxResult:        influx,
	})
}
