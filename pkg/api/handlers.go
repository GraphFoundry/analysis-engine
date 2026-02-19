package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"predictive-analysis-engine/pkg/analysis"
	"predictive-analysis-engine/pkg/clients/graph"
	"predictive-analysis-engine/pkg/config"
	"predictive-analysis-engine/pkg/logger"
	"predictive-analysis-engine/pkg/simulation"
)

type Handler struct {
	Config            *config.Config
	GraphClient       *graph.Client
	SimulationService *simulation.Service
	StartTime         time.Time
}

func NewHandler(cfg *config.Config, graphClient *graph.Client, simService *simulation.Service) *Handler {
	return &Handler{
		Config:            cfg,
		GraphClient:       graphClient,
		SimulationService: simService,
		StartTime:         time.Now(),
	}
}

// HealthHandler godoc
// @Summary Check API Health
// @Description Checks if the API and connections to the Graph Engine are healthy
// @Tags system
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /health [get]
func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	uptimeSeconds := time.Since(h.StartTime).Seconds()

	uptimeSeconds = float64(int(uptimeSeconds*10)) / 10.0

	ctx := r.Context()
	graphHealth, err := h.GraphClient.CheckHealth(ctx)

	status := "ok"
	var graphAPI interface{}

	if err == nil {
		graphAPI = map[string]interface{}{
			"connected":             true,
			"status":                graphHealth.Status,
			"stale":                 graphHealth.Stale,
			"lastUpdatedSecondsAgo": graphHealth.LastUpdatedSecondsAgo,
			"baseUrl":               h.Config.GraphAPI.BaseURL,
			"timeoutMs":             h.Config.GraphAPI.TimeoutMs,
		}
		if graphHealth.Stale {
			status = "degraded"
		}
	} else {
		status = "degraded"
		graphAPI = map[string]interface{}{
			"connected": false,
			"error":     err.Error(),
			"baseUrl":   h.Config.GraphAPI.BaseURL,
			"timeoutMs": h.Config.GraphAPI.TimeoutMs,
		}
	}

	resp := map[string]interface{}{
		"status":   status,
		"provider": "graph-engine",
		"graphApi": graphAPI,
		"config": map[string]interface{}{
			"maxTraversalDepth":    h.Config.Simulation.MaxTraversalDepth,
			"defaultLatencyMetric": h.Config.Simulation.DefaultLatencyMetric,
		},
		"telemetry": map[string]interface{}{
			"enabled":       h.Config.Telemetry.Enabled,
			"workerEnabled": h.Config.TelemetryWorker.Enabled,
		},
		"uptimeSeconds": uptimeSeconds,
	}

	respondJSON(w, http.StatusOK, resp)
}

// ServicesHandler godoc
// @Summary List Services
// @Description Fetches a list of services and their status from the Graph Engine
// @Tags services
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 503 {object} map[string]string
// @Router /services [get]
func (h *Handler) ServicesHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	type svcResult struct {
		data []graph.ServiceInfo
		err  error
	}
	type healthResult struct {
		data *graph.HealthResponse
		err  error
	}

	svcChan := make(chan svcResult, 1)
	healthChan := make(chan healthResult, 1)

	go func() {
		s, e := h.GraphClient.GetServices(ctx)
		svcChan <- svcResult{s, e}
	}()

	go func() {
		h, e := h.GraphClient.CheckHealth(ctx)
		healthChan <- healthResult{h, e}
	}()

	sRes := <-svcChan
	hRes := <-healthChan

	stale := true
	var lastUpdated *int
	windowMinutes := 5

	if hRes.err == nil {
		stale = hRes.data.Stale
		lastUpdated = &hRes.data.LastUpdatedSecondsAgo
		windowMinutes = hRes.data.WindowMinutes
	}

	if sRes.err != nil {
		logger.Error("Failed to fetch services", sRes.err)
		respondJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error":                 "Failed to fetch services from Graph Engine",
			"services":              []interface{}{},
			"count":                 0,
			"stale":                 true,
			"lastUpdatedSecondsAgo": nil,
			"windowMinutes":         windowMinutes,
		})
		return
	}

	type ServiceItem struct {
		ServiceId    string                 `json:"serviceId"`
		Name         string                 `json:"name"`
		Namespace    string                 `json:"namespace"`
		PodCount     int                    `json:"podCount"`
		Availability float64                `json:"availability"`
		Placement    graph.ServicePlacement `json:"placement"`
	}

	var services []ServiceItem
	for _, s := range sRes.data {
		services = append(services, ServiceItem{
			ServiceId:    fmt.Sprintf("%s:%s", s.Namespace, s.Name),
			Name:         s.Name,
			Namespace:    s.Namespace,
			PodCount:     s.PodCount,
			Availability: s.Availability,
			Placement:    s.Placement,
		})
	}

	resp := map[string]interface{}{
		"count":                 len(services),
		"services":              services,
		"stale":                 stale,
		"lastUpdatedSecondsAgo": lastUpdated,
		"windowMinutes":         windowMinutes,
	}

	respondJSON(w, http.StatusOK, resp)
}

// NodesHandler godoc
// @Summary List Nodes
// @Description Fetches a list of infrastructure nodes and their resources from the Graph Engine
// @Tags infrastructure
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 503 {object} map[string]string
// @Router /infrastructure/nodes [get]
func (h *Handler) NodesHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nodes, err := h.GraphClient.GetNodes(ctx)
	if err != nil {
		logger.Error("Failed to fetch nodes", err)
		respondJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": "Failed to fetch nodes from Graph Engine",
			"nodes": []interface{}{},
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"nodes": nodes,
	})
}

// TopRiskHandler godoc
// @Summary Get Top Risky Services
// @Description Returns services ordered by risk metrics (pagerank or betweenness)
// @Tags risk
// @Produce json
// @Param metric query string false "Risk metric (pagerank, betweenness)" default(pagerank)
// @Param limit query int false "Number of services to return (1-20)" default(5)
// @Success 200 {object} graph.TopCentralityResponse
// @Failure 400 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /risk/services/top [get]
func (h *Handler) TopRiskHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	metric := r.URL.Query().Get("metric")
	if metric == "" {
		metric = "pagerank"
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 5
	if limitStr != "" {
		fmt.Sscanf(limitStr, "%d", &limit)
		if limit < 1 {
			limit = 1
		}
		if limit > 20 {
			limit = 20
		}
	}

	result, err := analysis.GetTopRiskServices(ctx, h.GraphClient, metric, limit)

	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "Invalid metric") {
			respondError(w, http.StatusBadRequest, errMsg)
			return
		}
		if strings.Contains(errMsg, "disabled") {
			respondError(w, http.StatusServiceUnavailable, "Graph API is not enabled")
			return
		}
		if strings.Contains(strings.ToLower(errMsg), "timeout") {
			respondError(w, http.StatusGatewayTimeout, "Graph API timeout")
			return
		}

		logger.Error("Risk analysis error", err)
		respondError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// SimulateFailureHandler godoc
// @Summary Simulate Service Failure
// @Description Simulates a failure of a specific service and analyzes the impact
// @Tags simulation
// @Accept json
// @Produce json
// @Param request body simulation.FailureSimulationRequest true "Simulation parameters"
// @Success 200 {object} simulation.FailureSimulationResult
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /simulate/failure [post]
func (h *Handler) SimulateFailureHandler(w http.ResponseWriter, r *http.Request) {
	var req simulation.FailureSimulationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	if mode == "demo" {
		snapshotID := strings.TrimSpace(r.URL.Query().Get("snapshotId"))
		result, err := loadDemoFailureResult(req, snapshotID)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		if h.SimulationService != nil {
			h.SimulationService.LogDecision(r.Context(), "failure", req, result)
		}
		respondJSON(w, http.StatusOK, result)
		return
	}

	result, err := h.SimulationService.RunFailureSimulation(r.Context(), req)
	if err != nil {
		handleSimulationError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// SimulateScalingHandler godoc
// @Summary Simulate Scaling
// @Description Simulates scaling a service and analyzes latency impact
// @Tags simulation
// @Accept json
// @Produce json
// @Param request body simulation.ScalingSimulationRequest true "Simulation parameters"
// @Success 200 {object} simulation.ScalingSimulationResult
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /simulate/scale [post]
func (h *Handler) SimulateScalingHandler(w http.ResponseWriter, r *http.Request) {
	var req simulation.ScalingSimulationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	if mode == "demo" {
		snapshotID := strings.TrimSpace(r.URL.Query().Get("snapshotId"))
		result, err := loadDemoScalingResult(req, snapshotID)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		if h.SimulationService != nil {
			h.SimulationService.LogDecision(r.Context(), "scaling", req, result)
		}
		respondJSON(w, http.StatusOK, result)
		return
	}

	result, err := h.SimulationService.RunScalingSimulation(r.Context(), req)
	if err != nil {
		handleSimulationError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func (h *Handler) SimulateContextHandler(w http.ResponseWriter, r *http.Request) {
	serviceID := strings.TrimSpace(r.URL.Query().Get("serviceId"))
	if serviceID == "" {
		respondError(w, http.StatusBadRequest, "serviceId query parameter is required")
		return
	}

	k := 2
	if rawK := strings.TrimSpace(r.URL.Query().Get("k")); rawK != "" {
		parsed, err := strconv.Atoi(rawK)
		if err != nil || parsed < 1 || parsed > 3 {
			respondError(w, http.StatusBadRequest, "k must be an integer between 1 and 3")
			return
		}
		k = parsed
	}

	direction := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("direction")))
	if direction == "" {
		direction = "both"
	}
	if direction != "both" && direction != "in" && direction != "out" {
		respondError(w, http.StatusBadRequest, "direction must be one of: both, in, out")
		return
	}

	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	if mode == "" {
		mode = "live"
	}
	if mode != "live" && mode != "demo" {
		respondError(w, http.StatusBadRequest, "mode must be one of: live, demo")
		return
	}

	canonicalServiceID, graphLookupKey := normalizeServiceIDForLookup(serviceID)
	if mode == "demo" {
		snapshotID := strings.TrimSpace(r.URL.Query().Get("snapshotId"))
		demoContext, err := loadDemoSimulationContext(canonicalServiceID, k, direction, snapshotID)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, demoContext)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	neighborhood, err := h.GraphClient.GetNeighborhoodWithOptions(ctx, graphLookupKey, k, graph.NeighborhoodOptions{
		Direction: direction,
		MaxNodes:  200,
		MaxEdges:  400,
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			respondError(w, http.StatusServiceUnavailable, "Graph Engine context lookup timed out. Switch to Demo Snapshot Mode or retry.")
			return
		}
		errMsgLower := strings.ToLower(err.Error())
		if strings.Contains(errMsgLower, "http 404") {
			respondError(w, http.StatusNotFound, fmt.Sprintf("Service %s not found in graph context", canonicalServiceID))
			return
		}
		if isContextUnavailableError(errMsgLower) {
			respondError(w, http.StatusServiceUnavailable, "Graph Engine context unavailable. Switch to Demo Snapshot Mode or retry.")
			return
		}
		logger.Error("Failed to fetch simulation context", err)
		respondError(w, http.StatusInternalServerError, "Failed to build simulation context")
		return
	}

	nodesByID := map[string]simulation.SimulationContextNode{}
	lookupByName := map[string]string{}

	for _, node := range neighborhood.Nodes {
		ns := strings.TrimSpace(node.Namespace)
		if ns == "" {
			ns = "default"
		}
		serviceNodeID := strings.TrimSpace(node.ServiceId)
		if serviceNodeID == "" {
			serviceNodeID = fmt.Sprintf("%s:%s", ns, node.Name)
		}

		nodesByID[serviceNodeID] = simulation.SimulationContextNode{
			ServiceId:    serviceNodeID,
			Name:         node.Name,
			Namespace:    ns,
			PodCount:     node.PodCount,
			Availability: node.Availability,
		}
		if node.Name != "" {
			lookupByName[node.Name] = serviceNodeID
		}
	}

	edges := make([]simulation.SimulationContextEdge, 0, len(neighborhood.Edges))
	for _, edge := range neighborhood.Edges {
		source := strings.TrimSpace(firstNonEmpty(edge.Source, edge.From))
		target := strings.TrimSpace(firstNonEmpty(edge.Target, edge.To))

		if !strings.Contains(source, ":") {
			if mapped, ok := lookupByName[source]; ok {
				source = mapped
			} else {
				source = fmt.Sprintf("default:%s", source)
			}
		}
		if !strings.Contains(target, ":") {
			if mapped, ok := lookupByName[target]; ok {
				target = mapped
			} else {
				target = fmt.Sprintf("default:%s", target)
			}
		}

		edges = append(edges, simulation.SimulationContextEdge{
			Source:    source,
			Target:    target,
			Rate:      edge.Rate,
			ErrorRate: edge.ErrorRate,
			P50:       edge.P50,
			P95:       edge.P95,
			P99:       edge.P99,
		})
	}

	if _, exists := nodesByID[canonicalServiceID]; !exists {
		if centerRef := neighborhood.CenterRef; centerRef != nil {
			ns := strings.TrimSpace(centerRef.Namespace)
			if ns == "" {
				ns = "default"
			}
			centerID := strings.TrimSpace(centerRef.ServiceId)
			if centerID == "" {
				centerID = fmt.Sprintf("%s:%s", ns, centerRef.Name)
			}
			nodesByID[centerID] = simulation.SimulationContextNode{
				ServiceId: centerID,
				Name:      centerRef.Name,
				Namespace: ns,
			}
			canonicalServiceID = centerID
		}
	}

	nodes := make([]simulation.SimulationContextNode, 0, len(nodesByID))
	for _, node := range nodesByID {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ServiceId < nodes[j].ServiceId })
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source == edges[j].Source {
			return edges[i].Target < edges[j].Target
		}
		return edges[i].Source < edges[j].Source
	})

	target := simulation.ServiceRef{
		ServiceId: canonicalServiceID,
	}
	if node, ok := nodesByID[canonicalServiceID]; ok {
		target.Name = node.Name
		target.Namespace = node.Namespace
	}

	respondJSON(w, http.StatusOK, simulation.SimulationContextResponse{
		Target:    target,
		K:         k,
		Direction: direction,
		Truncated: neighborhood.Truncated,
		Nodes:     nodes,
		Edges:     edges,
	})
}

func (h *Handler) SimulationCapabilitiesHandler(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":      []string{"failure", "scale"},
		"experimental": []string{"add-service"},
	})
}

func (h *Handler) DemoSnapshotsHandler(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"snapshots": listDemoSnapshots(),
	})
}

// SimulateAddHandler godoc
// @Summary Simulate Adding Service
// @Description Simulates adding a new service to the cluster (capacity planning)
// @Tags simulation
// @Accept json
// @Produce json
// @Param request body simulation.AddSimulationRequest true "Simulation parameters"
// @Success 200 {object} simulation.AddSimulationResult
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /simulate/add [post]
func (h *Handler) SimulateAddHandler(w http.ResponseWriter, r *http.Request) {
	var req simulation.AddSimulationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Error("Invalid request body", err)
		respondError(w, http.StatusInternalServerError, "Invalid request body")
		return
	}
	if req.CPURequest <= 0 || req.RAMRequest <= 0 || req.Replicas <= 0 {
		respondError(w, http.StatusInternalServerError, "Invalid resource requests: cpu, ram, and replicas must be positive")
		return
	}

	result, err := h.SimulationService.RunAddSimulation(r.Context(), req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "connection refused") || strings.Contains(errMsg, "request failed") {
			respondError(w, http.StatusInternalServerError, "Failed to fetch cluster state: ")
			return
		}
		handleSimulationError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, result)
}

func handleSimulationError(w http.ResponseWriter, err error) {
	errMsg := err.Error()
	if strings.Contains(errMsg, "Service not found") {
		respondError(w, http.StatusNotFound, errMsg)
		return
	}
	if strings.Contains(errMsg, "maxDepth") || strings.Contains(errMsg, "must be") || strings.Contains(errMsg, "Invalid") {
		respondError(w, http.StatusBadRequest, errMsg)
		return
	}
	if strings.Contains(errMsg, "connection refused") || strings.Contains(errMsg, "request failed") {
		respondError(w, http.StatusServiceUnavailable, "Graph API unavailable: ")
		return
	}
	if strings.Contains(errMsg, "No nodes found") {
		respondError(w, http.StatusInternalServerError, errMsg)
		return
	}

	logger.Error("Simulation error", err)
	respondError(w, http.StatusInternalServerError, "Internal server error")
}

func isContextUnavailableError(errMsgLower string) bool {
	return strings.Contains(errMsgLower, "timeout") ||
		strings.Contains(errMsgLower, "deadline exceeded") ||
		strings.Contains(errMsgLower, "request failed") ||
		strings.Contains(errMsgLower, "connection refused") ||
		strings.Contains(errMsgLower, "http 502") ||
		strings.Contains(errMsgLower, "http 503") ||
		strings.Contains(errMsgLower, "http 504")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeServiceIDForLookup(raw string) (canonical string, lookup string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "default:", ""
	}
	if strings.Contains(raw, ":") {
		parts := strings.SplitN(raw, ":", 2)
		namespace := strings.TrimSpace(parts[0])
		name := strings.TrimSpace(parts[1])
		if namespace == "" {
			namespace = "default"
		}
		return fmt.Sprintf("%s:%s", namespace, name), name
	}
	return fmt.Sprintf("default:%s", raw), raw
}
