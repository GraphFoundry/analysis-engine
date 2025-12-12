---
applyTo: "**/graphEngineClient.js,**/providers/**/*.js,src/**/*.js"
description: 'Graph Engine HTTP API is the single source of truth for graph data - no alternatives'
---

# Graph API First Policy

Graph Engine HTTP API is the **single source of truth** for all graph and topology data in this repository.

---

## Core Principle

```
Graph Engine API → ONLY data source
       ↓
No alternatives → Return 503 if unavailable
```

**This policy replaces the previous Neo4j fallback approach.**

---

## When to Use Graph Engine API

Copilot must use Graph Engine API for:

- Fetching service topology
- Retrieving edge metrics (rate, latency, error rate)
- Getting node properties (serviceId, name, namespace)
- Any graph traversal operation
- Health checks and data freshness

**There is no alternative data source. No fallback logic is permitted.**

---

## Error Handling (No Fallback)

When Graph Engine API is unavailable:

1. **Return HTTP 503** with clear error message
2. **Include error code**: `GRAPH_ENGINE_UNAVAILABLE`
3. **Do NOT** attempt any fallback logic
4. **Log** the failure (without credentials)

**Example error response:**
```json
{
  "error": "Graph Engine unavailable",
  "code": "GRAPH_ENGINE_UNAVAILABLE",
  "message": "Cannot perform simulation without graph data",
  "retryable": true
}
```

---

## Contract Discipline

### Before Implementing Graph Engine Client Calls

Copilot must verify:

- [ ] Endpoint exists in Graph Engine API documentation
- [ ] Request format is documented (URL, params, body)
- [ ] Response format is documented (schema, status codes)
- [ ] Error cases are handled (404, 503, timeout)

### If Contract is Missing

Copilot must **STOP** and ask:

> "The Graph Engine API contract for [operation] is not documented. Please provide the endpoint specification (URL, request/response format) before proceeding."

### Never Invent

Copilot must **NEVER**:

- Make up endpoint paths (e.g., `/api/graph/services`)
- Make up request body shapes
- Make up response structures
- Assume authentication patterns
- Add fallback logic to any alternative data source
- Import direct database drivers

---

## Configuration

### Required Environment Variable

```bash
SERVICE_GRAPH_ENGINE_URL=http://service-graph-engine:3000
# or: GRAPH_ENGINE_BASE_URL=http://service-graph-engine:3000
```

Application must fail to start if this env var is missing.

### Configuration Pattern

```javascript
// Example: config.js
graphEngine: {
    baseUrl: process.env.SERVICE_GRAPH_ENGINE_URL || process.env.GRAPH_ENGINE_BASE_URL,
    timeout: parseInt(process.env.GRAPH_API_TIMEOUT_MS) || 20000
}

// Validate on startup
if (!graphEngine.baseUrl) {
    console.error('ERROR: SERVICE_GRAPH_ENGINE_URL is required');
    process.exit(1);
}
```

---

## Implementation Pattern

When implementing Graph Engine API consumption:

```javascript
// ✅ CORRECT: Graph Engine only, no fallback
async function getServiceTopology(serviceId) {
    try {
        return await graphEngineClient.getNeighborhood(serviceId);
    } catch (error) {
        // Propagate error - no fallback
        logger.error('Graph Engine request failed', {
            serviceId,
            error: error.message
        });
        throw new GraphEngineUnavailableError(error);
    }
}
```

---

## Blocked Patterns

**DO NOT** implement these patterns:

```javascript
// ❌ WRONG: Fallback logic to alternative data source
if (graphEngineAvailable) {
    return await graphEngine.get();
} else {
    return await alternativeSource.query();
}

// ❌ WRONG: Dual mode provider
const provider = config.useGraphEngine ? graphEngineProvider : fallbackProvider;

// ✅ CORRECT: Graph Engine only
const provider = new GraphEngineHttpProvider();
```

---

## Verification Checklist

Before merging Graph Engine client code, verify:

- [ ] No database driver imports in same file (`neo4j-driver`, `pg`, etc.)
- [ ] No fallback logic present
- [ ] Error handling returns 503 when Graph Engine unavailable
- [ ] Environment variable `SERVICE_GRAPH_ENGINE_URL` is required
- [ ] Tests mock Graph Engine responses only

---

## Quick Reference

| Situation | Copilot Action |
|-----------|----------------|
| Need graph data | Use Graph Engine API |
| Contract exists | Implement Graph API client |
| Contract missing | Stop, ask user for contract |
| Graph API unavailable | Return 503 with clear error message |
| User asks to add fallback | Refuse, cite this rule |
