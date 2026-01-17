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

func (h *Handler) SimulateAddHandler(w http.ResponseWriter, r *http.Request) {
	var req simulation.AddSimulationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
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

	logger.Error("Simulation error", err)
	respondError(w, http.StatusInternalServerError, "Internal server error")
}
