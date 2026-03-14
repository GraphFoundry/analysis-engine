package simulation

import (
	"fmt"
	"math"
	"strings"
)

// RunFailureShutdownScenario executes the Failure / Service Shutdown scenario model.
//
// It uses the immutable SimulationSnapshot inside the ExecutionContext to determine
// which services and communication paths are impacted when the target service is shut down.
// All before/after estimates are computed from deterministic formulas applied to snapshot
// edge data; no random values or wall-clock inputs are used.
//
// The function returns ResultStatusDeferred when the target service is not present in the
// snapshot graph; it never silently emits guessed numeric values.
func RunFailureShutdownScenario(ctx ExecutionContext) SimulationResponse {
	resp := BuildBaseResponse(ctx)
	params := ctx.Request.FailureShutdownParams

	targetID := strings.TrimSpace(params.TargetServiceID)

	// Locate target in the snapshot node list. Absence means we cannot compute blast radius.
	targetNode := findSnapshotNode(ctx.Snapshot, targetID)
	if targetNode == nil {
		resp.ResultStatus = ResultStatusDeferred
		resp.DeferredReason = fmt.Sprintf(
			"target service %q not found in snapshot graph; blast-radius cannot be computed without graph truth",
			targetID,
		)
		resp.Assumptions = []SimulationAssumption{}
		resp.ImpactedServices = []ImpactedService{}
		resp.ImpactedPaths = []ImpactedPath{}
		resp.BeforeAfterValues = []BeforeAfterValue{}
		NormalizeResponse(&resp)
		return resp
	}

	// Separate snapshot edges into incoming (callers) and outgoing (downstream) for the target.
	incomingEdges := filterEdgesByTarget(ctx.Snapshot.ServiceEdges, targetID)
	outgoingEdges := filterEdgesBySource(ctx.Snapshot.ServiceEdges, targetID)

	// Build all output components.
	impacted := buildFailureImpactedServices(ctx.Snapshot, targetID, *targetNode, incomingEdges, outgoingEdges)
	paths := buildFailureImpactedPaths(targetID, incomingEdges, outgoingEdges)
	bav, assumptions := buildFailureBeforeAfterValues(targetID, incomingEdges, ctx.Evidence)
	rec := buildFailureShutdownRecommendation(ctx, targetID, impacted, incomingEdges)

	resp.ResultStatus = ResultStatusOK
	resp.ImpactedServices = impacted
	resp.ImpactedPaths = paths
	resp.BeforeAfterValues = bav
	resp.Assumptions = assumptions
	resp.Recommendation = rec

	NormalizeResponse(&resp)
	return resp
}

// --- snapshot traversal helpers ---

// findSnapshotNode returns a pointer to the SnapshotServiceNode whose ServiceID equals
// serviceID, or nil if no match exists. The snapshot slice is sorted, so a linear scan
// is sufficient and deterministic.
func findSnapshotNode(snap SimulationSnapshot, serviceID string) *SnapshotServiceNode {
	for i := range snap.ServiceNodes {
		if snap.ServiceNodes[i].ServiceID == serviceID {
			return &snap.ServiceNodes[i]
		}
	}
	return nil
}

// filterEdgesByTarget returns all edges whose TargetServiceID equals targetID.
func filterEdgesByTarget(edges []SnapshotServiceEdge, targetID string) []SnapshotServiceEdge {
	var result []SnapshotServiceEdge
	for _, e := range edges {
		if e.TargetServiceID == targetID {
			result = append(result, e)
		}
	}
	return result
}

// filterEdgesBySource returns all edges whose SourceServiceID equals targetID.
func filterEdgesBySource(edges []SnapshotServiceEdge, targetID string) []SnapshotServiceEdge {
	var result []SnapshotServiceEdge
	for _, e := range edges {
		if e.SourceServiceID == targetID {
			result = append(result, e)
		}
	}
	return result
}

// --- impacted services ---

// buildFailureImpactedServices returns the target, its direct callers, and its direct
// downstream services drawn from the snapshot edge relationships.
// Role values: "target", "caller", "downstream".
func buildFailureImpactedServices(
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

// resolveNodeMeta returns the Name and Namespace of a service from the snapshot, or
// falls back to the serviceID itself when the node is not in the node list.
func resolveNodeMeta(snap SimulationSnapshot, serviceID string) (name, namespace string) {
	node := findSnapshotNode(snap, serviceID)
	if node != nil {
		return node.Name, node.Namespace
	}
	return serviceID, ""
}

// --- impacted paths ---

// buildFailureImpactedPaths returns the set of service communication paths that are
// disrupted when the target service shuts down. It emits:
//   - 1-hop caller → target paths (callers lose their connection)
//   - 1-hop target → downstream paths (downstream loses its upstream feed)
//   - 2-hop caller → target → downstream cross-paths (end-to-end call chains are severed)
//
// Cross-paths are capped at maxCrossPaths to avoid quadratic output on highly-connected targets.
func buildFailureImpactedPaths(
	targetID string,
	incomingEdges []SnapshotServiceEdge,
	outgoingEdges []SnapshotServiceEdge,
) []ImpactedPath {
	const maxCrossPaths = 20

	var paths []ImpactedPath

	for _, e := range incomingEdges {
		paths = append(paths, ImpactedPath{Path: []string{e.SourceServiceID, targetID}})
	}

	for _, e := range outgoingEdges {
		paths = append(paths, ImpactedPath{Path: []string{targetID, e.TargetServiceID}})
	}

	crossCount := 0
	for _, ie := range incomingEdges {
		if crossCount >= maxCrossPaths {
			break
		}
		for _, oe := range outgoingEdges {
			if crossCount >= maxCrossPaths {
				break
			}
			paths = append(paths, ImpactedPath{
				Path: []string{ie.SourceServiceID, targetID, oe.TargetServiceID},
			})
			crossCount++
		}
	}

	return paths
}

// --- before/after values and assumptions ---

// buildFailureBeforeAfterValues computes deterministic before/after estimates for the
// failure scenario. All values derive from snapshot edge data using explicit formulas.
// Three field references are emitted:
//   - failure.target.incoming_rps  (total RPS arriving at target; drops to 0 on shutdown)
//   - failure.target.error_rate    (aggregate error rate; rises to 1.0 on shutdown)
//   - failure.target.avg_p95_ms    (average P95 latency across incoming edges; nil after shutdown)
func buildFailureBeforeAfterValues(
	targetID string,
	incomingEdges []SnapshotServiceEdge,
	evidence EvidenceResolverResult,
) ([]BeforeAfterValue, []SimulationAssumption) {
	var totalRPS, weightedErrorRate, p95Sum float64
	var p95Count int
	evidenceSource := string(EvidenceSourceLiveServiceGraph)

	for _, e := range incomingEdges {
		totalRPS += e.RateRPS
		weightedErrorRate += e.ErrorRate * e.RateRPS
		if e.P95Ms != nil {
			p95Sum += *e.P95Ms
			p95Count++
		}
	}

	// Weighted average error rate (or 0 if no traffic).
	var beforeErrorRate float64
	if totalRPS > 0 {
		beforeErrorRate = weightedErrorRate / totalRPS
	}

	// After shutdown: all incoming RPS is lost and error rate is 1.0 (all calls fail).
	afterRPS := 0.0
	afterErrorRate := 1.0

	zero := 0.0
	one := 1.0

	var bavs []BeforeAfterValue

	// incoming_rps
	deltaRPS := afterRPS - totalRPS
	bavs = append(bavs, BeforeAfterValue{
		FieldRef:    "failure.target.incoming_rps",
		Description: "Total incoming request rate (RPS) to the target service",
		Unit:        "rps",
		BeforeValue: &totalRPS,
		AfterValue:  &zero,
		DeltaValue:  &deltaRPS,
	})

	// error_rate
	deltaErr := afterErrorRate - beforeErrorRate
	bavs = append(bavs, BeforeAfterValue{
		FieldRef:    "failure.target.error_rate",
		Description: "Aggregate error rate for calls to the target service (1.0 = 100% errors after shutdown)",
		Unit:        "ratio",
		BeforeValue: &beforeErrorRate,
		AfterValue:  &one,
		DeltaValue:  &deltaErr,
	})

	// avg_p95_ms (only when P95 data is available from snapshot edges)
	if p95Count > 0 {
		avgP95 := math.Round(p95Sum/float64(p95Count)*100) / 100
		bavs = append(bavs, BeforeAfterValue{
			FieldRef:    "failure.target.avg_p95_ms",
			Description: "Average P95 latency across incoming edges to the target (unavailable after shutdown)",
			Unit:        "ms",
			BeforeValue: &avgP95,
			AfterValue:  nil, // target is unreachable; latency is undefined
		})
	}

	// Determine which evidence source supplied edge data.
	if len(evidence.Sources) > 0 {
		evidenceSource = string(evidence.Sources[0])
	}

	assumptions := []SimulationAssumption{
		{
			Key:         "shutdown.complete_traffic_loss",
			Description: "All traffic directed at the target service is assumed lost immediately on shutdown; no partial degradation or graceful failover is modeled.",
			Source:      "engine_default",
		},
		{
			Key:         "shutdown.callers_error_rate_one",
			Description: "After shutdown, all callers of the target experience a 100% error rate (1.0) for requests to that service.",
			Source:      "engine_default",
		},
		{
			Key:         "edge_data.source",
			Description: fmt.Sprintf("Incoming RPS and error-rate values are taken from snapshot edge data sourced from %q.", evidenceSource),
			Source:      evidenceSource,
		},
	}

	return bavs, assumptions
}

// --- recommendation ---

// buildFailureShutdownRecommendation returns a deterministic operator recommendation
// for the failure / service shutdown scenario. The action and explanation reference
// the evidence sources used and the impacted service count.
func buildFailureShutdownRecommendation(
	ctx ExecutionContext,
	targetID string,
	impacted []ImpactedService,
	incomingEdges []SnapshotServiceEdge,
) SimulationRecommendation {
	callerCount := 0
	for _, svc := range impacted {
		if svc.Role == "caller" {
			callerCount++
		}
	}
	downstreamCount := 0
	for _, svc := range impacted {
		if svc.Role == "downstream" {
			downstreamCount++
		}
	}

	evidenceLabel := string(EvidenceSourceLiveServiceGraph)
	if len(ctx.Evidence.Sources) > 0 {
		evidenceLabel = string(ctx.Evidence.Sources[0])
	}

	var action, explanation string

	if callerCount == 0 && downstreamCount == 0 {
		action = "no_action_needed"
		explanation = fmt.Sprintf(
			"Target service %q has no callers or downstream dependencies in the snapshot graph (evidence: %s, mode: %s, confidence: %s). "+
				"Shutdown has no detected blast radius; no mitigation action is required.",
			targetID, evidenceLabel, ctx.Evidence.Mode, ctx.Evidence.Confidence,
		)
	} else {
		action = "implement_circuit_breaker_and_failover"
		explanation = fmt.Sprintf(
			"Shutting down service %q impacts %d caller(s) and %d downstream service(s) (evidence: %s, mode: %s, confidence: %s). "+
				"Implement circuit breakers on all %d caller(s) to prevent cascading failures, and establish failover or retry policies "+
				"for the %d affected downstream service(s). "+
				"Review snapshot-derived impacted paths and confirm with live cluster state before applying changes.",
			targetID, callerCount, downstreamCount, evidenceLabel, ctx.Evidence.Mode, ctx.Evidence.Confidence,
			callerCount, downstreamCount,
		)
	}

	return SimulationRecommendation{
		Action:      action,
		Explanation: explanation,
	}
}
