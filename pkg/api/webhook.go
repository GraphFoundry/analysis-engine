package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"predictive-analysis-engine/pkg/clients/graph"
	"predictive-analysis-engine/pkg/clients/telemetry"
	"predictive-analysis-engine/pkg/config"
	"predictive-analysis-engine/pkg/predictive"
	"predictive-analysis-engine/pkg/storage"
)

// WebhookHandler receives graph update webhooks from the service-graph-engine.
// It replaces the PollWorker by processing pushed data instead of polling.
type WebhookHandler struct {
	telemetryClient     *telemetry.TelemetryClient
	decisionStore       *storage.DecisionStore
	cfg                 *config.Config
	forwardURLs         []string
	httpClient          *http.Client
	processingSem       chan struct{}
	predictiveEvaluator *predictive.Evaluator

	// Cache the latest snapshot for API consumers
	mu             sync.RWMutex
	latestSnapshot *CachedGraphData

	// Cache the latest predictive analysis result
	predMu           sync.RWMutex
	latestPredictive *predictive.CurrentActionResponse

	// Basic fixed-window rate limiter state for inbound webhook traffic.
	rlMu          sync.Mutex
	rlWindowStart time.Time
	rlCount       int

	stats webhookRuntimeStats
}

type webhookRuntimeStats struct {
	received              uint64
	accepted              uint64
	duplicates            uint64
	failed                uint64
	forwarded             uint64
	retried               uint64
	processed             uint64
	processLatencyMsTotal uint64
}

// CachedGraphData holds the most recent data received via webhook.
type CachedGraphData struct {
	MetricsSnapshot *graph.MetricsSnapshotResponse  `json:"metricsSnapshot"`
	Services        []graph.ServiceInfo             `json:"services"`
	Nodes           []graph.NodeWithResources       `json:"nodes"`
	Centrality      *graph.CentralityScoresResponse `json:"centrality"`
	ReceivedAt      time.Time                       `json:"receivedAt"`
}

// WebhookPayload is the expected JSON structure from service-graph-engine.
type WebhookPayload struct {
	Event         string    `json:"event"`
	Timestamp     string    `json:"timestamp"`
	SchemaVersion string    `json:"schema_version,omitempty"`
	EventID       string    `json:"event_id,omitempty"`
	CorrelationID string    `json:"correlation_id,omitempty"`
	SentAt        string    `json:"sent_at,omitempty"`
	Data          GraphData `json:"data"`
}

// GraphData contains the full graph update pushed by service-graph-engine.
type GraphData struct {
	MetricsSnapshot WebhookMetricsSnapshot `json:"metricsSnapshot"`
	Services        []WebhookServiceInfo   `json:"services"`
	Infrastructure  WebhookInfrastructure  `json:"infrastructure"`
	Centrality      WebhookCentrality      `json:"centrality"`
}

type WebhookMetricsSnapshot struct {
	Timestamp string                  `json:"timestamp"`
	Window    string                  `json:"window"`
	Services  []WebhookServiceMetrics `json:"services"`
	Edges     []WebhookEdge           `json:"edges"`
}

type WebhookServiceMetrics struct {
	Name         string      `json:"name"`
	Namespace    string      `json:"namespace"`
	RPS          float64     `json:"rps"`
	ErrorRate    float64     `json:"errorRate"`
	P95          float64     `json:"p95"`
	PodCount     interface{} `json:"podCount"`
	Availability interface{} `json:"availability"`
}

type WebhookEdge struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	Namespace string  `json:"namespace"`
	RPS       float64 `json:"rps"`
	ErrorRate float64 `json:"errorRate"`
	P95       float64 `json:"p95"`
}

type WebhookServiceInfo struct {
	Name         string           `json:"name"`
	Namespace    string           `json:"namespace"`
	PodCount     interface{}      `json:"podCount"`
	Availability interface{}      `json:"availability"`
	Placement    WebhookPlacement `json:"placement"`
}

type WebhookPlacement struct {
	Nodes []WebhookNodePlacement `json:"nodes"`
}

type WebhookNodePlacement struct {
	Node      string               `json:"node"`
	Resources WebhookNodeResources `json:"resources"`
	Pods      []WebhookPodInfo     `json:"pods"`
}

type WebhookNodeResources struct {
	CPU struct {
		UsagePercent float64 `json:"usagePercent"`
		Cores        float64 `json:"cores"`
	} `json:"cpu"`
	RAM struct {
		UsedMB  float64 `json:"usedMB"`
		TotalMB float64 `json:"totalMB"`
	} `json:"ram"`
}

type WebhookPodInfo struct {
	Name            string  `json:"name"`
	RAMUsedMB       float64 `json:"ramUsedMB"`
	CPUUsagePercent float64 `json:"cpuUsagePercent"`
	UptimeSeconds   int     `json:"uptimeSeconds"`
}

type WebhookInfrastructure struct {
	Nodes []WebhookNodeInfo `json:"nodes"`
}

type WebhookNodeInfo struct {
	Name      string               `json:"name"`
	Resources WebhookNodeResources `json:"resources"`
}

type WebhookCentrality struct {
	Scores []WebhookCentralityScore `json:"scores"`
}

type WebhookCentralityScore struct {
	Service     string  `json:"service"`
	PageRank    float64 `json:"pagerank"`
	Betweenness float64 `json:"betweenness"`
}

type webhookEventMeta struct {
	EventID       string
	CorrelationID string
	EventType     string
	SentAt        string
}

func NewWebhookHandler(cfg *config.Config, tClient *telemetry.TelemetryClient, store *storage.DecisionStore, predEval *predictive.Evaluator) *WebhookHandler {
	forwardURLs := parseForwardURLs(cfg)
	maxInFlight := cfg.Webhook.MaxInFlight
	if maxInFlight <= 0 {
		maxInFlight = 32
	}

	h := &WebhookHandler{
		telemetryClient:     tClient,
		decisionStore:       store,
		cfg:                 cfg,
		forwardURLs:         forwardURLs,
		predictiveEvaluator: predEval,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		processingSem: make(chan struct{}, maxInFlight),
	}

	if len(forwardURLs) > 0 {
		log.Printf("[Webhook] Will forward graph updates to %d subscriber(s):", len(forwardURLs))
		for _, u := range forwardURLs {
			log.Printf("  → %s", u)
		}
	}

	return h
}

func parseForwardURLs(cfg *config.Config) []string {
	raw := cfg.Webhook.ForwardURLs
	if raw == "" {
		return nil
	}
	var urls []string
	for _, u := range strings.Split(raw, ",") {
		u = strings.TrimSpace(u)
		if u != "" {
			urls = append(urls, u)
		}
	}
	return urls
}

// HandleWebhookStatus returns operational status and counters for graph webhook processing.
func (h *WebhookHandler) HandleWebhookStatus(w http.ResponseWriter, r *http.Request) {
	stats := map[string]interface{}{
		"received_total":      atomic.LoadUint64(&h.stats.received),
		"accepted_total":      atomic.LoadUint64(&h.stats.accepted),
		"duplicates_total":    atomic.LoadUint64(&h.stats.duplicates),
		"failed_total":        atomic.LoadUint64(&h.stats.failed),
		"forwarded_total":     atomic.LoadUint64(&h.stats.forwarded),
		"retry_total":         atomic.LoadUint64(&h.stats.retried),
		"processed_total":     atomic.LoadUint64(&h.stats.processed),
		"in_flight":           len(h.processingSem),
		"max_in_flight":       cap(h.processingSem),
		"process_latency_avg": 0.0,
	}
	processed := atomic.LoadUint64(&h.stats.processed)
	if processed > 0 {
		totalLatency := atomic.LoadUint64(&h.stats.processLatencyMsTotal)
		stats["process_latency_avg"] = float64(totalLatency) / float64(processed)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"subscribers": h.forwardURLs,
		"stats":       stats,
	})
}

// HandleGraphUpdate is the HTTP handler for POST /webhook/graph-update
func (h *WebhookHandler) HandleGraphUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	if !h.allowInboundWebhook() {
		atomic.AddUint64(&h.stats.failed, 1)
		respondError(w, http.StatusTooManyRequests, "Webhook rate limit exceeded")
		return
	}

	// Read body
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		atomic.AddUint64(&h.stats.failed, 1)
		log.Printf("[Webhook] Failed to read body: %v", err)
		respondError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}
	defer r.Body.Close()
	atomic.AddUint64(&h.stats.received, 1)

	timestampHeader := r.Header.Get("X-Webhook-Timestamp")
	signatureHeader := r.Header.Get("X-Webhook-Signature")
	incomingCorrelationID := r.Header.Get("X-Correlation-Id")
	incomingEventID := r.Header.Get("X-Webhook-Id")

	// Verify HMAC signature and replay window if secret is configured.
	if h.cfg.Webhook.Secret != "" {
		ok, verifyErr := verifyIncomingSignature(
			body,
			signatureHeader,
			timestampHeader,
			h.cfg.Webhook.Secret,
			h.cfg.Webhook.AcceptLegacySignature,
		)
		if verifyErr != nil {
			atomic.AddUint64(&h.stats.failed, 1)
			log.Printf("[Webhook] Signature verification failed: %v", verifyErr)
			respondError(w, http.StatusBadRequest, verifyErr.Error())
			return
		}
		if !ok {
			atomic.AddUint64(&h.stats.failed, 1)
			log.Println("[Webhook] Invalid signature - rejecting request")
			respondError(w, http.StatusUnauthorized, "Invalid webhook signature")
			return
		}

		if timestampHeader != "" {
			replayOK, replayErr := isWithinReplayWindow(timestampHeader, h.cfg.Webhook.ReplayWindowSec)
			if replayErr != nil {
				atomic.AddUint64(&h.stats.failed, 1)
				log.Printf("[Webhook] Invalid webhook timestamp: %v", replayErr)
				respondError(w, http.StatusBadRequest, "Invalid webhook timestamp")
				return
			}
			if !replayOK {
				atomic.AddUint64(&h.stats.failed, 1)
				log.Printf("[Webhook] Replay window exceeded timestamp=%s", timestampHeader)
				respondError(w, http.StatusUnauthorized, "Webhook timestamp outside replay window")
				return
			}
		} else if !h.cfg.Webhook.AcceptLegacySignature {
			atomic.AddUint64(&h.stats.failed, 1)
			respondError(w, http.StatusBadRequest, "Missing X-Webhook-Timestamp")
			return
		}
	}

	// Parse payload
	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		atomic.AddUint64(&h.stats.failed, 1)
		log.Printf("[Webhook] Invalid JSON payload: %v", err)
		respondError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	if payload.Event != "graph_update" {
		atomic.AddUint64(&h.stats.failed, 1)
		respondError(w, http.StatusBadRequest, fmt.Sprintf("Unknown event type: %s", payload.Event))
		return
	}

	if !h.tryAcquireProcessingSlot() {
		atomic.AddUint64(&h.stats.failed, 1)
		respondError(w, http.StatusServiceUnavailable, "Webhook processor is busy")
		return
	}

	eventID := payload.EventID
	if eventID == "" {
		eventID = incomingEventID
	}
	if eventID == "" {
		eventID = buildLegacyEventID(body)
	}
	correlationID := payload.CorrelationID
	if correlationID == "" {
		correlationID = incomingCorrelationID
	}
	if correlationID == "" {
		correlationID = eventID
	}
	sentAt := payload.SentAt
	if sentAt == "" {
		sentAt = payload.Timestamp
	}
	if sentAt == "" {
		sentAt = time.Now().UTC().Format(time.RFC3339)
	}
	meta := webhookEventMeta{
		EventID:       eventID,
		CorrelationID: correlationID,
		EventType:     payload.Event,
		SentAt:        sentAt,
	}

	dedupeWindow := time.Duration(h.cfg.Webhook.DedupeWindowSec) * time.Second
	if dedupeWindow <= 0 {
		dedupeWindow = 24 * time.Hour
	}
	if h.decisionStore == nil {
		h.releaseProcessingSlot()
		atomic.AddUint64(&h.stats.failed, 1)
		log.Printf("[Webhook] Durable acceptance unavailable eventId=%s correlationId=%s", meta.EventID, meta.CorrelationID)
		respondError(w, http.StatusServiceUnavailable, "Webhook storage unavailable")
		return
	}

	isDuplicate, err := h.decisionStore.RegisterWebhookEvent(
		meta.EventID,
		hashBytesHex(body),
		"service-graph-engine",
		meta.CorrelationID,
		meta.EventType,
		dedupeWindow,
	)
	if err != nil {
		h.releaseProcessingSlot()
		if errors.Is(err, storage.ErrWebhookEventHashConflict) {
			atomic.AddUint64(&h.stats.failed, 1)
			log.Printf("[Webhook] Event hash conflict eventId=%s correlationId=%s", meta.EventID, meta.CorrelationID)
			respondError(w, http.StatusConflict, "Webhook event ID conflict")
			return
		}

		atomic.AddUint64(&h.stats.failed, 1)
		log.Printf("[Webhook] Durable acceptance failed eventId=%s correlationId=%s error=%v", meta.EventID, meta.CorrelationID, err)
		respondError(w, http.StatusServiceUnavailable, "Failed to durably accept webhook")
		return
	}

	if isDuplicate {
		h.releaseProcessingSlot()
		atomic.AddUint64(&h.stats.duplicates, 1)
		log.Printf("[Webhook] Duplicate event ignored eventId=%s correlationId=%s", meta.EventID, meta.CorrelationID)
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"success":     true,
			"duplicate":   true,
			"eventId":     meta.EventID,
			"correlation": meta.CorrelationID,
			"message":     "Duplicate webhook ignored",
		})
		return
	}

	log.Printf("[Webhook] Accepted event=%s correlationId=%s graph_update with %d services, %d edges",
		meta.EventID,
		meta.CorrelationID,
		len(payload.Data.MetricsSnapshot.Services),
		len(payload.Data.MetricsSnapshot.Edges))

	atomic.AddUint64(&h.stats.accepted, 1)

	// Process data asynchronously after durable acceptance.
	go h.processWebhookData(payload, body, meta)

	respondJSON(w, http.StatusAccepted, map[string]interface{}{
		"success":       true,
		"eventId":       meta.EventID,
		"correlationId": meta.CorrelationID,
		"message":       "Graph update accepted",
	})
}

// processWebhookData handles the actual data processing in a goroutine.
func (h *WebhookHandler) processWebhookData(payload WebhookPayload, rawBody []byte, meta webhookEventMeta) {
	defer h.releaseProcessingSlot()
	startedAt := time.Now()

	processTimeout := time.Duration(h.cfg.Webhook.ProcessTimeoutMs) * time.Millisecond
	if processTimeout <= 0 {
		processTimeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), processTimeout)
	defer cancel()

	data := payload.Data

	// 1. Write telemetry metrics to InfluxDB (same logic as PollWorker.poll)
	h.writeTelemetryMetrics(ctx, data, meta)

	// 2. Cache latest data for API consumers
	h.cacheLatestData(data)

	// 3. Run predictive analysis with the received data
	h.runPredictiveAnalysis(data)

	// 4. Forward to dashboard BFF webhook subscribers
	h.forwardToSubscribers(ctx, rawBody, meta)

	if ctx.Err() != nil {
		atomic.AddUint64(&h.stats.failed, 1)
		log.Printf("[Webhook] Processing timeout eventId=%s correlationId=%s: %v", meta.EventID, meta.CorrelationID, ctx.Err())
		return
	}

	atomic.AddUint64(&h.stats.processed, 1)
	atomic.AddUint64(&h.stats.processLatencyMsTotal, uint64(time.Since(startedAt).Milliseconds()))

	log.Printf("[Webhook] Processing complete eventId=%s correlationId=%s: %d services, %d edges",
		meta.EventID,
		meta.CorrelationID,
		len(data.MetricsSnapshot.Services),
		len(data.MetricsSnapshot.Edges))
}

// writeTelemetryMetrics writes received metrics to InfluxDB (replaces PollWorker logic).
func (h *WebhookHandler) writeTelemetryMetrics(ctx context.Context, data GraphData, meta webhookEventMeta) {
	servicePoints := buildServicePoints(data.MetricsSnapshot.Services, data.Services)
	edgePoints := buildEdgePoints(data.MetricsSnapshot.Edges)
	nodePoints, podPoints := buildInfraPoints(data.Services)

	if len(servicePoints) > 0 {
		if err := h.telemetryClient.WriteServiceMetrics(ctx, servicePoints); err != nil {
			atomic.AddUint64(&h.stats.failed, 1)
			log.Printf("[Webhook] Write service metrics failed eventId=%s correlationId=%s: %v", meta.EventID, meta.CorrelationID, err)
		}
	}

	if len(edgePoints) > 0 {
		if err := h.telemetryClient.WriteEdgeMetrics(ctx, edgePoints); err != nil {
			atomic.AddUint64(&h.stats.failed, 1)
			log.Printf("[Webhook] Write edge metrics failed eventId=%s correlationId=%s: %v", meta.EventID, meta.CorrelationID, err)
		}
	}

	if len(nodePoints) > 0 {
		if err := h.telemetryClient.WriteInfrastructureMetrics(ctx, nodePoints, podPoints); err != nil {
			atomic.AddUint64(&h.stats.failed, 1)
			log.Printf("[Webhook] Write infra metrics failed eventId=%s correlationId=%s: %v", meta.EventID, meta.CorrelationID, err)
		}
	}
}

// buildServicePoints converts webhook service metrics into telemetry ServicePoints.
func buildServicePoints(services []WebhookServiceMetrics, serviceInfos []WebhookServiceInfo) []telemetry.ServicePoint {
	var points []telemetry.ServicePoint

	availabilityByService := make(map[string]interface{}, len(serviceInfos))
	for _, svc := range serviceInfos {
		key := fmt.Sprintf("%s:%s", svc.Namespace, svc.Name)
		availabilityByService[key] = svc.Availability
	}

	for _, svc := range services {
		r := svc.RPS
		sp := telemetry.ServicePoint{
			Name:        svc.Name,
			Namespace:   svc.Namespace,
			RequestRate: &r,
		}
		if svc.RPS > 0 {
			e := svc.ErrorRate
			p := svc.P95
			sp.ErrorRate = &e
			sp.P95 = &p
		}

		key := fmt.Sprintf("%s:%s", svc.Namespace, svc.Name)
		if availabilityRaw, ok := availabilityByService[key]; ok {
			sp.Availability = normalizeAvailabilityPercent(availabilityRaw)
		} else {
			sp.Availability = normalizeAvailabilityPercent(svc.Availability)
		}

		points = append(points, sp)
	}
	return points
}

// buildEdgePoints converts webhook edge metrics into telemetry EdgePoints.
func buildEdgePoints(edges []WebhookEdge) []telemetry.EdgePoint {
	var points []telemetry.EdgePoint
	for _, edge := range edges {
		r := edge.RPS
		ep := telemetry.EdgePoint{
			From:        edge.From,
			To:          edge.To,
			Namespace:   edge.Namespace,
			RequestRate: &r,
		}
		if edge.RPS > 0 {
			e := edge.ErrorRate
			p := edge.P95
			ep.ErrorRate = &e
			ep.P95 = &p
		}
		points = append(points, ep)
	}
	return points
}

// infraNode holds deduplicated node data for infrastructure telemetry.
type infraNode struct {
	Node     string
	CPU      float64
	Cores    float64
	RAM      float64
	RAMTotal float64
	Pods     []WebhookPodInfo
}

// buildInfraPoints extracts deduplicated node and pod points from service placement data.
func buildInfraPoints(services []WebhookServiceInfo) ([]telemetry.PkgNodePoint, []telemetry.PkgPodPoint) {
	uniqueNodes := deduplicateNodes(services)
	return convertInfraToPoints(uniqueNodes)
}

// deduplicateNodes collects unique nodes across all services, merging pod lists.
func deduplicateNodes(services []WebhookServiceInfo) map[string]*infraNode {
	nodes := make(map[string]*infraNode)
	for _, svc := range services {
		for _, n := range svc.Placement.Nodes {
			if n.Node == "" {
				continue
			}
			existing, ok := nodes[n.Node]
			if !ok {
				nodes[n.Node] = &infraNode{
					Node:     n.Node,
					CPU:      n.Resources.CPU.UsagePercent,
					Cores:    n.Resources.CPU.Cores,
					RAM:      n.Resources.RAM.UsedMB,
					RAMTotal: n.Resources.RAM.TotalMB,
					Pods:     append([]WebhookPodInfo{}, n.Pods...),
				}
				continue
			}
			mergeNewPods(existing, n.Pods)
		}
	}
	return nodes
}

// mergeNewPods adds pods to an existing node entry, skipping duplicates.
func mergeNewPods(node *infraNode, newPods []WebhookPodInfo) {
	for _, np := range newPods {
		found := false
		for _, ep := range node.Pods {
			if ep.Name == np.Name {
				found = true
				break
			}
		}
		if !found {
			node.Pods = append(node.Pods, np)
		}
	}
}

// convertInfraToPoints converts deduplicated node data to telemetry points.
func convertInfraToPoints(nodes map[string]*infraNode) ([]telemetry.PkgNodePoint, []telemetry.PkgPodPoint) {
	var nodePoints []telemetry.PkgNodePoint
	var podPoints []telemetry.PkgPodPoint

	for _, u := range nodes {
		cpuUse := u.CPU
		cpuCores := u.Cores
		ramUsed := u.RAM
		ramTotal := u.RAMTotal
		podCount := float64(len(u.Pods))

		nodePoints = append(nodePoints, telemetry.PkgNodePoint{
			Name:            u.Node,
			CPUUsagePercent: &cpuUse,
			CPUTotalCores:   &cpuCores,
			RAMUsedMB:       &ramUsed,
			RAMTotalMB:      &ramTotal,
			PodCount:        &podCount,
		})

		for _, pod := range u.Pods {
			ram := pod.RAMUsedMB
			cpuPct := pod.CPUUsagePercent
			podPoints = append(podPoints, telemetry.PkgPodPoint{
				Name:            pod.Name,
				NodeName:        u.Node,
				RAMUsedMB:       &ram,
				CPUUsagePercent: &cpuPct,
			})
		}
	}
	return nodePoints, podPoints
}

// cacheLatestData stores the latest webhook data for API consumers.
func (h *WebhookHandler) cacheLatestData(data GraphData) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.latestSnapshot = &CachedGraphData{
		MetricsSnapshot: buildMetricsSnapshotResponse(data.MetricsSnapshot),
		Services:        convertServiceInfos(data.Services),
		Nodes:           convertNodeInfos(data.Infrastructure.Nodes),
		Centrality:      convertCentralityScores(data.Centrality),
		ReceivedAt:      time.Now(),
	}
}

// runPredictiveAnalysis evaluates the predictive recommendation using webhook data.
func (h *WebhookHandler) runPredictiveAnalysis(data GraphData) {
	if h.predictiveEvaluator == nil {
		return
	}

	snapshot := buildMetricsSnapshotResponse(data.MetricsSnapshot)
	services := convertServiceInfos(data.Services)
	nodes := convertNodeInfos(data.Infrastructure.Nodes)

	result := h.predictiveEvaluator.EvaluateFromSamples(snapshot, services, nodes)

	h.predMu.Lock()
	h.latestPredictive = &result
	h.predMu.Unlock()

	log.Printf("[Webhook] Predictive analysis complete: anomaly=%v healthScore=%.1f",
		result.AnomalyActive, result.HealthScore)
}

// GetLatestPredictive returns the cached predictive analysis result.
func (h *WebhookHandler) GetLatestPredictive() *predictive.CurrentActionResponse {
	h.predMu.RLock()
	defer h.predMu.RUnlock()
	return h.latestPredictive
}

// buildMetricsSnapshotResponse converts webhook metrics into graph.MetricsSnapshotResponse.
func buildMetricsSnapshotResponse(ms WebhookMetricsSnapshot) *graph.MetricsSnapshotResponse {
	var services []graph.ServiceMetrics
	for _, svc := range ms.Services {
		services = append(services, graph.ServiceMetrics{
			Name:      svc.Name,
			Namespace: svc.Namespace,
			RPS:       svc.RPS,
			ErrorRate: svc.ErrorRate,
			P95:       svc.P95,
		})
	}
	var edges []graph.EdgeSnapshot
	for _, e := range ms.Edges {
		edges = append(edges, graph.EdgeSnapshot{
			From:      e.From,
			To:        e.To,
			Namespace: e.Namespace,
			RPS:       e.RPS,
			ErrorRate: e.ErrorRate,
			P95:       e.P95,
		})
	}
	return &graph.MetricsSnapshotResponse{
		Timestamp: ms.Timestamp,
		Window:    ms.Window,
		Services:  services,
		Edges:     edges,
	}
}

// convertServiceInfos converts webhook service info into graph.ServiceInfo slice.
func convertServiceInfos(services []WebhookServiceInfo) []graph.ServiceInfo {
	var result []graph.ServiceInfo
	for _, svc := range services {
		podCount := toInt(svc.PodCount)
		availability := toFloat64(svc.Availability)
		result = append(result, graph.ServiceInfo{
			Name:         svc.Name,
			Namespace:    svc.Namespace,
			PodCount:     podCount,
			Availability: availability,
			Placement:    convertPlacement(svc.Placement),
		})
	}
	return result
}

// convertPlacement converts webhook placement data to graph.ServicePlacement.
func convertPlacement(p WebhookPlacement) graph.ServicePlacement {
	var nodes []graph.NodePlacement
	for _, n := range p.Nodes {
		var pods []graph.PodInfo
		for _, pod := range n.Pods {
			pods = append(pods, graph.PodInfo{
				Name:            pod.Name,
				RAMUsedMB:       pod.RAMUsedMB,
				CPUUsagePercent: pod.CPUUsagePercent,
				UptimeSeconds:   pod.UptimeSeconds,
			})
		}
		nodes = append(nodes, graph.NodePlacement{
			Node: n.Node,
			Resources: graph.NodeResources{
				CPU: graph.CPUResources{
					UsagePercent: n.Resources.CPU.UsagePercent,
					Cores:        n.Resources.CPU.Cores,
				},
				RAM: graph.RAMResources{
					UsedMB:  n.Resources.RAM.UsedMB,
					TotalMB: n.Resources.RAM.TotalMB,
				},
			},
			Pods: pods,
		})
	}
	return graph.ServicePlacement{Nodes: nodes}
}

// convertNodeInfos converts webhook infrastructure nodes to graph.NodeWithResources slice.
func convertNodeInfos(nodes []WebhookNodeInfo) []graph.NodeWithResources {
	var result []graph.NodeWithResources
	for _, n := range nodes {
		result = append(result, graph.NodeWithResources{
			Name: n.Name,
			Resources: graph.NodeResources{
				CPU: graph.CPUResources{
					UsagePercent: n.Resources.CPU.UsagePercent,
					Cores:        n.Resources.CPU.Cores,
				},
				RAM: graph.RAMResources{
					UsedMB:  n.Resources.RAM.UsedMB,
					TotalMB: n.Resources.RAM.TotalMB,
				},
			},
		})
	}
	return result
}

// convertCentralityScores converts webhook centrality data to graph.CentralityScoresResponse.
func convertCentralityScores(c WebhookCentrality) *graph.CentralityScoresResponse {
	if len(c.Scores) == 0 {
		return nil
	}
	var scores []graph.ServiceScore
	for _, s := range c.Scores {
		scores = append(scores, graph.ServiceScore{
			Service:     s.Service,
			PageRank:    s.PageRank,
			Betweenness: s.Betweenness,
		})
	}
	return &graph.CentralityScoresResponse{Scores: scores}
}

// toInt converts an interface{} (float64 or int from JSON) to int.
func toInt(v interface{}) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	default:
		return 0
	}
}

func normalizeAvailabilityPercent(v interface{}) *float64 {
	availability, ok := toOptionalFloat64(v)
	if !ok || availability < 0 {
		return nil
	}

	if availability <= 1 {
		availability = availability * 100
	}
	if availability > 100 {
		availability = 100
	}

	return &availability
}

func toOptionalFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case json.Number:
		parsed, err := val.Float64()
		return parsed, err == nil
	case map[string]interface{}:
		// Neo4j integers may be encoded as {low, high}; prefer high when present.
		if highRaw, exists := val["high"]; exists {
			if high, ok := toOptionalFloat64(highRaw); ok && high != 0 {
				return high, true
			}
		}
		if lowRaw, exists := val["low"]; exists {
			if low, ok := toOptionalFloat64(lowRaw); ok {
				return low, true
			}
		}
		return 0, false
	default:
		return 0, false
	}
}

// toFloat64 converts an interface{} (float64 or int from JSON) to float64.
func toFloat64(v interface{}) float64 {
	if value, ok := toOptionalFloat64(v); ok {
		return value
	}
	return 0
}

// GetLatestSnapshot returns the cached data from the most recent webhook.
func (h *WebhookHandler) GetLatestSnapshot() *CachedGraphData {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.latestSnapshot
}

// forwardToSubscribers forwards the raw webhook payload to dashboard BFF and other subscribers.
func (h *WebhookHandler) forwardToSubscribers(ctx context.Context, rawBody []byte, meta webhookEventMeta) {
	if len(h.forwardURLs) == 0 {
		return
	}

	for _, targetURL := range h.forwardURLs {
		if err := h.forwardWithRetry(ctx, targetURL, rawBody, meta); err != nil {
			atomic.AddUint64(&h.stats.failed, 1)
			log.Printf(
				"[Webhook] Forward failed eventId=%s correlationId=%s target=%s: %v",
				meta.EventID,
				meta.CorrelationID,
				targetURL,
				err,
			)
		}
	}
}

func (h *WebhookHandler) forwardWithRetry(ctx context.Context, targetURL string, rawBody []byte, meta webhookEventMeta) error {
	maxAttempts := h.cfg.Webhook.ForwardRetryMax
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	baseDelayMs := h.cfg.Webhook.ForwardRetryBaseMs
	if baseDelayMs <= 0 {
		baseDelayMs = 250
	}
	maxDelayMs := h.cfg.Webhook.ForwardRetryMaxMs
	if maxDelayMs <= 0 {
		maxDelayMs = 5000
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		timestampHeader := strconv.FormatInt(time.Now().UTC().Unix(), 10)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(rawBody))
		if err != nil {
			return fmt.Errorf("create request failed: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Event", "graph_update")
		req.Header.Set("X-Forwarded-From", "analysis-engine")
		req.Header.Set("X-Webhook-Id", meta.EventID)
		req.Header.Set("X-Correlation-Id", meta.CorrelationID)
		req.Header.Set("X-Webhook-Timestamp", timestampHeader)

		signingSecret := h.cfg.Webhook.ForwardSecret
		if signingSecret == "" {
			signingSecret = h.cfg.Webhook.Secret
		}
		if signingSecret != "" {
			signature := signPayloadWithTimestamp(rawBody, timestampHeader, signingSecret)
			req.Header.Set("X-Webhook-Signature", fmt.Sprintf("sha256=%s", signature))
		}

		resp, err := h.httpClient.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}

		if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			atomic.AddUint64(&h.stats.forwarded, 1)
			log.Printf(
				"[Webhook] Forwarded eventId=%s correlationId=%s target=%s status=%d",
				meta.EventID,
				meta.CorrelationID,
				targetURL,
				resp.StatusCode,
			)
			return nil
		}

		retryable := false
		if err != nil {
			retryable = isRetryableForwardError(err)
			lastErr = err
		} else {
			retryable = isRetryableStatusCode(resp.StatusCode)
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		}

		if !retryable || attempt >= maxAttempts {
			break
		}

		atomic.AddUint64(&h.stats.retried, 1)
		delay := calculateBackoffDelay(attempt, baseDelayMs, maxDelayMs)
		log.Printf(
			"[Webhook] Retrying forward eventId=%s correlationId=%s target=%s attempt=%d/%d delay=%s",
			meta.EventID,
			meta.CorrelationID,
			targetURL,
			attempt+1,
			maxAttempts,
			delay.String(),
		)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	return lastErr
}

func signPayloadWithTimestamp(body []byte, timestampHeader, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestampHeader))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyIncomingSignature(body []byte, signatureHeader, timestampHeader, secret string, acceptLegacy bool) (bool, error) {
	if signatureHeader == "" {
		return false, nil
	}

	// Expect "sha256=<hex>"
	parts := strings.SplitN(signatureHeader, "=", 2)
	if len(parts) != 2 || parts[0] != "sha256" {
		return false, nil
	}

	expectedMAC, err := hex.DecodeString(parts[1])
	if err != nil {
		return false, nil
	}

	mac := hmac.New(sha256.New, []byte(secret))
	if timestampHeader != "" {
		mac.Write([]byte(timestampHeader))
		mac.Write([]byte("."))
		mac.Write(body)
	} else {
		if !acceptLegacy {
			return false, nil
		}
		// Legacy compatibility mode signs body only.
		mac.Write(body)
	}
	actualMAC := mac.Sum(nil)

	return hmac.Equal(actualMAC, expectedMAC), nil
}

func isWithinReplayWindow(timestampHeader string, replayWindowSec int) (bool, error) {
	if replayWindowSec <= 0 {
		replayWindowSec = 300
	}
	sentAt, err := parseWebhookTimestamp(timestampHeader)
	if err != nil {
		return false, err
	}

	maxSkew := time.Duration(replayWindowSec) * time.Second
	diff := time.Since(sentAt)
	if diff < 0 {
		diff = -diff
	}
	return diff <= maxSkew, nil
}

func parseWebhookTimestamp(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, fmt.Errorf("missing timestamp")
	}

	if sec, err := strconv.ParseInt(raw, 10, 64); err == nil {
		// Support both epoch seconds and epoch milliseconds.
		if sec > 9999999999 {
			return time.UnixMilli(sec).UTC(), nil
		}
		return time.Unix(sec, 0).UTC(), nil
	}

	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts.UTC(), nil
	}

	return time.Time{}, fmt.Errorf("unsupported timestamp format")
}

func hashBytesHex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func buildLegacyEventID(body []byte) string {
	sum := hashBytesHex(body)
	if len(sum) > 20 {
		sum = sum[:20]
	}
	return fmt.Sprintf("legacy_%s", sum)
}

func (h *WebhookHandler) allowInboundWebhook() bool {
	limit := h.cfg.RateLimit.MaxRequests
	windowMs := h.cfg.RateLimit.WindowMs
	if limit <= 0 || windowMs <= 0 {
		return true
	}

	h.rlMu.Lock()
	defer h.rlMu.Unlock()

	now := time.Now()
	windowDuration := time.Duration(windowMs) * time.Millisecond
	if h.rlWindowStart.IsZero() || now.Sub(h.rlWindowStart) >= windowDuration {
		h.rlWindowStart = now
		h.rlCount = 0
	}

	if h.rlCount >= limit {
		return false
	}
	h.rlCount++
	return true
}

func (h *WebhookHandler) tryAcquireProcessingSlot() bool {
	select {
	case h.processingSem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (h *WebhookHandler) releaseProcessingSlot() {
	select {
	case <-h.processingSem:
	default:
	}
}

func isRetryableStatusCode(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= 500
}

func isRetryableForwardError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "temporary")
}

func calculateBackoffDelay(attempt, baseDelayMs, maxDelayMs int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if baseDelayMs <= 0 {
		baseDelayMs = 250
	}
	if maxDelayMs <= 0 {
		maxDelayMs = 5000
	}

	delay := baseDelayMs * (1 << (attempt - 1))
	if delay > maxDelayMs {
		delay = maxDelayMs
	}
	// Add up to 25% jitter.
	jitter := delay / 4
	if jitter > 0 {
		delay += int(time.Now().UnixNano() % int64(jitter+1))
	}
	return time.Duration(delay) * time.Millisecond
}
