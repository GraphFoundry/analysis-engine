package simulation

import (
	"fmt"
	"strings"
)

// ConfidenceLevel classifies how confident the simulation output is, based on available evidence tiers.
type ConfidenceLevel string

const (
	ConfidenceHigh   ConfidenceLevel = "HIGH"
	ConfidenceMedium ConfidenceLevel = "MEDIUM"
	ConfidenceLow    ConfidenceLevel = "LOW"
)

// EvidenceMode describes which evidence tier(s) were used to produce the simulation output.
type EvidenceMode string

const (
	EvidenceModeFull     EvidenceMode = "FULL"     // live graph + live runtime + Influx history
	EvidenceModePartial  EvidenceMode = "PARTIAL"  // live graph + live runtime, no Influx history
	EvidenceModeDegraded EvidenceMode = "DEGRADED" // deterministic fallback only (Influx unavailable/sparse)
	EvidenceModeFallback EvidenceMode = "FALLBACK" // deterministic fallback used for all tiers
)

// DegradedMode describes why degraded evidence mode is active.
type DegradedMode string

const (
	DegradedModeNone         DegradedMode = ""              // not in degraded mode
	DegradedModeInfluxEmpty  DegradedMode = "INFLUX_EMPTY"  // InfluxDB returned no data
	DegradedModeInfluxSparse DegradedMode = "INFLUX_SPARSE" // InfluxDB data insufficient
	DegradedModeInfluxError  DegradedMode = "INFLUX_ERROR"  // InfluxDB query failed
)

// SimulationResultStatus describes whether the result is actionable or deferred/unsupported.
type SimulationResultStatus string

const (
	ResultStatusOK          SimulationResultStatus = "OK"
	ResultStatusDeferred    SimulationResultStatus = "DEFERRED"
	ResultStatusUnsupported SimulationResultStatus = "UNSUPPORTED"
)

// AssumptionType classifies the machine-readable structure of an assumption.
type AssumptionType string

const (
	AssumptionTypeModelConstant   AssumptionType = "MODEL_CONSTANT"
	AssumptionTypeFormula         AssumptionType = "FORMULA"
	AssumptionTypeEvidenceBinding AssumptionType = "EVIDENCE_BINDING"
	AssumptionTypeClassification  AssumptionType = "CLASSIFICATION"
)

// ImpactedService identifies a service affected by the simulation.
type ImpactedService struct {
	ServiceID string `json:"serviceId"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Role      string `json:"role"` // e.g. "caller", "downstream", "target"
}

// ImpactedPath describes a service communication path affected by the simulation.
type ImpactedPath struct {
	Path []string `json:"path"` // ordered list of service IDs
}

// BeforeAfterValue captures a before/after numeric measurement for a simulation output field.
type BeforeAfterValue struct {
	// FieldRef identifies this value for UI/BFF traceability mapping.
	FieldRef string `json:"fieldRef"`
	// TraceRef is the stable response-field mapping path used by BFF/UI bindings.
	TraceRef    string   `json:"traceRef"`
	Description string   `json:"description"`
	Unit        string   `json:"unit,omitempty"`
	BeforeValue *float64 `json:"beforeValue"`
	AfterValue  *float64 `json:"afterValue"`
	DeltaValue  *float64 `json:"deltaValue,omitempty"`
}

// SimulationAssumption is a single declared, machine-readable assumption used in the simulation.
type SimulationAssumption struct {
	Key         string         `json:"key"`
	Type        AssumptionType `json:"type"`
	Value       string         `json:"value"`
	Description string         `json:"description"`
	Source      string         `json:"source"` // e.g. evidence source label or "engine_default"
	TraceRef    string         `json:"traceRef"`
}

// SimulationRecommendation is the operator recommendation output.
type SimulationRecommendation struct {
	Action             string   `json:"action"`                       // e.g. "scale_up", "co_locate", "migrate", "no_change", "failover"
	Explanation        string   `json:"explanation"`                  // human-readable rationale citing evidence sources
	EvidenceSourceRefs []string `json:"evidenceSourceRefs,omitempty"` // evidence source labels used in recommendation selection
}

// SimulationResponse is the canonical versioned response schema for all simulation scenarios.
//
// Required fields (must always be populated for OK status responses):
//   - Version, ScenarioType, SnapshotTimestamp, ResultStatus
//   - EvidenceSources, EvidenceMode, ConfidenceLevel
//   - ImpactedServices, ImpactedPaths, BeforeAfterValues
//   - Recommendation (including Explanation and EvidenceSourceRefs), Assumptions
//
// Optional fields (may be absent for deferred/unsupported or when unavailable):
//   - SnapshotHash, DegradedMode, DegradedModeReason, DeferredReason
//
// Unknown top-level fields from external sources must not be added here; use strict JSON
// unmarshalling (DisallowUnknownFields) at the API boundary.
type SimulationResponse struct {
	// Version mirrors the request schema version.
	Version string `json:"version"`

	// ScenarioType echoes the requested scenario for traceability.
	ScenarioType ScenarioType `json:"scenarioType"`

	// SnapshotTimestamp is the UTC RFC3339 timestamp from the request snapshot.
	SnapshotTimestamp string `json:"snapshotTimestamp"`

	// SnapshotHash is the deterministic hash of the snapshot content (optional but recommended).
	SnapshotHash string `json:"snapshotHash,omitempty"`

	// ResultStatus indicates whether the simulation produced actionable output (OK),
	// was deferred due to insufficient evidence, or is unsupported.
	ResultStatus SimulationResultStatus `json:"resultStatus"`

	// DeferredReason explains why results were deferred or unsupported (populated when ResultStatus != OK).
	DeferredReason string `json:"deferredReason,omitempty"`

	// EvidenceSources lists the evidence source labels used in this simulation.
	// Values must be from the defined EvidenceSourceLabel constants.
	EvidenceSources []string `json:"evidenceSources"`

	// EvidenceMode describes which tier combination was active.
	EvidenceMode EvidenceMode `json:"evidenceMode"`

	// DegradedMode is non-empty when Influx history is missing or sparse.
	DegradedMode DegradedMode `json:"degradedMode,omitempty"`

	// DegradedModeReason provides a human-readable explanation of the degraded state.
	DegradedModeReason string `json:"degradedModeReason,omitempty"`

	// ConfidenceLevel is the deterministic confidence classification for this result.
	ConfidenceLevel ConfidenceLevel `json:"confidenceLevel"`

	// Assumptions lists all declared assumptions used to compute the simulation output.
	Assumptions []SimulationAssumption `json:"assumptions"`

	// ImpactedServices lists services affected by the simulated scenario.
	ImpactedServices []ImpactedService `json:"impactedServices"`

	// ImpactedPaths lists service communication paths affected by the scenario.
	ImpactedPaths []ImpactedPath `json:"impactedPaths"`

	// BeforeAfterValues provides deterministic before/after measurements for key output fields.
	// Each value carries a FieldRef for BFF/UI traceability mapping.
	BeforeAfterValues []BeforeAfterValue `json:"beforeAfterValues"`

	// Recommendation is the operator recommendation for this simulation scenario.
	Recommendation SimulationRecommendation `json:"recommendation"`
}

// ValidateSimulationResponse validates a SimulationResponse for required fields and consistency.
// Returns nil if valid, or ValidationErrors describing all problems found.
func ValidateSimulationResponse(resp SimulationResponse) error {
	var errs ValidationErrors

	if resp.Version == "" {
		errs = append(errs, ValidationError{Code: ErrRespCodeMissingVersion, Message: "response version is required"})
	} else if resp.Version != SchemaVersion {
		errs = append(errs, ValidationError{
			Code:    ErrRespCodeInvalidVersion,
			Message: fmt.Sprintf("unsupported response version %q; only %q is accepted", resp.Version, SchemaVersion),
		})
	}

	if resp.ScenarioType == "" {
		errs = append(errs, ValidationError{Code: ErrRespCodeMissingScenarioType, Message: "response scenarioType is required"})
	} else if _, ok := validScenarioTypes[resp.ScenarioType]; !ok {
		errs = append(errs, ValidationError{
			Code:    ErrRespCodeInvalidScenarioType,
			Message: fmt.Sprintf("response scenarioType %q is not a supported scenario", resp.ScenarioType),
		})
	}

	if resp.SnapshotTimestamp == "" {
		errs = append(errs, ValidationError{Code: ErrRespCodeMissingSnapshotTimestamp, Message: "response snapshotTimestamp is required"})
	}

	if resp.ResultStatus == "" {
		errs = append(errs, ValidationError{Code: ErrRespCodeMissingResultStatus, Message: "response resultStatus is required"})
	} else if !isValidResultStatus(resp.ResultStatus) {
		errs = append(errs, ValidationError{
			Code:    ErrRespCodeInvalidResultStatus,
			Message: fmt.Sprintf("response resultStatus %q is not valid; must be OK, DEFERRED, or UNSUPPORTED", resp.ResultStatus),
		})
	}

	if len(resp.EvidenceSources) == 0 {
		errs = append(errs, ValidationError{Code: ErrRespCodeMissingEvidenceSources, Message: "response evidenceSources must not be empty"})
	}

	if resp.EvidenceMode == "" {
		errs = append(errs, ValidationError{Code: ErrRespCodeMissingEvidenceMode, Message: "response evidenceMode is required"})
	} else if !isValidEvidenceMode(resp.EvidenceMode) {
		errs = append(errs, ValidationError{
			Code:    ErrRespCodeInvalidEvidenceMode,
			Message: fmt.Sprintf("response evidenceMode %q is not valid", resp.EvidenceMode),
		})
	}

	if resp.ConfidenceLevel == "" {
		errs = append(errs, ValidationError{Code: ErrRespCodeMissingConfidenceLevel, Message: "response confidenceLevel is required"})
	} else if !isValidConfidenceLevel(resp.ConfidenceLevel) {
		errs = append(errs, ValidationError{
			Code:    ErrRespCodeInvalidConfidenceLevel,
			Message: fmt.Sprintf("response confidenceLevel %q is not valid; must be HIGH, MEDIUM, or LOW", resp.ConfidenceLevel),
		})
	}

	// Degraded-mode consistency: if DegradedMode is set, DegradedModeReason should explain it.
	if resp.DegradedMode != DegradedModeNone && resp.DegradedModeReason == "" {
		errs = append(errs, ValidationError{
			Code:    ErrRespCodeMissingDegradedReason,
			Message: "response degradedModeReason must be provided when degradedMode is set",
		})
	}

	// Deferred/unsupported responses must supply a deferredReason.
	if (resp.ResultStatus == ResultStatusDeferred || resp.ResultStatus == ResultStatusUnsupported) && resp.DeferredReason == "" {
		errs = append(errs, ValidationError{
			Code:    ErrRespCodeMissingDeferredReason,
			Message: "response deferredReason is required when resultStatus is DEFERRED or UNSUPPORTED",
		})
	}

	// OK responses must have a non-empty recommendation action.
	if resp.ResultStatus == ResultStatusOK && resp.Recommendation.Action == "" {
		errs = append(errs, ValidationError{
			Code:    ErrRespCodeMissingRecommendationAction,
			Message: "response recommendation.action is required for OK results",
		})
	}

	if resp.ResultStatus == ResultStatusOK && resp.Recommendation.Explanation == "" {
		errs = append(errs, ValidationError{
			Code:    ErrRespCodeMissingRecommendationExplanation,
			Message: "response recommendation.explanation is required for OK results",
		})
	}

	if resp.ResultStatus == ResultStatusOK && len(resp.Recommendation.EvidenceSourceRefs) == 0 {
		errs = append(errs, ValidationError{
			Code:    ErrRespCodeMissingRecommendationEvidenceRefs,
			Message: "response recommendation.evidenceSourceRefs must include evidence labels used in decision selection",
		})
	}

	for _, evidenceRef := range resp.Recommendation.EvidenceSourceRefs {
		if !containsString(resp.EvidenceSources, evidenceRef) {
			errs = append(errs, ValidationError{
				Code:    ErrRespCodeUnknownRecommendationEvidenceRef,
				Message: fmt.Sprintf("recommendation.evidenceSourceRefs contains %q which is not present in response evidenceSources", evidenceRef),
			})
		}
	}

	for i, bav := range resp.BeforeAfterValues {
		if strings.TrimSpace(bav.FieldRef) == "" {
			errs = append(errs, ValidationError{
				Code:    ErrRespCodeMissingBeforeAfterFieldRef,
				Message: fmt.Sprintf("response beforeAfterValues[%d].fieldRef is required", i),
			})
		}
		if strings.TrimSpace(bav.TraceRef) == "" {
			errs = append(errs, ValidationError{
				Code:    ErrRespCodeMissingBeforeAfterTraceRef,
				Message: fmt.Sprintf("response beforeAfterValues[%d].traceRef is required for field-level UI/BFF mapping", i),
			})
		}
	}

	for i, assumption := range resp.Assumptions {
		if strings.TrimSpace(assumption.Key) == "" {
			errs = append(errs, ValidationError{
				Code:    ErrRespCodeMissingAssumptionKey,
				Message: fmt.Sprintf("response assumptions[%d].key is required", i),
			})
		}
		if assumption.Type == "" {
			errs = append(errs, ValidationError{
				Code:    ErrRespCodeMissingAssumptionType,
				Message: fmt.Sprintf("response assumptions[%d].type is required", i),
			})
		} else if !isValidAssumptionType(assumption.Type) {
			errs = append(errs, ValidationError{
				Code:    ErrRespCodeInvalidAssumptionType,
				Message: fmt.Sprintf("response assumptions[%d].type %q is not valid", i, assumption.Type),
			})
		}
		if strings.TrimSpace(assumption.Value) == "" {
			errs = append(errs, ValidationError{
				Code:    ErrRespCodeMissingAssumptionValue,
				Message: fmt.Sprintf("response assumptions[%d].value is required", i),
			})
		}
		if strings.TrimSpace(assumption.Source) == "" {
			errs = append(errs, ValidationError{
				Code:    ErrRespCodeMissingAssumptionSource,
				Message: fmt.Sprintf("response assumptions[%d].source is required", i),
			})
		}
		if strings.TrimSpace(assumption.TraceRef) == "" {
			errs = append(errs, ValidationError{
				Code:    ErrRespCodeMissingAssumptionTraceRef,
				Message: fmt.Sprintf("response assumptions[%d].traceRef is required", i),
			})
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return errs
}

// isValidResultStatus checks that status is one of the declared values.
func isValidResultStatus(s SimulationResultStatus) bool {
	switch s {
	case ResultStatusOK, ResultStatusDeferred, ResultStatusUnsupported:
		return true
	}
	return false
}

// isValidEvidenceMode checks that mode is one of the declared values.
func isValidEvidenceMode(m EvidenceMode) bool {
	switch m {
	case EvidenceModeFull, EvidenceModePartial, EvidenceModeDegraded, EvidenceModeFallback:
		return true
	}
	return false
}

// isValidConfidenceLevel checks that level is one of the declared values.
func isValidConfidenceLevel(l ConfidenceLevel) bool {
	switch l {
	case ConfidenceHigh, ConfidenceMedium, ConfidenceLow:
		return true
	}
	return false
}

func isValidAssumptionType(t AssumptionType) bool {
	switch t {
	case AssumptionTypeModelConstant, AssumptionTypeFormula, AssumptionTypeEvidenceBinding, AssumptionTypeClassification:
		return true
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// Response-schema stable validation error codes.
const (
	ErrRespCodeMissingVersion                    = "SIM_RESP_ERR_001"
	ErrRespCodeInvalidVersion                    = "SIM_RESP_ERR_002"
	ErrRespCodeMissingScenarioType               = "SIM_RESP_ERR_003"
	ErrRespCodeInvalidScenarioType               = "SIM_RESP_ERR_004"
	ErrRespCodeMissingSnapshotTimestamp          = "SIM_RESP_ERR_005"
	ErrRespCodeMissingResultStatus               = "SIM_RESP_ERR_006"
	ErrRespCodeInvalidResultStatus               = "SIM_RESP_ERR_007"
	ErrRespCodeMissingEvidenceSources            = "SIM_RESP_ERR_008"
	ErrRespCodeMissingEvidenceMode               = "SIM_RESP_ERR_009"
	ErrRespCodeInvalidEvidenceMode               = "SIM_RESP_ERR_010"
	ErrRespCodeMissingConfidenceLevel            = "SIM_RESP_ERR_011"
	ErrRespCodeInvalidConfidenceLevel            = "SIM_RESP_ERR_012"
	ErrRespCodeMissingDegradedReason             = "SIM_RESP_ERR_013"
	ErrRespCodeMissingDeferredReason             = "SIM_RESP_ERR_014"
	ErrRespCodeMissingRecommendationAction       = "SIM_RESP_ERR_015"
	ErrRespCodeMissingRecommendationExplanation  = "SIM_RESP_ERR_016"
	ErrRespCodeMissingRecommendationEvidenceRefs = "SIM_RESP_ERR_017"
	ErrRespCodeUnknownRecommendationEvidenceRef  = "SIM_RESP_ERR_018"
	ErrRespCodeMissingBeforeAfterFieldRef        = "SIM_RESP_ERR_019"
	ErrRespCodeMissingBeforeAfterTraceRef        = "SIM_RESP_ERR_020"
	ErrRespCodeMissingAssumptionKey              = "SIM_RESP_ERR_021"
	ErrRespCodeMissingAssumptionType             = "SIM_RESP_ERR_022"
	ErrRespCodeInvalidAssumptionType             = "SIM_RESP_ERR_023"
	ErrRespCodeMissingAssumptionValue            = "SIM_RESP_ERR_024"
	ErrRespCodeMissingAssumptionSource           = "SIM_RESP_ERR_025"
	ErrRespCodeMissingAssumptionTraceRef         = "SIM_RESP_ERR_026"
)

// SimulationErrorResponse is the error payload returned by the /simulations/run endpoint
// when validation fails or the simulation is deferred/unsupported.
type SimulationErrorResponse struct {
	Error          string            `json:"error"`
	ResultStatus   string            `json:"resultStatus,omitempty"`
	DeferredReason string            `json:"deferredReason,omitempty"`
	Reason         string            `json:"reason,omitempty"`
	Errors         []ValidationError `json:"errors,omitempty"`
}
