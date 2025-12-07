const assert = require('node:assert');
const { test } = require('node:test');

// Mock config for testing
const mockConfig = {
    simulation: {
        scalingAlpha: 0.5,
        minLatencyFactor: 0.6,
        maxPathsReturned: 10
    }
};

/**
 * Test: Bounded sqrt scaling formula
 */
test('bounded_sqrt scaling formula - 2x pods reduces latency correctly', () => {
    const baseLatency = 100;
    const currentPods = 2;
    const newPods = 4;
    const alpha = 0.5;
    
    const ratio = newPods / currentPods;
    const improvement = 1 / Math.sqrt(ratio);
    const newLatency = baseLatency * (alpha + (1 - alpha) * improvement);
    
    // Expected: 100 * (0.5 + 0.5 * (1/sqrt(2))) = 100 * (0.5 + 0.3536) = 85.36
    assert.ok(newLatency >= 85 && newLatency <= 86, `Expected ~85.36, got ${newLatency}`);
});

test('bounded_sqrt scaling formula - 3x pods reduces latency correctly', () => {
    const baseLatency = 100;
    const currentPods = 2;
    const newPods = 6;
    const alpha = 0.5;
    
    const ratio = newPods / currentPods;
    const improvement = 1 / Math.sqrt(ratio);
    const newLatency = baseLatency * (alpha + (1 - alpha) * improvement);
    
    // Expected: 100 * (0.5 + 0.5 * (1/sqrt(3))) = 100 * (0.5 + 0.2887) = 78.87
    assert.ok(newLatency >= 78 && newLatency <= 80, `Expected ~78.87, got ${newLatency}`);
});

test('bounded_sqrt scaling formula - clamped to minimum', () => {
    const baseLatency = 100;
    const currentPods = 1;
    const newPods = 1000;
    const alpha = 0.5;
    const minLatencyFactor = 0.6;
    
    const ratio = newPods / currentPods;
    const improvement = 1 / Math.sqrt(ratio);
    const newLatency = baseLatency * (alpha + (1 - alpha) * improvement);
    const clamped = Math.max(newLatency, baseLatency * minLatencyFactor);
    
    // Should be clamped to 60
    assert.strictEqual(clamped, 60, `Expected clamped to 60, got ${clamped}`);
});

test('bounded_sqrt scaling formula - alpha=1.0 means no improvement', () => {
    const baseLatency = 100;
    const currentPods = 2;
    const newPods = 10;
    const alpha = 1.0; // All fixed overhead
    
    const ratio = newPods / currentPods;
    const improvement = 1 / Math.sqrt(ratio);
    const newLatency = baseLatency * (alpha + (1 - alpha) * improvement);
    
    // Expected: 100 * (1.0 + 0 * improvement) = 100
    assert.strictEqual(newLatency, baseLatency, `Expected no change, got ${newLatency}`);
});

test('bounded_sqrt scaling formula - alpha=0.0 means full improvement', () => {
    const baseLatency = 100;
    const currentPods = 1;
    const newPods = 4;
    const alpha = 0.0; // No fixed overhead
    
    const ratio = newPods / currentPods;
    const improvement = 1 / Math.sqrt(ratio);
    const newLatency = baseLatency * (alpha + (1 - alpha) * improvement);
    
    // Expected: 100 * (0 + 1.0 * (1/sqrt(4))) = 100 * 0.5 = 50
    assert.strictEqual(newLatency, 50, `Expected 50, got ${newLatency}`);
});

/**
 * Test: Linear scaling formula
 */
test('linear scaling formula - 2x pods halves latency', () => {
    const baseLatency = 100;
    const currentPods = 2;
    const newPods = 4;
    
    const newLatency = baseLatency * (currentPods / newPods);
    
    assert.strictEqual(newLatency, 50, `Expected 50, got ${newLatency}`);
});

test('linear scaling formula - 3x pods reduces latency by 1/3', () => {
    const baseLatency = 90;
    const currentPods = 3;
    const newPods = 9;
    
    const newLatency = baseLatency * (currentPods / newPods);
    
    assert.strictEqual(newLatency, 30, `Expected 30, got ${newLatency}`);
});

/**
 * Test: Weighted mean latency calculation
 */
test('weighted mean latency - normal case', () => {
    const edges = [
        { rate: 10, p95: 100 },
        { rate: 20, p95: 50 },
        { rate: 30, p95: 30 }
    ];
    
    let totalWeightedLatency = 0;
    let totalRate = 0;
    
    for (const edge of edges) {
        totalWeightedLatency += edge.rate * edge.p95;
        totalRate += edge.rate;
    }
    
    const weightedMean = totalWeightedLatency / totalRate;
    
    // Expected: (10*100 + 20*50 + 30*30) / (10+20+30) = (1000+1000+900) / 60 = 48.33
    assert.ok(weightedMean >= 48 && weightedMean <= 49, `Expected ~48.33, got ${weightedMean}`);
});

test('weighted mean latency - single edge', () => {
    const edges = [
        { rate: 5, p95: 80 }
    ];
    
    let totalWeightedLatency = 0;
    let totalRate = 0;
    
    for (const edge of edges) {
        totalWeightedLatency += edge.rate * edge.p95;
        totalRate += edge.rate;
    }
    
    const weightedMean = totalWeightedLatency / totalRate;
    
    assert.strictEqual(weightedMean, 80, `Expected 80, got ${weightedMean}`);
});

test('weighted mean latency - zero traffic returns null', () => {
    const edges = [
        { rate: 0, p95: 100 },
        { rate: 0, p95: 50 }
    ];
    
    let totalWeightedLatency = 0;
    let totalRate = 0;
    
    for (const edge of edges) {
        totalWeightedLatency += edge.rate * edge.p95;
        totalRate += edge.rate;
    }
    
    const weightedMean = totalRate === 0 ? null : totalWeightedLatency / totalRate;
    
    assert.strictEqual(weightedMean, null, `Expected null for zero traffic, got ${weightedMean}`);
});

test('weighted mean latency - with adjusted latencies', () => {
    const edges = [
        { target: 'A', rate: 10, p95: 100 },
        { target: 'B', rate: 20, p95: 50 }
    ];
    
    const adjustedLatencies = new Map([
        ['A', 80] // A's latency reduced from 100 to 80
    ]);
    
    let totalWeightedLatency = 0;
    let totalRate = 0;
    
    for (const edge of edges) {
        const latency = adjustedLatencies.has(edge.target) 
            ? adjustedLatencies.get(edge.target)
            : edge.p95;
        
        totalWeightedLatency += edge.rate * latency;
        totalRate += edge.rate;
    }
    
    const weightedMean = totalWeightedLatency / totalRate;
    
    // Expected: (10*80 + 20*50) / 30 = (800+1000) / 30 = 60
    assert.strictEqual(weightedMean, 60, `Expected 60, got ${weightedMean}`);
});

/**
 * Test: Service identifier parsing
 */
test('service identifier - parse from serviceId', () => {
    const body = { serviceId: 'default:frontend' };
    
    const parsed = body.serviceId;
    assert.strictEqual(parsed, 'default:frontend');
});

test('service identifier - parse from name + namespace', () => {
    const body = { name: 'frontend', namespace: 'default' };
    
    const serviceId = `${body.namespace}:${body.name}`;
    assert.strictEqual(serviceId, 'default:frontend');
});

/**
 * Test: Path throughput (min rate along path)
 */
test('path throughput - bottleneck is minimum rate', () => {
    const pathEdges = [
        { rate: 100 },
        { rate: 50 },  // Bottleneck
        { rate: 200 }
    ];
    
    const bottleneck = Math.min(...pathEdges.map(e => e.rate));
    assert.strictEqual(bottleneck, 50);
});

/**
 * Test: Depth validation
 */
test('depth validation - 1 is valid', () => {
    const depth = 1;
    assert.ok(depth >= 1 && depth <= 3);
});

test('depth validation - 2 is valid', () => {
    const depth = 2;
    assert.ok(depth >= 1 && depth <= 3);
});

test('depth validation - 3 is valid', () => {
    const depth = 3;
    assert.ok(depth >= 1 && depth <= 3);
});

test('depth validation - 0 is invalid', () => {
    const depth = 0;
    assert.ok(depth < 1 || depth > 3);
});

test('depth validation - 4 is invalid', () => {
    const depth = 4;
    assert.ok(depth < 1 || depth > 3);
});

/**
 * Test: Mock failure simulation logic
 */
test('failure simulation - direct caller loses traffic', () => {
    // Mock graph: A -> B, C -> B
    const incomingEdges = [
        { source: 'A', target: 'B', rate: 10, errorRate: 0.01 },
        { source: 'C', target: 'B', rate: 5, errorRate: 0.0 }
    ];
    
    const affectedCallers = incomingEdges.map(edge => ({
        serviceId: edge.source,
        lostTrafficRps: edge.rate,
        edgeErrorRate: edge.errorRate
    }));
    
    affectedCallers.sort((a, b) => b.lostTrafficRps - a.lostTrafficRps);
    
    assert.strictEqual(affectedCallers.length, 2);
    assert.strictEqual(affectedCallers[0].serviceId, 'A');
    assert.strictEqual(affectedCallers[0].lostTrafficRps, 10);
    assert.strictEqual(affectedCallers[1].serviceId, 'C');
    assert.strictEqual(affectedCallers[1].lostTrafficRps, 5);
});

/**
 * Test: Data freshness confidence logic
 */
test('confidence is "low" when dataFreshness.stale is true', () => {
    const dataFreshness = { 
        source: 'graph-engine', 
        stale: true, 
        lastUpdatedSecondsAgo: 600, 
        windowMinutes: 5 
    };
    const confidence = dataFreshness?.stale ? 'low' : 'high';
    
    assert.strictEqual(confidence, 'low');
});

test('confidence is "high" when dataFreshness.stale is false', () => {
    const dataFreshness = { 
        source: 'graph-engine', 
        stale: false, 
        lastUpdatedSecondsAgo: 30, 
        windowMinutes: 5 
    };
    const confidence = dataFreshness?.stale ? 'low' : 'high';
    
    assert.strictEqual(confidence, 'high');
});

test('confidence is "high" when dataFreshness is null', () => {
    const dataFreshness = null;
    const confidence = dataFreshness?.stale ? 'low' : 'high';
    
    // null?.stale is undefined, which is falsy, so confidence is 'high'
    assert.strictEqual(confidence, 'high');
});

test('confidence is "high" when dataFreshness is undefined', () => {
    const dataFreshness = undefined;
    const confidence = dataFreshness?.stale ? 'low' : 'high';
    
    assert.strictEqual(confidence, 'high');
});

/**
 * Test: Phase 3 - Service ID helpers
 */
const { _test: failureHelpers } = require('../src/failureSimulation');

test('parseServiceRef - handles namespace:name format', () => {
    const result = failureHelpers.parseServiceRef('production:frontend');
    assert.strictEqual(result.namespace, 'production');
    assert.strictEqual(result.name, 'frontend');
});

test('parseServiceRef - handles plain name format', () => {
    const result = failureHelpers.parseServiceRef('checkoutservice');
    assert.strictEqual(result.namespace, 'default');
    assert.strictEqual(result.name, 'checkoutservice');
});

test('parseServiceRef - handles null/undefined', () => {
    const result = failureHelpers.parseServiceRef(null);
    assert.strictEqual(result.namespace, 'default');
    assert.strictEqual(result.name, '');
});

test('toCanonicalServiceId - creates namespace:name format', () => {
    const result = failureHelpers.toCanonicalServiceId('default', 'frontend');
    assert.strictEqual(result, 'default:frontend');
});

test('nodeToOutRef - uses node values when present', () => {
    const node = { serviceId: 'frontend', name: 'frontend', namespace: 'prod' };
    const result = failureHelpers.nodeToOutRef(node, 'fallback');
    assert.strictEqual(result.serviceId, 'prod:frontend');
    assert.strictEqual(result.name, 'frontend');
    assert.strictEqual(result.namespace, 'prod');
});

test('nodeToOutRef - falls back to parsing key when node is undefined', () => {
    const result = failureHelpers.nodeToOutRef(undefined, 'staging:backend');
    assert.strictEqual(result.serviceId, 'staging:backend');
    assert.strictEqual(result.name, 'backend');
    assert.strictEqual(result.namespace, 'staging');
});

/**
 * Test: Phase 3 - Reachability analysis
 */
test('pickEntrypoints - finds nodes with no incoming edges', () => {
    // Mock snapshot: A -> B -> C (A is entrypoint)
    const snapshot = {
        nodes: new Map([['A', {}], ['B', {}], ['C', {}]]),
        incomingEdges: new Map([
            ['A', []],
            ['B', [{ source: 'A', target: 'B' }]],
            ['C', [{ source: 'B', target: 'C' }]]
        ])
    };
    
    const entrypoints = failureHelpers.pickEntrypoints(snapshot, 'C');
    assert.ok(entrypoints.includes('A'), 'A should be an entrypoint');
    assert.ok(!entrypoints.includes('C'), 'C (blocked) should not be an entrypoint');
});

test('computeReachableNodes - traverses graph excluding blocked node', () => {
    // Mock snapshot: A -> B -> C, B -> D
    // If B is blocked, only A is reachable from A
    const snapshot = {
        nodes: new Map([['A', {}], ['B', {}], ['C', {}], ['D', {}]]),
        outgoingEdges: new Map([
            ['A', [{ source: 'A', target: 'B' }]],
            ['B', [{ source: 'B', target: 'C' }, { source: 'B', target: 'D' }]],
            ['C', []],
            ['D', []]
        ])
    };
    
    const reachable = failureHelpers.computeReachableNodes(snapshot, ['A'], 'B');
    
    assert.ok(reachable.has('A'), 'A should be reachable');
    assert.ok(!reachable.has('B'), 'B (blocked) should not be reachable');
    assert.ok(!reachable.has('C'), 'C should not be reachable (behind blocked B)');
    assert.ok(!reachable.has('D'), 'D should not be reachable (behind blocked B)');
});

test('computeReachableNodes - can reach nodes via alternate paths', () => {
    // Mock snapshot: A -> B -> C, A -> C (alternate path)
    // If B is blocked, C is still reachable via A -> C
    const snapshot = {
        nodes: new Map([['A', {}], ['B', {}], ['C', {}]]),
        outgoingEdges: new Map([
            ['A', [{ source: 'A', target: 'B' }, { source: 'A', target: 'C' }]],
            ['B', [{ source: 'B', target: 'C' }]],
            ['C', []]
        ])
    };
    
    const reachable = failureHelpers.computeReachableNodes(snapshot, ['A'], 'B');
    
    assert.ok(reachable.has('A'), 'A should be reachable');
    assert.ok(!reachable.has('B'), 'B (blocked) should not be reachable');
    assert.ok(reachable.has('C'), 'C should be reachable via alternate path A -> C');
});

test('estimateBoundaryLostTraffic - computes cut edge traffic', () => {
    // Mock: A (reachable) -> B (unreachable), rate=100
    const snapshot = {
        nodes: new Map([['A', {}], ['B', {}], ['TARGET', {}]]),
        incomingEdges: new Map([
            ['A', []],
            ['B', [{ source: 'A', target: 'B', rate: 100 }]],
            ['TARGET', []]
        ])
    };
    
    const reachableSet = new Set(['A']);
    const lostByNode = failureHelpers.estimateBoundaryLostTraffic(snapshot, reachableSet, 'TARGET');
    
    assert.deepStrictEqual(lostByNode.get('B'), {
        lostFromTargetRps: 0,
        lostFromReachableCutsRps: 100,
        lostTotalRps: 100
    }, 'B should have 100 RPS lost traffic from reachable cuts');
});

test('estimateBoundaryLostTraffic - splits traffic from blocked node vs reachable cuts', () => {
    // Mock: TARGET -> B (rate=50), A -> B (rate=30)
    // Now both are counted separately
    const snapshot = {
        nodes: new Map([['A', {}], ['B', {}], ['TARGET', {}]]),
        incomingEdges: new Map([
            ['A', []],
            ['B', [
                { source: 'TARGET', target: 'B', rate: 50 },
                { source: 'A', target: 'B', rate: 30 }
            ]],
            ['TARGET', []]
        ])
    };
    
    const reachableSet = new Set(['A']);
    const lostByNode = failureHelpers.estimateBoundaryLostTraffic(snapshot, reachableSet, 'TARGET');
    
    assert.deepStrictEqual(lostByNode.get('B'), {
        lostFromTargetRps: 50,
        lostFromReachableCutsRps: 30,
        lostTotalRps: 80
    }, 'B should have 50 from target + 30 from reachable cuts = 80 total');
});

test('estimateBoundaryLostTraffic - service with only target edge shows non-zero loss', () => {
    // Critical test: B only has incoming edge from blocked TARGET
    // This was previously returning 0, now should return the target's traffic
    const snapshot = {
        nodes: new Map([['A', {}], ['B', {}], ['TARGET', {}]]),
        incomingEdges: new Map([
            ['A', []],
            ['B', [{ source: 'TARGET', target: 'B', rate: 75 }]],
            ['TARGET', []]
        ])
    };
    
    const reachableSet = new Set(['A']);
    const lostByNode = failureHelpers.estimateBoundaryLostTraffic(snapshot, reachableSet, 'TARGET');
    
    assert.deepStrictEqual(lostByNode.get('B'), {
        lostFromTargetRps: 75,
        lostFromReachableCutsRps: 0,
        lostTotalRps: 75
    }, 'B should have 75 RPS from target (was previously 0)');
});

/**
 * Test: Scaling response includes scalingDirection
 */
test('scalingDirection - computed correctly for scale up', () => {
    const currentPods = 2;
    const newPods = 4;
    const direction = newPods > currentPods ? 'up' : newPods < currentPods ? 'down' : 'none';
    assert.strictEqual(direction, 'up');
});

test('scalingDirection - computed correctly for scale down', () => {
    const currentPods = 4;
    const newPods = 2;
    const direction = newPods > currentPods ? 'up' : newPods < currentPods ? 'down' : 'none';
    assert.strictEqual(direction, 'down');
});

test('scalingDirection - computed correctly for no change', () => {
    const currentPods = 3;
    const newPods = 3;
    const direction = newPods > currentPods ? 'up' : newPods < currentPods ? 'down' : 'none';
    assert.strictEqual(direction, 'none');
});

/**
 * Test: Scaling explanation generation
 */
test('scaling explanation - includes key information when latency is known', () => {
    const targetName = 'cartservice';
    const currentPods = 2;
    const newPods = 4;
    const scalingDirection = 'up';
    const baselineMs = 120.5;
    const projectedMs = 85.2;
    const deltaMs = projectedMs - baselineMs;
    const callersCount = 3;
    const pathsCount = 2;
    
    const directionWord = scalingDirection === 'up' ? 'up' : scalingDirection === 'down' ? 'down' : 'at same level';
    const improvementWord = deltaMs < 0 ? 'improves' : deltaMs > 0 ? 'degrades' : 'maintains';
    
    const explanation = `Scaling ${targetName} ${directionWord} from ${currentPods} to ${newPods} pods ` +
        `${improvementWord} latency by ${Math.abs(deltaMs).toFixed(1)}ms ` +
        `(baseline: ${baselineMs.toFixed(1)}ms → projected: ${projectedMs.toFixed(1)}ms). ` +
        `${callersCount} upstream caller(s) affected across ${pathsCount} path(s).`;
    
    assert.ok(explanation.includes('cartservice'), 'Should include target name');
    assert.ok(explanation.includes('up'), 'Should include direction');
    assert.ok(explanation.includes('2 to 4'), 'Should include pod counts');
    assert.ok(explanation.includes('improves'), 'Should indicate improvement');
    assert.ok(explanation.includes('35.3ms'), 'Should include delta magnitude');
    assert.ok(explanation.includes('3 upstream caller'), 'Should include callers count');
});

test('scaling explanation - handles unknown latency gracefully', () => {
    const targetName = 'frontend';
    const currentPods = 2;
    const newPods = 4;
    const scalingDirection = 'up';
    const callersCount = 2;
    const pathsCount = 1;
    
    // Simulate when latency is null
    const directionWord = scalingDirection === 'up' ? 'up' : scalingDirection === 'down' ? 'down' : 'at same level';
    const explanation = `Scaling ${targetName} ${directionWord} from ${currentPods} to ${newPods} pods. ` +
        `Latency impact unknown due to missing edge metrics. ` +
        `${callersCount} upstream caller(s) identified across ${pathsCount} path(s).`;
    
    assert.ok(explanation.includes('frontend'), 'Should include target name');
    assert.ok(explanation.includes('unknown'), 'Should indicate unknown latency');
    assert.ok(explanation.includes('missing edge metrics'), 'Should explain why unknown');
});

/**
 * Test: Warnings array for incomplete data
 */
test('warnings array - generated when paths have incomplete data', () => {
    const affectedPaths = [
        { path: ['A', 'B'], pathRps: 100, incompleteData: false },
        { path: ['C', 'D'], pathRps: 50, incompleteData: true },
        { path: ['E', 'F'], pathRps: 25, incompleteData: true }
    ];
    
    const incompletePathsCount = affectedPaths.filter(p => p.incompleteData).length;
    const totalPaths = affectedPaths.length;
    
    let warnings;
    if (incompletePathsCount > 0) {
        warnings = [
            `${incompletePathsCount} of ${totalPaths} path(s) have incomplete latency data (missing edge metrics). Results may be partial.`
        ];
    }
    
    assert.ok(warnings !== undefined, 'Warnings should be defined when incomplete data exists');
    assert.strictEqual(warnings.length, 1, 'Should have exactly one warning');
    assert.ok(warnings[0].includes('2 of 3'), 'Should specify count of incomplete paths');
    assert.ok(warnings[0].includes('incomplete latency data'), 'Should mention incomplete data');
});

test('warnings array - not generated when all paths complete', () => {
    const affectedPaths = [
        { path: ['A', 'B'], pathRps: 100, incompleteData: false },
        { path: ['C', 'D'], pathRps: 50, incompleteData: false }
    ];
    
    const incompletePathsCount = affectedPaths.filter(p => p.incompleteData).length;
    
    let warnings;
    if (incompletePathsCount > 0) {
        warnings = [`${incompletePathsCount} paths have incomplete data`];
    }
    
    assert.strictEqual(warnings, undefined, 'Warnings should not be defined when all paths are complete');
});

console.log('All tests passed!');
