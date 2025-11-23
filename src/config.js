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
  
  if (!process.env.NEO4J_URI) {
    errors.push('NEO4J_URI is required (e.g., neo4j+s://xxxx.databases.neo4j.io)');
  }
  
  if (!process.env.NEO4J_PASSWORD) {
    errors.push('NEO4J_PASSWORD is required');
  }
  
  if (errors.length > 0) {
    console.error('\n❌ Missing required environment variables:\n');
    errors.forEach(err => console.error(`   - ${err}`));
    console.error('\n   Copy .env.example to .env and fill in your Neo4j credentials.\n');
    process.exit(1);
  }
}

/**
 * @typedef {Object} Neo4jConfig
 * @property {string} uri - Neo4j connection URI
 * @property {string} user - Neo4j username
 * @property {string} password - Neo4j password (never logged)
 */

/**
 * @typedef {Object} SimulationConfig
 * @property {string} defaultLatencyMetric - Default latency metric (p50, p95, p99)
 * @property {number} maxTraversalDepth - Maximum k-hop depth (validated to 1-3)
 * @property {string} scalingModel - Scaling model type (bounded_sqrt, linear)
 * @property {number} scalingAlpha - Fixed overhead fraction (0.0-1.0)
 * @property {number} minLatencyFactor - Minimum latency improvement factor
 * @property {number} timeoutMs - Neo4j query and HTTP request timeout
 * @property {number} maxPathsReturned - Maximum paths to return in results
 */

/**
 * @typedef {Object} ServerConfig
 * @property {number} port - HTTP server port
 */

/**
 * @typedef {Object} GraphApiConfig
 * @property {string} baseUrl - Base URL of service-graph-engine
 * @property {boolean} enabled - Whether to use the Graph API
 * @property {number} timeoutMs - Request timeout in milliseconds
 * @property {boolean} required - Whether Graph API failure should degrade overall status
 */

/**
 * @typedef {Object} Config
 * @property {Neo4jConfig} neo4j
 * @property {SimulationConfig} simulation
 * @property {ServerConfig} server
 * @property {GraphApiConfig} graphApi
 */

/** @type {Config} */
const config = {
  neo4j: {
    uri: process.env.NEO4J_URI,
    user: process.env.NEO4J_USER || 'neo4j',
    password: process.env.NEO4J_PASSWORD
  },
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
    port: parseInt(process.env.PORT) || 7000
  },
  graphApi: {
    baseUrl: process.env.SERVICE_GRAPH_ENGINE_URL || '',
    enabled: process.env.USE_GRAPH_ENGINE_API === 'true',
    timeoutMs: parseInt(process.env.GRAPH_API_TIMEOUT_MS) || 5000,
    required: process.env.REQUIRE_GRAPH_API === 'true'
  }
};

module.exports = config;
module.exports.validateEnv = validateEnv;
