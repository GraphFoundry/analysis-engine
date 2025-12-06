/**
 * CLI Input Validators
 * 
 * Validation functions for CLI arguments.
 * These validate user input BEFORE making HTTP requests.
 */

const { EXIT_CODES } = require('./exitCodes');

/**
 * Parse and validate serviceId format (namespace:name)
 * @param {string} serviceId - Service identifier
 * @returns {{ namespace: string, name: string }} Parsed service ID
 * @throws {Error} If format is invalid
 */
function parseServiceId(serviceId) {
    if (!serviceId || typeof serviceId !== 'string') {
        const err = new Error('serviceId is required');
        err.exitCode = EXIT_CODES.VALIDATION_ERROR;
        throw err;
    }
    
    const parts = serviceId.split(':');
    if (parts.length !== 2 || !parts[0] || !parts[1]) {
        const err = new Error('serviceId must be in format "namespace:name" (e.g., "default:cartservice")');
        err.exitCode = EXIT_CODES.VALIDATION_ERROR;
        throw err;
    }
    
    return { namespace: parts[0], name: parts[1] };
}

/**
 * Validate mutual exclusion: serviceId XOR (name + namespace)
 * @param {object} options - CLI options
 * @returns {{ serviceId?: string, name?: string, namespace?: string }} Validated identifier
 * @throws {Error} If validation fails
 */
function validateServiceIdentifier(options) {
    const hasServiceId = !!options.serviceId;
    const hasNamespace = !!options.namespace;
    const hasName = !!options.name;
    
    // Must provide serviceId OR (name + namespace), not both, not neither
    if (hasServiceId && (hasNamespace || hasName)) {
        const err = new Error('Cannot use --serviceId together with --name/--namespace. Use one or the other.');
        err.exitCode = EXIT_CODES.VALIDATION_ERROR;
        throw err;
    }
    
    if (!hasServiceId && !hasNamespace && !hasName) {
        const err = new Error('Must provide either --serviceId or both --name and --namespace');
        err.exitCode = EXIT_CODES.VALIDATION_ERROR;
        throw err;
    }
    
    if (hasNamespace !== hasName) {
        const err = new Error('--name and --namespace must be used together');
        err.exitCode = EXIT_CODES.VALIDATION_ERROR;
        throw err;
    }
    
    if (hasServiceId) {
        // Validate format
        parseServiceId(options.serviceId);
        return { serviceId: options.serviceId };
    }
    
    return { name: options.name, namespace: options.namespace };
}

/**
 * Validate positive integer
 * @param {string|number} value - Value to validate
 * @param {string} name - Parameter name for error messages
 * @param {object} [opts] - Options
 * @param {number} [opts.min] - Minimum value (inclusive)
 * @param {number} [opts.max] - Maximum value (inclusive)
 * @returns {number} Validated integer
 * @throws {Error} If validation fails
 */
function validatePositiveInt(value, name, opts = {}) {
    const num = parseInt(value, 10);
    
    if (isNaN(num)) {
        const err = new Error(`${name} must be a valid integer`);
        err.exitCode = EXIT_CODES.VALIDATION_ERROR;
        throw err;
    }
    
    if (num <= 0) {
        const err = new Error(`${name} must be a positive integer (got ${num})`);
        err.exitCode = EXIT_CODES.VALIDATION_ERROR;
        throw err;
    }
    
    if (opts.min !== undefined && num < opts.min) {
        const err = new Error(`${name} must be at least ${opts.min} (got ${num})`);
        err.exitCode = EXIT_CODES.VALIDATION_ERROR;
        throw err;
    }
    
    if (opts.max !== undefined && num > opts.max) {
        const err = new Error(`${name} must be at most ${opts.max} (got ${num})`);
        err.exitCode = EXIT_CODES.VALIDATION_ERROR;
        throw err;
    }
    
    return num;
}

/**
 * Validate float in range
 * @param {string|number} value - Value to validate
 * @param {string} name - Parameter name for error messages
 * @param {number} min - Minimum value (inclusive)
 * @param {number} max - Maximum value (inclusive)
 * @returns {number} Validated float
 * @throws {Error} If validation fails
 */
function validateFloatInRange(value, name, min, max) {
    const num = parseFloat(value);
    
    if (isNaN(num)) {
        const err = new Error(`${name} must be a valid number`);
        err.exitCode = EXIT_CODES.VALIDATION_ERROR;
        throw err;
    }
    
    if (num < min || num > max) {
        const err = new Error(`${name} must be between ${min} and ${max} (got ${num})`);
        err.exitCode = EXIT_CODES.VALIDATION_ERROR;
        throw err;
    }
    
    return num;
}

/**
 * Validate latency metric
 * @param {string} metric - Metric name
 * @returns {string} Validated metric
 * @throws {Error} If invalid
 */
function validateLatencyMetric(metric) {
    const valid = ['p50', 'p95', 'p99'];
    if (!valid.includes(metric)) {
        const err = new Error(`latencyMetric must be one of: ${valid.join(', ')} (got "${metric}")`);
        err.exitCode = EXIT_CODES.VALIDATION_ERROR;
        throw err;
    }
    return metric;
}

/**
 * Validate scaling model
 * @param {string} model - Model name
 * @returns {string} Validated model
 * @throws {Error} If invalid
 */
function validateScalingModel(model) {
    const valid = ['linear', 'bounded_sqrt', 'log'];
    if (!valid.includes(model)) {
        const err = new Error(`model must be one of: ${valid.join(', ')} (got "${model}")`);
        err.exitCode = EXIT_CODES.VALIDATION_ERROR;
        throw err;
    }
    return model;
}

/**
 * Validate risk metric
 * @param {string} metric - Metric name
 * @returns {string} Validated metric
 * @throws {Error} If invalid
 */
function validateRiskMetric(metric) {
    const valid = ['pagerank', 'betweenness'];
    if (!valid.includes(metric)) {
        const err = new Error(`metric must be one of: ${valid.join(', ')} (got "${metric}")`);
        err.exitCode = EXIT_CODES.VALIDATION_ERROR;
        throw err;
    }
    return metric;
}

module.exports = {
    parseServiceId,
    validateServiceIdentifier,
    validatePositiveInt,
    validateFloatInRange,
    validateLatencyMetric,
    validateScalingModel,
    validateRiskMetric
};
