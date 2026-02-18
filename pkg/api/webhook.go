package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"predictive-analysis-engine/pkg/clients/graph"
	"predictive-analysis-engine/pkg/clients/telemetry"
	"predictive-analysis-engine/pkg/config"
)

// WebhookHandler receives graph update webhooks from the service-graph-engine.
// It replaces the PollWorker by processing pushed data instead of polling.
type WebhookHandler struct {
	telemetryClient *telemetry.TelemetryClient
	cfg             *config.Config
	forwardURLs     []string
	httpClient      *http.Client

	// Cache the latest snapshot for API consumers
	mu             sync.RWMutex
	latestSnapshot *CachedGraphData
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
	Event     string    `json:"event"`
	Timestamp string    `json:"timestamp"`
	Data      GraphData `json:"data"`
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
		Cores        int     `json:"cores"`
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

func NewWebhookHandler(cfg *config.Config, tClient *telemetry.TelemetryClient) *WebhookHandler {
	forwardURLs := parseForwardURLs(cfg)

	h := &WebhookHandler{
		telemetryClient: tClient,
		cfg:             cfg,
		forwardURLs:     forwardURLs,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
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

// HandleGraphUpdate is the HTTP handler for POST /webhook/graph-update
func (h *WebhookHandler) HandleGraphUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	// Read body
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		log.Printf("[Webhook] Failed to read body: %v", err)
		respondError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}
	defer r.Body.Close()

	// Verify HMAC signature if secret is configured
	if h.cfg.Webhook.Secret != "" {
		sig := r.Header.Get("X-Webhook-Signature")
		if !verifySignature(body, sig, h.cfg.Webhook.Secret) {
			log.Println("[Webhook] Invalid signature - rejecting request")
			respondError(w, http.StatusUnauthorized, "Invalid webhook signature")
			return
		}
	}

	// Parse payload
	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("[Webhook] Invalid JSON payload: %v", err)
		respondError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	if payload.Event != "graph_update" {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("Unknown event type: %s", payload.Event))
		return
	}

	log.Printf("[Webhook] Received graph_update with %d services, %d edges",
		len(payload.Data.MetricsSnapshot.Services),
		len(payload.Data.MetricsSnapshot.Edges))

	// Process data asynchronously to respond quickly (202 Accepted)
	go h.processWebhookData(payload, body)

	respondJSON(w, http.StatusAccepted, map[string]interface{}{
		"success": true,
		"message": "Graph update accepted",
	})
}

// processWebhookData handles the actual data processing in a goroutine.
func (h *WebhookHandler) processWebhookData(payload WebhookPayload, rawBody []byte) {
	data := payload.Data

	// 1. Write telemetry metrics to InfluxDB (same logic as PollWorker.poll)
	h.writeTelemetryMetrics(data)

	// 2. Cache latest data for API consumers
	h.cacheLatestData(data)

	// 3. Forward to dashboard BFF webhook subscribers
	h.forwardToSubscribers(rawBody)

	log.Printf("[Webhook] Processing complete: %d services, %d edges",
		len(data.MetricsSnapshot.Services),
		len(data.MetricsSnapshot.Edges))
}

// writeTelemetryMetrics writes received metrics to InfluxDB (replaces PollWorker logic).
func (h *WebhookHandler) writeTelemetryMetrics(data GraphData) {
	servicePoints := buildServicePoints(data.MetricsSnapshot.Services)
	edgePoints := buildEdgePoints(data.MetricsSnapshot.Edges)
	nodePoints, podPoints := buildInfraPoints(data.Services)

	ctx := context.Background()

	if len(servicePoints) > 0 {
		if err := h.telemetryClient.WriteServiceMetrics(ctx, servicePoints); err != nil {
			log.Printf("[Webhook] Write service metrics failed: %v", err)
		}
	}

	if len(edgePoints) > 0 {
		if err := h.telemetryClient.WriteEdgeMetrics(ctx, edgePoints); err != nil {
			log.Printf("[Webhook] Write edge metrics failed: %v", err)
		}
	}

	if len(nodePoints) > 0 {
		if err := h.telemetryClient.WriteInfrastructureMetrics(ctx, nodePoints, podPoints); err != nil {
			log.Printf("[Webhook] Write infra metrics failed: %v", err)
		}
	}
}

// buildServicePoints converts webhook service metrics into telemetry ServicePoints.
func buildServicePoints(services []WebhookServiceMetrics) []telemetry.ServicePoint {
	var points []telemetry.ServicePoint
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
					Cores:    float64(n.Resources.CPU.Cores),
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

// toFloat64 converts an interface{} (float64 or int from JSON) to float64.
func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	default:
		return 0
	}
}

// GetLatestSnapshot returns the cached data from the most recent webhook.
func (h *WebhookHandler) GetLatestSnapshot() *CachedGraphData {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.latestSnapshot
}

// forwardToSubscribers forwards the raw webhook payload to dashboard BFF and other subscribers.
func (h *WebhookHandler) forwardToSubscribers(rawBody []byte) {
	if len(h.forwardURLs) == 0 {
		return
	}

	for _, url := range h.forwardURLs {
		go func(targetURL string) {
			req, err := http.NewRequest("POST", targetURL, bytes.NewReader(rawBody))
			if err != nil {
				log.Printf("[Webhook] Failed to create forward request to %s: %v", targetURL, err)
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Webhook-Event", "graph_update")
			req.Header.Set("X-Forwarded-From", "analysis-engine")

			resp, err := h.httpClient.Do(req)
			if err != nil {
				log.Printf("[Webhook] Failed to forward to %s: %v", targetURL, err)
				return
			}
			defer resp.Body.Close()
			log.Printf("[Webhook] Forwarded to %s (status %d)", targetURL, resp.StatusCode)
		}(url)
	}
}

// verifySignature validates the HMAC SHA-256 signature.
func verifySignature(body []byte, signatureHeader, secret string) bool {
	if signatureHeader == "" {
		return false
	}

	// Expect "sha256=<hex>"
	parts := strings.SplitN(signatureHeader, "=", 2)
	if len(parts) != 2 || parts[0] != "sha256" {
		return false
	}

	expectedMAC, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	actualMAC := mac.Sum(nil)

	return hmac.Equal(actualMAC, expectedMAC)
}
