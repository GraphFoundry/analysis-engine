# Copilot Usage Guide — what-if-simulation-engine

This guide explains how to use the custom agents in this repository with VS Code Copilot Chat, including normal chat sessions and Background Agents (Copilot CLI).

---

## Quick Reference

| Agent | Purpose | Tools | Invocation |
|-------|---------|-------|------------|
| **Planner** | Analyze, gather evidence, produce plans | `read`, `search` | `@planner` |
| **Implementer** | Execute approved plans | `read`, `search`, `edit` | `@implementer` |
| **Reviewer** | Validate changes against rules | `read`, `search` | `@reviewer` |

**Approval phrase (required before any edits):**
```
OK IMPLEMENT NOW
```

---

## 1. Using Agents in Normal Chat Sessions

### Starting with the Planner

1. Open VS Code Copilot Chat (`Ctrl+Alt+I` or `Cmd+Alt+I`)
2. From the agents dropdown at the bottom, select **Planner**
3. Describe what you want to accomplish:

```
I want to add a new endpoint POST /simulate/cascade that analyzes cascade failure scenarios.
```

The Planner will:
- Gather evidence from the codebase
- Produce a structured plan
- Ask clarifying questions
- Wait for your approval

### Approving Implementation

When you're satisfied with the plan, type exactly:

```
OK IMPLEMENT NOW
```

Then click the **Start Implementation** handoff button to switch to the Implementer agent.

### Reviewing Changes

After implementation, click **Review My Changes** to switch to the Reviewer agent.

The Reviewer will:
- Check plan compliance
- Verify Neo4j read-only constraints
- Check for security/logging issues
- Provide a structured report

### Workflow Diagram

```
┌──────────┐    OK IMPLEMENT NOW    ┌─────────────┐    Review    ┌──────────┐
│ Planner  │ ───────────────────▶  │ Implementer │ ──────────▶ │ Reviewer │
│          │                        │             │              │          │
│ • Read   │                        │ • Read      │              │ • Read   │
│ • Search │                        │ • Search    │              │ • Search │
│          │                        │ • Edit      │              │          │
└──────────┘                        └─────────────┘              └──────────┘
     ▲                                                                 │
     │                         Re-plan (if needed)                     │
     └─────────────────────────────────────────────────────────────────┘
```

---

## 2. Using Agents as Background Agents (Copilot CLI)

Background Agents run autonomously via the Copilot CLI while you continue other work. They're ideal for well-scoped tasks after planning is complete.

### Prerequisites

1. Install Copilot CLI:
   ```bash
   npm install -g @github/copilot
   ```

2. Enable custom agents for background sessions in VS Code settings:
   ```json
   {
     "github.copilot.chat.cli.customAgents.enabled": true
   }
   ```

### Starting a Background Agent Session

**Option A: From VS Code**
1. Open Chat view (`Ctrl+Alt+I`)
2. Select **New Chat** dropdown → **New Background Agent**
3. Select a custom agent (e.g., `Planner`, `Implementer`)
4. Enter your task description

**Option B: Hand off from local chat**
1. Complete planning with the Planner agent
2. Get approval (`OK IMPLEMENT NOW`)
3. Select **Continue In** → **Background Agent**

**Option C: Use `@cli` in chat**
```
@cli Implement the approved plan for adding POST /simulate/cascade
```

### Background Agent Limitations

⚠️ **Important:** Background agents have different capabilities than local agents:

| Feature | Local Agent | Background Agent |
|---------|-------------|------------------|
| VS Code runtime context | ✅ | ❌ |
| Failed test information | ✅ | ❌ |
| Text selections | ✅ | ❌ |
| MCP servers | ✅ | ❌ |
| Extension-provided tools | ✅ | ❌ |
| Terminal commands | ✅ | ✅ (may prompt) |
| File read/edit | ✅ | ✅ |

### Worktree Isolation (Recommended)

To prevent conflicts with your active work:

1. Start a background agent session
2. Select **Worktree** for isolation mode
3. The agent works in a separate Git worktree
4. Review and merge changes when complete

---

## 3. Safety Guidelines

### Always Review Diffs

Before accepting any changes:
- Use Source Control view to review all modified files
- Check for unintended scope creep
- Verify Neo4j queries are read-only

### Never Put Secrets in Prompts

❌ **Don't:**
```
Connect to Neo4j using password "mySecretPassword123"
```

✅ **Do:**
```
Use the NEO4J_PASSWORD environment variable for authentication
```

### Verify Read-Only Neo4j Access

After any change touching `src/neo4j.js` or graph queries, verify:
- All sessions use `defaultAccessMode: neo4j.session.READ`
- No write queries (CREATE, MERGE, DELETE, SET)
- `redactCredentials()` is preserved

---

## 4. Common Workflows

### Adding a New Endpoint

1. `@planner` — Describe the endpoint
2. Review plan, ask questions
3. `OK IMPLEMENT NOW`
4. Click **Start Implementation**
5. Click **Review My Changes**
6. Manually test: `npm start` + call endpoint

### Consuming Graph API

1. `@planner` — Describe data needed
2. Provide Graph API contract if known
3. Plan should prefer Graph API over Neo4j
4. `OK IMPLEMENT NOW`
5. Verify `GRAPH_API_BASE_URL` usage in implementation

### Neo4j Fallback Query

1. `@planner` — Explain why Graph API is insufficient
2. Plan must document fallback justification
3. `OK IMPLEMENT NOW`
4. Reviewer checks read-only constraint

---

## 5. Prompt Files

Reusable prompts are in `.github/prompts/`:

| Prompt | Purpose |
|--------|---------|
| `01-plan-change.prompt.md` | Template for planning changes |
| `02-implement-approved-plan.prompt.md` | Template for triggering implementation |
| `03-graph-api-consumer.prompt.md` | Consuming leader's Graph API |
| `04-neo4j-fallback.prompt.md` | Adding read-only Neo4j queries |
| `05-add-or-change-endpoint.prompt.md` | Endpoint modifications |
| `06-docs-update.prompt.md` | Documentation changes |
| `07-pr-summary.prompt.md` | Generate PR description |

---

## 6. Troubleshooting

### Agent Not Appearing in Dropdown

1. Ensure files are in `.github/agents/` with `.agent.md` extension
2. Reload VS Code window (`Ctrl+Shift+P` → "Developer: Reload Window")
3. Check for YAML frontmatter syntax errors

### Background Agent Can't Use Custom Agent

Verify setting is enabled:
```json
"github.copilot.chat.cli.customAgents.enabled": true
```

### Implementer Refuses to Edit

The Implementer requires the exact phrase `OK IMPLEMENT NOW` in the current conversation. Check:
- Phrase is spelled exactly (case-sensitive)
- Phrase was sent in the current session (not a previous one)

---

## 7. Related Files

- [.github/copilot-instructions.md](../.github/copilot-instructions.md) — Master instruction file
- [.github/instructions/](../.github/instructions/) — Detailed operating rules
- [.github/agents/](../.github/agents/) — Agent definitions
- [.github/prompts/](../.github/prompts/) — Reusable prompt templates
