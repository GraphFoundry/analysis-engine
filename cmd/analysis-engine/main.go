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
	httpSwagger "github.com/swaggo/http-swagger/v2"

	"predictive-analysis-engine/pkg/api"
	"predictive-analysis-engine/pkg/clients/graph"
	"predictive-analysis-engine/pkg/clients/telemetry"
	"predictive-analysis-engine/pkg/config"
	"predictive-analysis-engine/pkg/drills"
	"predictive-analysis-engine/pkg/simulation"
	"predictive-analysis-engine/pkg/storage"
	"predictive-analysis-engine/pkg/worker"
)

// @title Predictive Analysis Engine API
// @version 1.0
// @description API for the Predictive Analysis Engine, responsible for failure simulation, scaling analysis, and risk assessment.
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.email support@example.com

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

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

	simService := simulation.NewService(cfg, graphClient, store)

	apiHandler := api.NewHandler(cfg, graphClient, simService)
	decisionsHandler := &api.DecisionsHandler{Store: store}
	telemetryHandler := &api.TelemetryHandler{Client: telemetryClient, Cfg: cfg}

	drillEngine := drills.NewEngine(store, graphClient, telemetryClient, drills.EngineOptions{
		K8sClientOptions: drills.K8sClientOptions{
			KubeconfigPath: cfg.Drills.KubeconfigPath,
			KubeContext:    cfg.Drills.KubeContext,
			APIServer:      cfg.Drills.KubeAPIServer,
		},
		TargetedLoad: drills.TargetedLoadActionOptions{
			DeploymentName: cfg.Drills.LoadGeneratorDeployment,
			ContainerName:  cfg.Drills.LoadGeneratorContainer,
			RateEnvName:    cfg.Drills.TargetedLoadRateEnv,
			UsersEnvName:   cfg.Drills.TargetedLoadUsersEnv,
		},
	})
	drillsHandler := &api.DrillsHandler{Engine: drillEngine, Store: store}

	r := chi.NewRouter()

	r.Use(api.CORSMiddleware)
	r.Use(api.CorrelationMiddleware)

	// Swagger UI
	r.Get("/swagger/doc.json", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "docs/swagger.json")
	})
	r.Get("/swagger/*", httpSwagger.Handler(
		httpSwagger.URL("doc.json"), // The url pointing to API definition
	))

	r.Get("/health", apiHandler.HealthHandler)
	r.Get("/services", apiHandler.ServicesHandler)
	r.Get("/infrastructure/nodes", apiHandler.NodesHandler)
	r.Get("/risk/services/top", apiHandler.TopRiskHandler)
	r.Post("/simulate/failure", apiHandler.SimulateFailureHandler)
	r.Post("/simulate/scale", apiHandler.SimulateScalingHandler)
	r.Post("/simulate/add", apiHandler.SimulateAddHandler)
	r.Get("/simulate/context", apiHandler.SimulateContextHandler)
	r.Get("/simulations/capabilities", apiHandler.SimulationCapabilitiesHandler)
	r.Get("/demo/snapshots", apiHandler.DemoSnapshotsHandler)
	r.Get("/dependency-graph/snapshot", apiHandler.DependencyGraphHandler)
	r.Get("/predictive/actions/current", apiHandler.PredictiveCurrentActionHandler)

	decisionsHandler.RegisterRoutes(r)
	drillsHandler.RegisterRoutes(r)
	r.Mount("/telemetry", telemetryHandler.Routes())

	// Webhook endpoint: receives graph updates from service-graph-engine
	webhookHandler := api.NewWebhookHandler(cfg, telemetryClient, store)
	r.Post("/webhook/graph-update", webhookHandler.HandleGraphUpdate)
	r.Get("/webhook/status", webhookHandler.HandleWebhookStatus)

	// Only start PollWorker if webhook mode is disabled (fallback)
	var pollWorker *worker.PollWorker
	if !cfg.Webhook.Enabled {
		log.Println("Webhook mode disabled - starting PollWorker for backward compatibility")
		pollWorker = worker.NewPollWorker(cfg, graphClient, telemetryClient)
		pollWorker.Start()
	} else {
		log.Println("Webhook mode enabled - PollWorker disabled (data pushed via POST /webhook/graph-update)")
	}

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

	if pollWorker != nil {
		pollWorker.Stop()
	}

	telemetryClient.Close()

	log.Println("Server exited")
}
