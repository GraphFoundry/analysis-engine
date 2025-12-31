const { InfluxDBClient } = require('@influxdata/influxdb3-client');
const config = require('../config/config');

/**
 * Service to interact with InfluxDB and fetch telemetry data.
 */
class TelemetryService {
    constructor() {
        this.client = null;
        if (config.influx.host && config.influx.token && config.influx.database) {
            try {
                this.client = new InfluxDBClient({
                    host: config.influx.host,
                    token: config.influx.token,
                    database: config.influx.database
                });
            } catch (error) {
                console.error(`Failed to initialize InfluxDB client: ${error.message}`);
            }
        }
    }

    /**
     * Check if telemetry is enabled and configured.
     * @returns {Object} { enabled: boolean, error?: string }
     */
    checkStatus() {
        if (!config.telemetry.enabled) {
            return { enabled: false, error: 'Telemetry endpoints disabled. Set TELEMETRY_ENABLED=true to enable.' };
        }
        if (!this.client) {
            return { enabled: false, error: 'InfluxDB not configured. Set INFLUX_HOST, INFLUX_TOKEN, INFLUX_DATABASE' };
        }
        return { enabled: true };
    }

    /**
     * Parse time window string (e.g. '1w') into start/end timestamps.
     * @param {string} windowStr 
     * @returns {{ from: string, to: string, stepSeconds: number }}
     */
    parseTimeWindow(windowStr) {
        const now = new Date();
        const to = now.toISOString();
        let fromDate = new Date();
        let stepSeconds = 3600; // default 1h step for longer ranges

        switch (windowStr) {
            case '5d':
                fromDate.setDate(now.getDate() - 5);
                stepSeconds = 3600;
                break;
            case '1w':
                fromDate.setDate(now.getDate() - 7);
                stepSeconds = 3600;
                break;
            case '2w':
                fromDate.setDate(now.getDate() - 14);
                stepSeconds = 7200; // 2h step
                break;
            case '1m':
                fromDate.setMonth(now.getMonth() - 1);
                stepSeconds = 14400; // 4h step
                break;
            default:
                // Default to last 1 hour if unspecified or invalid
                fromDate.setHours(now.getHours() - 1);
                stepSeconds = 60;
        }

        return {
            from: fromDate.toISOString(),
            to,
            stepSeconds
        };
    }

    /**
     * Fetch aggregated edge metrics for a set of edges over a time window.
     * Useful for simulations using historical data.
     * 
     * @param {string} fromTime ISO string
     * @param {string} toTime ISO string
     * @returns {Promise<Map<string, {requestRate: number, errorRate: number}>>} keyed by "source:target"
     */
    async getAggregatedEdgeMetrics(fromTime, toTime) {
        const status = this.checkStatus();
        if (!status.enabled) return new Map();

        const query = `
            SELECT 
                "from" AS from_service,
                "to" AS to_service,
                AVG(request_rate) AS avg_request_rate,
                AVG(NULLIF(error_rate, 0)) AS avg_error_rate,
                AVG(NULLIF(p50, 0)) AS avg_p50,
                AVG(NULLIF(p95, 0)) AS avg_p95,
                AVG(NULLIF(p99, 0)) AS avg_p99
            FROM edge_metrics
            WHERE time >= '${fromTime}'
              AND time < '${toTime}'
            GROUP BY from_service, to_service
        `;

        const metricsMap = new Map();
        try {
            const reader = await this.client.query(query, config.influx.database);
            for await (const row of reader) {
                const key = `${row.from_service}->${row.to_service}`;
                metricsMap.set(key, {
                    requestRate: row.avg_request_rate || 0,
                    errorRate: row.avg_error_rate || 0,
                    p50: row.avg_p50 || 0,
                    p95: row.avg_p95 || 0,
                    p99: row.avg_p99 || 0
                });
            }
        } catch (err) {
            console.error('Error fetching aggregated edge metrics:', err);
        }

        return metricsMap;
    }

    /**
     * Fetch aggregated node metrics (CPU/RAM) over a time window.
     * @param {string} fromTime ISO string
     * @param {string} toTime ISO string
     * @returns {Promise<Map<string, {cpuUsage: number, ramUsage: number}>>} keyed by node name
     */
    async getAggregatedNodeMetrics(fromTime, toTime) {
        const status = this.checkStatus();
        if (!status.enabled) return new Map();

        const query = `
            SELECT 
                node,
                AVG(cpu_usage_percent) AS avg_cpu,
                AVG(ram_used_mb) AS avg_ram
            FROM node_metrics
            WHERE time >= '${fromTime}'
              AND time < '${toTime}'
            GROUP BY node
        `;

        const metricsMap = new Map();
        try {
            const reader = await this.client.query(query, config.influx.database);
            for await (const row of reader) {
                metricsMap.set(row.node, {
                    cpuUsagePercent: row.avg_cpu || 0,
                    ramUsageMB: row.avg_ram || 0
                });
            }
        } catch (err) {
            console.error('Error fetching aggregated node metrics:', err);
        }

        return metricsMap;
    }

    /**
     * Fetch service metrics.
     * @param {string} service 
     * @param {string} from 
     * @param {string} to 
     * @param {number} stepSeconds 
     */
    async getServiceMetrics(service, from, to, stepSeconds) {
        const serviceFilter = service ? `service = '${service.replaceAll("'", "''")}'` : '1=1';
        const query = `
          SELECT 
            DATE_BIN(INTERVAL '${stepSeconds} seconds', time, '1970-01-01T00:00:00Z'::TIMESTAMP) AS bucket,
            service,
            namespace,
            AVG(request_rate) AS avg_request_rate,
            AVG(NULLIF(error_rate, 0)) AS avg_error_rate,
            AVG(NULLIF(p50, 0)) AS avg_p50,
            AVG(NULLIF(p95, 0)) AS avg_p95,
            AVG(NULLIF(p99, 0)) AS avg_p99,
            AVG(NULLIF(availability, 0)) AS avg_availability
          FROM service_metrics
          WHERE ${serviceFilter}
            AND time >= '${from}'
            AND time < '${to}'
          GROUP BY bucket, service, namespace
          ORDER BY bucket ASC
        `;

        const results = [];
        const reader = await this.client.query(query, config.influx.database);

        for await (const row of reader) {
            results.push({
                timestamp: row.bucket,
                service: row.service,
                namespace: row.namespace,
                requestRate: row.avg_request_rate,
                errorRate: row.avg_error_rate,
                p50: row.avg_p50,
                p95: row.avg_p95,
                p99: row.avg_p99,
                availability: row.avg_availability
            });
        }
        return results;
    }

    /**
     * Fetch edge metrics.
     * @param {string} fromService 
     * @param {string} toService 
     * @param {string} from 
     * @param {string} to 
     * @param {number} stepSeconds 
     */
    async getEdgeMetrics(fromService, toService, from, to, stepSeconds) {
        const conditions = [
            `time >= '${from}'`,
            `time < '${to}'`
        ];

        if (fromService) {
            conditions.push(`"from" = '${fromService.replaceAll("'", "''")}'`);
        }

        if (toService) {
            conditions.push(`"to" = '${toService.replaceAll("'", "''")}'`);
        }

        const query = `
          SELECT 
            DATE_BIN(INTERVAL '${stepSeconds} seconds', time, '1970-01-01T00:00:00Z'::TIMESTAMP) AS bucket,
            "from" AS from_service,
            "to" AS to_service,
            namespace,
            AVG(request_rate) AS avg_request_rate,
            AVG(NULLIF(error_rate, 0)) AS avg_error_rate,
            AVG(NULLIF(p50, 0)) AS avg_p50,
            AVG(NULLIF(p95, 0)) AS avg_p95,
            AVG(NULLIF(p99, 0)) AS avg_p99
          FROM edge_metrics
          WHERE ${conditions.join(' AND ')}
          GROUP BY bucket, from_service, to_service, namespace
          ORDER BY bucket ASC
        `;

        const results = [];
        const reader = await this.client.query(query, config.influx.database);

        for await (const row of reader) {
            results.push({
                timestamp: row.bucket,
                from: row.from_service,
                to: row.to_service,
                namespace: row.namespace,
                requestRate: row.avg_request_rate,
                errorRate: row.avg_error_rate,
                p50: row.avg_p50,
                p95: row.avg_p95,
                p99: row.avg_p99
            });
        }
        return results;
    }
}

// Singleton instance
const telemetryService = new TelemetryService();

module.exports = telemetryService;
