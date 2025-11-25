/**
 * Provider Factory
 * 
 * Returns the appropriate GraphDataProvider based on configuration.
 * Uses singleton pattern to avoid multiple provider instances.
 */

const config = require('../config');

/** @type {import('./Neo4jGraphProvider').Neo4jGraphProvider | import('./GraphEngineHttpProvider').GraphEngineHttpProvider | null} */
let _provider = null;

/**
 * Get the configured graph data provider (singleton)
 * 
 * When USE_GRAPH_ENGINE_API=true, returns GraphEngineHttpProvider.
 * Otherwise, returns Neo4jGraphProvider (lazy-loads neo4j-driver).
 * 
 * @returns {import('./Neo4jGraphProvider').Neo4jGraphProvider | import('./GraphEngineHttpProvider').GraphEngineHttpProvider}
 */
function getProvider() {
    if (_provider) {
        return _provider;
    }

    if (config.graphApi.enabled) {
        // Use HTTP provider - does NOT load neo4j-driver
        const { GraphEngineHttpProvider } = require('./GraphEngineHttpProvider');
        _provider = new GraphEngineHttpProvider();
    } else {
        // Use Neo4j provider - lazy loads neo4j-driver on first use
        const { Neo4jGraphProvider } = require('./Neo4jGraphProvider');
        _provider = new Neo4jGraphProvider();
    }

    return _provider;
}

/**
 * Reset provider singleton (for testing)
 */
function resetProvider() {
    _provider = null;
}

module.exports = { getProvider, resetProvider };
