package simulation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"predictive-analysis-engine/pkg/clients/graph"
	"predictive-analysis-engine/pkg/config"
)

func TestSimulateAddService_SelectedNodeInfeasibleButFallbackExists(t *testing.T) {
	services := []graph.ServiceInfo{
		makeServiceInfo("default:baseline", 1, 2,
			makeNodePlacement("node-a", 75, 2, 1024, 2048),
			makeNodePlacement("node-b", 20, 2, 512, 4096),
		),
	}
	server := newAddSimulationTestServer(t, services, nil, emptyMetricsSnapshot())
	defer server.Close()

	result, err := SimulateAddService(context.Background(), newGraphClient(server.URL), testConfig(false), AddSimulationRequest{
		ServiceName:    "planned-api",
		TargetNodeName: "node-a",
		CPURequest:     0.8,
		RAMRequest:     1024,
		Replicas:       1,
	})
	if err != nil {
		t.Fatalf("SimulateAddService returned error: %v", err)
	}

	if !result.Success {
		t.Fatal("expected success when a fallback node can host the service")
	}
	if result.SelectedNodeSuitable {
		t.Fatal("expected preferred node to be unsuitable")
	}
	if result.RecommendedNodeName != "node-b" {
		t.Fatalf("expected recommended node node-b, got %q", result.RecommendedNodeName)
	}
	if len(result.SuitableNodes) == 0 || result.SuitableNodes[0].NodeName != "node-a" || !result.SuitableNodes[0].Preferred {
		t.Fatalf("expected preferred node to be shown first, got %+v", result.SuitableNodes)
	}
}

func TestSimulateAddService_SelectedNodeFeasibleAndPreferred(t *testing.T) {
	services := []graph.ServiceInfo{
		makeServiceInfo("default:baseline", 1, 2,
			makeNodePlacement("node-a", 10, 2, 512, 4096),
			makeNodePlacement("node-b", 40, 2, 2048, 4096),
		),
	}
	server := newAddSimulationTestServer(t, services, nil, emptyMetricsSnapshot())
	defer server.Close()

	result, err := SimulateAddService(context.Background(), newGraphClient(server.URL), testConfig(false), AddSimulationRequest{
		ServiceName:    "planned-api",
		TargetNodeName: "node-a",
		CPURequest:     0.5,
		RAMRequest:     512,
		Replicas:       1,
	})
	if err != nil {
		t.Fatalf("SimulateAddService returned error: %v", err)
	}

	if !result.Success {
		t.Fatal("expected success")
	}
	if !result.SelectedNodeSuitable {
		t.Fatal("expected preferred node to be suitable")
	}
	if result.SelectedNodeName != "node-a" {
		t.Fatalf("expected selected node node-a, got %q", result.SelectedNodeName)
	}
	if result.RecommendedNodeName != "" {
		t.Fatalf("expected no alternate recommendation, got %q", result.RecommendedNodeName)
	}
}

func TestSimulateAddService_SharedHostFlagDoesNotChangeNodeFit(t *testing.T) {
	services := []graph.ServiceInfo{
		makeServiceInfo("default:baseline", 1, 2,
			makeNodePlacement("node-a", 20, 2, 1024, 4096),
			makeNodePlacement("node-b", 20, 2, 1024, 4096),
		),
	}
	server := newAddSimulationTestServer(t, services, nil, emptyMetricsSnapshot())
	defer server.Close()

	req := AddSimulationRequest{
		ServiceName:    "planned-api",
		TargetNodeName: "node-a",
		CPURequest:     0.5,
		RAMRequest:     512,
		Replicas:       1,
	}

	clusterResult, err := SimulateAddService(context.Background(), newGraphClient(server.URL), testConfig(false), req)
	if err != nil {
		t.Fatalf("SimulateAddService returned error: %v", err)
	}
	machineResult, err := SimulateAddService(context.Background(), newGraphClient(server.URL), testConfig(true), req)
	if err != nil {
		t.Fatalf("SimulateAddService returned error: %v", err)
	}

	if clusterResult.SelectedNodeSuitable != machineResult.SelectedNodeSuitable {
		t.Fatalf("expected selected node suitability to stay the same: cluster=%t machine=%t", clusterResult.SelectedNodeSuitable, machineResult.SelectedNodeSuitable)
	}
	if len(clusterResult.SuitableNodes) != len(machineResult.SuitableNodes) {
		t.Fatalf("expected same node count, got %d vs %d", len(clusterResult.SuitableNodes), len(machineResult.SuitableNodes))
	}
	for index := range clusterResult.SuitableNodes {
		left := clusterResult.SuitableNodes[index]
		right := machineResult.SuitableNodes[index]
		if left.NodeName != right.NodeName || left.Suitable != right.Suitable || left.MaxPods != right.MaxPods {
			t.Fatalf("expected node fit to remain unchanged, got left=%+v right=%+v", left, right)
		}
	}
	if clusterResult.AggregateResources.Scope != "cluster" {
		t.Fatalf("expected cluster scope, got %q", clusterResult.AggregateResources.Scope)
	}
	if machineResult.AggregateResources.Scope != "machine" {
		t.Fatalf("expected machine scope, got %q", machineResult.AggregateResources.Scope)
	}
}

func TestSimulateAddService_MissingDependencyReturnsHighRisk(t *testing.T) {
	services := []graph.ServiceInfo{
		makeServiceInfo("default:baseline", 1, 2,
			makeNodePlacement("node-a", 20, 2, 512, 4096),
		),
	}
	server := newAddSimulationTestServer(t, services, nil, emptyMetricsSnapshot())
	defer server.Close()

	result, err := SimulateAddService(context.Background(), newGraphClient(server.URL), testConfig(false), AddSimulationRequest{
		ServiceName:    "planned-api",
		TargetNodeName: "node-a",
		CPURequest:     0.5,
		RAMRequest:     256,
		Replicas:       1,
		Dependencies: []DependencyRef{
			{ServiceId: "default:missing-db"},
		},
	})
	if err != nil {
		t.Fatalf("SimulateAddService returned error: %v", err)
	}

	if result.RiskAnalysis.DependencyRisk != "high" {
		t.Fatalf("expected high risk, got %q", result.RiskAnalysis.DependencyRisk)
	}
	if len(result.DependencyAnalysis.MissingServices) != 1 || result.DependencyAnalysis.MissingServices[0] != "default:missing-db" {
		t.Fatalf("expected missing dependency to be reported, got %+v", result.DependencyAnalysis.MissingServices)
	}
}

func TestSimulateAddService_UnobservedDependencyLinkReturnsMediumRisk(t *testing.T) {
	services := []graph.ServiceInfo{
		makeServiceInfo("default:baseline", 1, 2, makeNodePlacement("node-a", 20, 2, 512, 4096)),
		makeServiceInfo("default:gateway", 1, 2, makeNodePlacement("node-a", 25, 2, 512, 4096)),
		makeServiceInfo("default:db", 1, 2, makeNodePlacement("node-b", 30, 2, 1024, 4096)),
	}
	server := newAddSimulationTestServer(t, services, nil, emptyMetricsSnapshot())
	defer server.Close()

	result, err := SimulateAddService(context.Background(), newGraphClient(server.URL), testConfig(false), AddSimulationRequest{
		ServiceName:    "planned-api",
		TargetNodeName: "node-a",
		CPURequest:     0.5,
		RAMRequest:     256,
		Replicas:       1,
		Dependencies: []DependencyRef{
			{ServiceId: "default:gateway"},
			{ServiceId: "default:db"},
		},
	})
	if err != nil {
		t.Fatalf("SimulateAddService returned error: %v", err)
	}

	if result.RiskAnalysis.DependencyRisk != "medium" {
		t.Fatalf("expected medium risk, got %q", result.RiskAnalysis.DependencyRisk)
	}
	if len(result.DependencyAnalysis.LinkChecks) != 1 || result.DependencyAnalysis.LinkChecks[0].Observed {
		t.Fatalf("expected one unobserved link, got %+v", result.DependencyAnalysis.LinkChecks)
	}
}

func TestSimulateAddService_HealthyObservedDependencyChainReturnsLowRisk(t *testing.T) {
	services := []graph.ServiceInfo{
		makeServiceInfo("default:baseline", 1, 2, makeNodePlacement("node-a", 20, 2, 512, 4096)),
		makeServiceInfo("default:gateway", 0.99, 2, makeNodePlacement("node-a", 25, 2, 512, 4096)),
		makeServiceInfo("default:db", 0.98, 2, makeNodePlacement("node-b", 30, 2, 1024, 4096)),
	}
	metrics := &graph.MetricsSnapshotResponse{
		Timestamp: "2026-03-09T12:00:00Z",
		Window:    "1m",
		Edges: []graph.EdgeSnapshot{
			{
				From:      "gateway",
				To:        "db",
				Namespace: "default",
				RPS:       12.5,
				ErrorRate: 0.005,
				P95:       120,
			},
		},
	}
	server := newAddSimulationTestServer(t, services, nil, metrics)
	defer server.Close()

	result, err := SimulateAddService(context.Background(), newGraphClient(server.URL), testConfig(false), AddSimulationRequest{
		ServiceName:    "planned-api",
		TargetNodeName: "node-a",
		CPURequest:     0.5,
		RAMRequest:     256,
		Replicas:       1,
		Dependencies: []DependencyRef{
			{ServiceId: "default:gateway"},
			{ServiceId: "default:db"},
		},
	})
	if err != nil {
		t.Fatalf("SimulateAddService returned error: %v", err)
	}

	if result.RiskAnalysis.DependencyRisk != "low" {
		t.Fatalf("expected low risk, got %q", result.RiskAnalysis.DependencyRisk)
	}
	if len(result.DependencyAnalysis.LinkChecks) != 1 || !result.DependencyAnalysis.LinkChecks[0].Observed {
		t.Fatalf("expected one observed link, got %+v", result.DependencyAnalysis.LinkChecks)
	}
}

func newAddSimulationTestServer(
	t *testing.T,
	services []graph.ServiceInfo,
	nodes []graph.NodeWithResources,
	metrics *graph.MetricsSnapshotResponse,
) *httptest.Server {
	t.Helper()

	if metrics == nil {
		metrics = emptyMetricsSnapshot()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/services", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"services": services})
	})
	mux.HandleFunc("/infrastructure/nodes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"nodes": nodes})
	})
	mux.HandleFunc("/metrics/snapshot", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, metrics)
	})

	return httptest.NewServer(mux)
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}
}

func newGraphClient(baseURL string) *graph.Client {
	return graph.NewClient(config.GraphAPIConfig{
		BaseURL:   baseURL,
		TimeoutMs: 1000,
	})
}

func testConfig(sharedHostResources bool) *config.Config {
	return &config.Config{
		Simulation: config.SimulationConfig{
			SharedHostResources: sharedHostResources,
		},
	}
}

func emptyMetricsSnapshot() *graph.MetricsSnapshotResponse {
	return &graph.MetricsSnapshotResponse{
		Timestamp: "2026-03-09T12:00:00Z",
		Window:    "1m",
		Services:  []graph.ServiceMetrics{},
		Edges:     []graph.EdgeSnapshot{},
	}
}

func makeServiceInfo(serviceID string, availability float64, podCount int, placements ...graph.NodePlacement) graph.ServiceInfo {
	namespace, name := splitServiceID(serviceID)
	return graph.ServiceInfo{
		Name:         name,
		Namespace:    namespace,
		PodCount:     podCount,
		Availability: availability,
		Placement: graph.ServicePlacement{
			Nodes: placements,
		},
	}
}

func makeNodePlacement(node string, cpuUsagePercent float64, cpuCores float64, ramUsedMB, ramTotalMB float64) graph.NodePlacement {
	return graph.NodePlacement{
		Node: node,
		Resources: graph.NodeResources{
			CPU: graph.CPUResources{
				UsagePercent: cpuUsagePercent,
				Cores:        cpuCores,
			},
			RAM: graph.RAMResources{
				UsedMB:  ramUsedMB,
				TotalMB: ramTotalMB,
			},
		},
	}
}

func splitServiceID(serviceID string) (string, string) {
	parts := strings.SplitN(serviceID, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "default", serviceID
}
