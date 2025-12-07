# AGENTS.md — predictive-analysis-engine

This file provides universal agent instructions compatible with GitHub Copilot coding agent, OpenAI Codex, Claude, and any agent following the [openai/agents.md](https://github.com/openai/agents.md) standard.

---

## Project Overview

**What this is:** A predictive analysis engine for microservice call graphs. It analyzes microservice topologies and simulates failure/scaling scenarios to predict system behavior.

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
- **Add/update tests** for behavioral changes when test framework exists (see Testing Policy in `.github/copilot-instructions.md`)
- **Update relevant docs** when behavior/config/API changes
- **Update governance files** when workflows/standards are impacted
- **Update `openapi.yaml`** for any API add/change/removal (see `.github/copilot-instructions.md` §0.4)

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
├── .github/
│   ├── copilot-instructions.md  # Master Copilot instruction file
│   ├── agents/
│   │   ├── planner.agent.md     # Plan-first workflow agent
│   │   ├── implementer.agent.md # Code execution agent (requires approval)
│   │   └── reviewer.agent.md    # Change validation agent
│   ├── prompts/
│   │   ├── 01-plan-change.prompt.md
│   │   ├── 02-implement-approved-plan.prompt.md
│   │   ├── 03-graph-api-consumer.prompt.md
│   │   ├── 04-neo4j-fallback.prompt.md
│   │   ├── 05-add-or-change-endpoint.prompt.md
│   │   ├── 06-docs-update.prompt.md
│   │   └── 07-pr-summary.prompt.md
│   ├── instructions/
│   │   ├── 00-operating-rules.instructions.md
│   │   ├── 01-ownership-boundaries.instructions.md
│   │   ├── 02-graph-api-first.instructions.md
│   │   ├── 03-neo4j-readonly-fallback.instructions.md
│   │   ├── 04-errors-logging-secrets.instructions.md
│   │   └── 05-k8s-minikube-scope.instructions.md
│   └── skills/
│       ├── graph-api-client/SKILL.md
│       ├── k8s-deployment/SKILL.md
│       ├── neo4j-readonly/SKILL.md
│       └── simulation-runner/SKILL.md
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

### Master Configuration
- `.github/copilot-instructions.md` — Single source of truth for Copilot behavior

### Agent Personas (select from dropdown in Chat)
- `.github/agents/planner.agent.md` — Analyze, gather evidence, produce plans
- `.github/agents/implementer.agent.md` — Execute approved plans (requires `OK IMPLEMENT NOW`)
- `.github/agents/reviewer.agent.md` — Validate changes against rules

### Path-Specific Instructions (auto-applied)
- `.github/instructions/00-operating-rules.instructions.md` — Implementation lock, evidence requirements
- `.github/instructions/01-ownership-boundaries.instructions.md` — What this repo owns
- `.github/instructions/02-graph-api-first.instructions.md` — Graph API over Neo4j
- `.github/instructions/03-neo4j-readonly-fallback.instructions.md` — Read-only Neo4j
- `.github/instructions/04-errors-logging-secrets.instructions.md` — Security rules
- `.github/instructions/05-k8s-minikube-scope.instructions.md` — K8s context

### Agent Skills (auto-loaded based on context)
- `.github/skills/neo4j-readonly/` — Safe Cypher query patterns
- `.github/skills/graph-api-client/` — Graph API consumption patterns
- `.github/skills/simulation-runner/` — Simulation logic patterns
- `.github/skills/k8s-deployment/` — Kubernetes deployment patterns

### Reusable Prompts (invoke with `/` in chat)
- `.github/prompts/*.prompt.md` — 7 workflow templates

> **Note:** Custom agents appear in the **agent dropdown** in Chat, not via `@` mentions.
- `.github/prompts/` — Reusable task prompts
- `.github/skills/` — Agent skills for specialized workflows
