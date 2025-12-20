/**
 * Recommendation Engine
 * 
 * Generates actionable recommendations based on simulation results.
 * Rules are confidence-aware and threshold-based.
 */

/**
 * Traffic loss thresholds for recommendations
 */
const TRAFFIC_THRESHOLDS = {
    critical: 100,   // RPS - very high impact
    high: 50,        // RPS - significant impact
    medium: 10       // RPS - moderate impact
};

/**
 * Latency change thresholds for recommendations (ms)
 */
const LATENCY_THRESHOLDS = {
    significant: 50,   // ms - very noticeable
    moderate: 20,      // ms - somewhat noticeable
    minor: 5           // ms - barely noticeable
};

/**
 * @typedef {Object} Recommendation
 * @property {string} type - Recommendation type (circuit-breaker, redundancy, scaling, monitoring, etc.)
 * @property {string} priority - Priority level (critical, high, medium, low)
 * @property {string} target - Target service or component
 * @property {string} reason - Why this recommendation is made
 * @property {string} action - Suggested action
 */

/**
 * Generate recommendations for failure simulation results
 * 
 * @param {Object} result - Failure simulation result
 * @returns {Recommendation[]}
 */
function generateFailureRecommendations(result) {
    const recommendations = [];
    const confidence = result.confidence || 'unknown';
    
    // Add confidence warning if data is stale
    if (confidence === 'low') {
        recommendations.push({
            type: 'data-quality',
            priority: 'high',
            target: 'graph-data',
            reason: 'Graph data is stale (>5 minutes old)',
            action: 'Verify graph-engine is syncing properly before acting on predictions'
        });
    }

    const totalLost = result.totalLostTrafficRps || 0;
    const affectedCallers = result.affectedCallers || [];
    const unreachableServices = result.unreachableServices || [];
    const affectedDownstream = result.affectedDownstream || [];
    const targetName = result.target?.name || 'unknown';

    // Critical impact - high traffic loss
    if (totalLost >= TRAFFIC_THRESHOLDS.critical) {
        recommendations.push({
            type: 'circuit-breaker',
            priority: 'critical',
            target: targetName,
            reason: `Failure would cause ${totalLost.toFixed(1)} RPS total traffic loss`,
            action: `Implement circuit breaker with fallback for all callers of ${targetName}`
        });
    }

    // Multiple callers affected - need resilience
    if (affectedCallers.length >= 3) {
        const topCaller = affectedCallers[0];
        recommendations.push({
            type: 'redundancy',
            priority: 'high',
            target: targetName,
            reason: `${affectedCallers.length} upstream services depend on ${targetName}`,
            action: `Deploy ${targetName} across multiple availability zones`
        });
    }

    // High-traffic callers need circuit breakers
    for (const caller of affectedCallers) {
        if (caller.lostTrafficRps >= TRAFFIC_THRESHOLDS.high) {
            recommendations.push({
                type: 'circuit-breaker',
                priority: 'high',
                target: caller.name || caller.serviceId,
                reason: `${caller.name || caller.serviceId} would lose ${caller.lostTrafficRps.toFixed(1)} RPS`,
                action: `Add circuit breaker in ${caller.name} when calling ${targetName}`
            });
        }
    }

    // Unreachable services - cascading failure risk
    if (unreachableServices.length > 0) {
        const totalUnreachableLoss = unreachableServices.reduce(
            (sum, s) => sum + (s.lostTrafficRps || 0), 0
        );
        
        if (unreachableServices.length >= 2 || totalUnreachableLoss >= TRAFFIC_THRESHOLDS.medium) {
            recommendations.push({
                type: 'topology-review',
                priority: 'medium',
                target: targetName,
                reason: `${unreachableServices.length} service(s) become unreachable (cascade risk)`,
                action: `Review dependency graph; consider alternative paths for: ${unreachableServices.slice(0, 3).map(s => s.name).join(', ')}`
            });
        }
    }

    // Downstream impact
    if (affectedDownstream.length > 0) {
        const totalDownstreamLoss = affectedDownstream.reduce(
            (sum, s) => sum + (s.lostTrafficRps || 0), 0
        );
        
        if (totalDownstreamLoss >= TRAFFIC_THRESHOLDS.medium) {
            recommendations.push({
                type: 'graceful-degradation',
                priority: 'medium',
                target: targetName,
                reason: `Downstream services lose ${totalDownstreamLoss.toFixed(1)} RPS from ${targetName}`,
                action: `Implement graceful degradation in ${targetName} to reduce downstream blast radius`
            });
        }
    }

    // No significant impact - still recommend monitoring
    if (recommendations.length === 0 || 
        (recommendations.length === 1 && recommendations[0].type === 'data-quality')) {
        recommendations.push({
            type: 'monitoring',
            priority: 'low',
            target: targetName,
            reason: 'Low predicted impact, but failures can still occur',
            action: `Ensure alerting is configured for ${targetName} availability`
        });
    }

    return recommendations;
}

/**
 * Generate recommendations for scaling simulation results
 * 
 * @param {Object} result - Scaling simulation result
 * @returns {Recommendation[]}
 */
function generateScalingRecommendations(result) {
    const recommendations = [];
    const confidence = result.confidence || 'unknown';
    
    // Add confidence warning if data is stale
    if (confidence === 'low') {
        recommendations.push({
            type: 'data-quality',
            priority: 'high',
            target: 'graph-data',
            reason: 'Graph data is stale (>5 minutes old)',
            action: 'Verify graph-engine is syncing properly before acting on predictions'
        });
    }

    const targetName = result.target?.name || 'unknown';
    const latencyEstimate = result.latencyEstimate || {};
    const deltaMs = latencyEstimate.deltaMs;
    const currentPods = result.currentPods || 1;
    const newPods = result.newPods || 1;
    const scalingUp = newPods > currentPods;
    const affectedCallers = result.affectedCallers?.items || [];

    // Scaling down with negative delta (latency increase)
    if (!scalingUp && deltaMs !== null && deltaMs > 0) {
        if (deltaMs >= LATENCY_THRESHOLDS.significant) {
            recommendations.push({
                type: 'scaling-caution',
                priority: 'critical',
                target: targetName,
                reason: `Scaling down may increase latency by ${deltaMs.toFixed(1)}ms`,
                action: `Reconsider scaling ${targetName} from ${currentPods} to ${newPods} pods; latency increase is significant`
            });
        } else if (deltaMs >= LATENCY_THRESHOLDS.moderate) {
            recommendations.push({
                type: 'scaling-caution',
                priority: 'high',
                target: targetName,
                reason: `Scaling down may increase latency by ${deltaMs.toFixed(1)}ms`,
                action: `Monitor ${targetName} closely after scaling down; consider gradual rollout`
            });
        }
    }

    // Scaling up with improvement
    if (scalingUp && deltaMs !== null && deltaMs < 0) {
        const improvement = Math.abs(deltaMs);
        
        if (improvement >= LATENCY_THRESHOLDS.significant) {
            recommendations.push({
                type: 'scaling-benefit',
                priority: 'low',
                target: targetName,
                reason: `Scaling up reduces latency by ${improvement.toFixed(1)}ms`,
                action: `Scaling ${targetName} to ${newPods} pods is beneficial; consider permanent capacity increase`
            });
        }
    }

    // Minimal improvement from scaling up - cost consideration
    if (scalingUp && (deltaMs === null || Math.abs(deltaMs) < LATENCY_THRESHOLDS.minor)) {
        recommendations.push({
            type: 'cost-efficiency',
            priority: 'medium',
            target: targetName,
            reason: `Scaling from ${currentPods} to ${newPods} shows minimal latency benefit`,
            action: `Review if additional pods for ${targetName} are cost-effective; bottleneck may be elsewhere`
        });
    }

    // High-impact callers
    const callersWithSignificantImpact = affectedCallers.filter(
        c => c.deltaMs !== null && Math.abs(c.deltaMs) >= LATENCY_THRESHOLDS.moderate
    );

    if (callersWithSignificantImpact.length > 0) {
        const topCaller = callersWithSignificantImpact[0];
        recommendations.push({
            type: 'propagation-awareness',
            priority: 'medium',
            target: topCaller.name || topCaller.serviceId,
            reason: `${callersWithSignificantImpact.length} caller(s) see latency changes >= ${LATENCY_THRESHOLDS.moderate}ms`,
            action: `Inform teams owning upstream services (e.g., ${topCaller.name}) about expected latency changes`
        });
    }

    // No significant findings
    if (recommendations.length === 0 || 
        (recommendations.length === 1 && recommendations[0].type === 'data-quality')) {
        recommendations.push({
            type: 'proceed',
            priority: 'low',
            target: targetName,
            reason: 'No significant negative impact predicted',
            action: `Proceed with scaling ${targetName}; monitor for unexpected behavior`
        });
    }

    return recommendations;
}

module.exports = {
    generateFailureRecommendations,
    generateScalingRecommendations,
    // Exported for testing
    _test: {
        TRAFFIC_THRESHOLDS,
        LATENCY_THRESHOLDS
    }
};
