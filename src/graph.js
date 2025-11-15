const { executeQuery } = require('./neo4j');
const config = require('./config');

/**
 * @typedef {import('./neo4j').EdgeData} EdgeData
 * @typedef {import('./neo4j').NodeData} NodeData
 */

/**
 * @typedef {Object} GraphSnapshot
 * @property {Map<string, NodeData>} nodes - Map of serviceId to node data
 * @property {EdgeData[]} edges - Array of all edges
 * @property {Map<string, EdgeData[]>} incomingEdges - Map of target serviceId to incoming edges
 * @property {Map<string, EdgeData[]>} outgoingEdges - Map of source serviceId to outgoing edges
 */

/**
 * Fetch k-hop upstream neighborhood (nodes that can reach target)
 * Uses 2-query approach to avoid duplicates and path explosion
 * 
 * @param {string} targetServiceId - Target service ID
 * @param {number} maxDepth - Maximum traversal depth (validated to 1-3)
 * @returns {Promise<GraphSnapshot>}
 */
async function fetchUpstreamNeighborhood(targetServiceId, maxDepth) {
    // Validate depth (1-3 only, safe for string injection)
    if (maxDepth < 1 || maxDepth > 3 || !Number.isInteger(maxDepth)) {
        throw new Error(`Invalid maxDepth: ${maxDepth}. Must be 1, 2, or 3`);
    }
    
    // Query A: Get all node IDs in upstream neighborhood
    // String-inject depth (validated integer) to avoid parameterization issues
    const nodeQuery = `
        MATCH (target:Service {serviceId: $targetId})
        OPTIONAL MATCH path = (upstream:Service)-[:CALLS_NOW*1..${maxDepth}]->(target)
        WITH target, COLLECT(DISTINCT upstream) AS upstreams
        UNWIND upstreams + [target] AS service
        WITH DISTINCT service
        WHERE service IS NOT NULL
        RETURN service.serviceId AS serviceId,
            service.name AS name,
            service.namespace AS namespace
    `;
    
    const nodeResult = await executeQuery(nodeQuery, { targetId: targetServiceId });
    
    if (nodeResult.records.length === 0) {
        throw new Error(`Service not found: ${targetServiceId}`);
    }
    
    // Build node set
    const nodes = new Map();
    const nodeIds = [];
    
    nodeResult.records.forEach(record => {
        const serviceId = record.get('serviceId');
        const name = record.get('name');
        const namespace = record.get('namespace');
        
        nodes.set(serviceId, { serviceId, name, namespace });
        nodeIds.push(serviceId);
    });
    
    // Query B: Fetch all edges among these nodes
    const edgeQuery = `
        MATCH (a:Service)-[r:CALLS_NOW]->(b:Service)
        WHERE a.serviceId IN $nodeIds AND b.serviceId IN $nodeIds
        RETURN 
            a.serviceId AS source,
            b.serviceId AS target,
            r.rate AS rate,
            r.errorRate AS errorRate,
            r.p50 AS p50,
            r.p95 AS p95,
            r.p99 AS p99
    `;
    
    const edgeResult = await executeQuery(edgeQuery, { nodeIds });
    
    // Build edge arrays and adjacency maps
    const edges = [];
    const incomingEdges = new Map();
    const outgoingEdges = new Map();
    
    // Initialize adjacency maps for all nodes
    nodeIds.forEach(id => {
        incomingEdges.set(id, []);
        outgoingEdges.set(id, []);
    });
    
    edgeResult.records.forEach(record => {
        const edge = {
            source: record.get('source'),
            target: record.get('target'),
            rate: record.get('rate') || 0,
            errorRate: record.get('errorRate') || 0,
            p50: record.get('p50') || 0,
            p95: record.get('p95') || 0,
            p99: record.get('p99') || 0
        };
        
        edges.push(edge);
        incomingEdges.get(edge.target).push(edge);
        outgoingEdges.get(edge.source).push(edge);
    });
    
    return {
        nodes,
        edges,
        incomingEdges,
        outgoingEdges
    };
}

/**
 * Find top N paths by traffic volume (bottleneck throughput)
 * Uses min(edge.rate) along path as proxy for path throughput
 * Hard-capped to prevent combinatorial explosion
 * 
 * @param {GraphSnapshot} snapshot - Graph snapshot
 * @param {string} targetServiceId - Target service ID
 * @param {number} maxDepth - Maximum path length
 * @param {number} maxPaths - Maximum paths to return
 * @returns {Array<{path: string[], pathRps: number}>}
 */
function findTopPathsToTarget(snapshot, targetServiceId, maxDepth, maxPaths = config.simulation.maxPathsReturned) {
    const paths = [];
    const visited = new Set();
    
    // DFS to enumerate paths (limited by maxPaths hard cap)
    function dfs(currentId, currentPath, minRate) {
        if (paths.length >= maxPaths * 2) return; // Safety: early exit at 2x limit
        
        if (currentId === targetServiceId && currentPath.length > 1) {
            paths.push({
                path: [...currentPath],
                pathRps: minRate
            });
            return;
        }
        
        if (currentPath.length >= maxDepth) return;
        
        const outgoing = snapshot.outgoingEdges.get(currentId) || [];
        for (const edge of outgoing) {
            if (visited.has(edge.target)) continue; // Prevent cycles
            
            visited.add(edge.target);
            currentPath.push(edge.target);
            
            const newMinRate = Math.min(minRate, edge.rate);
            dfs(edge.target, currentPath, newMinRate);
            
            currentPath.pop();
            visited.delete(edge.target);
        }
    }
    
    // Start DFS from all nodes (except target)
    for (const [nodeId, _] of snapshot.nodes) {
        if (nodeId === targetServiceId) continue;
        if (paths.length >= maxPaths * 2) break;
        
        visited.clear();
        visited.add(nodeId);
        dfs(nodeId, [nodeId], Infinity);
    }
    
    // Sort by pathRps descending, take top N
    paths.sort((a, b) => b.pathRps - a.pathRps);
    return paths.slice(0, maxPaths);
}

module.exports = {
    fetchUpstreamNeighborhood,
    findTopPathsToTarget
};
