const assert = require('node:assert');
const { test, describe } = require('node:test');
const { _test } = require('../src/riskAnalysis');

const { determineRiskLevel, generateExplanation, RISK_THRESHOLDS } = _test;

describe('Risk Analysis - determineRiskLevel', () => {
    test('returns high for top 20% with positive score', () => {
        // Rank 0 out of 10 = top 0%
        assert.strictEqual(determineRiskLevel(0.5, 0, 10), 'high');
        // Rank 1 out of 10 = top 10%
        assert.strictEqual(determineRiskLevel(0.3, 1, 10), 'high');
    });

    test('returns medium for 20-50% with positive score', () => {
        // Rank 2 out of 10 = 20%
        assert.strictEqual(determineRiskLevel(0.2, 2, 10), 'medium');
        // Rank 4 out of 10 = 40%
        assert.strictEqual(determineRiskLevel(0.1, 4, 10), 'medium');
    });

    test('returns low for bottom 50% or zero score', () => {
        // Rank 5 out of 10 = 50%
        assert.strictEqual(determineRiskLevel(0.05, 5, 10), 'low');
        // Zero score
        assert.strictEqual(determineRiskLevel(0, 0, 10), 'low');
    });

    test('handles small lists', () => {
        // Single item list - rank 0/1 = 0%
        assert.strictEqual(determineRiskLevel(0.5, 0, 1), 'high');
        // Two item list - rank 0/2 = 0%
        assert.strictEqual(determineRiskLevel(0.5, 0, 2), 'high');
        // Two item list - rank 1/2 = 50%
        assert.strictEqual(determineRiskLevel(0.3, 1, 2), 'low');
    });

    test('handles empty list gracefully', () => {
        // Division by max(0,1) = 1
        assert.strictEqual(determineRiskLevel(0.5, 0, 0), 'low');
    });
});

describe('Risk Analysis - generateExplanation', () => {
    test('generates high risk explanation for pagerank', () => {
        const explanation = generateExplanation('frontend', 'pagerank', 0.35, 'high');
        assert.ok(explanation.includes('frontend'));
        assert.ok(explanation.includes('PageRank'));
        assert.ok(explanation.includes('0.3500'));
        assert.ok(explanation.includes('critical hub'));
    });

    test('generates medium risk explanation for betweenness', () => {
        const explanation = generateExplanation('cartservice', 'betweenness', 0.15, 'medium');
        assert.ok(explanation.includes('cartservice'));
        assert.ok(explanation.includes('betweenness centrality'));
        assert.ok(explanation.includes('moderate'));
    });

    test('generates low risk explanation', () => {
        const explanation = generateExplanation('emailservice', 'pagerank', 0.02, 'low');
        assert.ok(explanation.includes('emailservice'));
        assert.ok(explanation.includes('low'));
        assert.ok(explanation.includes('Lower risk'));
    });
});

describe('Risk Analysis - RISK_THRESHOLDS', () => {
    test('thresholds are defined', () => {
        assert.ok(RISK_THRESHOLDS.high !== undefined);
        assert.ok(RISK_THRESHOLDS.medium !== undefined);
        assert.ok(RISK_THRESHOLDS.low !== undefined);
    });

    test('thresholds are in descending order', () => {
        assert.ok(RISK_THRESHOLDS.high > RISK_THRESHOLDS.medium);
        assert.ok(RISK_THRESHOLDS.medium >= RISK_THRESHOLDS.low);
    });
});
