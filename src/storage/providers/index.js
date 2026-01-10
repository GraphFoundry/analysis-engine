/**
 * Provider Factory - Graph Engine Only
 * 
 * Returns GraphEngineHttpProvider as the single source of truth.
 * Uses singleton pattern to avoid multiple provider instances.
 */

const { GraphEngineHttpProvider } = require('./GraphEngineHttpProvider');

/** @type {import('./GraphEngineHttpProvider').GraphEngineHttpProvider | null} */
let _provider = null;

/**
 * Get the Graph Engine HTTP provider (singleton)
 * 
 * Graph Engine is the only data source - no fallback logic.
 * 
 * @returns {import('./GraphEngineHttpProvider').GraphEngineHttpProvider}
 */
function getProvider() {
    if (_provider) {
        return _provider;
    }

    _provider = new GraphEngineHttpProvider();
    return _provider;
}

/**
 * Reset provider singleton (for testing)
 */
function resetProvider() {
    _provider = null;
}

module.exports = { getProvider, resetProvider };
