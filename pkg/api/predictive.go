package api

import (
	"net/http"
	"time"

	"predictive-analysis-engine/pkg/predictive"
)

// PredictiveCurrentActionHandler godoc
// @Summary Get Current Predictive Recommendation
// @Description Returns the current anomaly state and recommended manual action derived from live metrics.
// @Tags predictive
// @Produce json
// @Success 200 {object} predictive.CurrentActionResponse
// @Failure 503 {object} map[string]string
// @Router /predictive/actions/current [get]
func (h *Handler) PredictiveCurrentActionHandler(w http.ResponseWriter, r *http.Request) {
	// Return the cached result from the most recent webhook-triggered analysis
	if h.WebhookHandler != nil {
		if cached := h.WebhookHandler.GetLatestPredictive(); cached != nil {
			respondJSON(w, http.StatusOK, cached)
			return
		}
	}

	// No webhook data received yet — return healthy default
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
}
