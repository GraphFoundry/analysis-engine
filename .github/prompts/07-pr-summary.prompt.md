# Prompt: Generate PR Summary

Use this prompt to generate a pull request summary after completing changes.

---

## Prompt Template

```
Please generate a PR summary for the changes made.

Include:
1. What was changed (high-level)
2. Files created/modified
3. Key decisions and trade-offs
4. Testing/verification steps
5. Checklist of rules enforced

Format for GitHub PR description.
```

---

## Example Usage

### After implementing changes

```
Please generate a PR summary for the changes made in this session.

Include:
1. What was changed (high-level)
2. Files created/modified
3. Key decisions and trade-offs
4. Testing/verification steps
5. Checklist of rules enforced

Format for GitHub PR description.
```

---

## Expected Response Format

```markdown
## Summary

Added POST /simulate/cascade endpoint for cascading failure simulation.

## Changes

### New Files
- `src/cascadeSimulation.js` - Cascade simulation logic

### Modified Files
- `index.js` - Added endpoint handler
- `src/validator.js` - Added cascade validation
- `README.md` - Documented new endpoint

## Key Decisions

- Used existing graph traversal pattern from failureSimulation.js
- Limited serviceIds array to max 10 to prevent timeout
- Graph Engine HTTP API single source

## Testing

1. Start server: `npm start`
2. Test endpoint:
   ```bash
   curl -X POST http://localhost:7000/simulate/cascade \
     -H "Content-Type: application/json" \
     -d '{"serviceIds": ["default:frontend"], "maxDepth": 2}'
   ```
3. Verify response format matches documentation

## Rules Enforced

- [x] Graph Engine single source policy
- [x] Timeout pattern preserved
- [x] Credential redaction used
- [x] No CI/CD changes
- [x] Tests added/updated (per Testing Policy)
- [x] Documentation updated
```

---

## PR Title Patterns

| Change Type | Title Pattern |
|-------------|---------------|
| New endpoint | `feat(api): add POST /simulate/cascade` |
| Bug fix | `fix(simulation): handle empty serviceIds array` |
| Documentation | `docs: update API reference for scaling endpoint` |
| Configuration | `chore(config): add GRAPH_API_BASE_URL support` |

---

## Checklist Template

Include this checklist in the PR:

```markdown
## Checklist

- [ ] Graph Engine single source policy enforced
- [ ] No credentials in logs
- [ ] Timeout patterns maintained
- [ ] Error handling follows existing patterns
- [ ] Documentation updated
- [ ] No CI/CD changes
- [ ] Tests added/updated (per Testing Policy in `.github/copilot-instructions.md`)
```
