package simulation

import (
	"fmt"
	"math"
	"strings"
)

// chattyRPSThreshold is the minimum observed RPS on the source→target edge that
// classifies the pair as chatty and warrants a co-location or migration recommendation.
// Pairs below this threshold are assigned no_change.
const chattyRPSThreshold = 50.0

// colocationLatencyFactor is the deterministic multiplier applied to observed P95 latency
// to project post-co-location latency. A value of 0.60 models a 40% reduction in
// inter-service communication latency achieved by placing services on the same node or zone.
const colocationLatencyFactor = 0.60

// RunChattyColocationScenario executes the Chatty-service co-location / migration scenario.
//
// It reasons from the direct communication edge between SourceServiceID and TargetServiceID
// in the snapshot to determine whether the pair qualifies as chatty and what topology change
// (co-locate, migrate, or no-change) would reduce communication overhead.
//
// The function returns ResultStatusDeferred when either service is absent from the snapshot
// or when no direct edge exists between the pair, because no defensible recommendation
// can be made without observed graph truth.
func RunChattyColocationScenario(ctx ExecutionContext) SimulationResponse {
	resp := BuildBaseResponse(ctx)
	params := ctx.Request.ChattyColocationParams

	sourceID := strings.TrimSpace(params.SourceServiceID)
	targetID := strings.TrimSpace(params.TargetServiceID)

	// Both services must be present in the snapshot graph.
	sourceNode := findSnapshotNode(ctx.Snapshot, sourceID)
	if sourceNode == nil {
		resp.ResultStatus = ResultStatusDeferred
		resp.DeferredReason = fmt.Sprintf(
			"source service %q not found in snapshot graph; chatty co-location impact cannot be computed without graph truth",
			sourceID,
		)
		resp.Assumptions = []SimulationAssumption{}
		resp.ImpactedServices = []ImpactedService{}
		resp.ImpactedPaths = []ImpactedPath{}
		resp.BeforeAfterValues = []BeforeAfterValue{}
		NormalizeResponse(&resp)
		return resp
	}

	targetNode := findSnapshotNode(ctx.Snapshot, targetID)
	if targetNode == nil {
		resp.ResultStatus = ResultStatusDeferred
		resp.DeferredReason = fmt.Sprintf(
			"target service %q not found in snapshot graph; chatty co-location impact cannot be computed without graph truth",
			targetID,
		)
		resp.Assumptions = []SimulationAssumption{}
		resp.ImpactedServices = []ImpactedService{}
		resp.ImpactedPaths = []ImpactedPath{}
		resp.BeforeAfterValues = []BeforeAfterValue{}
		NormalizeResponse(&resp)
		return resp
	}

	// Find the direct edge from source to target.
	// Without a measured edge we cannot defensibly claim the pair is chatty.
	edge := findDirectEdge(ctx.Snapshot, sourceID, targetID)
	if edge == nil {
		resp.ResultStatus = ResultStatusDeferred
		resp.DeferredReason = fmt.Sprintf(
			"no direct communication edge found from %q to %q in snapshot; "+
				"chatty co-location recommendation requires an observed call relationship",
			sourceID, targetID,
		)
		resp.Assumptions = []SimulationAssumption{}
		resp.ImpactedServices = []ImpactedService{}
		resp.ImpactedPaths = []ImpactedPath{}
		resp.BeforeAfterValues = []BeforeAfterValue{}
		NormalizeResponse(&resp)
		return resp
	}

	impacted := buildChattyImpactedServices(*sourceNode, *targetNode, sourceID, targetID)
	paths := buildChattyImpactedPaths(sourceID, targetID)
	bav, assumptions := buildChattyBeforeAfterValues(edge, ctx.Evidence)
	rec := buildChattyRecommendation(ctx, sourceID, targetID, *sourceNode, *targetNode, edge)

	resp.ResultStatus = ResultStatusOK
	resp.ImpactedServices = impacted
	resp.ImpactedPaths = paths
	resp.BeforeAfterValues = bav
	resp.Assumptions = assumptions
	resp.Recommendation = rec

	NormalizeResponse(&resp)
	return resp
}

// --- helpers ---

// findDirectEdge returns the SnapshotServiceEdge for the directed link from sourceID to
// targetID, or nil if no such edge exists in the snapshot.
func findDirectEdge(snap SimulationSnapshot, sourceID, targetID string) *SnapshotServiceEdge {
	for i := range snap.ServiceEdges {
		e := &snap.ServiceEdges[i]
		if e.SourceServiceID == sourceID && e.TargetServiceID == targetID {
			return e
		}
	}
	return nil
}

// --- impacted services ---

// buildChattyImpactedServices returns the chatty source and chatty target with their roles.
func buildChattyImpactedServices(
	sourceNode, targetNode SnapshotServiceNode,
	sourceID, targetID string,
) []ImpactedService {
	return []ImpactedService{
		{
			ServiceID: sourceID,
			Name:      sourceNode.Name,
			Namespace: sourceNode.Namespace,
			Role:      "chatty_source",
		},
		{
			ServiceID: targetID,
			Name:      targetNode.Name,
			Namespace: targetNode.Namespace,
			Role:      "chatty_target",
		},
	}
}

// --- impacted paths ---

// buildChattyImpactedPaths returns the single directed path from source to target.
func buildChattyImpactedPaths(sourceID, targetID string) []ImpactedPath {
	return []ImpactedPath{
		{Path: []string{sourceID, targetID}},
	}
}

// --- before/after values and assumptions ---

// buildChattyBeforeAfterValues computes deterministic before/after estimates for the
// chatty co-location scenario. Two field references are emitted:
//
//   - colocation.edge.rps             (before=observed RPS, after=unchanged — co-location
//     does not reduce call frequency, only communication cost)
//   - colocation.edge.latency_p95_ms  (before=observed P95, after=before × colocationLatencyFactor,
//     representing projected latency reduction from same-node or same-zone placement)
//
// The latency reduction factor is declared as an explicit assumption.
func buildChattyBeforeAfterValues(
	edge *SnapshotServiceEdge,
	evidence EvidenceResolverResult,
) ([]BeforeAfterValue, []SimulationAssumption) {
	evidenceSource := string(EvidenceSourceLiveServiceGraph)
	if len(evidence.Sources) > 0 {
		evidenceSource = string(evidence.Sources[0])
	}

	var bavs []BeforeAfterValue

	// --- edge RPS (unchanged by co-location) ---
	beforeRPS := math.Round(edge.RateRPS*100) / 100
	afterRPS := beforeRPS // co-location does not change call frequency
	deltaRPS := float64(0)
	bavs = append(bavs, BeforeAfterValue{
		FieldRef:    "colocation.edge.rps",
		Description: "Observed request rate (RPS) on the source→target communication edge; co-location does not change call frequency",
		Unit:        "rps",
		BeforeValue: &beforeRPS,
		AfterValue:  &afterRPS,
		DeltaValue:  &deltaRPS,
	})

	// --- P95 latency (projected improvement after co-location) ---
	if edge.P95Ms != nil {
		beforeLatency := math.Round(*edge.P95Ms*100) / 100
		afterLatency := math.Round(beforeLatency*colocationLatencyFactor*100) / 100
		deltaLatency := afterLatency - beforeLatency
		bavs = append(bavs, BeforeAfterValue{
			FieldRef:    "colocation.edge.latency_p95_ms",
			Description: "P95 latency on the source→target edge before and after projected co-location (same-node/zone placement)",
			Unit:        "ms",
			BeforeValue: &beforeLatency,
			AfterValue:  &afterLatency,
			DeltaValue:  &deltaLatency,
		})
	}

	assumptions := []SimulationAssumption{
		{
			Key: "colocation.latency_reduction_factor",
			Description: fmt.Sprintf(
				"Co-location is projected to reduce P95 inter-service latency by %.0f%% (factor %.2f). "+
					"This models same-node or same-zone placement eliminating cross-node network hops. "+
					"Actual reduction depends on underlying network topology and is not measured from history.",
				(1.0-colocationLatencyFactor)*100, colocationLatencyFactor,
			),
			Source: "engine_default",
		},
		{
			Key: "colocation.rps_unchanged",
			Description: "Co-location or migration does not alter the call frequency (RPS) between services; " +
				"it reduces communication overhead per call, not the number of calls.",
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

// buildChattyRecommendation returns a deterministic operator recommendation for the
// chatty co-location scenario. The recommendation action is one of:
//
//   - "co_locate"   — same namespace, high RPS: pin both services to the same node group
//   - "migrate"     — different namespaces, high RPS: move source or target to co-locate
//   - "no_change"   — RPS below chattyRPSThreshold: topology change is not warranted
//
// The explanation references the evidence source, mode, confidence, and observed RPS used
// in the classification decision.
func buildChattyRecommendation(
	ctx ExecutionContext,
	sourceID, targetID string,
	sourceNode, targetNode SnapshotServiceNode,
	edge *SnapshotServiceEdge,
) SimulationRecommendation {
	evidenceLabel := string(EvidenceSourceLiveServiceGraph)
	if len(ctx.Evidence.Sources) > 0 {
		evidenceLabel = string(ctx.Evidence.Sources[0])
	}

	observedRPS := math.Round(edge.RateRPS*100) / 100
	sameNamespace := sourceNode.Namespace == targetNode.Namespace

	var action, explanation string

	if observedRPS >= chattyRPSThreshold {
		if sameNamespace {
			action = "co_locate"
			explanation = fmt.Sprintf(
				"Services %q and %q communicate at %.2f RPS (threshold: %.0f RPS), "+
					"classifying them as a chatty pair (evidence: %s, mode: %s, confidence: %s). "+
					"Both services are in the same namespace (%q). "+
					"Recommendation: pin both services to the same node group or affinity zone to eliminate cross-node network hops. "+
					"Expected benefit: ~%.0f%% reduction in P95 inter-service latency per the engine co-location model. "+
					"Verify pod anti-affinity rules do not prevent same-node scheduling.",
				sourceID, targetID, observedRPS, chattyRPSThreshold,
				evidenceLabel, ctx.Evidence.Mode, ctx.Evidence.Confidence,
				sourceNode.Namespace, (1.0-colocationLatencyFactor)*100,
			)
		} else {
			action = "migrate"
			explanation = fmt.Sprintf(
				"Services %q (namespace: %q) and %q (namespace: %q) communicate at %.2f RPS (threshold: %.0f RPS), "+
					"classifying them as a chatty pair (evidence: %s, mode: %s, confidence: %s). "+
					"The services are in different namespaces, so same-node pinning may be insufficient. "+
					"Recommendation: migrate %q into the same namespace/cluster zone as %q, "+
					"or establish a dedicated service mesh lane between namespaces to reduce cross-namespace latency. "+
					"Expected benefit: ~%.0f%% reduction in P95 inter-service latency per the engine co-location model. "+
					"Confirm RBAC and network policy rules permit the migration.",
				sourceID, sourceNode.Namespace, targetID, targetNode.Namespace,
				observedRPS, chattyRPSThreshold,
				evidenceLabel, ctx.Evidence.Mode, ctx.Evidence.Confidence,
				sourceID, targetID, (1.0-colocationLatencyFactor)*100,
			)
		}
	} else {
		action = "no_change"
		explanation = fmt.Sprintf(
			"Services %q and %q communicate at %.2f RPS, which is below the chatty classification threshold of %.0f RPS "+
				"(evidence: %s, mode: %s, confidence: %s). "+
				"The observed communication frequency does not justify a topology change at this time. "+
				"No co-location or migration action is recommended. "+
				"Re-evaluate if traffic patterns change or if P95 latency on this edge exceeds service-level objectives.",
			sourceID, targetID, observedRPS, chattyRPSThreshold,
			evidenceLabel, ctx.Evidence.Mode, ctx.Evidence.Confidence,
		)
	}

	return SimulationRecommendation{
		Action:      action,
		Explanation: explanation,
	}
}
