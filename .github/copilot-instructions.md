# COPILOT MASTER INSTRUCTION — predictive-analysis-engine

**Purpose:** This is the single source of truth for how GitHub Copilot (and any Copilot "agent mode") must behave in this repository.

If any other prompt conflicts with this file, **this file wins**.

---

## 0) Absolute Rules (Implementation MUST be blocked by default)

### 0.1 No-Implementation Lock (Hard Stop)

Copilot is **NOT allowed** to create/edit/delete files unless the user explicitly types this exact approval phrase:

✅ **APPROVAL PHRASE:** `OK IMPLEMENT NOW`

If Copilot does not see that exact phrase in the user message, it must stop after planning + questions.

### 0.2 No Fake Claims / Evidence Rule (Hard Stop)

Copilot must not claim it "inspected" or "confirmed" anything unless it can show evidence.

**When stating repo facts, Copilot MUST include:**

- the **file path**
- and a **verbatim snippet (1–5 lines)** from that file

If Copilot cannot quote it, it must say: **"Unknown (not evidenced yet)"**.

### 0.3 Scope Limitations & Testing Policy (Hard Stop)

Copilot must **NOT**:

- add CI/CD workflows (`.github/workflows/*`) unless explicitly requested
- change production behavior "just because"
- do drive-by refactors unrelated to the task
- add a new test framework without explicit user approval (propose minimal scaffolding first)

#### Testing Policy

- If the repo already has a test framework/setup, then any change that affects runtime behavior (code/config/API/output) **MUST** include tests.
- **Bug fixes:** add/update a regression test that would fail without the fix.
- **Features/refactors:** add/update targeted tests covering the new/changed behavior.
- **Docs-only or formatting-only changes:** tests are N/A.
- Adding a new test framework is NOT allowed without explicit user approval (propose minimal scaffolding first).
- CI/CD workflow changes (`.github/workflows/*`) remain out of scope unless explicitly requested.

### 0.4 OpenAPI Documentation Policy (Hard Stop)

This repository maintains an OpenAPI 3.0 specification (`openapi.yaml`) that documents all HTTP API endpoints exposed by this service.

**Hard rule:** Any change that adds, modifies, or removes API behavior **MUST** include corresponding updates to `openapi.yaml` in the same change.

#### What counts as an API change:

- Adding a new endpoint (path + method)
- Changing request/response schema (body, query params, headers, status codes)
- Modifying endpoint behavior (even if signature is unchanged)
- Deprecating or removing an endpoint
- Changing error response formats

#### What must be updated in openapi.yaml:

- `paths:` section (add/modify/remove endpoint)
- `operationId:` (unique identifier for the operation)
- Request `parameters:` and `requestBody:` schemas
- Response `responses:` schemas for all status codes
- `components/schemas:` definitions (if new types introduced)
- `info.version:` (bump patch version for minor changes, minor version for new endpoints)

#### What does NOT require OpenAPI updates:

- Internal refactoring (no API signature change)
- Performance improvements (no API signature change)
- Documentation-only changes (e.g., updating README.md)
- Configuration changes that don't affect API behavior

#### Minimum checklist for API changes:

- [ ] `openapi.yaml` updated with new/changed endpoint details
- [ ] Request/response schemas match actual implementation
- [ ] All status codes documented (200, 400, 500, etc.)
- [ ] Version bumped in `info.version`
- [ ] Swagger UI validates (start server with `ENABLE_SWAGGER=true`, visit `/swagger`)

**Blocked without approval:** If Copilot is asked to add/modify an endpoint but not update OpenAPI spec, it must stop and cite this rule.

---

## 1) Ownership & Integration Boundaries (Non-negotiable)

### 1.1 Leader-owned / Team-owned (Treat as external)

Copilot must assume the following are **NOT owned by this repo** (do not change assumptions without explicit user instruction):

- Neo4j schema design / schema evolution
- metrics source/collection architecture (Prometheus/Grafana/Kiali stack)
- "Graph API" service implementation and contract ownership (leader/team owns it)

### 1.2 This repo-owned

This repo owns:

- predictive analysis logic
- its own HTTP API (endpoints exposed by this service)
- client-side consumption of leader's Graph API
- optional **read-only** Neo4j access as a fallback ONLY

---

## 2) Graph API First Policy (Must follow)

### 2.1 Default decision

When Copilot needs graph/topology data:

1. **Use leader's Graph API** (preferred)
2. Use Neo4j **read-only fallback** only if:
   - Graph API is missing the required capability, OR
   - Graph API is unavailable, OR
   - the user explicitly requests Neo4j usage

### 2.2 Contract discipline

If consuming Graph API:

- Copilot must not invent endpoints.
- Copilot must not invent request/response shapes.
- If the contract isn't documented in repo, Copilot must ask for it OR point out the missing contract.
- Require env var `GRAPH_API_BASE_URL` when Graph API mode is enabled.

---

## 3) Neo4j Fallback Policy (Read-only + minimal coupling)

### 3.1 Runtime queries

All runtime Neo4j queries in this repo must be **read-only**. The codebase enforces this via `defaultAccessMode: neo4j.session.READ`.

### 3.2 Schema/write queries

If any schema or write queries exist in the codebase (legacy or validation), they are **not to be touched or expanded** without explicit leader approval.

**Hard rule:** Copilot must never introduce or modify Neo4j write/schema logic unless the user explicitly approves.

### 3.3 Fallback constraints

- Do not assume schema details unless evidenced by a snippet in this repo.
- Prefer "data access adapter" patterns so simulation logic doesn't couple to raw Cypher.
- Existing safeguards (two-layer timeout, credential redaction) must be preserved.

---

## 4) Security & Logging Rules (Hard rules)

- Never print secrets (passwords, tokens, connection strings) to logs.
- The repo has a `redactCredentials()` function in `src/neo4j.js` — follow this pattern.
- Do not hardcode credentials or endpoints.
- Treat env vars + K8s secrets as the only acceptable secret sources unless user says otherwise.

---

## 5) Working Style (How Copilot must behave)

### 5.1 Plan-first workflow (Always)

Every task must follow this sequence:

1. **Inventory (read-only)**: identify relevant files + evidence snippets
2. **Plan**: steps, files to change, risk points, stop conditions
3. **Clarifying questions**: ask only what's needed to be ≥95% confident
4. **Wait** (no edits) until user says `OK IMPLEMENT NOW`
5. **Implement** (only after approval) in small, reversible changes
6. **Summarize**: what changed + manual verification steps + docs touched

### 5.2 Minimal questions, maximum signal

Keep questions minimal and practical. Ask questions only when:

- contract details are missing
- boundaries are unclear
- implementation choices would materially change behavior

### 5.3 Avoid "progress chatter"

Copilot must not output filler like "Now I will inspect…". Only output:

- findings with evidence
- plan
- questions
- next steps

---

## 6) Output Format Requirements (Always follow)

When responding, Copilot must use this exact structure:

### A) Evidence Inventory

- Bullet list of discovered facts with `path:` + snippet blocks (1–5 lines)

### B) Proposed Plan (No code changes yet)

- Steps
- Files to create/modify
- Risks
- Stop conditions

### C) Clarifying Questions (Only what's needed)

- Group by: Contract / Boundaries / Tone

### D) Waiting State

- End with: "Reply with `OK IMPLEMENT NOW` when you want me to create/edit files."

---

## 7) What Copilot is currently expected to build in this repo

Unless user overrides, the default deliverable is a `.github` pack containing:

- `.github/copilot-instructions.md` (this file)
- `.github/agents/`: `planner.md`, `implementer.md`, `reviewer.md`
- `.github/instructions/`: operating rules, ownership, Graph API policy, Neo4j fallback, errors/logging, K8s scope
- `.github/prompts/`: reusable workflow prompts
- `.github/skills/`: Agent Skills for specialized workflows (neo4j-readonly, graph-api-client, simulation-runner, k8s-deployment)

**Also see:**
- `AGENTS.md` (root): Universal agent instructions compatible with any AI agent

**Blocked until `OK IMPLEMENT NOW`.**

---

## 8) Definition of "Done"

A task is done only when:

- The plan has been produced
- Missing context has been asked
- The user approves implementation with `OK IMPLEMENT NOW`
- Files are created/updated exactly as proposed
- **Tests added/updated** when applicable (per Testing Policy in §0.3)
- **OpenAPI spec (`openapi.yaml`) updated** for any API behavior change (add/modify/remove endpoint) per §0.4
- **Relevant docs updated** when behavior/config/API changes
- **Governance files updated** when the change impacts workflows/standards
- **Verification:** `npm test` run when possible (otherwise provide commands + pass criteria)
- A final summary lists:
  - files changed
  - tests added/updated
  - key rules enforced
  - manual checks to perform
