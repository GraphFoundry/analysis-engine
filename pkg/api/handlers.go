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
	"predictive-analysis-engine/pkg/middleware"
	"predictive-analysis-engine/pkg/simulation"
	"predictive-analysis-engine/pkg/storage"
)

type Handler struct {
	Config        *config.Config
	GraphClient   *graph.Client
	DecisionStore *storage.DecisionStore
	StartTime     time.Time
}

func NewHandler(cfg *config.Config, graphClient *graph.Client, decisionStore *storage.DecisionStore) *Handler {
	return &Handler{
		Config:        cfg,
		GraphClient:   graphClient,
		DecisionStore: decisionStore,
		StartTime:     time.Now(),
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

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(resp)
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
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":                 sRes.err.Error(),
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

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(resp)
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
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": errMsg})
			return
		}
		if strings.Contains(errMsg, "disabled") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"error": "Graph API is not enabled"})
			return
		}
		if strings.Contains(strings.ToLower(errMsg), "timeout") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusGatewayTimeout)
			json.NewEncoder(w).Encode(map[string]string{"error": "Graph API timeout"})
			return
		}

		logger.Error("Risk analysis error", err)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Internal server error"})
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(result)
}

func (h *Handler) SimulateFailureHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req simulation.FailureSimulationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	result, err := simulation.SimulateFailure(ctx, h.GraphClient, req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "Service not found") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": errMsg})
			return
		}
		if strings.Contains(errMsg, "maxDepth") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": errMsg})
			return
		}

		logger.Error("Simulation error", err)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Internal server error"})
		return
	}

	if h.DecisionStore != nil {
		_, err := h.DecisionStore.LogDecision(storage.LogDecisionInput{
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
			Type:          "failure",
			Scenario:      req,
			Result:        result,
			CorrelationID: middleware.GetCorrelationID(ctx),
		})
		if err != nil {
			logger.Error("Failed to log decision", err)
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(result)
}

func (h *Handler) SimulateScalingHandler(w http.ResponseWriter, r *http.Request) {
	var req simulation.ScalingSimulationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	result, err := simulation.SimulateScaling(r.Context(), h.GraphClient, h.Config, req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "Service not found") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": errMsg})
			return
		}
		if strings.Contains(errMsg, "must be") || strings.Contains(errMsg, "Invalid") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": errMsg})
			return
		}

		logger.Error("Simulation error", err)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Internal server error"})
		return
	}

	if h.DecisionStore != nil {
		_, err := h.DecisionStore.LogDecision(storage.LogDecisionInput{
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
			Type:          "scaling",
			Scenario:      req,
			Result:        result,
			CorrelationID: middleware.GetCorrelationID(r.Context()),
		})
		if err != nil {
			logger.Error("Failed to log decision", err)
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(result)
}

func (h *Handler) SimulateAddHandler(w http.ResponseWriter, r *http.Request) {
	var req simulation.AddSimulationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	ctx := r.Context()
	result, err := simulation.SimulateAddService(ctx, h.GraphClient, req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if strings.Contains(err.Error(), "must be positive") {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if h.DecisionStore != nil {
		_, err := h.DecisionStore.LogDecision(storage.LogDecisionInput{
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
			Type:          "add",
			Scenario:      req,
			Result:        result,
			CorrelationID: middleware.GetCorrelationID(r.Context()),
		})
		if err != nil {
			logger.Error("Failed to log decision", err)
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(result)
}
