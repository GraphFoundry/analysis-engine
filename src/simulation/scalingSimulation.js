const { getProvider } = require('../storage/providers');
const { findTopPathsToTarget } = require('./pathAnalysis');
const { generateScalingRecommendations } = require('../utils/recommendations');
const { createTrace } = require('../utils/trace');
const config = require('../config/config');

/**
 * @typedef {import('./providers/GraphDataProvider').EdgeData} EdgeData
 * @typedef {import('./providers/GraphDataProvider').GraphSnapshot} GraphSnapshot
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
 * Compute minimum hop distance from source to target using BFS
 * Returns null if no path exists
 * 
 * @param {GraphSnapshot} snapshot - Graph snapshot
 * @param {string} sourceId - Source service ID
 * @param {string} targetId - Target service ID
 * @returns {number|null} - Hop distance or null
 */
function computeHopDistance(snapshot, sourceId, targetId) {
    if (sourceId === targetId) return 0;
    
    const visited = new Set([sourceId]);
    const queue = [{ id: sourceId, dist: 0 }];
    
    while (queue.length > 0) {
        const { id, dist } = queue.shift();
        const edges = snapshot.outgoingEdges.get(id) || [];
        
        for (const edge of edges) {
            if (edge.target === targetId) {
                return dist + 1;
            }
            if (!visited.has(edge.target)) {
                visited.add(edge.target);
                queue.push({ id: edge.target, dist: dist + 1 });
            }
        }
    }
    
    return null; // No path found
}

/**
 * Compute weighted mean latency for a service's outgoing calls
 * Formula: SUM(rate * latency) / SUM(rate)
 * Returns null if total rate is 0 (no traffic) OR if any latency is missing
 * 
 * @param {EdgeData[]} edges - Outgoing edges
 * @param {string} metric - Latency metric (p50, p95, p99)
 * @param {Map<string, number>} [adjustedLatencies] - Optional adjusted latencies for specific targets
 * @returns {number|null} - Weighted mean latency in ms, or null if incomplete data
 */
function computeWeightedMeanLatency(edges, metric, adjustedLatencies = new Map()) {
    let totalWeightedLatency = 0;
    let totalRate = 0;
    
    for (const edge of edges) {
        const rate = edge.rate ?? 0;
        const latency = adjustedLatencies.has(edge.target) 
            ? adjustedLatencies.get(edge.target)
            : edge[metric];
        
        // Skip zero-rate edges
        if (rate <= 0) continue;
        
        // If any required latency is missing, can't compute honestly
        if (latency === null || latency === undefined) {
            return null;
        }
        
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
 * @param {Object} options - Optional parameters (traceOptions, trace, correlationId)
 * @returns {Promise<ScalingSimulationResult>}
 */
async function simulateScaling(request, options = {}) {
    const maxDepth = request.maxDepth || config.simulation.maxTraversalDepth;
    const latencyMetric = request.latencyMetric || config.simulation.defaultLatencyMetric;
    const modelType = request.model?.type || config.simulation.scalingModel;
    const alpha = request.model?.alpha ?? config.simulation.scalingAlpha;
    const trace = options.trace || createTrace(options.traceOptions || {});
    
    // Validate inputs
    if (!Number.isInteger(maxDepth) || maxDepth < 1 || maxDepth > 3) {
        throw new Error(`maxDepth must be integer 1, 2, or 3. Got: ${maxDepth}`);
    }
    if (!['p50', 'p95', 'p99'].includes(latencyMetric)) {
        throw new Error(`Invalid latencyMetric: ${latencyMetric}`);
    }
    if (!Number.isInteger(request.currentPods) || !Number.isInteger(request.newPods)) {
        throw new Error('currentPods and newPods must be integers');
    }
    if (request.currentPods <= 0 || request.newPods <= 0) {
        throw new Error('currentPods and newPods must be positive');
    }
    if (alpha < 0 || alpha > 1) {
        throw new Error('alpha must be between 0 and 1');
    }
    
    // Fetch upstream neighborhood via Graph Engine
    const provider = getProvider();
    const snapshot = await provider.fetchUpstreamNeighborhood(request.serviceId, maxDepth, { trace });
    
    // Use normalized target key from snapshot (handles namespace:name vs plain name difference)
    const targetKey = snapshot.targetKey || request.serviceId;
    
    // Get target node info
    const targetNode = snapshot.nodes.get(targetKey);
    if (!targetNode) {
        throw new Error(`Service not found: ${request.serviceId}`);
    }
    
    // Apply scaling formula to target (compute ONCE using rate-weighted mean of incoming latencies)
    const { adjustedLatencies, baseLatency, newLatency: projectedLatency } = await trace.stage('apply-scaling-model', async () => {
        const latMap = new Map();
        const edges = snapshot.incomingEdges.get(targetKey) || [];
        
        // Compute rate-weighted mean baseline latency from incoming edges
        let baseLat = null;
        if (edges.length > 0) {
            let totalWeighted = 0;
            let totalRate = 0;
            for (const edge of edges) {
                const rate = edge.rate ?? 0;
                const lat = edge[latencyMetric];
                if (rate > 0 && lat !== null && lat !== undefined) {
                    totalWeighted += rate * lat;
                    totalRate += rate;
                }
            }
            if (totalRate > 0) {
                baseLat = totalWeighted / totalRate;
            }
        }
        
        // Apply scaling model if we have baseline
        let newLat = null;
        if (baseLat !== null) {
            if (modelType === 'bounded_sqrt') {
                newLat = applyBoundedSqrtScaling(
                    baseLat,
                    request.currentPods,
                    request.newPods,
                    alpha
                );
            } else if (modelType === 'linear') {
                newLat = applyLinearScaling(
                    baseLat,
                    request.currentPods,
                    request.newPods
                );
            } else {
                throw new Error(`Unknown scaling model: ${modelType}`);
            }
            latMap.set(targetKey, newLat);
        }
        
        return { adjustedLatencies: latMap, baseLatency: baseLat, newLatency: newLat };
    });
    
    // Add apply-scaling-model summary to trace
    trace.setSummary('apply-scaling-model', {
        model: { type: modelType, alpha },
        currentPods: request.currentPods,
        newPods: request.newPods,
        latencyFactor: baseLatency && projectedLatency ? (projectedLatency / baseLatency).toFixed(2) : null
    });
    
    // Compute impact on ALL upstream nodes (not just direct callers)
    // This shows true propagation through the dependency graph
    const affectedCallers = [];
    for (const [nodeId, nodeData] of snapshot.nodes) {
        // Skip the target itself
        if (nodeId === targetKey) continue;
        
        const nodeEdges = snapshot.outgoingEdges.get(nodeId) || [];
        if (nodeEdges.length === 0) continue;
        
        const beforeMs = computeWeightedMeanLatency(nodeEdges, latencyMetric);
        const afterMs = computeWeightedMeanLatency(nodeEdges, latencyMetric, adjustedLatencies);
        
        // Only include if there's actual impact (delta != 0) or measurable latency
        const deltaMs = (beforeMs !== null && afterMs !== null) ? (afterMs - beforeMs) : null;
        
        affectedCallers.push({
            serviceId: nodeId,
            name: nodeData?.name ?? nodeId.split(':')[1],
            namespace: nodeData?.namespace ?? nodeId.split(':')[0],
            hopDistance: computeHopDistance(snapshot, nodeId, targetKey),
            beforeMs,
            afterMs,
            deltaMs
        });
    }
    
    // Sort by absolute delta descending (biggest improvements first, nulls last)
    affectedCallers.sort((a, b) => {
        if (a.deltaMs === null) return 1;
        if (b.deltaMs === null) return -1;
        return Math.abs(b.deltaMs) - Math.abs(a.deltaMs);
    });
    
    // Compute real multi-hop paths using findTopPathsToTarget (inside trace stage)
    const { affectedPaths } = await trace.stage('path-analysis', async () => {
        const topPaths = findTopPathsToTarget(
            snapshot,
            targetKey,
            maxDepth,
            config.simulation.maxPathsReturned
        );
        
        // For each path, compute before/after latency (sum of edge latencies)
        const paths = [];
        for (const pathInfo of topPaths) {
            const { path } = pathInfo;
            let beforeMs = 0;
            let afterMs = 0;
            let hasIncompleteData = false;
            
            // Sum latencies along path edges
            for (let i = 0; i < path.length - 1; i++) {
                const source = path[i];
                const target = path[i + 1];
                const edges = snapshot.outgoingEdges.get(source) || [];
                const edge = edges.find(e => e.target === target);
                
                if (!edge || edge[latencyMetric] === null || edge[latencyMetric] === undefined) {
                    hasIncompleteData = true;
                    break;
                }
                
                const edgeLatency = edge[latencyMetric];
                beforeMs += edgeLatency;
                
                // Use adjusted latency if this edge points to target
                if (target === targetKey && adjustedLatencies.has(target)) {
                    afterMs += adjustedLatencies.get(target);
                } else {
                    afterMs += edgeLatency;
                }
            }
            
            paths.push({
                path,
                pathRps: pathInfo.pathRps,
                beforeMs: hasIncompleteData ? null : beforeMs,
                afterMs: hasIncompleteData ? null : afterMs,
                deltaMs: hasIncompleteData ? null : (afterMs - beforeMs),
                incompleteData: hasIncompleteData
            });
        }
    
    // Sort by absolute delta descending (null deltas last)
    paths.sort((a, b) => {
        if (a.deltaMs === null) return 1;
        if (b.deltaMs === null) return -1;
        return Math.abs(b.deltaMs) - Math.abs(a.deltaMs);
    });
    
    return { affectedPaths: paths };
});

// Add path-analysis summary to trace
trace.setSummary('path-analysis', {
    pathsFound: affectedPaths.length,
    pathsReturned: affectedPaths.length
});
    
    // Build path lookup: for each caller, find their best (highest pathRps) path to target
    const callerBestPath = new Map();
    for (const pathObj of affectedPaths) {
        const startNode = pathObj.path[0];
        if (!callerBestPath.has(startNode) || pathObj.pathRps > callerBestPath.get(startNode).pathRps) {
            callerBestPath.set(startNode, pathObj);
        }
    }
    
    // Enrich affectedCallers with end-to-end latency from their best path
    for (const caller of affectedCallers) {
        const bestPath = callerBestPath.get(caller.serviceId);
        if (bestPath && bestPath.deltaMs !== null) {
            caller.endToEndBeforeMs = bestPath.beforeMs;
            caller.endToEndAfterMs = bestPath.afterMs;
            caller.endToEndDeltaMs = bestPath.deltaMs;
            caller.viaPath = bestPath.path;
        } else {
            caller.endToEndBeforeMs = null;
            caller.endToEndAfterMs = null;
            caller.endToEndDeltaMs = null;
            caller.viaPath = null;
        }
    }
    
    // Add compute-impact summary to trace
    trace.setSummary('compute-impact', {
        affectedCallersCount: affectedCallers.length,
        affectedPathsCount: affectedPaths.length,
        latencyDeltaSummary: baseLatency && projectedLatency ? {
            before: Math.round(baseLatency * 100) / 100,
            after: Math.round(projectedLatency * 100) / 100,
            delta: Math.round((projectedLatency - baseLatency) * 100) / 100
        } : null
    });
    
    // Determine data confidence based on staleness
    const dataFreshness = snapshot.dataFreshness ?? null;
    const confidence = dataFreshness?.stale ? 'low' : 'high';

    // Compute scaling direction
    const scalingDirection = request.newPods > request.currentPods ? 'up' 
        : request.newPods < request.currentPods ? 'down' 
        : 'none';

    // Build result object (without recommendations first)
    const result = {
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
        dataFreshness,
        confidence,
        latencyMetric,
        scalingModel: { type: modelType, alpha },
        currentPods: request.currentPods,
        newPods: request.newPods,
        latencyEstimate: {
            description: 'Rate-weighted mean of incoming edge latency to target',
            baselineMs: baseLatency,
            projectedMs: adjustedLatencies.get(targetKey) ?? null,
            deltaMs: (baseLatency !== null && adjustedLatencies.has(targetKey)) 
                ? (adjustedLatencies.get(targetKey) - baseLatency) 
                : null,
            unit: 'milliseconds'
        },
        scalingDirection,
        affectedCallers: {
            description: 'Edge-level impact: deltaMs is change in this caller\'s direct outgoing edge latency. endToEndDeltaMs is cumulative path latency change.',
            items: affectedCallers.slice(0, config.simulation.maxPathsReturned)
        },
        affectedPaths
    };

    // Generate explanation string
    const latencyInfo = result.latencyEstimate;
    const callersCount = result.affectedCallers.items.length;
    const pathsCount = result.affectedPaths.length;
    const directionWord = scalingDirection === 'up' ? 'up' : scalingDirection === 'down' ? 'down' : 'at same level';
    
    if (latencyInfo.baselineMs !== null && latencyInfo.projectedMs !== null) {
        const improvementWord = latencyInfo.deltaMs < 0 ? 'improves' : latencyInfo.deltaMs > 0 ? 'degrades' : 'maintains';
        result.explanation = `Scaling ${targetNode.name} ${directionWord} from ${request.currentPods} to ${request.newPods} pods ` +
            `${improvementWord} latency by ${Math.abs(latencyInfo.deltaMs).toFixed(1)}ms ` +
            `(baseline: ${latencyInfo.baselineMs.toFixed(1)}ms → projected: ${latencyInfo.projectedMs.toFixed(1)}ms). ` +
            `${callersCount} upstream caller(s) affected across ${pathsCount} path(s).`;
    } else {
        result.explanation = `Scaling ${targetNode.name} ${directionWord} from ${request.currentPods} to ${request.newPods} pods. ` +
            `Latency impact unknown due to missing edge metrics. ` +
            `${callersCount} upstream caller(s) identified across ${pathsCount} path(s).`;
    }

    // Add warnings if any path has incomplete data
    const incompletePathsCount = result.affectedPaths.filter(p => p.incompleteData).length;
    if (incompletePathsCount > 0) {
        result.warnings = [
            `${incompletePathsCount} of ${pathsCount} path(s) have incomplete latency data (missing edge metrics). Results may be partial.`
        ];
    }

    // Generate recommendations based on result (inside trace stage)
    result.recommendations = await trace.stage('recommendations', async () => {
        return generateScalingRecommendations(result);
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
    simulateScaling
};
