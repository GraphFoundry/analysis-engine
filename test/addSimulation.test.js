const assert = require('node:assert');
const { test, describe, beforeEach, afterEach } = require('node:test');

// Mocks
let mockBehavior = {
    getServicesWithPlacement: async () => ({ ok: true, data: { services: [] } })
};

const mockGraphEngineClient = {
    getServicesWithPlacement: async (...args) => mockBehavior.getServicesWithPlacement(...args)
};

// Mock the require to intercept graphEngineClient
// We need to use a proxy or handle the require cache manually
// Since we are using commonjs and the module requires clients/graphEngineClient
// We can try to seed the require cache or use a DI approach. 
// However, simplest way in node:test without rewiring is often to mock the module if possible or separate logic.

// Let's rely on the fact that addSimulation.js imports specific method.
// We can mock the module in the cache before requiring addSimulation.

describe('simulateAdd', () => {
    let simulateAdd;

    beforeEach(() => {
        // Mock the module
        require.cache[require.resolve('../src/clients/graphEngineClient')] = {
            exports: mockGraphEngineClient
        };

        // Re-require module under test
        delete require.cache[require.resolve('../src/simulation/addSimulation')];
        simulateAdd = require('../src/simulation/addSimulation').simulateAdd;

        // Reset default behavior
        mockBehavior.getServicesWithPlacement = async () => ({
            ok: true,
            data: {
                services: [
                    {
                        placement: {
                            nodes: [
                                {
                                    node: 'node-1',
                                    resources: {
                                        cpu: { usagePercent: 50, cores: 4 }, // 2 cores used, 2 available
                                        ram: { usedMB: 4096, totalMB: 8192 } // 4GB used, 4GB available
                                    }
                                },
                                {
                                    node: 'node-2',
                                    resources: {
                                        cpu: { usagePercent: 90, cores: 4 }, // 3.6 cores used, 0.4 available
                                        ram: { usedMB: 7000, totalMB: 8192 } // 1GB avail
                                    }
                                }
                            ]
                        }
                    }
                ]
            }
        });
    });

    afterEach(() => {
        delete require.cache[require.resolve('../src/clients/graphEngineClient')];
        delete require.cache[require.resolve('../src/simulation/addSimulation')];
    });

    test('successfully places pod when capacity exists', async () => {
        const request = {
            serviceName: 'test-service',
            cpuRequest: 1,
            ramRequest: 1024,
            replicas: 1
        };

        const result = await simulateAdd(request);

        assert.strictEqual(result.success, true);
        assert.strictEqual(result.recommendation.distribution.length, 1);
        assert.strictEqual(result.recommendation.distribution[0].node, 'node-1');
        assert.strictEqual(result.recommendation.distribution[0].replicas, 1);
    });

    test('fails when no node has enough capacity', async () => {
        const request = {
            serviceName: 'test-service',
            cpuRequest: 10, // Too big
            ramRequest: 1024,
            replicas: 1
        };

        const result = await simulateAdd(request);

        assert.strictEqual(result.success, false);
        // Updated assertion for new explanation
        assert.ok(result.explanation.includes('Failed to find placement') || result.explanation.includes('Capacity limited'), 'Explanation was: ' + result.explanation);
    });

    test('distributes replicas across multiple nodes', async () => {
        mockBehavior.getServicesWithPlacement = async () => ({
            ok: true,
            data: {
                services: [
                    {
                        placement: {
                            nodes: [
                                {
                                    node: 'node-1',
                                    resources: { cpu: { usagePercent: 0, cores: 2 }, ram: { usedMB: 0, totalMB: 4096 } }
                                },
                                {
                                    node: 'node-2',
                                    resources: { cpu: { usagePercent: 0, cores: 2 }, ram: { usedMB: 0, totalMB: 4096 } }
                                }
                            ]
                        }
                    }
                ]
            }
        });

        const request = {
            serviceName: 'test-service',
            cpuRequest: 1,
            ramRequest: 1024,
            replicas: 3
        };

        const result = await simulateAdd(request);

        assert.strictEqual(result.success, true);
        const totalPlaced = result.recommendations[0].description.match(/Place 3 replicas/);
        assert.ok(totalPlaced);
        assert.strictEqual(result.totalCapacityPods, 4);

        // Check new fields
        assert.ok(result.suitableNodes);
        assert.ok(result.riskAnalysis);
    });

    test('calculates risk when dependencies are missing', async () => {
        const request = {
            serviceName: 'test-service',
            cpuRequest: 1,
            ramRequest: 128,
            dependencies: [{ serviceId: 'unknown:service', relation: 'calls' }]
        };

        const result = await simulateAdd(request);

        assert.strictEqual(result.riskAnalysis.dependencyRisk, 'high');
        assert.ok(result.riskAnalysis.description.includes('Missing dependencies'));
    });

    test('calculates minimal risk when dependencies exist', async () => {
        // Setup mock to have the dependency AND valid nodes
        mockBehavior.getServicesWithPlacement = async () => ({
            ok: true,
            data: {
                services: [
                    {
                        serviceId: 'existing:service',
                        placement: {
                            nodes: [{ node: 'node-1', resources: { cpu: { usagePercent: 0, cores: 2 }, ram: { usedMB: 0, totalMB: 4096 } } }]
                        }
                    }
                ]
            }
        });

        const request = {
            serviceName: 'test-service',
            cpuRequest: 1,
            ramRequest: 128,
            dependencies: [{ serviceId: 'existing:service', relation: 'calls' }]
        };

        const result = await simulateAdd(request);

        assert.strictEqual(result.riskAnalysis.dependencyRisk, 'low');
    });

    test('handles error from graph client', async () => {
        mockBehavior.getServicesWithPlacement = async () => ({ ok: false, error: 'API Error' });

        const request = { serviceName: 'test', cpuRequest: 1, ramRequest: 1, replicas: 1 };

        await assert.rejects(async () => {
            await simulateAdd(request);
        }, /Failed to fetch cluster state/);
    });
});
