const express = require('express');
const config = require('./src/config');
const { validateEnv } = require('./src/config');
const { checkHealth, closeDriver } = require('./src/neo4j');
const { simulateFailure } = require('./src/failureSimulation');
const { simulateScaling } = require('./src/scalingSimulation');
const {
    parseServiceIdentifier,
    normalizePodParams,
    validateScalingParams,
    validateLatencyMetric,
    validateDepth,
    validateScalingModel
} = require('./src/validator');

// Validate environment before starting server
validateEnv();

const app = express();
app.use(express.json());

// Track server start time
const startTime = Date.now();

/**
 * Health check endpoint
 * Returns Neo4j connectivity status and service count
 */
app.get('/health', async (req, res) => {
    try {
        const health = await checkHealth();
        const uptimeSeconds = Math.round((Date.now() - startTime) / 100) / 10; // Round to 1 decimal
        
        res.json({
            status: health.connected ? 'ok' : 'degraded',
            neo4j: {
                connected: health.connected,
                services: health.services,
                error: health.error
            },
            config: {
                maxTraversalDepth: config.simulation.maxTraversalDepth,
                defaultLatencyMetric: config.simulation.defaultLatencyMetric
            },
            uptimeSeconds
        });
    } catch (error) {
        res.status(500).json({
            status: 'error',
            error: error.message
        });
    }
});

/**
 * POST /simulate/failure
 * Simulate failure of a service and report impact
 * 
 * Request body:
 * - serviceId: string (OR name + namespace)
 * - name: string (optional, with namespace)
 * - namespace: string (optional, with name)
 * - maxDepth: number (optional, default from config)
 */
app.post('/simulate/failure', async (req, res) => {
    try {
        // Validate and parse request
        const identifier = parseServiceIdentifier(req.body);
        const maxDepth = validateDepth(
            req.body.maxDepth,
            config.simulation.maxTraversalDepth,
            config.simulation.maxTraversalDepth
        );
        
        // Execute simulation with timeout
        const simulationPromise = simulateFailure({
            serviceId: identifier.serviceId,
            maxDepth
        });
        
        const timeoutPromise = new Promise((_, reject) => {
            setTimeout(
                () => reject(new Error('Simulation timeout exceeded')),
                config.simulation.timeoutMs
            );
        });
        
        const result = await Promise.race([simulationPromise, timeoutPromise]);
        
        res.json(result);
    } catch (error) {
        if (error.message.includes('not found')) {
            res.status(404).json({ error: error.message });
        } else if (error.message.includes('timeout')) {
            res.status(504).json({ error: error.message });
        } else if (error.message.includes('must') || error.message.includes('invalid')) {
            res.status(400).json({ error: error.message });
        } else {
            console.error('Simulation error:', error);
            res.status(500).json({ error: 'Internal server error' });
        }
    }
});

/**
 * POST /simulate/scale
 * Simulate scaling of a service (change pod count) and report impact
 * 
 * Request body:
 * - serviceId: string (OR name + namespace)
 * - name: string (optional, with namespace)
 * - namespace: string (optional, with name)
 * - currentPods: number (required)
 * - newPods: number (required, aliases: targetPods, pods)
 * - latencyMetric: string (optional, p50/p95/p99)
 * - model: object (optional, { type: 'bounded_sqrt', alpha: 0.5 })
 * - maxDepth: number (optional, default from config)
 */
app.post('/simulate/scale', async (req, res) => {
    try {
        // Validate and parse request
        const identifier = parseServiceIdentifier(req.body);
        const newPods = normalizePodParams(req.body);
        validateScalingParams(req.body.currentPods, newPods);
        const latencyMetric = validateLatencyMetric(
            req.body.latencyMetric,
            config.simulation.defaultLatencyMetric
        );
        const maxDepth = validateDepth(
            req.body.maxDepth,
            config.simulation.maxTraversalDepth,
            config.simulation.maxTraversalDepth
        );
        const model = validateScalingModel(req.body.model);
        
        // Execute simulation with timeout
        const simulationPromise = simulateScaling({
            serviceId: identifier.serviceId,
            currentPods: req.body.currentPods,
            newPods,
            latencyMetric,
            model,
            maxDepth
        });
        
        const timeoutPromise = new Promise((_, reject) => {
            setTimeout(
                () => reject(new Error('Simulation timeout exceeded')),
                config.simulation.timeoutMs
            );
        });
        
        const result = await Promise.race([simulationPromise, timeoutPromise]);
        
        res.json(result);
    } catch (error) {
        if (error.message.includes('not found')) {
            res.status(404).json({ error: error.message });
        } else if (error.message.includes('timeout')) {
            res.status(504).json({ error: error.message });
        } else if (error.message.includes('must') || error.message.includes('invalid') || error.message.includes('Unknown')) {
            res.status(400).json({ error: error.message });
        } else {
            console.error('Simulation error:', error);
            res.status(500).json({ error: 'Internal server error' });
        }
    }
});

// Start server
const server = app.listen(config.server.port, () => {
    console.log(`[${new Date().toISOString()}] What-if Simulation Engine started`);
    console.log(`Port: ${config.server.port}`);
    console.log(`Max traversal depth: ${config.simulation.maxTraversalDepth}`);
    console.log(`Default latency metric: ${config.simulation.defaultLatencyMetric}`);
    console.log(`Scaling model: ${config.simulation.scalingModel} (alpha: ${config.simulation.scalingAlpha})`);
    console.log(`Timeout: ${config.simulation.timeoutMs}ms`);
});

// Graceful shutdown
const shutdown = async () => {
    console.log('\nShutting down service...');
    server.close();
    await closeDriver();
    console.log('Neo4j connection closed. Bye.');
    process.exit(0);
};

process.on('SIGINT', shutdown);
process.on('SIGTERM', shutdown);
