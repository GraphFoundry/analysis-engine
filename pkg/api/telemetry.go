package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"predictive-analysis-engine/pkg/clients/telemetry"
	"predictive-analysis-engine/pkg/config"

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

	if fromStr == "" || toStr == "" {
		http.Error(w, `{"error": "Missing required parameters: from, to"}`, http.StatusBadRequest)
		return
	}

	from, err := time.Parse(time.RFC3339, fromStr)
	if err != nil {
		http.Error(w, `{"error": "Invalid timestamp format"}`, http.StatusBadRequest)
		return
	}
	to, err := time.Parse(time.RFC3339, toStr)
	if err != nil {
		http.Error(w, `{"error": "Invalid timestamp format"}`, http.StatusBadRequest)
		return
	}

	if to.Sub(from) > MaxTimeRange {
		http.Error(w, `{"error": "Time range exceeds maximum of 7 days"}`, http.StatusBadRequest)
		return
	}

	step := 60
	if stepStr != "" {
		if s, err := strconv.Atoi(stepStr); err == nil && s > 0 {
			step = s
		}
	}

	metrics, err := h.Client.GetServiceMetrics(r.Context(), service, fromStr, toStr, step)
	if err != nil {
		fmt.Printf("[Telemetry Error] Service metrics failed: %v\n", err)

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

	if fromStr == "" || toStr == "" {
		http.Error(w, `{"error": "Missing required parameters: from, to"}`, http.StatusBadRequest)
		return
	}

	from, err := time.Parse(time.RFC3339, fromStr)
	if err != nil {
		http.Error(w, `{"error": "Invalid timestamp format"}`, http.StatusBadRequest)
		return
	}
	to, err := time.Parse(time.RFC3339, toStr)
	if err != nil {
		http.Error(w, `{"error": "Invalid timestamp format"}`, http.StatusBadRequest)
		return
	}

	if to.Sub(from) > MaxTimeRange {
		http.Error(w, `{"error": "Time range exceeds maximum of 7 days"}`, http.StatusBadRequest)
		return
	}

	step := 60
	if stepStr != "" {
		if s, err := strconv.Atoi(stepStr); err == nil && s > 0 {
			step = s
		}
	}

	metrics, err := h.Client.GetEdgeMetrics(r.Context(), fromSvc, toSvc, fromStr, toStr, step)
	if err != nil {
		fmt.Printf("[Telemetry Error] Edge metrics failed: %v\n", err)
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
