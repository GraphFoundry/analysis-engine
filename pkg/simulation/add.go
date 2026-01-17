package simulation

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"predictive-analysis-engine/pkg/clients/graph"
)

func SimulateAddService(ctx context.Context, client *graph.Client, req AddSimulationRequest) (*AddSimulationResult, error) {

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

	if req.CPURequest <= 0 || req.RAMRequest <= 0 || req.Replicas <= 0 {
		return nil, fmt.Errorf("Invalid resource requests: cpu, ram, and replicas must be positive")
	}

	services, err := client.GetServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("Failed to fetch cluster state: %v", err)
	}

	type rawNode struct {
		Name                  string
		CPUUsagePercent       float64
		CPUCores              int
		RAMUsedMB             float64
		RAMTotalMB            float64
		EffectiveCPUAvailable *float64
		EffectiveRAMAvailable *float64
	}
	rawNodes := make(map[string]*rawNode)

	for _, svc := range services {
		for _, node := range svc.Placement.Nodes {
			if node.Node == "" {
				continue
			}
			if _, exists := rawNodes[node.Node]; !exists {
				rawNodes[node.Node] = &rawNode{
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

		return nil, fmt.Errorf("No nodes found in cluster state. Cannot perform placement analysis.")
	}

	var minikubeNodes []*rawNode
	for _, n := range rawNodes {
		if strings.Contains(strings.ToLower(n.Name), "minikube") {
			minikubeNodes = append(minikubeNodes, n)
		}
	}

	if len(minikubeNodes) > 1 {

		var sharedCpuTotal float64
		var sharedRamTotal float64

		for _, n := range minikubeNodes {
			if float64(n.CPUCores) > sharedCpuTotal {
				sharedCpuTotal = float64(n.CPUCores)
			}
			if n.RAMTotalMB > sharedRamTotal {
				sharedRamTotal = n.RAMTotalMB
			}
		}

		var sharedCpuUsed float64
		var sharedRamUsed float64
		for _, n := range minikubeNodes {
			sharedCpuUsed += (n.CPUUsagePercent / 100.0) * float64(n.CPUCores)
			sharedRamUsed += n.RAMUsedMB
		}

		sharedCpuAvailable := math.Max(0, sharedCpuTotal-sharedCpuUsed)
		sharedRamAvailable := math.Max(0, sharedRamTotal-sharedRamUsed)

		for _, n := range minikubeNodes {
			nodeCpuAvail := math.Max(0, float64(n.CPUCores)-((n.CPUUsagePercent/100.0)*float64(n.CPUCores)))
			nodeRamAvail := math.Max(0, n.RAMTotalMB-n.RAMUsedMB)

			effCpu := math.Min(nodeCpuAvail, sharedCpuAvailable)
			effRam := math.Min(nodeRamAvail, sharedRamAvailable)

			n.EffectiveCPUAvailable = &effCpu
			n.EffectiveRAMAvailable = &effRam
		}
	}

	var nodeAnalysis []NodeCapacity

	for _, n := range rawNodes {
		var cpuAvail, ramAvail float64

		if n.EffectiveCPUAvailable != nil {
			cpuAvail = *n.EffectiveCPUAvailable
			ramAvail = *n.EffectiveRAMAvailable
		} else {
			cpuUsed := (n.CPUUsagePercent / 100.0) * float64(n.CPUCores)
			cpuAvail = math.Max(0, float64(n.CPUCores)-cpuUsed)
			ramAvail = math.Max(0, n.RAMTotalMB-n.RAMUsedMB)
		}

		cpuAvail = math.Round(cpuAvail*100) / 100
		ramAvail = math.Round(ramAvail*100) / 100

		cpuFit := math.Floor(cpuAvail / req.CPURequest)
		ramFit := math.Floor(ramAvail / float64(req.RAMRequest))
		maxPods := int(math.Min(cpuFit, ramFit))
		if maxPods < 0 {
			maxPods = 0
		}

		nc := NodeCapacity{
			Node:           n.Name,
			CPUAvailable:   cpuAvail,
			RAMAvailableMB: ramAvail,
			CPUTotal:       float64(n.CPUCores),
			RAMTotalMB:     n.RAMTotalMB,
			CanFit:         maxPods > 0,
			MaxPods:        maxPods,
			NodeName:       n.Name,
		}

		reason := ""
		if !nc.CanFit {
			if cpuFit < 1 && ramFit < 1 {
				reason = "Insufficient CPU and RAM"
			} else if cpuFit < 1 {
				reason = "Insufficient CPU"
			} else if ramFit < 1 {
				reason = "Insufficient RAM"
			}
		}
		nc.Reason = reason

		nodeAnalysis = append(nodeAnalysis, nc)
	}

	for i := range nodeAnalysis {
		n := &nodeAnalysis[i]
		score := 0

		if n.CanFit {
			projectedCpu := math.Max(0, n.CPUAvailable-req.CPURequest)
			projectedRam := math.Max(0, n.RAMAvailableMB-float64(req.RAMRequest))

			cpuHeadroom := 0.0
			if n.CPUTotal > 0 {
				cpuHeadroom = projectedCpu / n.CPUTotal
			}
			ramHeadroom := 0.0
			if n.RAMTotalMB > 0 {
				ramHeadroom = projectedRam / n.RAMTotalMB
			}

			val := 50 + ((cpuHeadroom+ramHeadroom)/2.0)*50
			score = int(math.Floor(val))

			n.Suitable = true
		} else {
			cpuFrac := 0.0
			if n.CPUTotal > 0 {
				cpuFrac = math.Min(1, n.CPUAvailable/req.CPURequest)
			}
			ramFrac := 0.0
			if n.RAMTotalMB > 0 {
				ramFrac = math.Min(1, n.RAMAvailableMB/float64(req.RAMRequest))
			}

			val := ((cpuFrac + ramFrac) / 2.0) * 40
			score = int(math.Floor(val))

			n.Suitable = false
		}

		n.Score = score
		n.AvailableCPU = n.CPUAvailable
		n.AvailableRAM = n.RAMAvailableMB
	}

	sort.Slice(nodeAnalysis, func(i, j int) bool {
		return nodeAnalysis[i].Score > nodeAnalysis[j].Score
	})

	totalCapacityPods := 0
	for _, n := range nodeAnalysis {
		totalCapacityPods += n.MaxPods
	}

	remainingReplicas := req.Replicas
	distribution := []PlacementDistribution{}

	for _, node := range nodeAnalysis {
		if remainingReplicas <= 0 {
			break
		}
		if node.MaxPods > 0 {
			take := int(math.Min(float64(remainingReplicas), float64(node.MaxPods)))
			distribution = append(distribution, PlacementDistribution{
				Node:     node.Node,
				Replicas: take,
			})
			remainingReplicas -= take
		}
	}

	success := remainingReplicas == 0

	dependencyRisk := "low"
	riskDescription := "No dependencies declared."
	var missingDeps []string

	if len(req.Dependencies) > 0 {

		for _, dep := range req.Dependencies {
			exists := false
			for _, s := range services {

				ns := s.Namespace
				if ns == "" {
					ns = "default"
				}
				id := fmt.Sprintf("%s:%s", ns, s.Name)
				if id == dep.ServiceId {
					exists = true
					break
				}
			}
			if !exists {
				missingDeps = append(missingDeps, dep.ServiceId)
			}
		}

		if len(missingDeps) > 0 {
			dependencyRisk = "high"
			riskDescription = fmt.Sprintf("Missing dependencies in cluster: %s.", strings.Join(missingDeps, ", "))
		} else if len(req.Dependencies) > 3 {
			dependencyRisk = "medium"
			riskDescription = "High number of dependencies increases complexity."
		} else {
			riskDescription = "All dependencies verified in current graph."
		}
	}

	var recommendations []FailureRecommendation
	if success {

		var parts []string
		for _, d := range distribution {
			parts = append(parts, fmt.Sprintf("%d on %s", d.Replicas, d.Node))
		}

		recommendations = append(recommendations, FailureRecommendation{
			Type:        "placement",
			Priority:    "high",
			Description: fmt.Sprintf("Place %d replicas across %d nodes: %s.", req.Replicas, len(distribution), strings.Join(parts, ", ")),
		})
	} else {

		placed := req.Replicas - remainingReplicas
		recommendations = append(recommendations, FailureRecommendation{
			Type:        "scaling",
			Priority:    "critical",
			Description: fmt.Sprintf("Insufficient capacity. Can only place %d replicas. Add nodes or reduce request.", placed),
		})
	}

	explanation := "Successfully found placement for all replicas."
	if !success {
		explanation = fmt.Sprintf("Failed to find placement for all replicas. Capacity limited to %d pods.", totalCapacityPods)
	}

	return &AddSimulationResult{
		TargetServiceName: req.ServiceName,
		Success:           success,
		Confidence:        "high",
		Explanation:       explanation,
		TotalCapacityPods: totalCapacityPods,
		SuitableNodes:     nodeAnalysis,
		RiskAnalysis: AddRiskAnalysis{
			DependencyRisk: dependencyRisk,
			Description:    riskDescription,
		},
		Recommendations: recommendations,
		Recommendation: &LegacyRecommendation{
			ServiceName:  req.ServiceName,
			CPURequest:   req.CPURequest,
			RAMRequest:   req.RAMRequest,
			Distribution: distribution,
		},
	}, nil
}
