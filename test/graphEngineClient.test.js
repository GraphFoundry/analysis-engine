/**
 * Tests for GraphEngineClient and /health endpoint with graphApi field
 * 
 * Uses Node.js built-in test runner and a minimal mock HTTP server.
 */

const assert = require('node:assert');
const { test, describe, beforeEach, afterEach } = require('node:test');
const http = require('node:http');

// We need to be able to control config for testing
// Store original env and restore after tests
const originalEnv = { ...process.env };

/**
 * Create a mock HTTP server that responds with given data
 */
function createMockServer(handler) {
    return new Promise((resolve) => {
        const server = http.createServer(handler);
        server.listen(0, '127.0.0.1', () => {
            const { port } = server.address();
            resolve({ server, port, url: `http://127.0.0.1:${port}` });
        });
    });
}

/**
 * Close mock server
 */
function closeMockServer(server) {
    return new Promise((resolve) => {
        server.close(resolve);
    });
}

describe('GraphEngineClient._httpGet', () => {
    let mockServer;

    afterEach(async () => {
        if (mockServer) {
            await closeMockServer(mockServer);
            mockServer = null;
        }
    });

    test('returns ok:true with parsed JSON on 200 response', async () => {
        const responseData = { status: 'OK', stale: false, lastUpdatedSecondsAgo: 30 };
        
        const mock = await createMockServer((req, res) => {
            res.writeHead(200, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify(responseData));
        });
        mockServer = mock.server;

        // Import after setting up mock
        const { _httpGet } = require('../src/graphEngineClient');
        
        const result = await _httpGet(`${mock.url}/graph/health`, 5000);
        
        assert.strictEqual(result.ok, true);
        assert.deepStrictEqual(result.data, responseData);
    });

    test('returns ok:false with status on non-200 response', async () => {
        const mock = await createMockServer((req, res) => {
            res.writeHead(500, { 'Content-Type': 'application/json' });
            res.end(JSON.stringify({ error: 'Internal Server Error' }));
        });
        mockServer = mock.server;

        const { _httpGet } = require('../src/graphEngineClient');
        
        const result = await _httpGet(`${mock.url}/graph/health`, 5000);
        
        assert.strictEqual(result.ok, false);
        assert.strictEqual(result.status, 500);
        assert.strictEqual(result.error, 'HTTP 500');
    });

    test('returns ok:false on timeout', async () => {
        const mock = await createMockServer((req, res) => {
            // Never respond - let it timeout
            // Note: we need to keep the connection open
        });
        mockServer = mock.server;

        const { _httpGet } = require('../src/graphEngineClient');
        
        // Use very short timeout
        const result = await _httpGet(`${mock.url}/graph/health`, 50);
        
        assert.strictEqual(result.ok, false);
        assert.strictEqual(result.error, 'Request timeout');
    });

    test('returns ok:false on connection refused', async () => {
        const { _httpGet } = require('../src/graphEngineClient');
        
        // Use a port that's not listening
        const result = await _httpGet('http://127.0.0.1:59999/graph/health', 1000);
        
        assert.strictEqual(result.ok, false);
        assert.ok(result.error.includes('ECONNREFUSED') || result.error.includes('connect'), 
            `Expected connection error, got: ${result.error}`);
    });

    test('returns ok:false on invalid JSON response', async () => {
        const mock = await createMockServer((req, res) => {
            res.writeHead(200, { 'Content-Type': 'application/json' });
            res.end('not valid json');
        });
        mockServer = mock.server;

        const { _httpGet } = require('../src/graphEngineClient');
        
        const result = await _httpGet(`${mock.url}/graph/health`, 5000);
        
        assert.strictEqual(result.ok, false);
        assert.ok(result.error.startsWith('Invalid JSON response:'), 
            `Expected 'Invalid JSON response:...' but got: ${result.error}`);
    });

    test('returns ok:false on HTML error page (common proxy error)', async () => {
        const mock = await createMockServer((req, res) => {
            res.writeHead(200, { 'Content-Type': 'text/html' });
            res.end('<html><body>502 Bad Gateway</body></html>');
        });
        mockServer = mock.server;

        const { _httpGet } = require('../src/graphEngineClient');
        
        const result = await _httpGet(`${mock.url}/graph/health`, 5000);
        
        assert.strictEqual(result.ok, false);
        assert.ok(result.error.startsWith('Invalid JSON response:'), 
            `Expected 'Invalid JSON response:...' but got: ${result.error}`);
    });
});

describe('GraphEngineClient.checkGraphHealth', () => {
    let mockServer;

    beforeEach(() => {
        // Clear require cache to reset config
        delete require.cache[require.resolve('../src/config')];
        delete require.cache[require.resolve('../src/graphEngineClient')];
    });

    afterEach(async () => {
        // Restore original env
        process.env = { ...originalEnv };
        
        if (mockServer) {
            await closeMockServer(mockServer);
            mockServer = null;
        }
        
        // Clear require cache
        delete require.cache[require.resolve('../src/config')];
        delete require.cache[require.resolve('../src/graphEngineClient')];
    });

    test('returns error when graph API is disabled', async () => {
        process.env.USE_GRAPH_ENGINE_API = 'false';
        
        const { checkGraphHealth } = require('../src/graphEngineClient');
        
        const result = await checkGraphHealth();
        
        assert.strictEqual(result.ok, false);
        assert.strictEqual(result.error, 'Graph API is disabled');
    });

    test('returns health data when enabled and API responds', async () => {
        const responseData = { status: 'OK', stale: false, lastUpdatedSecondsAgo: 45, windowMinutes: 5 };
        
        const mock = await createMockServer((req, res) => {
            if (req.url === '/graph/health') {
                res.writeHead(200, { 'Content-Type': 'application/json' });
                res.end(JSON.stringify(responseData));
            } else {
                res.writeHead(404);
                res.end();
            }
        });
        mockServer = mock.server;

        process.env.USE_GRAPH_ENGINE_API = 'true';
        process.env.SERVICE_GRAPH_ENGINE_URL = mock.url;
        
        const { checkGraphHealth } = require('../src/graphEngineClient');
        
        const result = await checkGraphHealth();
        
        assert.strictEqual(result.ok, true);
        assert.deepStrictEqual(result.data, responseData);
    });
});

describe('/health endpoint graphApi field', () => {
    // These tests verify the expected response shape
    // Full integration would require starting the actual server

    beforeEach(() => {
        // Clear require cache to reset config
        delete require.cache[require.resolve('../src/config')];
    });

    afterEach(() => {
        // Restore original env
        process.env = { ...originalEnv };
        // Clear require cache
        delete require.cache[require.resolve('../src/config')];
    });
    
    test('config has graphApi section with expected defaults', () => {
        delete require.cache[require.resolve('../src/config')];
        process.env.USE_GRAPH_ENGINE_API = undefined;
        process.env.SERVICE_GRAPH_ENGINE_URL = undefined;
        
        const config = require('../src/config');
        
        assert.strictEqual(typeof config.graphApi, 'object');
        assert.strictEqual(config.graphApi.enabled, false);
        assert.strictEqual(config.graphApi.timeoutMs, 5000);
        assert.strictEqual(config.graphApi.required, false);
    });

    test('config.graphApi.enabled is true when USE_GRAPH_ENGINE_API=true', () => {
        delete require.cache[require.resolve('../src/config')];
        process.env.USE_GRAPH_ENGINE_API = 'true';
        process.env.SERVICE_GRAPH_ENGINE_URL = 'http://localhost:3000';
        
        const config = require('../src/config');
        
        assert.strictEqual(config.graphApi.enabled, true);
        assert.strictEqual(config.graphApi.baseUrl, 'http://localhost:3000');
    });

    test('config.graphApi.required is true when REQUIRE_GRAPH_API=true', () => {
        delete require.cache[require.resolve('../src/config')];
        process.env.REQUIRE_GRAPH_API = 'true';
        
        const config = require('../src/config');
        
        assert.strictEqual(config.graphApi.required, true);
    });
});

describe('URL normalization', () => {
    test('normalizeBaseUrl removes trailing slash', () => {
        const { _normalizeBaseUrl } = require('../src/graphEngineClient');
        
        assert.strictEqual(_normalizeBaseUrl('http://localhost:3000/'), 'http://localhost:3000');
        assert.strictEqual(_normalizeBaseUrl('http://localhost:3000'), 'http://localhost:3000');
        assert.strictEqual(_normalizeBaseUrl('https://api.example.com/'), 'https://api.example.com');
    });
});
