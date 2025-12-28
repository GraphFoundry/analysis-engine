/**
 * HTTP client for service-graph-engine API
 * 
 * Uses native http/https modules to avoid external dependencies.
 * Returns { ok: true, data } on success or { ok: false, error, status? } on failure.
 * 
 * CONTAINER-LEVEL METRICS:
 * As of the latest update, the /services endpoint now includes pod-level container metrics:
 * - ramUsedMB: Pod RAM usage in MB (aggregated from all containers)
 * - cpuUsagePercent: Pod CPU usage as percentage of node's total cores
 * These metrics are available in the placement.nodes[].pods[] array.
 */

const http = require('node:http');
const https = require('node:https');
const config = require('../config/config');

/**
 * @typedef {Object} GraphHealthResponse
 * @property {string} status - Health status ("OK")
 * @property {number} lastUpdatedSecondsAgo - Seconds since last graph update
 * @property {number} windowMinutes - Aggregation window in minutes
 * @property {boolean} stale - Whether the graph data is stale
 */

/**
 * @typedef {Object} PodInfo
 * @property {string} name - Pod name
 * @property {number} ramUsedMB - Pod RAM usage in MB
 * @property {number} cpuUsagePercent - Pod CPU usage as percentage of node's total cores
 */

/**
 * @typedef {Object} NodeResources
 * @property {Object} cpu - CPU metrics
 * @property {number} cpu.usagePercent - Node CPU usage percentage
 * @property {number} cpu.cores - Total CPU cores on node
 * @property {Object} ram - RAM metrics
 * @property {number} ram.usedMB - RAM used on node in MB
 * @property {number} ram.totalMB - Total RAM on node in MB
 */

/**
 * @typedef {Object} NodePlacement
 * @property {string} node - Node name
 * @property {NodeResources} resources - Node resource usage
 * @property {Array<PodInfo>} pods - Pods running on this node
 */

/**
 * @typedef {Object} ServicePlacement
 * @property {Array<NodePlacement>} nodes - Nodes hosting this service's pods
 */

/**
 * @typedef {Object} ServiceInfo
 * @property {string} name - Service name
 * @property {string} namespace - Kubernetes namespace
 * @property {number} podCount - Number of pods running
 * @property {number} availability - Availability score (0-1)
 * @property {ServicePlacement} placement - Pod placement with container-level metrics
 */

/**
 * @typedef {Object} ServicesResponse
 * @property {Array<ServiceInfo>} services - List of services
 */

/**
 * @typedef {Object} EdgeMetrics
 * @property {number} rate - Request rate (requests per second)
 * @property {number} p50 - 50th percentile latency (ms)
 * @property {number} p95 - 95th percentile latency (ms)
 * @property {number} p99 - 99th percentile latency (ms)
 * @property {number} errorRate - Error rate (0-1)
 */

/**
 * @typedef {Object} Edge
 * @property {string} from - Source service name
 * @property {string} to - Target service name
 * @property {number} rate - Request rate
 * @property {number} errorRate - Error rate
 * @property {number} p50 - 50th percentile latency
 * @property {number} p95 - 95th percentile latency
 * @property {number} p99 - 99th percentile latency
 */

/**
 * @typedef {Object} Node
 * @property {string} name - Service name
 * @property {string} namespace - Kubernetes namespace
 * @property {number} podCount - Number of pods
 * @property {number} availability - Availability score (0-1)
 */

/**
 * @typedef {Object} NeighborhoodResponse
 * @property {string} center - Center service name
 * @property {number} k - Number of hops
 * @property {Array<Node>} nodes - List of nodes in neighborhood
 * @property {Array<Edge>} edges - List of edges in neighborhood
 */

/**
 * @typedef {Object} PeerMetrics
 * @property {number} rate - Request rate
 * @property {number} p50 - 50th percentile latency
 * @property {number} p95 - 95th percentile latency
 * @property {number} p99 - 99th percentile latency
 * @property {number} errorRate - Error rate
 */

/**
 * @typedef {Object} Peer
 * @property {string} service - Peer service name
 * @property {number} podCount - Number of pods
 * @property {number} availability - Availability score
 * @property {PeerMetrics} metrics - Edge metrics
 */

/**
 * @typedef {Object} PeersResponse
 * @property {string} service - Service name
 * @property {string} direction - Direction ('in' or 'out')
 * @property {number} windowMinutes - Aggregation window in minutes
 * @property {Array<Peer>} peers - List of peer services
 */

/**
 * @typedef {Object} CentralityScore
 * @property {string} service - Service name
 * @property {number} value - Centrality score value
 */

/**
 * @typedef {Object} CentralityTopResponse
 * @property {string} metric - The centrality metric used (pagerank/betweenness)
 * @property {Array<CentralityScore>} top - Top services by centrality
 */

/**
 * @typedef {Object} ServiceScore
 * @property {string} service - Service name
 * @property {number} pagerank - PageRank centrality score
 * @property {number} betweenness - Betweenness centrality score
 */

/**
 * @typedef {Object} CentralityScoresResponse
 * @property {number} windowMinutes - Aggregation window in minutes
 * @property {Array<ServiceScore>} scores - List of service centrality scores
 */

/**
 * @typedef {Object} ServiceMetrics
 * @property {string} name - Service name
 * @property {string} namespace - Kubernetes namespace
 * @property {number} rps - Requests per second
 * @property {number} errorRate - Error rate
 * @property {number} p95 - 95th percentile latency
 */

/**
 * @typedef {Object} EdgeSnapshot
 * @property {string} from - Source service
 * @property {string} to - Target service
 * @property {string} namespace - Kubernetes namespace
 * @property {number} rps - Requests per second
 * @property {number} errorRate - Error rate
 * @property {number} p95 - 95th percentile latency
 */

/**
 * @typedef {Object} MetricsSnapshotResponse
 * @property {string} timestamp - ISO timestamp
 * @property {string} window - Time window (e.g., '1m')
 * @property {Array<ServiceMetrics>} services - Service metrics
 * @property {Array<EdgeSnapshot>} edges - Edge metrics
 */

/**
 * @typedef {Object} ClientSuccess
 * @property {true} ok
 * @property {*} data - Parsed JSON response
 */

/**
 * @typedef {Object} ClientError
 * @property {false} ok
 * @property {string} error - Error message
 * @property {number} [status] - HTTP status code (if applicable)
 */

/**
 * Make an HTTP GET request with timeout
 * @param {string} url - Full URL to request
 * @param {number} timeoutMs - Request timeout in milliseconds
 * @returns {Promise<ClientSuccess|ClientError>}
 */
function httpGet(url, timeoutMs) {
    return new Promise((resolve) => {
        const parsedUrl = new URL(url);
        const transport = parsedUrl.protocol === 'https:' ? https : http;

        const req = transport.get(url, { timeout: timeoutMs }, (res) => {
            let data = '';

            res.on('data', (chunk) => {
                data += chunk;
            });

            res.on('end', () => {
                if (res.statusCode >= 200 && res.statusCode < 300) {
                    let parsed;
                    try {
                        parsed = JSON.parse(data);
                    } catch (parseError) {
                        // JSON parse failed - include parse error message
                        resolve({ 
                            ok: false, 
                            error: `Invalid JSON response: ${parseError.message}`, 
                            status: res.statusCode 
                        });
                        return;
                    }
                    resolve({ ok: true, data: parsed });
                } else {
                    resolve({ ok: false, error: `HTTP ${res.statusCode}`, status: res.statusCode });
                }
            });
        });

        req.on('error', (err) => {
            resolve({ ok: false, error: err.message });
        });

        req.on('timeout', () => {
            req.destroy();
            resolve({ ok: false, error: 'Request timeout' });
        });
    });
}

/**
 * Normalize base URL by removing trailing slash
 * @param {string} baseUrl
 * @returns {string}
 */
function normalizeBaseUrl(baseUrl) {
    return baseUrl.endsWith('/') ? baseUrl.slice(0, -1) : baseUrl;
}

/**
 * Check health of the service-graph-engine
 * @returns {Promise<ClientSuccess|ClientError>}
 */
async function checkGraphHealth() {
    const baseUrl = normalizeBaseUrl(config.graphApi.baseUrl);
    const url = `${baseUrl}/graph/health`;
    return httpGet(url, config.graphApi.timeoutMs);
}

/**
 * Get the configured base URL (for testing/debugging)
 * @returns {string|undefined}
 */
function getBaseUrl() {
    return config.graphApi.baseUrl;
}

/**
 * Check if graph API is enabled (always true - Graph Engine is the only data source)
 * @returns {boolean}
 */
function isEnabled() {
    return true;
}

/**
 * Get k-hop neighborhood for a service
/**
 * Get k-hop neighborhood for a service
 * @param {string} serviceName - Service name (e.g., "frontend")
 * @param {number} k - Number of hops
 * @returns {Promise<ClientSuccess|ClientError>}
 */
async function getNeighborhood(serviceName, k) {
    const baseUrl = normalizeBaseUrl(config.graphApi.baseUrl);
    const url = `${baseUrl}/services/${encodeURIComponent(serviceName)}/neighborhood?k=${k}`;
    return httpGet(url, config.graphApi.timeoutMs);
}

/**
 * Get peers (callers or callees) for a service
 * @param {string} serviceName - Service name (e.g., "frontend")
 * @param {string} direction - 'in' for callers, 'out' for callees
 * @returns {Promise<ClientSuccess|ClientError>}
 */
async function getPeers(serviceName, direction) {
    const baseUrl = normalizeBaseUrl(config.graphApi.baseUrl);
    const url = `${baseUrl}/services/${encodeURIComponent(serviceName)}/peers?direction=${direction}`;
    return httpGet(url, config.graphApi.timeoutMs);
}

/**
 * Get top services by centrality metric
 * @param {string} [metric='pagerank'] - Centrality metric (pagerank, betweenness)
 * @param {number} [limit=5] - Number of top services to return
 * @returns {Promise<ClientSuccess|ClientError>}
 */
async function getCentralityTop(metric = 'pagerank', limit = 5) {
    // Validate metric to prevent injection
    const validMetrics = ['pagerank', 'betweenness'];
    if (!validMetrics.includes(metric)) {
        return { ok: false, error: `Invalid metric: ${metric}. Allowed: ${validMetrics.join(', ')}` };
    }

    const baseUrl = normalizeBaseUrl(config.graphApi.baseUrl);
    const url = `${baseUrl}/centrality/top?metric=${metric}&limit=${limit}`;
    return httpGet(url, config.graphApi.timeoutMs);
}

/**
 * List all services from the graph (basic info only)
 * Returns {services: [{name, namespace, podCount, availability}, ...]}
 * @returns {Promise<ClientSuccess|ClientError>}
 */
async function getServices() {
    const baseUrl = normalizeBaseUrl(config.graphApi.baseUrl);
    const url = `${baseUrl}/services`;
    return httpGet(url, config.graphApi.timeoutMs);
}

/**
 * List all services with pod-level placement and resource metrics
 * Returns {services: [{name, namespace, podCount, availability, placement: {nodes: [...]}}, ...]}
 * Placement includes node-level CPU/RAM metrics and pod-level container metrics (ramUsedMB, cpuUsagePercent)
 * Note: This calls the same endpoint as getServices() - the Graph Engine always returns placement data
 * @returns {Promise<ClientSuccess|ClientError>}
 */
async function getServicesWithPlacement() {
    // Graph Engine's /services endpoint always includes placement data when available
    // This is a semantic wrapper for clarity in the codebase
    return getServices();
}

/**
 * Get metrics snapshot (all services and edges in one call)
 * Returns {timestamp, window, services: [...], edges: [...]}
 * @returns {Promise<ClientSuccess|ClientError>}
 */
async function getMetricsSnapshot() {
    const baseUrl = normalizeBaseUrl(config.graphApi.baseUrl);
    const url = `${baseUrl}/metrics/snapshot`;
    return httpGet(url, config.graphApi.timeoutMs);
}

/**
 * Get centrality scores for all services (PageRank and Betweenness)
 * Returns {windowMinutes, scores: [{service, pagerank, betweenness}, ...]}
 * @returns {Promise<ClientSuccess|ClientError>}
 */
async function getCentralityScores() {
    const baseUrl = normalizeBaseUrl(config.graphApi.baseUrl);
    const url = `${baseUrl}/centrality/scores`;
    return httpGet(url, config.graphApi.timeoutMs);
}

module.exports = {
    checkGraphHealth,
    getNeighborhood,
    getPeers,
    getCentralityTop,
    getCentralityScores,
    getServices,
    getServicesWithPlacement,
    getMetricsSnapshot,
    getBaseUrl,
    isEnabled,
    // Exported for testing
    _httpGet: httpGet,
    _normalizeBaseUrl: normalizeBaseUrl
};
