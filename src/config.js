require('dotenv').config();

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
 * @typedef {Object} Config
 * @property {Neo4jConfig} neo4j
 * @property {SimulationConfig} simulation
 * @property {ServerConfig} server
 */

/** @type {Config} */
module.exports = {
  neo4j: {
    uri: process.env.NEO4J_URI || 'neo4j+s://517b3e75.databases.neo4j.io',
    user: process.env.NEO4J_USER || 'neo4j',
    password: process.env.NEO4J_PASSWORD || 'Ex-hfrpIOCfghD-dZ04f2ya3-zbUpBdsZSgjwl6a8Rg'
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
  }
};
