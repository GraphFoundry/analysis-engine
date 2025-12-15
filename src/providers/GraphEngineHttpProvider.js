/**
 * Graph Engine HTTP Provider
 * 
 * Fetches graph data from the service-graph-engine HTTP API.
 * Implements the GraphDataProvider interface.
 * 
 * Uses /neighborhood endpoint (single call) instead of N+1 /peers calls.
 */

const config = require('../config');
const { checkGraphHealth, getNeighborhood } = require('../graphEngineClient');

/**
 * @typedef {import('./GraphDataProvider').GraphSnapshot} GraphSnapshot
 * @typedef {import('./GraphDataProvider').EdgeData} EdgeData
 * @typedef {import('./GraphDataProvider').NodeData} NodeData
 * @typedef {import('./GraphDataProvider').HealthResult} HealthResult
 */

/**
 * Normalize service ID to plain name for Graph Engine API
 * Input may be "namespace:name" or plain "name"
 * Graph Engine uses plain names like "frontend", "checkoutservice"
 * 
 * TODO: Graph Engine assumes unique service names across namespaces.
 * If multiple namespaces exist, this will need enhancement.
 * 
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

/**
 * Merge two edge metrics for deduplicating edges with same (from, to)
 * 
 * Merge rules:
 * - rate: SUM (total traffic across duplicates)
 * - errorRate: rate-weighted average (fallback to max if total rate is 0)
 * - p50/p95/p99: MAX (conservative - worst-case latency)
 * 
 * @param {EdgeData} a - First edge
 * @param {EdgeData} b - Second edge
 * @returns {{rate: number, errorRate: number, p50: number, p95: number, p99: number}}
 */
function mergeEdgeMetrics(a, b) {
    const r1 = a.rate ?? 0;
    const r2 = b.rate ?? 0;
    const total = r1 + r2;

    const e1 = a.errorRate ?? 0;
    const e2 = b.errorRate ?? 0;

    return {
        rate: total,
        errorRate: total > 0 ? ((e1 * r1) + (e2 * r2)) / total : Math.max(e1, e2),
        p50: Math.max(a.p50 ?? 0, b.p50 ?? 0),
        p95: Math.max(a.p95 ?? 0, b.p95 ?? 0),
        p99: Math.max(a.p99 ?? 0, b.p99 ?? 0),
    };
}

class GraphEngineHttpProvider {
    // No persistent state needed for HTTP provider

    /**
     * Check staleness and return freshness metadata
     * @private
     * @param {Object} trace - Optional trace instance
     * @returns {Promise<{stale: boolean, lastUpdatedSecondsAgo: number|null, windowMinutes: number}>}
     * @throws {Error} If unavailable (503) or stale and required=true (503)
     */
    async _checkStaleness(trace = null) {
        const executeCheck = async () => {
            return await checkGraphHealth();
        };

        const healthResult = trace && trace.stage
            ? await trace.stage('staleness-check', executeCheck)
            : await executeCheck();
        
        if (!healthResult.ok) {
            const err = new Error(`Graph API unavailable: ${healthResult.error}`);
            err.statusCode = 503;
            throw err;
        }

        const { stale, lastUpdatedSecondsAgo, windowMinutes } = healthResult.data;

        // Add staleness summary to trace
        if (trace && trace.setSummary) {
            trace.setSummary('staleness-check', {
                stale,
                lastUpdatedSecondsAgo,
                windowMinutes
            });
        }

        if (stale) {
            const staleAge = lastUpdatedSecondsAgo === null ? 'age unknown' : `${lastUpdatedSecondsAgo}s old`;
            const err = new Error(
                `Graph data is stale (${staleAge}). Simulation aborted.`
            );
            err.statusCode = 503;
            throw err;
        }

        // Return freshness metadata for inclusion in snapshot
        return { stale, lastUpdatedSecondsAgo, windowMinutes };
    }

    /**
     * Fetch k-hop neighborhood using Graph Engine HTTP API
     * 
     * Algorithm:
     * 1. Check staleness via /graph/health (returns freshness metadata)
     * 2. GET /services/{target}/neighborhood?k=K -> { nodes[], edges[] }
     * 3. Build nodes Map from nodes array
     * 4. Build edges from edges array, deduping by (from,to) key with merge
     * 5. Build adjacency maps (incomingEdges, outgoingEdges)
     * 
     * @param {string} targetServiceId - Target service ID (may be "namespace:name" or plain "name")
     * @param {number} maxDepth - Maximum traversal depth (1-3)
     * @param {Object} options - Optional parameters (trace)
     * @returns {Promise<GraphSnapshot>}
     */
    async fetchUpstreamNeighborhood(targetServiceId, maxDepth, options = {}) {
        const trace = options.trace || null;
        // Validate depth
        if (maxDepth < 1 || maxDepth > 3 || !Number.isInteger(maxDepth)) {
            throw new Error(`Invalid maxDepth: ${maxDepth}. Must be 1, 2, or 3`);
        }

        // Normalize service ID: extract plain name from "namespace:name" format
        const serviceName = normalizeServiceName(targetServiceId);

        // Step 1: Check staleness + get freshness metadata
        const freshness = await this._checkStaleness(trace);

        // Step 2: Get neighborhood (nodes + edges in single call)
        const fetchNeighborhood = async () => {
            return await getNeighborhood(serviceName, maxDepth);
        };

        const neighborhoodResult = trace && trace.stage
            ? await trace.stage('fetch-neighborhood', fetchNeighborhood)
            : await fetchNeighborhood();
        
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
        const rawEdgesCount = (neighborhoodResult.data.edges || []).length;

        // Add fetch summary to trace
        if (trace && trace.setSummary) {
            trace.setSummary('fetch-neighborhood', {
                depthUsed: maxDepth,
                nodesReturned: nodeNames.length,
                edgesReturned: rawEdgesCount
            });
        }

        // Build nodes Map
        /** @type {Map<string, NodeData>} */
        const nodes = new Map();
        for (const name of nodeNames) {
            nodes.set(name, {
                serviceId: name,
                name: name,
                namespace: 'default'  // Graph Engine doesn't provide namespace
            });
        }

        // Step 3: Build edges from /neighborhood.edges (dedupe by from->to)
        const rawEdges = neighborhoodResult.data.edges || [];
        
        /** @type {Map<string, EdgeData>} */
        const edgeMap = new Map();

        for (const e of rawEdges) {
            const from = e.from;
            const to = e.to;
            
            // Skip malformed or out-of-neighborhood edges
            if (!from || !to) continue;
            if (!nodeSet.has(from) || !nodeSet.has(to)) continue;

            const edgeKey = `${from}->${to}`;
            
            const candidate = {
                source: from,
                target: to,
                rate: e.rate ?? 0,
                errorRate: e.errorRate ?? 0,
                p50: e.p50 ?? 0,
                p95: e.p95 ?? 0,
                p99: e.p99 ?? 0
            };

            const existing = edgeMap.get(edgeKey);
            if (!existing) {
                edgeMap.set(edgeKey, candidate);
            } else {
                // Merge: sum rates, weighted errorRate, max latencies
                const merged = mergeEdgeMetrics(existing, candidate);
                edgeMap.set(edgeKey, { source: from, target: to, ...merged });
            }
        }

        // Step 4: Build edges array and adjacency maps
        const edges = Array.from(edgeMap.values());
        
        /** @type {Map<string, EdgeData[]>} */
        const incomingEdges = new Map();
        /** @type {Map<string, EdgeData[]>} */
        const outgoingEdges = new Map();

        // Initialize empty arrays for all nodes
        for (const name of nodeNames) {
            incomingEdges.set(name, []);
            outgoingEdges.set(name, []);
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

        // Add build-snapshot summary to trace
        if (trace && trace.setSummary) {
            trace.setSummary('build-snapshot', {
                serviceCount: nodes.size,
                edgeCount: edges.length
            });
        }

        return {
            nodes,
            edges,
            incomingEdges,
            outgoingEdges,
            // Normalized target key for lookups (plain service name in API mode)
            targetKey: serviceName,
            // Data freshness metadata for simulation responses
            dataFreshness: {
                source: 'graph-engine',
                stale: freshness.stale,
                lastUpdatedSecondsAgo: freshness.lastUpdatedSecondsAgo,
                windowMinutes: freshness.windowMinutes
            }
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

module.exports = { GraphEngineHttpProvider, mergeEdgeMetrics, normalizeServiceName };
