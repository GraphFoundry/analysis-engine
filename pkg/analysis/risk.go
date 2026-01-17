package analysis

import (
	"context"
	"fmt"
	"strings"

	"predictive-analysis-engine/pkg/clients/graph"
)

const (
	RiskHighThreshold   = 0.2
	RiskMediumThreshold = 0.5
)

func GetTopRiskServices(ctx context.Context, client *graph.Client, metric string, limit int) (*graph.TopCentralityResponse, error) {

	if metric != "pagerank" && metric != "betweenness" {
		return nil, fmt.Errorf("Invalid metric: %s. Allowed: pagerank, betweenness", metric)
	}

	centralityResult, err := client.GetTopCentrality(ctx, metric, limit)
	if err != nil {
		return nil, fmt.Errorf("Failed to fetch centrality data: %w", err)
	}

	healthResult, err := client.CheckHealth(ctx)

	var dataFreshness graph.DataFreshness
	confidence := "unknown"

	if err == nil && healthResult != nil {
		dataFreshness = graph.DataFreshness{
			Source:                "graph-engine",
			Stale:                 healthResult.Stale,
			LastUpdatedSecondsAgo: healthResult.LastUpdatedSecondsAgo,
			WindowMinutes:         healthResult.WindowMinutes,
		}
		if healthResult.Stale {
			confidence = "low"
		} else {
			confidence = "high"
		}
	} else {

	}

	topServices := centralityResult.Top
	if topServices == nil {
		topServices = []graph.CentralityScore{}
	}
	total := len(topServices)

	var services []graph.CentralityServiceInfo
	for rank, item := range topServices {
		score := item.Value
		riskLevel := determineRiskLevel(score, rank, total)

		id, name, namespace := parseServiceIdentifier(item.Service)
		explanation := generateExplanation(name, metric, score, riskLevel)

		services = append(services, graph.CentralityServiceInfo{
			ServiceId:       id,
			Name:            name,
			Namespace:       namespace,
			CentralityScore: score,
			RiskLevel:       riskLevel,
			Explanation:     explanation,
		})
	}

	return &graph.TopCentralityResponse{
		Metric:        metric,
		Services:      services,
		DataFreshness: dataFreshness,
		Confidence:    confidence,
	}, nil
}

func determineRiskLevel(score float64, rank int, total int) string {
	if total == 0 {
		return "low"
	}
	percentile := float64(rank) / float64(total)

	if score > 0 && percentile < RiskHighThreshold {
		return "high"
	} else if score > 0 && percentile < RiskMediumThreshold {
		return "medium"
	}
	return "low"
}

func generateExplanation(name, metric string, score float64, riskLevel string) string {
	metricLabel := "betweenness centrality"
	if metric == "pagerank" {
		metricLabel = "PageRank"
	}

	valStr := fmt.Sprintf("%.4f", score)

	switch riskLevel {
	case "high":
		return fmt.Sprintf("%s has high %s (%s), indicating it is a critical hub. Failure could cascade widely.", name, metricLabel, valStr)
	case "medium":
		return fmt.Sprintf("%s has moderate %s (%s). Monitor for dependencies.", name, metricLabel, valStr)
	default:
		return fmt.Sprintf("%s has low %s (%s). Lower risk of cascade.", name, metricLabel, valStr)
	}
}

func parseServiceIdentifier(raw string) (serviceId, name, namespace string) {
	if strings.Contains(raw, ":") {
		parts := strings.SplitN(raw, ":", 2)
		return raw, parts[1], parts[0]
	}
	return fmt.Sprintf("default:%s", raw), raw, "default"
}
