package simulation

import (
	"fmt"
	"math"
	"strings"
)

// RunScalingScenario executes the Scaling up/down scenario model.
//
// It uses the immutable SimulationSnapshot inside the ExecutionContext to determine
// the before/after impact of changing the pod count for the target service. Latency
// and RPS-capacity estimates are projected deterministically from snapshot edge data
// using explicit linear formulas; no random values or wall-clock inputs are used.
//
// The function returns ResultStatusDeferred when the target service is not present in
// the snapshot graph, preventing guessed numeric values from leaking into the response.
func RunScalingScenario(ctx ExecutionContext) SimulationResponse {
	resp := BuildBaseResponse(ctx)
	params := ctx.Request.ScalingParams

	targetID := strings.TrimSpace(params.TargetServiceID)

	// Locate target in snapshot. Absence means no graph truth to reason from.
	targetNode := findSnapshotNode(ctx.Snapshot, targetID)
	if targetNode == nil {
		resp.ResultStatus = ResultStatusDeferred
		resp.DeferredReason = fmt.Sprintf(
			"target service %q not found in snapshot graph; scaling impact cannot be computed without graph truth",
			targetID,
		)
		resp.Assumptions = []SimulationAssumption{}
		resp.ImpactedServices = []ImpactedService{}
		resp.ImpactedPaths = []ImpactedPath{}
		resp.BeforeAfterValues = []BeforeAfterValue{}
		NormalizeResponse(&resp)
		return resp
	}

	// No-change case: same pod count means no delta to project.
	if params.CurrentPods == params.NewPods {
		resp.ResultStatus = ResultStatusDeferred
		resp.DeferredReason = fmt.Sprintf(
			"scalingParams.newPods equals scalingParams.currentPods (%d); no scaling change to simulate",
			params.CurrentPods,
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

	latencyMetric := strings.TrimSpace(params.LatencyMetric)
	if latencyMetric == "" {
		latencyMetric = "p95"
	}

	impacted := buildScalingImpactedServices(ctx.Snapshot, targetID, *targetNode, incomingEdges)
	paths := buildScalingImpactedPaths(targetID, incomingEdges, outgoingEdges)
	bav, assumptions := buildScalingBeforeAfterValues(params, incomingEdges, latencyMetric, ctx.Evidence)
	rec := buildScalingRecommendation(ctx, targetID, params, incomingEdges)

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

// buildScalingImpactedServices returns the target and its direct callers.
// Callers are included because they observe latency changes when the target is rescaled.
// Role values: "target", "caller".
func buildScalingImpactedServices(
	snap SimulationSnapshot,
	targetID string,
	targetNode SnapshotServiceNode,
	incomingEdges []SnapshotServiceEdge,
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

	return services
}

// --- impacted paths ---

// buildScalingImpactedPaths returns the communication paths that are affected by the
// scaling change. Both caller→target and target→downstream paths are included because
// throughput and latency changes propagate in both directions.
func buildScalingImpactedPaths(
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

// buildScalingBeforeAfterValues computes deterministic before/after estimates for the
// scaling scenario. Three field references are emitted:
//   - scaling.target.pod_count          (before=currentPods, after=newPods)
//   - scaling.target.rps_capacity       (projected from incoming RPS × scaling ratio)
//   - scaling.target.latency_estimate   (projected from snapshot P95/P50/P99 × inverse ratio)
//
// The latency projection uses linear inverse-proportionality: after ≈ before × (current/new).
// This is declared as an explicit assumption so callers know the formula used.
func buildScalingBeforeAfterValues(
	params *ScalingParams,
	incomingEdges []SnapshotServiceEdge,
	latencyMetric string,
	evidence EvidenceResolverResult,
) ([]BeforeAfterValue, []SimulationAssumption) {
	currentPods := float64(params.CurrentPods)
	newPods := float64(params.NewPods)
	scalingRatio := newPods / currentPods

	evidenceSource := string(EvidenceSourceLiveServiceGraph)
	if len(evidence.Sources) > 0 {
		evidenceSource = string(evidence.Sources[0])
	}

	var bavs []BeforeAfterValue

	// --- pod_count ---
	beforePods := currentPods
	afterPods := newPods
	deltaPods := afterPods - beforePods
	bavs = append(bavs, BeforeAfterValue{
		FieldRef:    "scaling.target.pod_count",
		Description: "Number of pod replicas for the target service",
		Unit:        "pods",
		BeforeValue: &beforePods,
		AfterValue:  &afterPods,
		DeltaValue:  &deltaPods,
	})

	// --- rps_capacity ---
	// Total incoming RPS from snapshot edges is the observed load at current pod count.
	// Projected capacity after scaling = observed_rps × scaling_ratio.
	var totalRPS float64
	for _, e := range incomingEdges {
		totalRPS += e.RateRPS
	}
	afterRPS := math.Round(totalRPS*scalingRatio*100) / 100
	deltaRPS := afterRPS - totalRPS
	bavs = append(bavs, BeforeAfterValue{
		FieldRef:    "scaling.target.rps_capacity",
		Description: "Estimated request-handling capacity (RPS) based on current observed load and pod scaling ratio",
		Unit:        "rps",
		BeforeValue: &totalRPS,
		AfterValue:  &afterRPS,
		DeltaValue:  &deltaRPS,
	})

	// --- latency_estimate ---
	// Collect the requested latency percentile from snapshot edges (incoming to target).
	var latencySum float64
	var latencyCount int
	for _, e := range incomingEdges {
		var val *float64
		switch latencyMetric {
		case "p50":
			val = e.P50Ms
		case "p99":
			val = e.P99Ms
		default: // "p95"
			val = e.P95Ms
		}
		if val != nil {
			latencySum += *val
			latencyCount++
		}
	}

	if latencyCount > 0 {
		beforeLatency := math.Round(latencySum/float64(latencyCount)*100) / 100
		// Inverse-proportional: more pods → lower latency per pod.
		afterLatency := math.Round(beforeLatency/scalingRatio*100) / 100
		deltaLatency := afterLatency - beforeLatency
		fieldRef := fmt.Sprintf("scaling.target.latency_%s_ms", latencyMetric)
		bavs = append(bavs, BeforeAfterValue{
			FieldRef:    fieldRef,
			Description: fmt.Sprintf("Average %s latency estimate for calls to the target service (projected via inverse-proportional scaling)", strings.ToUpper(latencyMetric)),
			Unit:        "ms",
			BeforeValue: &beforeLatency,
			AfterValue:  &afterLatency,
			DeltaValue:  &deltaLatency,
		})
	}

	scaleDirection := "scale_up"
	if newPods < currentPods {
		scaleDirection = "scale_down"
	}

	assumptions := []SimulationAssumption{
		{
			Key: "scaling.linear_rps_capacity",
			Description: fmt.Sprintf(
				"RPS capacity is assumed to scale linearly with pod count (ratio: %.4g). "+
					"Non-linear effects (JVM warm-up, connection pool limits) are not modeled.",
				scalingRatio,
			),
			Source: "engine_default",
		},
		{
			Key: "scaling.inverse_proportional_latency",
			Description: fmt.Sprintf(
				"Latency is projected using inverse-proportional pod scaling: after_%s ≈ before_%s × (currentPods/newPods). "+
					"Actual latency may differ due to non-linear queuing or resource contention.",
				latencyMetric, latencyMetric,
			),
			Source: "engine_default",
		},
		{
			Key: "scaling.direction",
			Description: fmt.Sprintf(
				"Scenario direction is %s (currentPods=%d → newPods=%d).",
				scaleDirection, params.CurrentPods, params.NewPods,
			),
			Source: "engine_default",
		},
		{
			Key: "edge_data.source",
			Description: fmt.Sprintf(
				"Incoming RPS and latency values are taken from snapshot edge data sourced from %q.",
				evidenceSource,
			),
			Source: evidenceSource,
		},
	}

	return bavs, assumptions
}

// --- recommendation ---

// buildScalingRecommendation returns a deterministic operator recommendation for the
// scaling scenario. The action and explanation reference the evidence source, mode,
// and confidence used, and the computed scaling direction.
func buildScalingRecommendation(
	ctx ExecutionContext,
	targetID string,
	params *ScalingParams,
	incomingEdges []SnapshotServiceEdge,
) SimulationRecommendation {
	evidenceLabel := string(EvidenceSourceLiveServiceGraph)
	if len(ctx.Evidence.Sources) > 0 {
		evidenceLabel = string(ctx.Evidence.Sources[0])
	}

	scaleUp := params.NewPods > params.CurrentPods

	var totalRPS float64
	for _, e := range incomingEdges {
		totalRPS += e.RateRPS
	}

	var action, explanation string

	if scaleUp {
		action = "approve_scale_up"
		explanation = fmt.Sprintf(
			"Scaling service %q from %d to %d pods (%.2f× increase) is projected to increase RPS capacity proportionally "+
				"and reduce per-pod latency (evidence: %s, mode: %s, confidence: %s). "+
				"Current observed load is %.2f RPS. "+
				"Verify resource quotas and HPA limits before applying. "+
				"Review snapshot-derived impacted paths to confirm caller readiness.",
			targetID, params.CurrentPods, params.NewPods,
			float64(params.NewPods)/float64(params.CurrentPods),
			evidenceLabel, ctx.Evidence.Mode, ctx.Evidence.Confidence,
			totalRPS,
		)
	} else {
		// Scale down — assess whether reducing pods risks dropping below observed load.
		scalingRatio := float64(params.NewPods) / float64(params.CurrentPods)
		projectedCapacity := totalRPS * scalingRatio
		if projectedCapacity < totalRPS*0.8 {
			// Significant capacity reduction relative to observed load.
			action = "caution_scale_down"
			explanation = fmt.Sprintf(
				"Scaling service %q from %d to %d pods (%.2f× reduction) is projected to reduce RPS capacity to %.2f "+
					"against current observed load of %.2f RPS — a reduction exceeding 20%% of current capacity "+
					"(evidence: %s, mode: %s, confidence: %s). "+
					"Verify that projected capacity meets expected peak load before applying. "+
					"Consider staged scale-down with live monitoring of error rates and latency.",
				targetID, params.CurrentPods, params.NewPods,
				scalingRatio, projectedCapacity, totalRPS,
				evidenceLabel, ctx.Evidence.Mode, ctx.Evidence.Confidence,
			)
		} else {
			action = "approve_scale_down"
			explanation = fmt.Sprintf(
				"Scaling service %q from %d to %d pods (%.2f× reduction) is projected to reduce capacity to %.2f RPS "+
					"against current observed load of %.2f RPS, remaining within a safe operating margin "+
					"(evidence: %s, mode: %s, confidence: %s). "+
					"Monitor latency and error rates after applying; revert if degradation exceeds thresholds.",
				targetID, params.CurrentPods, params.NewPods,
				scalingRatio, projectedCapacity, totalRPS,
				evidenceLabel, ctx.Evidence.Mode, ctx.Evidence.Confidence,
			)
		}
	}

	return SimulationRecommendation{
		Action:      action,
		Explanation: explanation,
	}
}
