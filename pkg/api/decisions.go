package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"predictive-analysis-engine/pkg/storage"
)

type DecisionsHandler struct {
	Store *storage.DecisionStore
}

func (h *DecisionsHandler) RegisterRoutes(r *chi.Mux) {
	r.Post("/decisions/log", h.LogDecision)
	r.Get("/decisions/history", h.GetHistory)
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

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = v
		}
	}

	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil {
			offset = v
		}
	}

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
