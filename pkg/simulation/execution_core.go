package simulation

import (
	"encoding/json"
	"sort"
	"strings"
)

// ExecutionContext bundles all resolved inputs needed by a scenario model.
// All fields are read-only after construction; mutation must not occur during execution.
type ExecutionContext struct {
	// Request is the validated simulation request.
	Request SimulationRequest
	// Snapshot is the immutable cluster truth snapshot.
	Snapshot SimulationSnapshot
	// Evidence is the fully resolved evidence tier result.
	Evidence EvidenceResolverResult
}

// BuildExecutionContext constructs a fully resolved ExecutionContext from a validated
// SimulationRequest, an immutable SimulationSnapshot, and an InfluxCheckResult.
// The context is ready for deterministic scenario execution.
func BuildExecutionContext(req SimulationRequest, snap SimulationSnapshot, influx InfluxCheckResult) ExecutionContext {
	evidence := ResolveEvidenceTiersFromSnapshot(snap, influx)
	return ExecutionContext{
		Request:  req,
		Snapshot: snap,
		Evidence: evidence,
	}
}

// SortImpactedServices sorts services by ServiceID (primary) then Name (secondary)
// to ensure stable, deterministic ordering of impacted service lists.
func SortImpactedServices(services []ImpactedService) []ImpactedService {
	sort.Slice(services, func(i, j int) bool {
		a, b := services[i], services[j]
		if a.ServiceID != b.ServiceID {
			return a.ServiceID < b.ServiceID
		}
		return a.Name < b.Name
	})
	return services
}

// SortImpactedPaths sorts paths lexicographically by their path elements
// to ensure stable ordering in simulation responses.
func SortImpactedPaths(paths []ImpactedPath) []ImpactedPath {
	sort.Slice(paths, func(i, j int) bool {
		pi, pj := paths[i].Path, paths[j].Path
		for k := 0; k < len(pi) && k < len(pj); k++ {
			if pi[k] != pj[k] {
				return pi[k] < pj[k]
			}
		}
		return len(pi) < len(pj)
	})
	return paths
}

// SortBeforeAfterValues sorts BeforeAfterValues by FieldRef for stable output ordering.
func SortBeforeAfterValues(values []BeforeAfterValue) []BeforeAfterValue {
	sort.Slice(values, func(i, j int) bool {
		return values[i].FieldRef < values[j].FieldRef
	})
	return values
}

// SortAssumptions sorts SimulationAssumptions by Key for stable output ordering.
func SortAssumptions(assumptions []SimulationAssumption) []SimulationAssumption {
	sort.Slice(assumptions, func(i, j int) bool {
		return assumptions[i].Key < assumptions[j].Key
	})
	return assumptions
}

// NormalizeResponse applies stable sorting to all slice fields of a SimulationResponse
// so that the canonical JSON representation is byte-equivalent for equal logical content.
// NormalizeResponse must be called before CanonicalizeResponse.
// EvidenceSources is NOT sorted because its order encodes mandatory tier priority.
func NormalizeResponse(resp *SimulationResponse) {
	EnsureResponseTraceability(resp)
	SortImpactedServices(resp.ImpactedServices)
	SortImpactedPaths(resp.ImpactedPaths)
	SortBeforeAfterValues(resp.BeforeAfterValues)
	SortAssumptions(resp.Assumptions)
}

// EnsureResponseTraceability backfills traceability fields that are required by the
// canonical response contract but may be omitted by individual scenario builders.
// It applies deterministic defaults only when fields are empty.
func EnsureResponseTraceability(resp *SimulationResponse) {
	for i := range resp.BeforeAfterValues {
		if strings.TrimSpace(resp.BeforeAfterValues[i].TraceRef) == "" && strings.TrimSpace(resp.BeforeAfterValues[i].FieldRef) != "" {
			resp.BeforeAfterValues[i].TraceRef = "beforeAfterValues." + resp.BeforeAfterValues[i].FieldRef
		}
	}

	for i := range resp.Assumptions {
		assumption := &resp.Assumptions[i]
		if assumption.Type == "" {
			if assumption.Source == "engine_default" {
				assumption.Type = AssumptionTypeModelConstant
			} else {
				assumption.Type = AssumptionTypeEvidenceBinding
			}
		}
		if strings.TrimSpace(assumption.Value) == "" && strings.TrimSpace(assumption.Key) != "" {
			assumption.Value = assumption.Key
		}
		if strings.TrimSpace(assumption.TraceRef) == "" && strings.TrimSpace(assumption.Key) != "" {
			assumption.TraceRef = "assumptions." + assumption.Key
		}
	}

	if resp.ResultStatus == ResultStatusOK && resp.Recommendation.Action != "" && len(resp.Recommendation.EvidenceSourceRefs) == 0 {
		resp.Recommendation.EvidenceSourceRefs = append([]string(nil), resp.EvidenceSources...)
	}
}

// CanonicalizeResponse serialises a SimulationResponse to canonical JSON bytes.
// The response must be normalized via NormalizeResponse before calling this function.
// Two logically equivalent normalized responses produce byte-equal output.
func CanonicalizeResponse(resp SimulationResponse) ([]byte, error) {
	return json.Marshal(resp)
}

// EvidenceSourcesToStrings converts a slice of EvidenceSourceLabel to a string slice
// for population of SimulationResponse.EvidenceSources.
func EvidenceSourcesToStrings(labels []EvidenceSourceLabel) []string {
	out := make([]string, len(labels))
	for i, l := range labels {
		out[i] = string(l)
	}
	return out
}

// BuildBaseResponse constructs the common metadata fields of a SimulationResponse from
// an ExecutionContext. Scenario models are responsible for filling in ImpactedServices,
// ImpactedPaths, BeforeAfterValues, Assumptions, Recommendation, ResultStatus, and
// DeferredReason (when applicable).
func BuildBaseResponse(ctx ExecutionContext) SimulationResponse {
	return SimulationResponse{
		Version:            SchemaVersion,
		ScenarioType:       ctx.Request.ScenarioType,
		SnapshotTimestamp:  ctx.Snapshot.SnapshotTimestamp,
		SnapshotHash:       ctx.Snapshot.SnapshotHash,
		EvidenceSources:    EvidenceSourcesToStrings(ctx.Evidence.Sources),
		EvidenceMode:       ctx.Evidence.Mode,
		DegradedMode:       ctx.Evidence.DegradedMode,
		DegradedModeReason: ctx.Evidence.DegradedReason,
		ConfidenceLevel:    ctx.Evidence.Confidence,
	}
}
