/**
 * Risk Analysis Module
 * 
 * Provides centrality-based risk scoring for services.
 * Higher centrality = higher risk if the service fails.
 */

const { getCentralityTop, checkGraphHealth } = require('../clients/graphEngineClient');

/**
 * Risk level thresholds based on centrality score percentile
 */
const RISK_THRESHOLDS = {
    high: 0.2,    // Top 20% centrality
    medium: 0.1,  // 10-20% centrality
    low: 0        // Below 10%
};

/**
 * Determine risk level based on centrality score and rank
 * @param {number} score - Centrality score
 * @param {number} rank - Rank in the list (0-indexed)
 * @param {number} total - Total number of services returned
 * @returns {string} - Risk level (high, medium, low)
 */
function determineRiskLevel(score, rank, total) {
    // Handle edge case: empty list or zero total
    if (total === 0) {
        return 'low';
    }
    
    // Top 20% of returned services = high risk
    const percentile = rank / total;
    
    if (score > 0 && percentile < 0.2) {
        return 'high';
    } else if (score > 0 && percentile < 0.5) {
        return 'medium';
    }
    return 'low';
}

/**
 * Generate explanation for risk level
 * @param {string} serviceName - Service name
 * @param {string} metric - Centrality metric used
 * @param {number} score - Centrality score
 * @param {string} riskLevel - Determined risk level
 * @returns {string}
 */
function generateExplanation(serviceName, metric, score, riskLevel) {
    const metricLabel = metric === 'pagerank' ? 'PageRank' : 'betweenness centrality';
    
    if (riskLevel === 'high') {
        return `${serviceName} has high ${metricLabel} (${score.toFixed(4)}), indicating it is a critical hub. Failure could cascade widely.`;
    } else if (riskLevel === 'medium') {
        return `${serviceName} has moderate ${metricLabel} (${score.toFixed(4)}). Monitor for dependencies.`;
    }
    return `${serviceName} has low ${metricLabel} (${score.toFixed(4)}). Lower risk of cascade.`;
}

/**
 * @typedef {Object} RiskService
 * @property {string} serviceId - Canonical service ID (namespace:name)
 * @property {string} name - Service name
 * @property {string} namespace - Service namespace
 * @property {number} centralityScore - Raw centrality score
 * @property {string} riskLevel - Derived risk level (high, medium, low)
 * @property {string} explanation - Human-readable explanation
 */

/**
 * @typedef {Object} RiskAnalysisResult
 * @property {string} metric - Centrality metric used
 * @property {RiskService[]} services - Services ranked by risk
 * @property {Object} dataFreshness - Data freshness info
 * @property {string} confidence - Confidence level (high, low)
 */

/**
 * Get top risk services based on centrality
 * @param {Object} options
 * @param {string} [options.metric='pagerank'] - Centrality metric
 * @param {number} [options.limit=5] - Number of services to return
 * @returns {Promise<RiskAnalysisResult>}
 */
async function getTopRiskServices({ metric = 'pagerank', limit = 5 } = {}) {
    // Validate metric
    const validMetrics = ['pagerank', 'betweenness'];
    if (!validMetrics.includes(metric)) {
        throw new Error(`Invalid metric: ${metric}. Allowed: ${validMetrics.join(', ')}`);
    }

    // Fetch centrality data
    const centralityResult = await getCentralityTop(metric, limit);
    
    if (!centralityResult.ok) {
        throw new Error(`Failed to fetch centrality data: ${centralityResult.error}`);
    }

    // Fetch freshness data
    const healthResult = await checkGraphHealth();
    
    let dataFreshness = null;
    let confidence = 'unknown';
    
    if (healthResult.ok) {
        dataFreshness = {
            source: 'graph-engine',
            stale: healthResult.data.stale,
            lastUpdatedSecondsAgo: healthResult.data.lastUpdatedSecondsAgo,
            windowMinutes: healthResult.data.windowMinutes
        };
        confidence = healthResult.data.stale ? 'low' : 'high';
    }

    // Transform centrality data to risk services
    const topServices = centralityResult.data.top || [];
    const total = topServices.length;

    const services = topServices.map((item, rank) => {
        const rawServiceName = item.service;
        const score = item.value || 0;
        const riskLevel = determineRiskLevel(score, rank, total);
        
        // Parse namespace:name format if present, else default to "default" namespace
        const { serviceId, name, namespace } = parseServiceIdentifier(rawServiceName);
        
        return {
            serviceId,
            name,
            namespace,
            centralityScore: score,
            riskLevel,
            explanation: generateExplanation(name, metric, score, riskLevel)
        };
    });

    return {
        metric,
        services,
        dataFreshness,
        confidence
    };
}

/**
 * Parse service identifier - supports "namespace:name" format or plain name
 * @param {string} rawServiceName - Service name from Graph API
 * @returns {{serviceId: string, name: string, namespace: string}}
 */
function parseServiceIdentifier(rawServiceName) {
    if (rawServiceName.includes(':')) {
        const colonIndex = rawServiceName.indexOf(':');
        const namespace = rawServiceName.substring(0, colonIndex);
        const name = rawServiceName.substring(colonIndex + 1);
        return { serviceId: rawServiceName, name, namespace };
    }
    return { 
        serviceId: `default:${rawServiceName}`, 
        name: rawServiceName, 
        namespace: 'default' 
    };
}

module.exports = {
    getTopRiskServices,
    // Exported for testing
    _test: {
        determineRiskLevel,
        generateExplanation,
        parseServiceIdentifier,
        RISK_THRESHOLDS
    }
};
