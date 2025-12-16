# AGENTS.md — predictive-analysis-engine

This file provides universal agent instructions compatible with GitHub Copilot coding agent, OpenAI Codex, Claude, and any agent following the [openai/agents.md](https://github.com/openai/agents.md) standard.

---

## Project Overview

**What this is:** A predictive analysis engine for microservice call graphs. It analyzes microservice topologies and simulates failure/scaling scenarios to predict system behavior.

**Tech Stack:**
- **Runtime:** Node.js (CommonJS)
- **Framework:** Express.js
- **Data Source:** Graph Engine HTTP API (service-graph-engine)
- **External Dependency:** Graph API consumed via HTTP

**Key Files:**
- `index.js` — Main entry point, Express server setup
- `src/graphEngineClient.js` — Graph Engine HTTP client
- `src/providers/GraphEngineHttpProvider.js` — Graph data provider
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
Server starts on port defined by `PORT` env var (default: 5000).

### Run Tests
```bash
npm test
```
Uses Node.js built-in test runner.

### Environment Variables Required
```bash
# Required
SERVICE_GRAPH_ENGINE_URL=http://service-graph-engine:3000
# or: GRAPH_ENGINE_BASE_URL=http://service-graph-engine:3000

# Optional
PORT=5000
GRAPH_API_TIMEOUT_MS=20000
```

---

## Boundaries (Critical)

### ✅ ALWAYS DO
- Use Graph Engine HTTP API for all graph data access
- Follow the plan-first workflow: inventory → plan → questions → wait for approval
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
- Add CI/CD workflows (`.github/workflows/*`)
- Add or modify tests without explicit approval
- Log secrets, passwords, or connection strings
- Invent Graph API endpoints or request/response shapes
- Implement without user typing `OK IMPLEMENT NOW`

---

## Architecture

```
┌─────────────────┐     ┌─────────────────┐
│   HTTP Client   │────▶│  Express API  │────▶│  Graph Engine  │
└─────────────────┘     └─────────────────┘     │  HTTP API      │
                                         └─────────────────┘
```

### Data Flow Priority
1. **Graph Engine API** — Single source of truth for topology and metrics

---

## File Structure

```
├── index.js                 # Express server entry point
├── package.json             # Dependencies and scripts
├── src/
│   ├── config.js            # Environment configuration
│   ├── failureSimulation.js # Failure scenario logic
│   ├── scalingSimulation.js # Scaling scenario logic
│   ├── graphEngineClient.js # Graph Engine HTTP client
│   ├── providers/           # Graph data provider layer
│   │   ├── GraphDataProvider.js
│   │   ├── GraphEngineHttpProvider.js
│   │   └── index.js
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
│   │   ├── 04-graph-engine-integration.prompt.md
│   │   ├── 05-add-or-change-endpoint.prompt.md
│   │   ├── 06-docs-update.prompt.md
│   │   ├── 07-pr-summary.prompt.md
│   │   └── 08-post-change-verification.prompt.md
│   ├── instructions/
│   │   ├── 00-operating-rules.instructions.md
│   │   ├── 01-ownership-boundaries.instructions.md
│   │   ├── 02-graph-api-first.instructions.md
│   │   ├── 03-graph-engine-single-source.instructions.md
│   │   ├── 04-errors-logging-secrets.instructions.md
│   │   ├── 05-k8s-minikube-scope.instructions.md
│   │   └── 06-external-service-resilience.instructions.md
│   └── skills/
│       ├── graph-api-client/SKILL.md
│       ├── graph-engine-integration/SKILL.md
│       ├── k8s-deployment/SKILL.md
│       └── simulation-runner/SKILL.md
├── k8s/
│   └── (removed - not needed)
├── test/
│   └── simulation.test.js   # Test file
└── docs/
    └── COPILOT-USAGE-GUIDE.md
```

---

## Code Style

- **Naming:** camelCase for variables/functions, PascalCase for classes
- **Async:** Use async/await, not callbacks
- **Error handling:** Always wrap Graph Engine API calls in try-catch
- **Logging:** Never log secrets

---

## Additional Context

For detailed Copilot-specific rules, see:

### Master Configuration
- `.github/copilot-instructions.md` — Single source of truth for Copilot behavior

### Agent Personas (select from dropdown in Chat)
- `.github/agents/planner.agent.md` — Analyze, gather evidence, produce plans
- `.github/agents/implementer.agent.md` — Execute approved plans (requires `OK IMPLEMENT NOW`)
- `.github/agents/reviewer.agent.md` — Validate changes against rules
- `.github/agents/evidence-answerer.agent.md` — Answer questions with codebase proof (file+line+1–5 line snippet). No implementation.

### Path-Specific Instructions (auto-applied)
- `.github/instructions/00-operating-rules.instructions.md` — Implementation lock, evidence requirements
- `.github/instructions/01-ownership-boundaries.instructions.md` — What this repo owns
- `.github/instructions/02-graph-api-first.instructions.md` — Graph Engine API is single source of truth
- `.github/instructions/04-errors-logging-secrets.instructions.md` — Security rules
- `.github/instructions/05-k8s-minikube-scope.instructions.md` — K8s context

### Agent Skills (auto-loaded based on context)
- `.github/skills/graph-api-client/` — Graph Engine API consumption patterns
- `.github/skills/simulation-runner/` — Simulation logic patterns
- `.github/skills/k8s-deployment/` — Kubernetes deployment patterns

### Reusable Prompts (invoke with `/` in chat)
- `.github/prompts/*.prompt.md` — 7 workflow templates

> **Note:** Custom agents appear in the **agent dropdown** in Chat, not via `@` mentions.
- `.github/prompts/` — Reusable task prompts
- `.github/skills/` — Agent skills for specialized workflows
