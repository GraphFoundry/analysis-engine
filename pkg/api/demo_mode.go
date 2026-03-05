package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"predictive-analysis-engine/pkg/simulation"
)

type demoSnapshotInfo struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type demoSnapshotFile struct {
	ID       string                `json:"id"`
	Services []demoSnapshotService `json:"services"`
	Edges    []demoSnapshotEdge    `json:"edges"`
}

type demoSnapshotService struct {
	ServiceId    string  `json:"serviceId"`
	Name         string  `json:"name"`
	Namespace    string  `json:"namespace"`
	PodCount     int     `json:"podCount"`
	Availability float64 `json:"availability"`
}

type demoSnapshotEdge struct {
	Source    string  `json:"source"`
	Target    string  `json:"target"`
	Rate      float64 `json:"rate"`
	ErrorRate float64 `json:"errorRate"`
	P50       float64 `json:"p50"`
	P95       float64 `json:"p95"`
	P99       float64 `json:"p99"`
}

const (
	demoSnapshotSeedV1     = "seed-v1"
	demoFailureServiceID   = "default:checkoutservice"
	demoScaleServiceID     = "default:recommendationservice"
	demoScaleExpectedPods  = 2
	demoScaleProjectedPods = 5
)

var supportedDemoSnapshots = []demoSnapshotInfo{
	{
		ID:          demoSnapshotSeedV1,
		Label:       "Seed Snapshot v1",
		Description: "Deterministic snapshot used for panel demonstration fallback mode.",
	},
}

func listDemoSnapshots() []demoSnapshotInfo {
	return supportedDemoSnapshots
}

func loadDemoFailureResult(req simulation.FailureSimulationRequest, snapshotID string) (*simulation.FailureSimulationResult, error) {
	id := normalizeDemoSnapshotID(snapshotID)
	if id != demoSnapshotSeedV1 {
		return nil, fmt.Errorf("unsupported snapshotId %q", snapshotID)
	}

	serviceID := normalizeServiceID(req.ServiceId)
	if serviceID != demoFailureServiceID {
		return nil, fmt.Errorf("demo mode only supports failure scenario for %s", demoFailureServiceID)
	}

	filePath := filepath.Join("data", "demo", "scenarios", "failure-checkoutservice.json")
	var result simulation.FailureSimulationResult
	if err := loadDemoJSON(filePath, &result); err != nil {
		return nil, err
	}

	depth := req.Depth
	if req.MaxDepth > 0 {
		depth = req.MaxDepth
	}
	if depth <= 0 {
		depth = 1
	}

	result.RequestNormalized = simulation.RequestNormalization{
		ServiceId:      serviceID,
		GraphLookupKey: "checkoutservice",
		DepthUsed:      depth,
	}
	result.SourceMode = "demo"
	result.SnapshotId = id
	result.Neighborhood.DepthUsed = depth

	return &result, nil
}

func loadDemoScalingResult(req simulation.ScalingSimulationRequest, snapshotID string) (*simulation.ScalingSimulationResult, error) {
	id := normalizeDemoSnapshotID(snapshotID)
	if id != demoSnapshotSeedV1 {
		return nil, fmt.Errorf("unsupported snapshotId %q", snapshotID)
	}

	serviceID := normalizeServiceID(req.ServiceId)
	if serviceID != demoScaleServiceID {
		return nil, fmt.Errorf("demo mode only supports scaling scenario for %s", demoScaleServiceID)
	}

	if req.CurrentPods > 0 && req.CurrentPods != demoScaleExpectedPods {
		return nil, fmt.Errorf("demo mode scaling scenario expects currentPods=%d", demoScaleExpectedPods)
	}
	if req.NewPods > 0 && req.NewPods != demoScaleProjectedPods {
		return nil, fmt.Errorf("demo mode scaling scenario expects newPods=%d", demoScaleProjectedPods)
	}

	filePath := filepath.Join("data", "demo", "scenarios", "scale-recommendationservice.json")
	var result simulation.ScalingSimulationResult
	if err := loadDemoJSON(filePath, &result); err != nil {
		return nil, err
	}

	depth := req.MaxDepth
	if depth <= 0 {
		depth = req.Depth
	}
	if depth <= 0 {
		depth = 1
	}
	result.RequestNormalized = simulation.RequestNormalization{
		ServiceId:      serviceID,
		GraphLookupKey: "recommendationservice",
		DepthUsed:      depth,
	}
	result.SourceMode = "demo"
	result.SnapshotId = id
	result.Neighborhood.DepthUsed = depth
	result.CurrentPods = demoScaleExpectedPods
	result.NewPods = demoScaleProjectedPods

	return &result, nil
}

func loadDemoSimulationContext(serviceID string, k int, direction string, snapshotID string) (*simulation.SimulationContextResponse, error) {
	id := normalizeDemoSnapshotID(snapshotID)
	if id != demoSnapshotSeedV1 {
		return nil, fmt.Errorf("unsupported snapshotId %q", snapshotID)
	}

	if k <= 0 {
		k = 1
	}
	if k > 3 {
		k = 3
	}

	direction = strings.ToLower(strings.TrimSpace(direction))
	if direction == "" {
		direction = "both"
	}
	if direction != "both" && direction != "in" && direction != "out" {
		return nil, fmt.Errorf("direction must be one of: both, in, out")
	}

	filePath := filepath.Join("data", "demo", "snapshots", demoSnapshotSeedV1+".json")
	var snapshot demoSnapshotFile
	if err := loadDemoJSON(filePath, &snapshot); err != nil {
		return nil, err
	}

	servicesByID := make(map[string]demoSnapshotService, len(snapshot.Services))
	nameToID := make(map[string]string, len(snapshot.Services))
	for _, service := range snapshot.Services {
		canonicalID := normalizeServiceID(service.ServiceId)
		namespace, name := splitServiceID(canonicalID)
		if service.Namespace != "" {
			namespace = service.Namespace
		}
		if service.Name != "" {
			name = service.Name
		}
		normalizedService := demoSnapshotService{
			ServiceId:    fmt.Sprintf("%s:%s", namespace, name),
			Name:         name,
			Namespace:    namespace,
			PodCount:     service.PodCount,
			Availability: service.Availability,
		}
		servicesByID[normalizedService.ServiceId] = normalizedService
		nameToID[name] = normalizedService.ServiceId
	}

	targetID := normalizeServiceID(serviceID)
	targetNamespace, targetName := splitServiceID(targetID)
	if strings.TrimSpace(targetName) == "" {
		return nil, fmt.Errorf("serviceId query parameter is required")
	}
	if !strings.Contains(strings.TrimSpace(serviceID), ":") {
		if mapped, ok := nameToID[strings.TrimSpace(serviceID)]; ok {
			targetID = mapped
		}
	}
	if _, exists := servicesByID[targetID]; !exists {
		for idKey, service := range servicesByID {
			if service.Name == targetName {
				targetID = idKey
				break
			}
		}
	}
	targetService, exists := servicesByID[targetID]
	if !exists {
		return nil, fmt.Errorf("service %q not found in demo snapshot", serviceID)
	}
	if targetService.Namespace == "" {
		targetService.Namespace = targetNamespace
	}

	normalizedEdges := make([]demoSnapshotEdge, 0, len(snapshot.Edges))
	outgoing := make(map[string][]string)
	incoming := make(map[string][]string)

	for _, edge := range snapshot.Edges {
		source := resolveDemoEndpoint(edge.Source, servicesByID, nameToID)
		target := resolveDemoEndpoint(edge.Target, servicesByID, nameToID)
		normalizedEdges = append(normalizedEdges, demoSnapshotEdge{
			Source:    source,
			Target:    target,
			Rate:      edge.Rate,
			ErrorRate: edge.ErrorRate,
			P50:       edge.P50,
			P95:       edge.P95,
			P99:       edge.P99,
		})
		outgoing[source] = append(outgoing[source], target)
		incoming[target] = append(incoming[target], source)
	}

	type traversalItem struct {
		id    string
		depth int
	}

	visited := map[string]bool{targetID: true}
	queue := []traversalItem{{id: targetID, depth: 0}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.depth >= k {
			continue
		}

		neighbors := make([]string, 0)
		if direction == "both" || direction == "out" {
			neighbors = append(neighbors, outgoing[current.id]...)
		}
		if direction == "both" || direction == "in" {
			neighbors = append(neighbors, incoming[current.id]...)
		}

		for _, next := range neighbors {
			if next == "" || visited[next] {
				continue
			}
			visited[next] = true
			queue = append(queue, traversalItem{id: next, depth: current.depth + 1})
		}
	}

	nodes := make([]simulation.SimulationContextNode, 0, len(visited))
	for idKey := range visited {
		service := servicesByID[idKey]
		if service.ServiceId == "" {
			namespace, name := splitServiceID(idKey)
			service = demoSnapshotService{
				ServiceId:    idKey,
				Name:         name,
				Namespace:    namespace,
				PodCount:     1,
				Availability: 1,
			}
		}
		podCount := service.PodCount
		if podCount <= 0 {
			podCount = 1
		}
		availability := service.Availability
		if availability <= 0 {
			availability = 1
		}

		nodes = append(nodes, simulation.SimulationContextNode{
			ServiceId:    service.ServiceId,
			Name:         service.Name,
			Namespace:    service.Namespace,
			PodCount:     podCount,
			Availability: availability,
		})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ServiceId < nodes[j].ServiceId })

	edges := make([]simulation.SimulationContextEdge, 0)
	for _, edge := range normalizedEdges {
		if !visited[edge.Source] || !visited[edge.Target] {
			continue
		}
		edges = append(edges, simulation.SimulationContextEdge{
			Source:    edge.Source,
			Target:    edge.Target,
			Rate:      edge.Rate,
			ErrorRate: edge.ErrorRate,
			P50:       edge.P50,
			P95:       edge.P95,
			P99:       edge.P99,
		})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source == edges[j].Source {
			if edges[i].Target == edges[j].Target {
				return edges[i].Rate > edges[j].Rate
			}
			return edges[i].Target < edges[j].Target
		}
		return edges[i].Source < edges[j].Source
	})

	return &simulation.SimulationContextResponse{
		Target: simulation.ServiceRef{
			ServiceId: targetService.ServiceId,
			Name:      targetService.Name,
			Namespace: targetService.Namespace,
		},
		K:         k,
		Direction: direction,
		Truncated: false,
		Nodes:     nodes,
		Edges:     edges,
	}, nil
}

func normalizeDemoSnapshotID(snapshotID string) string {
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return "seed-v1"
	}
	return snapshotID
}

func normalizeServiceID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, ":") {
		parts := strings.SplitN(raw, ":", 2)
		namespace := strings.TrimSpace(parts[0])
		name := strings.TrimSpace(parts[1])
		if namespace == "" {
			namespace = "default"
		}
		return fmt.Sprintf("%s:%s", namespace, name)
	}
	return fmt.Sprintf("default:%s", raw)
}

func splitServiceID(serviceID string) (namespace string, name string) {
	normalized := normalizeServiceID(serviceID)
	parts := strings.SplitN(normalized, ":", 2)
	if len(parts) != 2 {
		return "default", normalized
	}
	namespace = strings.TrimSpace(parts[0])
	name = strings.TrimSpace(parts[1])
	if namespace == "" {
		namespace = "default"
	}
	return namespace, name
}

func resolveDemoEndpoint(raw string, servicesByID map[string]demoSnapshotService, nameToID map[string]string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, ":") {
		return normalizeServiceID(raw)
	}
	if mapped, ok := nameToID[raw]; ok {
		return mapped
	}
	canonical := normalizeServiceID(raw)
	if _, ok := servicesByID[canonical]; ok {
		return canonical
	}
	return canonical
}

func loadDemoJSON(path string, dest interface{}) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read demo payload %s: %w", path, err)
	}
	if err := json.Unmarshal(payload, dest); err != nil {
		return fmt.Errorf("failed to decode demo payload %s: %w", path, err)
	}
	return nil
}
