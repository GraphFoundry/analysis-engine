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
 * @property {BrokenPath[]} criticalPathsBroken - Top N broken paths
 */

/**
 * Simulate failure of a service by removing it from the graph (in-memory only)
 * Calculates traffic loss and identifies broken paths
 * 
 * Algorithm:
 * 1. Fetch k-hop upstream neighborhood
 * 2. Remove target node (in-memory)
 * 3. For each direct caller, compute lostTrafficRps (sum of edge rates)
 * 4. Find top N paths that included target (sorted by pathRps)
 * 
 * @param {FailureSimulationRequest} request - Simulation request
 * @returns {Promise<FailureSimulationResult>}
 */
async function simulateFailure(request) {
    const maxDepth = request.maxDepth || config.simulation.maxTraversalDepth;
    
    // Validate depth
    if (maxDepth < 1 || maxDepth > 3) {
        throw new Error(`maxDepth must be 1, 2, or 3. Got: ${maxDepth}`);
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
    
    // Calculate lost traffic for each caller
    const affectedCallers = directCallers.map(edge => ({
        serviceId: edge.source,
        lostTrafficRps: edge.rate,
        edgeErrorRate: edge.errorRate
    }));
    
    // Sort by lost traffic descending
    affectedCallers.sort((a, b) => b.lostTrafficRps - a.lostTrafficRps);
    
    // Find top N broken paths
    const brokenPaths = findTopPathsToTarget(
        snapshot,
        request.serviceId,
        maxDepth,
        config.simulation.maxPathsReturned
    );
    
    return {
        target: {
            serviceId: targetNode.serviceId,
            name: targetNode.name,
            namespace: targetNode.namespace
        },
        depth: maxDepth,
        affectedCallers,
        criticalPathsBroken: brokenPaths
    };
}

module.exports = {
    simulateFailure
};
