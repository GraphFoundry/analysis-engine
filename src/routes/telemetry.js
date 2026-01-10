/**
 * Telemetry query routes
 * GET /telemetry/service - Get service metrics from InfluxDB
 * GET /telemetry/edges - Get edge metrics from InfluxDB
 */

const express = require('express');
const router = express.Router();
const telemetryService = require('../services/telemetryService');

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
 */
router.get('/service', async (req, res) => {
  const status = telemetryService.checkStatus();
  if (!status.enabled) {
    return res.status(503).json({ error: status.error });
  }

  try {
    const { service, from, to, step } = req.query;

    if (!from || !to) {
      return res.status(400).json({ error: 'Missing required parameters: from, to' });
    }

    if (!validateTimestamp(from) || !validateTimestamp(to)) {
      return res.status(400).json({ error: 'Invalid timestamp format' });
    }

    validateTimeRange(from, to);

    const stepSeconds = Number.parseInt(step) || 60;
    const results = await telemetryService.getServiceMetrics(service, from, to, stepSeconds);

    res.json({
      service: service || 'all',
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
 */
router.get('/edges', async (req, res) => {
  const status = telemetryService.checkStatus();
  if (!status.enabled) {
    return res.status(503).json({ error: status.error });
  }

  try {
    const { fromService, toService, from, to, step } = req.query;

    if (!from || !to) {
      return res.status(400).json({ error: 'Missing required parameters: from, to' });
    }

    if (!validateTimestamp(from) || !validateTimestamp(to)) {
      return res.status(400).json({ error: 'Invalid timestamp format' });
    }

    validateTimeRange(from, to);

    const stepSeconds = Number.parseInt(step) || 60;
    const results = await telemetryService.getEdgeMetrics(fromService, toService, from, to, stepSeconds);

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
