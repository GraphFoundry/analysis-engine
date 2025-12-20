/**
 * @typedef {Object} ServiceIdentifier
 * @property {string} serviceId - Resolved service ID
 */

/**
 * Parse and validate service identifier from request body
 * Accepts either serviceId OR (name + namespace)
 * 
 * @param {Object} body - Request body
 * @returns {ServiceIdentifier}
 * @throws {Error} If identifier is invalid or missing
 */
function parseServiceIdentifier(body) {
    if (body.serviceId) {
        // Validate format: "namespace:name"
        if (typeof body.serviceId !== 'string' || !body.serviceId.includes(':')) {
            throw new Error('serviceId must be in format "namespace:name"');
        }
        return { serviceId: body.serviceId };
    }
    
    if (body.name && body.namespace) {
        if (typeof body.name !== 'string' || typeof body.namespace !== 'string') {
            throw new Error('name and namespace must be strings');
        }
        return { serviceId: `${body.namespace}:${body.name}` };
    }
    
    throw new Error('Must provide either serviceId OR (name AND namespace)');
}

/**
 * Normalize pod parameter aliases (newPods, targetPods, pods)
 * Accepts aliases and returns canonical 'newPods' value
 * 
 * @param {Object} body - Request body
 * @returns {number} - Normalized newPods value
 * @throws {Error} If conflicting values provided or missing
 */
function normalizePodParams(body) {
    const candidates = [
        { key: 'newPods', value: body.newPods },
        { key: 'targetPods', value: body.targetPods },
        { key: 'pods', value: body.pods }
    ].filter(c => c.value !== undefined && c.value !== null);
    
    if (candidates.length === 0) {
        throw new Error('Must provide newPods (or alias: targetPods, pods)');
    }
    
    // Check for conflicting values
    const uniqueValues = [...new Set(candidates.map(c => c.value))];
    if (uniqueValues.length > 1) {
        const conflictDesc = candidates.map(c => `${c.key}=${c.value}`).join(', ');
        throw new Error(`Conflicting pod values provided: ${conflictDesc}`);
    }
    
    return candidates[0].value;
}

/**
 * Validate scaling parameters
 * 
 * @param {number} currentPods - Current pod count
 * @param {number} newPods - New pod count
 * @throws {Error} If parameters are invalid
 */
function validateScalingParams(currentPods, newPods) {
    if (!Number.isInteger(currentPods) || currentPods <= 0) {
        throw new Error(`currentPods must be a positive integer. Got: ${currentPods}`);
    }
    
    if (!Number.isInteger(newPods) || newPods <= 0) {
        throw new Error(`newPods must be a positive integer. Got: ${newPods}`);
    }
}

/**
 * Validate and normalize latency metric
 * 
 * @param {string|undefined} metric - Latency metric
 * @param {string} defaultMetric - Default metric to use
 * @returns {string} - Validated metric
 * @throws {Error} If metric is invalid
 */
function validateLatencyMetric(metric, defaultMetric = 'p95') {
    const validMetrics = ['p50', 'p95', 'p99'];
    
    if (!metric) {
        return defaultMetric;
    }
    
    if (!validMetrics.includes(metric)) {
        throw new Error(`latencyMetric must be one of: ${validMetrics.join(', ')}. Got: ${metric}`);
    }
    
    return metric;
}

/**
 * Validate traversal depth
 * 
 * @param {number|undefined} depth - Traversal depth
 * @param {number} defaultDepth - Default depth
 * @param {number} maxAllowed - Maximum allowed depth
 * @returns {number} - Validated depth
 * @throws {Error} If depth is invalid
 */
function validateDepth(depth, defaultDepth = 2, maxAllowed = 3) {
    if (depth === undefined || depth === null) {
        return defaultDepth;
    }
    
    if (!Number.isInteger(depth) || depth < 1 || depth > maxAllowed) {
        throw new Error(`maxDepth must be an integer between 1 and ${maxAllowed}. Got: ${depth}`);
    }
    
    return depth;
}

/**
 * Validate scaling model configuration
 * 
 * @param {Object|undefined} model - Scaling model config
 * @returns {Object} - Validated model config
 * @throws {Error} If model config is invalid
 */
function validateScalingModel(model) {
    if (!model) {
        return { type: 'bounded_sqrt', alpha: 0.5 };
    }
    
    const validTypes = ['bounded_sqrt', 'linear'];
    
    if (!validTypes.includes(model.type)) {
        throw new Error(`model.type must be one of: ${validTypes.join(', ')}. Got: ${model.type}`);
    }
    
    if (model.alpha !== undefined) {
        if (typeof model.alpha !== 'number' || model.alpha < 0 || model.alpha > 1) {
            throw new Error(`model.alpha must be a number between 0 and 1. Got: ${model.alpha}`);
        }
    }
    
    return {
        type: model.type,
        alpha: model.alpha ?? 0.5
    };
}

module.exports = {
    parseServiceIdentifier,
    normalizePodParams,
    validateScalingParams,
    validateLatencyMetric,
    validateDepth,
    validateScalingModel
};
