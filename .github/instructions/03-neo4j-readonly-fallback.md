# Neo4j Read-Only Fallback Policy

This document governs how Copilot must handle Neo4j access in this repository.

---

## Core Principle

**Runtime queries must be read-only.** Any schema or write queries are validation/legacy and must not be expanded without leader approval.

---

## Runtime Query Rules

### Read-Only Enforcement

All runtime Neo4j sessions must use read-only mode:

```javascript
// REQUIRED pattern (from src/neo4j.js)
const session = driver.session({
    defaultAccessMode: neo4j.session.READ
});
```

### Allowed Operations

| Operation | Allowed | Example |
|-----------|---------|---------|
| MATCH | ✅ | `MATCH (s:Service) RETURN s` |
| OPTIONAL MATCH | ✅ | `OPTIONAL MATCH (a)-[r]->(b)` |
| WITH, WHERE, RETURN | ✅ | Query filtering and projection |
| UNWIND (read context) | ✅ | Processing arrays in read queries |

### Forbidden Operations

| Operation | Forbidden | Reason |
|-----------|-----------|--------|
| CREATE | ❌ | Write operation |
| MERGE | ❌ | Write operation |
| DELETE | ❌ | Write operation |
| SET | ❌ | Write operation |
| REMOVE | ❌ | Write operation |
| CREATE CONSTRAINT | ❌ | Schema modification |
| CREATE INDEX | ❌ | Schema modification |
| DROP | ❌ | Schema/data destruction |

---

## Schema/Write Queries (Legacy)

If schema or write queries exist in the codebase:

1. **Do not touch them** — They may be legacy or validation scripts
2. **Do not expand them** — No new write operations
3. **Do not call them from runtime code** — Keep them isolated
4. **Require leader approval** — Before any modification

**Hard Rule:** Copilot must never introduce or modify Neo4j write/schema logic unless the user explicitly approves.

---

## Existing Safeguards

The codebase has these safeguards that must be preserved:

### Two-Layer Timeout

```javascript
// From src/neo4j.js — PRESERVE THIS PATTERN
const queryPromise = session.run(query, params, {
    timeout: timeoutMs
});

const timeoutPromise = new Promise((_, reject) => {
    setTimeout(() => reject(new Error('Query timeout exceeded')), timeoutMs);
});

const result = await Promise.race([queryPromise, timeoutPromise]);
```

### Credential Redaction

```javascript
// From src/neo4j.js — PRESERVE THIS PATTERN
function redactCredentials(message) {
    if (!message) return message;
    return message
        .replace(new RegExp(config.neo4j.password, 'g'), '[REDACTED]')
        .replace(/password=([^&\s]+)/gi, 'password=[REDACTED]');
}
```

---

## Schema Assumptions

Copilot must **NOT** assume schema details unless evidenced by a snippet in this repo.

### Known Schema (Evidenced)

Based on queries in `src/graph.js`:

- Node label: `Service`
- Node properties: `serviceId`, `name`, `namespace`
- Relationship type: `CALLS_NOW`
- Relationship properties: `rate`, `errorRate`, `p50`, `p95`, `p99`

### Unknown Schema (Not Evidenced)

Copilot must say "Unknown (not evidenced yet)" for:

- Additional node labels
- Additional relationship types
- Index or constraint definitions
- Any property not seen in actual queries

---

## Fallback Conditions

Neo4j fallback is allowed only when:

| Condition | Action |
|-----------|--------|
| Graph API missing capability | Document which capability, use fallback |
| Graph API unavailable | Log warning, use fallback |
| User explicitly requests | Document in plan, proceed |

---

## Quick Reference

| Situation | Copilot Action |
|-----------|----------------|
| Need to add a query | Read-only only, preserve timeouts |
| See existing write query | Do not modify, report to user |
| Asked to add write query | Refuse, cite this document |
| Need schema info | Quote from existing queries, else "Unknown" |
| Modifying neo4j.js | Preserve redactCredentials, preserve timeouts |
