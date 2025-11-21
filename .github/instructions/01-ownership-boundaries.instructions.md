---
applyTo: "**/*"
description: 'Defines what this repository owns vs external team ownership - Neo4j schema, Graph API, and metrics are external'
---

# Ownership Boundaries

This document defines what this repository owns versus what is owned by external teams.

---

## Leader-Owned / Team-Owned (Treat as External)

Copilot must assume the following are **NOT owned by this repo**:

### Neo4j Schema

- **Owner:** Leader / Platform Team
- **This repo's role:** Consumer (read-only)
- **Copilot must NOT:**
  - Propose schema changes
  - Assume schema details without evidence
  - Add schema modification queries

**Important:** Any schema knowledge present in this repo (e.g., snippets in docs or comments) does not equal ownership. This repo consumes the schema; it does not define it.

### Metrics Source/Collection Architecture

- **Owner:** Leader / Platform Team
- **Components:** Prometheus, Grafana, Kiali stack
- **This repo's role:** Consumer of derived graph data
- **Copilot must NOT:**
  - Propose changes to metrics collection
  - Assume metrics availability without evidence

### Graph API Service

- **Owner:** Leader / Platform Team
- **This repo's role:** Client/consumer
- **Copilot must NOT:**
  - Invent Graph API endpoints
  - Invent request/response shapes
  - Assume contract details without documentation

---

## This Repo Owns

### What-If Simulation Logic

- All simulation algorithms
- Impact calculation formulas
- Path analysis logic

### HTTP API (This Service's Endpoints)

| Endpoint | Owner |
|----------|-------|
| `GET /health` | This repo |
| `POST /simulate/failure` | This repo |
| `POST /simulate/scale` | This repo |

### Graph API Client Code

- Client-side consumption of leader's Graph API
- Adapter patterns for graph data access
- Fallback logic when Graph API unavailable

### Neo4j Read-Only Fallback

- Read-only queries as fallback
- Query timeout enforcement
- Credential redaction

---

## Decision Matrix

| Need | First Choice | Fallback | Copilot Action |
|------|--------------|----------|----------------|
| Graph topology | Graph API | Neo4j read-only | Ask for Graph API contract first |
| Schema details | Ask leader | Evidence in repo | Never assume |
| Metrics data | Graph API | None | Do not access Prometheus directly |
| New endpoint | This repo | N/A | Plan and implement per rules |

---

## Boundary Violations

If Copilot is asked to cross a boundary, it must:

1. **Stop** — Do not proceed
2. **Cite** — Reference this document
3. **Ask** — Request explicit user override

**Example response:**

> "This request touches Neo4j schema, which is leader-owned (see `01-ownership-boundaries.md`). Copilot cannot proceed without explicit user approval to cross this boundary."
