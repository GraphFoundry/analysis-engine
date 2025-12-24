const express = require('express');
const { getMetricsSnapshot, checkGraphHealth } = require('../clients/graphEngineClient');

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

        // Fetch snapshot and health in parallel
        const [snapshotResult, healthResult] = await Promise.all([
            getMetricsSnapshot(),
            checkGraphHealth()
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
        const rawMetrics = snapshotResult.data?.metrics || {};

        // Build service name -> namespace map
        const serviceMap = new Map();
        rawServices.forEach(svc => {
            const ns = svc.namespace || 'default';
            serviceMap.set(svc.name, ns);
        });

        // Enrich nodes with telemetry
        const nodes = rawServices
            .filter(svc => !namespace || svc.namespace === namespace)
            .map(svc => {
                const ns = svc.namespace || 'default';
                const nodeId = `${ns}:${svc.name}`;
                const metrics = rawMetrics[svc.name] || {};

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
                    updatedAt: new Date().toISOString()
                };
            });

        // Enrich edges with telemetry (if available from Graph Engine)
        const edges = rawEdges
            .map(e => {
                const fromNs = serviceMap.get(e.from) || 'default';
                const toNs = e.namespace || 'default';
                const edgeId = `${fromNs}:${e.from}->${toNs}:${e.to}`;

                // Edge metrics (if Graph Engine provides them)
                const edgeMetrics = e.metrics || {};

                return {
                    id: edgeId,
                    source: `${fromNs}:${e.from}`,
                    target: `${toNs}:${e.to}`,
                    // Optional edge telemetry
                    reqRate: edgeMetrics.requestRate ?? undefined,
                    errorRatePct: edgeMetrics.errorRate ?? undefined,
                    latencyP95Ms: edgeMetrics.p95 ?? undefined
                };
            });

        res.json({
            nodes,
            edges,
            metadata: {
                stale,
                lastUpdatedSecondsAgo,
                windowMinutes,
                nodeCount: nodes.length,
                edgeCount: edges.length,
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
 * Calculate risk level based on telemetry metrics
 * @param {object} metrics - Service metrics
 * @returns {string} - "CRITICAL" | "HIGH" | "MEDIUM" | "LOW" | "UNKNOWN"
 */
function calculateRiskLevel(metrics) {
    if (!metrics || Object.keys(metrics).length === 0) {
        return 'UNKNOWN';
    }

    const { errorRate, availability, p95 } = metrics;

    // High risk conditions
    if (errorRate > 5) return 'HIGH';
    if (availability < 95) return 'CRITICAL';
    if (p95 > 1000) return 'HIGH';

    // Medium risk conditions
    if (errorRate > 1) return 'MEDIUM';
    if (availability < 99) return 'MEDIUM';
    if (p95 > 500) return 'MEDIUM';

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

    const { errorRate, availability, p95 } = metrics;

    if (errorRate > 5) return `High error rate (${errorRate.toFixed(2)}%)`;
    if (availability < 95) return `Low availability (${availability.toFixed(2)}%)`;
    if (p95 > 1000) return `P95 latency spike (${p95.toFixed(0)}ms)`;
    if (errorRate > 1) return `Elevated error rate (${errorRate.toFixed(2)}%)`;
    if (availability < 99) return `Availability degraded (${availability.toFixed(2)}%)`;
    if (p95 > 500) return `Slow responses (${p95.toFixed(0)}ms)`;

    return 'Operating normally';
}

module.exports = router;
