package simulation

import (
	"fmt"
	"math"
)

const (
	TrafficCritical = 100.0
	TrafficHigh     = 50.0
	TrafficMedium   = 10.0
)

func GenerateFailureRecommendations(result *FailureSimulationResult) []FailureRecommendation {
	var recommendations []FailureRecommendation
	confidence := result.Confidence
	if confidence == "" {
		confidence = "unknown"
	}

	if confidence == "low" {
		recommendations = append(recommendations, FailureRecommendation{
			Type:     "data-quality",
			Priority: "high",
			Target:   "graph-data",
			Reason:   "Graph data is stale (>5 minutes old)",
			Action:   "Verify graph-engine is syncing properly before acting on predictions",
		})
	}

	totalLost := result.TotalLostTrafficRps
	affectedCallers := result.AffectedCallers
	unreachableServices := result.UnreachableServices
	affectedDownstream := result.AffectedDownstream
	targetName := result.Target.Name
	if targetName == "" {
		targetName = "unknown"
	}

	if totalLost >= TrafficCritical {
		recommendations = append(recommendations, FailureRecommendation{
			Type:     "circuit-breaker",
			Priority: "critical",
			Target:   targetName,
			Reason:   fmt.Sprintf("Failure would cause %.1f RPS total traffic loss", totalLost),
			Action:   fmt.Sprintf("Implement circuit breaker with fallback for all callers of %s", targetName),
		})
	}

	if len(affectedCallers) >= 3 {
		recommendations = append(recommendations, FailureRecommendation{
			Type:     "redundancy",
			Priority: "high",
			Target:   targetName,
			Reason:   fmt.Sprintf("%d upstream services depend on %s", len(affectedCallers), targetName),
			Action:   fmt.Sprintf("Deploy %s across multiple availability zones", targetName),
		})
	}

	for _, caller := range affectedCallers {
		if caller.LostTrafficRps >= TrafficHigh {
			callerName := caller.Name
			if callerName == "" {
				callerName = caller.ServiceId
			}
			recommendations = append(recommendations, FailureRecommendation{
				Type:     "circuit-breaker",
				Priority: "high",
				Target:   callerName,
				Reason:   fmt.Sprintf("%s would lose %.1f RPS", callerName, caller.LostTrafficRps),
				Action:   fmt.Sprintf("Add circuit breaker in %s when calling %s", callerName, targetName),
			})
		}
	}

	if len(unreachableServices) > 0 {
		totalUnreachableLoss := 0.0
		for _, s := range unreachableServices {
			totalUnreachableLoss += s.LostTrafficRps
		}

		if len(unreachableServices) >= 2 || totalUnreachableLoss >= TrafficMedium {

			count := 0
			var names []string
			for _, s := range unreachableServices {
				if count >= 3 {
					break
				}
				names = append(names, s.Name)
				count++
			}
			joinedNames := ""
			for i, n := range names {
				if i > 0 {
					joinedNames += ", "
				}
				joinedNames += n
			}

			recommendations = append(recommendations, FailureRecommendation{
				Type:     "topology-review",
				Priority: "medium",
				Target:   targetName,
				Reason:   fmt.Sprintf("%d service(s) become unreachable (cascade risk)", len(unreachableServices)),
				Action:   fmt.Sprintf("Review dependency graph; consider alternative paths for: %s", joinedNames),
			})
		}
	}

	if len(affectedDownstream) > 0 {
		totalDownstreamLoss := 0.0
		for _, s := range affectedDownstream {
			totalDownstreamLoss += s.LostTrafficRps
		}

		if totalDownstreamLoss >= TrafficMedium {
			recommendations = append(recommendations, FailureRecommendation{
				Type:     "graceful-degradation",
				Priority: "medium",
				Target:   targetName,
				Reason:   fmt.Sprintf("Downstream services lose %.1f RPS from %s", totalDownstreamLoss, targetName),
				Action:   fmt.Sprintf("Implement graceful degradation in %s to reduce downstream blast radius", targetName),
			})
		}
	}

	hasDataQualityOnly := len(recommendations) == 1 && recommendations[0].Type == "data-quality"
	if len(recommendations) == 0 || hasDataQualityOnly {
		recommendations = append(recommendations, FailureRecommendation{
			Type:     "monitoring",
			Priority: "low",
			Target:   targetName,
			Reason:   "Low predicted impact, but failures can still occur",
			Action:   fmt.Sprintf("Ensure alerting is configured for %s availability", targetName),
		})
	}

	return recommendations
}

func toFixed(num float64, precision int) float64 {
	output := math.Pow(10, float64(precision))
	return float64(int(num*output)) / output
}
