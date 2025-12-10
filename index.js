const express = require('express');
const config = require('./src/config');
const { validateEnv } = require('./src/config');
const { getProvider } = require('./src/providers');
const { checkGraphHealth } = require('./src/graphEngineClient');
const { simulateFailure } = require('./src/failureSimulation');
const { simulateScaling } = require('./src/scalingSimulation');
const { getTopRiskServices } = require('./src/riskAnalysis');
const { correlationMiddleware } = require('./src/middleware/correlation');
const { rateLimitMiddleware } = require('./src/middleware/rateLimit');
const { setupSwagger } = require('./src/swagger');
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

// Swagger UI (conditional - only if ENABLE_SWAGGER=true)
setupSwagger(app);

// Correlation ID middleware (generates UUID, sets X-Correlation-Id header, logs requests)
app.use(correlationMiddleware());

// Track server start time
const startTime = Date.now();

/**
 * Health check endpoint
 * Returns Graph Engine connectivity status and config info
 * Always returns HTTP 200 with status: "ok" | "degraded"
 */
app.get('/health', async (req, res) => {
    try {
        const uptimeSeconds = Math.round((Date.now() - startTime) / 100) / 10;
        
        // Check Graph Engine health
        const graphResult = await checkGraphHealth();
        
        let status = 'ok';
        let graphApi;
        
        if (graphResult.ok) {
            const { stale, lastUpdatedSecondsAgo } = graphResult.data;
            
            // Status is degraded if graph is stale
            if (stale) {
                status = 'degraded';
            }
            
            graphApi = {
                connected: true,
                status: graphResult.data.status,
                stale,
                lastUpdatedSecondsAgo,
                baseUrl: config.graphApi.baseUrl,
                timeoutMs: config.graphApi.timeoutMs
            };
        } else {
            // Graph Engine unavailable = always degraded
            status = 'degraded';
            graphApi = {
                connected: false,
                error: graphResult.error,
                baseUrl: config.graphApi.baseUrl,
                timeoutMs: config.graphApi.timeoutMs
            };
        }

        res.json({
            status,
            provider: 'graph-engine',
            graphApi,
            config: {
                maxTraversalDepth: config.simulation.maxTraversalDepth,
                defaultLatencyMetric: config.simulation.defaultLatencyMetric
            },
            uptimeSeconds
        });
    } catch (error) {
        // Always return 200 even on error, with degraded status
        res.json({
            status: 'degraded',
            provider: 'graph-engine',
            error: error.message,
            uptimeSeconds: Math.round((Date.now() - startTime) / 100) / 10
        });
    }
});

// Rate limiter for simulation endpoints
const simulationRateLimiter = rateLimitMiddleware();

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
app.post('/simulate/failure', simulationRateLimiter, async (req, res) => {
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
app.post('/simulate/scale', simulationRateLimiter, async (req, res) => {
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

/**
 * GET /risk/services/top
 * Get top services by risk (based on centrality metrics)
 * 
 * Query params:
 * - metric: string (optional, 'pagerank' or 'betweenness', default: 'pagerank')
 * - limit: number (optional, 1-20, default: 5)
 */
app.get('/risk/services/top', async (req, res) => {
    try {
        const metric = req.query.metric || 'pagerank';
        const limit = Math.min(Math.max(parseInt(req.query.limit) || 5, 1), 20);
        
        const result = await getTopRiskServices({ metric, limit });
        
        res.json(result);
    } catch (error) {
        if (error.message.includes('Invalid metric')) {
            res.status(400).json({ error: error.message });
        } else if (error.message.includes('disabled')) {
            res.status(503).json({ error: 'Graph API is not enabled' });
        } else if (error.message.toLowerCase().includes('timeout')) {
            res.status(504).json({ error: 'Graph API timeout' });
        } else {
            console.error('Risk analysis error:', error);
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
