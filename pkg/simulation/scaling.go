package simulation

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"predictive-analysis-engine/pkg/clients/graph"
	"predictive-analysis-engine/pkg/config"
)

func SimulateScaling(ctx context.Context, client *graph.Client, cfg *config.Config, req ScalingSimulationRequest) (*ScalingSimulationResult, error) {

	maxDepth := req.MaxDepth
	if maxDepth == 0 {
		maxDepth = cfg.Simulation.MaxTraversalDepth
	}

	if maxDepth < 1 || maxDepth > 3 {
		return nil, fmt.Errorf("maxDepth must be integer 1, 2, or 3. Got: %d", maxDepth)
	}

	latencyMetric := req.LatencyMetric
	if latencyMetric == "" {
		latencyMetric = cfg.Simulation.DefaultLatencyMetric
	}
	if latencyMetric != "p50" && latencyMetric != "p95" && latencyMetric != "p99" {
		return nil, fmt.Errorf("Invalid latencyMetric: %s", latencyMetric)
	}

	if req.CurrentPods <= 0 {
		return nil, fmt.Errorf("currentPods must be a positive integer. Got: %d", req.CurrentPods)
	}
	if req.NewPods <= 0 {
		return nil, fmt.Errorf("newPods must be a positive integer. Got: %d", req.NewPods)
	}

	modelType := cfg.Simulation.ScalingModel
	alpha := cfg.Simulation.ScalingAlpha
	if req.Model != nil {
		if req.Model.Type != "" {
			modelType = req.Model.Type
		}
		if req.Model.Alpha != nil {
			alpha = *req.Model.Alpha
		}
	}
	if alpha < 0 || alpha > 1 {
		return nil, fmt.Errorf("alpha must be between 0 and 1")
	}

	neighborhood, err := client.GetNeighborhood(ctx, req.ServiceId, maxDepth)
	if err != nil {
		return nil, err
	}
	snapshot := buildSnapshot(neighborhood)

	targetKey := snapshot.TargetKey
	if targetKey == "" {
		targetKey = req.ServiceId
	}
	targetNode, ok := snapshot.Nodes[targetKey]
	if !ok {
		return nil, fmt.Errorf("Service not found: %s", req.ServiceId)
	}
	targetOut := nodeToOutRef(targetNode, targetKey)

	incomingEdges := snapshot.IncomingEdges[targetKey]
	var baseLat float64
	var totalWeighted, totalRate float64
	hasBaseData := false

	for _, edge := range incomingEdges {
		if edge.Rate <= 0 {
			continue
		}
		lat := getEdgeLatency(edge, latencyMetric)
		if lat != nil {
			totalWeighted += edge.Rate * *lat
			totalRate += edge.Rate
		}
	}
	if totalRate > 0 {
		baseLat = totalWeighted / totalRate
		hasBaseData = true
	}

	var newLat float64
	adjustedLatencies := make(map[string]float64)

	if hasBaseData {
		if modelType == "bounded_sqrt" {
			newLat = applyBoundedSqrtScaling(baseLat, req.CurrentPods, req.NewPods, alpha, cfg.Simulation.MinLatencyFactor)
		} else if modelType == "linear" {
			newLat = applyLinearScaling(baseLat, req.CurrentPods, req.NewPods)
		} else {
			return nil, fmt.Errorf("Unknown scaling model: %s", modelType)
		}
		adjustedLatencies[targetKey] = newLat
	}

	affectedCallers := []AffectedCallerScaling{}
	for nodeId, nodeData := range snapshot.Nodes {
		if nodeId == targetKey {
			continue
		}
		outEdges := snapshot.OutgoingEdges[nodeId]
		if len(outEdges) == 0 {
			continue
		}

		beforeMs := computeWeightedMeanLatency(outEdges, latencyMetric, nil)
		afterMs := computeWeightedMeanLatency(outEdges, latencyMetric, adjustedLatencies)

		var deltaMs *float64
		if beforeMs != nil && afterMs != nil {
			d := *afterMs - *beforeMs
			deltaMs = &d
		}

		dist := computeHopDistance(snapshot, nodeId, targetKey)
		hopDist := 0
		if dist != -1 {
			hopDist = dist
		}

		ns, n := parseServiceRef(nodeId)
		if nodeData.Name != "" {
			n = nodeData.Name
		}
		if nodeData.Namespace != "" {
			ns = nodeData.Namespace
		}

		affectedCallers = append(affectedCallers, AffectedCallerScaling{
			ServiceId:   nodeId,
			Name:        n,
			Namespace:   ns,
			HopDistance: hopDist,
			BeforeMs:    beforeMs,
			AfterMs:     afterMs,
			DeltaMs:     deltaMs,
		})
	}

	sort.Slice(affectedCallers, func(i, j int) bool {
		d1 := affectedCallers[i].DeltaMs
		d2 := affectedCallers[j].DeltaMs
		if d1 == nil {
			return false
		}
		if d2 == nil {
			return true
		}
		return math.Abs(*d1) > math.Abs(*d2)
	})

	maxPaths := cfg.Simulation.MaxPathsReturned
	topPaths := FindTopPathsToTarget(snapshot, targetKey, maxDepth, maxPaths)

	affectedPaths := []AffectedPathScaling{}
	callerBestPath := make(map[string]AffectedPathScaling)

	for _, p := range topPaths {
		pathIds := p.Path
		var beforeSum, afterSum float64
		hasIncomplete := false

		for i := 0; i < len(pathIds)-1; i++ {
			src := pathIds[i]
			tgt := pathIds[i+1]

			var edge *Edge
			if edges, ok := snapshot.OutgoingEdges[src]; ok {
				for _, e := range edges {
					if e.Target == tgt {
						edge = e
						break
					}
				}
			}

			if edge == nil {
				hasIncomplete = true
				break
			}
			lat := getEdgeLatency(edge, latencyMetric)
			if lat == nil {
				hasIncomplete = true
				break
			}

			beforeSum += *lat

			if adj, ok := adjustedLatencies[tgt]; ok {
				afterSum += adj
			} else {
				afterSum += *lat
			}
		}

		var pmBefore, pmAfter, pmDelta *float64
		if !hasIncomplete {
			b := beforeSum
			a := afterSum
			d := a - b
			pmBefore, pmAfter, pmDelta = &b, &a, &d
		}

		ap := AffectedPathScaling{
			Path:           pathIds,
			PathRps:        p.PathRps,
			BeforeMs:       pmBefore,
			AfterMs:        pmAfter,
			DeltaMs:        pmDelta,
			IncompleteData: hasIncomplete,
		}
		affectedPaths = append(affectedPaths, ap)

		startNode := pathIds[0]
		if currBest, exists := callerBestPath[startNode]; !exists || ap.PathRps > currBest.PathRps {
			callerBestPath[startNode] = ap
		}
	}

	sort.Slice(affectedPaths, func(i, j int) bool {
		d1 := affectedPaths[i].DeltaMs
		d2 := affectedPaths[j].DeltaMs
		if d1 == nil {
			return false
		}
		if d2 == nil {
			return true
		}
		return math.Abs(*d1) > math.Abs(*d2)
	})

	for i := range affectedCallers {
		c := &affectedCallers[i]
		if best, ok := callerBestPath[c.ServiceId]; ok && best.DeltaMs != nil {
			c.EndToEndBeforeMs = best.BeforeMs
			c.EndToEndAfterMs = best.AfterMs
			c.EndToEndDeltaMs = best.DeltaMs
			c.ViaPath = best.Path
		}
	}

	confidence := "high"
	healthRes, _ := client.CheckHealth(ctx)
	var df *DataFreshness
	if healthRes != nil {
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

	scalingDirection := "none"
	if req.NewPods > req.CurrentPods {
		scalingDirection = "up"
	} else if req.NewPods < req.CurrentPods {
		scalingDirection = "down"
	}

	var pBaseline, pProjected, pDelta *float64
	if hasBaseData {
		pBaseline = &baseLat
		pProjected = &newLat
		d := newLat - baseLat
		pDelta = &d
	}

	if targetOut.Namespace == "default" {
		targetOut.ServiceId = targetOut.Name
	}

	result := &ScalingSimulationResult{
		Target: targetOut,
		Neighborhood: NeighborhoodMeta{
			Description:  "k-hop upstream subgraph around target (not full graph)",
			ServiceCount: len(snapshot.Nodes),
			EdgeCount:    len(snapshot.Edges),
			DepthUsed:    maxDepth,
			GeneratedAt:  time.Now().Format(time.RFC3339),
		},
		DataFreshness:    df,
		Confidence:       confidence,
		LatencyMetric:    latencyMetric,
		ScalingModel:     ScalingModel{Type: modelType, Alpha: &alpha},
		CurrentPods:      req.CurrentPods,
		NewPods:          req.NewPods,
		ScalingDirection: scalingDirection,
		LatencyEstimate: ScalingLatencyEstimate{
			Description: "Rate-weighted mean of incoming edge latency to target",
			BaselineMs:  pBaseline,
			ProjectedMs: pProjected,
			DeltaMs:     pDelta,
			Unit:        "milliseconds",
		},
		AffectedCallers: AffectedCallersList{
			Description: "Edge-level impact: deltaMs is change in this caller's direct outgoing edge latency. endToEndDeltaMs is cumulative path latency change.",
			Items:       affectedCallers,
		},
		AffectedPaths:   affectedPaths,
		Recommendations: []FailureRecommendation{},
	}

	if len(result.AffectedCallers.Items) > cfg.Simulation.MaxPathsReturned {
		result.AffectedCallers.Items = result.AffectedCallers.Items[:cfg.Simulation.MaxPathsReturned]
	}

	directionWord := "at same level"
	if scalingDirection == "up" {
		directionWord = "up"
	}
	if scalingDirection == "down" {
		directionWord = "down"
	}

	callersCount := len(result.AffectedCallers.Items)
	pathsCount := len(result.AffectedPaths)

	if hasBaseData {
		improvementWord := "maintains"
		delta := *pDelta
		if delta < 0 {
			improvementWord = "improves"
		}
		if delta > 0 {
			improvementWord = "degrades"
		}

		result.Explanation = fmt.Sprintf("Scaling %s %s from %d to %d pods %s latency by %.1fms (baseline: %.1fms → projected: %.1fms). %d upstream caller(s) affected across %d path(s).",
			targetOut.Name, directionWord, req.CurrentPods, req.NewPods, improvementWord, math.Abs(delta), baseLat, newLat, callersCount, pathsCount)
	} else {
		result.Explanation = fmt.Sprintf("Scaling %s %s from %d to %d pods. Latency impact unknown due to missing edge metrics. %d upstream caller(s) identified across %d path(s).",
			targetOut.Name, directionWord, req.CurrentPods, req.NewPods, callersCount, pathsCount)
	}

	incompleteCount := 0
	for _, p := range result.AffectedPaths {
		if p.IncompleteData {
			incompleteCount++
		}
	}
	if incompleteCount > 0 {
		result.Warnings = []string{
			fmt.Sprintf("%d of %d path(s) have incomplete latency data (missing edge metrics). Results may be partial.", incompleteCount, pathsCount),
		}
	}

	recommendations := []FailureRecommendation{}

	if scalingDirection == "up" {
		isSmallBenefit := false
		if !hasBaseData {
			isSmallBenefit = true
		} else {
			benefit := math.Abs(*pDelta)
			if benefit < 10.0 {
				isSmallBenefit = true
			}
		}

		if isSmallBenefit {
			recommendations = append(recommendations, FailureRecommendation{
				Type:     "cost-efficiency",
				Priority: "medium",
				Target:   targetOut.Name,
				Reason:   fmt.Sprintf("Scaling from %d to %d shows minimal latency benefit", req.CurrentPods, req.NewPods),
				Action:   fmt.Sprintf("Review if additional pods for %s are cost-effective; bottleneck may be elsewhere", targetOut.Name),
			})
		}
	}

	result.Recommendations = recommendations

	return result, nil
}

func applyBoundedSqrtScaling(baseLatency float64, currentPods, newPods int, alpha, minLatencyFactor float64) float64 {
	ratio := float64(newPods) / float64(currentPods)
	improvement := 1.0 / math.Sqrt(ratio)
	newLatency := baseLatency * (alpha + (1.0-alpha)*improvement)

	minLatency := baseLatency * minLatencyFactor
	return math.Max(newLatency, minLatency)
}

func applyLinearScaling(baseLatency float64, currentPods, newPods int) float64 {
	return baseLatency * (float64(currentPods) / float64(newPods))
}

func computeWeightedMeanLatency(edges []*Edge, metric string, adjusted map[string]float64) *float64 {
	var totalWeighted, totalRate float64

	for _, edge := range edges {
		rate := edge.Rate
		if rate <= 0 {
			continue
		}

		var lat float64

		if adjusted != nil {
			if val, ok := adjusted[edge.Target]; ok {
				lat = val
				goto Accumulate
			}
		}

		if l := getEdgeLatency(edge, metric); l != nil {
			lat = *l
		} else {
			return nil
		}

	Accumulate:
		totalWeighted += rate * lat
		totalRate += rate
	}

	if totalRate == 0 {
		return nil
	}
	res := totalWeighted / totalRate
	return &res
}

func getEdgeLatency(edge *Edge, metric string) *float64 {

	switch metric {
	case "p50":
		return edge.P50
	case "p95":
		return edge.P95
	case "p99":
		return edge.P99
	}
	return nil
}

func computeHopDistance(snapshot *GraphSnapshot, sourceId, targetId string) int {
	if sourceId == targetId {
		return 0
	}

	visited := make(map[string]bool)
	type Item struct {
		id   string
		dist int
	}
	queue := []Item{{sourceId, 0}}
	visited[sourceId] = true

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		edges := snapshot.OutgoingEdges[curr.id]
		for _, e := range edges {
			if e.Target == targetId {
				return curr.dist + 1
			}
			if !visited[e.Target] {
				visited[e.Target] = true
				queue = append(queue, Item{e.Target, curr.dist + 1})
			}
		}
	}
	return -1
}
