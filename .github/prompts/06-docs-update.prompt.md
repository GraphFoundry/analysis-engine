# Prompt: Update Documentation

Use this prompt when you need to update README or other documentation.

---

## Prompt Template

```
I need to update documentation for:
- [What changed or needs documenting]

Please:
1. Find the relevant documentation files
2. Propose additions/changes
3. Keep the existing style and formatting
4. Do not remove existing content unless outdated

Do NOT implement until I say "OK IMPLEMENT NOW".
```

---

## Example Usage

### Documenting a new endpoint

```
I need to update documentation for:
- New endpoint POST /simulate/cascade
- Request format: { serviceIds: [...], maxDepth: 2 }
- Response format: { affected: [...], totalImpact: {...} }

Please:
1. Find the relevant documentation files
2. Propose additions/changes
3. Keep the existing style and formatting
4. Do not remove existing content unless outdated

Do NOT implement until I say "OK IMPLEMENT NOW".
```

### Updating configuration docs

```
I need to update documentation for:
- New environment variable GRAPH_API_BASE_URL
- Purpose: Base URL for Graph API when enabled
- Default: none (disabled when not set)

Please:
1. Find where config is documented (README.md, DEPLOYMENT.md)
2. Add to the configuration table
3. Keep consistent formatting

Do NOT implement until I say "OK IMPLEMENT NOW".
```

---

## Expected Response Format

```
## A) Evidence Inventory
- Main docs: [README.md]: current API section
- Deployment docs: [DEPLOYMENT.md]: current config section

## B) Proposed Changes

### README.md
Add to API Reference section:
```markdown
### Cascade Failure Simulation

**Endpoint:** `POST /simulate/cascade`

**Request:**
\`\`\`json
{
  "serviceIds": ["default:svc1", "default:svc2"],
  "maxDepth": 2
}
\`\`\`
...
```

### Configuration Table Addition
| Variable | Default | Description |
|----------|---------|-------------|
| `GRAPH_API_BASE_URL` | *(none)* | Base URL for Graph API |

## C) Waiting State
Reply with `OK IMPLEMENT NOW` when ready.
```

---

## Documentation Files in This Repo

| File | Purpose |
|------|---------|
| `README.md` | Main documentation (API, config, architecture) |
| `DEPLOYMENT.md` | Deployment instructions |
| `.github/instructions/*.md` | Copilot instructions |
| `.github/prompts/*.md` | Reusable prompts |

---

## Style Guidelines

- Use existing heading hierarchy
- Match code block formatting (json, bash, javascript)
- Keep tables aligned
- Use consistent terminology (serviceId, not service_id)
