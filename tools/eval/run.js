#!/usr/bin/env node
/**
 * Evaluation Harness - Scenario Runner
 * 
 * Reads scenarios from scenarios.json, calls the local API endpoints,
 * and writes predictions to out/predictions.json with timing data.
 * 
 * Usage:
 *   node tools/eval/run.js [scenarios.json] [output-dir]
 * 
 * Defaults:
 *   scenarios.json: tools/eval/scenarios.sample.json
 *   output-dir: tools/eval/out
 */

const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');

// Configuration
const API_BASE_URL = process.env.API_BASE_URL || 'http://localhost:7000';
const DEFAULT_SCENARIOS_FILE = path.join(__dirname, 'scenarios.sample.json');
const DEFAULT_OUTPUT_DIR = path.join(__dirname, 'out');

/**
 * Make HTTP POST request
 * @param {string} url 
 * @param {Object} body 
 * @returns {Promise<{statusCode: number, data: Object}>}
 */
function httpPost(url, body) {
    return new Promise((resolve, reject) => {
        const parsedUrl = new URL(url);
        const options = {
            hostname: parsedUrl.hostname,
            port: parsedUrl.port,
            path: parsedUrl.pathname,
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            timeout: 30000
        };

        const req = http.request(options, (res) => {
            let data = '';
            res.on('data', chunk => data += chunk);
            res.on('end', () => {
                try {
                    const parsed = JSON.parse(data);
                    resolve({ statusCode: res.statusCode, data: parsed });
                } catch (e) {
                    resolve({ statusCode: res.statusCode, data: { raw: data } });
                }
            });
        });

        req.on('error', reject);
        req.on('timeout', () => {
            req.destroy();
            reject(new Error('Request timeout'));
        });

        req.write(JSON.stringify(body));
        req.end();
    });
}

/**
 * Run a single scenario
 * @param {Object} scenario 
 * @returns {Promise<Object>}
 */
async function runScenario(scenario) {
    const { id, type, params } = scenario;
    const startTime = Date.now();
    
    let endpoint;
    if (type === 'failure') {
        endpoint = `${API_BASE_URL}/simulate/failure`;
    } else if (type === 'scaling') {
        endpoint = `${API_BASE_URL}/simulate/scale`;
    } else {
        return {
            scenarioId: id,
            error: `Unknown scenario type: ${type}`,
            durationMs: Date.now() - startTime
        };
    }

    try {
        const { statusCode, data } = await httpPost(endpoint, params);
        const durationMs = Date.now() - startTime;

        if (statusCode >= 200 && statusCode < 300) {
            return {
                scenarioId: id,
                type,
                prediction: data,
                durationMs
            };
        } else {
            return {
                scenarioId: id,
                type,
                error: data.error || `HTTP ${statusCode}`,
                durationMs
            };
        }
    } catch (error) {
        return {
            scenarioId: id,
            type,
            error: error.message,
            durationMs: Date.now() - startTime
        };
    }
}

/**
 * Main execution
 */
async function main() {
    const args = process.argv.slice(2);
    const scenariosFile = args[0] || DEFAULT_SCENARIOS_FILE;
    const outputDir = args[1] || DEFAULT_OUTPUT_DIR;

    console.log(`[eval/run] Loading scenarios from: ${scenariosFile}`);

    // Load scenarios
    if (!fs.existsSync(scenariosFile)) {
        console.error(`Error: Scenarios file not found: ${scenariosFile}`);
        process.exit(1);
    }

    const scenariosData = JSON.parse(fs.readFileSync(scenariosFile, 'utf-8'));
    const scenarios = scenariosData.scenarios || [];

    if (scenarios.length === 0) {
        console.error('Error: No scenarios found in file');
        process.exit(1);
    }

    console.log(`[eval/run] Running ${scenarios.length} scenario(s)...`);

    // Ensure output directory exists
    if (!fs.existsSync(outputDir)) {
        fs.mkdirSync(outputDir, { recursive: true });
    }

    // Run scenarios sequentially
    const results = [];
    const overallStartTime = Date.now();

    for (const scenario of scenarios) {
        console.log(`  [${scenario.id}] Running ${scenario.type} simulation...`);
        const result = await runScenario(scenario);
        results.push(result);
        
        if (result.error) {
            console.log(`    ❌ Error: ${result.error} (${result.durationMs}ms)`);
        } else {
            console.log(`    ✓ Completed (${result.durationMs}ms)`);
        }
    }

    const overallDurationMs = Date.now() - overallStartTime;

    // Compute overhead stats
    const successResults = results.filter(r => !r.error);
    const durations = successResults.map(r => r.durationMs);
    const overhead = {
        totalMs: overallDurationMs,
        scenarioCount: results.length,
        successCount: successResults.length,
        errorCount: results.length - successResults.length,
        avgPerScenarioMs: durations.length > 0 
            ? Math.round(durations.reduce((a, b) => a + b, 0) / durations.length) 
            : null,
        maxMs: durations.length > 0 ? Math.max(...durations) : null,
        minMs: durations.length > 0 ? Math.min(...durations) : null
    };

    // Build output
    const output = {
        runId: `run-${Date.now()}`,
        runAt: new Date().toISOString(),
        apiBaseUrl: API_BASE_URL,
        scenariosFile,
        predictions: results,
        overhead
    };

    // Write output
    const outputFile = path.join(outputDir, 'predictions.json');
    fs.writeFileSync(outputFile, JSON.stringify(output, null, 2));

    console.log(`\n[eval/run] Results written to: ${outputFile}`);
    console.log(`[eval/run] Overhead: ${overhead.totalMs}ms total, ${overhead.avgPerScenarioMs}ms avg per scenario`);
    console.log(`[eval/run] Success: ${overhead.successCount}/${overhead.scenarioCount}`);
}

main().catch(err => {
    console.error('Fatal error:', err);
    process.exit(1);
});
