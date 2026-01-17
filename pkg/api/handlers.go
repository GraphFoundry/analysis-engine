package api

import (
	"encoding/json"
	"fmt"
	"net/http"
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

	result, err := h.SimulationService.RunScalingSimulation(r.Context(), req)
	if err != nil {
		handleSimulationError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, result)
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
