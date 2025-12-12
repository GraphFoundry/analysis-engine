# Agent Skill: Graph Engine Integration

**Purpose:** Mechanical procedures for consuming Graph Engine API safely and consistently.

**When to use:** Adding/modifying code that fetches graph or topology data.

---

## Skill Overview

This skill provides repeatable patterns for:
1. Verifying Graph Engine API contracts
2. Implementing Graph Engine HTTP client methods
3. Adding provider layer methods
4. Creating endpoint handlers with proper error handling
5. Updating fixtures and mocks
6. Validating response schemas

---

## Procedure 1: Verify Graph Engine Contract

### Steps

1. **Locate contract documentation**
   ```bash
   # Check if contract exists in service-graph-engine repo
   ls ../service-graph-engine/docs/api/ || \
   cat ../service-graph-engine/README.md
   ```

2. **Extract endpoint details**
   - HTTP method (GET, POST, etc.)
   - URL path (e.g., `/api/topology`)
   - Query parameters
   - Request body schema
   - Response body schema
   - Status codes (200, 404, 500)

3. **If contract missing**
   - STOP implementation
   - Ask user: "Graph Engine API contract for [operation] not found. Please provide endpoint specification."

---

## Procedure 2: Implement Graph Engine Client Method

### Template

Add to `src/graphEngineClient.js`:

```javascript
/**
 * Description of what this fetches from Graph Engine
 * @param {Object} params - Query parameters
 * @returns {Promise<Object>} - Parsed response data
 * @throws {GraphEngineUnavailableError} - When service is down
 */
async getResourceName(params = {}) {
  const endpoint = '/api/resource'; // Verified endpoint path
  
  try {
    const response = await this.client.get(endpoint, {
      params,
      timeout: this.timeout,
      headers: {
        'Accept': 'application/json'
      }
    });
    
    logger.debug('Graph Engine request succeeded', {
      endpoint,
      params,
      statusCode: response.status
    });
    
    return response.data;
  } catch (error) {
    logger.error('Graph Engine request failed', {
      endpoint,
      params: redactSensitiveParams(params),
      error: error.message,
      code: error.code,
      statusCode: error.response?.status
    });
    
    throw this.handleError(error);
  }
}
```

### Checklist

- [ ] Method name is descriptive (e.g., `getTopology`, not `getData`)
- [ ] JSDoc comment explains purpose and throws
- [ ] Endpoint path is verified against contract
- [ ] Timeout is set explicitly (`this.timeout`)
- [ ] Error is logged (without credentials)
- [ ] Error is classified via `handleError()`
- [ ] No fallback logic

---

## Procedure 3: Update Provider Layer

### Template

Add to `src/providers/GraphEngineHttpProvider.js`:

```javascript
/**
 * Description matching client method
 * @param {Object} params - Parameters
 * @returns {Promise<Object>} - Data
 */
async getResourceName(params) {
  try {
    return await this.client.getResourceName(params);
  } catch (error) {
    // No fallback - propagate error
    throw error;
  }
}
```

### Checklist

- [ ] Method signature matches use case
- [ ] Delegates to `this.client` (GraphEngineClient)
- [ ] No transformation (unless required by contract)
- [ ] No fallback logic
- [ ] Error propagates to caller

---

## Procedure 4: Add Endpoint Handler

### Template

Add to `index.js`:

```javascript
/**
 * GET /api/endpoint-name
 * Description of what endpoint does
 */
app.get('/api/endpoint-name', async (req, res) => {
  try {
    // Validate input (if needed)
    const params = {
      serviceId: req.query.serviceId,
      // ... other params
    };
    
    // Fetch from Graph Engine
    const data = await graphProvider.getResourceName(params);
    
    // Return success
    res.json(data);
  } catch (error) {
    // Classify error and return appropriate status
    if (error.code === 'GRAPH_ENGINE_UNAVAILABLE') {
      return res.status(503).json({
        error: 'Graph Engine unavailable',
        code: 'GRAPH_ENGINE_UNAVAILABLE',
        message: 'Unable to fetch resource',
        retryable: true
      });
    } else if (error.response?.status === 404) {
      return res.status(404).json({
        error: 'Resource not found',
        code: 'NOT_FOUND'
      });
    } else if (error.response?.status >= 500) {
      return res.status(502).json({
        error: 'Upstream error',
        code: 'GRAPH_ENGINE_ERROR',
        message: error.message
      });
    } else {
      return res.status(500).json({
        error: 'Internal server error',
        message: error.message
      });
    }
  }
});
```

### Checklist

- [ ] Route path follows REST conventions
- [ ] Input validation (if needed)
- [ ] Calls provider method (not client directly)
- [ ] Returns 503 when Graph Engine unavailable
- [ ] Returns 404 when resource not found
- [ ] Returns 502 for upstream errors
- [ ] Error responses are structured (error, code, message)
- [ ] No silent failures

---

## Procedure 5: Update Fixtures and Mocks

### For Tests

Create mock in `test/fixtures/graph-engine-responses.js`:

```javascript
module.exports = {
  topology: {
    nodes: [
      { id: 'svc-1', name: 'service-a' },
      { id: 'svc-2', name: 'service-b' }
    ],
    edges: [
      { source: 'svc-1', target: 'svc-2', metrics: {...} }
    ]
  },
  
  resourceName: {
    // Example response structure
    id: 'res-1',
    data: {...}
  }
};
```

### In Test File

```javascript
const nock = require('nock');
const fixtures = require('./fixtures/graph-engine-responses');

test('endpoint returns data when Graph Engine available', async () => {
  // Mock Graph Engine response
  nock('http://service-graph-engine:3000')
    .get('/api/resource')
    .query({ serviceId: 'svc-1' })
    .reply(200, fixtures.resourceName);
  
  const response = await request(app)
    .get('/api/endpoint-name?serviceId=svc-1');
  
  assert.strictEqual(response.status, 200);
  assert.deepStrictEqual(response.body, fixtures.resourceName);
});

test('endpoint returns 503 when Graph Engine unavailable', async () => {
  // Mock connection failure
  nock('http://service-graph-engine:3000')
    .get('/api/resource')
    .replyWithError({ code: 'ECONNREFUSED' });
  
  const response = await request(app)
    .get('/api/endpoint-name?serviceId=svc-1');
  
  assert.strictEqual(response.status, 503);
  assert.strictEqual(response.body.code, 'GRAPH_ENGINE_UNAVAILABLE');
  assert.strictEqual(response.body.retryable, true);
});
```

### Checklist

- [ ] Fixtures match real Graph Engine response structure
- [ ] Tests cover success path (200)
- [ ] Tests cover service unavailable (503)
- [ ] Tests cover timeout scenario
- [ ] Tests cover upstream error (500 → 502)
- [ ] Tests cover not found (404)
- [ ] Nock mocks use correct URL and path

---

## Procedure 6: Validate Response Schemas

### Manual Validation

```bash
# Start Graph Engine (if available locally)
cd ../service-graph-engine && npm start &

# Test actual endpoint
curl -v http://localhost:3000/api/resource?serviceId=svc-1 | jq .

# Compare with fixture
diff <(curl -s http://localhost:3000/api/resource | jq -S .) \
     <(cat test/fixtures/graph-engine-responses.js | jq -S .resource)
```

### Automated Schema Validation (if available)

```javascript
const Ajv = require('ajv');
const ajv = new Ajv();

const schema = {
  type: 'object',
  required: ['nodes', 'edges'],
  properties: {
    nodes: { type: 'array' },
    edges: { type: 'array' }
  }
};

const validate = ajv.compile(schema);
const valid = validate(response.data);

if (!valid) {
  console.error('Schema validation failed:', validate.errors);
}
```

---

## Procedure 7: Update OpenAPI Spec

### Add Endpoint

Edit `openapi.yaml`:

```yaml
paths:
  /api/endpoint-name:
    get:
      summary: Brief description
      operationId: getResourceName
      tags:
        - graph-engine
      parameters:
        - name: serviceId
          in: query
          required: true
          schema:
            type: string
          description: Service identifier
      responses:
        '200':
          description: Success
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ResourceResponse'
        '404':
          description: Resource not found
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Error'
        '503':
          description: Graph Engine unavailable
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ServiceUnavailableError'
```

### Add Schema Component

```yaml
components:
  schemas:
    ResourceResponse:
      type: object
      required:
        - id
        - data
      properties:
        id:
          type: string
        data:
          type: object
```

### Bump Version

```yaml
info:
  version: 1.2.3  # Increment patch or minor
```

### Checklist

- [ ] Endpoint added to `paths:`
- [ ] All parameters documented
- [ ] All status codes documented (200, 404, 503, etc.)
- [ ] Response schemas defined in `components/schemas:`
- [ ] Version bumped
- [ ] Swagger UI validates (if enabled)

---

## Final Verification Commands

### 1. Code Scan
```bash
# No direct database access in runtime code
git grep -n -E "bolt://|driver\.session" -- src/ test/

# No fallback logic
git grep -n -i "fallback" src/

# Verify Graph Engine client usage
git grep -n "graphEngineClient" src/
```

### 2. Test Execution
```bash
npm test
```

### 3. OpenAPI Validation
```bash
# Install validator (if not installed)
npm install -g @apidevtools/swagger-cli

# Validate spec
swagger-cli validate openapi.yaml
```

### 4. Manual Testing
```bash
# Start service
npm start &

# Test new endpoint
curl -v http://localhost:3000/api/endpoint-name?serviceId=svc-1

# Test error case (stop Graph Engine first)
curl -v http://localhost:3000/api/endpoint-name?serviceId=svc-1
# Should return 503

# Cleanup
kill %1
```

---

## Common Patterns

### Pattern: Timeout Configuration
```javascript
const timeout = config.GRAPH_API_TIMEOUT_MS || 20000;
```

### Pattern: Error Classification
```javascript
handleError(error) {
  if (error.code === 'ECONNREFUSED' || error.code === 'ETIMEDOUT') {
    throw new GraphEngineUnavailableError(error);
  } else if (error.response?.status >= 500) {
    throw new GraphEngineUpstreamError(error);
  } else {
    throw error;
  }
}
```

### Pattern: Credential Redaction
```javascript
function redactSensitiveParams(params) {
  const redacted = { ...params };
  if (redacted.apiKey) redacted.apiKey = '***';
  if (redacted.token) redacted.token = '***';
  return redacted;
}
```

---

## Anti-Patterns to Avoid

| Anti-Pattern | Problem | Correct Approach |
|--------------|---------|------------------|
| `if (!graphEngine) { useDirectDB() }` | Fallback violates policy | Return 503 |
| No timeout | Hangs indefinitely | Set explicit timeout |
| Swallow errors | Hides failures | Propagate to caller |
| Invent endpoint | Contract violation | Verify endpoint exists |
| Skip tests | No regression safety | Add success + failure tests |
| Hardcode URL | Not configurable | Use env var |

---

## Success Criteria

Integration is complete when:

- [x] Contract verified (endpoint exists in Graph Engine docs)
- [x] Client method added with timeout and error handling
- [x] Provider method added (no fallback)
- [x] Endpoint handler added with 503 error handling
- [x] Tests added (success + failure scenarios)
- [x] Fixtures/mocks updated
- [x] OpenAPI spec updated and validated
- [x] Documentation updated
- [x] Verification commands pass
- [x] No direct database access introduced
- [x] No fallback logic added
