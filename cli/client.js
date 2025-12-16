/**
 * HTTP Client for CLI
 * 
 * Lightweight HTTP/HTTPS client wrapper for making API requests.
 * Uses Node.js built-in http/https modules (no external dependencies).
 */

const http = require('http');
const https = require('https');
const { URL } = require('url');
const { EXIT_CODES } = require('./utils/exitCodes');

// Default timeout: 30 seconds
const DEFAULT_TIMEOUT_MS = 30000;

/**
 * Get base URL from environment or use default
 * @returns {string} Base URL
 */
function getBaseUrl() {
    return process.env.PREDICTIVE_ENGINE_URL || 'http://localhost:5000';
}

/**
 * Make an HTTP request
 * @param {object} options - Request options
 * @param {string} options.method - HTTP method (GET, POST, etc.)
 * @param {string} options.path - URL path (e.g., '/health')
 * @param {object} [options.body] - Request body (will be JSON-stringified)
 * @param {object} [options.query] - Query parameters
 * @param {number} [options.timeoutMs] - Request timeout in milliseconds
 * @returns {Promise<{ statusCode: number, data: any }>} Response
 */
async function request(options) {
    const baseUrl = getBaseUrl();
    const { method, path, body, query, timeoutMs = DEFAULT_TIMEOUT_MS } = options;
    
    // Build URL with query params
    const url = new URL(path, baseUrl);
    if (query) {
        for (const [key, value] of Object.entries(query)) {
            if (value !== undefined && value !== null) {
                url.searchParams.set(key, String(value));
            }
        }
    }
    
    // Select http or https module
    const client = url.protocol === 'https:' ? https : http;
    
    const requestOptions = {
        method: method.toUpperCase(),
        hostname: url.hostname,
        port: url.port || (url.protocol === 'https:' ? 443 : 80),
        path: url.pathname + url.search,
        headers: {
            'Accept': 'application/json',
            'User-Agent': 'predict-cli/1.0.0'
        },
        timeout: timeoutMs
    };
    
    // Add body for POST/PUT/PATCH
    let bodyStr = null;
    if (body && ['POST', 'PUT', 'PATCH'].includes(requestOptions.method)) {
        bodyStr = JSON.stringify(body);
        requestOptions.headers['Content-Type'] = 'application/json';
        requestOptions.headers['Content-Length'] = Buffer.byteLength(bodyStr);
    }
    
    return new Promise((resolve, reject) => {
        const req = client.request(requestOptions, (res) => {
            let data = '';
            
            res.on('data', (chunk) => {
                data += chunk;
            });
            
            res.on('end', () => {
                let parsed;
                try {
                    parsed = data ? JSON.parse(data) : null;
                } catch {
                    // If not JSON, return raw string
                    parsed = data;
                }
                
                resolve({
                    statusCode: res.statusCode,
                    data: parsed
                });
            });
        });
        
        req.on('error', (err) => {
            const error = new Error(`Network error: ${err.message}`);
            error.exitCode = EXIT_CODES.NETWORK_ERROR;
            error.cause = err;
            reject(error);
        });
        
        req.on('timeout', () => {
            req.destroy();
            const error = new Error(`Request timed out after ${timeoutMs}ms`);
            error.exitCode = EXIT_CODES.NETWORK_ERROR;
            reject(error);
        });
        
        if (bodyStr) {
            req.write(bodyStr);
        }
        
        req.end();
    });
}

/**
 * Handle API response and throw appropriate errors
 * @param {{ statusCode: number, data: any }} response - API response
 * @returns {any} Response data if successful
 * @throws {Error} If response indicates an error
 */
function handleResponse(response) {
    const { statusCode, data } = response;
    
    if (statusCode >= 200 && statusCode < 300) {
        return data;
    }
    
    // Extract error message
    const message = data?.error || data?.message || `HTTP ${statusCode}`;
    const error = new Error(message);
    
    if (statusCode >= 400 && statusCode < 500) {
        // Client errors (4xx)
        error.exitCode = EXIT_CODES.SERVER_ERROR;
    } else if (statusCode >= 500) {
        // Server errors (5xx)
        error.exitCode = EXIT_CODES.SERVER_ERROR;
    } else {
        error.exitCode = EXIT_CODES.UNEXPECTED;
    }
    
    error.statusCode = statusCode;
    throw error;
}

/**
 * GET request helper
 * @param {string} path - URL path
 * @param {object} [query] - Query parameters
 * @returns {Promise<any>} Response data
 */
async function get(path, query) {
    const response = await request({ method: 'GET', path, query });
    return handleResponse(response);
}

/**
 * POST request helper
 * @param {string} path - URL path
 * @param {object} body - Request body
 * @returns {Promise<any>} Response data
 */
async function post(path, body) {
    const response = await request({ method: 'POST', path, body });
    return handleResponse(response);
}

module.exports = {
    getBaseUrl,
    request,
    handleResponse,
    get,
    post
};
