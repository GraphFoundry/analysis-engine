# Copilot Usage Guide — predictive-analysis-engine

This guide explains how to use the custom agents in this repository with VS Code Copilot Chat, including normal chat sessions and Background Agents (Copilot CLI).

---

## Quick Reference

| Agent | Purpose | Tools | How to Select |
|-------|---------|-------|---------------|
| **Planner** | Analyze, gather evidence, produce plans | `read`, `search` | Agent dropdown → Planner |
| **Implementer** | Execute approved plans | `read`, `edit`, `search`, + MCP tools (Firecrawl, Brave Search, Tavily, Context7, Git, etc.) | Agent dropdown → Implementer |
| **Reviewer** | Validate changes against rules | `read`, `search`, + MCP tools (Git, Firecrawl, Tavily, etc.) | Agent dropdown → Reviewer |

> **Note:** Custom agents are selected from the **Agents dropdown** at the bottom of the Chat view, NOT via `@` mentions. The `@` syntax is reserved for built-in chat participants like `@workspace` and `@terminal`.

**Approval phrase (required before any edits):**
```
OK IMPLEMENT NOW
```

---

## 1. Using Agents in Normal Chat Sessions

### Starting with the Planner

1. Open VS Code Copilot Chat (`Ctrl+Alt+I` or `Cmd+Alt+I`)
2. Click the **agent picker dropdown** at the bottom of the chat input (shows "Agent", "Plan", "Ask", or "Edit" by default)
3. Select **Planner** from the list of custom agents
4. Describe what you want to accomplish:

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
     "github.copilot.chat.cli.customAgents.enabled": true,
     "chat.agent.enabled": true,
     "chat.useAgentsMdFile": true,
     "chat.useAgentSkills": true
   }
   ```

3. (Optional) Enable organization-level agents:
   ```json
   {
     "github.copilot.chat.customAgents.showOrganizationAndEnterpriseAgents": true
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
- Verify API contracts are correct

### Never Put Secrets in Prompts

❌ **Don't:**
```
Connect to database using password "mySecretPassword123"
```

✅ **Do:**
```
Use environment variables for authentication
```

---

## 4. Common Workflows

### Adding a New Endpoint

1. Select **Planner** from agent dropdown — Describe the endpoint
2. Review plan, ask questions
3. `OK IMPLEMENT NOW`
4. Click **Start Implementation**
5. Click **Review My Changes**
6. Manually test: `npm start` + call endpoint

### Consuming Graph Engine API

1. Select **Planner** from agent dropdown — Describe data needed
2. Provide Graph Engine API contract if known
3. Plan should use Graph Engine API exclusively
4. `OK IMPLEMENT NOW`
5. Verify `SERVICE_GRAPH_ENGINE_URL` usage in implementation

---

## 5. Prompt Files

Reusable prompts are in `.github/prompts/`:

| Prompt | Purpose |
|--------|---------|
| `01-plan-change.prompt.md` | Template for planning changes |
| `02-implement-approved-plan.prompt.md` | Template for triggering implementation |
| `03-graph-api-consumer.prompt.md` | Consuming Graph Engine API |
---

## 6. Troubleshooting

### Agent Not Appearing in Dropdown

1. Ensure files are in `.github/agents/` with `.agent.md` extension
2. Verify VS Code settings are enabled:
   ```json
   {
     "chat.agent.enabled": true,
     "chat.useAgentsMdFile": true
   }
   ```
3. Reload VS Code window (`Ctrl+Shift+P` → "Developer: Reload Window")
4. Check for YAML frontmatter syntax errors in agent files

### Why Don't Agents Show with @ Autocomplete?

Custom agents (`.github/agents/*.agent.md`) are designed to appear in the **agent dropdown**, NOT via `@` mentions.

- **Dropdown agents** = Custom agents defined in `.github/agents/`
- **@ participants** = Built-in VS Code participants (`@workspace`, `@terminal`, `@vscode`) or extension-contributed participants

This is expected behavior, not a bug.

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

## 7. Agent Skills

Agent Skills are specialized knowledge modules that Copilot automatically loads when relevant to your prompt. They're stored in `.github/skills/`.

| Skill | Purpose | When Loaded |
|-------|---------|-------------|
| **graph-api-client** | Guide for consuming the Graph Engine API service | When asked to fetch graph data or integrate with Graph Engine |
| **simulation-runner** | Guide for running and extending simulation logic | When asked about failure/scaling simulations |
| **k8s-deployment** | Guide for Kubernetes deployment patterns | When asked about K8s manifests or deployment |

Skills are loaded automatically based on context. You don't need to reference them explicitly.

---

## 8. Instruction Files

Path-specific instructions in `.github/instructions/` are automatically applied based on which files you're working with:

| File | Applies To | Purpose |
|------|------------|---------|
| `00-operating-rules.instructions.md` | `**/*` | Absolute rules: implementation lock, evidence requirements |
| `01-ownership-boundaries.instructions.md` | `**/*` | What this repo owns vs external teams |
| `02-graph-api-first.instructions.md` | `**/graphEngineClient.js`, `**/providers/**/*.js` | Graph Engine API is single source of truth |
| `04-errors-logging-secrets.instructions.md` | `**/*.js` | Never log credentials |
## 9. Required VS Code Settings

Ensure these settings are enabled in `.vscode/settings.json`:

```json
{
  // Enable custom agents from .github/agents/
  "chat.agent.enabled": true,
  "chat.useAgentsMdFile": true,
  
  // Enable agent skills from .github/skills/
  "chat.useAgentSkills": true,
  
  // Enable instruction files from .github/instructions/
  "github.copilot.chat.codeGeneration.useInstructionFiles": true,
  "chat.instructionsFilesLocations": {
    ".github/instructions": true
  },
  
  // Enable prompt files from .github/prompts/
  "chat.promptFilesLocations": {
    ".github/prompts": true
  },
  
  // Enable custom agents in background/CLI sessions
  "github.copilot.chat.cli.customAgents.enabled": true
}
```

---

## 10. Related Files

- [.github/copilot-instructions.md](../.github/copilot-instructions.md) — Master instruction file
- [.github/instructions/](../.github/instructions/) — Path-specific coding standards (6 files)
- [.github/agents/](../.github/agents/) — Agent definitions (Planner, Implementer, Reviewer)
- [.github/prompts/](../.github/prompts/) — Reusable prompt templates (7 files)
- [.github/skills/](../.github/skills/) — Agent skills (4 folders)
