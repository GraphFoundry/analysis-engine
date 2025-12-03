#!/usr/bin/env node
/**
 * Evaluation Harness - Score Calculator
 * 
 * Compares predictions.json against groundTruth.json and computes accuracy metrics.
 * 
 * Usage:
 *   node tools/eval/score.js [predictions.json] [groundTruth.json]
 * 
 * Defaults:
 *   predictions.json: tools/eval/out/predictions.json
 *   groundTruth.json: tools/eval/groundTruth.sample.json
 * 
 * Metrics computed:
 *   - MAE (Mean Absolute Error) for affected service counts
 *   - MAPE (Mean Absolute Percentage Error) for traffic loss RPS
 *   - Spearman correlation for ranking (if N >= 2)
 */

const fs = require('node:fs');
const path = require('node:path');

const DEFAULT_PREDICTIONS_FILE = path.join(__dirname, 'out', 'predictions.json');
const DEFAULT_GROUND_TRUTH_FILE = path.join(__dirname, 'groundTruth.sample.json');

/**
 * Compute Mean Absolute Error
 * @param {number[]} predicted 
 * @param {number[]} actual 
 * @returns {number|null}
 */
function computeMAE(predicted, actual) {
    if (predicted.length === 0 || predicted.length !== actual.length) {
        return null;
    }
    const sum = predicted.reduce((acc, p, i) => acc + Math.abs(p - actual[i]), 0);
    return sum / predicted.length;
}

/**
 * Compute Mean Absolute Percentage Error
 * @param {number[]} predicted 
 * @param {number[]} actual 
 * @returns {number|null}
 */
function computeMAPE(predicted, actual) {
    if (predicted.length === 0 || predicted.length !== actual.length) {
        return null;
    }
    
    // Filter out zero actuals to avoid division by zero
    const validPairs = predicted
        .map((p, i) => ({ p, a: actual[i] }))
        .filter(pair => pair.a !== 0);
    
    if (validPairs.length === 0) {
        return null;
    }
    
    const sum = validPairs.reduce((acc, { p, a }) => acc + Math.abs((a - p) / a), 0);
    return sum / validPairs.length;
}

/**
 * Compute Spearman rank correlation coefficient
 * @param {number[]} x 
 * @param {number[]} y 
 * @returns {number|null}
 */
function computeSpearman(x, y) {
    if (x.length < 2 || x.length !== y.length) {
        return null;
    }

    const n = x.length;

    // Convert to ranks
    function toRanks(arr) {
        const sorted = arr.map((v, i) => ({ v, i })).sort((a, b) => a.v - b.v);
        const ranks = new Array(n);
        for (let i = 0; i < n; i++) {
            ranks[sorted[i].i] = i + 1;
        }
        return ranks;
    }

    const rankX = toRanks(x);
    const rankY = toRanks(y);

    // Compute Spearman correlation
    const dSquaredSum = rankX.reduce((acc, rx, i) => {
        const d = rx - rankY[i];
        return acc + d * d;
    }, 0);

    return 1 - (6 * dSquaredSum) / (n * (n * n - 1));
}

/**
 * Extract comparable metrics from prediction
 * @param {Object} prediction 
 * @returns {Object}
 */
function extractPredictionMetrics(prediction) {
    if (!prediction || prediction.error) {
        return null;
    }

    // For failure simulations
    if (prediction.type === 'failure') {
        const data = prediction.prediction || {};
        return {
            affectedCallersCount: data.affectedCallers?.length ?? null,
            totalLostTrafficRps: data.totalLostTrafficRps ?? null,
            unreachableCount: data.unreachableServices?.length ?? null
        };
    }

    // For scaling simulations
    if (prediction.type === 'scaling') {
        const data = prediction.prediction || {};
        return {
            latencyDeltaMs: data.latencyEstimate?.deltaMs ?? null,
            affectedCallersCount: data.affectedCallers?.items?.length ?? null
        };
    }

    return null;
}

/**
 * Main execution
 */
function main() {
    const args = process.argv.slice(2);
    const predictionsFile = args[0] || DEFAULT_PREDICTIONS_FILE;
    const groundTruthFile = args[1] || DEFAULT_GROUND_TRUTH_FILE;

    console.log(`[eval/score] Loading predictions from: ${predictionsFile}`);
    console.log(`[eval/score] Loading ground truth from: ${groundTruthFile}`);

    // Load files
    if (!fs.existsSync(predictionsFile)) {
        console.error(`Error: Predictions file not found: ${predictionsFile}`);
        console.error('Run tools/eval/run.js first to generate predictions.');
        process.exit(1);
    }

    if (!fs.existsSync(groundTruthFile)) {
        console.error(`Error: Ground truth file not found: ${groundTruthFile}`);
        process.exit(1);
    }

    const predictionsData = JSON.parse(fs.readFileSync(predictionsFile, 'utf-8'));
    const groundTruthData = JSON.parse(fs.readFileSync(groundTruthFile, 'utf-8'));

    const predictions = predictionsData.predictions || [];
    const groundTruth = groundTruthData.scenarios || [];

    // Build lookup maps
    const predictionMap = new Map(predictions.map(p => [p.scenarioId, p]));
    const truthMap = new Map(groundTruth.map(t => [t.scenarioId, t.actual]));

    // Find matching scenarios
    const matchedScenarios = [];
    for (const [scenarioId, actual] of truthMap.entries()) {
        const prediction = predictionMap.get(scenarioId);
        if (prediction && !prediction.error) {
            matchedScenarios.push({
                scenarioId,
                predicted: extractPredictionMetrics(prediction),
                actual
            });
        }
    }

    console.log(`[eval/score] Matched ${matchedScenarios.length} scenarios for comparison`);

    if (matchedScenarios.length === 0) {
        console.log('[eval/score] No matching scenarios to evaluate.');
        process.exit(0);
    }

    // Extract arrays for metric computation
    const predictedCounts = [];
    const actualCounts = [];
    const predictedTraffic = [];
    const actualTraffic = [];

    for (const { predicted, actual } of matchedScenarios) {
        if (predicted?.affectedCallersCount !== null && actual?.affectedCallersCount !== undefined) {
            predictedCounts.push(predicted.affectedCallersCount);
            actualCounts.push(actual.affectedCallersCount);
        }
        if (predicted?.totalLostTrafficRps !== null && actual?.totalLostTrafficRps !== undefined) {
            predictedTraffic.push(predicted.totalLostTrafficRps);
            actualTraffic.push(actual.totalLostTrafficRps);
        }
    }

    // Compute metrics
    const metrics = {
        sampleSize: matchedScenarios.length,
        accuracy: {
            affectedCallersMAE: computeMAE(predictedCounts, actualCounts),
            affectedCallersSampleSize: predictedCounts.length,
            trafficLossMAPE: computeMAPE(predictedTraffic, actualTraffic),
            trafficLossSampleSize: predictedTraffic.length
        },
        ranking: {
            spearmanCorrelation: computeSpearman(predictedTraffic, actualTraffic),
            note: predictedTraffic.length < 2 
                ? 'Insufficient data for ranking metrics (need N >= 2)'
                : null
        }
    };

    // Per-scenario breakdown
    const perScenario = matchedScenarios.map(({ scenarioId, predicted, actual }) => {
        const errors = {};
        
        if (predicted?.affectedCallersCount !== null && actual?.affectedCallersCount !== undefined) {
            errors.affectedCallersError = predicted.affectedCallersCount - actual.affectedCallersCount;
        }
        if (predicted?.totalLostTrafficRps !== null && actual?.totalLostTrafficRps !== undefined) {
            errors.trafficLossError = predicted.totalLostTrafficRps - actual.totalLostTrafficRps;
            if (actual.totalLostTrafficRps !== 0) {
                errors.trafficLossPctError = 
                    ((predicted.totalLostTrafficRps - actual.totalLostTrafficRps) / actual.totalLostTrafficRps) * 100;
            }
        }

        return {
            scenarioId,
            predicted,
            actual,
            errors
        };
    });

    // Output
    const output = {
        evaluatedAt: new Date().toISOString(),
        predictionsFile,
        groundTruthFile,
        metrics,
        perScenario
    };

    console.log('\n=== Evaluation Results ===\n');
    console.log(JSON.stringify(output, null, 2));

    // Write output file
    const outputFile = path.join(path.dirname(predictionsFile), 'scores.json');
    fs.writeFileSync(outputFile, JSON.stringify(output, null, 2));
    console.log(`\n[eval/score] Results written to: ${outputFile}`);
}

main();
