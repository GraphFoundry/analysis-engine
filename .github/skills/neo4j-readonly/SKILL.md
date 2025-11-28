---
name: neo4j-readonly
description: Guide for writing read-only Neo4j Cypher queries in this project. Use this when asked to query Neo4j, access graph data via fallback, or write Cypher queries.
license: MIT
---

# Neo4j Read-Only Query Skill

This skill helps you write safe, read-only Neo4j queries for the predictive analysis engine.

## When to Use This Skill

Use this skill when you need to:
- Write Cypher queries to fetch graph topology data
- Access Neo4j as a fallback when Graph API is unavailable
- Debug or validate Neo4j query patterns
- Understand the read-only constraints of this project

## Critical Constraints

### Read-Only Enforcement
All Neo4j sessions in this project MUST use read-only mode:
```javascript
const session = driver.session({
  database: 'neo4j',
  defaultAccessMode: neo4j.session.READ  // MANDATORY
});
```

### Never Use These Operations
- `CREATE` — Never create nodes or relationships
- `MERGE` — Never merge/upsert data
- `SET` — Never modify properties
- `DELETE` / `DETACH DELETE` — Never delete anything
- `REMOVE` — Never remove properties or labels
- Schema operations (`CREATE INDEX`, `CREATE CONSTRAINT`, etc.)

## Query Patterns

### Fetch All Services
```cypher
MATCH (s:Service)
RETURN s.name AS name, s.namespace AS namespace, s.replicas AS replicas
```

### Fetch Service Dependencies
```cypher
MATCH (s:Service)-[r:CALLS]->(t:Service)
RETURN s.name AS source, t.name AS target, r.latency AS latency
```

### Fetch Subgraph for Simulation
```cypher
MATCH path = (s:Service {name: $serviceName})-[:CALLS*0..3]->(t:Service)
RETURN path
```

### Check Service Health Metrics
```cypher
MATCH (s:Service {name: $serviceName})
RETURN s.errorRate AS errorRate, s.latencyP99 AS latencyP99, s.cpu AS cpu
```

## Error Handling Pattern

Always wrap Neo4j operations with proper error handling and credential redaction:

```javascript
const { redactCredentials } = require('./neo4j');

async function queryGraph(query, params) {
  const session = driver.session({
    database: 'neo4j',
    defaultAccessMode: neo4j.session.READ
  });
  
  try {
    const result = await session.run(query, params);
    return result.records.map(record => record.toObject());
  } catch (error) {
    // CRITICAL: Redact credentials before logging
    console.error('Neo4j query failed:', redactCredentials(error.message));
    throw error;
  } finally {
    await session.close();
  }
}
```

## Timeout Configuration

This project uses two-layer timeout protection:
1. **Driver-level:** Connection timeout in driver config
2. **Query-level:** Transaction timeout for long-running queries

```javascript
const session = driver.session({
  database: 'neo4j',
  defaultAccessMode: neo4j.session.READ
});

// With query timeout
await session.executeRead(tx => 
  tx.run(query, params),
  { timeout: 30000 }  // 30 second timeout
);
```

## When to Use Neo4j vs Graph API

| Scenario | Use |
|----------|-----|
| Graph API available | Graph API (preferred) |
| Graph API unavailable | Neo4j fallback |
| Graph API missing capability | Neo4j fallback |
| User explicitly requests Neo4j | Neo4j fallback |
| Write operations needed | ❌ NOT ALLOWED |

## Environment Variables

Required for Neo4j connection:
```
NEO4J_URI=bolt://localhost:7687
NEO4J_USER=neo4j
NEO4J_PASSWORD=<password>
```

## References

- [src/neo4j.js](../../../src/neo4j.js) — Neo4j client implementation
- [.github/instructions/03-neo4j-readonly-fallback.md](../../instructions/03-neo4j-readonly-fallback.md) — Policy documentation
