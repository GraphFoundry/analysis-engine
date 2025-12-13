---
applyTo: "**/graphEngineClient.js,**/providers/**/*.js,src/**/*.js"
description: 'Graph Engine is the single source of truth - no alternatives, no fallbacks, no direct database access'
---

# Graph Engine Single Source Policy

Graph Engine HTTP API is the **only** permitted data source for graph/topology data in this repository.

---

## Hard Rules (No Exceptions)

### Rule 1: Graph Engine Only

All graph data MUST come from Graph Engine HTTP API:
- Service topology
- Edge metrics (rate, latency, error rate)
- Node properties (serviceId, name, namespace)
- Graph traversal results
- Any derived graph analytics

### Rule 2: No Alternatives

Copilot must **NEVER**:
- Add direct database drivers or protocol-specific connections (forbidden)
- Create "fallback" logic to other data sources
- Implement feature flags to bypass Graph Engine
- Add conditional logic like `if (graphEngineUnavailable) { useFallback() }`

### Rule 3: No Fallback Pattern

There is **NO FALLBACK**. If Graph Engine is unavailable:
- Return HTTP 503 Service Unavailable
- Include error code `GRAPH_ENGINE_UNAVAILABLE`
- Provide clear error message to client
- Log the failure (without credentials)

---

## What Counts as a Violation

| Forbidden Pattern | Why It's Blocked |
|-------------------|------------------|
| `if (!graphEngine) { return directDB.query(...) }` | Fallback to direct DB |
| `import graphDB from 'graph-db-driver'` | Direct DB dependency |
| Adding `DIRECT_DB_URI` env var | Alternative data source |
| `graphProvider.getFallback()` | Bypass architecture |
| Feature flag `USE_DB_FALLBACK` | Undermines single source |

---

## Required Patterns

### 1) Single Provider Interface

```javascript
// ✅ CORRECT: Single provider, no alternatives
class GraphEngineHttpProvider {
  async getTopology() {
    try {
      return await graphEngineClient.get('/topology');
    } catch (error) {
      // No fallback - propagate error
      throw new GraphEngineUnavailableError(error);
    }
  }
}
```

### 2) Error Propagation (No Fallback)

```javascript
// ✅ CORRECT: Fail fast, return 503
app.post('/simulate/failure', async (req, res) => {
  try {
    const graph = await graphProvider.getTopology();
    // ... simulation logic
  } catch (error) {
    if (error instanceof GraphEngineUnavailableError) {
      return res.status(503).json({
        error: 'Graph Engine unavailable',
        code: 'GRAPH_ENGINE_UNAVAILABLE'
      });
    }
    throw error;
  }
});
```

### 3) Required Environment Variable

```bash
# REQUIRED - no default, no fallback
SERVICE_GRAPH_ENGINE_URL=http://service-graph-engine:3000
# or: GRAPH_ENGINE_BASE_URL=...
```

Application must fail to start if this env var is missing.

---

## Contract Discipline

### Before Implementing Graph Engine Client Code

Copilot must verify:
- [ ] Endpoint exists in Graph Engine API documentation
- [ ] Request format is documented (params, body, headers)
- [ ] Response format is documented (schema, status codes)
- [ ] Error cases are documented (404, 500, timeout)

### If Contract is Missing

Copilot must **STOP** and ask:

> "The Graph Engine API contract for [operation] is not documented. Please provide the endpoint specification (URL, request/response format) before proceeding."

### Never Invent

Copilot must **NEVER**:
- Make up endpoint paths (e.g., `/api/services/graph`)
- Assume request body shapes
- Assume response structures
- Invent authentication patterns

---

## Enforcement Checklist

When reviewing changes that touch graph data access:

- [ ] No database driver imports (graph-db-driver, pg, mysql2, etc.)
- [ ] No fallback conditional logic
- [ ] No alternative data source env vars
- [ ] Graph Engine client is the only provider
- [ ] Errors propagate to HTTP 503 (no silent fallback)
- [ ] `graphEngineClient` module used exclusively
- [ ] Contract verified before implementation

---

## Violation Response

If Copilot detects a violation request:

1. **STOP** — Do not proceed with implementation
2. **CITE** — Reference this document (03-graph-engine-single-source)
3. **ASK** — Request explicit user override

**Example:**
> "This request adds direct database fallback logic, which violates Graph Engine Single Source Policy (03-graph-engine-single-source.instructions.md). Graph Engine is the only permitted data source. If Graph Engine is unavailable, the service must return 503. Please confirm if you want to override this policy."

---

## Quick Reference

| Situation | Copilot Action |
|-----------|----------------|
| Graph Engine down | Return 503, no fallback |
| Missing Graph Engine contract | Stop, ask for contract |
| User requests fallback logic | Stop, cite this policy, ask for override |
| New graph data access needed | Use `graphEngineClient` only |
| Alternative data source proposed | Block, cite this policy |
