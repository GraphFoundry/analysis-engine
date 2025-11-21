# AGENTS.md — what-if-simulation-engine

This file provides universal agent instructions compatible with GitHub Copilot coding agent, OpenAI Codex, Claude, and any agent following the [openai/agents.md](https://github.com/openai/agents.md) standard.

---

## Project Overview

**What this is:** A what-if simulation engine for microservice call graphs. It analyzes microservice topologies and simulates failure/scaling scenarios to predict system behavior.

**Tech Stack:**
- **Runtime:** Node.js (CommonJS)
- **Framework:** Express.js
- **Database:** Neo4j (read-only access)
- **External Dependency:** Graph API (leader-owned, consumed via HTTP)

**Key Files:**
- `index.js` — Main entry point, Express server setup
- `src/graph.js` — Graph API client consumption
- `src/neo4j.js` — Neo4j read-only fallback with credential redaction
- `src/failureSimulation.js` — Failure scenario simulation logic
- `src/scalingSimulation.js` — Scaling scenario simulation logic
- `src/config.js` — Environment configuration
- `src/validator.js` — Request validation

---

## Commands

### Install Dependencies
```bash
npm install
```

### Run the Application
```bash
npm start
```
Server starts on port defined by `PORT` env var (default: 3000).

### Run Tests
```bash
npm test
```
Uses Node.js built-in test runner.

### Verify Neo4j Schema (Read-only)
```bash
npm run verify
```

### Environment Variables Required
```bash
# Required
NEO4J_URI=bolt://localhost:7687
NEO4J_USER=neo4j
NEO4J_PASSWORD=<password>

# Optional (Graph API mode)
GRAPH_API_BASE_URL=http://graph-api:8080

# Optional
PORT=3000
```

---

## Boundaries (Critical)

### ✅ ALWAYS DO
- Use read-only Neo4j queries (`defaultAccessMode: neo4j.session.READ`)
- Prefer Graph API over direct Neo4j access
- Follow the plan-first workflow: inventory → plan → questions → wait for approval
- Redact credentials in logs (use `redactCredentials()` from `src/neo4j.js`)
- Provide evidence (file path + snippet) when stating facts

### ⚠️ ASK FIRST
- Before consuming a new Graph API endpoint (verify contract exists)
- Before modifying any existing simulation logic
- Before adding new dependencies

### 🚫 NEVER DO
- Write to Neo4j (all queries must be read-only)
- Modify Neo4j schema
- Add CI/CD workflows (`.github/workflows/*`)
- Add or modify tests without explicit approval
- Log secrets, passwords, or connection strings
- Invent Graph API endpoints or request/response shapes
- Implement without user typing `OK IMPLEMENT NOW`

---

## Architecture

```
┌─────────────────┐     ┌──────────────┐     ┌─────────────┐
│   HTTP Client   │────▶│  Express API │────▶│  Graph API  │ (preferred)
└─────────────────┘     └──────────────┘     └─────────────┘
                              │
                              │ fallback only
                              ▼
                        ┌─────────────┐
                        │   Neo4j     │ (read-only)
                        │  (fallback) │
                        └─────────────┘
```

### Data Flow Priority
1. **Graph API** — Always try first (leader-owned service)
2. **Neo4j** — Fallback only when Graph API unavailable or missing capability

---

## File Structure

```
├── index.js                 # Express server entry point
├── package.json             # Dependencies and scripts
├── src/
│   ├── config.js            # Environment configuration
│   ├── failureSimulation.js # Failure scenario logic
│   ├── scalingSimulation.js # Scaling scenario logic
│   ├── graph.js             # Graph API client
│   ├── neo4j.js             # Neo4j read-only client + redaction
│   └── validator.js         # Request validation
├── k8s/
│   └── base/                # Kubernetes manifests
├── test/
│   └── simulation.test.js   # Test file
└── docs/
    └── COPILOT-USAGE-GUIDE.md
```

---

## Code Style

- **Naming:** camelCase for variables/functions, PascalCase for classes
- **Async:** Use async/await, not callbacks
- **Error handling:** Always wrap Neo4j/API calls in try-catch, redact credentials
- **Logging:** Never log secrets; use `redactCredentials()` pattern

---

## Additional Context

For detailed Copilot-specific rules, see:
- `.github/copilot-instructions.md` — Master instruction file
- `.github/agents/` — Custom agent personas (planner, implementer, reviewer)
- `.github/instructions/` — Path-specific coding standards
- `.github/prompts/` — Reusable task prompts
- `.github/skills/` — Agent skills for specialized workflows
