const assert = require('node:assert');
const { test, describe, beforeEach, afterEach } = require('node:test');

// Store original env
const originalEnv = { ...process.env };

describe('Config - Graph Engine Only Mode', () => {
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

    test('graphApi.graphEngineOnly is true when GRAPH_ENGINE_ONLY=true', () => {
        process.env.GRAPH_ENGINE_ONLY = 'true';
        process.env.GRAPH_ENGINE_BASE_URL = 'http://localhost:3000';
        
        const config = require('../src/config');
        
        assert.strictEqual(config.graphApi.graphEngineOnly, true);
    });

    test('graphApi.graphEngineOnly is false by default', () => {
        delete process.env.GRAPH_ENGINE_ONLY;
        process.env.NEO4J_URI = 'bolt://localhost';
        process.env.NEO4J_PASSWORD = 'test';
        
        const config = require('../src/config');
        
        assert.strictEqual(config.graphApi.graphEngineOnly, false);
    });

    test('graphApi.enabled is true when GRAPH_ENGINE_ONLY=true', () => {
        process.env.GRAPH_ENGINE_ONLY = 'true';
        process.env.GRAPH_ENGINE_BASE_URL = 'http://localhost:3000';
        
        const config = require('../src/config');
        
        assert.strictEqual(config.graphApi.enabled, true);
    });

    test('isGraphEngineOnlyMode returns correct value', () => {
        process.env.GRAPH_ENGINE_ONLY = 'true';
        
        const { isGraphEngineOnlyMode } = require('../src/config');
        
        assert.strictEqual(isGraphEngineOnlyMode(), true);
    });
});

describe('Provider Factory - Graph Engine Only Mode', () => {
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

    test('getProvider returns GraphEngineHttpProvider in graph-engine-only mode', () => {
        process.env.GRAPH_ENGINE_ONLY = 'true';
        process.env.USE_GRAPH_ENGINE_API = 'true';
        process.env.GRAPH_ENGINE_BASE_URL = 'http://localhost:3000';
        
        const { getProvider, resetProvider } = require('../src/providers');
        resetProvider();
        
        const provider = getProvider();
        
        // Check it's the HTTP provider (has no driver property)
        assert.strictEqual(provider.constructor.name, 'GraphEngineHttpProvider');
    });

    test('getProvider returns GraphEngineHttpProvider when USE_GRAPH_ENGINE_API=true', () => {
        process.env.USE_GRAPH_ENGINE_API = 'true';
        process.env.GRAPH_ENGINE_BASE_URL = 'http://localhost:3000';
        delete process.env.GRAPH_ENGINE_ONLY;
        
        const { getProvider, resetProvider } = require('../src/providers');
        resetProvider();
        
        const provider = getProvider();
        
        assert.strictEqual(provider.constructor.name, 'GraphEngineHttpProvider');
    });
});
