package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

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
		r.Get("/catalog", h.ListScenarioCatalog)
		r.Get("/k8s-health", h.K8sHealth)
		r.Post("/plan", h.PlanDrill)
		r.Post("/run", h.RunDrill)
		r.Get("/runs/{id}", h.GetDrillRun)
		r.Post("/runs/{id}/abort", h.AbortDrillRun)
		r.Post("/runs/{id}/recover", h.RecoverDrillRun)
		r.Post("/runs/{id}/accept", h.AcceptDrillRun)
		r.Get("/history", h.ListHistory)
	})
}

type DrillPlanRequest struct {
	Type   string          `json:"type"`
	Target string          `json:"target"`
	Config json.RawMessage `json:"config"`
}

type drillScenarioCatalogResponse struct {
	Scenarios []drills.ScenarioCatalogItem `json:"scenarios"`
}

func (h *DrillsHandler) ListScenarioCatalog(w http.ResponseWriter, r *http.Request) {
	scenarios := make([]drills.ScenarioCatalogItem, 0)
	if h.Engine != nil {
		scenarios = h.Engine.ScenarioCatalog()
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(drillScenarioCatalogResponse{Scenarios: scenarios})
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
		status := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "drill preflight failed") {
			status = http.StatusPreconditionFailed
		}
		http.Error(w, err.Error(), status)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted", "runId": req.RunID})
}

type drillRunResponse struct {
	storage.DrillRun
	CanRecover       bool    `json:"canRecover,omitempty"`
	RecoveryDeadline *string `json:"recoveryDeadline,omitempty"`
	RecoveryMode     string  `json:"recoveryMode,omitempty"`
	RecoverySource   string  `json:"recoverySource,omitempty"`
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

	resp := drillRunResponse{DrillRun: *run}
	if h.Engine != nil {
		if runtime := h.Engine.RuntimeState(id); runtime != nil {
			resp.CanRecover = runtime.CanRecover
			resp.RecoveryDeadline = runtime.RecoveryDeadline
			resp.RecoveryMode = runtime.RecoveryMode
			resp.RecoverySource = runtime.RecoverySource
		}
	}
	if resp.RecoverySource == "" {
		resp.RecoverySource = inferRecoverySource(run)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *DrillsHandler) AbortDrillRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "Missing drill run id", http.StatusBadRequest)
		return
	}

	if err := h.Engine.AbortDrill(id); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, drills.ErrDrillNotActive) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "aborted"})
}

func (h *DrillsHandler) RecoverDrillRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "Missing drill run id", http.StatusBadRequest)
		return
	}

	if err := h.Engine.RecoverDrill(id); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, drills.ErrDrillNotRecoverable) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "recovering"})
}

func (h *DrillsHandler) AcceptDrillRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "Missing drill run id", http.StatusBadRequest)
		return
	}

	if err := h.Engine.AcceptDrill(id); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, drills.ErrDrillNotRecoverable) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
}

func (h *DrillsHandler) K8sHealth(w http.ResponseWriter, r *http.Request) {
	if h.Engine == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"reachable": false,
			"error":     "drill engine not initialized",
		})
		return
	}

	result := h.Engine.CheckK8sConnectivity()

	w.Header().Set("Content-Type", "application/json")
	if !result.Reachable {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	json.NewEncoder(w).Encode(result)
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

func inferRecoverySource(run *storage.DrillRun) string {
	if run == nil || len(run.Timeline) == 0 {
		return ""
	}

	for i := len(run.Timeline) - 1; i >= 0; i-- {
		step := run.Timeline[i]
		if step.Phase != "Recovery" {
			continue
		}
		msg := strings.ToLower(step.Message)
		switch {
		case strings.Contains(msg, "source: manual"):
			return "manual"
		case strings.Contains(msg, "source: failsafe"):
			return "failsafe"
		case strings.Contains(msg, "source: abort"):
			return "abort"
		}
	}
	return ""
}
