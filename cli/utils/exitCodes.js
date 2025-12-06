/**
 * CLI Exit Codes
 * 
 * Standardized exit codes for the CLI to enable scripting and automation.
 */

const EXIT_CODES = {
    SUCCESS: 0,           // Operation completed successfully
    VALIDATION_ERROR: 1,  // Invalid arguments or input validation failed
    SERVER_ERROR: 2,      // HTTP 4xx/5xx from the API
    NETWORK_ERROR: 3,     // Network timeout or connection refused
    UNEXPECTED: 4         // Unexpected/unknown error
};

module.exports = { EXIT_CODES };
