package simulation

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// SchemaVersion is the version of the simulation request/response contract.
const SchemaVersion = "v1"

// ScenarioType enumerates the five locked simulation scenarios.
type ScenarioType string

const (
	ScenarioFailureShutdown   ScenarioType = "failure_shutdown"
	ScenarioScaling           ScenarioType = "scaling"
	ScenarioTrafficSpike      ScenarioType = "traffic_spike"
	ScenarioChattyColocation  ScenarioType = "chatty_colocation"
	ScenarioNetworkCut        ScenarioType = "network_cut"
)

// validScenarioTypes is the authoritative set of supported scenarios.
var validScenarioTypes = map[ScenarioType]struct{}{
	ScenarioFailureShutdown:  {},
	ScenarioScaling:          {},
	ScenarioTrafficSpike:     {},
	ScenarioChattyColocation: {},
	ScenarioNetworkCut:       {},
}

// Stable validation error codes.
const (
	ErrCodeMissingVersion         = "SIM_ERR_001"
	ErrCodeInvalidVersion         = "SIM_ERR_002"
	ErrCodeMissingScenarioType    = "SIM_ERR_003"
	ErrCodeUnsupportedScenario    = "SIM_ERR_004"
	ErrCodeMissingSnapshotRef     = "SIM_ERR_005"
	ErrCodeInvalidSnapshotTS      = "SIM_ERR_006"
	ErrCodeMissingScenarioParams  = "SIM_ERR_007"
	ErrCodeInvalidScenarioParams  = "SIM_ERR_008"
)

// ValidationError carries a stable error code plus a human-readable message.
type ValidationError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// ValidationErrors is a slice of ValidationError, satisfying the error interface.
type ValidationErrors []ValidationError

func (ve ValidationErrors) Error() string {
	msgs := make([]string, len(ve))
	for i, e := range ve {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "; ")
}

// SimulationRequest is the canonical versioned request schema for all simulation scenarios.
// Both SnapshotTimestamp and SnapshotHash are required to anchor outputs to an immutable snapshot.
type SimulationRequest struct {
	// Version must be "v1".
	Version string `json:"version"`

	// ScenarioType selects one of the five locked scenarios.
	ScenarioType ScenarioType `json:"scenarioType"`

	// SnapshotTimestamp is a UTC RFC3339 timestamp identifying the snapshot moment.
	SnapshotTimestamp string `json:"snapshotTimestamp"`

	// SnapshotHash is a deterministic hash derived from canonicalized snapshot content.
	// Optional on intake but strongly recommended for replay determinism.
	SnapshotHash string `json:"snapshotHash,omitempty"`

	// Exactly one of the following parameter fields must be populated,
	// corresponding to the chosen ScenarioType.

	FailureShutdownParams  *FailureShutdownParams  `json:"failureShutdownParams,omitempty"`
	ScalingParams          *ScalingParams          `json:"scalingParams,omitempty"`
	TrafficSpikeParams     *TrafficSpikeParams     `json:"trafficSpikeParams,omitempty"`
	ChattyColocationParams *ChattyColocationParams `json:"chattyColocationParams,omitempty"`
	NetworkCutParams       *NetworkCutParams       `json:"networkCutParams,omitempty"`
}

// FailureShutdownParams carries parameters for the Failure / Service Shutdown scenario.
type FailureShutdownParams struct {
	// TargetServiceID is the service being shut down (required).
	TargetServiceID string `json:"targetServiceId"`
	// MaxDepth bounds the blast-radius traversal (optional; 0 means use engine default).
	MaxDepth int `json:"maxDepth,omitempty"`
}

// ScalingParams carries parameters for the Scaling up/down scenario.
type ScalingParams struct {
	// TargetServiceID identifies the service being scaled (required).
	TargetServiceID string `json:"targetServiceId"`
	// CurrentPods is the number of pod replicas before scaling (required, >0).
	CurrentPods int `json:"currentPods"`
	// NewPods is the desired number of pod replicas after scaling (required, >0).
	NewPods int `json:"newPods"`
	// LatencyMetric selects which latency percentile to project (optional; default "p95").
	LatencyMetric string `json:"latencyMetric,omitempty"`
}

// TrafficSpikeParams carries parameters for the Traffic Spike / targeted load scenario.
type TrafficSpikeParams struct {
	// TargetServiceID is the service receiving the load spike (required).
	TargetServiceID string `json:"targetServiceId"`
	// LoadMultiplier is the relative increase factor (e.g. 3.0 = 3× baseline; required, >1.0).
	LoadMultiplier float64 `json:"loadMultiplier"`
}

// ChattyColocationParams carries parameters for the Chatty-service co-location / migration scenario.
type ChattyColocationParams struct {
	// SourceServiceID is the chatty caller (required).
	SourceServiceID string `json:"sourceServiceId"`
	// TargetServiceID is the chatty callee (required).
	TargetServiceID string `json:"targetServiceId"`
}

// NetworkCutParams carries parameters for the Network Cut / network degradation scenario.
type NetworkCutParams struct {
	// AffectedLinks lists source→target service-ID pairs representing the cut links (required, non-empty).
	AffectedLinks []NetworkLink `json:"affectedLinks"`
	// DegradationPercent expresses packet-loss or latency-addition as a percentage [0,100] (optional).
	DegradationPercent *float64 `json:"degradationPercent,omitempty"`
}

// NetworkLink describes a directed service communication edge subject to network disruption.
type NetworkLink struct {
	SourceServiceID string `json:"sourceServiceId"`
	TargetServiceID string `json:"targetServiceId"`
}

// ValidateSimulationRequest validates req and returns a deterministic set of ValidationErrors.
// It checks version, scenario type, snapshot reference, and scenario-specific parameters.
// Returns nil if validation passes.
func ValidateSimulationRequest(req SimulationRequest) error {
	var errs ValidationErrors

	// --- Version ---
	if req.Version == "" {
		errs = append(errs, ValidationError{Code: ErrCodeMissingVersion, Message: "version is required"})
	} else if req.Version != SchemaVersion {
		errs = append(errs, ValidationError{
			Code:    ErrCodeInvalidVersion,
			Message: fmt.Sprintf("unsupported version %q; only %q is accepted", req.Version, SchemaVersion),
		})
	}

	// --- ScenarioType ---
	if req.ScenarioType == "" {
		errs = append(errs, ValidationError{Code: ErrCodeMissingScenarioType, Message: "scenarioType is required"})
	} else if _, ok := validScenarioTypes[req.ScenarioType]; !ok {
		errs = append(errs, ValidationError{
			Code: ErrCodeUnsupportedScenario,
			Message: fmt.Sprintf(
				"unsupported scenarioType %q; supported values: failure_shutdown, scaling, traffic_spike, chatty_colocation, network_cut",
				req.ScenarioType,
			),
		})
	}

	// --- Snapshot reference ---
	if req.SnapshotTimestamp == "" {
		errs = append(errs, ValidationError{Code: ErrCodeMissingSnapshotRef, Message: "snapshotTimestamp is required"})
	} else {
		if _, err := time.Parse(time.RFC3339, req.SnapshotTimestamp); err != nil {
			errs = append(errs, ValidationError{
				Code:    ErrCodeInvalidSnapshotTS,
				Message: fmt.Sprintf("snapshotTimestamp must be a valid RFC3339 UTC timestamp; got %q", req.SnapshotTimestamp),
			})
		}
	}

	// --- Scenario-specific parameter validation ---
	// Only validate params when ScenarioType is known and valid.
	if _, ok := validScenarioTypes[req.ScenarioType]; ok {
		paramErrs := validateScenarioParams(req)
		errs = append(errs, paramErrs...)
	}

	if len(errs) == 0 {
		return nil
	}
	return errs
}

// validateScenarioParams checks that the correct params block is populated
// and that its required fields are present.
func validateScenarioParams(req SimulationRequest) ValidationErrors {
	var errs ValidationErrors

	switch req.ScenarioType {
	case ScenarioFailureShutdown:
		if req.FailureShutdownParams == nil {
			errs = append(errs, ValidationError{
				Code:    ErrCodeMissingScenarioParams,
				Message: "failureShutdownParams is required for scenarioType failure_shutdown",
			})
		} else if strings.TrimSpace(req.FailureShutdownParams.TargetServiceID) == "" {
			errs = append(errs, ValidationError{
				Code:    ErrCodeInvalidScenarioParams,
				Message: "failureShutdownParams.targetServiceId must not be empty",
			})
		}

	case ScenarioScaling:
		if req.ScalingParams == nil {
			errs = append(errs, ValidationError{
				Code:    ErrCodeMissingScenarioParams,
				Message: "scalingParams is required for scenarioType scaling",
			})
		} else {
			p := req.ScalingParams
			if strings.TrimSpace(p.TargetServiceID) == "" {
				errs = append(errs, ValidationError{
					Code:    ErrCodeInvalidScenarioParams,
					Message: "scalingParams.targetServiceId must not be empty",
				})
			}
			if p.CurrentPods <= 0 {
				errs = append(errs, ValidationError{
					Code:    ErrCodeInvalidScenarioParams,
					Message: "scalingParams.currentPods must be greater than 0",
				})
			}
			if p.NewPods <= 0 {
				errs = append(errs, ValidationError{
					Code:    ErrCodeInvalidScenarioParams,
					Message: "scalingParams.newPods must be greater than 0",
				})
			}
		}

	case ScenarioTrafficSpike:
		if req.TrafficSpikeParams == nil {
			errs = append(errs, ValidationError{
				Code:    ErrCodeMissingScenarioParams,
				Message: "trafficSpikeParams is required for scenarioType traffic_spike",
			})
		} else {
			p := req.TrafficSpikeParams
			if strings.TrimSpace(p.TargetServiceID) == "" {
				errs = append(errs, ValidationError{
					Code:    ErrCodeInvalidScenarioParams,
					Message: "trafficSpikeParams.targetServiceId must not be empty",
				})
			}
			if p.LoadMultiplier <= 1.0 {
				errs = append(errs, ValidationError{
					Code:    ErrCodeInvalidScenarioParams,
					Message: "trafficSpikeParams.loadMultiplier must be greater than 1.0",
				})
			}
		}

	case ScenarioChattyColocation:
		if req.ChattyColocationParams == nil {
			errs = append(errs, ValidationError{
				Code:    ErrCodeMissingScenarioParams,
				Message: "chattyColocationParams is required for scenarioType chatty_colocation",
			})
		} else {
			p := req.ChattyColocationParams
			if strings.TrimSpace(p.SourceServiceID) == "" {
				errs = append(errs, ValidationError{
					Code:    ErrCodeInvalidScenarioParams,
					Message: "chattyColocationParams.sourceServiceId must not be empty",
				})
			}
			if strings.TrimSpace(p.TargetServiceID) == "" {
				errs = append(errs, ValidationError{
					Code:    ErrCodeInvalidScenarioParams,
					Message: "chattyColocationParams.targetServiceId must not be empty",
				})
			}
		}

	case ScenarioNetworkCut:
		if req.NetworkCutParams == nil {
			errs = append(errs, ValidationError{
				Code:    ErrCodeMissingScenarioParams,
				Message: "networkCutParams is required for scenarioType network_cut",
			})
		} else {
			p := req.NetworkCutParams
			if len(p.AffectedLinks) == 0 {
				errs = append(errs, ValidationError{
					Code:    ErrCodeInvalidScenarioParams,
					Message: "networkCutParams.affectedLinks must contain at least one link",
				})
			}
			for i, link := range p.AffectedLinks {
				if strings.TrimSpace(link.SourceServiceID) == "" {
					errs = append(errs, ValidationError{
						Code:    ErrCodeInvalidScenarioParams,
						Message: fmt.Sprintf("networkCutParams.affectedLinks[%d].sourceServiceId must not be empty", i),
					})
				}
				if strings.TrimSpace(link.TargetServiceID) == "" {
					errs = append(errs, ValidationError{
						Code:    ErrCodeInvalidScenarioParams,
						Message: fmt.Sprintf("networkCutParams.affectedLinks[%d].targetServiceId must not be empty", i),
					})
				}
			}
			if p.DegradationPercent != nil {
				if *p.DegradationPercent < 0 || *p.DegradationPercent > 100 {
					errs = append(errs, ValidationError{
						Code:    ErrCodeInvalidScenarioParams,
						Message: "networkCutParams.degradationPercent must be between 0 and 100",
					})
				}
			}
		}
	}

	return errs
}

// IsValidationErrors returns true and the typed errors if err is a ValidationErrors value.
func IsValidationErrors(err error) (ValidationErrors, bool) {
	var ve ValidationErrors
	if errors.As(err, &ve) {
		return ve, true
	}
	return nil, false
}
