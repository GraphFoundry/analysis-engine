/**
 * Telemetry query routes
 * GET /telemetry/service - Get service metrics from InfluxDB
 * GET /telemetry/edges - Get edge metrics from InfluxDB
 */

const express = require('express');
const router = express.Router();
const { InfluxDBClient } = require('@influxdata/influxdb3-client');
const config = require('../config/config');

// Initialize InfluxDB client (singleton)
let influxClient;
if (config.influx.host && config.influx.token && config.influx.database) {
  try {
    influxClient = new InfluxDBClient({
      host: config.influx.host,
      token: config.influx.token,
      database: config.influx.database
    });
  } catch (error) {
    console.error(`Failed to initialize InfluxDB client: ${error.message}`);
  }
}

/**
 * Validate timestamp (ISO 8601)
 */
function validateTimestamp(ts) {
  const date = new Date(ts);
  return !Number.isNaN(date.getTime());
}

/**
 * Enforce max time range (7 days)
 */
function validateTimeRange(from, to) {
  const fromMs = new Date(from).getTime();
  const toMs = new Date(to).getTime();
  const maxRangeMs = 7 * 24 * 60 * 60 * 1000; // 7 days

  if (toMs - fromMs > maxRangeMs) {
    throw new Error('Time range exceeds maximum of 7 days');
  }
}

/**
 * GET /telemetry/service
 * Get time-series metrics for a service
 * 
 * Query params:
 * - service: Service name (required)
 * - from: Start timestamp ISO 8601 (required)
 * - to: End timestamp ISO 8601 (required)
 * - step: Time bucket size in seconds (optional, default: 60)
 */
router.get('/service', async (req, res) => {
  if (!config.telemetry.enabled) {
    return res.status(503).json({ 
      error: 'Telemetry endpoints disabled. Set TELEMETRY_ENABLED=true to enable.' 
    });
  }

  if (!influxClient) {
    return res.status(503).json({ 
      error: 'InfluxDB not configured. Set INFLUX_HOST, INFLUX_TOKEN, INFLUX_DATABASE' 
    });
  }

  try {
    const { service, from, to, step } = req.query;

    // Validate required params
    if (!service || !from || !to) {
      return res.status(400).json({ 
        error: 'Missing required parameters: service, from, to' 
      });
    }

    if (!validateTimestamp(from) || !validateTimestamp(to)) {
      return res.status(400).json({ 
        error: 'Invalid timestamp format. Use ISO 8601 (e.g., 2026-01-04T10:00:00Z)' 
      });
    }

    validateTimeRange(from, to);

    const stepSeconds = Number.parseInt(step) || 60;

    // SQL query for InfluxDB 3 (using DATE_BIN for time bucketing)
    const query = `
      SELECT 
        DATE_BIN(INTERVAL '${stepSeconds} seconds', time, '1970-01-01T00:00:00Z'::TIMESTAMP) AS bucket,
        service,
        namespace,
        AVG(request_rate) AS avg_request_rate,
        AVG(error_rate) AS avg_error_rate,
        AVG(p50) AS avg_p50,
        AVG(p95) AS avg_p95,
        AVG(p99) AS avg_p99,
        AVG(availability) AS avg_availability
      FROM service_metrics
      WHERE service = '${service.replaceAll("'", "''")}'
        AND time >= '${from}'
        AND time < '${to}'
      GROUP BY bucket, service, namespace
      ORDER BY bucket ASC
    `;

    const results = [];
    const reader = await influxClient.query(query, config.influx.database);

    for await (const row of reader) {
      results.push({
        timestamp: row.bucket,
        service: row.service,
        namespace: row.namespace,
        requestRate: row.avg_request_rate,
        errorRate: row.avg_error_rate,
        p50: row.avg_p50,
        p95: row.avg_p95,
        p99: row.avg_p99,
        availability: row.avg_availability
      });
    }

    res.json({
      service,
      from,
      to,
      step: stepSeconds,
      datapoints: results
    });

  } catch (error) {
    console.error('Error querying InfluxDB:', error);
    
    if (error.message.includes('Time range exceeds')) {
      return res.status(400).json({ error: error.message });
    }
    
    res.status(500).json({ error: 'Internal server error' });
  }
});

/**
 * GET /telemetry/edges
 * Get time-series metrics for edges between services
 * 
 * Query params:
 * - fromService: Source service name (optional)
 * - toService: Destination service name (optional)
 * - from: Start timestamp ISO 8601 (required)
 * - to: End timestamp ISO 8601 (required)
 * - step: Time bucket size in seconds (optional, default: 60)
 */
router.get('/edges', async (req, res) => {
  if (!config.telemetry.enabled) {
    return res.status(503).json({ 
      error: 'Telemetry endpoints disabled. Set TELEMETRY_ENABLED=true to enable.' 
    });
  }

  if (!influxClient) {
    return res.status(503).json({ 
      error: 'InfluxDB not configured. Set INFLUX_HOST, INFLUX_TOKEN, INFLUX_DATABASE' 
    });
  }

  try {
    const { fromService, toService, from, to, step } = req.query;

    // Validate required params
    if (!from || !to) {
      return res.status(400).json({ 
        error: 'Missing required parameters: from, to' 
      });
    }

    if (!validateTimestamp(from) || !validateTimestamp(to)) {
      return res.status(400).json({ 
        error: 'Invalid timestamp format. Use ISO 8601 (e.g., 2026-01-04T10:00:00Z)' 
      });
    }

    validateTimeRange(from, to);

    const stepSeconds = Number.parseInt(step) || 60;

    // Build WHERE clause
    const conditions = [
      `time >= '${from}'`,
      `time < '${to}'`
    ];

    if (fromService) {
      conditions.push(`"from" = '${fromService.replaceAll("'", "''")}'`);
    }

    if (toService) {
      conditions.push(`"to" = '${toService.replaceAll("'", "''")}'`);
    }

    // SQL query for InfluxDB 3 (using DATE_BIN for time bucketing)
    const query = `
      SELECT 
        DATE_BIN(INTERVAL '${stepSeconds} seconds', time, '1970-01-01T00:00:00Z'::TIMESTAMP) AS bucket,
        "from" AS from_service,
        "to" AS to_service,
        namespace,
        AVG(request_rate) AS avg_request_rate,
        AVG(error_rate) AS avg_error_rate,
        AVG(p50) AS avg_p50,
        AVG(p95) AS avg_p95,
        AVG(p99) AS avg_p99
      FROM edge_metrics
      WHERE ${conditions.join(' AND ')}
      GROUP BY bucket, from_service, to_service, namespace
      ORDER BY bucket ASC
    `;

    const results = [];
    const reader = await influxClient.query(query, config.influx.database);

    for await (const row of reader) {
      results.push({
        timestamp: row.bucket,
        from: row.from_service,
        to: row.to_service,
        namespace: row.namespace,
        requestRate: row.avg_request_rate,
        errorRate: row.avg_error_rate,
        p50: row.avg_p50,
        p95: row.avg_p95,
        p99: row.avg_p99
      });
    }

    res.json({
      fromService,
      toService,
      from,
      to,
      step: stepSeconds,
      datapoints: results
    });

  } catch (error) {
    console.error('Error querying InfluxDB:', error);
    
    if (error.message.includes('Time range exceeds')) {
      return res.status(400).json({ error: error.message });
    }
    
    res.status(500).json({ error: 'Internal server error' });
  }
});

module.exports = router;
