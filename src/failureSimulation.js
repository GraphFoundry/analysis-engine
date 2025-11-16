const { fetchUpstreamNeighborhood, findTopPathsToTarget } = require('./graph');
const config = require('./config');

/**
 * @typedef {import('./neo4j').EdgeData} EdgeData
 * @typedef {import('./graph').GraphSnapshot} GraphSnapshot
 */

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
 * 2. Treat target as unavailable (not actually removed from snapshot)
 * 3. For each direct caller, aggregate lostTrafficRps (sum of all edge rates to target)
 * 4. Find top N caller→target paths (sorted by pathRps)
 * 
 * @param {FailureSimulationRequest} request - Simulation request
 * @returns {Promise<FailureSimulationResult>}
 */
async function simulateFailure(request) {
    const maxDepth = request.maxDepth || config.simulation.maxTraversalDepth;
    
    // Validate depth (must be integer 1-3)
    if (!Number.isInteger(maxDepth) || maxDepth < 1 || maxDepth > 3) {
        throw new Error(`maxDepth must be integer 1, 2, or 3. Got: ${maxDepth}`);
    }
    
    // Fetch upstream neighborhood (read-only Neo4j query)
    const snapshot = await fetchUpstreamNeighborhood(request.serviceId, maxDepth);
    
    // Get target node info
    const targetNode = snapshot.nodes.get(request.serviceId);
    if (!targetNode) {
        throw new Error(`Service not found: ${request.serviceId}`);
    }
    
    // Find all direct callers of target
    const directCallers = snapshot.incomingEdges.get(request.serviceId) || [];
    
    // Aggregate lost traffic by caller (handles duplicate edges to same target)
    const callerMap = new Map();
    for (const edge of directCallers) {
        const id = edge.source;
        const callerNode = snapshot.nodes.get(id);
        const prev = callerMap.get(id) || { 
            serviceId: id, 
            name: callerNode?.name ?? id.split(':')[1],
            namespace: callerNode?.namespace ?? id.split(':')[0],
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
    const rawPaths = findTopPathsToTarget(
        snapshot,
        request.serviceId,
        maxDepth,
        config.simulation.maxPathsReturned * 2 // Fetch extra to allow for de-dupe
    );
    
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
    
    return {
        target: {
            serviceId: targetNode.serviceId,
            name: targetNode.name,
            namespace: targetNode.namespace
        },
        neighborhood: {
            description: 'k-hop upstream subgraph around target (not full graph)',
            serviceCount: snapshot.nodes.size,
            edgeCount: snapshot.edges.length,
            depthUsed: maxDepth,
            generatedAt: new Date().toISOString()
        },
        affectedCallers,
        criticalPathsToTarget,
        totalLostTrafficRps: affectedCallers.reduce((sum, c) => sum + c.lostTrafficRps, 0)
    };
}

module.exports = {
    simulateFailure
};
