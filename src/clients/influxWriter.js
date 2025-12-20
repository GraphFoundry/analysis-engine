/**
 * InfluxDB 3 Writer
 * Writes service and edge metrics to InfluxDB using line protocol
 */

const { InfluxDBClient } = require('@influxdata/influxdb3-client');
const config = require('../config/config');

class InfluxWriter {
  constructor() {
    this.client = null;
    this.database = config.influx.database;
    
    if (config.influx.host && config.influx.token && config.influx.database) {
      try {
        this.client = new InfluxDBClient({
          host: config.influx.host,
          token: config.influx.token,
          database: config.influx.database
        });
        console.log(`[InfluxDB] Writer initialized for database: ${this.database}`);
      } catch (error) {
        console.error(`[InfluxDB] Failed to initialize client: ${error.message}`);
      }
    } else {
      console.warn('[InfluxDB] Writer not configured (missing INFLUX_HOST, INFLUX_TOKEN, or INFLUX_DATABASE)');
    }
  }

  /**
   * Write service metrics to InfluxDB
   * @param {Array} services - Array of service objects with metrics
   */
  async writeServiceMetrics(services) {
    if (!this.client) {
      console.warn('[InfluxDB] Client not configured, skipping service metrics write');
      return;
    }

    if (!services || services.length === 0) {
      return;
    }

    try {
      const lines = services.map(svc => {
        const tags = `service=${this.escapeTag(svc.name)},namespace=${this.escapeTag(svc.namespace || 'default')}`;
        const fields = [
          `request_rate=${this.formatNumber(svc.requestRate)}`,
          `error_rate=${this.formatNumber(svc.errorRate)}`,
          `p50=${this.formatNumber(svc.p50)}`,
          `p95=${this.formatNumber(svc.p95)}`,
          `p99=${this.formatNumber(svc.p99)}`,
          `availability=${this.formatNumber(svc.availability)}`
        ].join(',');
        
        return `service_metrics,${tags} ${fields}`;
      });

      await this.client.write(lines.join('\n'), this.database);
      console.log(`[InfluxDB] Wrote ${services.length} service metrics`);
    } catch (error) {
      console.error(`[InfluxDB] Error writing service metrics: ${error.message}`);
    }
  }

  /**
   * Write edge metrics to InfluxDB
   * @param {Array} edges - Array of edge objects with metrics
   */
  async writeEdgeMetrics(edges) {
    if (!this.client) {
      console.warn('[InfluxDB] Client not configured, skipping edge metrics write');
      return;
    }

    if (!edges || edges.length === 0) {
      return;
    }

    try {
      const lines = edges.map(edge => {
        const tags = `from=${this.escapeTag(edge.from)},to=${this.escapeTag(edge.to)},namespace=${this.escapeTag(edge.namespace || 'default')}`;
        const fields = [
          `request_rate=${this.formatNumber(edge.requestRate)}`,
          `error_rate=${this.formatNumber(edge.errorRate)}`,
          `p50=${this.formatNumber(edge.p50)}`,
          `p95=${this.formatNumber(edge.p95)}`,
          `p99=${this.formatNumber(edge.p99)}`
        ].join(',');
        
        return `edge_metrics,${tags} ${fields}`;
      });

      await this.client.write(lines.join('\n'), this.database);
      console.log(`[InfluxDB] Wrote ${edges.length} edge metrics`);
    } catch (error) {
      console.error(`[InfluxDB] Error writing edge metrics: ${error.message}`);
    }
  }

  /**
   * Escape tag values for InfluxDB line protocol
   */
  escapeTag(value) {
    if (!value) return 'unknown';
    return String(value).replace(/[, =]/g, '\\$&');
  }

  /**
   * Format number values, handling null/undefined
   */
  formatNumber(value) {
    if (value === null || value === undefined || isNaN(value)) {
      return '0';
    }
    return String(value);
  }

  /**
   * Close the InfluxDB client
   */
  async close() {
    if (this.client) {
      try {
        await this.client.close();
        console.log('[InfluxDB] Writer closed');
      } catch (error) {
        console.error(`[InfluxDB] Error closing writer: ${error.message}`);
      }
    }
  }
}

module.exports = InfluxWriter;
