/**
 * Simulate Failure Command
 * 
 * Simulate a service failure and analyze impact.
 * 
 * Usage:
 *   predict simulate-failure --serviceId <namespace:name> [--maxDepth <n>] [--json]
 *   predict simulate-failure --name <name> --namespace <namespace> [--maxDepth <n>] [--json]
 */

const { post } = require('../client');
const { formatFailureSimulation, formatError } = require('../formatters');
const { EXIT_CODES } = require('../utils/exitCodes');
const { validateServiceIdentifier, validatePositiveInt } = require('../utils/validators');

/**
 * Execute failure simulation command
 * @param {object} options - Command options
 * @param {string} [options.serviceId] - Service ID (namespace:name)
 * @param {string} [options.name] - Service name
 * @param {string} [options.namespace] - Service namespace
 * @param {number} [options.maxDepth] - Maximum traversal depth
 * @param {boolean} [options.json] - Output as JSON
 */
async function simulateFailureCommand(options = {}) {
    try {
        // Validate service identifier (mutual exclusion)
        const identifier = validateServiceIdentifier(options);
        
        // Build request body
        const body = { ...identifier };
        
        // Validate and add maxDepth if provided
        if (options.maxDepth !== undefined) {
            body.maxDepth = validatePositiveInt(options.maxDepth, 'maxDepth', { min: 1, max: 10 });
        }
        
        // Make API request
        const data = await post('/simulate/failure', body);
        
        // Output result
        if (options.json) {
            console.log(JSON.stringify(data, null, 2));
        } else {
            console.log(formatFailureSimulation(data));
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

module.exports = { simulateFailureCommand };
