/**
 * Health Command
 * 
 * Check the health of the Predictive Analysis Engine.
 * 
 * Usage:
 *   predict health [--json]
 */

const { get } = require('../client');
const { formatHealth } = require('../formatters');
const { EXIT_CODES } = require('../utils/exitCodes');

/**
 * Execute health check command
 * @param {object} options - Command options
 * @param {boolean} [options.json] - Output as JSON
 */
async function healthCommand(options = {}) {
    try {
        const data = await get('/health');
        
        if (options.json) {
            console.log(JSON.stringify(data, null, 2));
        } else {
            console.log(formatHealth(data));
        }
        
        // Exit with appropriate code based on status
        if (data.status === 'ok') {
            process.exit(EXIT_CODES.SUCCESS);
        } else {
            // Degraded but reachable
            process.exit(EXIT_CODES.SUCCESS);
        }
    } catch (error) {
        if (options.json) {
            console.error(JSON.stringify({ error: error.message }, null, 2));
        } else {
            console.error(`Error: ${error.message}`);
        }
        process.exit(error.exitCode || EXIT_CODES.UNEXPECTED);
    }
}

module.exports = { healthCommand };
