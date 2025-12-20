/**
 * SQLite Decision Store
 * Stores decision logs from Pipeline Playground for audit trail and analysis
 */

const Database = require('better-sqlite3');
const fs = require('node:fs');
const path = require('node:path');
const config = require('../config/config');

class DecisionStore {
  constructor(dbPath = config.sqlite.dbPath) {
    this.dbPath = dbPath;
    this.db = null;
    this.init();
  }

  /**
   * Initialize database connection and schema
   */
  init() {
    try {
      // Ensure data directory exists
      const dir = path.dirname(this.dbPath);
      if (!fs.existsSync(dir)) {
        fs.mkdirSync(dir, { recursive: true });
      }

      // Open database with WAL mode for better concurrency
      this.db = new Database(this.dbPath);
      this.db.pragma('journal_mode = WAL');

      // Create schema if not exists
      this.db.exec(`
        CREATE TABLE IF NOT EXISTS decisions (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          timestamp TEXT NOT NULL,
          type TEXT NOT NULL,
          scenario TEXT NOT NULL,
          result TEXT NOT NULL,
          correlation_id TEXT,
          created_at TEXT DEFAULT CURRENT_TIMESTAMP
        );

        CREATE INDEX IF NOT EXISTS idx_decisions_timestamp ON decisions(timestamp DESC);
        CREATE INDEX IF NOT EXISTS idx_decisions_type ON decisions(type);
        CREATE INDEX IF NOT EXISTS idx_decisions_correlation_id ON decisions(correlation_id);
      `);

      console.log(`[DecisionStore] Initialized at ${this.dbPath}`);
    } catch (error) {
      console.error(`[DecisionStore] Initialization failed: ${error.message}`);
      this.db = null;
    }
  }

  /**
   * Log a decision
   * @param {Object} decision - Decision data
   * @param {string} decision.timestamp - ISO 8601 timestamp
   * @param {string} decision.type - Decision type (failure, scaling, risk)
   * @param {Object} decision.scenario - Scenario parameters
   * @param {Object} decision.result - Simulation result
   * @param {string} [decision.correlationId] - Optional correlation ID
   * @returns {Object} Inserted record with id
   */
  logDecision({ timestamp, type, scenario, result, correlationId }) {
    if (!this.db) {
      throw new Error('Database not initialized');
    }

    const stmt = this.db.prepare(`
      INSERT INTO decisions (timestamp, type, scenario, result, correlation_id)
      VALUES (?, ?, ?, ?, ?)
    `);

    const info = stmt.run(
      timestamp,
      type,
      JSON.stringify(scenario),
      JSON.stringify(result),
      correlationId || null
    );

    return {
      id: info.lastInsertRowid,
      timestamp
    };
  }

  /**
   * Get decision history with pagination and optional type filter
   * @param {Object} options - Query options
   * @param {number} [options.limit=50] - Page size (max 100)
   * @param {number} [options.offset=0] - Pagination offset
   * @param {string} [options.type] - Filter by decision type
   * @returns {Array} Array of decision records
   */
  getHistory({ limit = 50, offset = 0, type } = {}) {
    if (!this.db) {
      throw new Error('Database not initialized');
    }

    // Enforce limits
    limit = Math.min(Math.max(1, limit), 100);
    offset = Math.max(0, offset);

    let query = 'SELECT * FROM decisions';
    const params = [];

    if (type) {
      query += ' WHERE type = ?';
      params.push(type);
    }

    query += ' ORDER BY timestamp DESC LIMIT ? OFFSET ?';
    params.push(limit, offset);

    const stmt = this.db.prepare(query);
    const rows = stmt.all(...params);

    return rows.map(row => ({
      id: row.id,
      timestamp: row.timestamp,
      type: row.type,
      scenario: JSON.parse(row.scenario),
      result: JSON.parse(row.result),
      correlationId: row.correlation_id,
      createdAt: row.created_at
    }));
  }

  /**
   * Get total count of decisions (optionally filtered by type)
   * @param {string} [type] - Optional type filter
   * @returns {number} Total count
   */
  getCount(type) {
    if (!this.db) {
      throw new Error('Database not initialized');
    }

    let query = 'SELECT COUNT(*) as count FROM decisions';
    const params = [];

    if (type) {
      query += ' WHERE type = ?';
      params.push(type);
    }

    const stmt = this.db.prepare(query);
    const result = stmt.get(...params);
    return result.count;
  }

  /**
   * Close database connection
   */
  close() {
    if (this.db) {
      this.db.close();
      console.log('[DecisionStore] Database closed');
    }
  }
}

module.exports = DecisionStore;
