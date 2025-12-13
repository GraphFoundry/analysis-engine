---
name: "Graph Engine Integration Workflow"
description: "Step-by-step workflow for adding or modifying Graph Engine API dependencies"
---

# Graph Engine Integration Workflow

Use this prompt when adding new Graph Engine API endpoints or modifying existing integrations.

---

## Trigger Conditions

Use this workflow when:
- Adding a new endpoint that needs graph/topology data
- Modifying existing graph data consumption
- Changing Graph Engine API client code
- Updating graph data provider logic

---

## Pre-Implementation Checklist

Before writing code, verify:

### 1) Contract Validation
- [ ] Graph Engine API endpoint is documented
- [ ] Request format is known (URL, params, body, headers)
- [ ] Response format is known (schema, status codes)
- [ ] Error cases are documented (404, 500, timeout)

**If contract is missing:** STOP and ask user for endpoint specification.

### 2) Policy Compliance
- [ ] Review `.github/instructions/03-graph-engine-single-source.instructions.md`
- [ ] Review `.github/instructions/06-external-service-resilience.instructions.md`
- [ ] Confirm no fallback logic will be added
- [ ] Confirm timeout/error handling will be implemented

### 3) Existing Code Audit
- [ ] Search for similar Graph Engine API usage: `git grep -n "graphEngineClient"`
- [ ] Identify existing patterns to follow
- [ ] Check for existing error handling helpers

---

## Implementation Steps

### Step 1: Define Graph Engine Client Method

Add method to `src/graphEngineClient.js`:

```javascript
async getNewResource(params) {
  const url = `/api/new-resource`; // Verify endpoint exists
  
  try {
    const response = await this.client.get(url, {
      params,
      timeout: this.timeout
    });
    return response.data;
  } catch (error) {
    logger.error('Graph Engine request failed', {
      endpoint: url,
      params,
      error: error.message,
      code: error.code
    });
    throw this.handleError(error);
  }
}
```

**Required:**
- Explicit timeout
- Error logging (no credentials)
- Error classification via `handleError()`

### Step 2: Update Provider Layer

Update `src/providers/GraphEngineHttpProvider.js`:

```javascript
async getNewData(params) {
  try {
    return await this.client.getNewResource(params);
  } catch (error) {
    // No fallback - propagate error
    throw error;
  }
}
```

**Critical:** No fallback logic, no alternative data sources.

### Step 3: Add Endpoint Handler

Add route in `index.js`:

```javascript
app.get('/api/new-endpoint', async (req, res) => {
  try {
    const data = await graphProvider.getNewData(req.query);
    res.json(data);
  } catch (error) {
    if (error.code === 'GRAPH_ENGINE_UNAVAILABLE') {
      return res.status(503).json({
        error: 'Graph Engine unavailable',
        code: 'GRAPH_ENGINE_UNAVAILABLE',
        retryable: true
      });
    }
    // Handle other errors
    res.status(500).json({ error: error.message });
  }
});
```

**Required:**
- Return 503 when Graph Engine unavailable
- Structured error responses
- No silent failures

### Step 4: Update OpenAPI Spec

Update `openapi.yaml`:

```yaml
paths:
  /api/new-endpoint:
    get:
      summary: New endpoint description
      parameters: [...]
      responses:
        200:
          description: Success
          content:
            application/json:
              schema: {...}
        503:
          description: Graph Engine unavailable
          content:
            application/json:
              schema:
                type: object
                properties:
                  error:
                    type: string
                  code:
                    type: string
                  retryable:
                    type: boolean
```

**Bump version** in `info.version` (patch or minor).

### Step 5: Add Tests

Create test in `test/`:

```javascript
test('new endpoint returns data when Graph Engine available', async () => {
  // Mock Graph Engine response
  nock('http://graph-engine:3000')
    .get('/api/new-resource')
    .reply(200, { data: [...] });
  
  const response = await request(app).get('/api/new-endpoint');
  assert.strictEqual(response.status, 200);
});

test('new endpoint returns 503 when Graph Engine unavailable', async () => {
  // Mock Graph Engine failure
  nock('http://graph-engine:3000')
    .get('/api/new-resource')
    .replyWithError({ code: 'ECONNREFUSED' });
  
  const response = await request(app).get('/api/new-endpoint');
  assert.strictEqual(response.status, 503);
  assert.strictEqual(response.body.code, 'GRAPH_ENGINE_UNAVAILABLE');
});
```

**Required test scenarios:**
- Happy path (Graph Engine returns 200)
- Service unavailable (connection refused)
- Timeout (slow response)
- Upstream error (Graph Engine returns 500)

---

## Post-Implementation Verification

### 1) Code Quality Checks

Run these commands:

```bash
# No direct database drivers in runtime code
git grep -n -E "(require|import).*driver" -- src/ test/ | grep -v graphEngine

# No fallback logic
git grep -n -i "fallback.*database" -- src/

# Verify timeout usage
git grep -n "timeout:" src/graphEngineClient.js

# Verify error handling
git grep -n "catch (error)" src/
```

### 2) Test Execution

```bash
npm test
```

All tests must pass.

### 3) OpenAPI Validation

If Swagger UI is enabled:

```bash
ENABLE_SWAGGER=true npm start
# Visit http://localhost:3000/swagger
# Verify new endpoint appears and is valid
```

### 4) Documentation Updates

- [ ] Update README.md if new endpoint added
- [ ] Update DEPLOYMENT.md if new env vars required
- [ ] Update docs/COPILOT-USAGE-GUIDE.md if workflow changed

---

## Regression Prevention Checklist

- [ ] No database driver imports added
- [ ] No fallback conditional logic
- [ ] No alternative data source env vars
- [ ] `graphEngineClient` used exclusively
- [ ] Errors propagate to HTTP 503 (not swallowed)
- [ ] Tests cover both success and failure paths
- [ ] OpenAPI spec matches implementation
- [ ] Docs updated

---

## Final Summary Template

After implementation, provide this summary:

```
## Changes Made

### Files Modified:
- `src/graphEngineClient.js` — Added getNewResource() method
- `src/providers/GraphEngineHttpProvider.js` — Added getNewData() method
- `index.js` — Added /api/new-endpoint route
- `openapi.yaml` — Added endpoint specification, bumped version
- `test/new-endpoint.test.js` — Added tests (success + failure)

### Key Patterns Followed:
✅ Graph Engine single source (03-graph-engine-single-source)
✅ Timeout/error handling (06-external-service-resilience)
✅ OpenAPI updated (§0.4)
✅ Tests added (§0.3)

### Verification Results:
✅ No direct DB access: git grep clean
✅ No fallback logic: git grep clean
✅ Tests passing: npm test
✅ OpenAPI valid: Swagger UI validates

### Manual Checks Required:
- [ ] Start service: npm start
- [ ] Test endpoint: curl http://localhost:3000/api/new-endpoint
- [ ] Test Graph Engine down scenario: stop Graph Engine, verify 503
```

---

## When to Deviate

This workflow can be skipped for:
- Documentation-only changes
- Non-graph-related features
- Internal refactoring (no API changes)

Always follow this workflow for:
- New Graph Engine API consumption
- Modifying existing graph data access
- Changes to error handling patterns
