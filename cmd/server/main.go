package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"

	"predictive-analysis-engine/pkg/api"
	"predictive-analysis-engine/pkg/clients/graph"
	"predictive-analysis-engine/pkg/clients/telemetry"
	"predictive-analysis-engine/pkg/config"
	"predictive-analysis-engine/pkg/middleware"
	"predictive-analysis-engine/pkg/storage"
	"predictive-analysis-engine/pkg/worker"
)

func main() {

	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, using environment variables")
	}

	if err := config.ValidateEnv(); err != nil {
		log.Fatalf("❌ Configuration Error: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Predictive Analysis Engine started on port %d", cfg.Server.Port)
	log.Printf("Graph Engine URL: %s", cfg.GraphAPI.BaseURL)
	log.Printf("Decision Store: %s", cfg.SQLite.DBPath)

	store, err := storage.NewDecisionStore(cfg.SQLite.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize DecisionStore: %v", err)
	}
	defer store.Close()

	graphClient := graph.NewClient(cfg.GraphAPI)
	telemetryClient := telemetry.NewClient(cfg)

	apiHandler := api.NewHandler(cfg, graphClient, store)
	decisionsHandler := &api.DecisionsHandler{Store: store}
	telemetryHandler := &api.TelemetryHandler{Client: telemetryClient, Cfg: cfg}

	r := chi.NewRouter()

	r.Use(middleware.CorrelationMiddleware)

	r.Get("/health", apiHandler.HealthHandler)
	r.Get("/services", apiHandler.ServicesHandler)
	r.Get("/risk/services/top", apiHandler.TopRiskHandler)
	r.Post("/simulate/failure", apiHandler.SimulateFailureHandler)
	r.Post("/simulate/scale", apiHandler.SimulateScalingHandler)
	r.Post("/simulate/add", apiHandler.SimulateAddHandler)
	r.Get("/api/dependency-graph/snapshot", apiHandler.DependencyGraphHandler)
	r.Get("/dependency-graph/snapshot", apiHandler.DependencyGraphHandler)

	decisionsHandler.RegisterRoutes(r)
	r.Mount("/telemetry", telemetryHandler.Routes())

	pollWorker := worker.NewPollWorker(cfg, graphClient, telemetryClient)
	pollWorker.Start()

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	pollWorker.Stop()

	telemetryClient.Close()

	log.Println("Server exited")
}
