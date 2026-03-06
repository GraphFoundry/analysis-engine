package predictive

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"predictive-analysis-engine/pkg/clients/graph"
)

const (
	capacityCPUThreshold       = 75.0
	capacityRAMThreshold       = 80.0
	capacityCriticalThreshold  = 90.0
	capacityServiceRPSThresh   = 30.0
	capacityLatencyRPSThresh   = 150.0
	capacityLatencyHighP95Ms   = 1500.0
	capacityLatencyCriticalP95 = 3200.0
	networkEdgeRPSThresh       = 35.0
	networkTrafficIncreasePerc = 35.0
	networkSustainedRPSThresh  = 90.0
	networkSustainedP95Ms      = 180.0
	stickyHoldEvaluations      = 4
	maxScaleReplicas           = 8
)

// SnapshotProvider abstracts graph data retrieval for predictive evaluation.
type SnapshotProvider interface {
	GetMetricsSnapshot(ctx context.Context) (*graph.MetricsSnapshotResponse, error)
	GetServices(ctx context.Context) ([]graph.ServiceInfo, error)
	GetNodes(ctx context.Context) ([]graph.NodeWithResources, error)
}

type PrimaryBottleneck struct {
	Type          string `json:"type"` // capacity | network
	Namespace     string `json:"namespace,omitempty"`
	Service       string `json:"service,omitempty"`
	Node          string `json:"node,omitempty"`
	SourceService string `json:"sourceService,omitempty"`
	TargetService string `json:"targetService,omitempty"`
	SourceNode    string `json:"sourceNode,omitempty"`
	TargetNode    string `json:"targetNode,omitempty"`
}

type RecommendationConfig struct {
	Namespace     string `json:"namespace"`
	ObserveTokens int    `json:"observeTokens"`
	Replicas      *int   `json:"replicas,omitempty"`
	TargetNode    string `json:"targetNode,omitempty"`
}

type Recommendation struct {
	Title      string               `json:"title"`
	Message    string               `json:"message"`
	Severity   string               `json:"severity"`
	ActionType string               `json:"actionType"` // ScaleService | MigrateService
	DrillType  string               `json:"drillType"`  // PodScaleUp | MigrateService
	Target     string               `json:"target"`     // namespace/service
	Config     RecommendationConfig `json:"config"`
}

type Evidence struct {
	Timestamp          string  `json:"timestamp"`
	CPUPressurePercent float64 `json:"cpuPressurePercent,omitempty"`
	RAMPressurePercent float64 `json:"ramPressurePercent,omitempty"`
	ServiceRPS         float64 `json:"serviceRps,omitempty"`
	EdgeRPS            float64 `json:"edgeRps,omitempty"`
	EdgeP95Ms          float64 `json:"edgeP95Ms,omitempty"`
	TrafficIncreasePct float64 `json:"trafficIncreasePct,omitempty"`
	SourceNode         string  `json:"sourceNode,omitempty"`
	TargetNode         string  `json:"targetNode,omitempty"`
	SourceService      string  `json:"sourceService,omitempty"`
	TargetService      string  `json:"targetService,omitempty"`
}

type CurrentActionResponse struct {
	AnomalyActive     bool               `json:"anomalyActive"`
	HealthScore       float64            `json:"healthScore"`
	PrimaryBottleneck *PrimaryBottleneck `json:"primaryBottleneck"`
	TimeToImpactSec   *int               `json:"timeToImpactSec"`
	Recommendation    *Recommendation    `json:"recommendation"`
	Evidence          Evidence           `json:"evidence"`
}

type Evaluator struct {
	source SnapshotProvider

	mu                   sync.Mutex
	previousEdgeRPS      map[string]float64
	healthyStreak        int
	stickyRecommendation *CurrentActionResponse
}

func NewEvaluator(source SnapshotProvider) *Evaluator {
	return &Evaluator{
		source:          source,
		previousEdgeRPS: make(map[string]float64),
	}
}

// Evaluate fetches fresh graph data and returns the current predictive recommendation payload.
func (e *Evaluator) Evaluate(ctx context.Context) (CurrentActionResponse, error) {
	if e.source == nil {
		return healthyResponse(time.Now().UTC()), nil
	}

	var (
		snapshot    *graph.MetricsSnapshotResponse
		services    []graph.ServiceInfo
		nodes       []graph.NodeWithResources
		snapshotErr error
		servicesErr error
		nodesErr    error
		wg          sync.WaitGroup
	)

	wg.Add(3)
	go func() {
		defer wg.Done()
		snapshot, snapshotErr = e.source.GetMetricsSnapshot(ctx)
	}()
	go func() {
		defer wg.Done()
		services, servicesErr = e.source.GetServices(ctx)
	}()
	go func() {
		defer wg.Done()
		nodes, nodesErr = e.source.GetNodes(ctx)
	}()
	wg.Wait()

	if snapshotErr != nil {
		return CurrentActionResponse{}, snapshotErr
	}
	if servicesErr != nil {
		services = nil
	}
	if nodesErr != nil {
		nodes = nil
	}

	return e.EvaluateFromSamples(snapshot, services, nodes), nil
}

// EvaluateFromSamples evaluates a recommendation from already-collected snapshot payloads.
func (e *Evaluator) EvaluateFromSamples(
	snapshot *graph.MetricsSnapshotResponse,
	services []graph.ServiceInfo,
	nodes []graph.NodeWithResources,
) CurrentActionResponse {
	now := time.Now().UTC()
	if snapshot == nil {
		return healthyResponse(now)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	evaluated := e.evaluateLocked(snapshot, services, nodes, now)
	e.updatePreviousEdgeRates(snapshot)

	if evaluated.AnomalyActive {
		e.healthyStreak = 0
		cp := evaluated
		e.stickyRecommendation = &cp
		return evaluated
	}

	if e.stickyRecommendation != nil {
		e.healthyStreak++
		if e.healthyStreak < stickyHoldEvaluations {
			sticky := *e.stickyRecommendation
			sticky.HealthScore = evaluated.HealthScore
			sticky.Evidence.Timestamp = evaluated.Evidence.Timestamp
			sticky.AnomalyActive = true
			return sticky
		}
	}

	e.healthyStreak = 0
	e.stickyRecommendation = nil
	return evaluated
}

type nodePressure struct {
	cpu float64
	ram float64
}

type capacityCandidate struct {
	namespace   string
	service     string
	node        string
	cpu         float64
	ram         float64
	rps         float64
	p95         float64
	currentPods int
	severity    string
}

type networkCandidate struct {
	namespace         string
	sourceService     string
	targetService     string
	sourceNode        string
	targetNode        string
	rps               float64
	p95               float64
	trafficIncreasePc float64
	detectionMode     string
}

func (e *Evaluator) evaluateLocked(
	snapshot *graph.MetricsSnapshotResponse,
	services []graph.ServiceInfo,
	nodes []graph.NodeWithResources,
	now time.Time,
) CurrentActionResponse {
	if snapshot == nil {
		return healthyResponse(now)
	}

	metricByKey := make(map[string]graph.ServiceMetrics)
	metricByName := make(map[string]graph.ServiceMetrics)
	for _, svc := range snapshot.Services {
		ns := normalizeNamespace(svc.Namespace)
		key := serviceKey(ns, svc.Name)
		metricByKey[key] = svc
		metricByName[strings.ToLower(strings.TrimSpace(svc.Name))] = svc
	}

	nodePressureByName := make(map[string]nodePressure)
	for _, node := range nodes {
		nodePressureByName[node.Name] = nodePressure{
			cpu: node.Resources.CPU.UsagePercent,
			ram: percentFromRam(node.Resources.RAM.UsedMB, node.Resources.RAM.TotalMB),
		}
	}

	serviceNodeByKey := make(map[string]string)
	serviceNamespaceByName := make(map[string]string)
	var bestCapacity *capacityCandidate

	for _, svc := range services {
		ns := normalizeNamespace(svc.Namespace)
		key := serviceKey(ns, svc.Name)
		serviceNamespaceByName[strings.ToLower(strings.TrimSpace(svc.Name))] = ns

		bestNode, cpu, ram := primaryNodePressure(svc, nodePressureByName)
		if bestNode != "" {
			serviceNodeByKey[key] = bestNode
		}

		metric, ok := metricByKey[key]
		if !ok {
			metric, ok = metricByName[strings.ToLower(strings.TrimSpace(svc.Name))]
		}
		if !ok {
			continue
		}

		if metric.RPS < capacityServiceRPSThresh {
			continue
		}
		resourcePressure := cpu >= capacityCPUThreshold || ram >= capacityRAMThreshold
		latencyPressure := metric.RPS >= capacityLatencyRPSThresh && metric.P95 >= capacityLatencyHighP95Ms
		if !resourcePressure && !latencyPressure {
			continue
		}

		severity := "high"
		if cpu >= capacityCriticalThreshold || ram >= capacityCriticalThreshold || metric.P95 >= capacityLatencyCriticalP95 {
			severity = "critical"
		}

		candidate := &capacityCandidate{
			namespace:   ns,
			service:     svc.Name,
			node:        bestNode,
			cpu:         cpu,
			ram:         ram,
			rps:         metric.RPS,
			p95:         metric.P95,
			currentPods: maxInt(svc.PodCount, 1),
			severity:    severity,
		}

		if betterCapacityCandidate(candidate, bestCapacity) {
			bestCapacity = candidate
		}
	}

	var bestNetwork *networkCandidate
	for _, edge := range snapshot.Edges {
		ns := normalizeNamespace(edge.Namespace)
		if ns == "default" {
			if mappedNS, ok := serviceNamespaceByName[strings.ToLower(strings.TrimSpace(edge.To))]; ok {
				ns = mappedNS
			} else if mappedNS, ok := serviceNamespaceByName[strings.ToLower(strings.TrimSpace(edge.From))]; ok {
				ns = mappedNS
			}
		}

		fromKey := serviceKey(ns, edge.From)
		toKey := serviceKey(ns, edge.To)
		sourceNode := serviceNodeByKey[fromKey]
		targetNode := serviceNodeByKey[toKey]
		if sourceNode == "" || targetNode == "" || sourceNode == targetNode {
			continue
		}
		if edge.RPS < networkEdgeRPSThresh {
			continue
		}

		prev := e.previousEdgeRPS[edgeKey(ns, edge.From, edge.To)]
		trafficIncreasePc := 0.0
		if prev > 0 {
			trafficIncreasePc = ((edge.RPS - prev) / prev) * 100
		}

		isTrafficSurge := prev > 0 && trafficIncreasePc >= networkTrafficIncreasePerc
		isSustainedPressure := edge.RPS >= networkSustainedRPSThresh && edge.P95 >= networkSustainedP95Ms
		if !isTrafficSurge && !isSustainedPressure {
			continue
		}

		detectionMode := "surge"
		if isSustainedPressure && !isTrafficSurge {
			detectionMode = "sustained"
		}

		candidate := &networkCandidate{
			namespace:         ns,
			sourceService:     edge.From,
			targetService:     edge.To,
			sourceNode:        sourceNode,
			targetNode:        targetNode,
			rps:               edge.RPS,
			p95:               edge.P95,
			trafficIncreasePc: trafficIncreasePc,
			detectionMode:     detectionMode,
		}
		if betterNetworkCandidate(candidate, bestNetwork) {
			bestNetwork = candidate
		}
	}

	selectedType := ""
	var response CurrentActionResponse
	response.Evidence.Timestamp = timestampForResponse(snapshot.Timestamp, now)

	switch {
	case bestCapacity != nil && bestCapacity.severity == "critical":
		selectedType = "capacity"
		response = buildCapacityResponse(bestCapacity, response.Evidence.Timestamp)
	case bestNetwork != nil:
		selectedType = "network"
		response = buildNetworkResponse(bestNetwork, response.Evidence.Timestamp)
	case bestCapacity != nil:
		selectedType = "capacity"
		response = buildCapacityResponse(bestCapacity, response.Evidence.Timestamp)
	default:
		response = healthyResponse(now)
	}

	response.HealthScore = computeHealthScore(snapshot, nodePressureByName, selectedType, response.Recommendation)
	return response
}

func buildCapacityResponse(candidate *capacityCandidate, timestamp string) CurrentActionResponse {
	replicas := suggestedReplicas(candidate.currentPods, candidate.severity)
	timeToImpact := 180
	if candidate.severity == "critical" {
		timeToImpact = 60
	}

	title := "High Traffic Detected: Scale Service Now"
	if candidate.severity == "critical" {
		title = "Critical Saturation Risk: Scale Service Immediately"
	}

	severity := candidate.severity
	resourceDriven := candidate.cpu >= capacityCPUThreshold || candidate.ram >= capacityRAMThreshold
	message := candidate.service + " is nearing resource exhaustion. Increase replicas now."
	if resourceDriven && candidate.node != "" {
		message = "Node " + candidate.node + " hosting " + candidate.service + " is nearing resource exhaustion. Increase replicas now."
	}
	if !resourceDriven {
		message = candidate.service + " latency is spiking under load. Scale replicas now to absorb traffic."
	}

	return CurrentActionResponse{
		AnomalyActive: true,
		PrimaryBottleneck: &PrimaryBottleneck{
			Type:      "capacity",
			Namespace: candidate.namespace,
			Service:   candidate.service,
			Node:      candidate.node,
		},
		TimeToImpactSec: &timeToImpact,
		Recommendation: &Recommendation{
			Title:      title,
			Message:    message,
			Severity:   severity,
			ActionType: "ScaleService",
			DrillType:  "PodScaleUp",
			Target:     candidate.namespace + "/" + candidate.service,
			Config: RecommendationConfig{
				Namespace:     candidate.namespace,
				ObserveTokens: 30,
				Replicas:      &replicas,
			},
		},
		Evidence: Evidence{
			Timestamp:          timestamp,
			CPUPressurePercent: round1(candidate.cpu),
			RAMPressurePercent: round1(candidate.ram),
			ServiceRPS:         round1(candidate.rps),
		},
	}
}

func buildNetworkResponse(candidate *networkCandidate, timestamp string) CurrentActionResponse {
	timeToImpact := 240
	message := "Cross-node traffic is surging between " + candidate.sourceService + " and " + candidate.targetService +
		". Migrate " + candidate.targetService + " to " + candidate.sourceNode + " to reduce latency."
	if candidate.detectionMode == "sustained" {
		timeToImpact = 180
		message = "Cross-node traffic remains heavy between " + candidate.sourceService + " and " + candidate.targetService +
			". Co-locate services by moving " + candidate.targetService + " to " + candidate.sourceNode + "."
	}

	return CurrentActionResponse{
		AnomalyActive: true,
		PrimaryBottleneck: &PrimaryBottleneck{
			Type:          "network",
			Namespace:     candidate.namespace,
			SourceService: candidate.sourceService,
			TargetService: candidate.targetService,
			SourceNode:    candidate.sourceNode,
			TargetNode:    candidate.targetNode,
		},
		TimeToImpactSec: &timeToImpact,
		Recommendation: &Recommendation{
			Title:      "Cross-Node Chatter Detected: Co-Locate Services",
			Message:    message,
			Severity:   "high",
			ActionType: "MigrateService",
			DrillType:  "MigrateService",
			Target:     candidate.namespace + "/" + candidate.targetService,
			Config: RecommendationConfig{
				Namespace:     candidate.namespace,
				ObserveTokens: 35,
				TargetNode:    candidate.sourceNode,
			},
		},
		Evidence: Evidence{
			Timestamp:          timestamp,
			EdgeRPS:            round1(candidate.rps),
			EdgeP95Ms:          round1(candidate.p95),
			TrafficIncreasePct: round1(candidate.trafficIncreasePc),
			SourceNode:         candidate.sourceNode,
			TargetNode:         candidate.targetNode,
			SourceService:      candidate.sourceService,
			TargetService:      candidate.targetService,
		},
	}
}

func (e *Evaluator) updatePreviousEdgeRates(snapshot *graph.MetricsSnapshotResponse) {
	if snapshot == nil {
		return
	}

	next := make(map[string]float64, len(snapshot.Edges))
	for _, edge := range snapshot.Edges {
		ns := normalizeNamespace(edge.Namespace)
		next[edgeKey(ns, edge.From, edge.To)] = edge.RPS
	}
	e.previousEdgeRPS = next
}

func healthyResponse(now time.Time) CurrentActionResponse {
	return CurrentActionResponse{
		AnomalyActive:     false,
		HealthScore:       100,
		PrimaryBottleneck: nil,
		TimeToImpactSec:   nil,
		Recommendation:    nil,
		Evidence: Evidence{
			Timestamp: now.UTC().Format(time.RFC3339),
		},
	}
}

func betterCapacityCandidate(candidate, current *capacityCandidate) bool {
	if candidate == nil {
		return false
	}
	if current == nil {
		return true
	}

	severityRank := map[string]int{"critical": 2, "high": 1}
	if severityRank[candidate.severity] != severityRank[current.severity] {
		return severityRank[candidate.severity] > severityRank[current.severity]
	}

	candidatePressure := math.Max(math.Max(candidate.cpu, candidate.ram), math.Min(100, candidate.p95/20))
	currentPressure := math.Max(math.Max(current.cpu, current.ram), math.Min(100, current.p95/20))
	if candidatePressure != currentPressure {
		return candidatePressure > currentPressure
	}
	return candidate.rps > current.rps
}

func betterNetworkCandidate(candidate, current *networkCandidate) bool {
	if candidate == nil {
		return false
	}
	if current == nil {
		return true
	}

	candidateCanonical := isCanonicalScenarioPair(candidate.sourceService, candidate.targetService)
	currentCanonical := isCanonicalScenarioPair(current.sourceService, current.targetService)
	if candidateCanonical != currentCanonical {
		return candidateCanonical
	}

	if candidate.detectionMode != current.detectionMode {
		return candidate.detectionMode == "surge"
	}

	if candidate.trafficIncreasePc != current.trafficIncreasePc {
		return candidate.trafficIncreasePc > current.trafficIncreasePc
	}

	if candidate.rps != current.rps {
		return candidate.rps > current.rps
	}

	return strings.ToLower(candidate.targetService) < strings.ToLower(current.targetService)
}

func isCanonicalScenarioPair(sourceService, targetService string) bool {
	s := strings.ToLower(strings.TrimSpace(sourceService))
	t := strings.ToLower(strings.TrimSpace(targetService))
	return (s == "frontend" && t == "productcatalogservice") ||
		(s == "productcatalogservice" && t == "frontend")
}

func suggestedReplicas(currentPods int, severity string) int {
	increment := 1
	if severity == "critical" {
		increment = 2
	}
	target := maxInt(currentPods, 1) + increment
	if target > maxScaleReplicas {
		return maxScaleReplicas
	}
	return target
}

func primaryNodePressure(service graph.ServiceInfo, fallback map[string]nodePressure) (node string, cpu float64, ram float64) {
	placements := service.Placement.Nodes
	if len(placements) == 0 {
		return "", 0, 0
	}

	type candidate struct {
		name     string
		cpu      float64
		ram      float64
		podCount int
	}
	candidates := make([]candidate, 0, len(placements))
	for _, placement := range placements {
		resCPU := placement.Resources.CPU.UsagePercent
		resRAM := percentFromRam(placement.Resources.RAM.UsedMB, placement.Resources.RAM.TotalMB)

		if fallbackPressure, ok := fallback[placement.Node]; ok {
			// Prefer infrastructure-node view when present.
			if fallbackPressure.cpu > 0 {
				resCPU = fallbackPressure.cpu
			}
			if fallbackPressure.ram > 0 {
				resRAM = fallbackPressure.ram
			}
		}

		candidates = append(candidates, candidate{
			name:     placement.Node,
			cpu:      resCPU,
			ram:      resRAM,
			podCount: len(placement.Pods),
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].podCount != candidates[j].podCount {
			return candidates[i].podCount > candidates[j].podCount
		}
		return math.Max(candidates[i].cpu, candidates[i].ram) > math.Max(candidates[j].cpu, candidates[j].ram)
	})

	best := candidates[0]
	return best.name, best.cpu, best.ram
}

func computeHealthScore(
	snapshot *graph.MetricsSnapshotResponse,
	nodePressureByName map[string]nodePressure,
	selectedType string,
	recommendation *Recommendation,
) float64 {
	maxCPU := 0.0
	maxRAM := 0.0
	for _, pressure := range nodePressureByName {
		if pressure.cpu > maxCPU {
			maxCPU = pressure.cpu
		}
		if pressure.ram > maxRAM {
			maxRAM = pressure.ram
		}
	}

	maxEdgeRPS := 0.0
	maxServiceP95 := 0.0
	if snapshot != nil {
		for _, svc := range snapshot.Services {
			if svc.P95 > maxServiceP95 {
				maxServiceP95 = svc.P95
			}
		}
		for _, edge := range snapshot.Edges {
			if edge.RPS > maxEdgeRPS {
				maxEdgeRPS = edge.RPS
			}
		}
	}

	penalty := 0.0
	if maxCPU > 60 {
		penalty += (maxCPU - 60) * 0.7
	}
	if maxRAM > 65 {
		penalty += (maxRAM - 65) * 0.6
	}
	if maxEdgeRPS > 35 {
		penalty += math.Min(15, (maxEdgeRPS-35)*0.2)
	}
	if maxServiceP95 > 250 {
		penalty += math.Min(28, (maxServiceP95-250)/110)
	}
	if selectedType == "capacity" && recommendation != nil && recommendation.Severity == "critical" {
		penalty += 10
	} else if selectedType != "" {
		penalty += 5
	}

	score := 100 - penalty
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return round1(score)
}

func timestampForResponse(snapshotTimestamp string, fallback time.Time) string {
	if strings.TrimSpace(snapshotTimestamp) != "" {
		return snapshotTimestamp
	}
	return fallback.UTC().Format(time.RFC3339)
}

func normalizeNamespace(namespace string) string {
	ns := strings.TrimSpace(namespace)
	if ns == "" {
		return "default"
	}
	return ns
}

func serviceKey(namespace, name string) string {
	return normalizeNamespace(namespace) + "/" + strings.TrimSpace(name)
}

func edgeKey(namespace, from, to string) string {
	return normalizeNamespace(namespace) + "/" + strings.TrimSpace(from) + "->" + strings.TrimSpace(to)
}

func percentFromRam(usedMB, totalMB float64) float64 {
	if totalMB <= 0 {
		return 0
	}
	return (usedMB / totalMB) * 100
}

func round1(value float64) float64 {
	return math.Round(value*10) / 10
}

func maxInt(values ...int) int {
	if len(values) == 0 {
		return 0
	}
	maxVal := values[0]
	for _, value := range values[1:] {
		if value > maxVal {
			maxVal = value
		}
	}
	return maxVal
}
