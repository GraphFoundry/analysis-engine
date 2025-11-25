/**
 * Graph Engine HTTP Provider
 * 
 * Fetches graph data from the service-graph-engine HTTP API.
 * Implements the same interface as Neo4jGraphProvider.
 */

const config = require('../config');
const { checkGraphHealth, getNeighborhood, getPeers } = require('../graphEngineClient');

/**
 * @typedef {import('./GraphDataProvider').GraphSnapshot} GraphSnapshot
 * @typedef {import('./GraphDataProvider').EdgeData} EdgeData
 * @typedef {import('./GraphDataProvider').NodeData} NodeData
 * @typedef {import('./GraphDataProvider').HealthResult} HealthResult
 */

/** Concurrency limit for parallel /peers requests */
const CONCURRENCY_LIMIT = 5;

/**
 * Split array into chunks for controlled concurrency
 * @param {Array} array 
 * @param {number} size 
 * @returns {Array<Array>}
 */
function chunkArray(array, size) {
    const chunks = [];
    for (let i = 0; i < array.length; i += size) {
        chunks.push(array.slice(i, i + size));
    }
    return chunks;
}

/**
 * Normalize service ID to plain name for Graph Engine API
 * Input may be "namespace:name" (from Neo4j mode) or plain "name" (direct)
 * Graph Engine uses plain names like "frontend", "checkoutservice"
 * @param {string} serviceId
 * @returns {string} Plain service name
 */
function normalizeServiceName(serviceId) {
    // If format is "namespace:name", extract just the name
    if (serviceId.includes(':')) {
        return serviceId.split(':').pop();
    }
    return serviceId;
}

class GraphEngineHttpProvider {
    // No persistent state needed for HTTP provider

    /**
     * Check staleness and handle according to config
     * @private
     * @returns {Promise<void>}
     * @throws {Error} If stale and required=true
     */
    async _checkStaleness() {
        const healthResult = await checkGraphHealth();
        
        if (!healthResult.ok) {
            const err = new Error(`Graph API unavailable: ${healthResult.error}`);
            err.statusCode = 503;
            throw err;
        }

        const { stale, lastUpdatedSecondsAgo } = healthResult.data;

        if (stale) {
            const staleAge = lastUpdatedSecondsAgo === null ? 'age unknown' : `${lastUpdatedSecondsAgo}s old`;
            if (config.graphApi.required) {
                const err = new Error(
                    `Graph data is stale (${staleAge}). Simulation aborted.`
                );
                err.statusCode = 503;
                throw err;
            } else {
                console.warn(
                    `[WARN] Graph data is stale (${staleAge}). Proceeding anyway.`
                );
            }
        }
    }

    /**
     * Fetch k-hop neighborhood using Graph Engine HTTP API
     * 
     * Algorithm:
     * 1. Check staleness via /graph/health
     * 2. GET /services/{target}/neighborhood?k=K -> nodes[]
     * 3. For each node, GET /services/{node}/peers?direction=out
     * 4. Filter edges to those where target is in node set
     * 5. Build GraphSnapshot with same shape as Neo4j provider
     * 
     * @param {string} targetServiceId - Target service ID (may be "namespace:name" or plain "name")
     * @param {number} maxDepth - Maximum traversal depth (1-3)
     * @returns {Promise<GraphSnapshot>}
     */
    async fetchUpstreamNeighborhood(targetServiceId, maxDepth) {
        // Validate depth
        if (maxDepth < 1 || maxDepth > 3 || !Number.isInteger(maxDepth)) {
            throw new Error(`Invalid maxDepth: ${maxDepth}. Must be 1, 2, or 3`);
        }

        // Normalize service ID: extract plain name from "namespace:name" format
        const serviceName = normalizeServiceName(targetServiceId);

        // Step 1: Check staleness
        await this._checkStaleness();

        // Step 2: Get neighborhood nodes
        const neighborhoodResult = await getNeighborhood(serviceName, maxDepth);
        
        if (!neighborhoodResult.ok) {
            if (neighborhoodResult.status === 404) {
                throw new Error(`Service not found: ${targetServiceId}`);
            }
            throw new Error(`Failed to fetch neighborhood: ${neighborhoodResult.error}`);
        }

        const nodeNames = neighborhoodResult.data.nodes || [];
        
        if (nodeNames.length === 0) {
            throw new Error(`Service not found: ${targetServiceId}`);
        }

        const nodeSet = new Set(nodeNames);

        // Build nodes Map
        // Note: Graph API returns plain service names, not "namespace:name"
        /** @type {Map<string, NodeData>} */
        const nodes = new Map();
        for (const serviceName of nodeNames) {
            nodes.set(serviceName, {
                serviceId: serviceName,
                name: serviceName,
                namespace: 'default'  // Default namespace since API doesn't provide it
            });
        }

        // Step 3: Fetch edges via /peers for each node (parallel with concurrency limit)
        /** @type {Map<string, EdgeData>} */
        const edgeMap = new Map(); // key: "source->target", dedupes by max(rate)

        const chunks = chunkArray(nodeNames, CONCURRENCY_LIMIT);
        
        for (const chunk of chunks) {
            const results = await Promise.all(
                chunk.map(async (nodeName) => {
                    const peersResult = await getPeers(nodeName, 'out');
                    if (peersResult.ok && peersResult.data.peers) {
                        return { nodeName, peers: peersResult.data.peers };
                    }
                    return { nodeName, peers: [] };
                })
            );

            for (const { nodeName, peers } of results) {
                for (const peer of peers) {
                    // Only keep edges where target is in our node set
                    const targetName = peer.service;
                    if (!nodeSet.has(targetName)) {
                        continue;
                    }

                    const edgeKey = `${nodeName}->${targetName}`;
                    const metrics = peer.metrics || {};
                    
                    const edge = {
                        source: nodeName,
                        target: targetName,
                        rate: metrics.rate ?? 0,
                        errorRate: metrics.errorRate ?? 0,
                        p50: metrics.p50 ?? 0,
                        p95: metrics.p95 ?? 0,
                        p99: metrics.p99 ?? 0
                    };

                    // Dedupe rule: keep edge with max(rate)
                    const existing = edgeMap.get(edgeKey);
                    if (!existing || edge.rate > existing.rate) {
                        edgeMap.set(edgeKey, edge);
                    }
                }
            }
        }

        // Step 4: Build edges array and adjacency maps
        const edges = Array.from(edgeMap.values());
        
        /** @type {Map<string, EdgeData[]>} */
        const incomingEdges = new Map();
        /** @type {Map<string, EdgeData[]>} */
        const outgoingEdges = new Map();

        // Initialize empty arrays for all nodes
        for (const nodeName of nodeNames) {
            incomingEdges.set(nodeName, []);
            outgoingEdges.set(nodeName, []);
        }

        // Populate adjacency maps
        for (const edge of edges) {
            // Safety guard for unexpected edge endpoints
            if (!incomingEdges.has(edge.target)) {
                incomingEdges.set(edge.target, []);
            }
            if (!outgoingEdges.has(edge.source)) {
                outgoingEdges.set(edge.source, []);
            }

            incomingEdges.get(edge.target).push(edge);
            outgoingEdges.get(edge.source).push(edge);
        }

        return {
            nodes,
            edges,
            incomingEdges,
            outgoingEdges,
            // Normalized target key for lookups (plain service name in API mode)
            targetKey: serviceName
        };
    }

    /**
     * Check Graph Engine health
     * @returns {Promise<HealthResult>}
     */
    async checkHealth() {
        const result = await checkGraphHealth();
        
        if (result.ok) {
            return {
                connected: true,
                stale: result.data.stale,
                lastUpdatedSecondsAgo: result.data.lastUpdatedSecondsAgo
            };
        } else {
            return {
                connected: false,
                error: result.error
            };
        }
    }

    /**
     * Close provider (no-op for HTTP provider)
     * @returns {Promise<void>}
     */
    async close() {
        // No persistent connections to close for HTTP provider
    }
}

module.exports = { GraphEngineHttpProvider };
