package api

import (
	"context"
	"net/http"
	"time"

	"predictive-analysis-engine/pkg/logger"
	"predictive-analysis-engine/pkg/predictive"
)

// PredictiveActionEvaluator defines the predictive recommendation evaluator contract.
type PredictiveActionEvaluator interface {
	Evaluate(ctx context.Context) (predictive.CurrentActionResponse, error)
}

// PredictiveCurrentActionHandler godoc
// @Summary Get Current Predictive Recommendation
// @Description Returns the current anomaly state and recommended manual action derived from live metrics.
// @Tags predictive
// @Produce json
// @Success 200 {object} predictive.CurrentActionResponse
// @Failure 503 {object} map[string]string
// @Router /predictive/actions/current [get]
func (h *Handler) PredictiveCurrentActionHandler(w http.ResponseWriter, r *http.Request) {
	if h.PredictiveEvaluator == nil {
		respondJSON(w, http.StatusOK, predictive.CurrentActionResponse{
			AnomalyActive:     false,
			HealthScore:       100,
			PrimaryBottleneck: nil,
			TimeToImpactSec:   nil,
			Recommendation:    nil,
			Evidence: predictive.Evidence{
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			},
		})
		return
	}

	payload, err := h.PredictiveEvaluator.Evaluate(r.Context())
	if err != nil {
		logger.Error("Failed to evaluate predictive action", err)
		respondError(w, http.StatusServiceUnavailable, "Failed to evaluate predictive action")
		return
	}

	respondJSON(w, http.StatusOK, payload)
}
