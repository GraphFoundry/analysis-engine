---
applyTo: "**/*.js"
description: 'Security rules for error handling, logging, and secrets - never log credentials, use redactCredentials()'
---

# Errors, Logging & Secrets Policy

This document governs how Copilot must handle errors, logging, and secrets.

---

## Secrets Management

### Hard Rules

| Rule | Enforcement |
|------|-------------|
| Never hardcode credentials | ❌ No passwords, tokens, or connection strings in code |
| Never log secrets | ❌ No secrets in console.log, console.error, or any log output |
| Only env vars or K8s secrets | ✅ These are the only acceptable secret sources |

### Acceptable Secret Sources

```javascript
// ✅ Environment variables
const apiKey = process.env.GRAPH_ENGINE_API_KEY;

// ✅ K8s secrets via env injection
// (defined in deployment.yaml, not in code)
env:
  - name: GRAPH_ENGINE_API_KEY
    valueFrom:
      secretKeyRef:
        name: graph-engine-credentials
        key: API_KEY
```

### Forbidden Patterns

```javascript
// ❌ NEVER do this
const apiKey = 'sk-1234567890abcdef';
const url = 'https://api.example.com?key=secret-key-here';
console.log('Connecting with API key:', apiKey);
```

---

## Credential Redaction

The repo has a `redactCredentials()` function. Copilot must use this pattern.

### Credential Redaction Pattern

When logging errors that may contain sensitive data:

```javascript
// Generic pattern for redacting credentials
function redactSensitiveData(message) {
    if (!message) return message;
    return message
        .replace(/password=([^&\s]+)/gi, 'password=[REDACTED]')
        .replace(/apikey=([^&\s]+)/gi, 'apikey=[REDACTED]')
        .replace(/token=([^&\s]+)/gi, 'token=[REDACTED]');
}
```

### When to Apply

| Situation | Action |
|-----------|--------|
| Logging error messages | Apply redaction |
| Returning errors in HTTP responses | Apply redaction |
| Logging connection strings | Apply redaction |
| Logging configuration | Never log password fields |

---

## Error Handling

### HTTP Status Mapping

The repo uses message-based status mapping:

```javascript
// From index.js — FOLLOW THIS PATTERN
if (error.message.includes('not found')) {
    res.status(404).json({ error: error.message });
} else if (error.message.includes('timeout')) {
    res.status(504).json({ error: error.message });
} else if (error.message.includes('must') || error.message.includes('invalid')) {
    res.status(400).json({ error: error.message });
} else {
    console.error('Simulation error:', error);
    res.status(500).json({ error: 'Internal server error' });
}
```

### Error Response Format

```javascript
// Standard error response
{
    "error": "Human-readable error message"
}

// Never include in error responses:
// - Stack traces (in production)
// - Credentials
// - Internal system details
```

---

## Logging Rules

### What to Log

| Category | Log Level | Example |
|----------|-----------|---------|
| Server startup | info | Port, config summary |
| Health checks | debug | Connection status |
| Simulation requests | info | Service ID, parameters (no secrets) |
| Errors | error | Redacted error messages |

### What NOT to Log

| Category | Reason |
|----------|--------|
| Passwords / API keys | Security violation |
| Full connection strings | May contain credentials |
| Raw error messages from external services | May contain sensitive data |
| Request bodies with secrets | Security violation |

### Log Format

```javascript
// ✅ Good: No secrets, structured info
console.log(`Simulation request: serviceId=${serviceId}, maxDepth=${maxDepth}`);

// ❌ Bad: Contains potential secrets
console.log('Full config object:', config);
```

---

## Configuration Logging at Startup

Safe to log:

```javascript
console.log(`Port: ${config.server.port}`);
console.log(`Max traversal depth: ${config.simulation.maxTraversalDepth}`);
console.log(`Default latency metric: ${config.simulation.defaultLatencyMetric}`);
```

Never log:

```javascript
// ❌ NEVER
console.log(`API key: ${config.apiKey}`);
console.log(`Full config with secrets:`, config);
```

---

## Quick Reference

| Situation | Copilot Action |
|-----------|----------------|
| Adding error handling | Use message-based status mapping |
| Logging errors | Apply redactCredentials() |
| Adding new config | Use env vars, never hardcode |
| Modifying HTTP responses | Never include credentials |
| Startup logging | Log safe config only |
