package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"predictive-analysis-engine/pkg/clients/graph"
)

type GraphSnapshotResponse struct {
	Nodes    []SnapshotNode   `json:"nodes"`
	Edges    []SnapshotEdge   `json:"edges"`
	Metadata SnapshotMetadata `json:"metadata"`
}

type SnapshotNode struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Namespace       string   `json:"namespace"`
	RiskLevel       string   `json:"riskLevel"`
	RiskReason      string   `json:"riskReason"`
	ReqRate         *float64 `json:"reqRate,omitempty"`
	ErrorRatePct    *float64 `json:"errorRatePct,omitempty"`
	LatencyP95Ms    *float64 `json:"latencyP95Ms,omitempty"`
	AvailabilityPct *float64 `json:"availabilityPct,omitempty"`
	PodCount        *int     `json:"podCount,omitempty"`
	Availability    *float64 `json:"availability,omitempty"`
	PageRank        *float64 `json:"pageRank,omitempty"`
	Betweenness     *float64 `json:"betweenness,omitempty"`
	UpdatedAt       string   `json:"updatedAt"`
}

type SnapshotEdge struct {
	ID           string  `json:"id"`
	Source       string  `json:"source"`
	Target       string  `json:"target"`
	ReqRate      float64 `json:"reqRate"`
	LatencyP95Ms float64 `json:"latencyP95Ms"`
}

type SnapshotMetadata struct {
	Stale                 bool   `json:"stale"`
	LastUpdatedSecondsAgo *int   `json:"lastUpdatedSecondsAgo"`
	WindowMinutes         int    `json:"windowMinutes"`
	NodeCount             int    `json:"nodeCount"`
	EdgeCount             int    `json:"edgeCount"`
	NodesWithMetrics      int    `json:"nodesWithMetrics"`
	EdgesWithMetrics      int    `json:"edgesWithMetrics"`
	GeneratedAt           string `json:"generatedAt"`
}

func (h *Handler) DependencyGraphHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := r.URL.Query().Get("namespace")

	var wg sync.WaitGroup
	wg.Add(3)

	var snapshotResult *graph.MetricsSnapshotResponse
	var snapshotErr error

	var healthResult *graph.HealthResponse
	var healthErr error

	var centralityResult *graph.CentralityScoresResponse
	var centralityErr error

	go func() {
		defer wg.Done()
		snapshotResult, snapshotErr = h.GraphClient.GetMetricsSnapshot(ctx)
	}()

	go func() {
		defer wg.Done()
		healthResult, healthErr = h.GraphClient.CheckHealth(ctx)
	}()

	go func() {
		defer wg.Done()
		centralityResult, centralityErr = h.GraphClient.GetCentralityScores(ctx)
	}()

	wg.Wait()

	stale := true
	var lastUpdatedSecondsAgo *int
	windowMinutes := 5

	if healthErr == nil && healthResult != nil {
		stale = healthResult.Stale
		l := healthResult.LastUpdatedSecondsAgo
		lastUpdatedSecondsAgo = &l
		windowMinutes = healthResult.WindowMinutes
	}

	if snapshotErr != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Failed to fetch graph snapshot from Graph Engine",
			"nodes": []interface{}{},
			"edges": []interface{}{},
			"metadata": map[string]interface{}{
				"stale":                 true,
				"lastUpdatedSecondsAgo": nil,
				"windowMinutes":         windowMinutes,
			},
		})
		return
	}

	rawServices := snapshotResult.Services
	rawEdges := snapshotResult.Edges

	serviceNamespaceMap := make(map[string]string)
	serviceMetricsMap := make(map[string]graph.ServiceMetrics)

	centralityMap := make(map[string]graph.ServiceScore)
	if centralityErr == nil && centralityResult != nil {
		for _, s := range centralityResult.Scores {
			centralityMap[s.Service] = s
		}
	}

	nodes := []SnapshotNode{}
	nodesWithMetricsCount := 0

	for _, svc := range rawServices {
		ns := svc.Namespace
		if ns == "" {
			ns = "default"
		}
		serviceNamespaceMap[svc.Name] = ns
		serviceMetricsMap[svc.Name] = svc

		if namespace != "" && ns != namespace {
			continue
		}

		riskLevel, riskReason := calculateRiskLevel(svc)

		reqRate := svc.RPS

		errPct := svc.ErrorRate * 100.0
		p95 := svc.P95
		availPct := svc.Availability.Value * 100.0
		if svc.Availability.Value == 0 && svc.ErrorRate == 0 && svc.RPS == 0 {

		}

		podCountVal := svc.PodCount.Value
		availabilityVal := svc.Availability.Value

		var pageRank, betweenness *float64
		if score, ok := centralityMap[svc.Name]; ok {
			pr := score.PageRank
			b := score.Betweenness
			pageRank = &pr
			betweenness = &b
		}

		node := SnapshotNode{
			ID:              fmt.Sprintf("%s:%s", ns, svc.Name),
			Name:            svc.Name,
			Namespace:       ns,
			RiskLevel:       riskLevel,
			RiskReason:      riskReason,
			ReqRate:         &reqRate,
			ErrorRatePct:    &errPct,
			LatencyP95Ms:    &p95,
			AvailabilityPct: &availPct,
			PodCount:        &podCountVal,
			Availability:    &availabilityVal,
			PageRank:        pageRank,
			Betweenness:     betweenness,
			UpdatedAt:       time.Now().Format(time.RFC3339),
		}

		nodesWithMetricsCount++

		nodes = append(nodes, node)
	}

	edges := []SnapshotEdge{}
	edgesWithMetricsCount := 0

	for _, e := range rawEdges {

		fromNs, ok := serviceNamespaceMap[e.From]
		if !ok {
			fromNs = "default"
		}
		toNs := e.Namespace
		if toNs == "" {

			if ns, ok := serviceNamespaceMap[e.To]; ok {
				toNs = ns
			} else {
				toNs = "default"
			}
		}

		id := fmt.Sprintf("%s:%s->%s:%s", fromNs, e.From, toNs, e.To)

		rate := e.RPS

		p95 := e.P95

		edge := SnapshotEdge{
			ID:           id,
			Source:       fmt.Sprintf("%s:%s", fromNs, e.From),
			Target:       fmt.Sprintf("%s:%s", toNs, e.To),
			ReqRate:      rate,
			LatencyP95Ms: p95,
		}

		edgesWithMetricsCount++

		edges = append(edges, edge)
	}

	resp := GraphSnapshotResponse{
		Nodes: nodes,
		Edges: edges,
		Metadata: SnapshotMetadata{
			Stale:                 stale,
			LastUpdatedSecondsAgo: lastUpdatedSecondsAgo,
			WindowMinutes:         windowMinutes,
			NodeCount:             len(nodes),
			EdgeCount:             len(edges),
			NodesWithMetrics:      nodesWithMetricsCount,
			EdgesWithMetrics:      edgesWithMetricsCount,
			GeneratedAt:           time.Now().Format(time.RFC3339),
		},
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(resp)
}

func calculateRiskLevel(m graph.ServiceMetrics) (string, string) {

	isPodCountObject := m.PodCount.IsObject
	isAvailabilityObject := m.Availability.IsObject

	availPct := m.Availability.Value * 100.0
	errPct := m.ErrorRate * 100.0

	if m.PodCount.Value == 0 && !isPodCountObject {
		return "CRITICAL", "No pods running"
	}

	if isAvailabilityObject {

	} else {
		if availPct < 50 {
			return "CRITICAL", fmt.Sprintf("Critical availability (%.1f%%)", availPct)
		}

		if errPct > 5.0 {
			return "HIGH", fmt.Sprintf("High error rate (%.2f%%)", errPct)
		}
		if availPct < 95.0 {
			return "HIGH", fmt.Sprintf("Low availability (%.1f%%)", availPct)
		}
		if m.P95 > 1000 {
			return "HIGH", fmt.Sprintf("P95 latency spike (%.0fms)", m.P95)
		}

		if errPct > 1.0 {
			return "MEDIUM", fmt.Sprintf("Elevated error rate (%.2f%%)", errPct)
		}
		if availPct < 99.0 {
			return "MEDIUM", fmt.Sprintf("Availability degraded (%.1f%%)", availPct)
		}
		if m.P95 > 500 {
			return "MEDIUM", fmt.Sprintf("Slow responses (%.0fms)", m.P95)
		}
	}

	if m.RPS == 0 && m.ErrorRate == 0 && m.P95 == 0 {

		return "LOW", "Operating normally"

	}

	return "LOW", "Operating normally"
}
