/**
 * HTTP client for service-graph-engine API
 * 
 * Uses native http/https modules to avoid external dependencies.
 * Returns { ok: true, data } on success or { ok: false, error, status? } on failure.
 */

const http = require('node:http');
const https = require('node:https');
const config = require('./config');

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
    if (!config.graphApi.enabled) {
        return { ok: false, error: 'Graph API is disabled' };
    }

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
 * Check if graph API is enabled
 * @returns {boolean}
 */
function isEnabled() {
    return config.graphApi.enabled;
}

module.exports = {
    checkGraphHealth,
    getBaseUrl,
    isEnabled,
    // Exported for testing
    _httpGet: httpGet,
    _normalizeBaseUrl: normalizeBaseUrl
};
