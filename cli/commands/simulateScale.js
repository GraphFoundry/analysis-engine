/**
 * Simulate Scale Command
 * 
 * Simulate scaling a service and predict latency impact.
 * 
 * Usage:
 *   predict simulate-scale --serviceId <namespace:name> --currentPods <n> --newPods <n> [options]
 * 
 * Options:
 *   --latencyMetric <p50|p95|p99>  Latency percentile (default: p95)
 *   --model <linear|bounded_sqrt|log>  Scaling model (default: bounded_sqrt)
 *   --alpha <0-1>                  Model alpha parameter (default: 0.5)
 *   --maxDepth <n>                 Max traversal depth
 *   --json                         Output as JSON
 */

const { post } = require('../client');
const { formatScalingSimulation, formatError } = require('../formatters');
const { EXIT_CODES } = require('../utils/exitCodes');
const {
    validateServiceIdentifier,
    validatePositiveInt,
    validateFloatInRange,
    validateLatencyMetric,
    validateScalingModel
} = require('../utils/validators');

/**
 * Execute scaling simulation command
 * @param {object} options - Command options
 * @param {string} [options.serviceId] - Service ID (namespace:name)
 * @param {string} [options.name] - Service name
 * @param {string} [options.namespace] - Service namespace
 * @param {number} options.currentPods - Current pod count
 * @param {number} options.newPods - Target pod count
 * @param {string} [options.latencyMetric] - Latency percentile (p50, p95, p99)
 * @param {string} [options.model] - Scaling model type
 * @param {number} [options.alpha] - Model alpha parameter
 * @param {number} [options.maxDepth] - Maximum traversal depth
 * @param {boolean} [options.json] - Output as JSON
 */
async function simulateScaleCommand(options = {}) {
    try {
        // Validate service identifier (mutual exclusion)
        const identifier = validateServiceIdentifier(options);
        
        // Validate required params
        if (options.currentPods === undefined) {
            const err = new Error('--currentPods is required');
            err.exitCode = EXIT_CODES.VALIDATION_ERROR;
            throw err;
        }
        if (options.newPods === undefined) {
            const err = new Error('--newPods is required');
            err.exitCode = EXIT_CODES.VALIDATION_ERROR;
            throw err;
        }
        
        const currentPods = validatePositiveInt(options.currentPods, 'currentPods', { min: 1 });
        const newPods = validatePositiveInt(options.newPods, 'newPods', { min: 1 });
        
        // Build request body
        const body = {
            ...identifier,
            currentPods,
            newPods
        };
        
        // Add optional params
        if (options.latencyMetric !== undefined) {
            body.latencyMetric = validateLatencyMetric(options.latencyMetric);
        }
        
        if (options.maxDepth !== undefined) {
            body.maxDepth = validatePositiveInt(options.maxDepth, 'maxDepth', { min: 1, max: 10 });
        }
        
        // Build model object if model params provided
        if (options.model !== undefined || options.alpha !== undefined) {
            body.model = {};
            
            if (options.model !== undefined) {
                body.model.type = validateScalingModel(options.model);
            }
            
            if (options.alpha !== undefined) {
                body.model.alpha = validateFloatInRange(options.alpha, 'alpha', 0, 1);
            }
        }
        
        // Make API request
        const data = await post('/simulate/scale', body);
        
        // Output result
        if (options.json) {
            console.log(JSON.stringify(data, null, 2));
        } else {
            console.log(formatScalingSimulation(data));
        }
        
        process.exit(EXIT_CODES.SUCCESS);
    } catch (error) {
        if (options.json) {
            console.error(JSON.stringify({ error: error.message }, null, 2));
        } else {
            console.error(formatError(error));
        }
        process.exit(error.exitCode || EXIT_CODES.UNEXPECTED);
    }
}

module.exports = { simulateScaleCommand };
