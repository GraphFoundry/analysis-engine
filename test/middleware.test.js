const assert = require('node:assert');
const { test, describe, mock, beforeEach, afterEach } = require('node:test');
const { correlationMiddleware, generateCorrelationId } = require('../src/middleware/correlation');
const { rateLimitMiddleware, getClientKey, _test } = require('../src/middleware/rateLimit');

describe('Correlation ID Middleware', () => {
    test('generateCorrelationId returns valid UUID format', () => {
        const id = generateCorrelationId();
        // UUID v4 format: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
        const uuidRegex = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
        assert.ok(uuidRegex.test(id), `Expected UUID format, got: ${id}`);
    });

    test('generateCorrelationId returns unique values', () => {
        const ids = new Set();
        for (let i = 0; i < 100; i++) {
            ids.add(generateCorrelationId());
        }
        assert.strictEqual(ids.size, 100, 'Expected 100 unique IDs');
    });

    test('middleware sets X-Correlation-Id header', () => {
        const middleware = correlationMiddleware();
        
        const req = {
            headers: {},
            method: 'GET',
            path: '/health',
            query: {}
        };
        
        let headerSet = null;
        const res = {
            setHeader: (name, value) => {
                if (name === 'X-Correlation-Id') headerSet = value;
            },
            on: () => {}
        };
        
        const next = mock.fn();
        
        middleware(req, res, next);
        
        assert.ok(headerSet, 'X-Correlation-Id header should be set');
        assert.strictEqual(req.correlationId, headerSet, 'req.correlationId should match header');
        assert.strictEqual(next.mock.calls.length, 1, 'next() should be called');
    });

    test('middleware uses existing correlation ID from request header', () => {
        const middleware = correlationMiddleware();
        const existingId = 'existing-correlation-id-123';
        
        const req = {
            headers: { 'x-correlation-id': existingId },
            method: 'POST',
            path: '/simulate/failure',
            query: {}
        };
        
        let headerSet = null;
        const res = {
            setHeader: (name, value) => {
                if (name === 'X-Correlation-Id') headerSet = value;
            },
            on: () => {}
        };
        
        const next = mock.fn();
        
        middleware(req, res, next);
        
        assert.strictEqual(headerSet, existingId, 'Should use existing correlation ID');
        assert.strictEqual(req.correlationId, existingId);
    });

    test('middleware attaches correlationId to request object', () => {
        const middleware = correlationMiddleware();
        
        const req = {
            headers: {},
            method: 'GET',
            path: '/test',
            query: {}
        };
        
        const res = {
            setHeader: () => {},
            on: () => {}
        };
        
        middleware(req, res, () => {});
        
        assert.ok(req.correlationId, 'correlationId should be attached to req');
        assert.strictEqual(typeof req.correlationId, 'string');
    });
});

describe('Rate Limit Middleware', () => {
    beforeEach(() => {
        _test.clearStore();
    });

    afterEach(() => {
        _test.stopCleanup();
    });

    test('getClientKey extracts remoteAddress', () => {
        const req = { socket: { remoteAddress: '192.168.1.1' } };
        assert.strictEqual(getClientKey(req), '192.168.1.1');
    });

    test('getClientKey falls back to req.ip', () => {
        const req = { socket: {}, ip: '10.0.0.1' };
        assert.strictEqual(getClientKey(req), '10.0.0.1');
    });

    test('middleware sets rate limit headers', () => {
        const middleware = rateLimitMiddleware({ windowMs: 60000, maxRequests: 10 });
        
        const req = { 
            socket: { remoteAddress: '127.0.0.1' },
            path: '/test'
        };
        
        const headers = {};
        const res = {
            setHeader: (name, value) => { headers[name] = value; },
            status: () => res,
            json: () => {}
        };
        
        const next = mock.fn();
        middleware(req, res, next);
        
        assert.strictEqual(headers['X-RateLimit-Limit'], 10);
        assert.strictEqual(headers['X-RateLimit-Remaining'], 9);
        assert.ok(headers['X-RateLimit-Reset'] > 0);
        assert.strictEqual(next.mock.calls.length, 1);
    });

    test('middleware returns 429 when limit exceeded', () => {
        const middleware = rateLimitMiddleware({ windowMs: 60000, maxRequests: 3 });
        
        const req = { 
            socket: { remoteAddress: '127.0.0.2' },
            path: '/test',
            correlationId: 'test-123'
        };
        
        const headers = {};
        let statusCode = null;
        let responseBody = null;
        
        const res = {
            setHeader: (name, value) => { headers[name] = value; },
            status: (code) => { statusCode = code; return res; },
            json: (body) => { responseBody = body; }
        };
        
        const next = mock.fn();
        
        // First 3 requests should pass
        for (let i = 0; i < 3; i++) {
            middleware(req, res, next);
        }
        assert.strictEqual(next.mock.calls.length, 3);
        
        // 4th request should be rate limited
        middleware(req, res, next);
        
        assert.strictEqual(statusCode, 429);
        assert.strictEqual(responseBody.error, 'Too many requests');
        assert.ok(responseBody.retryAfterMs > 0);
        assert.strictEqual(next.mock.calls.length, 3); // Still 3, not called again
    });

    test('different clients have separate limits', () => {
        const middleware = rateLimitMiddleware({ windowMs: 60000, maxRequests: 2 });
        
        const req1 = { socket: { remoteAddress: '1.1.1.1' }, path: '/test' };
        const req2 = { socket: { remoteAddress: '2.2.2.2' }, path: '/test' };
        
        const res = {
            setHeader: () => {},
            status: () => res,
            json: () => {}
        };
        
        const next = mock.fn();
        
        // Client 1: 2 requests
        middleware(req1, res, next);
        middleware(req1, res, next);
        
        // Client 2: 2 requests
        middleware(req2, res, next);
        middleware(req2, res, next);
        
        // All 4 should pass
        assert.strictEqual(next.mock.calls.length, 4);
    });
});
