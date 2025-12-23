const express = require('express');
const config = require('./src/config/config');
const { validateEnv } = require('./src/config/config');
const { getProvider } = require('./src/storage/providers');
const { checkGraphHealth, getServices, getMetricsSnapshot } = require('./src/clients/graphEngineClient');
const { simulateFailure } = require('./src/simulation/failureSimulation');
const { simulateScaling } = require('./src/simulation/scalingSimulation');
const { getTopRiskServices } = require('./src/simulation/riskAnalysis');
const { correlationMiddleware } = require('./src/middleware/correlation');
const { rateLimitMiddleware } = require('./src/middleware/rateLimit');
const { setupSwagger } = require('./src/utils/swagger');
const { parseTraceOptions } = require('./src/utils/traceOptions');
const { createTrace } = require('./src/utils/trace');
const { getWorker } = require('./src/telemetry/pollWorker');
const { getDecisionStore, closeDecisionStore } = require('./src/storage/decisionStoreSingleton');
const {
    parseServiceIdentifier,
    normalizePodParams,
    validateScalingParams,
    validateLatencyMetric,
    validateDepth,
    validateScalingModel
} = require('./src/utils/validator');

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
            telemetry: {
                enabled: config.telemetry.enabled,
                workerEnabled: config.telemetryWorker.enabled
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

/**
 * GET /services
 * List all discovered services from the graph
 * Returns normalized serviceId (namespace:name) for UI consumption
 */
app.get('/services', async (req, res) => {
    try {
        // Fetch snapshot (services + edges) and health in parallel
        // We use getMetricsSnapshot because it returns the edges, unlike getServices
        const [snapshotResult, healthResult] = await Promise.all([
            getMetricsSnapshot(),
            checkGraphHealth()
        ]);

        // Extract freshness info from health result
        let stale = true;
        let lastUpdatedSecondsAgo = null;
        let windowMinutes = 5;

        if (healthResult.ok && healthResult.data) {
            stale = healthResult.data.stale ?? true;
            lastUpdatedSecondsAgo = healthResult.data.lastUpdatedSecondsAgo ?? null;
            windowMinutes = healthResult.data.windowMinutes ?? 5;
        }

        // Handle snapshot fetch failure
        if (!snapshotResult.ok) {
            // Fallback: try basic getServices if snapshot fails (e.g. no metrics yet)
            console.warn('Snapshot failed, falling back to basic services list:', snapshotResult.error);
            const servicesResult = await getServices();

            if (!servicesResult.ok) {
                return res.status(503).json({
                    error: servicesResult.error || 'Failed to fetch services from Graph Engine',
                    services: [],
                    count: 0,
                    stale: true,
                    lastUpdatedSecondsAgo: null,
                    windowMinutes
                });
            }

            const rawServices = servicesResult.data?.services || [];
            const services = rawServices.map(svc => ({
                serviceId: `${svc.namespace || 'default'}:${svc.name}`,
                name: svc.name,
                namespace: svc.namespace || 'default'
            }));

            return res.json({
                services,
                count: services.length,
                stale,
                lastUpdatedSecondsAgo,
                windowMinutes
            });
        }

        // Process Snapshot Data
        const rawServices = snapshotResult.data?.services || [];
        const rawEdges = snapshotResult.data?.edges || [];

        const services = rawServices.map(svc => ({
            serviceId: `${svc.namespace || 'default'}:${svc.name}`,
            name: svc.name,
            namespace: svc.namespace || 'default'
        }));

        const serviceMap = new Map();
        services.forEach(s => serviceMap.set(s.name, s.namespace));

        const edges = rawEdges.map(e => {
            const fromNs = serviceMap.get(e.from) || 'default';
            const toNs = e.namespace || 'default';
            return {
                source: `${fromNs}:${e.from}`,
                target: `${toNs}:${e.to}`
            };
        });

        res.json({
            services,
            count: services.length,
            stale,
            lastUpdatedSecondsAgo,
            windowMinutes,
            edges
        });

    } catch (error) {
        // Graph Engine unreachable - return 503 with empty services
        res.status(503).json({
            error: error.message || 'Graph Engine unreachable',
            services: [],
            count: 0,
            stale: true,
            lastUpdatedSecondsAgo: null,
            windowMinutes: 5
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
        // Parse trace options from query string
        const traceOptions = parseTraceOptions(req.query);
        const trace = createTrace(traceOptions);

        // Validate and parse request (inside trace stage)
        const { identifier, maxDepth: resolvedMaxDepth } = await trace.stage('scenario-parse', async () => {
            const id = parseServiceIdentifier(req.body);
            const depth = validateDepth(
                req.body.maxDepth,
                config.simulation.maxTraversalDepth,
                config.simulation.maxTraversalDepth
            );
            return { identifier: id, maxDepth: depth };
        });

        // Add scenario-parse summary to trace
        trace.setSummary('scenario-parse', {
            serviceIdResolved: identifier.serviceId,
            maxDepth: resolvedMaxDepth
        });

        // Execute simulation with timeout
        const simulationPromise = simulateFailure({
            serviceId: identifier.serviceId,
            maxDepth: resolvedMaxDepth
        }, {
            traceOptions,
            trace,
            correlationId: req.correlationId
        });

        const timeoutPromise = new Promise((_, reject) => {
            setTimeout(
                () => reject(new Error('Simulation timeout exceeded')),
                config.simulation.timeoutMs
            );
        });

        const result = await Promise.race([simulationPromise, timeoutPromise]);

        // Add correlationId to body only when trace enabled
        if (traceOptions.trace && req.correlationId) {
            result.correlationId = req.correlationId;
        }

        // Auto-log decision to SQLite (best-effort, silent failure)
        const decisionStore = getDecisionStore();
        if (decisionStore) {
            try {
                const inserted = decisionStore.logDecision({
                    timestamp: new Date().toISOString(),
                    type: 'failure',
                    scenario: {
                        serviceId: identifier.serviceId,
                        maxDepth: resolvedMaxDepth
                    },
                    result: {
                        totalLostTrafficRps: result.totalLostTrafficRps,
                        affectedCallersCount: result.affectedCallers?.length || 0,
                        affectedDownstreamCount: result.affectedDownstream?.length || 0,
                        unreachableCount: result.unreachableServices?.length || 0,
                        confidence: result.confidence
                    },
                    correlationId: req.correlationId
                });

                // Debug logging (guarded by env var)
                if (process.env.DEBUG_DECISIONS === 'true') {
                    console.log(`[DecisionStore Debug] Auto-logged failure: id=${inserted.id}, serviceId=${identifier.serviceId}`);
                }
            } catch (error_) {
                console.error('[DecisionStore] Auto-log failed (non-blocking):', error_.message);
            }
        }

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
        // Parse trace options from query string
        const traceOptions = parseTraceOptions(req.query);
        const trace = createTrace(traceOptions);

        // Validate and parse request (inside trace stage)
        const { identifier, newPods, latencyMetric: resolvedLatencyMetric, maxDepth: resolvedMaxDepth, model: resolvedModel } = await trace.stage('scenario-parse', async () => {
            const id = parseServiceIdentifier(req.body);
            const pods = normalizePodParams(req.body);
            validateScalingParams(req.body.currentPods, pods);
            const metric = validateLatencyMetric(
                req.body.latencyMetric,
                config.simulation.defaultLatencyMetric
            );
            const depth = validateDepth(
                req.body.maxDepth,
                config.simulation.maxTraversalDepth,
                config.simulation.maxTraversalDepth
            );
            const m = validateScalingModel(req.body.model);
            return {
                identifier: id,
                newPods: pods,
                latencyMetric: metric,
                maxDepth: depth,
                model: m
            };
        });

        // Add scenario-parse summary to trace
        trace.setSummary('scenario-parse', {
            serviceIdResolved: identifier.serviceId,
            maxDepth: resolvedMaxDepth,
            latencyMetric: resolvedLatencyMetric,
            model: resolvedModel
        });

        // Execute simulation with timeout
        const simulationPromise = simulateScaling({
            serviceId: identifier.serviceId,
            currentPods: req.body.currentPods,
            newPods,
            latencyMetric: resolvedLatencyMetric,
            model: resolvedModel,
            maxDepth: resolvedMaxDepth
        }, {
            traceOptions,
            trace,
            correlationId: req.correlationId
        });

        const timeoutPromise = new Promise((_, reject) => {
            setTimeout(
                () => reject(new Error('Simulation timeout exceeded')),
                config.simulation.timeoutMs
            );
        });

        const result = await Promise.race([simulationPromise, timeoutPromise]);

        // Add correlationId to body only when trace enabled
        if (traceOptions.trace && req.correlationId) {
            result.correlationId = req.correlationId;
        }

        // Auto-log decision to SQLite (best-effort, silent failure)
        const decisionStore = getDecisionStore();
        if (decisionStore) {
            try {
                const inserted = decisionStore.logDecision({
                    timestamp: new Date().toISOString(),
                    type: 'scaling',
                    scenario: {
                        serviceId: identifier.serviceId,
                        currentPods: req.body.currentPods,
                        newPods,
                        latencyMetric: resolvedLatencyMetric,
                        maxDepth: resolvedMaxDepth
                    },
                    result: {
                        predictedLatencyReduction: result.predictedLatencyReduction,
                        latencyMetric: result.latencyMetric,
                        affectedDownstreamCount: result.affectedDownstream?.length || 0,
                        confidence: result.confidence
                    },
                    correlationId: req.correlationId
                });

                // Debug logging (guarded by env var)
                if (process.env.DEBUG_DECISIONS === 'true') {
                    console.log(`[DecisionStore Debug] Auto-logged scaling: id=${inserted.id}, serviceId=${identifier.serviceId}`);
                }
            } catch (error_) {
                console.error('[DecisionStore] Auto-log failed (non-blocking):', error_.message);
            }
        }

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
        const limit = Math.min(Math.max(Number.parseInt(req.query.limit) || 5, 1), 20);

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

// Decision logging routes
const decisionsRouter = require('./src/routes/decisions');
app.use('/decisions', decisionsRouter);

// Telemetry query routes
const telemetryRouter = require('./src/routes/telemetry');
app.use('/telemetry', telemetryRouter);

// Start server
const server = app.listen(config.server.port, () => {
    console.log(`[${new Date().toISOString()}] Predictive Analysis Engine started`);
    console.log(`Port: ${config.server.port}`);
    console.log(`Max traversal depth: ${config.simulation.maxTraversalDepth}`);
    console.log(`Default latency metric: ${config.simulation.defaultLatencyMetric}`);
    console.log(`Scaling model: ${config.simulation.scalingModel} (alpha: ${config.simulation.scalingAlpha})`);
    console.log(`Timeout: ${config.simulation.timeoutMs}ms`);

    // Initialize DecisionStore singleton at startup
    getDecisionStore();

    // Start background telemetry poll worker
    const pollWorker = getWorker();
    pollWorker.start();
});

// Graceful shutdown
const shutdown = async () => {
    console.log('\nShutting down service...');

    // Stop poll worker
    const pollWorker = getWorker();
    await pollWorker.stop();

    // Close decision store
    await closeDecisionStore();

    server.close();
    const provider = getProvider();
    await provider.close();
    console.log('Provider connection closed. Bye.');
    process.exit(0);
};

process.on('SIGINT', shutdown);
process.on('SIGTERM', shutdown);
