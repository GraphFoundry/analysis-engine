package drills

import (
	"context"
	"encoding/json"
	"errors"
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
	StatusPlanned          = "Planned"
	StatusRunning          = "Running"
	StatusObserving        = "Observing"
	StatusAwaitingRecovery = "AwaitingRecovery"
	StatusRecovering       = "Recovering"
	StatusCompleted        = "Completed"
	StatusAborted          = "Aborted"
	StatusFailed           = "Failed"

	recoveryModeManualWithFailsafe = "manual_with_failsafe"
	recoveryFailsafeTimeout        = 5 * time.Minute
)

var (
	ErrDrillNotActive      = errors.New("drill is not actively running")
	ErrDrillNotRecoverable = errors.New("drill is not awaiting recovery")
)

type recoveryTrigger struct {
	Source      string
	MarkAborted bool
}

type DrillRuntimeState struct {
	CanRecover       bool    `json:"canRecover,omitempty"`
	RecoveryDeadline *string `json:"recoveryDeadline,omitempty"`
	RecoveryMode     string  `json:"recoveryMode,omitempty"`
	RecoverySource   string  `json:"recoverySource,omitempty"`
}

type drillSession struct {
	runID     string
	cancel    context.CancelFunc
	recoverCh chan recoveryTrigger

	mu               sync.Mutex
	action           Action
	namespace        string
	awaitingRecovery bool
	recoveryStarted  bool
	recoveryDeadline time.Time
	recoverySource   string
	recoveryAborted  bool
}

func newDrillSession(runID string, cancel context.CancelFunc) *drillSession {
	return &drillSession{
		runID:     runID,
		cancel:    cancel,
		recoverCh: make(chan recoveryTrigger, 1),
	}
}

func (s *drillSession) setActionContext(action Action, namespace string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.action = action
	s.namespace = namespace
}

func (s *drillSession) beginAwaitingRecovery(deadline time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.awaitingRecovery = true
	s.recoveryStarted = false
	s.recoveryDeadline = deadline.UTC()
}

func (s *drillSession) requestRecovery(trigger recoveryTrigger) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.recoveryStarted {
		return false, nil
	}
	if !s.awaitingRecovery {
		return false, ErrDrillNotRecoverable
	}

	s.awaitingRecovery = false
	s.recoveryStarted = true
	s.recoverySource = trigger.Source
	s.recoveryAborted = trigger.MarkAborted

	select {
	case s.recoverCh <- trigger:
	default:
	}

	return true, nil
}

func (s *drillSession) beginFailsafeRecovery() recoveryTrigger {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.recoveryStarted {
		return recoveryTrigger{Source: s.recoverySource, MarkAborted: s.recoveryAborted}
	}

	s.awaitingRecovery = false
	s.recoveryStarted = true
	s.recoverySource = "failsafe"
	s.recoveryAborted = false
	return recoveryTrigger{Source: "failsafe"}
}

func (s *drillSession) runtimeState() DrillRuntimeState {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := DrillRuntimeState{
		RecoveryMode:   recoveryModeManualWithFailsafe,
		RecoverySource: s.recoverySource,
	}

	if s.awaitingRecovery {
		state.CanRecover = true
		deadline := s.recoveryDeadline.UTC().Format(time.RFC3339)
		state.RecoveryDeadline = &deadline
	}

	return state
}

type Engine struct {
	store           *storage.DecisionStore
	graphClient     *graph.Client
	telemetryClient *telemetry.TelemetryClient
	k8sClients      *K8sClientFactory
	actionFactories map[string]func() Action
	active          sync.Map // maps runID -> *drillSession
}

type EngineOptions struct {
	K8sClientOptions K8sClientOptions
	TargetedLoad     TargetedLoadActionOptions
}

func NewEngine(store *storage.DecisionStore, graphClient *graph.Client, telemetryClient *telemetry.TelemetryClient, options ...EngineOptions) *Engine {
	var opts EngineOptions
	if len(options) > 0 {
		opts = options[0]
	}

	k8sClients := NewK8sClientFactory(opts.K8sClientOptions)

	e := &Engine{
		store:           store,
		graphClient:     graphClient,
		telemetryClient: telemetryClient,
		k8sClients:      k8sClients,
		actionFactories: make(map[string]func() Action),
	}
	e.actionFactories["ServiceShutdown"] = func() Action { return NewScaleDeploymentAction(k8sClients) }
	e.actionFactories["ServiceBrownout"] = func() Action { return NewScaleDeploymentAction(k8sClients) }
	e.actionFactories["ScaleStress"] = func() Action { return NewScaleDeploymentAction(k8sClients) }
	e.actionFactories["NetworkCut"] = func() Action { return NewNetworkPolicyAction(k8sClients) }
	e.actionFactories["ExtendedNetworkCut"] = func() Action { return NewNetworkPolicyAction(k8sClients) }
	e.actionFactories["TargetedLoad"] = func() Action { return NewTargetedLoadAction(opts.TargetedLoad, k8sClients) }
	e.actionFactories["TrafficSpike"] = func() Action { return NewTargetedLoadAction(opts.TargetedLoad, k8sClients) }
	return e
}

type RunConfig struct {
	Namespace     string `json:"namespace"`
	ObserveTokens int    `json:"observeTokens"` // seconds to observe before recovery gate
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

	if err := e.preflightExecuteDrill(run); err != nil {
		e.failRun(run, "Validate", err.Error())
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	session := newDrillSession(runID, cancel)
	e.active.Store(runID, session)

	go e.runStateMachine(ctx, run, session)

	return nil
}

func (e *Engine) preflightExecuteDrill(run *storage.DrillRun) error {
	if run == nil {
		return fmt.Errorf("drill preflight failed: nil run")
	}
	if e.k8sClients == nil {
		// Use default resolution path so preflight still validates env/kubeconfig state.
		e.k8sClients = NewK8sClientFactory(K8sClientOptions{})
	}

	parsedConfig, namespace, targetName, err := e.parseRunConfigAndTarget(run)
	if err != nil {
		return fmt.Errorf("drill preflight failed: invalid run configuration: %w", err)
	}

	_ = parsedConfig // reserved for future preflight checks
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch run.Type {
	case "ServiceShutdown", "ServiceBrownout", "ScaleStress":
		if targetName == "" {
			return fmt.Errorf("drill preflight failed: empty deployment target for %s", run.Type)
		}
		return e.k8sClients.PreflightDeploymentAccess(ctx, namespace, targetName)
	case "NetworkCut", "ExtendedNetworkCut", "TargetedLoad", "TrafficSpike":
		return e.k8sClients.PreflightNamespaceAccess(ctx, namespace)
	default:
		// Unsupported drill types are handled by the state machine validation path.
		return nil
	}
}

func (e *Engine) parseRunConfigAndTarget(run *storage.DrillRun) (RunConfig, string, string, error) {
	var parsedConfig RunConfig
	if run == nil {
		return parsedConfig, "", "", fmt.Errorf("nil run")
	}

	if err := json.Unmarshal(run.Config, &parsedConfig); err != nil {
		return parsedConfig, "", "", err
	}

	namespace := parsedConfig.Namespace
	target := run.Target
	if namespace == "" {
		parts := strings.Split(run.Target, "/")
		if len(parts) == 2 {
			namespace = parts[0]
			target = parts[1]
		} else {
			namespace = "default"
		}
	}

	return parsedConfig, namespace, target, nil
}

func (e *Engine) runStateMachine(ctx context.Context, run *storage.DrillRun, session *drillSession) {
	defer e.active.Delete(run.ID)

	e.updateStatus(run, StatusRunning)
	e.logStep(run.ID, "Validate", "Starting drill validation", "Ok")

	actionFactory, exists := e.actionFactories[run.Type]
	if !exists {
		e.failRun(run, "Validate", "Unsupported drill type")
		return
	}
	action := actionFactory()

	parsedConfig, namespace, targetName, err := e.parseRunConfigAndTarget(run)
	if err != nil {
		e.failRun(run, "Validate", "Invalid configuration format")
		return
	}
	run.Target = targetName
	session.setActionContext(action, namespace)

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

	recovery := recoveryTrigger{Source: "failsafe"}
	select {
	case <-ctx.Done():
		// Aborted early -> recover immediately.
		e.logStep(run.ID, "Observation", "Drill aborted by user during observation", "Warn")
		run.Verdict = "Aborted"
		recovery = recoveryTrigger{Source: "abort", MarkAborted: true}
	case <-time.After(observeTime):
		run.Verdict = "Success"
		recovery = e.awaitRecoveryAuthorization(ctx, run, session)
		if recovery.MarkAborted {
			run.Verdict = "Aborted"
		}
	}

	// 4. Recovery
	e.updateStatus(run, StatusRecovering)
	e.logStep(run.ID, "Recovery", e.recoveryInitiationMessage(recovery), "Ok")

	// Use background context for rollback just in case main ctx was cancelled.
	if err := action.Rollback(context.Background(), namespace, run.Target, run.Config); err != nil {
		e.logStep(run.ID, "Recovery", fmt.Sprintf("Rollback failed: %v", err), "Error")
		run.Verdict = "Partial/Fail"
	} else {
		e.logStep(run.ID, "Recovery", "Rollback successful", "Ok")
	}

	// 5. Post Snapshot
	e.logStep(run.ID, "PostSnapshot", "Capturing final metrics", "Ok")
	if snapshot, err := e.captureSnapshot(context.Background()); err == nil {
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

func (e *Engine) awaitRecoveryAuthorization(ctx context.Context, run *storage.DrillRun, session *drillSession) recoveryTrigger {
	deadline := time.Now().UTC().Add(recoveryFailsafeTimeout)
	session.beginAwaitingRecovery(deadline)
	e.updateStatus(run, StatusAwaitingRecovery)
	e.logStep(run.ID, "Recovery", fmt.Sprintf("Observation complete; awaiting operator recovery (failsafe in %s)", recoveryFailsafeTimeout), "Warn")

	timer := time.NewTimer(recoveryFailsafeTimeout)
	defer timer.Stop()

	select {
	case trigger := <-session.recoverCh:
		return trigger
	case <-timer.C:
		return session.beginFailsafeRecovery()
	case <-ctx.Done():
		return recoveryTrigger{Source: "abort", MarkAborted: true}
	}
}

func (e *Engine) recoveryInitiationMessage(trigger recoveryTrigger) string {
	switch trigger.Source {
	case "manual":
		return "Initiating operator-approved rollback (source: manual)"
	case "abort":
		return "Emergency rollback initiated (source: abort)"
	case "failsafe":
		return "Failsafe timeout reached; initiating rollback (source: failsafe)"
	default:
		return "Initiating rollback"
	}
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
	raw, ok := e.active.Load(runID)
	if !ok {
		return fmt.Errorf("%w: %s", ErrDrillNotActive, runID)
	}
	session, ok := raw.(*drillSession)
	if !ok || session == nil {
		return fmt.Errorf("%w: %s", ErrDrillNotActive, runID)
	}

	if queued, err := session.requestRecovery(recoveryTrigger{Source: "abort", MarkAborted: true}); err == nil && queued {
		return nil
	} else if err != nil && !errors.Is(err, ErrDrillNotRecoverable) {
		return err
	}

	session.cancel()
	return nil
}

func (e *Engine) RecoverDrill(runID string) error {
	raw, ok := e.active.Load(runID)
	if !ok {
		return fmt.Errorf("%w: %s", ErrDrillNotRecoverable, runID)
	}
	session, ok := raw.(*drillSession)
	if !ok || session == nil {
		return fmt.Errorf("%w: %s", ErrDrillNotRecoverable, runID)
	}

	_, err := session.requestRecovery(recoveryTrigger{Source: "manual"})
	return err
}

// K8sHealthResult holds the outcome of a Kubernetes connectivity probe.
type K8sHealthResult struct {
	Reachable bool   `json:"reachable"`
	Host      string `json:"host,omitempty"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// CheckK8sConnectivity performs a lightweight probe against the configured
// Kubernetes API server. It returns a structured result rather than an error
// so that callers (HTTP handlers) can always produce a JSON response.
func (e *Engine) CheckK8sConnectivity() K8sHealthResult {
	if e.k8sClients == nil {
		e.k8sClients = NewK8sClientFactory(K8sClientOptions{})
	}

	clientset, restCfg, err := e.k8sClients.clientsetWithConfig()
	if err != nil {
		return K8sHealthResult{
			Reachable: false,
			Error:     err.Error(),
			Hint:      "Unable to load Kubernetes client configuration. Ensure DRILLS_KUBECONFIG_PATH or KUBECONFIG is set, or that the analysis engine is running inside a cluster.",
		}
	}

	host := ""
	if restCfg != nil {
		host = restCfg.Host
	}

	info, err := clientset.Discovery().ServerVersion()
	if err != nil {
		hint := "Kubernetes API server is unreachable."
		if isLoopbackAPIHost(host) {
			hint = "Detected loopback Kubernetes API endpoint (" + host + "). Start an SSH tunnel to your cluster on this port, or set DRILLS_KUBE_API_SERVER to a reachable cluster endpoint."
		}
		return K8sHealthResult{
			Reachable: false,
			Host:      host,
			Error:     err.Error(),
			Hint:      hint,
		}
	}

	return K8sHealthResult{
		Reachable: true,
		Host:      host,
		Version:   info.GitVersion,
	}
}

func (e *Engine) RuntimeState(runID string) *DrillRuntimeState {
	raw, ok := e.active.Load(runID)
	if !ok {
		return nil
	}
	session, ok := raw.(*drillSession)
	if !ok || session == nil {
		return nil
	}
	state := session.runtimeState()
	return &state
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
