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

      // Try to get snapshot endpoint first (efficient single request)
      let services = [];
      let edges = [];

      const snapshotResult = await graphEngineClient.getMetricsSnapshot();
      
      if (snapshotResult.ok && snapshotResult.data) {
        console.log('[PollWorker] Using /metrics/snapshot endpoint');
        services = snapshotResult.data.services || [];
        edges = snapshotResult.data.edges || [];
      } else {
        console.warn(`[PollWorker] Snapshot endpoint failed: ${snapshotResult.error}`);
        console.log('[PollWorker] Falling back to /services + individual peer calls (expensive)');

        // Fallback: Get services list
        const servicesResult = await graphEngineClient.getServices();
        if (!servicesResult.ok) {
          throw new Error(`Failed to get services: ${servicesResult.error}`);
        }

        const servicesList = servicesResult.data.services || [];
        services = servicesList;

        // Build edges from individual peer calls (limit concurrency to 5)
        const edgesMap = new Map();
        const concurrencyLimit = 5;
        
        for (let i = 0; i < servicesList.length; i += concurrencyLimit) {
          const batch = servicesList.slice(i, i + concurrencyLimit);
          const peerResults = await Promise.all(
            batch.map(async (svc) => {
              const outResult = await graphEngineClient.getPeers(svc.name, 'out');
              return outResult.ok ? outResult.data.peers || [] : [];
            })
          );

          peerResults.flat().forEach(peer => {
            const key = `${peer.from}->${peer.to}`;
            if (!edgesMap.has(key)) {
              edgesMap.set(key, peer);
            }
          });
        }

        edges = Array.from(edgesMap.values());
      }

      // Write to InfluxDB
      if (services.length > 0) {
        await this.influxWriter.writeServiceMetrics(services);
      }

      if (edges.length > 0) {
        await this.influxWriter.writeEdgeMetrics(edges);
      }

      this.lastSuccessAt = new Date();
      console.log(`[PollWorker] Poll complete: ${services.length} services, ${edges.length} edges`);
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
