---
name: graph-api-client
description: Guide for consuming the leader-owned Graph API service. Use this when asked to fetch graph data, integrate with Graph API, or understand API consumption patterns.
license: MIT
---

# Graph API Client Skill

This skill helps you consume the leader-owned Graph API service correctly in the predictive analysis engine.

## When to Use This Skill

Use this skill when you need to:
- Fetch microservice topology data
- Integrate with the Graph API service
- Understand the API-first architecture
- Add new Graph API consumption patterns

## Critical Constraints

### Graph Engine API Only Policy
Graph Engine HTTP API is the single source of truth:
1. **Use Graph Engine API** for all graph data
2. **No fallback** — If unavailable, return 503
3. **No alternatives** — No direct database access permitted

### Contract Discipline
- **Never invent endpoints** — Only use documented endpoints
- **Never invent request/response shapes** — Follow existing contracts
- **If contract is missing** — Ask the user or point out the gap
- **Require env var** — `GRAPH_API_BASE_URL` must be set

## Configuration

```javascript
// src/config.js pattern
const config = {
  graphApi: {
    baseUrl: process.env.GRAPH_API_BASE_URL || 'http://graph-api:8080',
    timeout: parseInt(process.env.GRAPH_API_TIMEOUT) || 30000,
  }
};
```

## Client Pattern

### Basic HTTP Client
```javascript
const axios = require('axios');
const config = require('./config');

async function fetchFromGraphApi(endpoint, params = {}) {
  const url = `${config.graphApi.baseUrl}${endpoint}`;
  
  try {
    const response = await axios.get(url, {
      params,
      timeout: config.graphApi.timeout,
      headers: {
        'Content-Type': 'application/json',
        'Accept': 'application/json'
      }
    });
    return response.data;
  } catch (error) {
    if (error.response) {
      // Server responded with error
      logger.error('Graph Engine error', {
        status: error.response.status,
        endpoint
      });
    } else if (error.request) {
      // No response received - service unavailable
      logger.error('Graph Engine unavailable');
      throw new GraphEngineUnavailableError('Service unreachable');
    }
    throw error;
  }
}
```

### Error Handling Pattern (No Fallback)
```javascript
async function getServiceTopology(serviceName) {
  try {
    return await fetchFromGraphEngine(`/topology`, { serviceName });
  } catch (error) {
    // No fallback - propagate error to return 503
    throw error;
  }
}
```

## Expected Endpoints (Verify Before Use)

**⚠️ These are example patterns. Verify actual contracts with leader/team.**

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/services` | GET | List all services |
| `/api/v1/services/:name` | GET | Get service details |
| `/api/v1/services/:name/dependencies` | GET | Get service dependencies |
| `/api/v1/topology` | GET | Get full topology graph |
| `/health` | GET | Health check endpoint |

## Error Handling

```javascript
class GraphApiError extends Error {
  constructor(message, statusCode, endpoint) {
    super(message);
    this.name = 'GraphApiError';
    this.statusCode = statusCode;
    this.endpoint = endpoint;
  }
}

// Usage
if (error.response?.status === 404) {
  throw new GraphApiError(
    `Endpoint not found: ${endpoint}`,
    404,
    endpoint
  );
}
```

## Environment Variables

```bash
# Required for Graph API mode
GRAPH_API_BASE_URL=http://graph-api:8080

# Optional
GRAPH_API_TIMEOUT=30000
```

## Testing Graph API Availability

```javascript
async function isGraphApiAvailable() {
  try {
    await axios.get(`${config.graphApi.baseUrl}/health`, {
      timeout: 20000
    });
    return true;
  } catch {
    return false;
  }
}
```

## When NOT to Use This Skill

- When user explicitly requests direct database access (require override approval)
- For write operations (Graph API is read-only from this service's perspective)
- When contract for needed endpoint doesn't exist (ask first!)

## References

- [src/graph.js](../../../src/graph.js) — Graph API client implementation
- [.github/instructions/02-graph-api-first.md](../../instructions/02-graph-api-first.md) — Policy documentation
