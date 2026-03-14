package simulation

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"predictive-analysis-engine/pkg/clients/graph"
	"predictive-analysis-engine/pkg/config"
)

type rawAddNode struct {
	Name            string
	CPUUsagePercent float64
	CPUCores        float64
	RAMUsedMB       float64
	RAMTotalMB      float64
}

type aggregatedEdgeTelemetry struct {
	RPS       float64
	ErrorRate float64
	P95       float64
}

// SimulateAddService evaluates capacity and placement feasibility for a new service.
func SimulateAddService(ctx context.Context, client *graph.Client, cfg *config.Config, req AddSimulationRequest) (*AddSimulationResult, error) {
	if req.ServiceName == "" {
		req.ServiceName = "new-service"
	}
	if req.CPURequest == 0 {
		req.CPURequest = 0.1
	}
	if req.RAMRequest == 0 {
		req.RAMRequest = 128
	}
	if req.Replicas == 0 {
		req.Replicas = 1
	}

	req.TargetNodeName = strings.TrimSpace(req.TargetNodeName)
	req.ServiceName = strings.TrimSpace(req.ServiceName)

	if req.CPURequest <= 0 || req.RAMRequest <= 0 || req.Replicas <= 0 {
		return nil, fmt.Errorf("Invalid resource requests: cpu, ram, and replicas must be positive")
	}

	services, err := client.GetServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("Failed to fetch cluster state: %w", err)
	}

	metricsSnapshot, metricsErr := client.GetMetricsSnapshot(ctx)
	if metricsErr != nil {
		metricsSnapshot = nil
	}

	rawNodes, infraErr := collectRawAddNodes(ctx, client, services)
	if infraErr != nil {
		return nil, infraErr
	}

	rankedNodes := analyzeAddNodes(rawNodes, req)
	totalCapacityPods := 0
	for _, node := range rankedNodes {
		totalCapacityPods += node.MaxPods
	}

	distribution, remainingReplicas := buildPlacementDistribution(rankedNodes, req.TargetNodeName, req.Replicas)
	success := remainingReplicas == 0

	selectedNodeFound := false
	selectedNodeSuitable := false
	for _, node := range rankedNodes {
		if node.NodeName == req.TargetNodeName {
			selectedNodeFound = true
			selectedNodeSuitable = node.Suitable
			break
		}
	}

	recommendedNodeName := ""
	topSuitableNodeName := ""
	for _, node := range rankedNodes {
		if node.Suitable {
			topSuitableNodeName = node.NodeName
			break
		}
	}
	if topSuitableNodeName != "" && topSuitableNodeName != req.TargetNodeName {
		recommendedNodeName = topSuitableNodeName
	}

	dependencyAnalysis, riskAnalysis := analyzeDependencyChain(req.ServiceName, req.Dependencies, services, metricsSnapshot)
	recommendations := buildAddRecommendations(req, distribution, success, selectedNodeFound, selectedNodeSuitable, recommendedNodeName, remainingReplicas, riskAnalysis)
	explanation := buildAddExplanation(req, success, selectedNodeFound, selectedNodeSuitable, recommendedNodeName, totalCapacityPods)

	return &AddSimulationResult{
		TargetServiceName:    req.ServiceName,
		Success:              success,
		Confidence:           "high",
		Explanation:          explanation,
		TotalCapacityPods:    totalCapacityPods,
		SelectedNodeName:     req.TargetNodeName,
		SelectedNodeSuitable: selectedNodeSuitable,
		RecommendedNodeName:  recommendedNodeName,
		SuitableNodes:        orderNodesForDisplay(rankedNodes, req.TargetNodeName),
		AggregateResources:   buildAggregateResources(rawNodes, cfg.Simulation.SharedHostResources),
		DependencyAnalysis:   dependencyAnalysis,
		RiskAnalysis:         riskAnalysis,
		Recommendations:      recommendations,
		Recommendation: &LegacyRecommendation{
			ServiceName:  req.ServiceName,
			CPURequest:   req.CPURequest,
			RAMRequest:   req.RAMRequest,
			Distribution: distribution,
		},
	}, nil
}

func collectRawAddNodes(ctx context.Context, client *graph.Client, services []graph.ServiceInfo) (map[string]*rawAddNode, error) {
	rawNodes := make(map[string]*rawAddNode)

	for _, svc := range services {
		for _, node := range svc.Placement.Nodes {
			if node.Node == "" {
				continue
			}
			if _, exists := rawNodes[node.Node]; !exists {
				rawNodes[node.Node] = &rawAddNode{
					Name:            node.Node,
					CPUUsagePercent: node.Resources.CPU.UsagePercent,
					CPUCores:        node.Resources.CPU.Cores,
					RAMUsedMB:       node.Resources.RAM.UsedMB,
					RAMTotalMB:      node.Resources.RAM.TotalMB,
				}
			}
		}
	}

	if len(rawNodes) == 0 {
		infraNodes, infraErr := client.GetNodes(ctx)
		if infraErr != nil {
			return nil, fmt.Errorf("Failed to fetch cluster state: %w", infraErr)
		}
		for _, node := range infraNodes {
			if node.Name == "" {
				continue
			}
			rawNodes[node.Name] = &rawAddNode{
				Name:            node.Name,
				CPUUsagePercent: node.Resources.CPU.UsagePercent,
				CPUCores:        node.Resources.CPU.Cores,
				RAMUsedMB:       node.Resources.RAM.UsedMB,
				RAMTotalMB:      node.Resources.RAM.TotalMB,
			}
		}
	}

	if len(rawNodes) == 0 {
		return nil, fmt.Errorf("No nodes found in cluster state. Cannot perform placement analysis.")
	}

	return rawNodes, nil
}

func analyzeAddNodes(rawNodes map[string]*rawAddNode, req AddSimulationRequest) []NodeCapacity {
	nodeAnalysis := make([]NodeCapacity, 0, len(rawNodes))

	for _, node := range rawNodes {
		cpuUsed := (node.CPUUsagePercent / 100.0) * node.CPUCores
		cpuAvail := round2(math.Max(0, node.CPUCores-cpuUsed))
		ramAvail := round2(math.Max(0, node.RAMTotalMB-node.RAMUsedMB))

		cpuFit := math.Floor(cpuAvail / req.CPURequest)
		ramFit := math.Floor(ramAvail / float64(req.RAMRequest))
		maxPods := int(math.Min(cpuFit, ramFit))
		if maxPods < 0 {
			maxPods = 0
		}

		projectedCPU := cpuAvail
		projectedRAM := ramAvail
		if maxPods > 0 {
			projectedCPU = round2(math.Max(0, cpuAvail-req.CPURequest))
			projectedRAM = round2(math.Max(0, ramAvail-float64(req.RAMRequest)))
		}

		canFit := maxPods > 0
		score := computeNodeScore(canFit, cpuAvail, ramAvail, node.CPUCores, node.RAMTotalMB, req)

		nodeAnalysis = append(nodeAnalysis, NodeCapacity{
			Node:               node.Name,
			NodeName:           node.Name,
			CPUAvailable:       cpuAvail,
			RAMAvailableMB:     ramAvail,
			CPUTotal:           node.CPUCores,
			RAMTotalMB:         round2(node.RAMTotalMB),
			CanFit:             canFit,
			MaxPods:            maxPods,
			Score:              score,
			Suitable:           canFit,
			AvailableCPU:       cpuAvail,
			AvailableRAM:       ramAvail,
			ProjectedCPUFree:   projectedCPU,
			ProjectedRAMFreeMB: projectedRAM,
			Preferred:          node.Name == req.TargetNodeName,
			Reason:             buildNodeReason(cpuFit, ramFit, req),
		})
	}

	sort.SliceStable(nodeAnalysis, func(i, j int) bool {
		if nodeAnalysis[i].Score == nodeAnalysis[j].Score {
			if nodeAnalysis[i].Suitable == nodeAnalysis[j].Suitable {
				return nodeAnalysis[i].NodeName < nodeAnalysis[j].NodeName
			}
			return nodeAnalysis[i].Suitable && !nodeAnalysis[j].Suitable
		}
		return nodeAnalysis[i].Score > nodeAnalysis[j].Score
	})

	for i := range nodeAnalysis {
		nodeAnalysis[i].Rank = i + 1
	}

	return nodeAnalysis
}

func computeNodeScore(canFit bool, cpuAvail, ramAvail, cpuTotal, ramTotal float64, req AddSimulationRequest) int {
	if canFit {
		projectedCPU := math.Max(0, cpuAvail-req.CPURequest)
		projectedRAM := math.Max(0, ramAvail-float64(req.RAMRequest))

		cpuHeadroom := 0.0
		if cpuTotal > 0 {
			cpuHeadroom = projectedCPU / cpuTotal
		}
		ramHeadroom := 0.0
		if ramTotal > 0 {
			ramHeadroom = projectedRAM / ramTotal
		}

		return int(math.Floor(50 + ((cpuHeadroom+ramHeadroom)/2.0)*50))
	}

	cpuFrac := math.Min(1, cpuAvail/req.CPURequest)
	ramFrac := math.Min(1, ramAvail/float64(req.RAMRequest))
	return int(math.Floor(((cpuFrac + ramFrac) / 2.0) * 40))
}

func buildNodeReason(cpuFit, ramFit float64, req AddSimulationRequest) string {
	if cpuFit >= 1 && ramFit >= 1 {
		return ""
	}
	if cpuFit < 1 && ramFit < 1 {
		return fmt.Sprintf("Needs %.2f CPU cores and %d MB RAM, but this node lacks both.", req.CPURequest, req.RAMRequest)
	}
	if cpuFit < 1 {
		return fmt.Sprintf("Needs %.2f CPU cores, but this node does not have enough free CPU.", req.CPURequest)
	}
	return fmt.Sprintf("Needs %d MB RAM, but this node does not have enough free memory.", req.RAMRequest)
}

func buildPlacementDistribution(rankedNodes []NodeCapacity, targetNodeName string, replicas int) ([]PlacementDistribution, int) {
	remainingReplicas := replicas
	distribution := make([]PlacementDistribution, 0, len(rankedNodes))

	for _, node := range orderNodesForPlacement(rankedNodes, targetNodeName) {
		if remainingReplicas <= 0 {
			break
		}
		if node.MaxPods <= 0 {
			continue
		}

		take := int(math.Min(float64(remainingReplicas), float64(node.MaxPods)))
		distribution = append(distribution, PlacementDistribution{
			Node:     node.Node,
			Replicas: take,
		})
		remainingReplicas -= take
	}

	return distribution, remainingReplicas
}

func orderNodesForPlacement(rankedNodes []NodeCapacity, targetNodeName string) []NodeCapacity {
	if targetNodeName == "" {
		return rankedNodes
	}

	ordered := make([]NodeCapacity, 0, len(rankedNodes))
	for _, node := range rankedNodes {
		if node.NodeName == targetNodeName {
			ordered = append(ordered, node)
			break
		}
	}
	for _, node := range rankedNodes {
		if node.NodeName == targetNodeName {
			continue
		}
		ordered = append(ordered, node)
	}
	return ordered
}

func orderNodesForDisplay(rankedNodes []NodeCapacity, targetNodeName string) []NodeCapacity {
	if targetNodeName == "" {
		return rankedNodes
	}

	selectedIdx := -1
	for i, node := range rankedNodes {
		if node.NodeName == targetNodeName {
			selectedIdx = i
			break
		}
	}
	if selectedIdx <= 0 {
		return rankedNodes
	}

	ordered := make([]NodeCapacity, 0, len(rankedNodes))
	ordered = append(ordered, rankedNodes[selectedIdx])
	ordered = append(ordered, rankedNodes[:selectedIdx]...)
	ordered = append(ordered, rankedNodes[selectedIdx+1:]...)
	return ordered
}

func buildAggregateResources(rawNodes map[string]*rawAddNode, sharedHostResources bool) AggregateResources {
	scope := "cluster"
	totalCPU := 0.0
	totalRAM := 0.0
	usedCPU := 0.0
	usedRAM := 0.0

	if sharedHostResources && len(rawNodes) > 1 {
		scope = "machine"
		for _, node := range rawNodes {
			totalCPU = math.Max(totalCPU, float64(node.CPUCores))
			totalRAM = math.Max(totalRAM, node.RAMTotalMB)
			usedCPU += (node.CPUUsagePercent / 100.0) * float64(node.CPUCores)
			usedRAM += node.RAMUsedMB
		}
	} else {
		for _, node := range rawNodes {
			totalCPU += float64(node.CPUCores)
			totalRAM += node.RAMTotalMB
			usedCPU += (node.CPUUsagePercent / 100.0) * float64(node.CPUCores)
			usedRAM += node.RAMUsedMB
		}
	}

	return AggregateResources{
		Scope:                      scope,
		NodeCount:                  len(rawNodes),
		TotalCPU:                   round2(totalCPU),
		UsedCPU:                    round2(usedCPU),
		AvailableCPU:               round2(math.Max(0, totalCPU-usedCPU)),
		TotalRAMMB:                 round2(totalRAM),
		UsedRAMMB:                  round2(usedRAM),
		AvailableRAMMB:             round2(math.Max(0, totalRAM-usedRAM)),
		SharedHostResourcesEnabled: sharedHostResources,
	}
}

func analyzeDependencyChain(
	serviceName string,
	dependencies []DependencyRef,
	services []graph.ServiceInfo,
	metricsSnapshot *graph.MetricsSnapshotResponse,
) (AddDependencyAnalysis, AddRiskAnalysis) {
	normalizedDeps := make([]string, 0, len(dependencies))
	for _, dep := range dependencies {
		serviceID := strings.TrimSpace(dep.ServiceId)
		if serviceID == "" {
			continue
		}
		normalizedDeps = append(normalizedDeps, serviceID)
	}

	analysis := AddDependencyAnalysis{
		Chain: append([]string{serviceName}, normalizedDeps...),
	}
	if len(normalizedDeps) == 0 {
		analysis.Summary = "No dependency chain declared."
		return analysis, AddRiskAnalysis{
			DependencyRisk: "low",
			Description:    "No dependencies declared.",
		}
	}

	servicesByID := make(map[string]graph.ServiceInfo, len(services))
	for _, svc := range services {
		servicesByID[canonicalServiceID(svc.Namespace, svc.Name)] = svc
	}

	edgeTelemetry := buildEdgeTelemetryMap(metricsSnapshot)

	var highReason string
	var mediumReason string

	for _, depID := range normalizedDeps {
		check := AddDependencyServiceCheck{
			ServiceId: depID,
			Exists:    false,
		}

		svc, exists := servicesByID[depID]
		if !exists {
			analysis.MissingServices = append(analysis.MissingServices, depID)
			if highReason == "" {
				highReason = fmt.Sprintf("Declared dependency %s is missing from the current cluster state.", depID)
			}
			analysis.ServiceChecks = append(analysis.ServiceChecks, check)
			continue
		}

		check.Exists = true
		if availabilityPct, ok := normalizeAvailabilityPct(svc.Availability); ok {
			check.AvailabilityPct = floatPtr(round2(availabilityPct))
			if availabilityPct < 90 && highReason == "" {
				highReason = fmt.Sprintf("Dependency %s availability is %.0f%%, below the 90%% threshold.", depID, availabilityPct)
			}
		}

		podCount := svc.PodCount
		check.PodCount = intPtr(podCount)
		if hasOnlyHighPressureNodes(svc) {
			check.OnlyHighPressureNodes = true
			if mediumReason == "" {
				mediumReason = fmt.Sprintf("Dependency %s is only running on heavily loaded nodes.", depID)
			}
		}

		analysis.ServiceChecks = append(analysis.ServiceChecks, check)
	}

	if len(normalizedDeps) > 3 && mediumReason == "" {
		mediumReason = fmt.Sprintf("Dependency chain length is %d, which increases rollout complexity.", len(normalizedDeps))
	}

	for i := 0; i < len(normalizedDeps)-1; i++ {
		sourceID := normalizedDeps[i]
		targetID := normalizedDeps[i+1]
		check := AddDependencyLinkCheck{
			SourceServiceId: sourceID,
			TargetServiceId: targetID,
			Observed:        false,
		}

		if telemetry, ok := edgeTelemetry[sourceID+"=>"+targetID]; ok {
			check.Observed = true
			check.RPS = floatPtr(round2(telemetry.RPS))
			check.ErrorRate = floatPtr(round4(telemetry.ErrorRate))
			check.P95 = floatPtr(round2(telemetry.P95))

			if telemetry.ErrorRate >= 0.02 && highReason == "" {
				highReason = fmt.Sprintf("Dependency link %s -> %s has %.2f%% errors.", sourceID, targetID, telemetry.ErrorRate*100)
			}
			if telemetry.P95 >= 250 && mediumReason == "" {
				mediumReason = fmt.Sprintf("Dependency link %s -> %s has p95 latency %.0f ms.", sourceID, targetID, telemetry.P95)
			}
		} else if mediumReason == "" {
			mediumReason = fmt.Sprintf("Dependency link %s -> %s is not observed in current telemetry.", sourceID, targetID)
		}

		analysis.LinkChecks = append(analysis.LinkChecks, check)
	}

	risk := "low"
	description := "Dependency chain validated against current graph."
	switch {
	case highReason != "":
		risk = "high"
		description = highReason
	case mediumReason != "":
		risk = "medium"
		description = mediumReason
	case len(normalizedDeps) == 1:
		description = "Dependency service verified in current graph."
	}

	analysis.Summary = buildDependencySummary(description, analysis)
	return analysis, AddRiskAnalysis{
		DependencyRisk: risk,
		Description:    description,
	}
}

func buildEdgeTelemetryMap(metricsSnapshot *graph.MetricsSnapshotResponse) map[string]aggregatedEdgeTelemetry {
	result := make(map[string]aggregatedEdgeTelemetry)
	if metricsSnapshot == nil {
		return result
	}

	for _, edge := range metricsSnapshot.Edges {
		namespace := strings.TrimSpace(edge.Namespace)
		if namespace == "" {
			namespace = "default"
		}
		key := canonicalServiceID(namespace, edge.From) + "=>" + canonicalServiceID(namespace, edge.To)
		current := result[key]
		current.RPS += edge.RPS
		current.ErrorRate = math.Max(current.ErrorRate, edge.ErrorRate)
		current.P95 = math.Max(current.P95, edge.P95)
		result[key] = current
	}

	return result
}

func normalizeAvailabilityPct(raw float64) (float64, bool) {
	if raw < 0 {
		return 0, false
	}
	if raw <= 1 {
		raw *= 100
	}
	if raw > 100 {
		raw = 100
	}
	return raw, true
}

func hasOnlyHighPressureNodes(service graph.ServiceInfo) bool {
	if len(service.Placement.Nodes) == 0 {
		return false
	}

	hasPlacement := false
	for _, node := range service.Placement.Nodes {
		if node.Node == "" {
			continue
		}
		hasPlacement = true

		cpuHot := node.Resources.CPU.UsagePercent >= 80
		ramHot := false
		if node.Resources.RAM.TotalMB > 0 {
			ramHot = (node.Resources.RAM.UsedMB/node.Resources.RAM.TotalMB)*100 >= 80
		}
		if !cpuHot && !ramHot {
			return false
		}
	}

	return hasPlacement
}

func buildDependencySummary(description string, analysis AddDependencyAnalysis) string {
	if len(analysis.Chain) <= 1 {
		return description
	}

	observedLinks := 0
	for _, check := range analysis.LinkChecks {
		if check.Observed {
			observedLinks++
		}
	}

	var builder strings.Builder
	builder.WriteString(description)
	builder.WriteString(" Chain: ")
	builder.WriteString(strings.Join(analysis.Chain, " -> "))
	builder.WriteString(".")

	if len(analysis.LinkChecks) > 0 {
		builder.WriteString(fmt.Sprintf(" Observed %d of %d inter-service link(s).", observedLinks, len(analysis.LinkChecks)))
	}
	if len(analysis.MissingServices) > 0 {
		builder.WriteString(" Missing services: ")
		builder.WriteString(strings.Join(analysis.MissingServices, ", "))
		builder.WriteString(".")
	}

	return builder.String()
}

func buildAddRecommendations(
	req AddSimulationRequest,
	distribution []PlacementDistribution,
	success bool,
	selectedNodeFound bool,
	selectedNodeSuitable bool,
	recommendedNodeName string,
	remainingReplicas int,
	riskAnalysis AddRiskAnalysis,
) []FailureRecommendation {
	recommendations := make([]FailureRecommendation, 0, 3)

	switch {
	case success && req.TargetNodeName != "" && selectedNodeSuitable:
		var placements []string
		for _, placement := range distribution {
			placements = append(placements, fmt.Sprintf("%d on %s", placement.Replicas, placement.Node))
		}
		recommendations = append(recommendations, FailureRecommendation{
			Type:        "placement",
			Priority:    "high",
			Description: fmt.Sprintf("Place %d replica(s) with the preferred node first: %s.", req.Replicas, strings.Join(placements, ", ")),
		})
		if recommendedNodeName != "" {
			recommendations = append(recommendations, FailureRecommendation{
				Type:        "placement",
				Priority:    "medium",
				Description: fmt.Sprintf("Preferred node %s fits, but %s keeps more headroom if you want a safer placement.", req.TargetNodeName, recommendedNodeName),
			})
		}
	case success && req.TargetNodeName != "" && !selectedNodeSuitable && recommendedNodeName != "":
		recommendations = append(recommendations, FailureRecommendation{
			Type:        "placement",
			Priority:    "high",
			Description: fmt.Sprintf("Preferred node %s cannot host the service. Use %s as the fallback placement target.", req.TargetNodeName, recommendedNodeName),
		})
	case success:
		var placements []string
		for _, placement := range distribution {
			placements = append(placements, fmt.Sprintf("%d on %s", placement.Replicas, placement.Node))
		}
		recommendations = append(recommendations, FailureRecommendation{
			Type:        "placement",
			Priority:    "high",
			Description: fmt.Sprintf("Place %d replica(s) across %d node(s): %s.", req.Replicas, len(distribution), strings.Join(placements, ", ")),
		})
	default:
		placed := req.Replicas - remainingReplicas
		recommendations = append(recommendations, FailureRecommendation{
			Type:        "scaling",
			Priority:    "critical",
			Description: fmt.Sprintf("Insufficient capacity. Can only place %d of %d replica(s). Add nodes or reduce the requested CPU/RAM.", placed, req.Replicas),
		})
	}

	if req.TargetNodeName != "" && !selectedNodeFound {
		recommendations = append(recommendations, FailureRecommendation{
			Type:        "placement",
			Priority:    "medium",
			Description: fmt.Sprintf("Preferred node %s was not found in the current cluster snapshot.", req.TargetNodeName),
		})
	}

	if len(req.Dependencies) > 0 && riskAnalysis.DependencyRisk != "low" {
		priority := "medium"
		if riskAnalysis.DependencyRisk == "high" {
			priority = "high"
		}
		recommendations = append(recommendations, FailureRecommendation{
			Type:        "dependency",
			Priority:    priority,
			Description: riskAnalysis.Description,
		})
	}

	return recommendations
}

func buildAddExplanation(
	req AddSimulationRequest,
	success bool,
	selectedNodeFound bool,
	selectedNodeSuitable bool,
	recommendedNodeName string,
	totalCapacityPods int,
) string {
	switch {
	case success && req.TargetNodeName != "" && selectedNodeSuitable && recommendedNodeName != "":
		return fmt.Sprintf("Preferred node %s can host the service, and %s would retain more post-placement headroom if you want a safer option.", req.TargetNodeName, recommendedNodeName)
	case success && req.TargetNodeName != "" && selectedNodeSuitable:
		return fmt.Sprintf("Preferred node %s can host the requested service resources.", req.TargetNodeName)
	case success && req.TargetNodeName != "" && !selectedNodeSuitable && recommendedNodeName != "":
		return fmt.Sprintf("Preferred node %s cannot host the requested resources, but the cluster can still place the service by using %s.", req.TargetNodeName, recommendedNodeName)
	case success && req.TargetNodeName != "" && !selectedNodeFound && recommendedNodeName != "":
		return fmt.Sprintf("Preferred node %s was not found, but the cluster can still place the service on %s.", req.TargetNodeName, recommendedNodeName)
	case success:
		return "Successfully found placement for all requested replicas."
	default:
		return fmt.Sprintf("Failed to find placement for all replicas. Current node-level capacity is limited to %d pod(s).", totalCapacityPods)
	}
}

func canonicalServiceID(namespace, name string) string {
	ns := strings.TrimSpace(namespace)
	if ns == "" {
		ns = "default"
	}
	return fmt.Sprintf("%s:%s", ns, strings.TrimSpace(name))
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

func round4(value float64) float64 {
	return math.Round(value*10000) / 10000
}

func floatPtr(value float64) *float64 {
	return &value
}

func intPtr(value int) *int {
	return &value
}
