package simulation

import (
	"context"
	"time"

	"predictive-analysis-engine/pkg/clients/graph"
	"predictive-analysis-engine/pkg/common"
	"predictive-analysis-engine/pkg/config"
	"predictive-analysis-engine/pkg/logger"
	"predictive-analysis-engine/pkg/storage"
)

type Service struct {
	graphClient   *graph.Client
	decisionStore *storage.DecisionStore
	config        *config.Config
}

func NewService(cfg *config.Config, gc *graph.Client, ds *storage.DecisionStore) *Service {
	return &Service{
		config:        cfg,
		graphClient:   gc,
		decisionStore: ds,
	}
}

func (s *Service) RunFailureSimulation(ctx context.Context, req FailureSimulationRequest) (*FailureSimulationResult, error) {
	result, err := SimulateFailure(ctx, s.graphClient, req)
	if err != nil {
		return nil, err
	}

	if s.decisionStore != nil {
		_, err := s.decisionStore.LogDecision(storage.LogDecisionInput{
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
			Type:          "failure",
			Scenario:      req,
			Result:        result,
			CorrelationID: common.GetCorrelationID(ctx),
		})
		if err != nil {
			logger.Error("Failed to log decision", err)
		}
	}

	return result, nil
}

func (s *Service) RunScalingSimulation(ctx context.Context, req ScalingSimulationRequest) (*ScalingSimulationResult, error) {
	result, err := SimulateScaling(ctx, s.graphClient, s.config, req)
	if err != nil {
		return nil, err
	}

	if s.decisionStore != nil {
		_, err := s.decisionStore.LogDecision(storage.LogDecisionInput{
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
			Type:          "scaling",
			Scenario:      req,
			Result:        result,
			CorrelationID: common.GetCorrelationID(ctx),
		})
		if err != nil {
			logger.Error("Failed to log decision", err)
		}
	}

	return result, nil
}

func (s *Service) RunAddSimulation(ctx context.Context, req AddSimulationRequest) (*AddSimulationResult, error) {
	result, err := SimulateAddService(ctx, s.graphClient, req)
	if err != nil {
		return nil, err
	}

	if s.decisionStore != nil {
		_, err := s.decisionStore.LogDecision(storage.LogDecisionInput{
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
			Type:          "add",
			Scenario:      req,
			Result:        result,
			CorrelationID: common.GetCorrelationID(ctx),
		})
		if err != nil {
			logger.Error("Failed to log decision", err)
		}
	}

	return result, nil
}
