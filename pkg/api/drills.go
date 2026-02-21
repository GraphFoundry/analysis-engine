package api

import (
	"encoding/json"
	"net/http"

	"predictive-analysis-engine/pkg/drills"
	"predictive-analysis-engine/pkg/storage"

	"github.com/go-chi/chi/v5"
)

type DrillsHandler struct {
	Engine *drills.Engine
	Store  *storage.DecisionStore
}

func (h *DrillsHandler) RegisterRoutes(r chi.Router) {
	r.Route("/drills", func(r chi.Router) {
		r.Post("/plan", h.PlanDrill)
		r.Post("/run", h.RunDrill)
		r.Get("/runs/{id}", h.GetDrillRun)
		r.Post("/runs/{id}/abort", h.AbortDrillRun)
		r.Get("/history", h.ListHistory)
	})
}

type DrillPlanRequest struct {
	Type   string          `json:"type"`
	Target string          `json:"target"`
	Config json.RawMessage `json:"config"`
}

func (h *DrillsHandler) PlanDrill(w http.ResponseWriter, r *http.Request) {
	var req DrillPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	run, err := h.Engine.PlanDrill(req.Type, req.Target, req.Config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

type DrillRunRequest struct {
	RunID string `json:"runId"`
}

func (h *DrillsHandler) RunDrill(w http.ResponseWriter, r *http.Request) {
	var req DrillRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	if err := h.Engine.ExecuteDrill(req.RunID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted", "runId": req.RunID})
}

func (h *DrillsHandler) GetDrillRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "Missing drill rum id", http.StatusBadRequest)
		return
	}

	run, err := h.Store.GetDrillRun(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if run == nil {
		http.Error(w, "Run not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

func (h *DrillsHandler) AbortDrillRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "Missing drill run id", http.StatusBadRequest)
		return
	}

	if err := h.Engine.AbortDrill(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "aborted"})
}

func (h *DrillsHandler) ListHistory(w http.ResponseWriter, r *http.Request) {
	runs, err := h.Store.ListDrillRuns(50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}
