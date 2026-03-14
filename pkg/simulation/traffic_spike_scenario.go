package simulation

import (
	"fmt"
	"math"
	"strings"
)

// RunTrafficSpikeScenario executes the Traffic Spike / targeted load scenario model.
//
// It uses the immutable SimulationSnapshot inside the ExecutionContext to project the
// impact of a sudden load increase on the target service. Before/after values are computed
// from deterministic formulas applied to snapshot edge data; no random values or wall-clock
// inputs are used.
//
// The function returns ResultStatusDeferred when the target service is not present in
// the snapshot graph, preventing guessed numeric values from leaking into the response.
func RunTrafficSpikeScenario(ctx ExecutionContext) SimulationResponse {
	resp := BuildBaseResponse(ctx)
	params := ctx.Request.TrafficSpikeParams

	targetID := strings.TrimSpace(params.TargetServiceID)

	// Locate target in snapshot. Absence means no graph truth to reason from.
	targetNode := findSnapshotNode(ctx.Snapshot, targetID)
	if targetNode == nil {
		resp.ResultStatus = ResultStatusDeferred
		resp.DeferredReason = fmt.Sprintf(
			"target service %q not found in snapshot graph; traffic spike impact cannot be computed without graph truth",
			targetID,
		)
		resp.Assumptions = []SimulationAssumption{}
		resp.ImpactedServices = []ImpactedService{}
		resp.ImpactedPaths = []ImpactedPath{}
		resp.BeforeAfterValues = []BeforeAfterValue{}
		NormalizeResponse(&resp)
		return resp
	}

	incomingEdges := filterEdgesByTarget(ctx.Snapshot.ServiceEdges, targetID)
	outgoingEdges := filterEdgesBySource(ctx.Snapshot.ServiceEdges, targetID)

	impacted := buildSpikeImpactedServices(ctx.Snapshot, targetID, *targetNode, incomingEdges, outgoingEdges)
	paths := buildSpikeImpactedPaths(targetID, incomingEdges, outgoingEdges)
	bav, assumptions := buildSpikeBeforeAfterValues(params, incomingEdges, ctx.Evidence)
	rec := buildSpikeRecommendation(ctx, targetID, params, incomingEdges)

	resp.ResultStatus = ResultStatusOK
	resp.ImpactedServices = impacted
	resp.ImpactedPaths = paths
	resp.BeforeAfterValues = bav
	resp.Assumptions = assumptions
	resp.Recommendation = rec

	NormalizeResponse(&resp)
	return resp
}

// --- impacted services ---

// buildSpikeImpactedServices returns the target, its direct callers, and its direct
// downstream services drawn from snapshot edge relationships.
// Role values: "target", "caller", "downstream".
func buildSpikeImpactedServices(
	snap SimulationSnapshot,
	targetID string,
	targetNode SnapshotServiceNode,
	incomingEdges []SnapshotServiceEdge,
	outgoingEdges []SnapshotServiceEdge,
) []ImpactedService {
	services := []ImpactedService{
		{
			ServiceID: targetID,
			Name:      targetNode.Name,
			Namespace: targetNode.Namespace,
			Role:      "target",
		},
	}

	seen := map[string]bool{targetID: true}

	for _, e := range incomingEdges {
		id := e.SourceServiceID
		if seen[id] {
			continue
		}
		seen[id] = true
		name, ns := resolveNodeMeta(snap, id)
		services = append(services, ImpactedService{
			ServiceID: id,
			Name:      name,
			Namespace: ns,
			Role:      "caller",
		})
	}

	for _, e := range outgoingEdges {
		id := e.TargetServiceID
		if seen[id] {
			continue
		}
		seen[id] = true
		name, ns := resolveNodeMeta(snap, id)
		services = append(services, ImpactedService{
			ServiceID: id,
			Name:      name,
			Namespace: ns,
			Role:      "downstream",
		})
	}

	return services
}

// --- impacted paths ---

// buildSpikeImpactedPaths returns the communication paths affected by the load spike.
// Both caller→target and target→downstream paths are included because increased load
// propagates pressure in both directions through the call chain.
func buildSpikeImpactedPaths(
	targetID string,
	incomingEdges []SnapshotServiceEdge,
	outgoingEdges []SnapshotServiceEdge,
) []ImpactedPath {
	var paths []ImpactedPath

	for _, e := range incomingEdges {
		paths = append(paths, ImpactedPath{Path: []string{e.SourceServiceID, targetID}})
	}

	for _, e := range outgoingEdges {
		paths = append(paths, ImpactedPath{Path: []string{targetID, e.TargetServiceID}})
	}

	return paths
}

// --- before/after values and assumptions ---

// buildSpikeBeforeAfterValues computes deterministic before/after estimates for the
// traffic spike scenario. Two field references are always emitted:
//
//   - spike.target.incoming_rps   (before=observed RPS, after=observed × multiplier)
//   - spike.target.latency_p95_ms (before=observed P95, after=projected under load)
//
// Latency projection uses a linear model: after ≈ before × LoadMultiplier.
// This over-estimates latency degradation under spike (real queuing effects are sub-linear
// under moderate load) and is declared as an explicit conservative assumption.
func buildSpikeBeforeAfterValues(
	params *TrafficSpikeParams,
	incomingEdges []SnapshotServiceEdge,
	evidence EvidenceResolverResult,
) ([]BeforeAfterValue, []SimulationAssumption) {
	multiplier := params.LoadMultiplier

	evidenceSource := string(EvidenceSourceLiveServiceGraph)
	if len(evidence.Sources) > 0 {
		evidenceSource = string(evidence.Sources[0])
	}

	// Aggregate incoming RPS and P95 latency from snapshot edges.
	var totalRPS float64
	var p95Sum float64
	var p95Count int
	for _, e := range incomingEdges {
		totalRPS += e.RateRPS
		if e.P95Ms != nil {
			p95Sum += *e.P95Ms
			p95Count++
		}
	}

	var bavs []BeforeAfterValue

	// --- incoming_rps ---
	spikeRPS := math.Round(totalRPS*multiplier*100) / 100
	deltaRPS := spikeRPS - totalRPS
	bavs = append(bavs, BeforeAfterValue{
		FieldRef:    "spike.target.incoming_rps",
		Description: "Total incoming request rate (RPS) to the target service before and during the load spike",
		Unit:        "rps",
		BeforeValue: &totalRPS,
		AfterValue:  &spikeRPS,
		DeltaValue:  &deltaRPS,
	})

	// --- latency_p95_ms (only when P95 data is available from snapshot edges) ---
	if p95Count > 0 {
		beforeLatency := math.Round(p95Sum/float64(p95Count)*100) / 100
		// Conservative linear model: latency scales proportionally with load multiplier.
		afterLatency := math.Round(beforeLatency*multiplier*100) / 100
		deltaLatency := afterLatency - beforeLatency
		bavs = append(bavs, BeforeAfterValue{
			FieldRef:    "spike.target.latency_p95_ms",
			Description: "Average P95 latency for calls to the target service (projected under spike load using linear model)",
			Unit:        "ms",
			BeforeValue: &beforeLatency,
			AfterValue:  &afterLatency,
			DeltaValue:  &deltaLatency,
		})
	}

	assumptions := []SimulationAssumption{
		{
			Key: "spike.linear_latency_model",
			Description: fmt.Sprintf(
				"P95 latency under spike is projected using a conservative linear model: after_p95 ≈ before_p95 × LoadMultiplier (%.4g). "+
					"Real latency degradation may be sub-linear (under moderate queuing) or super-linear (near saturation); "+
					"this model provides an upper-bound estimate.",
				multiplier,
			),
			Source: "engine_default",
		},
		{
			Key: "spike.rps_linear_scale",
			Description: fmt.Sprintf(
				"Spike load is modeled as a uniform %.4g× increase applied to total observed incoming RPS. "+
					"Non-uniform distribution (e.g., burst to subset of endpoints) is not modeled.",
				multiplier,
			),
			Source: "engine_default",
		},
		{
			Key: "edge_data.source",
			Description: fmt.Sprintf(
				"Baseline RPS and latency values are taken from snapshot edge data sourced from %q.",
				evidenceSource,
			),
			Source: evidenceSource,
		},
	}

	return bavs, assumptions
}

// --- recommendation ---

// buildSpikeRecommendation returns a deterministic operator recommendation for the
// traffic spike scenario. The action and explanation reference the evidence source, mode,
// confidence, and projected load values used in the decision.
func buildSpikeRecommendation(
	ctx ExecutionContext,
	targetID string,
	params *TrafficSpikeParams,
	incomingEdges []SnapshotServiceEdge,
) SimulationRecommendation {
	evidenceLabel := string(EvidenceSourceLiveServiceGraph)
	if len(ctx.Evidence.Sources) > 0 {
		evidenceLabel = string(ctx.Evidence.Sources[0])
	}

	var totalRPS float64
	for _, e := range incomingEdges {
		totalRPS += e.RateRPS
	}

	spikeRPS := math.Round(totalRPS*params.LoadMultiplier*100) / 100

	// Classify severity: multipliers ≥ 3× are high-severity and warrant pre-emptive scaling;
	// multipliers between 1× and 3× are moderate and warrant monitoring plus readiness checks.
	var action, explanation string

	if params.LoadMultiplier >= 3.0 {
		action = "pre_emptive_scale_up_required"
		explanation = fmt.Sprintf(
			"A %.2f× load spike on service %q is projected to increase incoming RPS from %.2f to %.2f "+
				"(evidence: %s, mode: %s, confidence: %s). "+
				"At this magnitude, the service is at high risk of saturation and latency degradation. "+
				"Pre-emptively scale up replicas and configure rate-limiting or load-shedding before the spike arrives. "+
				"Review downstream services for cascading pressure and confirm HPA and circuit breaker policies are active.",
			params.LoadMultiplier, targetID, totalRPS, spikeRPS,
			evidenceLabel, ctx.Evidence.Mode, ctx.Evidence.Confidence,
		)
	} else {
		action = "monitor_and_prepare_rate_limits"
		explanation = fmt.Sprintf(
			"A %.2f× load spike on service %q is projected to increase incoming RPS from %.2f to %.2f "+
				"(evidence: %s, mode: %s, confidence: %s). "+
				"Monitor P95 latency and error rates closely during the spike window. "+
				"Ensure auto-scaling policies (HPA) can respond within the spike ramp time, "+
				"and verify rate-limiting and circuit-breaker settings on callers. "+
				"Review snapshot-derived impacted paths to confirm downstream services can absorb cascaded load.",
			params.LoadMultiplier, targetID, totalRPS, spikeRPS,
			evidenceLabel, ctx.Evidence.Mode, ctx.Evidence.Confidence,
		)
	}

	return SimulationRecommendation{
		Action:      action,
		Explanation: explanation,
	}
}
