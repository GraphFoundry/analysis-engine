package api

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"predictive-analysis-engine/pkg/storage"
)

// DecisionsHandler exposes decision logging and history endpoints.
type DecisionsHandler struct {
	Store *storage.DecisionStore
}

// RegisterRoutes wires decisions endpoints onto the root router.
func (h *DecisionsHandler) RegisterRoutes(r *chi.Mux) {
	r.Post("/decisions/log", h.LogDecision)
	r.Get("/decisions/history", h.GetHistory)
	r.Get("/decisions/{id}", h.GetDecisionByID)
	r.Post("/decisions/compare", h.CompareDecisions)
	r.Get("/decisions/{id}/export", h.ExportDecision)
	r.Get("/simulations/metrics", h.GetSimulationMetrics)
}

// LogDecision godoc
// @Summary Log a Decision
// @Description Logs a decision made by the system or a user
// @Tags decisions
// @Accept json
// @Produce json
// @Param request body storage.LogDecisionInput true "Decision details"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /decisions/log [post]
func (h *DecisionsHandler) LogDecision(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if h.Store == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "Decision store not available. Check SQLite configuration."})
		return
	}

	var input storage.LogDecisionInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	if input.Timestamp == "" || input.Type == "" || input.Scenario == nil || input.Result == nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Missing required fields: timestamp, type, scenario, result"})
		return
	}

	if _, err := time.Parse(time.RFC3339, input.Timestamp); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid timestamp format. Use ISO 8601 (e.g., 2026-01-04T10:00:00Z)"})
		return
	}

	validTypes := map[string]bool{"failure": true, "scaling": true, "risk": true, "add": true}
	if !validTypes[input.Type] {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid type. Must be one of: failure, scaling, risk"})
		return
	}

	record, err := h.Store.LogDecision(input)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Internal server error"})
		return
	}

	w.WriteHeader(http.StatusCreated)

	resp := map[string]interface{}{
		"id":        record.ID,
		"timestamp": record.Timestamp,
	}
	json.NewEncoder(w).Encode(resp)
}

// GetHistory godoc
// @Summary Get Decision History
// @Description Retrieves a history of logged decisions with pagination
// @Tags decisions
// @Produce json
// @Param limit query int false "Limit number of records" default(50)
// @Param offset query int false "Offset for pagination" default(0)
// @Param type query string false "Filter by decision type"
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /decisions/history [get]
func (h *DecisionsHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		http.Error(w, `{"error": "Decision store not available. Check SQLite configuration."}`, 503)
		return
	}

	limit := parseQueryInt(r.URL.Query().Get("limit"), 50)
	offset := parseQueryInt(r.URL.Query().Get("offset"), 0)

	decisionType := r.URL.Query().Get("type")

	records, err := h.Store.GetHistory(storage.GetHistoryOptions{
		Limit:  limit,
		Offset: offset,
		Type:   decisionType,
	})
	if err != nil {
		http.Error(w, `{"error": "Internal server error"}`, 500)
		return
	}
	if records == nil {
		records = []storage.DecisionRecord{}
	}

	count, err := h.Store.GetCount(decisionType)
	if err != nil {
		http.Error(w, `{"error": "Internal server error"}`, 500)
		return
	}

	resp := map[string]interface{}{
		"decisions": records,
		"pagination": map[string]interface{}{
			"limit":  limit,
			"offset": offset,
			"total":  count,
		},
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(resp)
}

func (h *DecisionsHandler) GetDecisionByID(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		http.Error(w, `{"error": "Decision store not available. Check SQLite configuration."}`, 503)
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, `{"error":"Invalid decision id"}`, 400)
		return
	}

	record, err := h.Store.GetByID(id)
	if err != nil {
		http.Error(w, `{"error":"Internal server error"}`, 500)
		return
	}
	if record == nil {
		http.Error(w, `{"error":"Decision not found"}`, 404)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(record)
}

type compareDecisionsRequest struct {
	LeftID  int64 `json:"leftId"`
	RightID int64 `json:"rightId"`
}

func (h *DecisionsHandler) CompareDecisions(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		http.Error(w, `{"error": "Decision store not available. Check SQLite configuration."}`, 503)
		return
	}

	var req compareDecisionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request body"}`, 400)
		return
	}
	if req.LeftID <= 0 || req.RightID <= 0 {
		http.Error(w, `{"error":"leftId and rightId must be positive integers"}`, 400)
		return
	}

	left, err := h.Store.GetByID(req.LeftID)
	if err != nil {
		http.Error(w, `{"error":"Internal server error"}`, 500)
		return
	}
	if left == nil {
		http.Error(w, `{"error":"Left decision not found"}`, 404)
		return
	}

	right, err := h.Store.GetByID(req.RightID)
	if err != nil {
		http.Error(w, `{"error":"Internal server error"}`, 500)
		return
	}
	if right == nil {
		http.Error(w, `{"error":"Right decision not found"}`, 404)
		return
	}

	leftAffected := estimateAffectedServices(*left)
	rightAffected := estimateAffectedServices(*right)
	leftConfidence := extractString(left.Result, "confidence")
	rightConfidence := extractString(right.Result, "confidence")

	var latencyDeltaDiff *float64
	if leftLatencyDelta, ok := extractLatencyDeltaMs(left.Result); ok {
		if rightLatencyDelta, ok2 := extractLatencyDeltaMs(right.Result); ok2 {
			diff := rightLatencyDelta - leftLatencyDelta
			latencyDeltaDiff = &diff
		}
	}

	resp := map[string]interface{}{
		"left":  left,
		"right": right,
		"summary": map[string]interface{}{
			"leftType":              left.Type,
			"rightType":             right.Type,
			"affectedServicesLeft":  leftAffected,
			"affectedServicesRight": rightAffected,
			"affectedServicesDelta": rightAffected - leftAffected,
			"leftConfidence":        leftConfidence,
			"rightConfidence":       rightConfidence,
			"confidenceChanged":     leftConfidence != rightConfidence,
			"latencyDeltaDiffMs":    latencyDeltaDiff,
			"scenarioServiceLeft":   extractString(left.Scenario, "serviceId"),
			"scenarioServiceRight":  extractString(right.Scenario, "serviceId"),
			"scenarioDepthLeft":     extractNumber(left.Scenario, "maxDepth"),
			"scenarioDepthRight":    extractNumber(right.Scenario, "maxDepth"),
			"generatedAt":           time.Now().UTC().Format(time.RFC3339),
		},
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *DecisionsHandler) ExportDecision(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		http.Error(w, `{"error": "Decision store not available. Check SQLite configuration."}`, 503)
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, `{"error":"Invalid decision id"}`, 400)
		return
	}

	record, err := h.Store.GetByID(id)
	if err != nil {
		http.Error(w, `{"error":"Internal server error"}`, 500)
		return
	}
	if record == nil {
		http.Error(w, `{"error":"Decision not found"}`, 404)
		return
	}

	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "json"
	}

	if format == "json" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"decision-%d.json\"", id))
		_ = json.NewEncoder(w).Encode(record)
		return
	}

	if format != "csv" {
		http.Error(w, `{"error":"Unsupported export format. Use json or csv."}`, 400)
		return
	}

	scenarioMap := toMap(record.Scenario)
	resultMap := toMap(record.Result)
	latencyDelta, _ := extractLatencyDeltaMs(resultMap)

	var csvBuffer bytes.Buffer
	writer := csv.NewWriter(&csvBuffer)
	_ = writer.Write([]string{
		"id", "timestamp", "type", "serviceId", "maxDepth", "currentPods", "newPods",
		"confidence", "affectedCallers", "affectedDownstream", "affectedPaths",
		"totalLostTrafficRps", "latencyDeltaMs", "correlationId",
	})
	_ = writer.Write([]string{
		fmt.Sprintf("%d", record.ID),
		record.Timestamp,
		record.Type,
		extractString(scenarioMap, "serviceId"),
		fmt.Sprintf("%.0f", extractNumber(scenarioMap, "maxDepth")),
		fmt.Sprintf("%.0f", extractNumber(scenarioMap, "currentPods")),
		fmt.Sprintf("%.0f", extractNumber(scenarioMap, "newPods")),
		extractString(resultMap, "confidence"),
		fmt.Sprintf("%d", countArrayItems(resultMap, "affectedCallers")),
		fmt.Sprintf("%d", countArrayItems(resultMap, "affectedDownstream")),
		fmt.Sprintf("%d", countArrayItems(resultMap, "affectedPaths")),
		fmt.Sprintf("%.2f", extractNumber(resultMap, "totalLostTrafficRps")),
		fmt.Sprintf("%.2f", latencyDelta),
		record.CorrelationID,
	})
	writer.Flush()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"decision-%d.csv\"", id))
	_, _ = w.Write(csvBuffer.Bytes())
}

func (h *DecisionsHandler) GetSimulationMetrics(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		http.Error(w, `{"error": "Decision store not available. Check SQLite configuration."}`, 503)
		return
	}

	window := r.URL.Query().Get("window")
	if window == "" {
		window = "7d"
	}
	duration, ok := parseWindow(window)
	if !ok {
		http.Error(w, `{"error":"Invalid window. Use format like 24h, 7d, 30d."}`, 400)
		return
	}

	since := time.Now().UTC().Add(-duration)
	records, err := h.Store.GetHistorySince(since)
	if err != nil {
		http.Error(w, `{"error":"Internal server error"}`, 500)
		return
	}

	runs := len(records)
	failureRuns := 0
	scaleRuns := 0
	lowConfidenceRuns := 0
	totalAffected := 0.0
	affectedSampleCount := 0.0
	totalLatencyDelta := 0.0
	latencySampleCount := 0.0

	type trendBucket struct {
		Runs        int `json:"runs"`
		FailureRuns int `json:"failureRuns"`
		ScaleRuns   int `json:"scaleRuns"`
	}
	trend := map[string]*trendBucket{}

	for _, record := range records {
		resultMap := toMap(record.Result)
		recordType := strings.ToLower(record.Type)
		if recordType == "failure" {
			failureRuns++
		}
		if recordType == "scaling" || recordType == "scale" {
			scaleRuns++
		}

		if strings.EqualFold(extractString(resultMap, "confidence"), "low") {
			lowConfidenceRuns++
		}

		affected := float64(estimateAffectedServices(record))
		if affected > 0 {
			totalAffected += affected
			affectedSampleCount++
		}

		if delta, ok := extractLatencyDeltaMs(resultMap); ok {
			totalLatencyDelta += delta
			latencySampleCount++
		}

		ts, err := time.Parse(time.RFC3339, record.Timestamp)
		if err != nil {
			continue
		}
		bucketKey := ts.UTC().Format("2006-01-02")
		if _, exists := trend[bucketKey]; !exists {
			trend[bucketKey] = &trendBucket{}
		}
		trend[bucketKey].Runs++
		if recordType == "failure" {
			trend[bucketKey].FailureRuns++
		}
		if recordType == "scaling" || recordType == "scale" {
			trend[bucketKey].ScaleRuns++
		}
	}

	avgAffected := 0.0
	if affectedSampleCount > 0 {
		avgAffected = totalAffected / affectedSampleCount
	}
	avgLatencyDelta := 0.0
	if latencySampleCount > 0 {
		avgLatencyDelta = totalLatencyDelta / latencySampleCount
	}

	orderedKeys := make([]string, 0, len(trend))
	for key := range trend {
		orderedKeys = append(orderedKeys, key)
	}
	sort.Strings(orderedKeys)

	trendSeries := make([]map[string]interface{}, 0, len(orderedKeys))
	for _, key := range orderedKeys {
		bucket := trend[key]
		trendSeries = append(trendSeries, map[string]interface{}{
			"date":        key,
			"runs":        bucket.Runs,
			"failureRuns": bucket.FailureRuns,
			"scaleRuns":   bucket.ScaleRuns,
		})
	}

	resp := map[string]interface{}{
		"window":              window,
		"runs":                runs,
		"failureRuns":         failureRuns,
		"scaleRuns":           scaleRuns,
		"avgAffectedServices": roundFloat(avgAffected, 2),
		"avgLatencyDeltaMs":   roundFloat(avgLatencyDelta, 2),
		"lowConfidenceRuns":   lowConfidenceRuns,
		"trend":               trendSeries,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

func parseQueryInt(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func toMap(value interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	switch typed := value.(type) {
	case map[string]interface{}:
		return typed
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return map[string]interface{}{}
		}
		out := map[string]interface{}{}
		if err := json.Unmarshal(raw, &out); err != nil {
			return map[string]interface{}{}
		}
		return out
	}
}

func extractString(value interface{}, key string) string {
	m := toMap(value)
	raw, ok := m[key]
	if !ok || raw == nil {
		return ""
	}
	if s, ok := raw.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", raw)
}

func extractNumber(value interface{}, key string) float64 {
	m := toMap(value)
	raw, ok := m[key]
	if !ok || raw == nil {
		return 0
	}
	switch typed := raw.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		f, _ := typed.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(typed, 64)
		return f
	default:
		return 0
	}
}

func countArrayItems(value interface{}, key string) int {
	m := toMap(value)
	raw, ok := m[key]
	if !ok || raw == nil {
		return 0
	}
	switch typed := raw.(type) {
	case []interface{}:
		return len(typed)
	case map[string]interface{}:
		if items, ok := typed["items"].([]interface{}); ok {
			return len(items)
		}
	}
	return 0
}

func estimateAffectedServices(record storage.DecisionRecord) int {
	resultMap := toMap(record.Result)
	if strings.EqualFold(record.Type, "failure") {
		return countArrayItems(resultMap, "affectedCallers") + countArrayItems(resultMap, "affectedDownstream")
	}
	if strings.EqualFold(record.Type, "scaling") || strings.EqualFold(record.Type, "scale") {
		return countArrayItems(resultMap, "affectedPaths")
	}
	return 0
}

func extractLatencyDeltaMs(result interface{}) (float64, bool) {
	resultMap := toMap(result)
	raw, ok := resultMap["latencyEstimate"]
	if !ok || raw == nil {
		return 0, false
	}
	latencyMap := toMap(raw)
	if delta, ok := latencyMap["deltaMs"]; ok {
		switch typed := delta.(type) {
		case float64:
			return typed, true
		case int:
			return float64(typed), true
		case json.Number:
			f, err := typed.Float64()
			return f, err == nil
		}
	}
	return 0, false
}

func parseWindow(window string) (time.Duration, bool) {
	window = strings.TrimSpace(strings.ToLower(window))
	if window == "" {
		return 7 * 24 * time.Hour, true
	}
	if strings.HasSuffix(window, "d") {
		daysText := strings.TrimSuffix(window, "d")
		days, err := strconv.Atoi(daysText)
		if err != nil || days <= 0 {
			return 0, false
		}
		return time.Duration(days) * 24 * time.Hour, true
	}
	if strings.HasSuffix(window, "h") {
		hoursText := strings.TrimSuffix(window, "h")
		hours, err := strconv.Atoi(hoursText)
		if err != nil || hours <= 0 {
			return 0, false
		}
		return time.Duration(hours) * time.Hour, true
	}
	return 0, false
}

func roundFloat(value float64, decimals int) float64 {
	factor := 1.0
	for i := 0; i < decimals; i++ {
		factor *= 10
	}
	return float64(int(value*factor+0.5)) / factor
}
