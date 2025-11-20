---
description: Trigger implementation of an approved plan after user says OK IMPLEMENT NOW.
agent: Implementer
---

# Prompt: Implement Approved Plan

Use this prompt after a plan has been approved to trigger implementation.

---

## Prompt Template

```
OK IMPLEMENT NOW

Please implement the approved plan:
[reference the plan or paste key points]

After implementation, provide:
1. List of files created/modified
2. Key rules that were enforced
3. Manual verification steps
```

---

## Example Usage

### After plan approval

```
OK IMPLEMENT NOW

Please implement the approved plan for adding POST /simulate/latency.

After implementation, provide:
1. List of files created/modified
2. Key rules that were enforced
3. Manual verification steps
```

### With specific instructions

```
OK IMPLEMENT NOW

Implement the plan with these adjustments:
- Use "latencyAnalysis" as the function name instead of "analyzeLatency"
- Add detailed JSDoc comments

After implementation, provide:
1. List of files created/modified
2. Key rules that were enforced
3. Manual verification steps
```

---

## Expected Response Format

Copilot should respond with:

```
## Implementation Summary

### Files Created
- `path/to/new/file.js`

### Files Modified
- `path/to/existing.js` (lines X-Y)

### Key Rules Enforced
- Read-only Neo4j access preserved
- Credential redaction used
- Timeout pattern maintained

### Manual Verification Steps
1. Run `npm start`
2. Test endpoint: `curl -X POST localhost:7000/simulate/latency ...`
3. Verify no Neo4j write operations
```

---

## Post-Implementation

After implementation, you may want to:

1. **Review changes:** Ask Copilot to review
2. **Test manually:** Follow verification steps
3. **Iterate:** Request adjustments if needed
