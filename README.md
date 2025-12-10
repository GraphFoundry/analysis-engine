# Predictive Analysis Engine

## Overview

The Predictive Analysis Engine is a microservice observability tool that performs predictive impact analysis on service call graphs. It enables operators to simulate infrastructure changes—service failures and scaling operations—before executing them in production, thereby reducing risk and improving operational decision-making.

**Source of Truth:** This service uses the Graph Engine API as its single data source. All graph topology and metrics data is retrieved via HTTP from `service-graph-engine`.

## Architecture

### System Context

```
┌─────────────────────┐
│ Prometheus          │
│ (Metrics Source)    │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│ service-graph-      │
│ engine              │◀──── HTTP/JSON
│ (Graph Engine API)  │
└──────────┬──────────┘
           │
           │ HTTP API
           ▼
┌──────────────────────┐
│ predictive-analysis- │
│ engine               │
│ (This Service)       │
└──────────┬───────────┘
           │
           ▼
┌──────────────────────┐
│ REST API Consumers   │
│ (Operators, UIs)     │
└──────────────────────┘
```

### Key Design Principles

1. **Graph Engine Only**: This service exclusively uses the Graph Engine HTTP API. No direct database access. Graph modifications exist only in-memory during simulation execution.

2. **Configurable Defaults**: All simulation parameters (latency metrics, scaling formulas, traversal depth) are configurable via environment variables or per-request overrides.

3. **Performance Bounded**: Hard limits on traversal depth (max 3 hops) and path enumeration (top N=10) prevent combinatorial explosion on large graphs.

4. **Timeout Enforcement**: HTTP request timeouts ensure fast failure detection when Graph Engine is unavailable.

## Data Model

The engine consumes graph data from the Graph Engine API with the following structure:

**Service Nodes:**
- `serviceId` / `name`: Service identifier (plain name like "frontend")
- `namespace`: Service namespace (typically "default")

**Edges (Calls):**
- `from` → `to`: Caller → callee direction
- `rate`: Request rate (RPS from Prometheus metrics)
- `errorRate`: Error rate (RPS)
- `p50`, `p95`, `p99`: Latency percentiles (milliseconds)

> **Note:** The Graph Engine API provides plain service names (e.g., "frontend") rather than namespace-prefixed identifiers. This service handles both formats for backward compatibility.

**Data Freshness:**
- Graph Engine provides staleness indicators
- Simulations abort if data is stale

## Configuration

All configuration is managed via environment variables with sensible defaults.

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVICE_GRAPH_ENGINE_URL` | `http://service-graph-engine:3000` | Graph Engine API base URL |
| `GRAPH_ENGINE_BASE_URL` | *(alias)* | Alternative name for SERVICE_GRAPH_ENGINE_URL |
| `GRAPH_API_TIMEOUT_MS` | `5000` | Graph Engine HTTP request timeout (ms) |
| `DEFAULT_LATENCY_METRIC` | `p95` | Default latency metric (p50, p95, p99) |
| `MAX_TRAVERSAL_DEPTH` | `2` | Maximum k-hop traversal depth (1-3) |
| `SCALING_MODEL` | `bounded_sqrt` | Scaling formula (bounded_sqrt, linear) |
| `SCALING_ALPHA` | `0.5` | Fixed overhead fraction (0.0-1.0) |
| `MIN_LATENCY_FACTOR` | `0.6` | Minimum latency improvement factor |
| `TIMEOUT_MS` | `8000` | Overall request timeout (ms) |
| `MAX_PATHS_RETURNED` | `10` | Maximum paths in simulation results |
| `PORT` | `7000` | HTTP server port |
| `RATE_LIMIT_WINDOW_MS` | `60000` | Rate limit sliding window (ms) |
| `RATE_LIMIT_MAX_REQUESTS` | `60` | Max requests per window per client |

**Setup:**

```bash
cp .env.example .env
# Edit .env with your Graph Engine URL
```

## API Reference

### Health Check

**Endpoint:** `GET /health`

**Response:**
```json
{
  "status": "ok",
  "provider": "graph-engine",
  "graphApi": {
    "connected": true,
    "status": "healthy",
    "stale": false,
    "lastUpdatedSecondsAgo": 12
  },
  "config": {
    "maxTraversalDepth": 2,
    "defaultLatencyMetric": "p95"
  },
  "uptimeSeconds": 42.3
}
```

**Status Codes:**
- `200 OK`: Always (even when degraded)

---

### Failure Simulation

**Endpoint:** `POST /simulate/failure`

Simulates the removal of a service from the call graph and computes the impact on upstream callers and critical paths.

**Request Body:**

```json
{
  "serviceId": "default:checkoutservice",
  "maxDepth": 2
}
```

Or, using name/namespace:

```json
{
  "name": "checkoutservice",
  "namespace": "default",
  "maxDepth": 2
}
```

**Parameters:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `serviceId` | string | conditional* | Service identifier ("namespace:name") |
| `name` | string | conditional* | Service name |
| `namespace` | string | conditional* | Service namespace |
| `maxDepth` | number | optional | Traversal depth (1-3, default: 2) |

*Either `serviceId` OR (`name` AND `namespace`) required.

**Response:**

```json
{
  "target": {
    "serviceId": "default:checkoutservice",
    "name": "checkoutservice",
    "namespace": "default"
  },
  "neighborhood": {
    "description": "k-hop upstream subgraph around target (not full graph)",
    "serviceCount": 3,
    "edgeCount": 2,
    "depthUsed": 2,
    "generatedAt": "2025-12-25T08:22:28.950Z"
  },
  "affectedCallers": [
    {
      "serviceId": "default:frontend",
      "name": "frontend",
      "namespace": "default",
      "lostTrafficRps": 0.178,
      "edgeErrorRate": 0.0
    }
  ],
  "criticalPathsToTarget": [
    {
      "path": ["default:loadgenerator", "default:frontend", "default:checkoutservice"],
      "pathRps": 0.178
    }
  ],
  "totalLostTrafficRps": 0.178
}
```

**Response Fields:**

- `neighborhood`: Metadata about the k-hop upstream subgraph used for analysis
- `affectedCallers`: Direct callers that lose traffic, sorted by `lostTrafficRps` descending
- `criticalPathsToTarget`: Top N paths by traffic volume that include the failed service
- `pathRps`: Bottleneck throughput (min edge rate along path)
- `totalLostTrafficRps`: Sum of lost traffic across all affected callers

**Status Codes:**
- `200 OK`: Simulation successful
- `400 Bad Request`: Invalid request parameters
- `404 Not Found`: Service not found in graph
- `504 Gateway Timeout`: Simulation timeout exceeded
- `500 Internal Server Error`: Server error

**Example:**

```bash
curl -X POST http://localhost:7000/simulate/failure \
  -H "Content-Type: application/json" \
  -d '{"serviceId": "default:checkoutservice"}'
```

---

### Scaling Simulation

**Endpoint:** `POST /simulate/scale`

Simulates changing the pod count for a service and computes the impact on latency for upstream callers and critical paths.

**Request Body:**

```json
{
  "serviceId": "default:frontend",
  "currentPods": 2,
  "newPods": 6,
  "latencyMetric": "p95",
  "model": {
    "type": "bounded_sqrt",
    "alpha": 0.5
  },
  "maxDepth": 2
}
```

**Parameters:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `serviceId` | string | conditional* | Service identifier ("namespace:name") |
| `name` | string | conditional* | Service name |
| `namespace` | string | conditional* | Service namespace |
| `currentPods` | number | **required** | Current pod count (positive integer) |
| `newPods` | number | **required** | New pod count (positive integer). Aliases: `targetPods`, `pods` |
| `latencyMetric` | string | optional | Latency metric (p50, p95, p99, default: p95) |
| `model.type` | string | optional | Scaling model (bounded_sqrt, linear, default: bounded_sqrt) |
| `model.alpha` | number | optional | Fixed overhead fraction (0.0-1.0, default: 0.5) |
| `maxDepth` | number | optional | Traversal depth (1-3, default: 2) |

*Either `serviceId` OR (`name` AND `namespace`) required.

> **Parameter Aliases:** For convenience, `newPods` accepts aliases `targetPods` and `pods`. If multiple aliases are provided with conflicting values, the request returns 400.

**Response:**

```json
{
  "target": {
    "serviceId": "default:frontend",
    "name": "frontend",
    "namespace": "default"
  },
  "scalingModel": {
    "type": "bounded_sqrt",
    "alpha": 0.5
  },
  "neighborhood": {
    "description": "k-hop upstream subgraph around target (not full graph)",
    "serviceCount": 2,
    "edgeCount": 1,
    "depthUsed": 2,
    "generatedAt": "2025-12-25T08:22:28.950Z"
  },
  "latencyEstimate": {
    "description": "Latency figures: baselineMs is current weighted mean, projectedMs is post-scaling estimate, unit is milliseconds",
    "metric": "p95"
  },
  "currentPods": 2,
  "newPods": 6,
  "affectedCallers": [
    {
      "serviceId": "default:loadgenerator",
      "name": "loadgenerator",
      "namespace": "default",
      "hopDistance": 1,
      "baselineMs": 34.67,
      "projectedMs": 24.89,
      "deltaMs": -9.78
    }
  ],
  "affectedPaths": [
    {
      "path": ["default:loadgenerator", "default:frontend"],
      "baselineMs": 34.67,
      "projectedMs": 24.89,
      "deltaMs": -9.78
    }
  ]
}
```

**Response Fields:**

- `neighborhood`: Metadata about the k-hop upstream subgraph used for analysis
- `latencyEstimate`: Description and metric for latency values (all in milliseconds)
- `affectedCallers`: ALL upstream nodes in neighborhood with latency impact, sorted by `|deltaMs|` descending
- `hopDistance`: Minimum hop distance from caller to target (1 = direct, 2 = 2-hop, etc.)
- `baselineMs`, `projectedMs`: Weighted mean latency before/after scaling (may be `null` if no traffic)
- `deltaMs`: Latency change (negative = improvement)
- `affectedPaths`: Top N paths by traffic with latency changes

**Status Codes:**
- `200 OK`: Simulation successful
- `400 Bad Request`: Invalid request parameters
- `404 Not Found`: Service not found in graph
- `504 Gateway Timeout`: Simulation timeout exceeded
- `500 Internal Server Error`: Server error

**Example:**

```bash
curl -X POST http://localhost:7000/simulate/scale \
  -H "Content-Type: application/json" \
  -d '{
    "serviceId": "default:frontend",
    "currentPods": 2,
    "newPods": 6
  }'
```

---

### Risk Analysis

**Endpoint:** `GET /risk/services/top`

Returns the top services by centrality-based risk score. Services with higher centrality (PageRank or betweenness) are at higher risk of causing cascading failures.

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `metric` | string | `pagerank` | Centrality metric (`pagerank` or `betweenness`) |
| `limit` | number | `5` | Number of services to return (1-20) |

**Response:**

```json
{
  "metric": "pagerank",
  "services": [
    {
      "serviceId": "default:frontend",
      "name": "frontend",
      "score": 0.2847,
      "riskLevel": "high",
      "explanation": "frontend has high PageRank (0.2847), indicating it is a critical hub. Failure could cascade widely."
    },
    {
      "serviceId": "default:checkoutservice",
      "name": "checkoutservice",
      "score": 0.1523,
      "riskLevel": "medium",
      "explanation": "checkoutservice has moderate PageRank (0.1523). Monitor for dependencies."
    }
  ],
  "generatedAt": "2025-12-29T10:00:00.000Z"
}
```

**Response Fields:**

- `metric`: Centrality metric used for ranking
- `services`: Top N services by centrality score, each with:
  - `riskLevel`: `high` (top 20%), `medium` (20-50%), or `low` (bottom 50%)
  - `explanation`: Human-readable risk explanation
- `generatedAt`: Timestamp of analysis

**Example:**

```bash
curl "http://localhost:7000/risk/services/top?metric=pagerank&limit=10"
```

---

### Recommendations in Simulation Responses

Both failure and scaling simulation responses now include actionable recommendations:

**Failure Simulation Response (new field):**
```json
{
  "target": { ... },
  "affectedCallers": [ ... ],
  "recommendations": [
    {
      "type": "circuit-breaker",
      "priority": "high",
      "message": "Consider implementing circuit breakers for callers losing >50 RPS."
    },
    {
      "type": "redundancy",
      "priority": "medium", 
      "message": "3 callers depend on this service. Consider deploying replicas or fallback endpoints."
    }
  ]
}
```

**Scaling Simulation Response (new field):**
```json
{
  "target": { ... },
  "latencyEstimate": { ... },
  "recommendations": [
    {
      "type": "scaling-benefit",
      "priority": "medium",
      "message": "Scaling from 2 to 4 pods shows >30% latency improvement. Proceed if cost-effective."
    }
  ]
}
```

**Recommendation Types:**

| Type | Applies To | Description |
|------|-----------|-------------|
| `data-quality-warning` | Both | Low confidence due to stale/missing data |
| `circuit-breaker` | Failure | High traffic loss suggests circuit breakers |
| `redundancy` | Failure | Multiple callers suggest replication |
| `topology-review` | Failure | Unreachable services detected |
| `monitoring` | Failure | Low impact, but monitor affected callers |
| `scaling-caution` | Scale | Scaling down increases latency significantly |
| `scaling-benefit` | Scale | Scaling up provides >30% improvement |
| `cost-efficiency` | Scale | Minimal benefit, may not justify cost |
| `propagation-awareness` | Scale | Callers will see latency changes |
| `proceed` | Scale | No significant impact detected |

---

## Operational Features

### Correlation ID

All requests are assigned a unique correlation ID for distributed tracing:

- **Header:** `X-Correlation-Id`
- If provided in the request, it is preserved; otherwise, a UUID is generated
- All log entries include the correlation ID for request tracing

**Example:**
```bash
curl -H "X-Correlation-Id: my-trace-123" http://localhost:7000/health
# Response includes: X-Correlation-Id: my-trace-123
```

### Rate Limiting

Simulation endpoints (`POST /simulate/*`) are rate-limited to prevent abuse:

- **Default:** 60 requests per minute per client IP
- **Headers returned:**
  - `X-RateLimit-Limit`: Maximum requests per window
  - `X-RateLimit-Remaining`: Remaining requests in current window
  - `X-RateLimit-Reset`: Unix timestamp when window resets

**Rate Limit Exceeded (HTTP 429):**
```json
{
  "error": "Too many requests",
  "retryAfterMs": 45000
}
```

### Structured Logging

All logs are output in JSON format for easy parsing:

```json
{
  "timestamp": "2025-12-29T10:00:00.000Z",
  "level": "info",
  "message": "request_start",
  "correlationId": "abc-123",
  "method": "POST",
  "path": "/simulate/failure"
}
```

---

## Evaluation Harness

CLI tools for evaluating simulation accuracy against ground truth:

### Run Scenarios

```bash
node tools/eval/run.js \
  --scenarios tools/eval/scenarios.sample.json \
  --output predictions.json \
  --base-url http://localhost:7000
```

**Scenario Format:**
```json
[
  {
    "id": "scenario-1",
    "type": "failure",
    "request": { "serviceId": "default:frontend" }
  }
]
```

### Score Predictions

```bash
node tools/eval/score.js \
  --predictions predictions.json \
  --ground-truth tools/eval/groundTruth.sample.json
```

**Metrics Computed:**
- **MAE** (Mean Absolute Error)
- **MAPE** (Mean Absolute Percentage Error)
- **Spearman ρ** (rank correlation, if N ≥ 2)

---

## Simulation Algorithms

### Failure Simulation

**Algorithm:**

1. Fetch k-hop upstream neighborhood (services that can reach target)
2. Remove target node from in-memory graph
3. For each direct caller:
   - Compute `lostTrafficRps` = sum of edge rates (caller → target)
4. Enumerate paths to target (DFS with cycle prevention)
5. Sort paths by `pathRps` = min(edge.rate) along path (bottleneck throughput)
6. Return top N paths (hard-capped at 10)

**Complexity:**
- Node query: O(V + E) within k-hop neighborhood
- Edge query: O(E) among fetched nodes
- Path enumeration: O(P) where P is bounded by `MAX_PATHS_RETURNED * 2`

**Limitations:**
- Does not model cascading failures (transitive impacts beyond traffic loss)
- Path throughput uses min-rate proxy (not end-to-end latency)

---

### Scaling Simulation

**Algorithm:**

1. Fetch k-hop upstream neighborhood (services that can reach target)
2. For each incoming edge to target:
   - Apply scaling formula to edge latency (in-memory only)
3. For each caller:
   - Compute weighted mean latency **before**: `Σ(rate * latency) / Σ(rate)`
   - Compute weighted mean latency **after** (with target's adjusted latency)
   - Delta = after - before
4. Compute path latencies (sum of edge latencies along path)
5. Sort callers and paths by absolute delta descending
6. Return top N (hard-capped at 10)

**Bounded Square Root Formula:**

```
r = newPods / currentPods
improvement = 1 / sqrt(r)
newLatency = baseLatency * (alpha + (1 - alpha) * improvement)
newLatency = max(newLatency, baseLatency * MIN_LATENCY_FACTOR)
```

**Parameters:**
- `alpha`: Fixed overhead fraction (0.0 = full sqrt improvement, 1.0 = no improvement)
- `MIN_LATENCY_FACTOR`: Minimum achievable latency (default 0.6 = 60% of baseline)

**Example** (alpha=0.5, 2→6 pods, baseLatency=100ms):
- r = 3
- improvement = 1/√3 ≈ 0.577
- newLatency = 100 * (0.5 + 0.5 * 0.577) = 78.9ms
- Clamped to min 60ms

**Linear Formula** (alternative):

```
newLatency = baseLatency * (currentPods / newPods)
```

**Complexity:**
- Node query: O(V + E) within k-hop neighborhood
- Edge query: O(E) among fetched nodes
- Weighted mean computation: O(E) per caller
- Path computation: O(E * P)

**Limitations:**
- Does not model concurrency limits or saturation effects
- Assumes latency is purely a function of pod count (no disk I/O, external API dependencies)
- Σ(rate) = 0 case returns `null` for latency (no traffic to measure)

---

## Examples

### Example 1: Simulate Checkout Service Failure

**Current Graph State:**
- `default:frontend` → `default:checkoutservice` (0.178 RPS)
- `default:loadgenerator` → `default:frontend` (5.31 RPS)

**Request:**

```bash
curl -X POST http://localhost:7000/simulate/failure \
  -H "Content-Type: application/json" \
  -d '{
    "serviceId": "default:checkoutservice",
    "maxDepth": 2
  }'
```

**Response:**

```json
{
  "target": {
    "serviceId": "default:checkoutservice",
    "name": "checkoutservice",
    "namespace": "default"
  },
  "depth": 2,
  "affectedCallers": [
    {
      "serviceId": "default:frontend",
      "lostTrafficRps": 0.178,
      "edgeErrorRate": 0.0
    }
  ],
  "criticalPathsBroken": [
    {
      "path": ["default:loadgenerator", "default:frontend", "default:checkoutservice"],
      "pathRps": 0.178
    }
  ]
}
```

**Interpretation:**
- `frontend` loses 0.178 RPS to `checkoutservice`
- End-to-end path `loadgenerator → frontend → checkoutservice` is broken (bottleneck: 0.178 RPS)

---

### Example 2: Simulate Frontend Scaling (2→6 pods)

**Current Graph State:**
- `default:loadgenerator` → `default:frontend` (5.31 RPS, p95=34.67ms)

**Request:**

```bash
curl -X POST http://localhost:7000/simulate/scale \
  -H "Content-Type: application/json" \
  -d '{
    "serviceId": "default:frontend",
    "currentPods": 2,
    "newPods": 6,
    "latencyMetric": "p95"
  }'
```

**Response:**

```json
{
  "target": {
    "serviceId": "default:frontend",
    "name": "frontend",
    "namespace": "default"
  },
  "latencyMetric": "p95",
  "currentPods": 2,
  "newPods": 6,
  "affectedCallers": [
    {
      "serviceId": "default:loadgenerator",
      "beforeMs": 34.67,
      "afterMs": 27.34,
      "deltaMs": -7.33
    }
  ],
  "affectedPaths": [
    {
      "path": ["default:loadgenerator", "default:frontend"],
      "beforeMs": 34.67,
      "afterMs": 27.34,
      "deltaMs": -7.33
    }
  ]
}
```

**Interpretation:**
- Scaling `frontend` from 2→6 pods (3x increase)
- Using bounded_sqrt (alpha=0.5): latency improves by ~21% (34.67ms → 27.34ms)
- `loadgenerator`'s calls to `frontend` benefit from reduced latency

---

## Running the Service

### Prerequisites

- Node.js >= 18.x
- Access to `service-graph-engine` HTTP API

### Installation

```bash
npm install
```

### Configuration

```bash
cp .env.example .env
# Edit .env with your Graph Engine URL (default: http://service-graph-engine:3000)
```

### Start Server

```bash
npm start
```

**Output:**

```
[2025-12-25T10:00:00.000Z] Predictive Analysis Engine started
Port: 7000
Max traversal depth: 2
Default latency metric: p95
Scaling model: bounded_sqrt (alpha: 0.5)
Timeout: 8000ms
```

### Verify Deployment

```bash
curl http://localhost:7000/health
```

**Expected Response:**

```json
{
  "status": "ok",
  "provider": "graph-engine",
  "graphApi": {
    "connected": true,
    "status": "healthy"
  },
  "uptimeSeconds": 5.2
}
```

---

## Testing

Run test suite:

```bash
npm test
```

---

## Security Considerations

1. **HTTP Only**: All data access via Graph Engine HTTP API (no direct database access)
2. **Input Validation**: All user inputs validated before use
3. **Timeout Protection**: Prevents resource exhaustion from expensive Graph Engine queries
4. **Rate Limiting**: Simulation endpoints protected against abuse

---

## Limitations

### Current Scope (Progress 1)

1. **No Cascading Failure Modeling**: Failure simulation reports immediate traffic loss, not transitive failures
2. **No Concurrency Modeling**: Scaling simulation assumes latency is purely pod-count dependent
3. **Fixed Traversal Depth**: Maximum k=3 hops (prevents combinatorial explosion)
4. **Path Enumeration Bounded**: Top N=10 paths only (prevents memory exhaustion)

### Future Enhancements

- Support for custom latency formulas (user-provided JavaScript functions)
- Historical comparison (compare current metrics to past snapshots)
- Multi-service failure scenarios
- Visualization layer (graph rendering with impact highlighting)

---

## Integration Points

### With service-graph-engine

- **Dependency**: Consumes Graph Engine HTTP API for topology and metrics
- **Endpoints Used**:
  - `GET /graph/health` - Data freshness status
  - `GET /services/{name}/neighborhood?k={depth}` - k-hop neighborhood
- **No coordination required**: Graph Engine provides read-only data access

### With Other Components

- **REST API**: Standard HTTP JSON (no authentication in current version)
- **Service Identifier**: Accepts plain service names (e.g., "frontend")
- **Extensible**: Response format includes detailed metadata for downstream processing

---

## Troubleshooting

### Error: "Service not found"

**Cause:** Target service does not exist in Neo4j graph

**Solution:** Verify service exists:

```bash
curl -X POST http://localhost:7000/simulate/failure \
  -H "Content-Type: application/json" \
  -d '{"serviceId": "default:frontend"}'
```

Check `/health` endpoint for service count.

---

### Error: "Query timeout exceeded"

**Cause:** Graph traversal took longer than `TIMEOUT_MS` (default 8000ms)

**Solution:**
1. Reduce `maxDepth` in request (try 1 instead of 2)
2. Increase `TIMEOUT_MS` in `.env` (if graph is legitimately large)
3. Check Graph Engine performance

---

### Error: "Graph API unavailable"

**Cause:** Cannot reach Graph Engine or it returned an error

**Solution:** 
1. Verify Graph Engine is running: `curl http://service-graph-engine:3000/health`
2. Check `SERVICE_GRAPH_ENGINE_URL` in `.env`
3. Review Graph Engine logs

---

## Copilot Integration

This repository includes extensive GitHub Copilot customization for AI-assisted development:

| Component | Location | Purpose |
|-----------|----------|---------|
| **Custom Agents** | `.github/agents/` | Planner, Implementer, Reviewer personas |
| **Instruction Files** | `.github/instructions/` | Path-specific coding rules (6 files) |
| **Agent Skills** | `.github/skills/` | Specialized knowledge modules (4 skills) |
| **Prompt Templates** | `.github/prompts/` | Reusable workflow prompts (7 files) |

**Key workflow:**
1. Select **Planner** from agent dropdown → Describe your task
2. Review the plan, ask questions
3. Type `OK IMPLEMENT NOW` to approve
4. Select **Implementer** → Execute the plan
5. Select **Reviewer** → Validate changes

For complete documentation, see:
- [AGENTS.md](AGENTS.md) — Universal agent instructions
- [docs/COPILOT-USAGE-GUIDE.md](docs/COPILOT-USAGE-GUIDE.md) — Detailed usage guide

---

## License

ISC

---

## Authors

Research Team - Adaptive Microservice Management
