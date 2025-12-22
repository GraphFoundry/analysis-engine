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
        // Note: InfluxDB 3 client uses nanosecond precision by default
        // Timestamps in line protocol are automatically handled
        console.log(`[InfluxDB] Writer initialized for database: ${this.database} (precision: nanoseconds)`);
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
        
        // Build fields array, filtering out null values
        const fieldPairs = [
          { key: 'request_rate', value: this.formatNumber(svc.requestRate) },
          { key: 'error_rate', value: this.formatNumber(svc.errorRate) },
          { key: 'p50', value: this.formatNumber(svc.p50) },
          { key: 'p95', value: this.formatNumber(svc.p95) },
          { key: 'p99', value: this.formatNumber(svc.p99) },
          { key: 'availability', value: this.formatNumber(svc.availability) }
        ].filter(f => f.value !== null);
        
        // Skip if no valid fields
        if (fieldPairs.length === 0) return null;
        
        const fields = fieldPairs.map(f => `${f.key}=${f.value}`).join(',');
        return `service_metrics,${tags} ${fields}`;
      }).filter(line => line !== null);

      if (lines.length === 0) {
        console.log('[InfluxDB] No valid service metrics to write (all null)');
        return;
      }

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
        
        // Build fields array, filtering out null values
        const fieldPairs = [
          { key: 'request_rate', value: this.formatNumber(edge.requestRate) },
          { key: 'error_rate', value: this.formatNumber(edge.errorRate) },
          { key: 'p50', value: this.formatNumber(edge.p50) },
          { key: 'p95', value: this.formatNumber(edge.p95) },
          { key: 'p99', value: this.formatNumber(edge.p99) }
        ].filter(f => f.value !== null);
        
        // Skip if no valid fields
        if (fieldPairs.length === 0) return null;
        
        const fields = fieldPairs.map(f => `${f.key}=${f.value}`).join(',');
        return `edge_metrics,${tags} ${fields}`;
      }).filter(line => line !== null);

      if (lines.length === 0) {
        console.log('[InfluxDB] No valid edge metrics to write (all null)');
        return;
      }

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
   * Returns null for missing values (InfluxDB line protocol omits null fields)
   * This ensures averages don't include zeros for missing data
   */
  formatNumber(value) {
    if (value === null || value === undefined || Number.isNaN(value)) {
      return null;
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
