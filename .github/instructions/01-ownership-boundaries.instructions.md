---
applyTo: "**/*"
description: 'Defines what this repository owns vs external team ownership - Graph Engine schema and metrics are external'
---

# Ownership Boundaries

This document defines what this repository owns versus what is owned by external teams.

---

## Leader-Owned / Team-Owned (Treat as External)

Copilot must assume the following are **NOT owned by this repo**:

### Graph Engine Schema

- **Owner:** Leader / Platform Team (via service-graph-engine)
- **This repo's role:** Consumer via HTTP API
- **Copilot must NOT:**
  - Propose schema changes
  - Assume schema details without evidence from Graph Engine API documentation
  - Invent Graph Engine endpoints

**Important:** This repo consumes graph data via HTTP API; it does not define the schema or data model.

### Metrics Source/Collection Architecture

- **Owner:** Leader / Platform Team
- **Components:** Prometheus, Grafana, Kiali stack
- **This repo's role:** Consumer of derived graph data
- **Copilot must NOT:**
  - Propose changes to metrics collection
  - Assume metrics availability without evidence

### Graph Engine API Service

- **Owner:** Leader / Platform Team (service-graph-engine)
- **This repo's role:** HTTP client/consumer
- **Copilot must NOT:**
  - Invent Graph Engine API endpoints
  - Invent request/response shapes
  - Assume contract details without documentation

---

## This Repo Owns

### Predictive Analysis Simulation Logic

- All simulation algorithms
- Impact calculation formulas
- Path analysis logic

### HTTP API (This Service's Endpoints)

| Endpoint | Owner |
|----------|-------|
| `GET /health` | This repo |
| `POST /simulate/failure` | This repo |
| `POST /simulate/scale` | This repo |

### Graph Engine HTTP Client Code

- Client-side consumption of Graph Engine API
- Adapter patterns for graph data access
- Error handling when Graph Engine unavailable (return 503)

---

## Decision Matrix

| Need | Data Source | Fallback | Copilot Action |
|------|-------------|----------|----------------|
| Graph topology | Graph Engine API | None (return 503) | Use GraphEngineHttpProvider |
| Schema details | Ask leader | Evidence in repo | Never assume |
| Metrics data | Graph Engine API | None (return 503) | Do not access Prometheus directly |
| New endpoint | This repo | N/A | Plan and implement per rules |

---

## Boundary Violations

If Copilot is asked to cross a boundary, it must:

1. **Stop** — Do not proceed
2. **Cite** — Reference this document
3. **Ask** — Request explicit user override

**Example response:**

> "This request touches Graph Engine schema, which is leader-owned (see `01-ownership-boundaries.md`). Copilot cannot proceed without explicit user approval to cross this boundary."
