# Prompt: Add or Change HTTP Endpoint

Use this prompt when adding a new API endpoint or modifying an existing one.

---

## Prompt Template

```
I need to [add/modify] an endpoint:
- Method: [GET/POST/PUT/DELETE]
- Path: [/path/to/endpoint]
- Purpose: [what it does]
- Request: [body/params format]
- Response: [expected response format]

Please:
1. Check existing endpoint patterns in index.js
2. Follow the validation patterns from validator.js
3. Use consistent error handling (status codes, messages)
4. Preserve timeout patterns for any async operations
5. Document the endpoint in README.md

Do NOT implement until I say "OK IMPLEMENT NOW".
```

---

## Example Usage

### Adding a new endpoint

```
I need to add an endpoint:
- Method: POST
- Path: /simulate/cascade
- Purpose: Simulate cascading failure across multiple services
- Request: { serviceIds: ["default:svc1", "default:svc2"], maxDepth: 2 }
- Response: { affected: [...], totalImpact: {...} }

Please:
1. Check existing endpoint patterns in index.js
2. Follow the validation patterns from validator.js
3. Use consistent error handling (status codes, messages)
4. Preserve timeout patterns for any async operations
5. Document the endpoint in README.md

Do NOT implement until I say "OK IMPLEMENT NOW".
```

### Modifying an existing endpoint

```
I need to modify the POST /simulate/failure endpoint:
- Add optional parameter: includeMetrics (boolean)
- When true, include edge metrics in response
- Default: false (backward compatible)

Please:
1. Show current implementation
2. Propose changes with backward compatibility
3. Update validation as needed
4. Update README documentation

Do NOT implement until I say "OK IMPLEMENT NOW".
```

---

## Expected Response Format

```
## A) Evidence Inventory
- Existing endpoint pattern: [index.js]: `snippet`
- Validation pattern: [validator.js]: `snippet`
- Error handling pattern: [index.js]: `snippet`

## B) Proposed Plan

### New/Modified Files
- `index.js`: Add endpoint handler
- `src/cascadeSimulation.js`: Implement simulation logic
- `src/validator.js`: Add validation functions
- `README.md`: Document endpoint

### Endpoint Implementation Outline
```javascript
app.post('/simulate/cascade', async (req, res) => {
    try {
        // Validate
        // Execute with timeout
        // Return response
    } catch (error) {
        // Error handling per existing pattern
    }
});
```

### Error Status Mapping
- 400: Invalid request (missing params, invalid format)
- 404: Service not found
- 504: Timeout exceeded
- 500: Internal error

## C) Clarifying Questions
- Should serviceIds accept both formats (serviceId and name+namespace)?
- Maximum number of serviceIds allowed in one request?

## D) Waiting State
Reply with `OK IMPLEMENT NOW` when ready.
```

---

## Endpoint Checklist

- [ ] Follows existing route patterns
- [ ] Input validation using validator.js patterns
- [ ] Timeout protection (Promise.race)
- [ ] Consistent error status codes
- [ ] Credential redaction in error logs
- [ ] README documentation updated
