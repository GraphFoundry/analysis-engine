/**
 * Parse trace options from query parameters
 * 
 * @param {Object} query - Express req.query object
 * @returns {Object} Normalized trace options
 */
function parseTraceOptions(query = {}) {
    // Helper: treat "true", "1", or boolean true as true
    const toBool = (val) => {
        return val === true || val === 'true' || val === '1';
    };

    return {
        trace: toBool(query.trace),
        includeSnapshot: toBool(query.includeSnapshot),
        includeRawPaths: toBool(query.includeRawPaths),
        includeEdgeDetails: toBool(query.includeEdgeDetails)
    };
}

module.exports = { parseTraceOptions };
