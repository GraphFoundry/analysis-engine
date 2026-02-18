package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"predictive-analysis-engine/pkg/clients/telemetry"
	"predictive-analysis-engine/pkg/config"
	"predictive-analysis-engine/pkg/logger"

	"github.com/go-chi/chi/v5"
)

type TelemetryHandler struct {
	Client *telemetry.TelemetryClient
	Cfg    *config.Config
}

const MaxTimeRange = 7 * 24 * time.Hour

func (h *TelemetryHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/service", h.GetServiceMetrics)
	r.Get("/edges", h.GetEdgeMetrics)
	return r
}

// GetServiceMetrics godoc
// @Summary Get Service Metrics
// @Description Fetches telemetry metrics for a specific service or all services
// @Tags telemetry
// @Produce json
// @Param service query string false "Service name"
// @Param from query string true "Start timestamp (ISO 8601)"
// @Param to query string true "End timestamp (ISO 8601)"
// @Param step query int false "Step size in seconds" default(60)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /telemetry/service [get]
func (h *TelemetryHandler) GetServiceMetrics(w http.ResponseWriter, r *http.Request) {
	enabled, reason := h.Client.CheckStatus()
	if !enabled {
		http.Error(w, fmt.Sprintf(`{"error": "%s"}`, reason), http.StatusServiceUnavailable)
		return
	}

	service := r.URL.Query().Get("service")
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	stepStr := r.URL.Query().Get("step")

	if errMsg := validateTimeRangeQuery(fromStr, toStr); errMsg != "" {
		http.Error(w, fmt.Sprintf(`{"error": "%s"}`, errMsg), http.StatusBadRequest)
		return
	}

	step := parseStepQuery(stepStr)

	metrics, err := h.Client.GetServiceMetrics(r.Context(), service, fromStr, toStr, step)
	if err != nil {
		logger.Error("[Telemetry] Service metrics failed", err)

		http.Error(w, `{"error": "Internal server error"}`, http.StatusInternalServerError)
		return
	}

	resp := map[string]interface{}{
		"service":    "all",
		"from":       fromStr,
		"to":         toStr,
		"step":       step,
		"datapoints": metrics,
	}
	if service != "" {
		resp["service"] = service
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(resp)
}

// GetEdgeMetrics godoc
// @Summary Get Edge Metrics
// @Description Fetches telemetry metrics for edges between services
// @Tags telemetry
// @Produce json
// @Param fromService query string false "Source service name"
// @Param toService query string false "Target service name"
// @Param from query string true "Start timestamp (ISO 8601)"
// @Param to query string true "End timestamp (ISO 8601)"
// @Param step query int false "Step size in seconds" default(60)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /telemetry/edges [get]
func (h *TelemetryHandler) GetEdgeMetrics(w http.ResponseWriter, r *http.Request) {
	enabled, reason := h.Client.CheckStatus()
	if !enabled {
		http.Error(w, fmt.Sprintf(`{"error": "%s"}`, reason), http.StatusServiceUnavailable)
		return
	}

	fromSvc := r.URL.Query().Get("fromService")
	toSvc := r.URL.Query().Get("toService")
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	stepStr := r.URL.Query().Get("step")

	if errMsg := validateTimeRangeQuery(fromStr, toStr); errMsg != "" {
		http.Error(w, fmt.Sprintf(`{"error": "%s"}`, errMsg), http.StatusBadRequest)
		return
	}

	step := parseStepQuery(stepStr)

	metrics, err := h.Client.GetEdgeMetrics(r.Context(), fromSvc, toSvc, fromStr, toStr, step)
	if err != nil {
		logger.Error("[Telemetry] Edge metrics failed", err)
		http.Error(w, `{"error": "Internal server error"}`, http.StatusInternalServerError)
		return
	}

	resp := map[string]interface{}{
		"fromService": fromSvc,
		"toService":   toSvc,
		"from":        fromStr,
		"to":          toStr,
		"step":        step,
		"datapoints":  metrics,
	}

	if fromSvc == "" {
		delete(resp, "fromService")
	}
	if toSvc == "" {
		delete(resp, "toService")
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(resp)
}

func validateTimeRangeQuery(fromStr, toStr string) string {
	if fromStr == "" || toStr == "" {
		return "Missing required parameters: from, to"
	}

	from, err := time.Parse(time.RFC3339, fromStr)
	if err != nil {
		return "Invalid timestamp format"
	}
	to, err := time.Parse(time.RFC3339, toStr)
	if err != nil {
		return "Invalid timestamp format"
	}

	if to.Sub(from) > MaxTimeRange {
		return "Time range exceeds maximum of 7 days"
	}

	return ""
}

func parseStepQuery(stepStr string) int {
	step := 60
	if stepStr != "" {
		if s, err := strconv.Atoi(stepStr); err == nil && s > 0 {
			step = s
		}
	}
	return step
}
