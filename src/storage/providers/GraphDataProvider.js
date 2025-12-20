/**
 * GraphDataProvider Interface Contract (JSDoc only)
 * 
 * All graph data providers must implement these methods.
 */

/**
 * @typedef {Object} NodeData
 * @property {string} serviceId - Service identifier (plain name like "frontend")
 * @property {string} name - Service name
 * @property {string} [namespace] - Service namespace (optional, defaults to "default")
 */

/**
 * @typedef {Object} EdgeData
 * @property {string} source - Source service ID
 * @property {string} target - Target service ID
 * @property {number} rate - Request rate (RPS)
 * @property {number} errorRate - Error rate (RPS)
 * @property {number} p50 - P50 latency (ms)
 * @property {number} p95 - P95 latency (ms)
 * @property {number} p99 - P99 latency (ms)
 */

/**
 * @typedef {Object} DataFreshness
 * @property {string} source - Data source (always 'graph-engine')
 * @property {boolean} stale - Whether data is stale
 * @property {number|null} lastUpdatedSecondsAgo - Seconds since last update
 * @property {number|null} [windowMinutes] - Aggregation window in minutes
 */

/**
 * @typedef {Object} GraphSnapshot
 * @property {Map<string, NodeData>} nodes - Map of serviceId to node data
 * @property {EdgeData[]} edges - Array of all edges
 * @property {Map<string, EdgeData[]>} incomingEdges - Map of target serviceId to incoming edges
 * @property {Map<string, EdgeData[]>} outgoingEdges - Map of source serviceId to outgoing edges
 * @property {string} [targetKey] - Provider-normalized identifier used as the key in nodes/edges maps.
 *   Graph Engine uses plain service names (e.g., "checkoutservice").
 *   Simulations should use this for all map lookups instead of request.serviceId.
 * @property {DataFreshness} [dataFreshness] - Data freshness metadata for simulation responses
 */

/**
 * @typedef {Object} HealthResult
 * @property {boolean} connected - Whether the data source is connected
 * @property {number} [services] - Number of services (optional)
 * @property {boolean} [stale] - Whether data is stale (Graph API only)
 * @property {number} [lastUpdatedSecondsAgo] - Seconds since last update (Graph API only)
 * @property {string} [error] - Error message if not connected
 */

/**
 * GraphDataProvider Contract
 * 
 * Implementations must provide:
 * 
 * @method fetchUpstreamNeighborhood
 * @param {string} targetServiceId - Target service ID
 * @param {number} maxDepth - Maximum traversal depth (1-3)
 * @returns {Promise<GraphSnapshot>}
 * 
 * @method checkHealth
 * @returns {Promise<HealthResult>}
 * 
 * @method close
 * @returns {Promise<void>}
 */

module.exports = {
    // This module only exports JSDoc types for documentation
    // No runtime code - just contract documentation
};
