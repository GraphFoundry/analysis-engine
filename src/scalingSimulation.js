const { fetchUpstreamNeighborhood } = require('./graph');
const config = require('./config');

/**
 * @typedef {import('./neo4j').EdgeData} EdgeData
 * @typedef {import('./graph').GraphSnapshot} GraphSnapshot
 */

/**
 * @typedef {Object} ScalingModel
 * @property {string} type - Model type (bounded_sqrt, linear)
 * @property {number} [alpha] - Fixed overhead fraction (0.0-1.0)
 */

/**
 * @typedef {Object} ScalingSimulationRequest
 * @property {string} serviceId - Target service ID
 * @property {number} currentPods - Current pod count
 * @property {number} newPods - New pod count
 * @property {string} [latencyMetric] - Latency metric to use (p50, p95, p99)
 * @property {ScalingModel} [model] - Scaling model configuration
 * @property {number} [maxDepth] - Maximum traversal depth
 */

/**
 * @typedef {Object} AffectedCallerScaling
 * @property {string} serviceId - Caller service ID
 * @property {number|null} beforeMs - Weighted mean latency before scaling (null if no traffic)
 * @property {number|null} afterMs - Weighted mean latency after scaling (null if no traffic)
 * @property {number|null} deltaMs - Latency change (negative = improvement)
 */

/**
 * @typedef {Object} AffectedPath
 * @property {string[]} path - Array of service IDs in path
 * @property {number} beforeMs - Path latency before scaling
 * @property {number} afterMs - Path latency after scaling
 * @property {number} deltaMs - Latency change
 */

/**
 * @typedef {Object} ScalingSimulationResult
 * @property {Object} target - Target service info
 * @property {string} latencyMetric - Latency metric used
 * @property {number} currentPods - Current pod count
 * @property {number} newPods - New pod count
 * @property {AffectedCallerScaling[]} affectedCallers - Callers with latency changes
 * @property {AffectedPath[]} affectedPaths - Top N paths with latency changes
 */

/**
 * Apply bounded square root scaling formula
 * Formula: newLatency = baseLatency * (alpha + (1 - alpha) * (1 / sqrt(ratio)))
 * Clamped to minimum = baseLatency * minLatencyFactor
 * 
 * @param {number} baseLatency - Current latency (ms)
 * @param {number} currentPods - Current pod count
 * @param {number} newPods - New pod count
 * @param {number} alpha - Fixed overhead fraction (0.0-1.0)
 * @returns {number} - New latency (ms)
 */
function applyBoundedSqrtScaling(baseLatency, currentPods, newPods, alpha) {
    const ratio = newPods / currentPods;
    const improvement = 1 / Math.sqrt(ratio);
    const newLatency = baseLatency * (alpha + (1 - alpha) * improvement);
    
    // Clamp to minimum (can't improve beyond 60% of baseline by default)
    const minLatency = baseLatency * config.simulation.minLatencyFactor;
    return Math.max(newLatency, minLatency);
}

/**
 * Apply linear scaling formula
 * Formula: newLatency = baseLatency * (currentPods / newPods)
 * 
 * @param {number} baseLatency - Current latency (ms)
 * @param {number} currentPods - Current pod count
 * @param {number} newPods - New pod count
 * @returns {number} - New latency (ms)
 */
function applyLinearScaling(baseLatency, currentPods, newPods) {
    return baseLatency * (currentPods / newPods);
}

/**
 * Compute weighted mean latency for a service's outgoing calls
 * Formula: SUM(rate * latency) / SUM(rate)
 * Returns null if total rate is 0 (no traffic)
 * 
 * @param {EdgeData[]} edges - Outgoing edges
 * @param {string} metric - Latency metric (p50, p95, p99)
 * @param {Map<string, number>} [adjustedLatencies] - Optional adjusted latencies for specific targets
 * @returns {number|null} - Weighted mean latency in ms, or null if no traffic
 */
function computeWeightedMeanLatency(edges, metric, adjustedLatencies = new Map()) {
    let totalWeightedLatency = 0;
    let totalRate = 0;
    
    for (const edge of edges) {
        const rate = edge.rate;
        const latency = adjustedLatencies.has(edge.target) 
            ? adjustedLatencies.get(edge.target)
            : edge[metric];
        
        totalWeightedLatency += rate * latency;
        totalRate += rate;
    }
    
    // Handle zero traffic case
    if (totalRate === 0) {
        return null;
    }
    
    return totalWeightedLatency / totalRate;
}

/**
 * Simulate scaling of a service (increase/decrease pod count)
 * Adjusts latencies on incoming edges to target, propagates upstream
 * 
 * Algorithm:
 * 1. Fetch k-hop upstream neighborhood
 * 2. For each incoming edge to target, apply scaling formula (in-memory)
 * 3. For each caller, compute weighted mean latency before/after
 * 4. Compute path latencies (sum of edges along path)
 * 5. Return top N paths by traffic volume
 * 
 * @param {ScalingSimulationRequest} request - Simulation request
 * @returns {Promise<ScalingSimulationResult>}
 */
async function simulateScaling(request) {
    const maxDepth = request.maxDepth || config.simulation.maxTraversalDepth;
    const latencyMetric = request.latencyMetric || config.simulation.defaultLatencyMetric;
    const modelType = request.model?.type || config.simulation.scalingModel;
    const alpha = request.model?.alpha ?? config.simulation.scalingAlpha;
    
    // Validate inputs
    if (maxDepth < 1 || maxDepth > 3) {
        throw new Error(`maxDepth must be 1, 2, or 3. Got: ${maxDepth}`);
    }
    if (!['p50', 'p95', 'p99'].includes(latencyMetric)) {
        throw new Error(`Invalid latencyMetric: ${latencyMetric}`);
    }
    if (request.currentPods <= 0 || request.newPods <= 0) {
        throw new Error('currentPods and newPods must be positive');
    }
    if (alpha < 0 || alpha > 1) {
        throw new Error('alpha must be between 0 and 1');
    }
    
    // Fetch upstream neighborhood (read-only Neo4j query)
    const snapshot = await fetchUpstreamNeighborhood(request.serviceId, maxDepth);
    
    // Get target node info
    const targetNode = snapshot.nodes.get(request.serviceId);
    if (!targetNode) {
        throw new Error(`Service not found: ${request.serviceId}`);
    }
    
    // Apply scaling formula to all incoming edges to target (in-memory adjustment)
    const adjustedLatencies = new Map();
    const incomingEdges = snapshot.incomingEdges.get(request.serviceId) || [];
    
    for (const edge of incomingEdges) {
        const currentLatency = edge[latencyMetric];
        let newLatency;
        
        if (modelType === 'bounded_sqrt') {
            newLatency = applyBoundedSqrtScaling(
                currentLatency,
                request.currentPods,
                request.newPods,
                alpha
            );
        } else if (modelType === 'linear') {
            newLatency = applyLinearScaling(
                currentLatency,
                request.currentPods,
                request.newPods
            );
        } else {
            throw new Error(`Unknown scaling model: ${modelType}`);
        }
        
        adjustedLatencies.set(request.serviceId, newLatency);
    }
    
    // Calculate impact on direct callers
    const affectedCallers = [];
    
    for (const edge of incomingEdges) {
        const callerId = edge.source;
        const callerEdges = snapshot.outgoingEdges.get(callerId) || [];
        
        const beforeMs = computeWeightedMeanLatency(callerEdges, latencyMetric);
        const afterMs = computeWeightedMeanLatency(callerEdges, latencyMetric, adjustedLatencies);
        
        affectedCallers.push({
            serviceId: callerId,
            beforeMs,
            afterMs,
            deltaMs: (beforeMs !== null && afterMs !== null) ? (afterMs - beforeMs) : null
        });
    }
    
    // Sort by absolute delta descending (biggest improvements first)
    affectedCallers.sort((a, b) => {
        if (a.deltaMs === null) return 1;
        if (b.deltaMs === null) return -1;
        return Math.abs(b.deltaMs) - Math.abs(a.deltaMs);
    });
    
    // Compute path latencies (simplified: sum of edge latencies along path)
    // For demo, find paths to target and compute before/after
    const affectedPaths = [];
    
    // For each direct caller, create simple 2-hop paths
    for (const edge of incomingEdges.slice(0, config.simulation.maxPathsReturned)) {
        const path = [edge.source, request.serviceId];
        const beforeMs = edge[latencyMetric];
        const afterMs = adjustedLatencies.get(request.serviceId) || beforeMs;
        
        affectedPaths.push({
            path,
            beforeMs,
            afterMs,
            deltaMs: afterMs - beforeMs
        });
    }
    
    // Sort by absolute delta descending
    affectedPaths.sort((a, b) => Math.abs(b.deltaMs) - Math.abs(a.deltaMs));
    
    return {
        target: {
            serviceId: targetNode.serviceId,
            name: targetNode.name,
            namespace: targetNode.namespace
        },
        latencyMetric,
        currentPods: request.currentPods,
        newPods: request.newPods,
        affectedCallers: affectedCallers.slice(0, config.simulation.maxPathsReturned),
        affectedPaths
    };
}

module.exports = {
    simulateScaling
};
