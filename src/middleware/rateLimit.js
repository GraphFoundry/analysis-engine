/**
 * Rate Limiting Middleware
 * 
 * In-memory sliding window rate limiter.
 * Uses req.socket.remoteAddress as client identifier.
 * 
 * Returns 429 Too Many Requests when limit exceeded.
 */

const config = require('../config/config');
const logger = require('../utils/logger');

/**
 * In-memory store for request timestamps per client
 * @type {Map<string, number[]>}
 */
const requestStore = new Map();

/**
 * Clean up old entries from the store (called periodically)
 * @param {number} windowMs 
 */
function cleanup(windowMs) {
    const now = Date.now();
    for (const [key, timestamps] of requestStore.entries()) {
        const valid = timestamps.filter(t => now - t < windowMs);
        if (valid.length === 0) {
            requestStore.delete(key);
        } else {
            requestStore.set(key, valid);
        }
    }
}

// Periodic cleanup every 60 seconds
let cleanupInterval = null;

/**
 * Start cleanup interval (for production use)
 */
function startCleanup() {
    if (!cleanupInterval) {
        cleanupInterval = setInterval(() => cleanup(config.rateLimit.windowMs), 60000);
        cleanupInterval.unref(); // Don't prevent process exit
    }
}

/**
 * Stop cleanup interval (for testing)
 */
function stopCleanup() {
    if (cleanupInterval) {
        clearInterval(cleanupInterval);
        cleanupInterval = null;
    }
}

/**
 * Clear all rate limit data (for testing)
 */
function clearStore() {
    requestStore.clear();
}

/**
 * Get client identifier from request
 * @param {Object} req - Express request
 * @returns {string}
 */
function getClientKey(req) {
    return req.socket?.remoteAddress || req.ip || 'unknown';
}

/**
 * Rate limiting middleware factory
 * @param {Object} [options]
 * @param {number} [options.windowMs] - Window size in ms (default from config)
 * @param {number} [options.maxRequests] - Max requests per window (default from config)
 * @returns {Function} Express middleware
 */
function rateLimitMiddleware(options = {}) {
    const windowMs = options.windowMs ?? config.rateLimit.windowMs;
    const maxRequests = options.maxRequests ?? config.rateLimit.maxRequests;
    
    startCleanup();
    
    return (req, res, next) => {
        const clientKey = getClientKey(req);
        const now = Date.now();
        
        // Get existing timestamps for this client
        let timestamps = requestStore.get(clientKey) || [];
        
        // Filter to only timestamps within the window
        timestamps = timestamps.filter(t => now - t < windowMs);
        
        // Calculate remaining requests (subtract 1 for current request)
        const remaining = Math.max(0, maxRequests - timestamps.length - 1);
        const resetTime = timestamps.length > 0 
            ? Math.ceil((timestamps[0] + windowMs) / 1000)
            : Math.ceil((now + windowMs) / 1000);
        
        // Set rate limit headers
        res.setHeader('X-RateLimit-Limit', maxRequests);
        res.setHeader('X-RateLimit-Remaining', remaining);
        res.setHeader('X-RateLimit-Reset', resetTime);
        
        // Check if limit exceeded
        if (timestamps.length >= maxRequests) {
            logger.warn('rate_limit_exceeded', {
                correlationId: req.correlationId,
                clientKey,
                path: req.path,
                limit: maxRequests,
                windowMs
            });
            
            res.status(429).json({
                error: 'Too many requests',
                retryAfterMs: timestamps[0] + windowMs - now
            });
            return;
        }
        
        // Record this request
        timestamps.push(now);
        requestStore.set(clientKey, timestamps);
        
        next();
    };
}

module.exports = {
    rateLimitMiddleware,
    getClientKey,
    // Exported for testing
    _test: {
        clearStore,
        stopCleanup,
        startCleanup,
        requestStore
    }
};
