package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Simulation      SimulationConfig
	Server          ServerConfig
	GraphAPI        GraphAPIConfig
	RateLimit       RateLimitConfig
	Influx          InfluxConfig
	SQLite          SQLiteConfig
	TelemetryWorker TelemetryWorkerConfig
	Telemetry       TelemetryConfig
	Webhook         WebhookConfig
	Drills          DrillsConfig
}

type SimulationConfig struct {
	DefaultLatencyMetric string
	MaxTraversalDepth    int
	ScalingModel         string
	ScalingAlpha         float64
	MinLatencyFactor     float64
	TimeoutMs            int
	MaxPathsReturned     int
	SharedHostResources  bool
}

type ServerConfig struct {
	Port int
}

type GraphAPIConfig struct {
	BaseURL   string
	TimeoutMs int
	Namespace string
}

type RateLimitConfig struct {
	WindowMs    int
	MaxRequests int
}

type InfluxConfig struct {
	Host      string
	Token     string
	TokenFile string
	Database  string
}

type SQLiteConfig struct {
	DBPath string
}

type TelemetryWorkerConfig struct {
	Enabled        bool
	PollIntervalMs int
}

type TelemetryConfig struct {
	Enabled bool
}

type WebhookConfig struct {
	Enabled               bool
	Secret                string
	ForwardSecret         string
	ForwardURLs           string
	ReplayWindowSec       int
	DedupeWindowSec       int
	ForwardRetryMax       int
	ForwardRetryBaseMs    int
	ForwardRetryMaxMs     int
	MaxInFlight           int
	ProcessTimeoutMs      int
	AcceptLegacySignature bool
}

type DrillsConfig struct {
	KubeconfigPath          string
	KubeContext             string
	KubeAPIServer           string
	LoadGeneratorDeployment string
	LoadGeneratorContainer  string
	TargetedLoadRateEnv     string
	TargetedLoadUsersEnv    string
}

func Load() (*Config, error) {
	cfg := &Config{
		Simulation: SimulationConfig{
			DefaultLatencyMetric: getEnv("DEFAULT_LATENCY_METRIC", "p95"),
			MaxTraversalDepth:    getEnvInt("MAX_TRAVERSAL_DEPTH", 2),
			ScalingModel:         getEnv("SCALING_MODEL", "bounded_sqrt"),
			ScalingAlpha:         getEnvFloat("SCALING_ALPHA", 0.5),
			MinLatencyFactor:     getEnvFloat("MIN_LATENCY_FACTOR", 0.6),
			TimeoutMs:            getEnvInt("TIMEOUT_MS", 8000),
			MaxPathsReturned:     getEnvInt("MAX_PATHS_RETURNED", 10),
			SharedHostResources:  getEnv("SHARED_HOST_RESOURCES", "false") == "true",
		},
		Server: ServerConfig{
			Port: getEnvInt("PORT", 5000),
		},
		GraphAPI: GraphAPIConfig{
			BaseURL:   getGraphBaseURL(),
			TimeoutMs: getEnvInt("GRAPH_API_TIMEOUT_MS", 15000),
			Namespace: getEnv("OVERVIEW_NAMESPACE", "default"),
		},
		RateLimit: RateLimitConfig{
			WindowMs:    getEnvInt("RATE_LIMIT_WINDOW_MS", 60000),
			MaxRequests: getEnvInt("RATE_LIMIT_MAX", 60),
		},
		Influx: InfluxConfig{
			Host:      getEnv("INFLUX_HOST", ""),
			Token:     getEnv("INFLUX_TOKEN", ""),
			TokenFile: getEnv("INFLUX_TOKEN_FILE", ""),
			Database:  getEnv("INFLUX_DATABASE", ""),
		},
		SQLite: SQLiteConfig{
			DBPath: getEnv("SQLITE_DB_PATH", "./data/decisions.db"),
		},
		TelemetryWorker: TelemetryWorkerConfig{
			Enabled:        getEnv("TELEMETRY_WORKER_ENABLED", "true") != "false",
			PollIntervalMs: getEnvInt("TELEMETRY_POLL_INTERVAL_MS", 60000),
		},
		Telemetry: TelemetryConfig{
			Enabled: getEnv("TELEMETRY_ENABLED", "true") != "false",
		},
		Webhook: WebhookConfig{
			Enabled:               getEnv("WEBHOOK_ENABLED", "true") != "false",
			Secret:                getEnv("WEBHOOK_SECRET", ""),
			ForwardSecret:         getEnv("WEBHOOK_FORWARD_SECRET", ""),
			ForwardURLs:           getEnv("WEBHOOK_FORWARD_URLS", ""),
			ReplayWindowSec:       getEnvInt("WEBHOOK_REPLAY_WINDOW_SEC", 300),
			DedupeWindowSec:       getEnvInt("WEBHOOK_DEDUPE_WINDOW_SEC", 86400),
			ForwardRetryMax:       getEnvInt("WEBHOOK_FORWARD_RETRY_MAX", 5),
			ForwardRetryBaseMs:    getEnvInt("WEBHOOK_FORWARD_RETRY_BASE_DELAY_MS", 250),
			ForwardRetryMaxMs:     getEnvInt("WEBHOOK_FORWARD_RETRY_MAX_DELAY_MS", 5000),
			MaxInFlight:           getEnvInt("WEBHOOK_MAX_IN_FLIGHT", 32),
			ProcessTimeoutMs:      getEnvInt("WEBHOOK_PROCESS_TIMEOUT_MS", 15000),
			AcceptLegacySignature: getEnv("WEBHOOK_ACCEPT_LEGACY_SIGNATURE", "true") != "false",
		},
		Drills: DrillsConfig{
			KubeconfigPath:          getEnv("DRILLS_KUBECONFIG_PATH", getEnv("DRILLS_KUBECONFIG", "")),
			KubeContext:             getEnv("DRILLS_KUBE_CONTEXT", ""),
			KubeAPIServer:           getEnv("DRILLS_KUBE_API_SERVER", ""),
			LoadGeneratorDeployment: getEnv("DRILLS_LOADGEN_DEPLOYMENT", "loadgenerator"),
			LoadGeneratorContainer:  getEnv("DRILLS_LOADGEN_CONTAINER", "main"),
			TargetedLoadRateEnv:     getEnv("DRILLS_TARGETED_LOAD_RATE_ENV", "RATE"),
			TargetedLoadUsersEnv:    getEnv("DRILLS_TARGETED_LOAD_USERS_ENV", "USERS"),
		},
	}

	return cfg, nil
}

func ValidateEnv() error {
	v1 := os.Getenv("GRAPH_ENGINE_BASE_URL")
	v2 := os.Getenv("SERVICE_GRAPH_ENGINE_URL")

	if v1 == "" && v2 == "" {
		return fmt.Errorf("GRAPH_ENGINE_BASE_URL (or SERVICE_GRAPH_ENGINE_URL) is required")
	}
	return nil
}

func getGraphBaseURL() string {
	if v := os.Getenv("GRAPH_ENGINE_BASE_URL"); v != "" {
		return v
	}
	if v := os.Getenv("SERVICE_GRAPH_ENGINE_URL"); v != "" {
		return v
	}
	return "http://service-graph-engine:3000"
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}

func getEnvFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}
