package simulation

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"predictive-analysis-engine/pkg/clients/graph"
)

// SimulateFailure evaluates blast radius and traffic impact for a target service failure.
func SimulateFailure(ctx context.Context, client *graph.Client, req FailureSimulationRequest) (*FailureSimulationResult, error) {
	maxDepth := req.Depth
	if req.MaxDepth > 0 {
		maxDepth = req.MaxDepth
	}

	if maxDepth < 1 {
		maxDepth = 1
	}

	if maxDepth > 3 {
		return nil, fmt.Errorf("maxDepth > 3 not supported. Got: %d", maxDepth)
	}

	normalizedServiceID, graphLookupKey := normalizeServiceIdentifier(req.ServiceId)
	neighborhood, err := client.GetNeighborhoodWithOptions(ctx, graphLookupKey, maxDepth, graph.NeighborhoodOptions{
		Direction: "both",
		MaxNodes:  200,
		MaxEdges:  400,
	})
	if err != nil {
		return nil, err
	}

	snapshot := buildSnapshot(neighborhood)

	targetKey := normalizedServiceID
	if _, ok := snapshot.Nodes[targetKey]; !ok {
		targetKey = snapshot.TargetKey
	}

	targetNode, ok := snapshot.Nodes[targetKey]
	if !ok {
		return nil, fmt.Errorf("Service not found: %s", req.ServiceId)
	}
	targetOut := nodeToOutRef(targetNode, targetKey)

	directCallers := snapshot.IncomingEdges[targetKey]
	callerMap := make(map[string]*AffectedCaller)

	for _, edge := range directCallers {
		id := edge.Source
		callerNode := snapshot.Nodes[id]
		callerOut := nodeToOutRef(callerNode, id)

		existing, exists := callerMap[id]
		if !exists {
			existing = &AffectedCaller{
				ServiceId: callerOut.ServiceId,
				Name:      callerOut.Name,
				Namespace: callerOut.Namespace,
			}
			callerMap[id] = existing
		}
		existing.LostTrafficRps += edge.Rate
		existing.EdgeErrorRate = math.Max(existing.EdgeErrorRate, edge.ErrorRate)
	}

	var affectedCallers []AffectedCaller
	for _, c := range callerMap {
		affectedCallers = append(affectedCallers, *c)
	}
	sort.Slice(affectedCallers, func(i, j int) bool {
		return affectedCallers[i].LostTrafficRps > affectedCallers[j].LostTrafficRps
	})

	criticalPaths := FindTopPathsToTarget(snapshot, targetKey, maxDepth, MaxPathsReturned)

	directCallees := snapshot.OutgoingEdges[targetKey]
	downstreamMap := make(map[string]*AffectedDownstream)

	for _, edge := range directCallees {
		calleeKey := edge.Target

		if calleeKey == "" || calleeKey == targetKey {
			continue
		}

		calleeNode := snapshot.Nodes[calleeKey]
		calleeOut := nodeToOutRef(calleeNode, calleeKey)

		existing, exists := downstreamMap[calleeKey]
		if !exists {
			existing = &AffectedDownstream{
				ServiceId: calleeOut.ServiceId,
				Name:      calleeOut.Name,
				Namespace: calleeOut.Namespace,
			}
			downstreamMap[calleeKey] = existing
		}
		existing.LostTrafficRps += edge.Rate
		existing.EdgeErrorRate = math.Max(existing.EdgeErrorRate, edge.ErrorRate)
	}

	var affectedDownstream []AffectedDownstream
	for _, d := range downstreamMap {
		affectedDownstream = append(affectedDownstream, *d)
	}
	sort.Slice(affectedDownstream, func(i, j int) bool {
		return affectedDownstream[i].LostTrafficRps > affectedDownstream[j].LostTrafficRps
	})

	entrypoints := pickEntrypoints(snapshot, targetKey)
	reachable := computeReachableNodes(snapshot, entrypoints, targetKey)
	lostByNode := estimateBoundaryLostTraffic(snapshot, reachable, targetKey)

	var unreachableServices []UnreachableService
	for k, n := range snapshot.Nodes {
		if k == targetKey {
			continue
		}
		if !reachable[k] {
			out := nodeToOutRef(n, k)
			loss := lostByNode[k]
			unreachableServices = append(unreachableServices, UnreachableService{
				ServiceId:                out.ServiceId,
				Name:                     out.Name,
				Namespace:                out.Namespace,
				LostTrafficRps:           loss.LostTotalRps,
				LostFromTargetRps:        loss.LostFromTargetRps,
				LostFromReachableCutsRps: loss.LostFromReachableCutsRps,
			})
		}
	}
	sort.Slice(unreachableServices, func(i, j int) bool {
		return unreachableServices[i].LostTrafficRps > unreachableServices[j].LostTrafficRps
	})

	totalLostTrafficRps := 0.0
	for _, c := range affectedCallers {
		totalLostTrafficRps += c.LostTrafficRps
	}

	if affectedCallers == nil {
		affectedCallers = []AffectedCaller{}
	}
	if affectedDownstream == nil {
		affectedDownstream = []AffectedDownstream{}
	}
	if unreachableServices == nil {
		unreachableServices = []UnreachableService{}
	}
	if criticalPaths == nil {
		criticalPaths = []BrokenPath{}
	}

	confidence := "high"

	healthRes, _ := client.CheckHealth(ctx)
	var df *DataFreshness
	if healthRes != nil {
		confidence = "high"
		if healthRes.Stale {
			confidence = "low"
		}
		df = &DataFreshness{
			Source:                "graph-engine",
			Stale:                 healthRes.Stale,
			LastUpdatedSecondsAgo: healthRes.LastUpdatedSecondsAgo,
			WindowMinutes:         healthRes.WindowMinutes,
		}
	}

	explanation := fmt.Sprintf("If %s fails, %d upstream caller(s) lose direct access, %d downstream service(s) lose traffic from this target, and %d service(s) may become unreachable within the %d-hop neighborhood.",
		targetOut.Name, len(affectedCallers), len(affectedDownstream), len(unreachableServices), maxDepth)

	result := &FailureSimulationResult{
		Target: targetOut,
		Neighborhood: NeighborhoodMeta{
			Description:  "k-hop neighborhood subgraph around target (not full graph)",
			ServiceCount: len(snapshot.Nodes),
			EdgeCount:    len(snapshot.Edges),
			DepthUsed:    maxDepth,
			GeneratedAt:  time.Now().Format(time.RFC3339),
		},
		RequestNormalized: RequestNormalization{
			ServiceId:      normalizedServiceID,
			GraphLookupKey: graphLookupKey,
			DepthUsed:      maxDepth,
		},
		DataFreshness:       df,
		Confidence:          confidence,
		Explanation:         explanation,
		AffectedCallers:     affectedCallers,
		AffectedDownstream:  affectedDownstream,
		UnreachableServices: unreachableServices,
		CriticalPaths:       criticalPaths,
		ImpactGraph:         buildFailureImpactGraph(snapshot, targetKey, affectedCallers, affectedDownstream, unreachableServices, criticalPaths),
		TotalLostTrafficRps: totalLostTrafficRps,
		SourceMode:          "live",
	}

	result.Recommendations = GenerateFailureRecommendations(result)
	if result.Recommendations == nil {
		result.Recommendations = []FailureRecommendation{}
	}

	return result, nil
}

func buildSnapshot(resp *graph.NeighborhoodResponse) *GraphSnapshot {
	nodes := make(map[string]*Node)
	edges := make([]*Edge, 0)
	incoming := make(map[string][]*Edge)
	outgoing := make(map[string][]*Edge)

	nameToID := make(map[string]string)

	for _, n := range resp.Nodes {
		key := strings.TrimSpace(n.ServiceId)
		if key == "" {
			key = toCanonicalServiceId(n.Namespace, n.Name)
		}
		nodes[key] = &Node{Name: n.Name, Namespace: n.Namespace}

		if n.Name != "" {
			nameToID[n.Name] = key
		}

		nameToID[key] = key
	}

	for _, e := range resp.Edges {
		srcRaw := firstNonEmpty(e.Source, e.From)
		tgtRaw := firstNonEmpty(e.Target, e.To)
		srcID := resolveEdgeEndpointID(srcRaw, nameToID)
		tgtID := resolveEdgeEndpointID(tgtRaw, nameToID)

		edge := &Edge{
			Source:    srcID,
			Target:    tgtID,
			Rate:      e.Rate,
			ErrorRate: e.ErrorRate,
			P50:       &e.P50,
			P95:       &e.P95,
			P99:       &e.P99,
		}
		edges = append(edges, edge)
		incoming[edge.Target] = append(incoming[edge.Target], edge)
		outgoing[edge.Source] = append(outgoing[edge.Source], edge)
	}

	targetKey := resp.Center
	if resp.CenterRef != nil && strings.TrimSpace(resp.CenterRef.ServiceId) != "" {
		targetKey = resp.CenterRef.ServiceId
	}
	if mapped, ok := nameToID[targetKey]; ok {
		targetKey = mapped
	} else {
		targetKey = toCanonicalServiceId("default", targetKey)
	}

	return &GraphSnapshot{
		Nodes:         nodes,
		IncomingEdges: incoming,
		OutgoingEdges: outgoing,
		Edges:         edges,
		TargetKey:     targetKey,
	}
}

func parseServiceRef(idOrName string) (namespace, name string) {
	if idOrName == "" {
		return "default", ""
	}
	if idx := strings.Index(idOrName, ":"); idx > 0 {
		return idOrName[:idx], idOrName[idx+1:]
	}
	return "default", idOrName
}

func toCanonicalServiceId(namespace, name string) string {
	if namespace == "" {
		namespace = "default"
	}
	return fmt.Sprintf("%s:%s", namespace, name)
}

func normalizeServiceIdentifier(raw string) (canonical string, graphLookupKey string) {
	ns, name := parseServiceRef(strings.TrimSpace(raw))
	if name == "" {
		return toCanonicalServiceId("default", raw), raw
	}
	return toCanonicalServiceId(ns, name), name
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func resolveEdgeEndpointID(raw string, nameToID map[string]string) string {
	if mapped, ok := nameToID[raw]; ok {
		return mapped
	}
	if strings.Contains(raw, ":") {
		return raw
	}
	return toCanonicalServiceId("default", raw)
}

func nodeToOutRef(node *Node, fallbackKey string) ServiceRef {
	ns, n := parseServiceRef(fallbackKey)
	if node != nil {
		if node.Name != "" {
			n = node.Name
		}
		if node.Namespace != "" {
			ns = node.Namespace
		}
	}
	return ServiceRef{
		ServiceId: toCanonicalServiceId(ns, n),
		Name:      n,
		Namespace: ns,
	}
}

func pickEntrypoints(snapshot *GraphSnapshot, blockedKey string) []string {
	var entrypoints []string
	for k := range snapshot.Nodes {
		if k == blockedKey {
			continue
		}

		if len(snapshot.IncomingEdges[k]) == 0 {
			entrypoints = append(entrypoints, k)
		}
	}

	if len(entrypoints) == 0 {
		for k := range snapshot.Nodes {
			if k != blockedKey {
				entrypoints = append(entrypoints, k)
			}
		}
	}
	return entrypoints
}

func computeReachableNodes(snapshot *GraphSnapshot, entrypoints []string, blockedKey string) map[string]bool {
	visited := make(map[string]bool)
	queue := make([]string, 0, len(entrypoints))

	for _, e := range entrypoints {
		if e == "" || e == blockedKey {
			continue
		}
		visited[e] = true
		queue = append(queue, e)
	}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		outs := snapshot.OutgoingEdges[curr]
		for _, edge := range outs {
			nxt := edge.Target
			if nxt == "" || nxt == blockedKey {
				continue
			}
			if _, exists := snapshot.Nodes[nxt]; !exists {
				continue
			}
			if visited[nxt] {
				continue
			}
			visited[nxt] = true
			queue = append(queue, nxt)
		}
	}
	return visited
}

type trafficLoss struct {
	LostFromTargetRps        float64
	LostFromReachableCutsRps float64
	LostTotalRps             float64
}

func estimateBoundaryLostTraffic(snapshot *GraphSnapshot, reachable map[string]bool, blockedKey string) map[string]trafficLoss {
	lostByNode := make(map[string]trafficLoss)

	for k := range snapshot.Nodes {
		if k == blockedKey || reachable[k] {
			continue
		}

		incoming := snapshot.IncomingEdges[k]
		var lTraffic, lCuts float64

		for _, e := range incoming {
			if e.Source == blockedKey {
				lTraffic += e.Rate
				continue
			}
			if reachable[e.Source] {
				lCuts += e.Rate
			}
		}

		lostByNode[k] = trafficLoss{
			LostFromTargetRps:        lTraffic,
			LostFromReachableCutsRps: lCuts,
			LostTotalRps:             lTraffic + lCuts,
		}
	}
	return lostByNode
}

func buildFailureImpactGraph(
	snapshot *GraphSnapshot,
	targetKey string,
	affectedCallers []AffectedCaller,
	affectedDownstream []AffectedDownstream,
	unreachableServices []UnreachableService,
	criticalPaths []BrokenPath,
) ImpactGraph {
	nodeStatus := make(map[string]string)
	nodeStatus[targetKey] = "target_failed"

	brokenNodes := make(map[string]bool)
	for _, caller := range affectedCallers {
		if caller.ServiceId != "" {
			brokenNodes[caller.ServiceId] = true
		}
	}
	for _, downstream := range affectedDownstream {
		if downstream.ServiceId != "" {
			brokenNodes[downstream.ServiceId] = true
		}
	}
	for _, path := range criticalPaths {
		for _, serviceID := range path.Path {
			if serviceID != targetKey {
				brokenNodes[serviceID] = true
			}
		}
	}
	for serviceID := range brokenNodes {
		if nodeStatus[serviceID] == "" {
			nodeStatus[serviceID] = "broken"
		}
	}

	unreachable := make(map[string]bool)
	for _, service := range unreachableServices {
		if service.ServiceId != "" {
			unreachable[service.ServiceId] = true
			nodeStatus[service.ServiceId] = "unreachable"
		}
	}

	nodeKeys := make([]string, 0, len(snapshot.Nodes))
	for key := range snapshot.Nodes {
		nodeKeys = append(nodeKeys, key)
	}
	sort.Strings(nodeKeys)

	nodes := make([]ImpactGraphNode, 0, len(nodeKeys))
	for _, key := range nodeKeys {
		ref := nodeToOutRef(snapshot.Nodes[key], key)
		status := nodeStatus[key]
		if status == "" {
			status = "normal"
		}
		nodes = append(nodes, ImpactGraphNode{
			ServiceId: ref.ServiceId,
			Name:      ref.Name,
			Namespace: ref.Namespace,
			Status:    status,
		})
	}

	edges := make([]ImpactGraphEdge, 0, len(snapshot.Edges))
	for _, edge := range snapshot.Edges {
		status := "normal"
		if edge.Source == targetKey || edge.Target == targetKey || nodeStatus[edge.Source] == "broken" || nodeStatus[edge.Target] == "broken" {
			status = "broken"
		}
		if unreachable[edge.Source] || unreachable[edge.Target] {
			status = "unreachable"
		}
		edges = append(edges, ImpactGraphEdge{
			Source:    edge.Source,
			Target:    edge.Target,
			Rate:      edge.Rate,
			ErrorRate: edge.ErrorRate,
			P95:       edge.P95,
			Status:    status,
		})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source == edges[j].Source {
			return edges[i].Target < edges[j].Target
		}
		return edges[i].Source < edges[j].Source
	})

	return ImpactGraph{
		Nodes: nodes,
		Edges: edges,
	}
}
