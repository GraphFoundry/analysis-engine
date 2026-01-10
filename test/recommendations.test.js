const assert = require('node:assert');
const { test, describe } = require('node:test');
const { 
    generateFailureRecommendations, 
    generateScalingRecommendations,
    _test 
} = require('../src/recommendations');

const { TRAFFIC_THRESHOLDS, LATENCY_THRESHOLDS } = _test;

describe('Failure Recommendations', () => {
    test('adds data-quality warning when confidence is low', () => {
        const result = {
            confidence: 'low',
            target: { name: 'testservice' },
            totalLostTrafficRps: 0,
            affectedCallers: [],
            unreachableServices: [],
            affectedDownstream: []
        };
        
        const recs = generateFailureRecommendations(result);
        const dataQualityRec = recs.find(r => r.type === 'data-quality');
        
        assert.ok(dataQualityRec, 'Should have data-quality recommendation');
        assert.strictEqual(dataQualityRec.priority, 'high');
        assert.ok(dataQualityRec.reason.includes('stale'));
    });

    test('generates circuit-breaker for critical traffic loss', () => {
        const result = {
            confidence: 'high',
            target: { name: 'checkoutservice' },
            totalLostTrafficRps: 150, // > 100 threshold
            affectedCallers: [
                { name: 'frontend', lostTrafficRps: 150 }
            ],
            unreachableServices: [],
            affectedDownstream: []
        };
        
        const recs = generateFailureRecommendations(result);
        const circuitBreakerRec = recs.find(r => r.type === 'circuit-breaker' && r.priority === 'critical');
        
        assert.ok(circuitBreakerRec, 'Should have critical circuit-breaker recommendation');
        assert.ok(circuitBreakerRec.reason.includes('150'));
    });

    test('generates redundancy for multiple callers', () => {
        const result = {
            confidence: 'high',
            target: { name: 'productservice' },
            totalLostTrafficRps: 30,
            affectedCallers: [
                { name: 'frontend', lostTrafficRps: 10 },
                { name: 'checkout', lostTrafficRps: 10 },
                { name: 'recommendation', lostTrafficRps: 10 }
            ],
            unreachableServices: [],
            affectedDownstream: []
        };
        
        const recs = generateFailureRecommendations(result);
        const redundancyRec = recs.find(r => r.type === 'redundancy');
        
        assert.ok(redundancyRec, 'Should have redundancy recommendation');
        assert.ok(redundancyRec.reason.includes('3'));
    });

    test('generates topology-review for unreachable services', () => {
        const result = {
            confidence: 'high',
            target: { name: 'gateway' },
            totalLostTrafficRps: 20,
            affectedCallers: [],
            unreachableServices: [
                { name: 'service-a', lostTrafficRps: 10 },
                { name: 'service-b', lostTrafficRps: 15 }
            ],
            affectedDownstream: []
        };
        
        const recs = generateFailureRecommendations(result);
        const topologyRec = recs.find(r => r.type === 'topology-review');
        
        assert.ok(topologyRec, 'Should have topology-review recommendation');
        assert.ok(topologyRec.reason.includes('2'));
    });

    test('generates monitoring for low-impact scenarios', () => {
        const result = {
            confidence: 'high',
            target: { name: 'emailservice' },
            totalLostTrafficRps: 2,
            affectedCallers: [{ name: 'checkout', lostTrafficRps: 2 }],
            unreachableServices: [],
            affectedDownstream: []
        };
        
        const recs = generateFailureRecommendations(result);
        const monitoringRec = recs.find(r => r.type === 'monitoring');
        
        assert.ok(monitoringRec, 'Should have monitoring recommendation for low impact');
        assert.strictEqual(monitoringRec.priority, 'low');
    });
});

describe('Scaling Recommendations', () => {
    test('adds data-quality warning when confidence is low', () => {
        const result = {
            confidence: 'low',
            target: { name: 'testservice' },
            currentPods: 2,
            newPods: 4,
            latencyEstimate: { deltaMs: -10 },
            affectedCallers: { items: [] }
        };
        
        const recs = generateScalingRecommendations(result);
        const dataQualityRec = recs.find(r => r.type === 'data-quality');
        
        assert.ok(dataQualityRec, 'Should have data-quality recommendation');
    });

    test('generates scaling-caution for significant latency increase when scaling down', () => {
        const result = {
            confidence: 'high',
            target: { name: 'frontend' },
            currentPods: 4,
            newPods: 2,
            latencyEstimate: { deltaMs: 60 }, // > 50ms threshold
            affectedCallers: { items: [] }
        };
        
        const recs = generateScalingRecommendations(result);
        const cautionRec = recs.find(r => r.type === 'scaling-caution');
        
        assert.ok(cautionRec, 'Should have scaling-caution recommendation');
        assert.strictEqual(cautionRec.priority, 'critical');
        assert.ok(cautionRec.reason.includes('60'));
    });

    test('generates scaling-benefit for significant improvement', () => {
        const result = {
            confidence: 'high',
            target: { name: 'api-gateway' },
            currentPods: 2,
            newPods: 4,
            latencyEstimate: { deltaMs: -55 }, // > 50ms improvement
            affectedCallers: { items: [] }
        };
        
        const recs = generateScalingRecommendations(result);
        const benefitRec = recs.find(r => r.type === 'scaling-benefit');
        
        assert.ok(benefitRec, 'Should have scaling-benefit recommendation');
        assert.ok(benefitRec.reason.includes('55'));
    });

    test('generates cost-efficiency warning for minimal benefit', () => {
        const result = {
            confidence: 'high',
            target: { name: 'worker' },
            currentPods: 2,
            newPods: 10,
            latencyEstimate: { deltaMs: -2 }, // < 5ms threshold
            affectedCallers: { items: [] }
        };
        
        const recs = generateScalingRecommendations(result);
        const costRec = recs.find(r => r.type === 'cost-efficiency');
        
        assert.ok(costRec, 'Should have cost-efficiency recommendation');
        assert.ok(costRec.reason.includes('minimal'));
    });

    test('generates propagation-awareness for affected callers', () => {
        const result = {
            confidence: 'high',
            target: { name: 'database-proxy' },
            currentPods: 2,
            newPods: 4,
            latencyEstimate: { deltaMs: -30 },
            affectedCallers: { 
                items: [
                    { name: 'api', serviceId: 'default:api', deltaMs: -25 }
                ]
            }
        };
        
        const recs = generateScalingRecommendations(result);
        const propagationRec = recs.find(r => r.type === 'propagation-awareness');
        
        assert.ok(propagationRec, 'Should have propagation-awareness recommendation');
        assert.ok(propagationRec.target.includes('api'));
    });

    test('generates proceed for no significant impact', () => {
        const result = {
            confidence: 'high',
            target: { name: 'logging' },
            currentPods: 2,
            newPods: 3,
            latencyEstimate: { deltaMs: -8 }, // Between 5-20ms
            affectedCallers: { items: [] }
        };
        
        const recs = generateScalingRecommendations(result);
        const proceedRec = recs.find(r => r.type === 'proceed');
        
        assert.ok(proceedRec, 'Should have proceed recommendation');
        assert.strictEqual(proceedRec.priority, 'low');
    });
});

describe('Thresholds', () => {
    test('traffic thresholds are defined correctly', () => {
        assert.strictEqual(TRAFFIC_THRESHOLDS.critical, 100);
        assert.strictEqual(TRAFFIC_THRESHOLDS.high, 50);
        assert.strictEqual(TRAFFIC_THRESHOLDS.medium, 10);
    });

    test('latency thresholds are defined correctly', () => {
        assert.strictEqual(LATENCY_THRESHOLDS.significant, 50);
        assert.strictEqual(LATENCY_THRESHOLDS.moderate, 20);
        assert.strictEqual(LATENCY_THRESHOLDS.minor, 5);
    });
});
