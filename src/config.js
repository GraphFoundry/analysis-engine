require('dotenv').config();

/**
 * Validate required environment variables at startup.
 * Fails fast with clear error messages before any connections are attempted.
 * 
 * Call this explicitly from index.js before starting the server.
 * Not auto-run on import to avoid breaking tests and utility scripts.
 */
function validateEnv() {
  const errors = [];
  
  // Graph Engine is always required
  if (!process.env.GRAPH_ENGINE_BASE_URL && !process.env.SERVICE_GRAPH_ENGINE_URL) {
    errors.push('GRAPH_ENGINE_BASE_URL (or SERVICE_GRAPH_ENGINE_URL) is required');
  }
  
  if (errors.length > 0) {
    console.error('\n❌ Missing required environment variables:\n');
    errors.forEach(err => console.error(`   - ${err}`));
    console.error('\n   Set GRAPH_ENGINE_BASE_URL or SERVICE_GRAPH_ENGINE_URL to point to service-graph-engine.\n');
    console.error('   Example: GRAPH_ENGINE_BASE_URL=http://service-graph-engine:3000\n');
    process.exit(1);
  }
}

/**
 * @typedef {Object} SimulationConfig
 * @property {string} defaultLatencyMetric - Default latency metric (p50, p95, p99)
 * @property {number} maxTraversalDepth - Maximum k-hop depth (validated to 1-3)
 * @property {string} scalingModel - Scaling model type (bounded_sqrt, linear)
 * @property {number} scalingAlpha - Fixed overhead fraction (0.0-1.0)
 * @property {number} minLatencyFactor - Minimum latency improvement factor
 * @property {number} timeoutMs - HTTP request timeout
 * @property {number} maxPathsReturned - Maximum paths to return in results
 */

/**
 * @typedef {Object} ServerConfig
 * @property {number} port - HTTP server port
 */

/**
 * @typedef {Object} GraphApiConfig
 * @property {string} baseUrl - Base URL of service-graph-engine
 * @property {number} timeoutMs - Request timeout in milliseconds
 * @property {boolean} required - Whether Graph API failure should degrade overall status
 */

/**
 * @typedef {Object} Config
 * @property {SimulationConfig} simulation
 * @property {ServerConfig} server
 * @property {GraphApiConfig} graphApi
 */

/** @type {Config} */
const config = {
  simulation: {
    defaultLatencyMetric: process.env.DEFAULT_LATENCY_METRIC || 'p95',
    maxTraversalDepth: parseInt(process.env.MAX_TRAVERSAL_DEPTH) || 2,
    scalingModel: process.env.SCALING_MODEL || 'bounded_sqrt',
    scalingAlpha: parseFloat(process.env.SCALING_ALPHA) || 0.5,
    minLatencyFactor: parseFloat(process.env.MIN_LATENCY_FACTOR) || 0.6,
    timeoutMs: parseInt(process.env.TIMEOUT_MS) || 8000,
    maxPathsReturned: parseInt(process.env.MAX_PATHS_RETURNED) || 10
  },
  server: {
    port: parseInt(process.env.PORT) || 5000
  },
  graphApi: {
    baseUrl: process.env.GRAPH_ENGINE_BASE_URL || process.env.SERVICE_GRAPH_ENGINE_URL || 'http://service-graph-engine:3000',
    timeoutMs: parseInt(process.env.GRAPH_API_TIMEOUT_MS) || 5000
  },
  rateLimit: {
    windowMs: parseInt(process.env.RATE_LIMIT_WINDOW_MS) || 60000,
    maxRequests: parseInt(process.env.RATE_LIMIT_MAX) || 60
  },
  influx: {
    host: process.env.INFLUX_HOST || '',
    token: process.env.INFLUX_TOKEN || '',
    database: process.env.INFLUX_DATABASE || ''
  },
  sqlite: {
    dbPath: process.env.SQLITE_DB_PATH || './data/decisions.db'
  },
  telemetryWorker: {
    enabled: process.env.TELEMETRY_WORKER_ENABLED !== 'false',
    pollIntervalMs: parseInt(process.env.TELEMETRY_POLL_INTERVAL_MS) || 60000
  }
};

module.exports = config;
module.exports.validateEnv = validateEnv;
