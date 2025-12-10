---
applyTo: "**/graphEngineClient.js,**/providers/**/*.js,src/**/*.js"
description: 'Graph Engine API is the single source of truth - no fallback to Neo4j'
---

# Graph Engine API Only Policy

This repository uses Graph Engine API as the **single source of truth** for all graph and topology data.

---

## Non-Negotiable Rules

```
Graph Engine API → ONLY data source
       ↓
No fallback → Return 503 if unavailable
```

---

## When to Use Graph Engine API

Copilot must use Graph Engine API for:

- Fetching service topology
- Retrieving edge metrics (rate, latency, error rate)
- Getting node properties (serviceId, name, namespace)
- Any graph traversal operation
- Health checks and data freshness

**There is no alternative data source.**

---

## Error Handling

When Graph Engine API is unavailable:

1. **Return HTTP 503** with clear error message
2. **Include error code**: `GRAPH_ENGINE_UNAVAILABLE`
3. **Do NOT** attempt fallback logic
4. **Log** the failure with correlation ID

**Example error response:**
```json
{
  "code": "GRAPH_ENGINE_UNAVAILABLE",
  "message": "Graph Engine API is unavailable. Cannot perform simulation.",
  "timeoutMs": 5000
}
```

---

## Contract Discipline

### Before Implementing Graph Engine Client Calls

Copilot must verify:

- [ ] Endpoint exists in `src/graphEngineClient.js` OR is documented
- [ ] Request format is documented
- [ ] Response format is documented
- [ ] Error cases are handled (404, 503, timeout)

### If Contract is Missing

Copilot must **STOP** and ask:

> "The Graph Engine API contract for [operation] is not documented. Please provide the contract (endpoint, request/response format)."

### Never Invent

Copilot must **NEVER**:

- Make up endpoint paths (e.g., `/api/graph/services`)
- Make up request body shapes
- Make up response structures
- Assume authentication patterns
- Add fallback logic to Neo4j or any other data source

---

## Configuration

### Required Environment Variable

```bash
SERVICE_GRAPH_ENGINE_URL=http://service-graph-engine:3000
# or: GRAPH_ENGINE_BASE_URL=http://service-graph-engine:3000
```

### Configuration Pattern

```javascript
// Example: config.js
graphApi: {
    baseUrl: process.env.SERVICE_GRAPH_ENGINE_URL || process.env.GRAPH_ENGINE_BASE_URL,
    timeoutMs: parseInt(process.env.GRAPH_API_TIMEOUT_MS) || 5000,
    required: process.env.REQUIRE_GRAPH_API !== 'false' // Default true
}
```

---

## Implementation Pattern

When implementing Graph Engine API consumption:

```javascript
// Graph Engine only - no fallback
async function getServiceTopology(serviceId) {
    try {
        return await graphEngineClient.getNeighborhood(serviceId, maxDepth);
    } catch (error) {
        // Return 503 if Graph Engine unavailable
        if (error.statusCode === 503 || error.message.includes('unavailable')) {
            throw new ServiceUnavailableError('Graph Engine API unavailable');
        }
        throw error;
    }
}
```

---

## Blocked Patterns

**DO NOT** implement these patterns:

```javascript
// ❌ WRONG: Fallback logic
if (graphApiAvailable) {
    return await graphApi.get();
} else {
    return await neo4j.query();
}

// ❌ WRONG: Dual mode
const provider = config.useGraphApi ? graphProvider : neo4jProvider;

// ✅ CORRECT: Graph Engine only
const provider = new GraphEngineHttpProvider();
```

---

## Verification Checklist

Before merging Graph Engine client code, verify:

- [ ] No Neo4j imports in same file
- [ ] No fallback logic present
- [ ] Error handling returns 503 when Graph Engine unavailable
- [ ] Environment variable `SERVICE_GRAPH_ENGINE_URL` is required
- [ ] Tests mock Graph Engine responses (not Neo4j)

---

## Quick Reference

| Situation | Copilot Action |
|-----------|----------------|
| Need graph data | Check for Graph API contract first |
| Contract exists | Implement Graph API client |
| Contract missing | Stop, ask user for contract |
| Graph API unavailable | Use Neo4j fallback, document reason |
| User requests Neo4j | Confirm in plan, proceed with read-only |
