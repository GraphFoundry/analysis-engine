/**
 * Risk Top Command
 * 
 * Get top services by risk score based on centrality metrics.
 * 
 * Usage:
 *   predict risk-top [--metric <pagerank|betweenness>] [--limit <1-20>] [--json]
 */

const { get } = require('../client');
const { formatRiskTop, formatError } = require('../formatters');
const { EXIT_CODES } = require('../utils/exitCodes');
const { validatePositiveInt, validateRiskMetric } = require('../utils/validators');

/**
 * Execute risk-top command
 * @param {object} options - Command options
 * @param {string} [options.metric] - Risk metric (pagerank or betweenness)
 * @param {number} [options.limit] - Number of services to return (1-20)
 * @param {boolean} [options.json] - Output as JSON
 */
async function riskTopCommand(options = {}) {
    try {
        // Build query params
        const query = {};
        
        if (options.metric !== undefined) {
            query.metric = validateRiskMetric(options.metric);
        }
        
        if (options.limit !== undefined) {
            query.limit = validatePositiveInt(options.limit, 'limit', { min: 1, max: 20 });
        }
        
        // Make API request
        const data = await get('/risk/services/top', query);
        
        // Output result
        if (options.json) {
            console.log(JSON.stringify(data, null, 2));
        } else {
            console.log(formatRiskTop(data));
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

module.exports = { riskTopCommand };
