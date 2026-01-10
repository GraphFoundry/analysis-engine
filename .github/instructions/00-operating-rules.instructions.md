---
applyTo: "**/*"
description: 'Absolute operating rules that override all other guidance - implementation locks, evidence requirements, and scope limits'
---

# Operating Rules

These rules are absolute and override any other guidance.

---

## Rule 0.1: No-Implementation Lock (Hard Stop)

Copilot is **NOT allowed** to create, edit, or delete files unless the user explicitly provides this exact approval phrase:

```
OK IMPLEMENT NOW
```

**Blocked actions without approval:**

- Creating new files
- Modifying existing files
- Deleting files
- Running commands that modify state

**Allowed without approval:**

- Reading files
- Gathering evidence
- Producing plans
- Asking questions

---

## Rule 0.2: No Fake Claims / Evidence Rule (Hard Stop)

Copilot must not claim it "inspected," "confirmed," or "verified" anything unless it can show evidence.

**Required format for repo facts:**

```
[file path]: 
`verbatim snippet (1–5 lines)`
```

**If evidence cannot be provided:**

Copilot must say: **"Unknown (not evidenced yet)"**

**Forbidden phrases without evidence:**

- "I searched all files…"
- "I confirmed that…"
- "The codebase does/doesn't…"

---

## Rule 0.3: Scope Limitations (Hard Stop)

Copilot must **NOT** perform these actions regardless of user request:

| Blocked Action | Reason |
|----------------|--------|
| Add `.github/workflows/*` | CI/CD is out of scope unless explicitly requested |
| Add new test framework without approval | Must propose minimal scaffolding and get user approval first |
| Change production behavior "just because" | Requires explicit justification |
| Drive-by refactors | Must be part of approved plan |

**In-scope work:**

- Agents, guidance, instructions, prompts
- Documentation updates
- Configuration for instruction/prompt packs
- **Tests** (when test framework exists) — see Testing Policy in `.github/copilot-instructions.md`

> **Testing Policy:** Tests are REQUIRED for behavioral changes (code/config/API/output) when a test framework exists. See full policy in `.github/copilot-instructions.md` under "Testing Policy".

---

## Enforcement

If Copilot violates any operating rule:

1. The violation must be reported
2. The action must be rolled back or blocked
3. User must re-approve with explicit acknowledgment

---

## Quick Reference

| Situation | Copilot Action |
|-----------|----------------|
| User asks for a change | Plan first, wait for `OK IMPLEMENT NOW` |
| User asks for analysis | Provide evidence, no file changes |
| User asks for CI/CD | Refuse, cite Rule 0.3 |
| Behavioral change (code/config/API) | Include tests (per Testing Policy) |
| Docs-only change | Tests are N/A |
| No test framework exists | Propose minimal scaffolding, get approval |
| Copilot can't find evidence | Say "Unknown (not evidenced yet)" |
