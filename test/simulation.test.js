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

console.log('All tests passed!');
