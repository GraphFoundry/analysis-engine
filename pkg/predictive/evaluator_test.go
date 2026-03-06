package predictive

import (
	"testing"

	"predictive-analysis-engine/pkg/clients/graph"
)

func nodeResources(cpu float64, usedMB float64, totalMB float64) graph.NodeResources {
	return graph.NodeResources{
		CPU: graph.CPUResources{UsagePercent: cpu, Cores: 8},
		RAM: graph.RAMResources{UsedMB: usedMB, TotalMB: totalMB},
	}
}

func TestEvaluateFromSamples_SustainedCrossNodeTrafficKeepsNetworkRecommendation(t *testing.T) {
	evaluator := NewEvaluator(nil)

	services := []graph.ServiceInfo{
		{
			Name:      "frontend",
			Namespace: "onlineboutique",
			PodCount:  1,
			Placement: graph.ServicePlacement{Nodes: []graph.NodePlacement{{
				Node:      "boutique-m03",
				Resources: nodeResources(14, 1800, 32000),
				Pods:      []graph.PodInfo{{Name: "frontend-pod"}},
			}}},
		},
		{
			Name:      "productcatalogservice",
			Namespace: "onlineboutique",
			PodCount:  1,
			Placement: graph.ServicePlacement{Nodes: []graph.NodePlacement{{
				Node:      "boutique-m02",
				Resources: nodeResources(12, 1700, 32000),
				Pods:      []graph.PodInfo{{Name: "productcatalog-pod"}},
			}}},
		},
	}

	nodes := []graph.NodeWithResources{
		{Name: "boutique-m02", Resources: nodeResources(12, 1700, 32000)},
		{Name: "boutique-m03", Resources: nodeResources(14, 1800, 32000)},
	}

	snapshot1 := &graph.MetricsSnapshotResponse{
		Timestamp: "2026-03-06T17:12:00Z",
		Services: []graph.ServiceMetrics{
			{Name: "frontend", Namespace: "onlineboutique", RPS: 240, P95: 140},
			{Name: "productcatalogservice", Namespace: "onlineboutique", RPS: 190, P95: 30},
		},
		Edges: []graph.EdgeSnapshot{{
			From:      "frontend",
			To:        "productcatalogservice",
			Namespace: "onlineboutique",
			RPS:       180,
			P95:       240,
		}},
	}

	first := evaluator.EvaluateFromSamples(snapshot1, services, nodes)
	if !first.AnomalyActive {
		t.Fatalf("expected anomaly on first sustained sample")
	}
	if first.Recommendation == nil {
		t.Fatalf("expected recommendation for sustained network pressure")
	}
	if first.Recommendation.ActionType != "MigrateService" {
		t.Fatalf("expected migrate recommendation, got %s", first.Recommendation.ActionType)
	}

	snapshot2 := &graph.MetricsSnapshotResponse{
		Timestamp: "2026-03-06T17:12:10Z",
		Services:  snapshot1.Services,
		Edges: []graph.EdgeSnapshot{{
			From:      "frontend",
			To:        "productcatalogservice",
			Namespace: "onlineboutique",
			RPS:       176,
			P95:       230,
		}},
	}

	second := evaluator.EvaluateFromSamples(snapshot2, services, nodes)
	if !second.AnomalyActive {
		t.Fatalf("expected anomaly to persist under sustained pressure")
	}
	if second.Recommendation == nil || second.Recommendation.ActionType != "MigrateService" {
		t.Fatalf("expected migrate recommendation to persist, got %+v", second.Recommendation)
	}
}

func TestEvaluateFromSamples_LatencySpikeTriggersCapacityScale(t *testing.T) {
	evaluator := NewEvaluator(nil)

	services := []graph.ServiceInfo{
		{
			Name:      "frontend",
			Namespace: "onlineboutique",
			PodCount:  2,
			Placement: graph.ServicePlacement{Nodes: []graph.NodePlacement{{
				Node:      "boutique-m03",
				Resources: nodeResources(18, 2000, 32000),
				Pods:      []graph.PodInfo{{Name: "frontend-pod-1"}, {Name: "frontend-pod-2"}},
			}}},
		},
		{
			Name:      "loadgenerator",
			Namespace: "onlineboutique",
			PodCount:  1,
			Placement: graph.ServicePlacement{Nodes: []graph.NodePlacement{{
				Node:      "boutique-m03",
				Resources: nodeResources(8, 1400, 32000),
				Pods:      []graph.PodInfo{{Name: "loadgenerator-pod"}},
			}}},
		},
	}

	nodes := []graph.NodeWithResources{
		{Name: "boutique-m03", Resources: nodeResources(18, 2000, 32000)},
	}

	snapshot := &graph.MetricsSnapshotResponse{
		Timestamp: "2026-03-06T17:13:00Z",
		Services: []graph.ServiceMetrics{
			{Name: "frontend", Namespace: "onlineboutique", RPS: 420, P95: 4200},
			{Name: "loadgenerator", Namespace: "onlineboutique", RPS: 45, P95: 95},
		},
		Edges: []graph.EdgeSnapshot{{
			From:      "loadgenerator",
			To:        "frontend",
			Namespace: "onlineboutique",
			RPS:       210,
			P95:       95,
		}},
	}

	result := evaluator.EvaluateFromSamples(snapshot, services, nodes)
	if !result.AnomalyActive {
		t.Fatalf("expected anomaly for severe latency saturation")
	}
	if result.Recommendation == nil {
		t.Fatalf("expected recommendation for severe latency saturation")
	}
	if result.Recommendation.ActionType != "ScaleService" {
		t.Fatalf("expected scale recommendation, got %s", result.Recommendation.ActionType)
	}
	if result.Recommendation.DrillType != "PodScaleUp" {
		t.Fatalf("expected PodScaleUp drill type, got %s", result.Recommendation.DrillType)
	}
	if result.Recommendation.Severity != "critical" {
		t.Fatalf("expected critical severity, got %s", result.Recommendation.Severity)
	}
	if result.TimeToImpactSec == nil || *result.TimeToImpactSec > 60 {
		t.Fatalf("expected urgent time to impact <= 60 sec, got %v", result.TimeToImpactSec)
	}
}
