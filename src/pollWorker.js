/**
 * Background Poll Worker
 * Polls Graph Engine API and writes metrics to InfluxDB
 */

const graphEngineClient = require('./graphEngineClient');
const InfluxWriter = require('./influxWriter');
const config = require('./config');

class PollWorker {
  constructor() {
    this.influxWriter = new InfluxWriter();
    this.intervalId = null;
    this.isRunning = false;
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
    try {
      console.log('[PollWorker] Polling Graph Engine...');

      // Try to get snapshot endpoint first (efficient single request)
      let data;
      try {
        data = await graphEngineClient.getMetricsSnapshot();
        console.log('[PollWorker] Using /metrics/snapshot endpoint');
      } catch (snapshotError) {
        console.warn(`[PollWorker] Snapshot endpoint unavailable: ${snapshotError.message}`);
        console.log('[PollWorker] Falling back to /services + /peers (expensive)');

        // Fallback to multi-request approach
        const [services, peers] = await Promise.all([
          graphEngineClient.getServices(),
          graphEngineClient.getPeers()
        ]);

        data = {
          services: services.services || [],
          peers: peers.peers || []
        };
      }

      // Write to InfluxDB
      if (data.services && data.services.length > 0) {
        await this.influxWriter.writeServiceMetrics(data.services);
      }

      if (data.peers && data.peers.length > 0) {
        await this.influxWriter.writeEdgeMetrics(data.peers);
      }

      console.log(`[PollWorker] Poll complete: ${data.services?.length || 0} services, ${data.peers?.length || 0} edges`);
    } catch (error) {
      console.error(`[PollWorker] Poll failed: ${error.message}`);
      // Continue running despite errors
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
