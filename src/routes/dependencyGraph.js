const express = require('express');
const { getMetricsSnapshot, checkGraphHealth, getCentralityScores } = require('../clients/graphEngineClient');

const router = express.Router();

/**
 * GET /api/dependency-graph/snapshot
 * Returns enriched graph snapshot with node and edge telemetry
 * 
 * Query params:
 * - range: time range (e.g., "1h", "5m") - currently informational only
 * - namespace: filter by namespace (optional)
 * 
 * Response shape designed for Incident Explorer UI
 */
router.get('/snapshot', async (req, res) => {
    try {
        const { namespace } = req.query;

        // Fetch snapshot, health and centrality in parallel
        const [snapshotResult, healthResult, centralityResult] = await Promise.all([
            getMetricsSnapshot(),
            checkGraphHealth(),
            getCentralityScores()
        ]);

        // Extract freshness info
        let stale = true;
        let lastUpdatedSecondsAgo = null;
        let windowMinutes = 5;

        if (healthResult.ok && healthResult.data) {
            stale = healthResult.data.stale ?? true;
            lastUpdatedSecondsAgo = healthResult.data.lastUpdatedSecondsAgo ?? null;
            windowMinutes = healthResult.data.windowMinutes ?? 5;
        }

        // Handle snapshot fetch failure
        if (!snapshotResult.ok) {
            return res.status(503).json({
                error: snapshotResult.error || 'Failed to fetch graph snapshot from Graph Engine',
                nodes: [],
                edges: [],
                metadata: {
                    stale: true,
                    lastUpdatedSecondsAgo: null,
                    windowMinutes
                }
            });
        }

        const rawServices = snapshotResult.data?.services || [];
        const rawEdges = snapshotResult.data?.edges || [];

        // Build service name -> namespace map AND metrics map
        const serviceMap = new Map();
        const metricsMap = new Map(); // Key: service name, Value: metrics

        rawServices.forEach(svc => {
            const ns = svc.namespace || 'default';
            serviceMap.set(svc.name, ns);

            // Extract metrics from service object
            // Graph Engine returns: { name, namespace, rps, errorRate, p95, podCount, availability }
            metricsMap.set(svc.name, {
                requestRate: svc.rps || 0,
                errorRate: svc.errorRate ? svc.errorRate * 100 : 0, // Convert to percentage
                p95: svc.p95 || 0,
                podCount: svc.podCount ?? 0,
                availability: svc.availability !== undefined ? svc.availability * 100 : null // Convert 0-1 to percentage
            });
        });

        // Build centrality map
        const centralityMap = new Map();
        if (centralityResult.ok && centralityResult.data?.scores) {
            centralityResult.data.scores.forEach(s => {
                centralityMap.set(s.service, s);
            });
        }

        // Enrich nodes with telemetry
        const nodes = rawServices
            .filter(svc => !namespace || svc.namespace === namespace)
            .map(svc => {
                const ns = svc.namespace || 'default';
                const nodeId = `${ns}:${svc.name}`;
                const metrics = metricsMap.get(svc.name) || {};

                // Calculate risk level based on metrics
                const riskLevel = calculateRiskLevel(metrics);
                const riskReason = getRiskReason(metrics);

                return {
                    id: nodeId,
                    name: svc.name,
                    namespace: ns,
                    riskLevel,
                    riskReason,
                    // Aggregated telemetry (optional if unavailable)
                    reqRate: metrics.requestRate ?? undefined,
                    errorRatePct: metrics.errorRate ?? undefined,
                    latencyP95Ms: metrics.p95 ?? undefined,
                    availabilityPct: metrics.availability ?? undefined,
                    podCount: metrics.podCount ?? undefined,
                    availability: svc.availability ?? undefined, // 0-1 score from Graph Engine
                    pageRank: centralityMap.get(svc.name)?.pagerank,
                    betweenness: centralityMap.get(svc.name)?.betweenness,
                    updatedAt: new Date().toISOString()
                };
            });

        // Enrich edges with telemetry (if available from Graph Engine)
        const edges = rawEdges
            .map(e => {
                const fromNs = serviceMap.get(e.from) || 'default';
                const toNs = e.namespace || 'default';
                const edgeId = `${fromNs}:${e.from}->${toNs}:${e.to}`;

                return {
                    id: edgeId,
                    source: `${fromNs}:${e.from}`,
                    target: `${toNs}:${e.to}`,
                    // Edge telemetry is at the root of the edge object in service-graph-engine
                    reqRate: e.rps ?? undefined,
                    errorRatePct: e.errorRate ? e.errorRate * 100 : undefined, // Convert 0-1 to %
                    latencyP95Ms: e.p95 ?? undefined
                };
            });

        // Count nodes and edges with metrics (for debugging)
        const nodesWithMetrics = nodes.filter(n =>
            n.reqRate !== undefined || n.errorRatePct !== undefined || n.latencyP95Ms !== undefined
        ).length;
        const edgesWithMetrics = edges.filter(e =>
            e.reqRate !== undefined || e.errorRatePct !== undefined || e.latencyP95Ms !== undefined
        ).length;

        res.json({
            nodes,
            edges,
            metadata: {
                stale,
                lastUpdatedSecondsAgo,
                windowMinutes,
                nodeCount: nodes.length,
                edgeCount: edges.length,
                nodesWithMetrics,
                edgesWithMetrics,
                generatedAt: new Date().toISOString()
            }
        });

    } catch (error) {
        console.error('Error fetching dependency graph snapshot:', error);
        res.status(503).json({
            error: error.message || 'Graph Engine unreachable',
            nodes: [],
            edges: [],
            metadata: {
                stale: true,
                lastUpdatedSecondsAgo: null,
                windowMinutes: 5
            }
        });
    }
});

/**
 * Calculate availability percentage from error rate
 * @param {number} errorRate - Error rate as decimal (e.g., 0.002 = 0.2%)
 * @returns {number} - Availability percentage
 */
function calculateAvailability(errorRate) {
    if (typeof errorRate !== 'number') return 100;
    return errorRate > 0 ? (1 - errorRate) * 100 : 100;
}

/**
 * Calculate risk level based on telemetry metrics
 * @param {object} metrics - Service metrics
 * @returns {string} - "CRITICAL" | "HIGH" | "MEDIUM" | "LOW" | "UNKNOWN"
 */
function calculateRiskLevel(metrics) {
    if (!metrics || Object.keys(metrics).length === 0) {
        return 'UNKNOWN';
    }

    const { errorRate, availability, p95, podCount } = metrics;

    // Critical conditions
    if (podCount === 0) return 'CRITICAL';
    if (availability !== null && availability !== undefined && availability < 50) return 'CRITICAL';

    // High risk conditions
    if (errorRate > 5) return 'HIGH';
    if (availability !== null && availability !== undefined && availability < 95) return 'HIGH';
    if (p95 > 1000) return 'HIGH';

    // Medium risk conditions
    if (errorRate > 1) return 'MEDIUM';
    if (availability !== null && availability !== undefined && availability < 99) return 'MEDIUM';
    if (p95 > 500) return 'MEDIUM';

    // No metrics available
    if (availability === null || availability === undefined) return 'UNKNOWN';

    return 'LOW';
}

/**
 * Get human-readable risk reason
 * @param {object} metrics - Service metrics
 * @returns {string} - Risk reason
 */
function getRiskReason(metrics) {
    if (!metrics || Object.keys(metrics).length === 0) {
        return 'No recent metrics available';
    }

    const { errorRate, availability, p95, podCount } = metrics;

    if (podCount === 0) return 'No pods running';
    if (availability !== null && availability !== undefined && availability < 50) return `Critical availability (${availability.toFixed(1)}%)`;
    if (errorRate > 5) return `High error rate (${errorRate.toFixed(2)}%)`;
    if (availability !== null && availability !== undefined && availability < 95) return `Low availability (${availability.toFixed(1)}%)`;
    if (p95 > 1000) return `P95 latency spike (${p95.toFixed(0)}ms)`;
    if (errorRate > 1) return `Elevated error rate (${errorRate.toFixed(2)}%)`;
    if (availability !== null && availability !== undefined && availability < 99) return `Availability degraded (${availability.toFixed(1)}%)`;
    if (p95 > 500) return `Slow responses (${p95.toFixed(0)}ms)`;

    if (availability === null || availability === undefined) return 'No traffic metrics';

    return 'Operating normally';
}

module.exports = router;
