# Prompt: Neo4j Fallback Query

Use this prompt when you need to add or modify Neo4j read-only queries.

---

## Prompt Template

```
I need a Neo4j query to [describe what you need].

Please:
1. Verify this is read-only (MATCH only, no writes)
2. Check for similar existing queries in the codebase
3. Follow the existing timeout and error handling patterns
4. Preserve credential redaction
5. Document why Graph API fallback is needed

Do NOT implement until I say "OK IMPLEMENT NOW".
```

---

## Example Usage

### Adding a new query

```
I need a Neo4j query to find all services with error rate above a threshold.

Please:
1. Verify this is read-only (MATCH only, no writes)
2. Check for similar existing queries in the codebase
3. Follow the existing timeout and error handling patterns
4. Preserve credential redaction
5. Document why Graph API fallback is needed

Reason for fallback: Graph API doesn't support filtering by error rate threshold.

Do NOT implement until I say "OK IMPLEMENT NOW".
```

### Modifying an existing query

```
I need to modify the upstream neighborhood query to also return pagerank scores.

Please:
1. Verify the modification is still read-only
2. Show the current query and proposed changes
3. Preserve timeout and error handling patterns
4. Note any schema assumptions being made

Do NOT implement until I say "OK IMPLEMENT NOW".
```

---

## Expected Response Format

```
## A) Evidence Inventory
- Existing query pattern: [graph.js]: `snippet`
- Timeout pattern: [neo4j.js]: `snippet`
- Redaction pattern: [neo4j.js]: `snippet`

## B) Proposed Query

```cypher
MATCH (s:Service)-[r:CALLS_NOW]->(t:Service)
WHERE r.errorRate > $threshold
RETURN s.serviceId AS source, t.serviceId AS target, r.errorRate AS errorRate
```

## C) Plan
1. Add query function in graph.js
2. Add validation in validator.js
3. Expose via new endpoint or existing

## D) Schema Assumptions
- Using: errorRate property on CALLS_NOW (evidenced in existing queries)
- Unknown: Whether all edges have errorRate populated

## E) Fallback Justification
Graph API doesn't support: [describe missing capability]

## F) Waiting State
Reply with `OK IMPLEMENT NOW` when ready.
```

---

## Checklist Before Approval

- [ ] Query is read-only (no CREATE, MERGE, DELETE, SET)
- [ ] Uses executeQuery() with timeout
- [ ] Errors passed through redactCredentials()
- [ ] Fallback reason documented
- [ ] Schema assumptions are evidenced
