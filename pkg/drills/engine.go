package drills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"predictive-analysis-engine/pkg/clients/graph"
	"predictive-analysis-engine/pkg/clients/telemetry"
	"predictive-analysis-engine/pkg/storage"

	"github.com/google/uuid"
)

const (
	StatusPlanned    = "Planned"
	StatusRunning    = "Running"
	StatusObserving  = "Observing"
	StatusRecovering = "Recovering"
	StatusCompleted  = "Completed"
	StatusAborted    = "Aborted"
	StatusFailed     = "Failed"
)

type Engine struct {
	store           *storage.DecisionStore
	graphClient     *graph.Client
	telemetryClient *telemetry.TelemetryClient
	actions         map[string]Action
	active          sync.Map // maps runID -> context.CancelFunc
}

func NewEngine(store *storage.DecisionStore, graphClient *graph.Client, telemetryClient *telemetry.TelemetryClient) *Engine {
	e := &Engine{
		store:           store,
		graphClient:     graphClient,
		telemetryClient: telemetryClient,
		actions:         make(map[string]Action),
	}
	e.actions["ServiceShutdown"] = NewScaleDeploymentAction()
	e.actions["ScaleStress"] = NewScaleDeploymentAction()
	e.actions["NetworkCut"] = NewNetworkPolicyAction()
	e.actions["TargetedLoad"] = NewMockAction("Simulating artificial traffic spikes")
	return e
}

type RunConfig struct {
	Namespace     string `json:"namespace"`
	ObserveTokens int    `json:"observeTokens"` // seconds to observe before rollback
}

func (e *Engine) PlanDrill(drillType, target string, config json.RawMessage) (*storage.DrillRun, error) {
	id := uuid.New().String()

	run := storage.DrillRun{
		ID:        id,
		Type:      drillType,
		Target:    target,
		Status:    StatusPlanned,
		StartTime: time.Now().UTC().Format(time.RFC3339),
		Config:    config,
		Verdict:   "Pending",
	}

	if err := e.store.InsertDrillRun(run); err != nil {
		return nil, fmt.Errorf("failed to save planned drill: %w", err)
	}

	return &run, nil
}

func (e *Engine) ExecuteDrill(runID string) error {
	run, err := e.store.GetDrillRun(runID)
	if err != nil || run == nil {
		return fmt.Errorf("run not found or error: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	e.active.Store(runID, cancel)

	go e.runStateMachine(ctx, run)

	return nil
}

func (e *Engine) runStateMachine(ctx context.Context, run *storage.DrillRun) {
	defer e.active.Delete(run.ID)

	e.updateStatus(run, StatusRunning)
	e.logStep(run.ID, "Validate", "Starting drill validation", "Ok")

	action, exists := e.actions[run.Type]
	if !exists {
		e.failRun(run, "Validate", "Unsupported drill type")
		return
	}

	var parsedConfig RunConfig
	if err := json.Unmarshal(run.Config, &parsedConfig); err != nil {
		e.failRun(run, "Validate", "Invalid configuration format")
		return
	}

	namespace := parsedConfig.Namespace
	if namespace == "" {
		parts := strings.Split(run.Target, "/")
		if len(parts) == 2 {
			namespace = parts[0]
			run.Target = parts[1]
		} else {
			namespace = "default"
		}
	}

	// 1. Warmup Snapshot
	e.logStep(run.ID, "Warmup", "Capturing baseline metrics", "Ok")
	if snapshot, err := e.captureSnapshot(ctx); err == nil {
		run.PreSnapshot = snapshot
	} else {
		e.logStep(run.ID, "Warmup", fmt.Sprintf("Warning: Snapshot failed: %v", err), "Warn")
	}
	e.store.UpdateDrillRun(*run)

	// 2. Action
	e.logStep(run.ID, "Action", fmt.Sprintf("Executing action: %s on %s", run.Type, run.Target), "Ok")
	if err := action.Execute(ctx, namespace, run.Target, run.Config); err != nil {
		e.failRun(run, "Action", fmt.Sprintf("Failed to execute action: %v", err))
		return
	}

	e.updateStatus(run, StatusObserving)

	// 3. Observe Window
	observeTime := time.Duration(parsedConfig.ObserveTokens) * time.Second
	if observeTime <= 0 {
		observeTime = 15 * time.Second
	}

	e.logStep(run.ID, "Observation", fmt.Sprintf("Observing system for %v", observeTime), "Ok")

	select {
	case <-ctx.Done():
		// Aborted early!
		e.logStep(run.ID, "Observation", "Drill aborted by user during observation", "Warn")
		run.Verdict = "Aborted"
	case <-time.After(observeTime):
		// Natural progression
		run.Verdict = "Success"
	}

	// 4. Recovery
	e.updateStatus(run, StatusRecovering)
	e.logStep(run.ID, "Recovery", "Initiating automatic rollback", "Ok")

	// Use background context for rollback just in case main ctx was cancelled
	if err := action.Rollback(context.Background(), namespace, run.Target, run.Config); err != nil {
		e.logStep(run.ID, "Recovery", fmt.Sprintf("Rollback failed: %v", err), "Error")
		run.Verdict = "Partial/Fail"
	} else {
		e.logStep(run.ID, "Recovery", "Rollback successful", "Ok")
	}

	// 5. Post Snapshot
	e.logStep(run.ID, "PostSnapshot", "Capturing final metrics", "Ok")
	if snapshot, err := e.captureSnapshot(ctx); err == nil {
		run.PostSnapshot = snapshot
	} else {
		e.logStep(run.ID, "PostSnapshot", fmt.Sprintf("Warning: Snapshot failed: %v", err), "Warn")
	}

	// Finalize
	endTime := time.Now().UTC().Format(time.RFC3339)
	run.EndTime = &endTime
	if run.Verdict == "Aborted" {
		e.updateStatus(run, StatusAborted)
	} else {
		e.updateStatus(run, StatusCompleted)
	}
	e.logStep(run.ID, "Finalize", "Drill completed", "Ok")
}

func (e *Engine) captureSnapshot(ctx context.Context) (json.RawMessage, error) {
	if e.graphClient == nil {
		return nil, fmt.Errorf("graph client not initialized")
	}
	snapshot, err := e.graphClient.GetMetricsSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(snapshot)
}

func (e *Engine) AbortDrill(runID string) error {
	cancel, ok := e.active.Load(runID)
	if !ok {
		return fmt.Errorf("drill %s is not actively running", runID)
	}
	cancel.(context.CancelFunc)()
	return nil
}

func (e *Engine) updateStatus(run *storage.DrillRun, status string) {
	run.Status = status
	e.store.UpdateDrillRun(*run)
}

func (e *Engine) logStep(runID, phase, message, status string) {
	e.store.AddDrillStep(storage.DrillStep{
		RunID:     runID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Phase:     phase,
		Message:   message,
		Status:    status,
	})
}

func (e *Engine) failRun(run *storage.DrillRun, phase, reason string) {
	e.logStep(run.ID, phase, reason, "Error")
	run.Verdict = "Failed"
	endTime := time.Now().UTC().Format(time.RFC3339)
	run.EndTime = &endTime
	e.updateStatus(run, StatusFailed)
}
