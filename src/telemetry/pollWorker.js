/**
 * Background Poll Worker
 * Polls Graph Engine API and writes metrics to InfluxDB
 */

const graphEngineClient = require('../clients/graphEngineClient');
const InfluxWriter = require('../clients/influxWriter');
const config = require('../config/config');

class PollWorker {
  constructor() {
    this.influxWriter = new InfluxWriter();
    this.intervalId = null;
    this.isRunning = false;
    this.polling = false;
    this.lastPollAt = null;
    this.lastSuccessAt = null;
  }

  /**
   * Start the poll worker
   */
  start() {
    if (!config.telemetryWorker.enabled) {
      console.log('[PollWorker] Disabled (TELEMETRY_WORKER_ENABLED=false)');
      return;
    }

    if (this.isRunning) {
      console.warn('[PollWorker] Already running');
      return;
    }

    console.log(`[PollWorker] Starting with ${config.telemetryWorker.pollIntervalMs}ms interval`);
    this.isRunning = true;

    // Run immediately on start
    this.poll();

    // Schedule recurring polls
    this.intervalId = setInterval(() => {
      this.poll();
    }, config.telemetryWorker.pollIntervalMs);
  }

  /**
   * Stop the poll worker
   */
  async stop() {
    if (!this.isRunning) {
      return;
    }

    console.log('[PollWorker] Stopping...');
    this.isRunning = false;

    if (this.intervalId) {
      clearInterval(this.intervalId);
      this.intervalId = null;
    }

    await this.influxWriter.close();
    console.log('[PollWorker] Stopped');
  }

  /**
   * Execute one poll cycle
   */
  async poll() {
    // Overlap protection - skip if previous poll still running
    if (this.polling) {
      console.warn('[PollWorker] Previous poll still running, skipping this cycle');
      return;
    }

    this.polling = true;
    this.lastPollAt = new Date();

    try {
      console.log('[PollWorker] Polling Graph Engine...');

      // 1. Fetch Request/Latency Metrics (via Snapshot)
      let services = [];
      let edges = [];

      try {
        const snapshotResult = await graphEngineClient.getMetricsSnapshot();

        if (snapshotResult.ok && snapshotResult.data) {
          // Transform Graph Engine schema to InfluxDB schema
          services = (snapshotResult.data.services || []).map(svc => {
            const hasTraffic = svc.rps && svc.rps > 0;
            return {
              name: svc.name,
              namespace: svc.namespace,
              requestRate: svc.rps ?? null,
              errorRate: hasTraffic ? svc.errorRate : null,
              p50: null,
              p95: hasTraffic ? svc.p95 : null,
              p99: null,
              availability: null
            };
          });

          edges = (snapshotResult.data.edges || []).map(edge => {
            const hasTraffic = edge.rps && edge.rps > 0;
            return {
              from: edge.from,
              to: edge.to,
              namespace: edge.namespace,
              requestRate: edge.rps ?? null,
              errorRate: hasTraffic ? edge.errorRate : null,
              p50: null,
              p95: hasTraffic ? edge.p95 : null,
              p99: null
            };
          });
        }
      } catch (err) {
        console.error(`[PollWorker] Snapshot fetch failed: ${err.message}`);
      }

      // 2. Fetch Infrastructure Metrics (via Services with Placement)
      // This is a separate call because snapshot is optimized for light edges
      let infraData = { nodes: [], services: [] };
      try {
        const servicesResult = await graphEngineClient.getServicesWithPlacement();
        if (servicesResult.ok && servicesResult.data && servicesResult.data.services) {
          // Extract Nodes from the services list (Graph Engine returns services -> placement -> nodes)
          // We need to de-duplicate nodes since multiple services run on same nodes
          const nodeMap = new Map();

          servicesResult.data.services.forEach(svc => {
            if (svc.placement && svc.placement.nodes) {
              svc.placement.nodes.forEach(node => {
                if (!node.node) return;
                if (!nodeMap.has(node.node)) {
                  nodeMap.set(node.node, node);
                } else {
                  // Merge pods
                  const existing = nodeMap.get(node.node);
                  if (node.pods && node.pods.length > 0) {
                    // Add pods that aren't already listed (simple check by name)
                    node.pods.forEach(p => {
                      if (!existing.pods.some(ep => ep.name === p.name)) {
                        existing.pods.push(p);
                      }
                    });
                  }
                }
              });
            }
          });

          infraData.nodes = Array.from(nodeMap.values());
        }
      } catch (err) {
        console.error(`[PollWorker] Infra fetch failed: ${err.message}`);
      }

      // 3. Write to InfluxDB
      const promises = [];

      if (services.length > 0) {
        promises.push(this.influxWriter.writeServiceMetrics(services));
      }

      if (edges.length > 0) {
        promises.push(this.influxWriter.writeEdgeMetrics(edges));
      }

      if (infraData.nodes.length > 0) {
        promises.push(this.influxWriter.writeInfrastructureMetrics(infraData));
      }

      await Promise.all(promises);

      this.lastSuccessAt = new Date();
      console.log(`[PollWorker] Poll complete: ${services.length} services, ${edges.length} edges, ${infraData.nodes.length} nodes`);

    } catch (error) {
      console.error(`[PollWorker] Poll failed: ${error.message}`);
      // Continue running despite errors
    } finally {
      this.polling = false;
    }
  }
}

// Singleton instance
let workerInstance = null;

/**
 * Get or create the singleton poll worker instance
 */
function getWorker() {
  if (!workerInstance) {
    workerInstance = new PollWorker();
  }
  return workerInstance;
}

module.exports = {
  getWorker,
  PollWorker
};
