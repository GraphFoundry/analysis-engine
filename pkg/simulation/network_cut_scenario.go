package simulation

import (
	"fmt"
	"math"
	"strings"
)

// networkCutFullThreshold: DegradationPercent >= this value is treated as a full cut.
const networkCutFullThreshold = 100.0

// networkCutMatchedLink pairs a declared NetworkLink with its resolved snapshot edge.
type networkCutMatchedLink struct {
	link NetworkLink
	edge SnapshotServiceEdge
}

// RunNetworkCutScenario executes the Network Cut / network degradation scenario model.
//
// It evaluates the impact of severing or degrading one or more directed service communication
// links declared in NetworkCutParams.AffectedLinks. For each link that matches a snapshot edge,
// deterministic before/after values are computed from the edge data and the optional
// DegradationPercent. A full cut (DegradationPercent == nil or 100) sets after RPS to zero and
// error rate to 1.0. A partial degradation adjusts RPS, error rate, and latency proportionally.
//
// The function returns ResultStatusDeferred when none of the declared affected links exist in the
// snapshot graph; it never emits guessed numeric values for links absent from the snapshot.
func RunNetworkCutScenario(ctx ExecutionContext) SimulationResponse {
	resp := BuildBaseResponse(ctx)
	params := ctx.Request.NetworkCutParams

	// Resolve which declared links actually exist in the snapshot edge set.
	var matched []networkCutMatchedLink
	var missingLinks []NetworkLink

	for _, link := range params.AffectedLinks {
		srcID := strings.TrimSpace(link.SourceServiceID)
		tgtID := strings.TrimSpace(link.TargetServiceID)
		edge := findNetworkEdge(ctx.Snapshot.ServiceEdges, srcID, tgtID)
		if edge == nil {
			missingLinks = append(missingLinks, link)
			continue
		}
		matched = append(matched, networkCutMatchedLink{link: link, edge: *edge})
	}

	// If no declared links are present in the snapshot, return DEFERRED.
	if len(matched) == 0 {
		linkStrs := make([]string, len(params.AffectedLinks))
		for i, l := range params.AffectedLinks {
			linkStrs[i] = fmt.Sprintf("%s\u2192%s", l.SourceServiceID, l.TargetServiceID)
		}
		resp.ResultStatus = ResultStatusDeferred
		resp.DeferredReason = fmt.Sprintf(
			"none of the %d declared affected link(s) [%s] were found in the snapshot graph; "+
				"network cut impact cannot be computed without graph truth",
			len(params.AffectedLinks), strings.Join(linkStrs, ", "),
		)
		resp.Assumptions = []SimulationAssumption{}
		resp.ImpactedServices = []ImpactedService{}
		resp.ImpactedPaths = []ImpactedPath{}
		resp.BeforeAfterValues = []BeforeAfterValue{}
		NormalizeResponse(&resp)
		return resp
	}

	// Determine degradation mode: full cut vs. partial degradation.
	isFullCut := params.DegradationPercent == nil || *params.DegradationPercent >= networkCutFullThreshold
	var degradationFactor float64 // fraction of traffic/capacity lost, e.g. 0.30 for 30%
	if !isFullCut {
		degradationFactor = *params.DegradationPercent / 100.0
	} else {
		degradationFactor = 1.0
	}

	impacted := buildNetworkCutImpactedServices(ctx.Snapshot, matched)
	paths := buildNetworkCutImpactedPaths(matched)
	bav, assumptions := buildNetworkCutBeforeAfterValues(matched, isFullCut, degradationFactor, ctx.Evidence)
	rec := buildNetworkCutRecommendation(ctx, matched, isFullCut, degradationFactor, missingLinks)

	resp.ResultStatus = ResultStatusOK
	resp.ImpactedServices = impacted
	resp.ImpactedPaths = paths
	resp.BeforeAfterValues = bav
	resp.Assumptions = assumptions
	resp.Recommendation = rec

	NormalizeResponse(&resp)
	return resp
}

// --- edge lookup ---

// findNetworkEdge returns a pointer to the first SnapshotServiceEdge with the given
// source and target, or nil if none exists.
func findNetworkEdge(edges []SnapshotServiceEdge, srcID, tgtID string) *SnapshotServiceEdge {
	for i := range edges {
		if edges[i].SourceServiceID == srcID && edges[i].TargetServiceID == tgtID {
			return &edges[i]
		}
	}
	return nil
}

// --- impacted services ---

// buildNetworkCutImpactedServices collects unique service IDs from both endpoints of all
// matched links. Role: "cut_source" for the sending side, "cut_target" for the receiving side.
func buildNetworkCutImpactedServices(snap SimulationSnapshot, matched []networkCutMatchedLink) []ImpactedService {
	seen := map[string]string{} // serviceID → role (first assignment wins)

	for _, m := range matched {
		srcID := m.link.SourceServiceID
		tgtID := m.link.TargetServiceID
		if _, ok := seen[srcID]; !ok {
			seen[srcID] = "cut_source"
		}
		if _, ok := seen[tgtID]; !ok {
			seen[tgtID] = "cut_target"
		}
	}

	services := make([]ImpactedService, 0, len(seen))
	for id, role := range seen {
		name, ns := resolveNodeMeta(snap, id)
		services = append(services, ImpactedService{
			ServiceID: id,
			Name:      name,
			Namespace: ns,
			Role:      role,
		})
	}
	return services
}

// --- impacted paths ---

// buildNetworkCutImpactedPaths returns one ImpactedPath per matched link.
func buildNetworkCutImpactedPaths(matched []networkCutMatchedLink) []ImpactedPath {
	paths := make([]ImpactedPath, 0, len(matched))
	for _, m := range matched {
		paths = append(paths, ImpactedPath{
			Path: []string{m.link.SourceServiceID, m.link.TargetServiceID},
		})
	}
	return paths
}

// --- before/after values and assumptions ---

// buildNetworkCutBeforeAfterValues computes deterministic before/after estimates for each
// matched link. For a full cut: after_rps=0, after_error_rate=1.0, after_latency=nil (unreachable).
// For partial degradation (factor f in [0,1)):
//
//	after_rps        = before_rps × (1 − f)
//	after_error_rate = 1.0 − (1.0 − before_error_rate) × (1 − f)
//	after_latency_p95 = before_latency_p95 × (1 + f)   [congestion model]
func buildNetworkCutBeforeAfterValues(
	matched []networkCutMatchedLink,
	isFullCut bool,
	degradationFactor float64,
	evidence EvidenceResolverResult,
) ([]BeforeAfterValue, []SimulationAssumption) {
	evidenceSource := string(EvidenceSourceLiveServiceGraph)
	if len(evidence.Sources) > 0 {
		evidenceSource = string(evidence.Sources[0])
	}

	var bavs []BeforeAfterValue

	for _, m := range matched {
		srcID := m.link.SourceServiceID
		tgtID := m.link.TargetServiceID
		e := m.edge
		prefix := fmt.Sprintf("network.link.%s.%s", srcID, tgtID)

		// --- RPS ---
		beforeRPS := e.RateRPS
		var afterRPS float64
		if isFullCut {
			afterRPS = 0.0
		} else {
			afterRPS = math.Round(beforeRPS*(1.0-degradationFactor)*100) / 100
		}
		deltaRPS := afterRPS - beforeRPS
		bavs = append(bavs, BeforeAfterValue{
			FieldRef:    prefix + ".rps",
			Description: fmt.Sprintf("Request rate on link %s\u2192%s", srcID, tgtID),
			Unit:        "rps",
			BeforeValue: &beforeRPS,
			AfterValue:  &afterRPS,
			DeltaValue:  &deltaRPS,
		})

		// --- Error rate ---
		beforeErr := e.ErrorRate
		var afterErr float64
		if isFullCut {
			afterErr = 1.0
		} else {
			// Degraded path still passes (1−f) fraction of traffic cleanly.
			afterErr = math.Round((1.0-(1.0-beforeErr)*(1.0-degradationFactor))*10000) / 10000
		}
		deltaErr := afterErr - beforeErr
		bavs = append(bavs, BeforeAfterValue{
			FieldRef:    prefix + ".error_rate",
			Description: fmt.Sprintf("Error rate on link %s\u2192%s (1.0 = 100%% errors after full cut)", srcID, tgtID),
			Unit:        "ratio",
			BeforeValue: &beforeErr,
			AfterValue:  &afterErr,
			DeltaValue:  &deltaErr,
		})

		// --- Latency P95 (only when edge has P95 data and link is not fully cut) ---
		if e.P95Ms != nil && !isFullCut {
			beforeP95 := math.Round(*e.P95Ms*100) / 100
			afterP95 := math.Round(beforeP95*(1.0+degradationFactor)*100) / 100
			deltaP95 := afterP95 - beforeP95
			bavs = append(bavs, BeforeAfterValue{
				FieldRef:    prefix + ".latency_p95_ms",
				Description: fmt.Sprintf("P95 latency on link %s\u2192%s (congestion model: increases with packet loss)", srcID, tgtID),
				Unit:        "ms",
				BeforeValue: &beforeP95,
				AfterValue:  &afterP95,
				DeltaValue:  &deltaP95,
			})
		}
	}

	// Build assumptions.
	var modeDesc string
	if isFullCut {
		modeDesc = "full network cut (100% packet loss)"
	} else {
		modeDesc = fmt.Sprintf("partial degradation (%.1f%% packet loss / latency addition)", degradationFactor*100)
	}

	assumptions := []SimulationAssumption{
		{
			Key:         "network_cut.degradation_mode",
			Description: fmt.Sprintf("Simulation models %s for all declared affected links.", modeDesc),
			Source:      "engine_default",
		},
		{
			Key:         "network_cut.error_rate_model",
			Description: "After a full cut, error rate reaches 1.0; for partial degradation, effective error rate is 1 - (1 - baseline_error) * (1 - loss_fraction). No graceful failover or retry is modeled.",
			Source:      "engine_default",
		},
		{
			Key:         "network_cut.latency_model",
			Description: "For partial degradation, P95 latency is projected as before_p95 * (1 + loss_fraction). This is a conservative upper-bound congestion model. Latency is undefined (nil) after a full cut.",
			Source:      "engine_default",
		},
		{
			Key:         "edge_data.source",
			Description: fmt.Sprintf("Baseline RPS, error-rate, and latency values are taken from snapshot edge data sourced from %q.", evidenceSource),
			Source:      evidenceSource,
		},
	}

	return bavs, assumptions
}

// --- recommendation ---

// buildNetworkCutRecommendation returns a deterministic operator recommendation
// for the network cut / degradation scenario.
func buildNetworkCutRecommendation(
	ctx ExecutionContext,
	matched []networkCutMatchedLink,
	isFullCut bool,
	degradationFactor float64,
	missingLinks []NetworkLink,
) SimulationRecommendation {
	evidenceLabel := string(EvidenceSourceLiveServiceGraph)
	if len(ctx.Evidence.Sources) > 0 {
		evidenceLabel = string(ctx.Evidence.Sources[0])
	}

	matchedCount := len(matched)
	missingCount := len(missingLinks)

	var action, explanation string

	if isFullCut {
		action = "implement_failover_routing_and_circuit_breakers"
		explanation = fmt.Sprintf(
			"A full network cut on %d matched link(s) will drop all traffic to zero and raise error rates to 100%% "+
				"(evidence: %s, mode: %s, confidence: %s). "+
				"Implement failover routing to redirect traffic away from severed links, and apply circuit breakers on "+
				"affected callers to prevent cascading failures. "+
				"Confirm with live cluster state before applying changes.",
			matchedCount, evidenceLabel, ctx.Evidence.Mode, ctx.Evidence.Confidence,
		)
	} else {
		pct := math.Round(degradationFactor * 100)
		if pct >= 50 {
			action = "apply_circuit_breaker_with_retry_and_monitor"
		} else {
			action = "monitor_and_apply_traffic_shaping"
		}
		explanation = fmt.Sprintf(
			"A %.0f%% network degradation on %d matched link(s) is projected to reduce throughput and increase latency "+
				"(evidence: %s, mode: %s, confidence: %s). "+
				"Apply traffic shaping and rate limiting on degraded links. "+
				"For degradation >= 50%%, introduce circuit breakers with retry logic to limit error propagation. "+
				"Monitor real-time latency and error rates and escalate to failover routing if degradation worsens.",
			pct, matchedCount, evidenceLabel, ctx.Evidence.Mode, ctx.Evidence.Confidence,
		)
	}

	if missingCount > 0 {
		explanation += fmt.Sprintf(
			" Note: %d declared link(s) were not found in the snapshot graph and are excluded from this analysis.",
			missingCount,
		)
	}

	return SimulationRecommendation{
		Action:      action,
		Explanation: explanation,
	}
}
