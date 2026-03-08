package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"predictive-analysis-engine/pkg/clients/graph"
	"predictive-analysis-engine/pkg/logger"
	"predictive-analysis-engine/pkg/simulation"
)

// SimulationsRunHandler handles the unified POST /simulations/run endpoint.
// It validates the request, builds an immutable snapshot from live graph data,
// resolves evidence tiers, and dispatches to the appropriate scenario runner.
func (h *Handler) SimulationsRunHandler(w http.ResponseWriter, r *http.Request) {
	var req simulation.SimulationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, simulation.SimulationErrorResponse{
			Error:  "Invalid request body",
			Errors: []simulation.ValidationError{{Code: "SIM_ERR_PARSE", Message: "Failed to parse JSON request body"}},
		})
		return
	}

	if err := simulation.ValidateSimulationRequest(req); err != nil {
		if ve, ok := err.(simulation.ValidationErrors); ok {
			respondJSON(w, http.StatusBadRequest, simulation.SimulationErrorResponse{
				Error:  ve.Error(),
				Errors: ve,
			})
			return
		}
		respondJSON(w, http.StatusBadRequest, simulation.SimulationErrorResponse{
			Error: err.Error(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	snap, influxCheck, err := h.buildLiveSnapshot(ctx, req)
	if err != nil {
		logger.Error("Failed to build snapshot for simulation", err)
		respondJSON(w, http.StatusServiceUnavailable, simulation.SimulationErrorResponse{
			Error:        "Failed to build cluster snapshot from live data",
			ResultStatus: string(simulation.ResultStatusDeferred),
			DeferredReason: fmt.Sprintf(
				"Could not build live snapshot: %s. Retry when the service graph is available.",
				err.Error(),
			),
		})
		return
	}

	execCtx := simulation.BuildExecutionContext(req, snap, influxCheck)

	if !simulation.IsScenarioSupported(req.ScenarioType) {
		resp := simulation.BuildDeferredResponse(execCtx,
			fmt.Sprintf("scenario type %q is not supported", req.ScenarioType))
		resp.ResultStatus = simulation.ResultStatusUnsupported
		simulation.NormalizeResponse(&resp)
		respondJSON(w, http.StatusOK, resp)
		return
	}

	if sufficient, reason := simulation.EvidenceSufficientForScenario(execCtx); !sufficient {
		resp := simulation.BuildDeferredResponse(execCtx, reason)
		simulation.NormalizeResponse(&resp)
		respondJSON(w, http.StatusOK, resp)
		return
	}

	resp := h.dispatchScenario(execCtx)
	simulation.NormalizeResponse(&resp)
	respondJSON(w, http.StatusOK, resp)
}

// dispatchScenario routes to the correct scenario runner based on ScenarioType.
func (h *Handler) dispatchScenario(ctx simulation.ExecutionContext) simulation.SimulationResponse {
	switch ctx.Request.ScenarioType {
	case simulation.ScenarioFailureShutdown:
		return simulation.RunFailureShutdownScenario(ctx)
	case simulation.ScenarioScaling:
		return simulation.RunScalingScenario(ctx)
	case simulation.ScenarioTrafficSpike:
		return simulation.RunTrafficSpikeScenario(ctx)
	case simulation.ScenarioChattyColocation:
		return simulation.RunChattyColocationScenario(ctx)
	case simulation.ScenarioNetworkCut:
		return simulation.RunNetworkCutScenario(ctx)
	default:
		resp := simulation.BuildBaseResponse(ctx)
		resp.ResultStatus = simulation.ResultStatusUnsupported
		resp.DeferredReason = fmt.Sprintf("no scenario runner for %q", ctx.Request.ScenarioType)
		resp.ImpactedServices = []simulation.ImpactedService{}
		resp.ImpactedPaths = []simulation.ImpactedPath{}
		resp.BeforeAfterValues = []simulation.BeforeAfterValue{}
		resp.Assumptions = []simulation.SimulationAssumption{}
		return resp
	}
}

// buildLiveSnapshot fetches live graph and runtime data and composes an immutable snapshot.
func (h *Handler) buildLiveSnapshot(ctx context.Context, req simulation.SimulationRequest) (simulation.SimulationSnapshot, simulation.InfluxCheckResult, error) {
	// Fetch services and metrics snapshot in parallel.
	type svcResult struct {
		data []graph.ServiceInfo
		err  error
	}
	type metricsResult struct {
		data *graph.MetricsSnapshotResponse
		err  error
	}

	svcCh := make(chan svcResult, 1)
	metricsCh := make(chan metricsResult, 1)

	go func() {
		s, e := h.GraphClient.GetServices(ctx)
		svcCh <- svcResult{s, e}
	}()

	go func() {
		m, e := h.GraphClient.GetMetricsSnapshot(ctx)
		metricsCh <- metricsResult{m, e}
	}()

	sRes := <-svcCh
	mRes := <-metricsCh

	if sRes.err != nil && mRes.err != nil {
		return simulation.SimulationSnapshot{}, simulation.InfluxCheckResult{},
			fmt.Errorf("service graph unavailable: %w", sRes.err)
	}

	namespace := strings.TrimSpace(h.Config.GraphAPI.Namespace)
	if namespace == "" {
		namespace = "default"
	}

	// Build snapshot nodes from services.
	var nodes []simulation.SnapshotServiceNode
	var runtimeServices []simulation.SnapshotRuntimeService
	if sRes.err == nil {
		for _, svc := range sRes.data {
			ns := svc.Namespace
			if ns == "" {
				ns = "default"
			}
			serviceID := fmt.Sprintf("%s:%s", ns, svc.Name)
			nodes = append(nodes, simulation.SnapshotServiceNode{
				ServiceID: serviceID,
				Name:      svc.Name,
				Namespace: ns,
			})
			runtimeServices = append(runtimeServices, simulation.SnapshotRuntimeService{
				ServiceID:    serviceID,
				PodCount:     svc.PodCount,
				ReadyPods:    svc.PodCount,
				Availability: svc.Availability,
			})
		}
	}

	// Build snapshot edges from metrics.
	var edges []simulation.SnapshotServiceEdge
	if mRes.err == nil && mRes.data != nil {
		for _, e := range mRes.data.Edges {
			source := normalizeEdgeServiceID(e.From, namespace)
			target := normalizeEdgeServiceID(e.To, namespace)
			edge := simulation.SnapshotServiceEdge{
				SourceServiceID: source,
				TargetServiceID: target,
				RateRPS:         e.RPS,
				ErrorRate:       e.ErrorRate,
			}
			if e.P95 > 0 {
				p95 := e.P95
				edge.P95Ms = &p95
			}
			edges = append(edges, edge)
		}
	}

	// Parse the request timestamp or use now.
	ts := time.Now().UTC()
	if req.SnapshotTimestamp != "" {
		if parsed, err := time.Parse(time.RFC3339, req.SnapshotTimestamp); err == nil {
			ts = parsed
		}
	}

	snap := simulation.ComposeSnapshotAt(simulation.SnapshotInput{
		Nodes:           nodes,
		Edges:           edges,
		RuntimeServices: runtimeServices,
	}, ts)

	// InfluxDB is not integrated as a direct dependency; report as unavailable.
	influxCheck := simulation.InfluxCheckResult{
		Reachable:      false,
		DataSufficient: false,
	}

	hasLiveGraph := sRes.err == nil && len(nodes) > 0

	// If no live graph data, snapshot is still valid but evidence will be degraded.
	_ = hasLiveGraph

	return snap, influxCheck, nil
}

// normalizeEdgeServiceID ensures an edge service ID has namespace prefix.
func normalizeEdgeServiceID(id string, defaultNamespace string) string {
	id = strings.TrimSpace(id)
	if strings.Contains(id, ":") {
		return id
	}
	return fmt.Sprintf("%s:%s", defaultNamespace, id)
}
