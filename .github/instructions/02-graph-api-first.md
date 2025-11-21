---
applyTo: "**/graph.js,**/api/**/*.js,src/**/*.js"
---

# Graph API First Policy

When Copilot needs graph or topology data, it must prefer the leader's Graph API over direct Neo4j access.

---

## Decision Hierarchy

```
1. Graph API (preferred)
       ↓ (only if unavailable or missing capability)
2. Neo4j read-only fallback
```

---

## When to Use Graph API

Copilot must use Graph API when:

- Fetching service topology
- Retrieving edge metrics (rate, latency, error rate)
- Getting node properties (serviceId, name, namespace)
- Any graph traversal operation

---

## When Neo4j Fallback is Allowed

Copilot may use Neo4j read-only access only when:

1. **Graph API is missing the required capability** — Document which capability is missing
2. **Graph API is unavailable** — Temporary outage or not deployed
3. **User explicitly requests Neo4j usage** — Must be documented in plan

---

## Contract Discipline

### Before Implementing Graph API Client

Copilot must verify:

- [ ] Contract document exists in repo OR user has provided it
- [ ] Endpoint URL pattern is documented
- [ ] Request format is documented
- [ ] Response format is documented

### If Contract is Missing

Copilot must **STOP** and ask:

> "The Graph API contract for [operation] is not documented in this repo. Please provide the contract (endpoint, request/response format) or confirm that Neo4j fallback should be used."

### Never Invent

Copilot must **NEVER**:

- Make up endpoint paths (e.g., `/api/graph/services`)
- Make up request body shapes
- Make up response structures
- Assume authentication patterns

---

## Configuration

### Required Environment Variable

When Graph API mode is enabled, require:

```bash
GRAPH_API_BASE_URL=http://graph-api-service:8080
```

### Configuration Pattern

```javascript
// Example: config.js
graphApi: {
    baseUrl: process.env.GRAPH_API_BASE_URL,
    enabled: !!process.env.GRAPH_API_BASE_URL,
    timeoutMs: parseInt(process.env.GRAPH_API_TIMEOUT_MS) || 5000
}
```

---

## Implementation Pattern

When implementing Graph API consumption:

```javascript
// Preferred: Graph API client with fallback
async function getServiceTopology(serviceId) {
    if (config.graphApi.enabled) {
        return await graphApiClient.getTopology(serviceId);
    }
    
    // Fallback: Neo4j read-only (document why)
    console.log('Graph API unavailable, using Neo4j fallback');
    return await neo4jFallback.getTopology(serviceId);
}
```

---

## Quick Reference

| Situation | Copilot Action |
|-----------|----------------|
| Need graph data | Check for Graph API contract first |
| Contract exists | Implement Graph API client |
| Contract missing | Stop, ask user for contract |
| Graph API unavailable | Use Neo4j fallback, document reason |
| User requests Neo4j | Confirm in plan, proceed with read-only |
