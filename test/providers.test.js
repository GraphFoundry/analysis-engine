/**
 * Tests for GraphEngineHttpProvider utilities
 * 
 * Uses Node.js built-in test runner.
 */

const assert = require('node:assert');
const { test, describe } = require('node:test');

const { 
    mergeEdgeMetrics, 
    normalizeServiceName 
} = require('../src/providers/GraphEngineHttpProvider');

describe('normalizeServiceName', () => {
    test('returns plain name unchanged', () => {
        assert.strictEqual(normalizeServiceName('checkoutservice'), 'checkoutservice');
        assert.strictEqual(normalizeServiceName('frontend'), 'frontend');
    });

    test('extracts name from namespace:name format', () => {
        assert.strictEqual(normalizeServiceName('default:checkoutservice'), 'checkoutservice');
        assert.strictEqual(normalizeServiceName('prod:frontend'), 'frontend');
        assert.strictEqual(normalizeServiceName('staging:payment-service'), 'payment-service');
    });

    test('handles edge case with multiple colons', () => {
        // Takes last segment after split
        assert.strictEqual(normalizeServiceName('ns:sub:service'), 'service');
    });
});

describe('mergeEdgeMetrics', () => {
    test('sums rate correctly', () => {
        const a = { rate: 10, errorRate: 0.1, p50: 50, p95: 100, p99: 150 };
        const b = { rate: 20, errorRate: 0.05, p50: 40, p95: 120, p99: 180 };
        
        const merged = mergeEdgeMetrics(a, b);
        
        assert.strictEqual(merged.rate, 30, 'rate should be summed');
    });

    test('computes rate-weighted errorRate', () => {
        const a = { rate: 10, errorRate: 0.1, p50: 50, p95: 100, p99: 150 };
        const b = { rate: 20, errorRate: 0.05, p50: 40, p95: 120, p99: 180 };
        
        const merged = mergeEdgeMetrics(a, b);
        
        // Expected: (0.1 * 10 + 0.05 * 20) / 30 = (1 + 1) / 30 = 0.0667
        const expected = (0.1 * 10 + 0.05 * 20) / 30;
        assert.ok(
            Math.abs(merged.errorRate - expected) < 0.0001,
            `errorRate should be weighted avg: expected ${expected}, got ${merged.errorRate}`
        );
    });

    test('takes max for latency percentiles (conservative)', () => {
        const a = { rate: 10, errorRate: 0.1, p50: 50, p95: 100, p99: 150 };
        const b = { rate: 20, errorRate: 0.05, p50: 40, p95: 120, p99: 180 };
        
        const merged = mergeEdgeMetrics(a, b);
        
        assert.strictEqual(merged.p50, 50, 'p50 should be max');
        assert.strictEqual(merged.p95, 120, 'p95 should be max');
        assert.strictEqual(merged.p99, 180, 'p99 should be max');
    });

    test('handles zero total rate (falls back to max errorRate)', () => {
        const a = { rate: 0, errorRate: 0.1, p50: 50, p95: 100, p99: 150 };
        const b = { rate: 0, errorRate: 0.2, p50: 40, p95: 120, p99: 180 };
        
        const merged = mergeEdgeMetrics(a, b);
        
        assert.strictEqual(merged.rate, 0, 'rate should be 0');
        assert.strictEqual(merged.errorRate, 0.2, 'errorRate should be max when rate is 0');
    });

    test('handles null/undefined metrics gracefully', () => {
        const a = { rate: null, errorRate: undefined, p50: 50, p95: null, p99: 150 };
        const b = { rate: 20, errorRate: 0.05, p50: undefined, p95: 120, p99: null };
        
        const merged = mergeEdgeMetrics(a, b);
        
        // null/undefined coerced to 0
        assert.strictEqual(merged.rate, 20, 'null rate treated as 0');
        assert.strictEqual(merged.p50, 50, 'p50 should be max (50 vs 0)');
        assert.strictEqual(merged.p95, 120, 'p95 should be max (0 vs 120)');
        assert.strictEqual(merged.p99, 150, 'p99 should be max (150 vs 0)');
    });
});

describe('edge deduplication integration', () => {
    test('duplicate edges merge into single edge with correct metrics', () => {
        // Simulate duplicate edges from /neighborhood response
        const rawEdges = [
            { from: 'A', to: 'B', rate: 10, errorRate: 0.1, p50: 50, p95: 100, p99: 150 },
            { from: 'A', to: 'B', rate: 20, errorRate: 0.05, p50: 40, p95: 120, p99: 180 },
            { from: 'B', to: 'C', rate: 5, errorRate: 0, p50: 30, p95: 60, p99: 90 }
        ];
        
        // Simulate the deduplication logic from fetchUpstreamNeighborhood
        const edgeMap = new Map();
        
        for (const e of rawEdges) {
            const key = `${e.from}->${e.to}`;
            
            const candidate = {
                source: e.from,
                target: e.to,
                rate: e.rate ?? 0,
                errorRate: e.errorRate ?? 0,
                p50: e.p50 ?? 0,
                p95: e.p95 ?? 0,
                p99: e.p99 ?? 0
            };
            
            const existing = edgeMap.get(key);
            if (!existing) {
                edgeMap.set(key, candidate);
            } else {
                // Use the real mergeEdgeMetrics function
                const merged = mergeEdgeMetrics(existing, candidate);
                edgeMap.set(key, { source: e.from, target: e.to, ...merged });
            }
        }
        
        const edges = Array.from(edgeMap.values());
        
        // Assertions
        assert.strictEqual(edges.length, 2, 'should have 2 unique edges after merge');
        
        const abEdge = edges.find(e => e.source === 'A' && e.target === 'B');
        assert.ok(abEdge, 'A->B edge should exist');
        assert.strictEqual(abEdge.rate, 30, 'A->B rate should be summed (10 + 20)');
        assert.strictEqual(abEdge.p95, 120, 'A->B p95 should be max (100 vs 120)');
        assert.strictEqual(abEdge.p99, 180, 'A->B p99 should be max (150 vs 180)');
        
        // Verify weighted errorRate: (0.1*10 + 0.05*20) / 30 = 2/30 = 0.0667
        const expectedErrorRate = (0.1 * 10 + 0.05 * 20) / 30;
        assert.ok(
            Math.abs(abEdge.errorRate - expectedErrorRate) < 0.0001,
            `A->B errorRate should be weighted avg: expected ${expectedErrorRate}, got ${abEdge.errorRate}`
        );
        
        const bcEdge = edges.find(e => e.source === 'B' && e.target === 'C');
        assert.ok(bcEdge, 'B->C edge should exist');
        assert.strictEqual(bcEdge.rate, 5, 'B->C rate should be unchanged');
    });
});
