package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"predictive-analysis-engine/pkg/clients/graph"
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
		r.Get("/runs/{id}/snapshot", h.GetDrillRunSnapshot)
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

type drillRunSnapshotResponse struct {
	RunID             string                         `json:"runId"`
	SnapshotTimestamp string                         `json:"snapshotTimestamp"`
	VMState           drillRunVMSnapshot             `json:"vmState"`
	BackendMetrics    drillRunBackendMetricsSnapshot `json:"backendMetrics"`
	DashboardMetrics  drillRunDashboardSnapshot      `json:"dashboardMetrics"`
	GraphSummary      drillRunGraphSummarySnapshot   `json:"graphSummary"`
}

type drillRunVMSnapshot struct {
	Status           string  `json:"status"`
	Verdict          string  `json:"verdict"`
	Target           string  `json:"target"`
	SourceTimestamp  *string `json:"sourceTimestamp,omitempty"`
	CanRecover       bool    `json:"canRecover"`
	RecoveryDeadline *string `json:"recoveryDeadline,omitempty"`
	RecoveryMode     string  `json:"recoveryMode,omitempty"`
	RecoverySource   string  `json:"recoverySource,omitempty"`
}

type drillRunBackendMetricsSnapshot struct {
	TargetService   string                       `json:"targetService"`
	SourceTimestamp *string                      `json:"sourceTimestamp,omitempty"`
	Baseline        *drillRunServiceMetricValues `json:"baseline,omitempty"`
	Final           *drillRunServiceMetricValues `json:"final,omitempty"`
}

type drillRunDashboardSnapshot struct {
	Source          string                       `json:"source"`
	SourceTimestamp *string                      `json:"sourceTimestamp,omitempty"`
	Baseline        *drillRunServiceMetricValues `json:"baseline,omitempty"`
	Final           *drillRunServiceMetricValues `json:"final,omitempty"`
}

type drillRunGraphSummarySnapshot struct {
	ServiceCount    int                          `json:"serviceCount"`
	EdgeCount       int                          `json:"edgeCount"`
	SourceTimestamp *string                      `json:"sourceTimestamp,omitempty"`
	Target          *drillRunServiceMetricValues `json:"target,omitempty"`
}

type drillRunServiceMetricValues struct {
	Service      string  `json:"service"`
	Namespace    string  `json:"namespace,omitempty"`
	RPS          float64 `json:"rps"`
	ErrorRate    float64 `json:"errorRate"`
	P95          float64 `json:"p95"`
	Availability float64 `json:"availability"`
	PodCount     int     `json:"podCount"`
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

func (h *DrillsHandler) GetDrillRunSnapshot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "Missing drill run id", http.StatusBadRequest)
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

	targetService, targetNamespace := resolveDrillTarget(run)
	preSnapshot := decodeDrillMetricsSnapshot(run.PreSnapshot)
	postSnapshot := decodeDrillMetricsSnapshot(run.PostSnapshot)
	preTimestamp := extractDrillSnapshotTimestamp(preSnapshot)
	postTimestamp := extractDrillSnapshotTimestamp(postSnapshot)

	baseline := extractDrillServiceMetrics(preSnapshot, targetService, targetNamespace)
	final := extractDrillServiceMetrics(postSnapshot, targetService, targetNamespace)

	vmState := drillRunVMSnapshot{
		Status:          run.Status,
		Verdict:         run.Verdict,
		Target:          run.Target,
		SourceTimestamp: extractDrillRunSourceTimestamp(run),
	}
	if h.Engine != nil {
		if runtime := h.Engine.RuntimeState(id); runtime != nil {
			vmState.CanRecover = runtime.CanRecover
			vmState.RecoveryDeadline = runtime.RecoveryDeadline
			vmState.RecoveryMode = runtime.RecoveryMode
			vmState.RecoverySource = runtime.RecoverySource
		}
	}
	if vmState.RecoverySource == "" {
		vmState.RecoverySource = inferRecoverySource(run)
	}

	graphSnapshot := postSnapshot
	if graphSnapshot == nil {
		graphSnapshot = preSnapshot
	}
	graphTimestamp := extractDrillSnapshotTimestamp(graphSnapshot)
	metricsTimestamp := chooseDrillSourceTimestamp(postTimestamp, preTimestamp)

	resp := drillRunSnapshotResponse{
		RunID:             run.ID,
		SnapshotTimestamp: time.Now().UTC().Format(time.RFC3339),
		VMState:           vmState,
		BackendMetrics: drillRunBackendMetricsSnapshot{
			TargetService:   targetService,
			SourceTimestamp: metricsTimestamp,
			Baseline:        baseline,
			Final:           final,
		},
		DashboardMetrics: drillRunDashboardSnapshot{
			Source:          "drill_run_snapshots",
			SourceTimestamp: metricsTimestamp,
			Baseline:        baseline,
			Final:           final,
		},
		GraphSummary: buildDrillGraphSummary(graphSnapshot, targetService, targetNamespace, graphTimestamp),
	}

	w.Header().Set("Cache-Control", "no-store")
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

func resolveDrillTarget(run *storage.DrillRun) (service string, namespace string) {
	if run == nil {
		return "", ""
	}

	service = strings.TrimSpace(run.Target)
	namespace = ""

	type drillRunConfig struct {
		Namespace string `json:"namespace"`
	}
	var cfg drillRunConfig
	if len(bytes.TrimSpace(run.Config)) > 0 {
		if err := json.Unmarshal(run.Config, &cfg); err == nil {
			namespace = strings.TrimSpace(cfg.Namespace)
		}
	}

	if strings.Contains(service, "/") {
		parts := strings.SplitN(service, "/", 2)
		if len(parts) == 2 {
			if namespace == "" {
				namespace = strings.TrimSpace(parts[0])
			}
			service = strings.TrimSpace(parts[1])
		}
	}

	return service, namespace
}

func decodeDrillMetricsSnapshot(raw json.RawMessage) *graph.MetricsSnapshotResponse {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	var snapshot graph.MetricsSnapshotResponse
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil
	}
	return &snapshot
}

func extractDrillServiceMetrics(snapshot *graph.MetricsSnapshotResponse, service, namespace string) *drillRunServiceMetricValues {
	if snapshot == nil || strings.TrimSpace(service) == "" {
		return nil
	}

	normalizedService := strings.TrimSpace(service)
	normalizedNamespace := strings.TrimSpace(namespace)

	for i := range snapshot.Services {
		candidate := snapshot.Services[i]
		if !strings.EqualFold(candidate.Name, normalizedService) {
			continue
		}
		if normalizedNamespace != "" && !strings.EqualFold(candidate.Namespace, normalizedNamespace) {
			continue
		}

		return &drillRunServiceMetricValues{
			Service:      candidate.Name,
			Namespace:    candidate.Namespace,
			RPS:          candidate.RPS,
			ErrorRate:    candidate.ErrorRate,
			P95:          candidate.P95,
			Availability: candidate.Availability.Value,
			PodCount:     candidate.PodCount.Value,
		}
	}

	if normalizedNamespace == "" {
		return nil
	}

	for i := range snapshot.Services {
		candidate := snapshot.Services[i]
		if !strings.EqualFold(candidate.Name, normalizedService) {
			continue
		}
		return &drillRunServiceMetricValues{
			Service:      candidate.Name,
			Namespace:    candidate.Namespace,
			RPS:          candidate.RPS,
			ErrorRate:    candidate.ErrorRate,
			P95:          candidate.P95,
			Availability: candidate.Availability.Value,
			PodCount:     candidate.PodCount.Value,
		}
	}

	return nil
}

func buildDrillGraphSummary(snapshot *graph.MetricsSnapshotResponse, service, namespace string, sourceTimestamp *string) drillRunGraphSummarySnapshot {
	if snapshot == nil {
		return drillRunGraphSummarySnapshot{}
	}

	return drillRunGraphSummarySnapshot{
		ServiceCount:    len(snapshot.Services),
		EdgeCount:       len(snapshot.Edges),
		SourceTimestamp: sourceTimestamp,
		Target:          extractDrillServiceMetrics(snapshot, service, namespace),
	}
}

func extractDrillSnapshotTimestamp(snapshot *graph.MetricsSnapshotResponse) *string {
	if snapshot == nil {
		return nil
	}
	ts := strings.TrimSpace(snapshot.Timestamp)
	if ts == "" {
		return nil
	}
	return &ts
}

func extractDrillRunSourceTimestamp(run *storage.DrillRun) *string {
	if run == nil {
		return nil
	}
	if run.EndTime != nil {
		end := strings.TrimSpace(*run.EndTime)
		if end != "" {
			return &end
		}
	}
	start := strings.TrimSpace(run.StartTime)
	if start == "" {
		return nil
	}
	return &start
}

func chooseDrillSourceTimestamp(primary, fallback *string) *string {
	if primary != nil {
		return primary
	}
	return fallback
}
