const { getProvider } = require('../storage/providers');
const { findTopPathsToTarget } = require('./pathAnalysis');
const { generateFailureRecommendations } = require('../utils/recommendations');
const { createTrace } = require('../utils/trace');
const config = require('../config/config');

/**
 * @typedef {import('./providers/GraphDataProvider').EdgeData} EdgeData
 * @typedef {import('./providers/GraphDataProvider').GraphSnapshot} GraphSnapshot
 */

// ============================================================================
// Service ID Helpers (ensure canonical namespace:name format)
// ============================================================================

/**
 * Parse a service reference into namespace and name
 * Handles both "namespace:name" and plain "name" formats
 * 
 * @param {string} idOrName - Service ID or name
 * @returns {{namespace: string, name: string}}
 */
function parseServiceRef(idOrName) {
    if (!idOrName) return { namespace: 'default', name: '' };

    const str = String(idOrName);
    const colonIdx = str.indexOf(':');

    if (colonIdx > 0) {
        return {
            namespace: str.slice(0, colonIdx) || 'default',
            name: str.slice(colonIdx + 1) || ''
        };
    }
    return { namespace: 'default', name: str };
}

/**
 * Create canonical serviceId in "namespace:name" format
 * 
 * @param {string} namespace 
 * @param {string} name 
 * @returns {string}
 */
function toCanonicalServiceId(namespace, name) {
    const ns = namespace || 'default';
    return `${ns}:${name}`;
}

/**
 * Convert a node to output reference with canonical serviceId
 * 
 * @param {Object|undefined} node - Node from snapshot
 * @param {string} fallbackKey - Key to parse if node is missing
 * @returns {{serviceId: string, name: string, namespace: string}}
 */
function nodeToOutRef(node, fallbackKey) {
    const parsed = parseServiceRef(fallbackKey);
    const name = node?.name ?? parsed.name;
    const namespace = node?.namespace ?? parsed.namespace;
    return {
        serviceId: toCanonicalServiceId(namespace, name),
        name,
        namespace
    };
}

// ============================================================================
// Reachability Analysis Helpers
// ============================================================================

/**
 * Find entrypoint nodes (nodes with no incoming edges within the snapshot)
 * These are the "roots" from which we can traverse to find reachable nodes
 * 
 * @param {GraphSnapshot} snapshot 
 * @param {string} blockedKey - Node to exclude (the failed target)
 * @returns {string[]}
 */
function pickEntrypoints(snapshot, blockedKey) {
    const keys = Array.from(snapshot.nodes.keys()).filter(k => k !== blockedKey);

    // First choice: nodes with no incoming edges (within the neighborhood)
    let entrypoints = keys.filter(k => (snapshot.incomingEdges.get(k)?.length || 0) === 0);

    // Fallback: if neighborhood is truncated and has no "true roots", use all nodes except target
    if (entrypoints.length === 0) entrypoints = keys;

    return entrypoints;
}

/**
 * BFS to find all nodes reachable from entrypoints, excluding blocked node
 * 
 * @param {GraphSnapshot} snapshot 
 * @param {string[]} entrypoints - Starting nodes
 * @param {string} blockedKey - Node to treat as removed
 * @returns {Set<string>} - Set of reachable node keys
 */
function computeReachableNodes(snapshot, entrypoints, blockedKey) {
    const visited = new Set();
    const queue = [];

    for (const e of entrypoints) {
        if (!e || e === blockedKey) continue;
        visited.add(e);
        queue.push(e);
    }

    while (queue.length > 0) {
        const cur = queue.shift();
        const outs = snapshot.outgoingEdges.get(cur) || [];

        for (const edge of outs) {
            const nxt = edge.target;
            if (!nxt || nxt === blockedKey) continue;
            if (!snapshot.nodes.has(nxt)) continue;
            if (visited.has(nxt)) continue;

            visited.add(nxt);
            queue.push(nxt);
        }
    }

    return visited;
}

/**
 * Estimate lost traffic for unreachable nodes.
 * Splits loss into:
 *  - lostFromTargetRps: traffic that used to come from the failed/blocked node
 *  - lostFromReachableCutsRps: traffic from other reachable sources now cut off
 *  - lostTotalRps: sum of both
 * 
 * @param {GraphSnapshot} snapshot 
 * @param {Set<string>} reachableSet 
 * @param {string} blockedKey 
 * @returns {Map<string, {lostFromTargetRps: number, lostFromReachableCutsRps: number, lostTotalRps: number}>}
 */
function estimateBoundaryLostTraffic(snapshot, reachableSet, blockedKey) {
    const unreachableKeys = Array.from(snapshot.nodes.keys())
        .filter(k => k !== blockedKey && !reachableSet.has(k));

    const lostByNode = new Map();

    for (const nodeKey of unreachableKeys) {
        const incoming = snapshot.incomingEdges.get(nodeKey) || [];

        let lostFromTargetRps = 0;
        let lostFromReachableCutsRps = 0;

        for (const e of incoming) {
            const rate = e.rate ?? 0;

            if (e.source === blockedKey) {
                lostFromTargetRps += rate;
                continue;
            }

            if (reachableSet.has(e.source)) {
                lostFromReachableCutsRps += rate;
            }
        }

        const lostTotalRps = lostFromTargetRps + lostFromReachableCutsRps;

        lostByNode.set(nodeKey, { lostFromTargetRps, lostFromReachableCutsRps, lostTotalRps });
    }

    return lostByNode;
}

/**
 * @typedef {Object} FailureSimulationRequest
 * @property {string} serviceId - Target service ID
 * @property {number} [maxDepth] - Maximum traversal depth (default from config)
 */

/**
 * @typedef {Object} AffectedCaller
 * @property {string} serviceId - Caller service ID
 * @property {number} lostTrafficRps - Lost traffic in requests per second
 * @property {number} edgeErrorRate - Error rate on removed edge
 */

/**
 * @typedef {Object} BrokenPath
 * @property {string[]} path - Array of service IDs in path
 * @property {number} pathRps - Path throughput (bottleneck rate)
 */

/**
 * @typedef {Object} FailureSimulationResult
 * @property {Object} target - Target service info
 * @property {number} depth - Traversal depth used
 * @property {AffectedCaller[]} affectedCallers - Direct callers impacted
 * @property {BrokenPath[]} criticalPathsToTarget - Top N caller→target paths that become unavailable
 */

/**
 * Simulate failure of a service (treated as unavailable for path analysis)
 * Calculates traffic loss and identifies caller→target paths that become unavailable
 * 
 * Algorithm:
 * 1. Fetch k-hop upstream neighborhood
 * 2. If timeWindow provided, fetch aggregated metrics and overlay on snapshot
 * 3. Treat target as unavailable (not actually removed from snapshot)
 * 4. For each direct caller, aggregate lostTrafficRps (sum of all edge rates to target)
 * 5. Find top N caller→target paths (sorted by pathRps)
 * 
 * @param {FailureSimulationRequest} request - Simulation request
 * @param {Object} options - Optional parameters (traceOptions, correlationId)
 * @returns {Promise<FailureSimulationResult>}
 */
async function simulateFailure(request, options = {}) {
    const maxDepth = request.maxDepth || config.simulation.maxTraversalDepth;
    const timeWindow = request.timeWindow;
    const trace = options.trace || createTrace(options.traceOptions || {});

    // Validate depth (must be integer 1-3)
    if (!Number.isInteger(maxDepth) || maxDepth < 1 || maxDepth > 3) {
        throw new Error(`maxDepth must be integer 1, 2, or 3. Got: ${maxDepth}`);
    }

    // Fetch upstream neighborhood via Graph Engine
    const provider = getProvider();
    const snapshot = await provider.fetchUpstreamNeighborhood(request.serviceId, maxDepth, { trace });

    // ========================================================================
    // Optional: Overlay Time Window Telemetry
    // ========================================================================
    if (timeWindow) {
        const telemetryService = require('../services/telemetryService');
        const { from, to } = telemetryService.parseTimeWindow(timeWindow);

        await trace.stage('overlay-telemetry', async () => {
            const metricsMap = await telemetryService.getAggregatedEdgeMetrics(from, to);

            // Overlay metrics on existing edges in the snapshot
            for (const edge of snapshot.edges) {
                const key = `${edge.source}->${edge.target}`;
                const metrics = metricsMap.get(key);

                if (metrics) {
                    edge.rate = metrics.requestRate;
                    edge.errorRate = metrics.errorRate;
                }
            }
        });

        trace.setSummary('overlay-telemetry', { timeWindow, from, to });
    }

    // Use normalized target key from snapshot (handles namespace:name vs plain name difference)
    const targetKey = snapshot.targetKey || request.serviceId;

    // Get target node info
    const targetNode = snapshot.nodes.get(targetKey);
    if (!targetNode) {
        throw new Error(`Service not found: ${request.serviceId}`);
    }

    // Build canonical target reference
    const targetOut = nodeToOutRef(targetNode, targetKey);

    // Find all direct callers of target
    const directCallers = snapshot.incomingEdges.get(targetKey) || [];

    // Aggregate lost traffic by caller (handles duplicate edges to same target)
    const callerMap = new Map();
    for (const edge of directCallers) {
        const id = edge.source;
        const callerNode = snapshot.nodes.get(id);
        const callerOut = nodeToOutRef(callerNode, id);

        const prev = callerMap.get(id) || {
            serviceId: callerOut.serviceId,
            name: callerOut.name,
            namespace: callerOut.namespace,
            lostTrafficRps: 0,
            edgeErrorRate: 0
        };

        prev.lostTrafficRps += edge.rate;
        // Use max error rate as worst-case for this caller
        prev.edgeErrorRate = Math.max(prev.edgeErrorRate, edge.errorRate);

        callerMap.set(id, prev);
    }

    // Convert to array and sort by lost traffic descending
    const affectedCallers = Array.from(callerMap.values())
        .sort((a, b) => b.lostTrafficRps - a.lostTrafficRps);

    // Find top N paths to target (de-duplicated by path key)
    const rawPaths = await trace.stage('path-analysis', async () => {
        return findTopPathsToTarget(
            snapshot,
            targetKey,
            maxDepth,
            config.simulation.maxPathsReturned * 2 // Fetch extra to allow for de-dupe
        );
    });

    // De-duplicate paths by join key
    const seenPaths = new Set();
    const criticalPathsToTarget = [];
    for (const pathInfo of rawPaths) {
        const key = pathInfo.path.join('->');
        if (seenPaths.has(key)) continue;
        seenPaths.add(key);
        criticalPathsToTarget.push(pathInfo);
        if (criticalPathsToTarget.length >= config.simulation.maxPathsReturned) break;
    }

    // Add path-analysis summary to trace
    trace.setSummary('path-analysis', {
        pathsFound: rawPaths.length,
        pathsReturned: criticalPathsToTarget.length
    });

    // ========================================================================
    // Phase 3: Downstream and Unreachable Impact Analysis
    // ========================================================================

    // Direct downstream dependents of target (services the target calls)
    const directCallees = snapshot.outgoingEdges.get(targetKey) || [];
    const downstreamMap = new Map();

    for (const edge of directCallees) {
        const calleeKey = edge.target;
        if (!calleeKey || calleeKey === targetKey) continue;

        const calleeNode = snapshot.nodes.get(calleeKey);
        const calleeOut = nodeToOutRef(calleeNode, calleeKey);

        const prev = downstreamMap.get(calleeKey) || {
            serviceId: calleeOut.serviceId,
            name: calleeOut.name,
            namespace: calleeOut.namespace,
            lostTrafficRps: 0,
            edgeErrorRate: 0
        };

        prev.lostTrafficRps += edge.rate ?? 0;
        prev.edgeErrorRate = Math.max(prev.edgeErrorRate, edge.errorRate ?? 0);

        downstreamMap.set(calleeKey, prev);
    }

    const affectedDownstream = Array.from(downstreamMap.values())
        .sort((a, b) => b.lostTrafficRps - a.lostTrafficRps);

    // Compute reachability after "removing" the target (inside trace stage)
    const { unreachableServices, totalLostTrafficRps } = await trace.stage('compute-impact', async () => {
        const entrypoints = pickEntrypoints(snapshot, targetKey);
        const reachable = computeReachableNodes(snapshot, entrypoints, targetKey);
        const lostByNode = estimateBoundaryLostTraffic(snapshot, reachable, targetKey);

        const unreachableList = Array.from(snapshot.nodes.keys())
            .filter(k => k !== targetKey && !reachable.has(k))
            .map(k => {
                const n = snapshot.nodes.get(k);
                const out = nodeToOutRef(n, k);
                const loss = lostByNode.get(k) || { lostFromTargetRps: 0, lostFromReachableCutsRps: 0, lostTotalRps: 0 };
                return {
                    ...out,
                    lostTrafficRps: loss.lostTotalRps,
                    lostFromTargetRps: loss.lostFromTargetRps,
                    lostFromReachableCutsRps: loss.lostFromReachableCutsRps
                };
            })
            .sort((a, b) => b.lostTrafficRps - a.lostTrafficRps);

        const totalLost = affectedCallers.reduce((sum, c) => sum + c.lostTrafficRps, 0);

        return { unreachableServices: unreachableList, totalLostTrafficRps: totalLost };
    });

    // Add compute-impact summary to trace
    trace.setSummary('compute-impact', {
        affectedCallersCount: affectedCallers.length,
        affectedDownstreamCount: affectedDownstream.length,
        unreachableCount: unreachableServices.length,
        totalLostTrafficRps
    });

    // Determine data confidence based on staleness
    const dataFreshness = snapshot.dataFreshness ?? null;
    const confidence = dataFreshness?.stale ? 'low' : 'high';

    // Build explanation for operators
    const explanation = `If ${targetOut.name} fails, ${affectedCallers.length} upstream caller(s) lose direct access, ` +
        `${affectedDownstream.length} downstream service(s) lose traffic from this target, ` +
        `and ${unreachableServices.length} service(s) may become unreachable within the ${maxDepth}-hop neighborhood.`;

    // Build result object (without recommendations first)
    const result = {
        target: targetOut,
        neighborhood: {
            description: 'k-hop neighborhood subgraph around target (not full graph)',
            serviceCount: snapshot.nodes.size,
            edgeCount: snapshot.edges.length,
            depthUsed: maxDepth,
            generatedAt: new Date().toISOString()
        },
        dataFreshness,
        confidence,
        explanation,
        affectedCallers,
        affectedDownstream,
        unreachableServices,
        criticalPathsToTarget,
        totalLostTrafficRps
    };

    // Generate recommendations based on result (inside trace stage)
    result.recommendations = await trace.stage('recommendations', async () => {
        return generateFailureRecommendations(result);
    });

    // Add recommendations summary to trace
    trace.setSummary('recommendations', {
        recommendationCount: result.recommendations.length
    });

    // Attach pipeline trace if enabled
    const pipelineTrace = trace.finalize();
    if (pipelineTrace) {
        result.pipelineTrace = pipelineTrace;
    }

    return result;
}

module.exports = {
    simulateFailure,
    // Exported for unit testing
    _test: {
        parseServiceRef,
        toCanonicalServiceId,
        nodeToOutRef,
        pickEntrypoints,
        computeReachableNodes,
        estimateBoundaryLostTraffic
    }
};
