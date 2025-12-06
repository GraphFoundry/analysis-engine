#!/usr/bin/env node

/**
 * Predictive Analysis Engine CLI
 * 
 * A command-line interface for interacting with the Predictive Analysis Engine.
 * Makes HTTP requests to the API server.
 * 
 * Environment Variables:
 *   PREDICTIVE_ENGINE_URL - Base URL of the API (default: http://localhost:7000)
 * 
 * Exit Codes:
 *   0 - Success
 *   1 - Validation error (invalid arguments)
 *   2 - Server error (HTTP 4xx/5xx)
 *   3 - Network error (timeout, connection refused)
 *   4 - Unexpected error
 */

const { Command } = require('commander');
const { EXIT_CODES } = require('../cli/utils/exitCodes');

// Load package.json for version
let version = '1.0.0';
try {
    const pkg = require('../package.json');
    version = pkg.version || version;
} catch {
    // Ignore if package.json can't be loaded
}

const program = new Command();

program
    .name('predict')
    .description('CLI for the Predictive Analysis Engine')
    .version(version);

// Health command
program
    .command('health')
    .description('Check the health of the Predictive Analysis Engine')
    .option('--json', 'Output as JSON')
    .action(async (options) => {
        const { healthCommand } = require('../cli/commands/health');
        await healthCommand(options);
    });

// Simulate Failure command
program
    .command('simulate-failure')
    .description('Simulate a service failure and analyze impact')
    .option('-s, --serviceId <id>', 'Service ID in format namespace:name (e.g., default:cartservice)')
    .option('-n, --name <name>', 'Service name (use with --namespace)')
    .option('-N, --namespace <namespace>', 'Service namespace (use with --name)')
    .option('-d, --maxDepth <depth>', 'Maximum traversal depth (1-10)')
    .option('--json', 'Output as JSON')
    .action(async (options) => {
        const { simulateFailureCommand } = require('../cli/commands/simulateFailure');
        await simulateFailureCommand(options);
    });

// Simulate Scale command
program
    .command('simulate-scale')
    .description('Simulate scaling a service and predict latency impact')
    .requiredOption('-s, --serviceId <id>', 'Service ID in format namespace:name')
    .requiredOption('-c, --currentPods <count>', 'Current number of pods')
    .requiredOption('-p, --newPods <count>', 'Target number of pods')
    .option('-l, --latencyMetric <metric>', 'Latency percentile: p50, p95, p99 (default: p95)')
    .option('-m, --model <type>', 'Scaling model: linear, bounded_sqrt, log (default: bounded_sqrt)')
    .option('-a, --alpha <value>', 'Model alpha parameter: 0-1 (default: 0.5)')
    .option('-d, --maxDepth <depth>', 'Maximum traversal depth (1-10)')
    .option('--json', 'Output as JSON')
    .action(async (options) => {
        const { simulateScaleCommand } = require('../cli/commands/simulateScale');
        await simulateScaleCommand(options);
    });

// Risk Top command
program
    .command('risk-top')
    .description('Get top services by risk score')
    .option('-m, --metric <metric>', 'Risk metric: pagerank, betweenness (default: pagerank)')
    .option('-l, --limit <count>', 'Number of services to return: 1-20 (default: 5)')
    .option('--json', 'Output as JSON')
    .action(async (options) => {
        const { riskTopCommand } = require('../cli/commands/riskTop');
        await riskTopCommand(options);
    });

// Handle unknown commands
program.on('command:*', (operands) => {
    console.error(`Error: Unknown command '${operands[0]}'`);
    console.error('Run "predict --help" for a list of available commands.');
    process.exit(EXIT_CODES.VALIDATION_ERROR);
});

// Parse arguments
program.parseAsync(process.argv).catch((err) => {
    console.error(`Error: ${err.message}`);
    process.exit(err.exitCode || EXIT_CODES.UNEXPECTED);
});
