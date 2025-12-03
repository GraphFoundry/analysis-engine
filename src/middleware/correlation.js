/**
 * Correlation ID Middleware
 * 
 * Generates a unique correlation ID for each request and attaches it to:
 * - req.correlationId (for downstream use)
 * - X-Correlation-Id response header
 * 
 * Also logs request start/end with structured context.
 */

const crypto = require('node:crypto');
const logger = require('../logger');

/**
 * Generate a UUID v4
 * @returns {string}
 */
function generateCorrelationId() {
    return crypto.randomUUID();
}

/**
 * Correlation ID middleware factory
 * @returns {Function} Express middleware
 */
function correlationMiddleware() {
    return (req, res, next) => {
        const startTime = Date.now();
        
        // Generate or use existing correlation ID (from upstream proxy)
        const correlationId = req.headers['x-correlation-id'] || generateCorrelationId();
        req.correlationId = correlationId;
        
        // Set response header
        res.setHeader('X-Correlation-Id', correlationId);
        
        // Log request start
        logger.info('request_start', {
            correlationId,
            method: req.method,
            path: req.path,
            query: Object.keys(req.query).length > 0 ? req.query : undefined
        });
        
        // Capture response finish for logging
        res.on('finish', () => {
            const durationMs = Date.now() - startTime;
            
            logger.info('request_end', {
                correlationId,
                method: req.method,
                path: req.path,
                statusCode: res.statusCode,
                durationMs
            });
        });
        
        next();
    };
}

module.exports = {
    correlationMiddleware,
    generateCorrelationId
};
