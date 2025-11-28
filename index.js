const express = require('express');
const config = require('./src/config');
const { validateEnv } = require('./src/config');
const { getProvider } = require('./src/providers');
const { checkGraphHealth } = require('./src/graphEngineClient');
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
 * Returns data source connectivity status (Neo4j or Graph API) and config info
 */
app.get('/health', async (req, res) => {
    try {
        const provider = getProvider();
        const providerHealth = await provider.checkHealth();
        const uptimeSeconds = Math.round((Date.now() - startTime) / 100) / 10;

        // Graph API health (conditional)
        let graphApi;
        if (config.graphApi.enabled) {
            const graphResult = await checkGraphHealth();
            if (graphResult.ok) {
                graphApi = {
                    enabled: true,
                    available: true,
                    status: graphResult.data.status,
                    stale: graphResult.data.stale,
                    lastUpdatedSecondsAgo: graphResult.data.lastUpdatedSecondsAgo,
                    // Debug fields for troubleshooting
                    baseUrl: config.graphApi.baseUrl,
                    timeoutMs: config.graphApi.timeoutMs
                };
            } else {
                graphApi = {
                    enabled: true,
                    available: false,
                    reason: graphResult.error,
                    // Debug fields for troubleshooting
                    baseUrl: config.graphApi.baseUrl,
                    timeoutMs: config.graphApi.timeoutMs
                };
            }
        } else {
            graphApi = { enabled: false, reason: 'disabled' };
        }

        // Determine overall status based on active provider
        let overallStatus;
        if (config.graphApi.enabled) {
            // In Graph API mode, check Graph API availability
            const graphApiOk = graphApi.available === true;
            const staleOk = !config.graphApi.required || !graphApi.stale;
            overallStatus = (graphApiOk && staleOk) ? 'ok' : 'degraded';
        } else {
            // In Neo4j mode, check Neo4j connectivity
            overallStatus = providerHealth.connected ? 'ok' : 'degraded';
        }

        res.json({
            status: overallStatus,
            dataSource: config.graphApi.enabled ? 'graph-api' : 'neo4j',
            provider: {
                connected: providerHealth.connected,
                services: providerHealth.services,
                stale: providerHealth.stale,
                error: providerHealth.error
            },
            graphApi,
            config: {
                maxTraversalDepth: config.simulation.maxTraversalDepth,
                defaultLatencyMetric: config.simulation.defaultLatencyMetric,
                graphApiEnabled: config.graphApi.enabled
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
        // Handle errors with explicit statusCode (e.g., stale graph data)
        if (error.statusCode) {
            res.status(error.statusCode).json({ error: error.message });
        } else if (error.message.includes('not found')) {
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
        // Handle errors with explicit statusCode (e.g., stale graph data)
        if (error.statusCode) {
            res.status(error.statusCode).json({ error: error.message });
        } else if (error.message.includes('not found')) {
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
    console.log(`[${new Date().toISOString()}] Predictive Analysis Engine started`);
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
    const provider = getProvider();
    await provider.close();
    console.log('Provider connection closed. Bye.');
    process.exit(0);
};

process.on('SIGINT', shutdown);
process.on('SIGTERM', shutdown);
