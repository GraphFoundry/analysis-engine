/**
 * Structured JSON Logger
 * 
 * Minimal logger that outputs JSON-formatted log lines for structured logging.
 * Compatible with log aggregators (ELK, Loki, CloudWatch, etc.)
 */

const LOG_LEVELS = {
    debug: 10,
    info: 20,
    warn: 30,
    error: 40
};

const currentLevel = LOG_LEVELS[process.env.LOG_LEVEL] || LOG_LEVELS.info;

/**
 * Format and output a log entry as JSON
 * @param {string} level - Log level
 * @param {string} message - Log message
 * @param {Object} [context] - Additional context fields
 */
function log(level, message, context = {}) {
    if (LOG_LEVELS[level] < currentLevel) return;

    const entry = {
        timestamp: new Date().toISOString(),
        level,
        message,
        ...context
    };

    // Remove undefined values
    Object.keys(entry).forEach(key => {
        if (entry[key] === undefined) delete entry[key];
    });

    const output = level === 'error' ? console.error : console.log;
    output(JSON.stringify(entry));
}

/**
 * Log info message
 * @param {string} message 
 * @param {Object} [context] 
 */
function info(message, context) {
    log('info', message, context);
}

/**
 * Log warning message
 * @param {string} message 
 * @param {Object} [context] 
 */
function warn(message, context) {
    log('warn', message, context);
}

/**
 * Log error message
 * @param {string} message 
 * @param {Object} [context] 
 */
function error(message, context) {
    log('error', message, context);
}

/**
 * Log debug message
 * @param {string} message 
 * @param {Object} [context] 
 */
function debug(message, context) {
    log('debug', message, context);
}

module.exports = {
    info,
    warn,
    error,
    debug,
    log,
    LOG_LEVELS
};
