---
applyTo: "**/graphEngineClient.js,src/**/*.js,index.js"
description: 'Timeout, error handling, and health check patterns for external service dependencies'
---

# External Service Resilience Policy

This document defines required patterns for consuming external HTTP services (Graph Engine, etc.).

---

## Required Patterns

### 1) Timeouts (Always Set)

All HTTP requests to external services MUST have explicit timeouts:

```javascript
// ✅ CORRECT: Explicit timeout
const response = await axios.get(url, {
  timeout: config.GRAPH_API_TIMEOUT_MS || 20000
});

// ❌ FORBIDDEN: No timeout (hangs indefinitely)
const response = await axios.get(url);
```

**Default timeout:** 20000ms (20 seconds)  
**Configuration:** Via `GRAPH_API_TIMEOUT_MS` env var

---

### 2) Error Handling (Structured)

All external service errors MUST be caught and classified:

```javascript
// ✅ CORRECT: Structured error handling
try {
  const data = await graphEngineClient.get('/topology');
  return data;
} catch (error) {
  if (error.code === 'ECONNREFUSED' || error.code === 'ETIMEDOUT') {
    // Service unavailable
    throw new ServiceUnavailableError('Graph Engine', error);
  } else if (error.response?.status === 404) {
    // Not found
    throw new NotFoundError('Topology data not found');
  } else if (error.response?.status >= 500) {
    // Upstream server error
    throw new UpstreamError('Graph Engine', error);
  } else {
    // Unknown error
    throw error;
  }
}
```

**Error Classification:**
- `ECONNREFUSED` / `ETIMEDOUT` → Service unavailable (503)
- HTTP 404 → Not found (404)
- HTTP 500-599 → Upstream error (502)
- HTTP 400-499 → Client error (propagate status)

---

### 3) Error Response Format

When external service failures cause endpoint failures, return structured errors:

```javascript
// ✅ CORRECT: Structured error response
{
  "error": "Graph Engine unavailable",
  "code": "GRAPH_ENGINE_UNAVAILABLE",
  "message": "Unable to fetch topology data",
  "retryable": false
}
```

**Required fields:**
- `error` — Human-readable message
- `code` — Machine-readable error code
- `message` — Detailed context
- `retryable` — Boolean (true if client can retry)

**Standard error codes:**
- `GRAPH_ENGINE_UNAVAILABLE` — Service down or unreachable
- `GRAPH_ENGINE_TIMEOUT` — Request exceeded timeout
- `GRAPH_ENGINE_ERROR` — Upstream returned 500
- `INVALID_REQUEST` — Client sent bad request
- `NOT_FOUND` — Resource not found

---

### 4) Health Endpoint (Degraded State)

The `/health` endpoint MUST report degraded state when external dependencies fail:

```javascript
// ✅ CORRECT: Health check with dependency status
app.get('/health', async (req, res) => {
  const health = { status: 'ok', dependencies: {} };
  
  try {
    await graphEngineClient.get('/health', { timeout: 2000 });
    health.dependencies.graphEngine = { status: 'ok' };
  } catch (error) {
    health.dependencies.graphEngine = { 
      status: 'unavailable',
      error: error.message 
    };
    health.status = 'degraded';
  }
  
  const statusCode = health.status === 'ok' ? 200 : 503;
  res.status(statusCode).json(health);
});
```

**Health states:**
- `ok` — All dependencies healthy (200)
- `degraded` — Some dependencies down (503)
- `down` — Critical failure (503)

**Health check timeout:** 2000ms (faster than API timeout)

---

### 5) Logging (No Credentials)

Log external service failures without exposing credentials:

```javascript
// ✅ CORRECT: Redacted logging
logger.error('Graph Engine request failed', {
  endpoint: '/topology',
  url: redactUrl(fullUrl), // Remove credentials
  error: error.message,
  code: error.code,
  duration: elapsedMs
});

// ❌ FORBIDDEN: Credential exposure
logger.error('Request failed', {
  url: 'http://user:password@graph-engine:3000/topology' // BAD!
});
```

**Redaction rules:**
- Strip username/password from URLs
- Mask API tokens/keys
- Never log full Authorization headers
- Use `redactCredentials()` helper (from 04-errors-logging-secrets)

---

## Configuration

### Required Environment Variables

```bash
# Graph Engine base URL (required)
SERVICE_GRAPH_ENGINE_URL=http://service-graph-engine:3000

# Timeout for Graph Engine requests (optional, default: 20000)
GRAPH_API_TIMEOUT_MS=20000

# Health check timeout (optional, default: 2000)
HEALTH_CHECK_TIMEOUT_MS=2000
```

### Validation on Startup

Application MUST validate required env vars and fail fast:

```javascript
// ✅ CORRECT: Startup validation
if (!config.SERVICE_GRAPH_ENGINE_URL) {
  console.error('ERROR: SERVICE_GRAPH_ENGINE_URL is required');
  process.exit(1);
}
```

---

## Anti-Patterns (Forbidden)

| Anti-Pattern | Why Forbidden | Correct Approach |
|--------------|---------------|------------------|
| No timeout on HTTP requests | Hangs indefinitely | Set explicit timeout |
| Swallowing errors silently | Hides failures | Log and propagate |
| Returning 200 when dependency down | Misleads clients | Return 503 |
| Logging full URLs with credentials | Security risk | Use redactUrl() |
| Hardcoded service URLs | Not configurable | Use env vars |
| No health endpoint | Can't monitor | Add /health with deps |

---

## Testing Requirements

When adding/modifying external service integrations:

- [ ] Test timeout behavior (mock slow response)
- [ ] Test connection refused (service down)
- [ ] Test HTTP 500 errors (upstream failure)
- [ ] Test HTTP 404 errors (not found)
- [ ] Verify error response format
- [ ] Verify health endpoint reflects dependency status
- [ ] Verify no credentials in logs

---

## Enforcement Checklist

When reviewing external service client code:

- [ ] All HTTP requests have explicit timeouts
- [ ] Errors are caught and classified
- [ ] Error responses follow standard format
- [ ] Health endpoint checks dependencies
- [ ] Logs use credential redaction
- [ ] Required env vars validated on startup
- [ ] No hardcoded service URLs
- [ ] Tests cover timeout/error scenarios

---

## Quick Reference

| Scenario | Action | HTTP Status |
|----------|--------|-------------|
| Service unreachable | Return structured error | 503 |
| Request timeout | Return timeout error | 503 |
| Upstream 500 error | Return upstream error | 502 |
| Upstream 404 error | Return not found | 404 |
| Client bad request | Return validation error | 400 |
| All dependencies healthy | Health = ok | 200 |
| Any dependency down | Health = degraded | 503 |
