package simulation

import "fmt"

// SupportedScenarios returns the authoritative ordered list of supported scenario types.
// Any scenario type NOT in this list is unsupported and must not be routed through the
// simulation execution core. Callers can use IsScenarioSupported to gate routing.
func SupportedScenarios() []ScenarioType {
	return []ScenarioType{
		ScenarioFailureShutdown,
		ScenarioScaling,
		ScenarioTrafficSpike,
		ScenarioChattyColocation,
		ScenarioNetworkCut,
	}
}

// IsScenarioSupported reports whether t is one of the five locked supported scenarios.
func IsScenarioSupported(t ScenarioType) bool {
	_, ok := validScenarioTypes[t]
	return ok
}

// EvidenceSufficientForScenario reports whether the evidence in ctx is sufficient
// to produce a defensible simulation output for the given scenario. When evidence
// is insufficient, it returns false and a human-readable reason explaining why.
//
// Rules:
//   - FALLBACK mode means no live graph and no live runtime data are available.
//     All five scenarios require at least one live tier (graph OR runtime) to produce
//     defensible per-service impact output; FALLBACK alone is not enough.
//   - For all other evidence modes (FULL, PARTIAL, DEGRADED) the simulation can
//     proceed and must declare its assumptions and degraded state explicitly.
func EvidenceSufficientForScenario(ctx ExecutionContext) (sufficient bool, reason string) {
	mode := ctx.Evidence.Mode
	if mode == EvidenceModeFallback {
		return false, fmt.Sprintf(
			"evidence mode is FALLBACK (no live service graph or runtime data available); "+
				"scenario %q requires at least one live evidence tier to produce defensible output",
			ctx.Request.ScenarioType,
		)
	}
	return true, ""
}

// BuildDeferredResponse constructs a SimulationResponse with ResultStatus=DEFERRED
// and the provided deferral reason. It carries all evidence/snapshot metadata from
// ctx but sets no BeforeAfterValues, no ImpactedServices, no ImpactedPaths,
// no Assumptions, and no Recommendation.Action, guaranteeing that no guessed
// numeric values are labeled as accurate.
func BuildDeferredResponse(ctx ExecutionContext, reason string) SimulationResponse {
	resp := BuildBaseResponse(ctx)
	resp.ResultStatus = ResultStatusDeferred
	resp.DeferredReason = reason
	// Explicitly initialize slices to empty (not nil) for consistent JSON serialization.
	resp.ImpactedServices = []ImpactedService{}
	resp.ImpactedPaths = []ImpactedPath{}
	resp.BeforeAfterValues = []BeforeAfterValue{}
	resp.Assumptions = []SimulationAssumption{}
	return resp
}

// BuildUnsupportedResponse constructs a SimulationResponse with ResultStatus=UNSUPPORTED
// and the provided reason. Semantically used when a scenario type or parameter combination
// is outside the supported contract, as opposed to a transient data-availability deferral.
// Like BuildDeferredResponse, no numeric output fields are populated.
func BuildUnsupportedResponse(ctx ExecutionContext, reason string) SimulationResponse {
	resp := BuildBaseResponse(ctx)
	resp.ResultStatus = ResultStatusUnsupported
	resp.DeferredReason = reason
	resp.ImpactedServices = []ImpactedService{}
	resp.ImpactedPaths = []ImpactedPath{}
	resp.BeforeAfterValues = []BeforeAfterValue{}
	resp.Assumptions = []SimulationAssumption{}
	return resp
}

// EnforceDeferredConstraints strips any numeric output fields (BeforeAfterValues,
// ImpactedServices, ImpactedPaths, Assumptions, Recommendation.Action/Explanation)
// from resp if its ResultStatus is DEFERRED or UNSUPPORTED. This ensures that
// deferred/unsupported results can never leak guessed numeric values.
// Call this before serializing any response.
func EnforceDeferredConstraints(resp *SimulationResponse) {
	if resp.ResultStatus == ResultStatusDeferred || resp.ResultStatus == ResultStatusUnsupported {
		resp.BeforeAfterValues = []BeforeAfterValue{}
		resp.ImpactedServices = []ImpactedService{}
		resp.ImpactedPaths = []ImpactedPath{}
		resp.Assumptions = []SimulationAssumption{}
		resp.Recommendation = SimulationRecommendation{}
	}
}

// ValidateDeferredConstraints returns an error if a DEFERRED or UNSUPPORTED response
// contains non-empty numeric output fields that would falsely imply accurate simulation
// results. This is the enforcement-time check counterpart to EnforceDeferredConstraints.
func ValidateDeferredConstraints(resp SimulationResponse) error {
	if resp.ResultStatus != ResultStatusDeferred && resp.ResultStatus != ResultStatusUnsupported {
		return nil
	}
	if len(resp.BeforeAfterValues) > 0 {
		return fmt.Errorf(
			"deferred/unsupported response must not contain BeforeAfterValues (got %d entries); "+
				"numeric output in a deferred result would be labeled as accurate",
			len(resp.BeforeAfterValues),
		)
	}
	if resp.Recommendation.Action != "" {
		return fmt.Errorf(
			"deferred/unsupported response must not contain recommendation.action %q; "+
				"an actionable recommendation in a deferred result implies false accuracy",
			resp.Recommendation.Action,
		)
	}
	return nil
}
