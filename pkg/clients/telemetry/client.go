package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"predictive-analysis-engine/pkg/config"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
)

type TelemetryClient struct {
	client     influxdb2.Client
	httpClient *http.Client
	writeAPI   api.WriteAPIBlocking
	cfg        *config.Config
}

type ServiceMetric struct {
	Timestamp    string  `json:"timestamp"`
	Service      string  `json:"service"`
	Namespace    string  `json:"namespace"`
	RequestRate  float64 `json:"requestRate"`
	ErrorRate    float64 `json:"errorRate"`
	P50          float64 `json:"p50"`
	P95          float64 `json:"p95"`
	P99          float64 `json:"p99"`
	Availability float64 `json:"availability"`
}

type EdgeMetric struct {
	Timestamp   string  `json:"timestamp"`
	From        string  `json:"from"`
	To          string  `json:"to"`
	Namespace   string  `json:"namespace"`
	RequestRate float64 `json:"requestRate"`
	ErrorRate   float64 `json:"errorRate"`
	P50         float64 `json:"p50"`
	P95         float64 `json:"p95"`
	P99         float64 `json:"p99"`
}

type ServicePoint struct {
	Name         string
	Namespace    string
	RequestRate  *float64
	ErrorRate    *float64
	P50          *float64
	P95          *float64
	P99          *float64
	Availability *float64
}

type EdgePoint struct {
	From        string
	To          string
	Namespace   string
	RequestRate *float64
	ErrorRate   *float64
	P50         *float64
	P95         *float64
	P99         *float64
}

type PkgNodePoint struct {
	Name            string
	CPUUsagePercent *float64
	CPUTotalCores   *float64
	RAMUsedMB       *float64
	RAMTotalMB      *float64
	PodCount        *float64
}

type PkgPodPoint struct {
	Name            string
	NodeName        string
	RAMUsedMB       *float64
	CPUUsagePercent *float64
	CPUUsageCores   *float64
}

type influxQLResponse struct {
	Results []struct {
		Series []struct {
			Name    string            `json:"name"`
			Columns []string          `json:"columns"`
			Values  [][]interface{}   `json:"values"`
			Tags    map[string]string `json:"tags"`
		} `json:"series"`
		Error string `json:"error,omitempty"`
	} `json:"results"`
	Error string `json:"error,omitempty"`
}

func NewClient(cfg *config.Config) *TelemetryClient {
	if cfg.Influx.Host == "" || cfg.Influx.Token == "" {
		return &TelemetryClient{cfg: cfg}
	}

	client := influxdb2.NewClient(cfg.Influx.Host, cfg.Influx.Token)

	org := "default"

	writeAPI := client.WriteAPIBlocking(org, cfg.Influx.Database)

	return &TelemetryClient{
		client:     client,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		writeAPI:   writeAPI,
		cfg:        cfg,
	}
}

func (c *TelemetryClient) Close() {
	if c.client != nil {
		c.client.Close()
	}
}

func (c *TelemetryClient) CheckStatus() (bool, string) {
	if !c.cfg.Telemetry.Enabled {
		return false, "Telemetry endpoints disabled. Set TELEMETRY_ENABLED=true to enable."
	}
	if c.client == nil {
		return false, "InfluxDB not configured. Set INFLUX_HOST, INFLUX_TOKEN, INFLUX_DATABASE"
	}
	return true, ""
}

func (c *TelemetryClient) queryInfluxQL(ctx context.Context, q string) (*influxQLResponse, error) {
	u, err := url.Parse(c.cfg.Influx.Host)
	if err != nil {
		return nil, fmt.Errorf("invalid influx host: %w", err)
	}
	u.Path = "/query"
	query := u.Query()
	query.Set("db", c.cfg.Influx.Database)
	query.Set("q", q)
	query.Set("epoch", "ns")
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+c.cfg.Influx.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("influx query failed (status %d): %s", resp.StatusCode, string(body))
	}

	var res influxQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	if res.Error != "" {
		return nil, fmt.Errorf("influx api error: %s", res.Error)
	}
	if len(res.Results) > 0 && res.Results[0].Error != "" {
		return nil, fmt.Errorf("influx query error: %s", res.Results[0].Error)
	}

	return &res, nil
}

func (c *TelemetryClient) GetServiceMetrics(ctx context.Context, service string, from, to string, stepSeconds int) ([]ServiceMetric, error) {

	stepStr := fmt.Sprintf("%ds", stepSeconds)

	baseQuery := `SELECT 
		mean("request_rate") AS "avg_request_rate", 
		mean("error_rate") AS "avg_error_rate", 
		mean("p50") AS "avg_p50", 
		mean("p95") AS "avg_p95", 
		mean("p99") AS "avg_p99", 
		mean("availability") AS "avg_availability" 
		FROM "service_metrics" 
		WHERE time >= '%s' AND time < '%s'`

	whereClause := fmt.Sprintf(baseQuery, from, to)

	if service != "" {
		whereClause += fmt.Sprintf(" AND \"service\" = '%s'", escapeString(service))
	}

	query := fmt.Sprintf(`%s GROUP BY time(%s), "service", "namespace" fill(none)`, whereClause, stepStr)

	res, err := c.queryInfluxQL(ctx, query)
	if err != nil {
		return nil, err
	}

	var metrics []ServiceMetric

	for _, result := range res.Results {
		for _, series := range result.Series {

			svcName := series.Tags["service"]
			namespace := series.Tags["namespace"]

			colMap := make(map[string]int)
			for i, col := range series.Columns {
				colMap[col] = i
			}

			for _, row := range series.Values {

				if len(row) != len(series.Columns) {
					continue
				}

				getFloat := func(name string) float64 {
					idx, ok := colMap[name]
					if !ok || row[idx] == nil {
						return 0
					}

					if f, ok := row[idx].(float64); ok {
						return f
					}

					return 0
				}

				getTime := func() string {
					idx, ok := colMap["time"]
					if !ok || row[idx] == nil {
						return ""
					}

					if s, ok := row[idx].(string); ok {
						return s
					}

					if f, ok := row[idx].(float64); ok {
						t := time.Unix(0, int64(f))
						return t.Format(time.RFC3339)
					}
					return ""
				}

				m := ServiceMetric{
					Timestamp:    getTime(),
					Service:      svcName,
					Namespace:    namespace,
					RequestRate:  getFloat("avg_request_rate"),
					ErrorRate:    getFloat("avg_error_rate"),
					P50:          getFloat("avg_p50"),
					P95:          getFloat("avg_p95"),
					P99:          getFloat("avg_p99"),
					Availability: getFloat("avg_availability"),
				}
				metrics = append(metrics, m)
			}
		}
	}

	return metrics, nil
}

func (c *TelemetryClient) GetEdgeMetrics(ctx context.Context, fromSvc, toSvc, from, to string, stepSeconds int) ([]EdgeMetric, error) {

	stepStr := fmt.Sprintf("%ds", stepSeconds)

	baseQuery := `SELECT 
		mean("request_rate") AS "avg_request_rate", 
		mean("error_rate") AS "avg_error_rate", 
		mean("p50") AS "avg_p50", 
		mean("p95") AS "avg_p95", 
		mean("p99") AS "avg_p99" 
		FROM "edge_metrics" 
		WHERE time >= '%s' AND time < '%s'`

	whereClause := fmt.Sprintf(baseQuery, from, to)

	if fromSvc != "" {
		whereClause += fmt.Sprintf(" AND \"from\" = '%s'", escapeString(fromSvc))
	}
	if toSvc != "" {
		whereClause += fmt.Sprintf(" AND \"to\" = '%s'", escapeString(toSvc))
	}

	query := fmt.Sprintf(`%s GROUP BY time(%s), "from", "to", "namespace" fill(none)`, whereClause, stepStr)

	res, err := c.queryInfluxQL(ctx, query)
	if err != nil {
		return nil, err
	}

	var metrics []EdgeMetric

	for _, result := range res.Results {
		for _, series := range result.Series {
			fromTag := series.Tags["from"]
			toTag := series.Tags["to"]
			namespace := series.Tags["namespace"]

			colMap := make(map[string]int)
			for i, col := range series.Columns {
				colMap[col] = i
			}

			for _, row := range series.Values {
				if len(row) != len(series.Columns) {
					continue
				}

				getFloat := func(name string) float64 {
					idx, ok := colMap[name]
					if !ok || row[idx] == nil {
						return 0
					}
					if f, ok := row[idx].(float64); ok {
						return f
					}
					return 0
				}

				getTime := func() string {
					idx, ok := colMap["time"]
					if !ok || row[idx] == nil {
						return ""
					}
					if s, ok := row[idx].(string); ok {
						return s
					}
					if f, ok := row[idx].(float64); ok {
						t := time.Unix(0, int64(f))
						return t.Format(time.RFC3339)
					}
					return ""
				}

				m := EdgeMetric{
					Timestamp:   getTime(),
					From:        fromTag,
					To:          toTag,
					Namespace:   namespace,
					RequestRate: getFloat("avg_request_rate"),
					ErrorRate:   getFloat("avg_error_rate"),
					P50:         getFloat("avg_p50"),
					P95:         getFloat("avg_p95"),
					P99:         getFloat("avg_p99"),
				}
				metrics = append(metrics, m)
			}
		}
	}

	return metrics, nil
}

func (c *TelemetryClient) WriteServiceMetrics(ctx context.Context, points []ServicePoint) error {
	if c.writeAPI == nil {
		return nil
	}
	var influxPoints []*write.Point
	now := time.Now()

	for _, p := range points {
		fields := make(map[string]interface{})
		if p.RequestRate != nil {
			fields["request_rate"] = *p.RequestRate
		}
		if p.ErrorRate != nil {
			fields["error_rate"] = *p.ErrorRate
		}
		if p.P50 != nil {
			fields["p50"] = *p.P50
		}
		if p.P95 != nil {
			fields["p95"] = *p.P95
		}
		if p.P99 != nil {
			fields["p99"] = *p.P99
		}
		if p.Availability != nil {
			fields["availability"] = *p.Availability
		}

		if len(fields) == 0 {
			continue
		}

		pt := influxdb2.NewPoint(
			"service_metrics",
			map[string]string{
				"service":   p.Name,
				"namespace": p.Namespace,
			},
			fields,
			now,
		)
		influxPoints = append(influxPoints, pt)
	}

	if len(influxPoints) > 0 {
		return c.writeAPI.WritePoint(ctx, influxPoints...)
	}
	return nil
}

func (c *TelemetryClient) WriteEdgeMetrics(ctx context.Context, points []EdgePoint) error {
	if c.writeAPI == nil {
		return nil
	}
	var influxPoints []*write.Point
	now := time.Now()

	for _, p := range points {
		fields := make(map[string]interface{})
		if p.RequestRate != nil {
			fields["request_rate"] = *p.RequestRate
		}
		if p.ErrorRate != nil {
			fields["error_rate"] = *p.ErrorRate
		}
		if p.P50 != nil {
			fields["p50"] = *p.P50
		}
		if p.P95 != nil {
			fields["p95"] = *p.P95
		}
		if p.P99 != nil {
			fields["p99"] = *p.P99
		}

		if len(fields) == 0 {
			continue
		}

		pt := influxdb2.NewPoint(
			"edge_metrics",
			map[string]string{
				"from":      p.From,
				"to":        p.To,
				"namespace": p.Namespace,
			},
			fields,
			now,
		)
		influxPoints = append(influxPoints, pt)
	}

	if len(influxPoints) > 0 {
		return c.writeAPI.WritePoint(ctx, influxPoints...)
	}
	return nil
}

func (c *TelemetryClient) WriteInfrastructureMetrics(ctx context.Context, nodes []PkgNodePoint, pods []PkgPodPoint) error {
	if c.writeAPI == nil {
		return nil
	}
	var influxPoints []*write.Point
	now := time.Now()

	for _, n := range nodes {
		fields := make(map[string]interface{})
		if n.CPUUsagePercent != nil {
			fields["cpu_usage_percent"] = *n.CPUUsagePercent
		}
		if n.CPUTotalCores != nil {
			fields["cpu_total_cores"] = *n.CPUTotalCores
		}
		if n.RAMUsedMB != nil {
			fields["ram_used_mb"] = *n.RAMUsedMB
		}
		if n.RAMTotalMB != nil {
			fields["ram_total_mb"] = *n.RAMTotalMB
		}
		if n.PodCount != nil {
			fields["pod_count"] = *n.PodCount
		}

		if len(fields) == 0 {
			continue
		}

		pt := influxdb2.NewPoint(
			"node_metrics",
			map[string]string{"node": n.Name},
			fields,
			now,
		)
		influxPoints = append(influxPoints, pt)
	}

	for _, p := range pods {
		fields := make(map[string]interface{})
		if p.RAMUsedMB != nil {
			fields["ram_used_mb"] = *p.RAMUsedMB
		}
		if p.CPUUsagePercent != nil {
			fields["cpu_usage_percent"] = *p.CPUUsagePercent
		}
		if p.CPUUsageCores != nil {
			fields["cpu_usage_cores"] = *p.CPUUsageCores
		}

		if len(fields) == 0 {
			continue
		}

		pt := influxdb2.NewPoint(
			"pod_metrics",
			map[string]string{
				"pod":  p.Name,
				"node": p.NodeName,
			},
			fields,
			now,
		)
		influxPoints = append(influxPoints, pt)
	}

	if len(influxPoints) > 0 {
		return c.writeAPI.WritePoint(ctx, influxPoints...)
	}
	return nil
}

func escapeString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
