const { performance } = require('node:perf_hooks');

// Preview caps for trace summaries
const TRACE_PREVIEW_MAX_NODES = 10;
const TRACE_PREVIEW_MAX_EDGES = 10;
const TRACE_PREVIEW_MAX_PATHS = 20;

/**
 * Cap array to max size for preview
 * @param {Array} arr - Array to cap
 * @param {number} max - Maximum size
 * @returns {Array} Capped array
 */
function capArray(arr, max) {
    if (!Array.isArray(arr)) return [];
    return arr.slice(0, max);
}

/**
 * Create a trace instance for pipeline execution tracking
 * 
 * @param {Object} traceOptions - Trace options from parseTraceOptions
 * @returns {Object} Trace API (no-op if trace disabled, active if enabled)
 */
function createTrace(traceOptions = {}) {
    const enabled = traceOptions.trace === true;

    if (!enabled) {
        // No-op API when trace disabled
        return {
            stage: async (name, fn) => await fn(),
            addWarning: () => {},
            setSummary: () => {},
            finalize: () => null
        };
    }

    // Active trace: maintain internal state
    const stages = [];
    const stageMap = new Map(); // stageName -> stageObject for setSummary

    return {
        /**
         * Execute a function inside a traced stage
         * Measures execution time using performance.now()
         * 
         * @param {string} name - Stage name (kebab-case recommended)
         * @param {Function} fn - Async or sync function to execute
         * @returns {Promise<any>} Result of fn
         */
        stage: async (name, fn) => {
            const start = performance.now();
            let result;
            try {
                result = await fn();
            } finally {
                const end = performance.now();
                const ms = Math.round((end - start) * 100) / 100; // 2 decimal places

                const stageObj = {
                    name,
                    ms
                };

                stages.push(stageObj);
                stageMap.set(name, stageObj);
            }
            return result;
        },

        /**
         * Add a warning to a specific stage
         * Warnings are collected and included in trace output
         * 
         * @param {string} stageName - Stage to attach warning to
         * @param {string} message - Warning message
         */
        addWarning: (stageName, message) => {
            const stage = stageMap.get(stageName);
            if (stage) {
                if (!stage.warnings) {
                    stage.warnings = [];
                }
                stage.warnings.push(message);
            }
        },

        /**
         * Set summary metadata for a stage (after execution)
         * Summary should be small (counts, metrics, top-N lists)
         * 
         * @param {string} stageName - Stage to attach summary to
         * @param {Object} summary - Summary object (size-limited)
         */
        setSummary: (stageName, summary) => {
            const stage = stageMap.get(stageName);
            if (stage) {
                stage.summary = summary;
            }
        },

        /**
         * Finalize trace and return trace object
         * Returns null if trace disabled (already handled by no-op API)
         * 
         * @returns {Object|null} Trace object or null
         */
        finalize: () => {
            return {
                options: traceOptions,
                stages,
                generatedAt: new Date().toISOString()
            };
        }
    };
}

module.exports = { 
    createTrace, 
    TRACE_PREVIEW_MAX_NODES,
    TRACE_PREVIEW_MAX_EDGES,
    TRACE_PREVIEW_MAX_PATHS,
    capArray
};
