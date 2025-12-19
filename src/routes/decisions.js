/**
 * Decision logging routes
 * POST /decisions/log - Log a decision
 * GET /decisions/history - Get decision history
 */

const express = require('express');
const router = express.Router();
const DecisionStore = require('../decisionStore');
const config = require('../config');

// Initialize decision store (singleton)
let decisionStore;
try {
  decisionStore = new DecisionStore(config.sqlite.dbPath);
} catch (error) {
  console.error(`Failed to initialize DecisionStore: ${error.message}`);
}

/**
 * POST /decisions/log
 * Log a decision from Pipeline Playground
 */
router.post('/log', (req, res) => {
  if (!decisionStore) {
    return res.status(503).json({ 
      error: 'Decision store not available. Check SQLite configuration.' 
    });
  }

  try {
    const { timestamp, type, scenario, result, correlationId } = req.body;

    // Validate required fields
    if (!timestamp || !type || !scenario || !result) {
      return res.status(400).json({ 
        error: 'Missing required fields: timestamp, type, scenario, result' 
      });
    }

    // Validate timestamp format (basic check)
    if (isNaN(Date.parse(timestamp))) {
      return res.status(400).json({ 
        error: 'Invalid timestamp format. Use ISO 8601 (e.g., 2026-01-04T10:00:00Z)' 
      });
    }

    // Validate type
    const validTypes = ['failure', 'scaling', 'risk'];
    if (!validTypes.includes(type)) {
      return res.status(400).json({ 
        error: `Invalid type. Must be one of: ${validTypes.join(', ')}` 
      });
    }

    const inserted = decisionStore.logDecision({
      timestamp,
      type,
      scenario,
      result,
      correlationId
    });

    res.status(201).json(inserted);
  } catch (error) {
    console.error('Error logging decision:', error);
    res.status(500).json({ error: 'Internal server error' });
  }
});

/**
 * GET /decisions/history
 * Get decision history with pagination and optional type filter
 */
router.get('/history', (req, res) => {
  if (!decisionStore) {
    return res.status(503).json({ 
      error: 'Decision store not available. Check SQLite configuration.' 
    });
  }

  try {
    const limit = Number.parseInt(req.query.limit) || 50;
    const offset = Number.parseInt(req.query.offset) || 0;
    const { type } = req.query;

    // Get decisions and total count
    const decisions = decisionStore.getHistory({ limit, offset, type });
    const total = decisionStore.getCount(type);

    res.json({
      decisions,
      pagination: {
        limit,
        offset,
        total
      }
    });
  } catch (error) {
    console.error('Error retrieving decision history:', error);
    res.status(500).json({ error: 'Internal server error' });
  }
});

module.exports = router;
