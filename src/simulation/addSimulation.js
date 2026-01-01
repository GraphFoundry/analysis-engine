const { getServicesWithPlacement } = require('../clients/graphEngineClient');
const config = require('../config/config');
const telemetryService = require('../services/telemetryService');

/**
 * @typedef {Object} AddSimulationRequest
 * @property {string} serviceName - Name of the new service
 * @property {number} cpuRequest - CPU cores requested per pod
 * @property {number} ramRequest - RAM MB requested per pod
 * @property {number} replicas - Number of replicas
 * @property {string} [timeWindow] - Historical time window (e.g. '1w')
 */

/**
 * @typedef {Object} NodeCapacity
 * @property {string} node - Node name
 * @property {number} cpuAvailable - Available CPU cores
 * @property {number} ramAvailableMB - Available RAM in MB
 * @property {number} cpuTotal - Total CPU cores
 * @property {number} ramTotalMB - Total RAM in MB
 * @property {boolean} canFit - Whether this node can fit at least one pod
 * @property {number} maxPods - Max pods this node can fit
 */

/**
 * @typedef {Object} AddSimulationResult
 * @property {boolean} success - Whether the placement is possible for all replicas
 * @property {string} confidence - 'high' or 'low' based on data freshness
 * @property {string} explanation - Human readable explanation
 * @property {Array<NodeCapacity>} nodeAnalysis - Analysis of each node's capacity
 * @property {Object} recommendation - Placement recommendation
 * @property {Array<{node: string, replicas: number}>} recommendation.distribution - Recommended pod distribution
 * @property {number} totalCapacityPods - Total number of pods the cluster can fit
 */

/**
 * Simulate adding a new service to the cluster.
 * Checks if there is enough capacity (CPU/RAM) on existing nodes to schedule the requested pods.
 * 
 * @param {AddSimulationRequest} request 
 * @returns {Promise<AddSimulationResult>}
 */
async function simulateAdd(request) {
    const { serviceName, cpuRequest = 0.1, ramRequest = 128, replicas = 1, dependencies = [], timeWindow } = request;

    // Validate inputs
    if (cpuRequest <= 0 || ramRequest <= 0 || replicas <= 0) {
        throw new Error('Invalid resource requests: cpu, ram, and replicas must be positive');
    }

    // 1. Fetch current cluster state
    const result = await getServicesWithPlacement();

    if (!result.ok) {
        throw new Error(`Failed to fetch cluster state: ${result.error}`);
    }

    // 2. Extract Node Metrics
    // We need to look at all unique nodes and their current usage
    const nodeMap = new Map();

    // Iterate through all services to find all nodes and their reported usage
    const services = result.data.services || [];
    services.forEach(svc => {
        if (svc.placement && svc.placement.nodes) {
            svc.placement.nodes.forEach(n => {
                if (!n.node) return;

                // We assume the node resource totals are consistent across reports
                // Usage needs to be aggregated or taken from the node-level report
                // In graphEngineClient type defs, it says placement.nodes has:
                // resources: { cpu: { usagePercent, cores }, ram: { usedMB, totalMB } }

                // If we haven't seen this node, add it
                if (!nodeMap.has(n.node)) {
                    nodeMap.set(n.node, {
                        name: n.node,
                        cpuUsagePercent: n.resources?.cpu?.usagePercent || 0,
                        cpuCores: n.resources?.cpu?.cores || 0,
                        ramUsedMB: n.resources?.ram?.usedMB || 0,
                        ramTotalMB: n.resources?.ram?.totalMB || 0
                    });
                }
            });
        }
    });

    // OVERRIDE with historical metrics if requested
    if (timeWindow) {
        try {
            const { from, to } = telemetryService.parseTimeWindow(timeWindow);
            const aggregatedNodes = await telemetryService.getAggregatedNodeMetrics(from, to);

            // Loop through our known nodes and update their usage with historical averages
            for (const [nodeName, nodeData] of nodeMap) {
                const history = aggregatedNodes.get(nodeName);
                if (history) {
                    nodeData.cpuUsagePercent = history.cpuUsagePercent;
                    nodeData.ramUsedMB = history.ramUsageMB;
                    nodeData.isHistorical = true;
                }
            }
        } catch (err) {
            console.error('Failed to overlay historical node metrics:', err);
        }
    }

    const nodes = Array.from(nodeMap.values());

    if (nodes.length === 0) {
        throw new Error('No nodes found in cluster state. Cannot perform placement analysis.');
    }

    // --- HOST RESOURCE DEDUPLICATION (Minikube Fix) ---
    // If multiple nodes are detected as "minikube", they likely share the host's resources.
    // Standard reporting (e.g. docker driver) often reports the full Host CPU/RAM for all nodes, leading to double counting.
    // We adjust available capacity by treating them as a shared pool.

    const minikubeNodes = nodes.filter(n => n.name.toLowerCase().includes('minikube'));

    if (minikubeNodes.length > 1) {
        // Assume shared host: Capacity is the MAX of any node (assuming identical reporting), Usage is the SUM of all nodes.
        const sharedCpuTotal = Math.max(...minikubeNodes.map(n => n.cpuCores));
        const sharedRamTotal = Math.max(...minikubeNodes.map(n => n.ramTotalMB));

        const sharedCpuUsed = minikubeNodes.reduce((sum, n) => sum + ((n.cpuUsagePercent / 100) * n.cpuCores), 0);
        const sharedRamUsed = minikubeNodes.reduce((sum, n) => sum + n.ramUsedMB, 0);

        const sharedCpuAvailable = Math.max(0, sharedCpuTotal - sharedCpuUsed);
        const sharedRamAvailable = Math.max(0, sharedRamTotal - sharedRamUsed);

        // Apply the tighter constraint (Shared Available vs Node Reported Available)
        // We override the values in the node objects so the downstream analysis uses the corrected values.
        minikubeNodes.forEach(node => {
            // Node reported available
            const nodeCpuAvail = Math.max(0, node.cpuCores - ((node.cpuUsagePercent / 100) * node.cpuCores));
            const nodeRamAvail = Math.max(0, node.ramTotalMB - node.ramUsedMB);

            // Effective available is the minimum of local node headroom and global shared headroom
            node.effectiveCpuAvailable = Math.min(nodeCpuAvail, sharedCpuAvailable);
            node.effectiveRamAvailable = Math.min(nodeRamAvail, sharedRamAvailable);
        });
    }

    // 3. Analyze Capacity per Node
    const nodeAnalysis = nodes.map(node => {
        // Use effective available if calculated (minikube), else calculate standard
        let cpuAvailable, ramAvailable;

        if (node.effectiveCpuAvailable !== undefined) {
            cpuAvailable = node.effectiveCpuAvailable;
            ramAvailable = node.effectiveRamAvailable;
        } else {
            const cpuUsed = (node.cpuUsagePercent / 100) * node.cpuCores;
            cpuAvailable = Math.max(0, node.cpuCores - cpuUsed);
            ramAvailable = Math.max(0, node.ramTotalMB - node.ramUsedMB);
        }

        // Check how many pods fit
        // Constraint: Pod fits if CPU <= Available AND RAM <= Available
        const cpuFit = Math.floor(cpuAvailable / cpuRequest);
        const ramFit = Math.floor(ramAvailable / ramRequest);
        const maxPods = Math.min(cpuFit, ramFit);

        return {
            node: node.name,
            cpuAvailable: Number.parseFloat(cpuAvailable.toFixed(2)),
            ramAvailableMB: Number.parseFloat(ramAvailable.toFixed(2)),
            cpuTotal: node.cpuCores,
            ramTotalMB: node.ramTotalMB,
            canFit: maxPods > 0,
            maxPods
        };
    });

    // 4. Generate Recommendation (Greedy Strategy with Scoring)
    // Sort nodes by remaining capacity score
    // Score based on how 'easily' it fits relative to available resources
    const scoredNodes = nodeAnalysis.map(n => {
        let score = 0;
        if (n.canFit) {
            const cpuHeadroom = n.cpuTotal > 0 ? n.cpuAvailable / n.cpuTotal : 0;
            const ramHeadroom = n.ramTotalMB > 0 ? n.ramAvailableMB / n.ramTotalMB : 0;
            // Base 50 + up to 50 for headroom
            score = Math.floor(50 + ((cpuHeadroom + ramHeadroom) / 2) * 50);
        } else {
            // 0-49 based on how close it is
            const cpuFrac = n.cpuTotal > 0 ? Math.min(1, n.cpuAvailable / cpuRequest) : 0;
            const ramFrac = n.ramTotalMB > 0 ? Math.min(1, n.ramAvailableMB / ramRequest) : 0;
            score = Math.floor(((cpuFrac + ramFrac) / 2) * 40);
        }

        return {
            ...n,
            score,
            // Add UI-friendly fields
            nodeName: n.node,
            suitable: n.canFit,
            reason: n.canFit ? undefined : (n.cpuAvailable < cpuRequest ? 'Insufficient CPU' : 'Insufficient RAM'),
            availableCpu: n.cpuAvailable,
            availableRam: n.ramAvailableMB
        };
    });

    // Sort by score descending
    scoredNodes.sort((a, b) => b.score - a.score);

    const totalCapacityPods = nodeAnalysis.reduce((sum, n) => sum + n.maxPods, 0);

    // Distribution
    let remainingReplicas = replicas;
    const distribution = [];

    for (const node of scoredNodes) {
        if (remainingReplicas <= 0) break;
        if (node.maxPods > 0) {
            const take = Math.min(remainingReplicas, node.maxPods);
            distribution.push({ node: node.node, replicas: take });
            remainingReplicas -= take;
        }
    }

    const success = remainingReplicas === 0;

    // --- Risk Analysis ---
    let dependencyRisk = 'low';
    let riskDescription = 'No major risks detected.';
    const missingDeps = [];

    if (dependencies && dependencies.length > 0) {
        dependencies.forEach(dep => {
            const depServiceId = dep.serviceId;
            const exists = services.some(s => {
                const sId = s.serviceId || `${s.namespace}:${s.name}`;
                return sId === depServiceId;
            });
            if (!exists) {
                missingDeps.push(depServiceId);
            }
        });

        if (missingDeps.length > 0) {
            dependencyRisk = 'high';
            riskDescription = `Missing dependencies in cluster: ${missingDeps.join(', ')}.`;
        } else if (dependencies.length > 3) {
            dependencyRisk = 'medium';
            riskDescription = 'High number of dependencies increases complexity.';
        } else {
            riskDescription = 'All dependencies verified in current graph.';
        }
    } else {
        riskDescription = 'No dependencies declared.';
    }

    // 5. Build Result
    const recommendations = [];
    if (success) {
        recommendations.push({
            type: 'placement',
            priority: 'high',
            description: `Place ${replicas} replicas across ${distribution.length} nodes: ${distribution.map(d => `${d.replicas} on ${d.node}`).join(', ')}.`
        });
    } else {
        recommendations.push({
            type: 'scaling',
            priority: 'critical',
            description: `Insufficient capacity. Can only place ${replicas - remainingReplicas} replicas. Add nodes or reduce request.`
        });
    }

    return {
        targetServiceName: serviceName,
        success,
        confidence: 'high',
        explanation: success
            ? `Successfully found placement for all replicas.`
            : `Failed to find placement for all replicas. Capacity limited to ${totalCapacityPods} pods.`,
        totalCapacityPods,
        suitableNodes: scoredNodes, // Matches frontend expectation
        riskAnalysis: {
            dependencyRisk,
            description: riskDescription
        },
        recommendations,
        // Keep old fields just in case? Or cleaner to remove?
        // Let's keep nodeAnalysis as just the array of capacities if needed, but 'suitableNodes' has it all.
        // openapi spec needs update to match this structure.
        recommendation: { // For backward compat with my previous change/spec
            serviceName,
            cpuRequest,
            ramRequest,
            distribution
        }
    };
}

module.exports = { simulateAdd };
