/**
 * Path Analysis Functions
 * 
 * Pure computational functions for analyzing paths in a graph snapshot.
 * These functions have NO Neo4j dependency - they work on in-memory data structures.
 */

const config = require('./config');

/**
 * @typedef {import('./providers/GraphDataProvider').GraphSnapshot} GraphSnapshot
 */

/**
 * Find top N paths by traffic volume (bottleneck throughput)
 * Uses min(edge.rate) along path as proxy for path throughput
 * Hard-capped to prevent combinatorial explosion
 * 
 * @param {GraphSnapshot} snapshot - Graph snapshot
 * @param {string} targetServiceId - Target service ID
 * @param {number} maxDepth - Maximum hops (edges) in path
 * @param {number} [maxPaths] - Maximum paths to return (default from config)
 * @returns {Array<{path: string[], pathRps: number}>}
 */
function findTopPathsToTarget(snapshot, targetServiceId, maxDepth, maxPaths = config.simulation.maxPathsReturned) {
    const paths = [];
    const visited = new Set();
    
    // DFS to enumerate paths (limited by maxPaths hard cap)
    // Uses hop-based depth: hops = currentPath.length - 1 (edges, not nodes)
    function dfs(currentId, currentPath, minRate) {
        if (paths.length >= maxPaths * 2) return; // Safety: early exit at 2x limit
        
        const hops = currentPath.length - 1; // hops = number of edges traversed
        
        // Found target with at least 1 hop
        if (currentId === targetServiceId && hops >= 1) {
            paths.push({
                path: [...currentPath],
                pathRps: minRate
            });
            return;
        }
        
        // Stop exploring if we've reached max hops
        if (hops >= maxDepth) return;
        
        // Sort outgoing edges for determinism: by rate desc, then target name asc
        const outgoing = (snapshot.outgoingEdges.get(currentId) || [])
            .slice()
            .sort((e1, e2) => (e2.rate - e1.rate) || e1.target.localeCompare(e2.target));
        
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
    
    // Start DFS from all nodes (except target), sorted for determinism
    const startNodeIds = Array.from(snapshot.nodes.keys()).sort((a, b) => a.localeCompare(b));
    for (const nodeId of startNodeIds) {
        if (nodeId === targetServiceId) continue;
        if (paths.length >= maxPaths * 2) break;
        
        visited.clear();
        visited.add(nodeId);
        dfs(nodeId, [nodeId], Infinity);
    }
    
    // Sort by pathRps descending (already deterministic via sorted exploration)
    paths.sort((a, b) => b.pathRps - a.pathRps);
    return paths.slice(0, maxPaths);
}

module.exports = {
    findTopPathsToTarget
};
