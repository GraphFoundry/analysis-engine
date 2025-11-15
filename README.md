# What-If Simulation Engine

## Overview

The What-If Simulation Engine is a microservice observability tool that performs predictive impact analysis on service call graphs. It enables operators to simulate infrastructure changes—service failures and scaling operations—before executing them in production, thereby reducing risk and improving operational decision-making.

This service integrates with the existing Neo4j-based service graph infrastructure (populated by `service-graph-engine`) to provide real-time "what-if" analysis capabilities.

## Architecture

### System Context

```
┌─────────────────────┐
│ Prometheus          │
│ (Metrics Source)    │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐       ┌──────────────────┐
│ service-graph-      │──────▶│ Neo4j            │
│ engine              │       │ (Graph Database) │
│ (Metric Ingestion)  │       └────────┬─────────┘
└─────────────────────┘                │
                                       │ READ-ONLY
                                       ▼
                            ┌──────────────────────┐
                            │ what-if-simulation-  │
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

1. **Read-Only Analysis**: All Neo4j queries are read-only. Graph modifications exist only in-memory during simulation execution.

2. **Configurable Defaults**: All simulation parameters (latency metrics, scaling formulas, traversal depth) are configurable via environment variables or per-request overrides.

3. **Performance Bounded**: Hard limits on traversal depth (max 3 hops) and path enumeration (top N=10) prevent combinatorial explosion on large graphs.

4. **Timeout Enforcement**: Two-layer timeout protection (Neo4j transaction timeout + overall request timeout) ensures fast failure detection.

## Graph Schema

The engine operates on the following Neo4j schema (managed by `service-graph-engine`):

**Nodes:**
- Label: `Service`
- Properties: `serviceId` (unique), `name`, `namespace`, `createdAt`, `updatedAt`, `pagerank`, `betweenness`

**Relationships:**
- Type: `CALLS_NOW` (direction: caller → callee)
- Properties: `rate`, `errorRate`, `p50`, `p95`, `p99`, `windowStart`, `windowEnd`, `lastUpdated`

**ServiceId Format:** `"namespace:name"` (e.g., `"default:frontend"`)

## Configuration

All configuration is managed via environment variables with sensible defaults.

| Variable | Default | Description |
|----------|---------|-------------|
| `NEO4J_URI` | `neo4j+s://...` | Neo4j connection URI |
| `NEO4J_USER` | `neo4j` | Neo4j username |
| `NEO4J_PASSWORD` | *(required)* | Neo4j password (never logged) |
| `DEFAULT_LATENCY_METRIC` | `p95` | Default latency metric (p50, p95, p99) |
| `MAX_TRAVERSAL_DEPTH` | `2` | Maximum k-hop traversal depth (1-3) |
| `SCALING_MODEL` | `bounded_sqrt` | Scaling formula (bounded_sqrt, linear) |
| `SCALING_ALPHA` | `0.5` | Fixed overhead fraction (0.0-1.0) |
| `MIN_LATENCY_FACTOR` | `0.6` | Minimum latency improvement factor |
| `TIMEOUT_MS` | `8000` | Query and request timeout (ms) |
| `MAX_PATHS_RETURNED` | `10` | Maximum paths in simulation results |
| `PORT` | `3000` | HTTP server port |

**Setup:**

```bash
cp .env.example .env
# Edit .env with your Neo4j credentials
```

## API Reference

### Health Check

**Endpoint:** `GET /health`

**Response:**
```json
{
  "status": "ok",
  "neo4j": {
    "connected": true,
    "services": 11
  },
  "uptime": 42.3
}
```

**Status Codes:**
- `200 OK`: Service healthy
- `500 Internal Server Error`: Service error

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

**Response Fields:**

- `affectedCallers`: Direct callers that lose traffic, sorted by `lostTrafficRps` descending
- `criticalPathsBroken`: Top N paths by traffic volume that include the failed service
- `pathRps`: Bottleneck throughput (min edge rate along path)

**Status Codes:**
- `200 OK`: Simulation successful
- `400 Bad Request`: Invalid request parameters
- `404 Not Found`: Service not found in graph
- `504 Gateway Timeout`: Simulation timeout exceeded
- `500 Internal Server Error`: Server error

**Example:**

```bash
curl -X POST http://localhost:3000/simulate/failure \
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
| `newPods` | number | **required** | New pod count (positive integer) |
| `latencyMetric` | string | optional | Latency metric (p50, p95, p99, default: p95) |
| `model.type` | string | optional | Scaling model (bounded_sqrt, linear, default: bounded_sqrt) |
| `model.alpha` | number | optional | Fixed overhead fraction (0.0-1.0, default: 0.5) |
| `maxDepth` | number | optional | Traversal depth (1-3, default: 2) |

*Either `serviceId` OR (`name` AND `namespace`) required.

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
      "afterMs": 24.89,
      "deltaMs": -9.78
    }
  ],
  "affectedPaths": [
    {
      "path": ["default:loadgenerator", "default:frontend"],
      "beforeMs": 34.67,
      "afterMs": 24.89,
      "deltaMs": -9.78
    }
  ]
}
```

**Response Fields:**

- `affectedCallers`: Callers with changed latency, sorted by absolute `deltaMs` descending
- `beforeMs`, `afterMs`: Weighted mean latency (may be `null` if caller has zero traffic)
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
curl -X POST http://localhost:3000/simulate/scale \
  -H "Content-Type: application/json" \
  -d '{
    "serviceId": "default:frontend",
    "currentPods": 2,
    "newPods": 6
  }'
```

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
curl -X POST http://localhost:3000/simulate/failure \
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
curl -X POST http://localhost:3000/simulate/scale \
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

- Node.js >= 14.x
- Neo4j database (populated by `service-graph-engine`)

### Installation

```bash
npm install
```

### Configuration

```bash
cp .env.example .env
# Edit .env with your Neo4j credentials
```

### Start Server

```bash
npm start
```

**Output:**

```
[2025-12-25T10:00:00.000Z] What-if Simulation Engine started
Port: 3000
Max traversal depth: 2
Default latency metric: p95
Scaling model: bounded_sqrt (alpha: 0.5)
Timeout: 8000ms
```

### Verify Deployment

```bash
curl http://localhost:3000/health
```

**Expected Response:**

```json
{
  "status": "ok",
  "neo4j": {
    "connected": true,
    "services": 11
  },
  "uptime": 5.2
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

1. **Credential Management**: Neo4j password is never logged (redacted in all error messages)
2. **Read-Only Access**: All Neo4j queries use `READ` access mode
3. **Input Validation**: All user inputs validated before use
4. **Timeout Protection**: Prevents resource exhaustion from expensive queries

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

- **Dependency**: Reads same Neo4j graph (Services + CALLS_NOW edges)
- **Schema**: Assumes schema managed by `service-graph-engine`
- **No coordination required**: Both services are read-only consumers

### With Other Components

- **REST API**: Standard HTTP JSON (no authentication in Progress 1)
- **Service Identifier**: Accepts both `serviceId` and `name`+`namespace` formats
- **Extensible**: Response format includes detailed metadata for downstream processing

---

## Troubleshooting

### Error: "Service not found"

**Cause:** Target service does not exist in Neo4j graph

**Solution:** Verify service exists:

```bash
curl -X POST http://localhost:3000/simulate/failure \
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

---

### Error: "Neo4j connection failed"

**Cause:** Invalid credentials or unreachable database

**Solution:** Verify Neo4j credentials in `.env`:

```bash
# Test connection
node verify-schema.js
```

---

## License

ISC

---

## Authors

Research Team - Adaptive Microservice Management
