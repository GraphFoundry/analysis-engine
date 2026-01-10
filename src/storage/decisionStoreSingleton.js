/**
 * DecisionStore Singleton
 * Ensures only one DecisionStore instance across the application
 */

const DecisionStore = require('./decisionStore');
const config = require('../config/config');

let instance = null;

/**
 * Get or create the singleton DecisionStore instance
 * @returns {DecisionStore|null} DecisionStore instance or null if initialization failed
 */
function getDecisionStore() {
  if (!instance) {
    try {
      instance = new DecisionStore(config.sqlite.dbPath);
    } catch (error) {
      console.error(`[DecisionStoreSingleton] Failed to initialize: ${error.message}`);
      return null;
    }
  }
  return instance;
}

/**
 * Close the DecisionStore connection (for graceful shutdown)
 */
async function closeDecisionStore() {
  if (instance && instance.db) {
    try {
      instance.db.close();
      console.log('[DecisionStore] Connection closed');
      instance = null;
    } catch (error) {
      console.error(`[DecisionStore] Error closing: ${error.message}`);
    }
  }
}

module.exports = {
  getDecisionStore,
  closeDecisionStore
};
