/**
 * CLI Output Formatters
 * 
 * Functions to format API responses for human-readable console output.
 * Supports plain text and optional table formatting.
 */

/**
 * Format health check response
 * @param {object} data - Health check response
 * @returns {string} Formatted output
 */
function formatHealth(data) {
    const lines = [];
    
    // Status header with visual indicator
    const statusIcon = data.status === 'ok' ? '✓' : '!';
    lines.push(`${statusIcon} Status: ${data.status.toUpperCase()}`);
    lines.push(`  Data Source: ${data.dataSource}`);
    lines.push(`  Uptime: ${data.uptimeSeconds}s`);
    lines.push('');
    
    // Provider info
    lines.push('Provider:');
    lines.push(`  Connected: ${data.provider?.connected ? 'Yes' : 'No'}`);
    if (data.provider?.services !== undefined) {
        lines.push(`  Services: ${data.provider.services}`);
    }
    if (data.provider?.stale !== undefined) {
        lines.push(`  Stale: ${data.provider.stale ? 'Yes' : 'No'}`);
    }
    if (data.provider?.error) {
        lines.push(`  Error: ${data.provider.error}`);
    }
    lines.push('');
    
    // Graph API info
    if (data.graphApi) {
        lines.push('Graph API:');
        lines.push(`  Enabled: ${data.graphApi.enabled ? 'Yes' : 'No'}`);
        if (data.graphApi.enabled) {
            lines.push(`  Available: ${data.graphApi.available ? 'Yes' : 'No'}`);
            if (data.graphApi.status) {
                lines.push(`  Status: ${data.graphApi.status}`);
            }
            if (data.graphApi.stale !== undefined) {
                lines.push(`  Stale: ${data.graphApi.stale ? 'Yes' : 'No'}`);
            }
        }
        if (data.graphApi.reason && !data.graphApi.available) {
            lines.push(`  Reason: ${data.graphApi.reason}`);
        }
        lines.push('');
    }
    
    // Config
    if (data.config) {
        lines.push('Config:');
        lines.push(`  Max Traversal Depth: ${data.config.maxTraversalDepth}`);
        lines.push(`  Default Latency Metric: ${data.config.defaultLatencyMetric}`);
    }
    
    return lines.join('\n');
}

/**
 * Format failure simulation response
 * @param {object} data - Failure simulation response
 * @returns {string} Formatted output
 */
function formatFailureSimulation(data) {
    const lines = [];
    
    // Header
    lines.push(`Failure Simulation: ${data.failedService || 'Unknown'}`);
    lines.push('='.repeat(50));
    lines.push('');
    
    // Summary
    if (data.impact) {
        lines.push('Impact Summary:');
        lines.push(`  Total Affected: ${data.impact.totalAffected || 0} services`);
        lines.push(`  Direct Dependents: ${data.impact.directDependents || 0}`);
        lines.push(`  Indirect Dependents: ${data.impact.indirectDependents || 0}`);
        lines.push('');
    }
    
    // Affected services list
    if (data.affectedServices && data.affectedServices.length > 0) {
        lines.push('Affected Services:');
        for (const svc of data.affectedServices) {
            const depth = svc.depth !== undefined ? ` (depth: ${svc.depth})` : '';
            const impact = svc.impactScore !== undefined ? ` [impact: ${(svc.impactScore * 100).toFixed(1)}%]` : '';
            lines.push(`  • ${svc.serviceId || svc.name}${depth}${impact}`);
        }
        lines.push('');
    }
    
    // Recommendations
    if (data.recommendations && data.recommendations.length > 0) {
        lines.push('Recommendations:');
        for (let i = 0; i < data.recommendations.length; i++) {
            lines.push(`  ${i + 1}. ${data.recommendations[i]}`);
        }
    }
    
    return lines.join('\n');
}

/**
 * Format scaling simulation response
 * @param {object} data - Scaling simulation response
 * @returns {string} Formatted output
 */
function formatScalingSimulation(data) {
    const lines = [];
    
    // Header
    lines.push(`Scaling Simulation: ${data.serviceId || 'Unknown'}`);
    lines.push('='.repeat(50));
    lines.push('');
    
    // Scaling details
    lines.push('Scaling Change:');
    lines.push(`  Current Pods: ${data.currentPods}`);
    lines.push(`  New Pods: ${data.newPods}`);
    lines.push(`  Change: ${data.newPods > data.currentPods ? '+' : ''}${data.newPods - data.currentPods} pods`);
    lines.push('');
    
    // Latency predictions
    if (data.latencyPrediction) {
        lines.push('Latency Prediction:');
        lines.push(`  Metric: ${data.latencyMetric || 'p95'}`);
        lines.push(`  Current: ${formatLatency(data.latencyPrediction.current)}`);
        lines.push(`  Predicted: ${formatLatency(data.latencyPrediction.predicted)}`);
        const change = data.latencyPrediction.changePercent;
        const changeStr = change !== undefined ? 
            `${change > 0 ? '+' : ''}${change.toFixed(1)}%` : 'N/A';
        lines.push(`  Change: ${changeStr}`);
        lines.push('');
    }
    
    // Model info
    if (data.model) {
        lines.push('Model:');
        lines.push(`  Type: ${data.model.type || data.model}`);
        if (data.model.alpha !== undefined) {
            lines.push(`  Alpha: ${data.model.alpha}`);
        }
        lines.push('');
    }
    
    // Downstream impact
    if (data.downstreamImpact && data.downstreamImpact.length > 0) {
        lines.push('Downstream Impact:');
        for (const svc of data.downstreamImpact) {
            const latency = svc.predictedLatency !== undefined ? 
                ` → ${formatLatency(svc.predictedLatency)}` : '';
            lines.push(`  • ${svc.serviceId || svc.name}${latency}`);
        }
    }
    
    return lines.join('\n');
}

/**
 * Format top risk services response
 * @param {object} data - Risk analysis response
 * @returns {string} Formatted output
 */
function formatRiskTop(data) {
    const lines = [];
    
    // Header
    lines.push(`Top Risk Services (by ${data.metric || 'pagerank'})`);
    lines.push('='.repeat(50));
    lines.push('');
    
    // Services table-like format
    if (data.services && data.services.length > 0) {
        // Find max name length for alignment
        const maxNameLen = Math.max(...data.services.map(s => (s.serviceId || s.name || '').length), 10);
        
        // Header row
        lines.push(`${'Service'.padEnd(maxNameLen)}  Score      Rank`);
        lines.push(`${'-'.repeat(maxNameLen)}  -------    ----`);
        
        for (let i = 0; i < data.services.length; i++) {
            const svc = data.services[i];
            const name = (svc.serviceId || svc.name || 'Unknown').padEnd(maxNameLen);
            const score = svc.score !== undefined ? svc.score.toFixed(4).padStart(7) : '   N/A ';
            lines.push(`${name}  ${score}    #${i + 1}`);
        }
    } else {
        lines.push('No services found.');
    }
    
    return lines.join('\n');
}

/**
 * Format latency value with units
 * @param {number} latencyMs - Latency in milliseconds
 * @returns {string} Formatted latency
 */
function formatLatency(latencyMs) {
    if (latencyMs === undefined || latencyMs === null) {
        return 'N/A';
    }
    if (latencyMs >= 1000) {
        return `${(latencyMs / 1000).toFixed(2)}s`;
    }
    return `${latencyMs.toFixed(1)}ms`;
}

/**
 * Format error for console output
 * @param {Error} error - Error object
 * @returns {string} Formatted error
 */
function formatError(error) {
    const lines = [];
    lines.push(`Error: ${error.message}`);
    if (error.statusCode) {
        lines.push(`  HTTP Status: ${error.statusCode}`);
    }
    return lines.join('\n');
}

module.exports = {
    formatHealth,
    formatFailureSimulation,
    formatScalingSimulation,
    formatRiskTop,
    formatLatency,
    formatError
};
