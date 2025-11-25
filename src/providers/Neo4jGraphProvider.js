/**
 * Neo4j Graph Data Provider
 * 
 * Wraps existing Neo4j-based graph functions with lazy loading
 * to prevent neo4j-driver from being loaded when not needed.
 */

class Neo4jGraphProvider {
    /** @type {Object|null} */
    _graph = null;
    
    /** @type {Object|null} */
    _neo4j = null;

    /**
     * Lazily load Neo4j modules only when first used
     * @private
     */
    _load() {
        if (!this._graph) {
            // Lazy require - only loads neo4j-driver when actually used
            this._graph = require('../graph');
            this._neo4j = require('../neo4j');
        }
    }

    /**
     * Fetch k-hop upstream neighborhood from Neo4j
     * @param {string} targetServiceId - Target service ID
     * @param {number} maxDepth - Maximum traversal depth (1-3)
     * @returns {Promise<import('./GraphDataProvider').GraphSnapshot>}
     */
    async fetchUpstreamNeighborhood(targetServiceId, maxDepth) {
        this._load();
        const snapshot = await this._graph.fetchUpstreamNeighborhood(targetServiceId, maxDepth);
        // Add targetKey for consistency with GraphEngineHttpProvider
        // In Neo4j mode, keys are the same as input serviceId (namespace:name format)
        snapshot.targetKey = targetServiceId;
        return snapshot;
    }

    /**
     * Check Neo4j health
     * @returns {Promise<import('./GraphDataProvider').HealthResult>}
     */
    async checkHealth() {
        this._load();
        return this._neo4j.checkHealth();
    }

    /**
     * Close Neo4j driver connection
     * @returns {Promise<void>}
     */
    async close() {
        if (this._neo4j) {
            await this._neo4j.closeDriver();
        }
    }
}

module.exports = { Neo4jGraphProvider };
