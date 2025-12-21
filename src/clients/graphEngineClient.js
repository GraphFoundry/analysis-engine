/**
 * HTTP client for service-graph-engine API
 * 
 * Uses native http/https modules to avoid external dependencies.
 * Returns { ok: true, data } on success or { ok: false, error, status? } on failure.
 */

const http = require('node:http');
const https = require('node:https');
const config = require('../config/config');

/**
 * @typedef {Object} GraphHealthResponse
 * @property {string} status - Health status ("OK")
 * @property {number|null} lastUpdatedSecondsAgo - Seconds since last graph update
 * @property {number} windowMinutes - Aggregation window in minutes
 * @property {boolean} stale - Whether the graph data is stale
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
 * @typedef {Object} CentralityTopResult
 * @property {string} metric - The centrality metric used
 * @property {Array<{service: string, value: number}>} top - Top services by centrality
 */

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
 * List all services from the graph
 * @returns {Promise<ClientSuccess|ClientError>}
 */
async function getServices() {
    const baseUrl = normalizeBaseUrl(config.graphApi.baseUrl);
    const url = `${baseUrl}/services`;
    return httpGet(url, config.graphApi.timeoutMs);
}

/**
 * Get metrics snapshot (all services and edges in one call)
 * Returns {services: [...], edges: [...], timestamp, window}
 * @returns {Promise<ClientSuccess|ClientError>}
 */
async function getMetricsSnapshot() {
    const baseUrl = normalizeBaseUrl(config.graphApi.baseUrl);
    const url = `${baseUrl}/metrics/snapshot`;
    return httpGet(url, config.graphApi.timeoutMs);
}

module.exports = {
    checkGraphHealth,
    getNeighborhood,
    getPeers,
    getCentralityTop,
    getServices,
    getMetricsSnapshot,
    getBaseUrl,
    isEnabled,
    // Exported for testing
    _httpGet: httpGet,
    _normalizeBaseUrl: normalizeBaseUrl
};
