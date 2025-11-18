# Prompt: Plan a Change

Use this prompt when you want Copilot to analyze and plan a change before implementing.

---

## Prompt Template

```
I need to [describe the change you want].

Before implementing, please:
1. Gather evidence from the codebase
2. Identify affected files
3. Propose a step-by-step plan
4. List any risks or stop conditions
5. Ask clarifying questions if needed

Do NOT implement until I say "OK IMPLEMENT NOW".
```

---

## Example Usage

### Adding a new endpoint

```
I need to add a new endpoint POST /simulate/latency that analyzes latency impact.

Before implementing, please:
1. Gather evidence from the codebase
2. Identify affected files
3. Propose a step-by-step plan
4. List any risks or stop conditions
5. Ask clarifying questions if needed

Do NOT implement until I say "OK IMPLEMENT NOW".
```

### Modifying existing behavior

```
I need to change the default MAX_TRAVERSAL_DEPTH from 2 to 3.

Before implementing, please:
1. Gather evidence from the codebase
2. Identify affected files
3. Propose a step-by-step plan
4. List any risks or stop conditions
5. Ask clarifying questions if needed

Do NOT implement until I say "OK IMPLEMENT NOW".
```

---

## Expected Response Format

Copilot should respond with:

```
## A) Evidence Inventory
- [file]: `snippet`
- [file]: `snippet`

## B) Proposed Plan
1. Step one
2. Step two
- Files: list of files
- Risks: identified risks
- Stop conditions: when to halt

## C) Clarifying Questions
- Question 1?
- Question 2?

## D) Waiting State
Reply with `OK IMPLEMENT NOW` when ready.
```

---

## Approval

When satisfied with the plan, reply:

```
OK IMPLEMENT NOW
```
