# Prompt: Implement Graph API Consumer

Use this prompt when implementing client code to consume the leader's Graph API.

---

## Prompt Template

```
I need to consume the Graph API for [describe the data/operation needed].

Here is the contract:
- Endpoint: [method] [path]
- Request: [describe request body/params]
- Response: [describe response format]

Please:
1. Plan the implementation following Graph API First policy
2. Include fallback to Neo4j read-only if needed
3. Use GRAPH_API_BASE_URL env var
4. Handle errors appropriately

Do NOT implement until I say "OK IMPLEMENT NOW".
```

---

## Example Usage

### With known contract

```
I need to consume the Graph API for fetching service topology.

Here is the contract:
- Endpoint: GET /api/v1/services/{serviceId}/topology
- Request: path param serviceId (format: "namespace:name")
- Response: { nodes: [...], edges: [...] }

Please:
1. Plan the implementation following Graph API First policy
2. Include fallback to Neo4j read-only if needed
3. Use GRAPH_API_BASE_URL env var
4. Handle errors appropriately

Do NOT implement until I say "OK IMPLEMENT NOW".
```

### Without contract (discovery)

```
I need to consume the Graph API for fetching service topology, but I don't have the contract documented.

Please:
1. Check if a contract exists in this repo
2. If not, tell me what information you need
3. Do not invent endpoints or response shapes

Do NOT implement until I provide the contract and say "OK IMPLEMENT NOW".
```

---

## Expected Response Format

### If contract is provided:

```
## A) Evidence Inventory
- Config pattern: [file]: `snippet`
- Existing HTTP client usage: [file]: `snippet`

## B) Proposed Plan
1. Add Graph API client module
2. Implement topology fetcher with fallback
3. Update config.js for GRAPH_API_BASE_URL
- Files: src/graphApiClient.js, src/config.js
- Risks: API unavailability, response format changes

## C) Clarifying Questions
- Authentication: Does the API require auth headers?
- Timeout: Should I use the same timeout as Neo4j (8000ms)?

## D) Waiting State
Reply with `OK IMPLEMENT NOW` when ready.
```

### If contract is missing:

```
## Contract Required

The Graph API contract is not documented in this repo. Per Graph API First policy, I cannot invent endpoints.

Please provide:
- Endpoint URL pattern
- HTTP method
- Request format (body/params)
- Response format
- Authentication requirements (if any)

Once you provide the contract, I will plan the implementation.
```

---

## Configuration Pattern

When implementing, Copilot should use:

```javascript
// config.js
graphApi: {
    baseUrl: process.env.GRAPH_API_BASE_URL,
    enabled: !!process.env.GRAPH_API_BASE_URL,
    timeoutMs: parseInt(process.env.GRAPH_API_TIMEOUT_MS) || 5000
}
```
