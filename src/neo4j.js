const neo4j = require('neo4j-driver');
const config = require('./config');

/**
 * Convert Neo4j Integer or other numeric types to native JS number
 * Neo4j often returns integers as neo4j.Integer objects which break Math operations
 * @param {*} value - Value to convert
 * @returns {number|null} - Native JS number or null if not convertible
 */
function toNumber(value) {
    if (value === null || value === undefined) return null;
    if (neo4j.isInt(value)) return value.toNumber();
    const n = Number(value);
    return Number.isFinite(n) ? n : null;
}

/**
 * @typedef {Object} EdgeData
 * @property {string} source - Source service ID
 * @property {string} target - Target service ID
 * @property {number} rate - Request rate (RPS)
 * @property {number} errorRate - Error rate (RPS)
 * @property {number} p50 - P50 latency (ms)
 * @property {number} p95 - P95 latency (ms)
 * @property {number} p99 - P99 latency (ms)
 */

/**
 * @typedef {Object} NodeData
 * @property {string} serviceId - Service ID (namespace:name)
 * @property {string} name - Service name
 * @property {string} namespace - Service namespace
 */

// Initialize Neo4j driver with timeout configuration
const driver = neo4j.driver(
    config.neo4j.uri,
    neo4j.auth.basic(config.neo4j.user, config.neo4j.password),
    {
        maxConnectionLifetime: 3 * 60 * 60 * 1000, // 3 hours
        maxConnectionPoolSize: 50,
        connectionAcquisitionTimeout: config.simulation.timeoutMs
    }
);

/**
 * Redact password from error messages for security
 * @param {string} message - Error message
 * @returns {string} - Redacted message
 */
function redactCredentials(message) {
    if (!message) return message;
    return message
        .replace(new RegExp(config.neo4j.password, 'g'), '[REDACTED]')
        .replace(/password=([^&\s]+)/gi, 'password=[REDACTED]');
}

/**
 * Execute a Neo4j query with timeout enforcement
 * @param {string} query - Cypher query
 * @param {Object} params - Query parameters
 * @param {number} [timeoutMs] - Optional timeout override
 * @returns {Promise<import('neo4j-driver').QueryResult>}
 */
async function executeQuery(query, params = {}, timeoutMs = config.simulation.timeoutMs) {
    const session = driver.session({
        defaultAccessMode: neo4j.session.READ
    });
    
    try {
        // Two-layer timeout enforcement
        const queryPromise = session.run(query, params, {
            timeout: timeoutMs
        });
        
        const timeoutPromise = new Promise((_, reject) => {
            setTimeout(() => reject(new Error('Query timeout exceeded')), timeoutMs);
        });
        
        const result = await Promise.race([queryPromise, timeoutPromise]);
        return result;
    } catch (error) {
        // Redact credentials from error messages
        error.message = redactCredentials(error.message);
        throw error;
    } finally {
        await session.close();
    }
}

/**
 * Check Neo4j connectivity
 * @returns {Promise<{connected: boolean, services?: number, error?: string}>}
 */
async function checkHealth() {
    try {
        const result = await executeQuery(
            'MATCH (s:Service) RETURN count(s) AS total',
            {},
            5000 // Short timeout for health check
        );
        
        return {
            connected: true,
            services: result.records[0].get('total').toNumber()
        };
    } catch (error) {
        return {
            connected: false,
            error: redactCredentials(error.message)
        };
    }
}

/**
 * Close Neo4j driver connection
 * @returns {Promise<void>}
 */
async function closeDriver() {
    await driver.close();
}

module.exports = {
    driver,
    executeQuery,
    checkHealth,
    closeDriver,
    redactCredentials,
    toNumber
};
