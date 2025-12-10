const assert = require('node:assert');
const { test, describe, beforeEach, afterEach } = require('node:test');

// Store original env
const originalEnv = { ...process.env };

describe('Config - Graph Engine Only', () => {
    beforeEach(() => {
        // Clear cached modules
        delete require.cache[require.resolve('../src/config')];
    });

    afterEach(() => {
        // Restore original env
        Object.keys(process.env).forEach(key => {
            if (!(key in originalEnv)) delete process.env[key];
        });
        Object.assign(process.env, originalEnv);
        delete require.cache[require.resolve('../src/config')];
    });

    test('graphApi.baseUrl defaults to service-graph-engine:3000', () => {
        delete process.env.SERVICE_GRAPH_ENGINE_URL;
        delete process.env.GRAPH_ENGINE_BASE_URL;
        
        const config = require('../src/config');
        
        assert.strictEqual(config.graphApi.baseUrl, 'http://service-graph-engine:3000');
    });

    test('graphApi.baseUrl uses SERVICE_GRAPH_ENGINE_URL when set', () => {
        process.env.SERVICE_GRAPH_ENGINE_URL = 'http://custom-url:8080';
        
        const config = require('../src/config');
        
        assert.strictEqual(config.graphApi.baseUrl, 'http://custom-url:8080');
    });
});

describe('Provider Factory - Graph Engine Only', () => {
    beforeEach(() => {
        delete require.cache[require.resolve('../src/config')];
        delete require.cache[require.resolve('../src/providers')];
        delete require.cache[require.resolve('../src/providers/index')];
    });

    afterEach(() => {
        Object.keys(process.env).forEach(key => {
            if (!(key in originalEnv)) delete process.env[key];
        });
        Object.assign(process.env, originalEnv);
        delete require.cache[require.resolve('../src/config')];
        delete require.cache[require.resolve('../src/providers')];
        delete require.cache[require.resolve('../src/providers/index')];
    });

    test('getProvider always returns GraphEngineHttpProvider', () => {
        process.env.SERVICE_GRAPH_ENGINE_URL = 'http://localhost:3000';
        
        const { getProvider, resetProvider } = require('../src/providers');
        resetProvider();
        
        const provider = getProvider();
        
        assert.strictEqual(provider.constructor.name, 'GraphEngineHttpProvider');
    });

    test('getProvider returns same instance on multiple calls (singleton)', () => {
        process.env.SERVICE_GRAPH_ENGINE_URL = 'http://localhost:3000';
        
        const { getProvider, resetProvider } = require('../src/providers');
        resetProvider();
        
        const provider1 = getProvider();
        const provider2 = getProvider();
        
        assert.strictEqual(provider1, provider2);
    });
});

