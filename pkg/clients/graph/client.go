package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"predictive-analysis-engine/pkg/common"
	"predictive-analysis-engine/pkg/config"
	"predictive-analysis-engine/pkg/logger"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type NeighborhoodOptions struct {
	Direction string
	MaxNodes  int
	MaxEdges  int
}

func NewClient(cfg config.GraphAPIConfig) *Client {

	baseURL := strings.TrimSuffix(cfg.BaseURL, "/")

	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.TimeoutMs) * time.Millisecond,
		},
	}
}

func (c *Client) CheckHealth(ctx context.Context) (*HealthResponse, error) {
	var resp HealthResponse
	if err := c.get(ctx, "/graph/health", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetServices(ctx context.Context) ([]ServiceInfo, error) {

	var wrapper struct {
		Services []ServiceInfo `json:"services"`
	}
	if err := c.get(ctx, "/services", &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Services, nil
}

func (c *Client) GetNodes(ctx context.Context) ([]NodeWithResources, error) {
	var wrapper struct {
		Nodes []NodeWithResources `json:"nodes"`
	}
	if err := c.get(ctx, "/infrastructure/nodes", &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Nodes, nil
}

func (c *Client) GetNeighborhood(ctx context.Context, serviceName string, k int) (*NeighborhoodResponse, error) {
	return c.GetNeighborhoodWithOptions(ctx, serviceName, k, NeighborhoodOptions{
		Direction: "both",
	})
}

func (c *Client) GetNeighborhoodWithOptions(
	ctx context.Context,
	serviceID string,
	k int,
	opts NeighborhoodOptions,
) (*NeighborhoodResponse, error) {
	if k <= 0 {
		k = 1
	}
	direction := opts.Direction
	if direction == "" {
		direction = "both"
	}

	q := url.Values{}
	q.Set("k", fmt.Sprintf("%d", k))
	q.Set("direction", direction)
	if opts.MaxNodes > 0 {
		q.Set("maxNodes", fmt.Sprintf("%d", opts.MaxNodes))
	}
	if opts.MaxEdges > 0 {
		q.Set("maxEdges", fmt.Sprintf("%d", opts.MaxEdges))
	}

	var resp NeighborhoodResponse
	primaryPath := fmt.Sprintf("/services/%s/neighborhood?%s", url.PathEscape(serviceID), q.Encode())
	primaryErr := c.get(ctx, primaryPath, &resp)
	if primaryErr == nil {
		if len(resp.Nodes) > 0 || !strings.Contains(serviceID, ":") {
			return &resp, nil
		}
	} else if !strings.Contains(serviceID, ":") {
		return nil, primaryErr
	}

	// Backward-compatible fallback for graph engines that still key neighborhood by service name.
	serviceName := serviceID
	if parts := strings.SplitN(serviceID, ":", 2); len(parts) == 2 {
		serviceName = parts[1]
	}

	fallbackPath := fmt.Sprintf("/services/%s/neighborhood?%s", url.PathEscape(serviceName), q.Encode())
	var fallbackResp NeighborhoodResponse
	if err := c.get(ctx, fallbackPath, &fallbackResp); err != nil {
		if primaryErr != nil {
			return nil, fmt.Errorf("%w (fallback by service name also failed: %v)", primaryErr, err)
		}
		return nil, err
	}

	return &fallbackResp, nil
}

func (c *Client) GetMetricsSnapshot(ctx context.Context) (*MetricsSnapshotResponse, error) {
	var resp MetricsSnapshotResponse
	if err := c.get(ctx, "/metrics/snapshot", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetTopCentrality(ctx context.Context, metric string, limit int) (*CentralityTopResponse, error) {
	path := fmt.Sprintf("/centrality/top?metric=%s&limit=%d", metric, limit)
	var resp CentralityTopResponse
	if err := c.get(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetCentralityScores(ctx context.Context) (*CentralityScoresResponse, error) {
	var resp CentralityScoresResponse
	if err := c.get(ctx, "/centrality", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) get(ctx context.Context, path string, dest interface{}) error {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}

	if cid, ok := ctx.Value(common.CorrelationIDKey).(string); ok {
		req.Header.Set("X-Correlation-Id", cid)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logger.Error(fmt.Sprintf("[GraphClient] Request failed for %s", url), err)
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Error(fmt.Sprintf("[GraphClient] HTTP %d for %s", resp.StatusCode, url), nil)
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("invalid JSON response: %w", err)
	}

	return nil
}
