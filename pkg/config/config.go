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
}

type SimulationConfig struct {
	DefaultLatencyMetric string
	MaxTraversalDepth    int
	ScalingModel         string
	ScalingAlpha         float64
	MinLatencyFactor     float64
	TimeoutMs            int
	MaxPathsReturned     int
}

type ServerConfig struct {
	Port int
}

type GraphAPIConfig struct {
	BaseURL   string
	TimeoutMs int
}

type RateLimitConfig struct {
	WindowMs    int
	MaxRequests int
}

type InfluxConfig struct {
	Host     string
	Token    string
	Database string
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
		},
		Server: ServerConfig{
			Port: getEnvInt("PORT", 5000),
		},
		GraphAPI: GraphAPIConfig{
			BaseURL:   getGraphBaseURL(),
			TimeoutMs: getEnvInt("GRAPH_API_TIMEOUT_MS", 5000),
		},
		RateLimit: RateLimitConfig{
			WindowMs:    getEnvInt("RATE_LIMIT_WINDOW_MS", 60000),
			MaxRequests: getEnvInt("RATE_LIMIT_MAX", 60),
		},
		Influx: InfluxConfig{
			Host:     getEnv("INFLUX_HOST", ""),
			Token:    getEnv("INFLUX_TOKEN", ""),
			Database: getEnv("INFLUX_DATABASE", ""),
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
