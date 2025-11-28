---
name: simulation-runner
description: Guide for running and understanding predictive analysis simulations. Use this when asked to simulate failures, scaling scenarios, or understand simulation logic.
license: MIT
---

# Simulation Runner Skill

This skill helps you work with the predictive analysis engine's core functionality — simulating failure and scaling scenarios for microservice architectures.

## When to Use This Skill

Use this skill when you need to:
- Understand how simulations work
- Add new simulation scenarios
- Debug simulation logic
- Extend failure or scaling simulation capabilities

## Simulation Types

### 1. Failure Simulation
Simulates what happens when a service fails completely or partially.

**Input:**
```json
{
  "serviceName": "payment-service",
  "failureType": "complete",  // or "partial"
  "failureRate": 1.0          // 0.0 to 1.0
}
```

**Output:**
```json
{
  "affectedServices": ["order-service", "checkout-service"],
  "cascadeDepth": 2,
  "estimatedImpact": {
    "errorRateIncrease": 0.45,
    "latencyIncrease": 250
  }
}
```

### 2. Scaling Simulation
Simulates the effect of scaling a service up or down.

**Input:**
```json
{
  "serviceName": "api-gateway",
  "currentReplicas": 3,
  "targetReplicas": 6,
  "expectedLoad": 1.5  // multiplier
}
```

**Output:**
```json
{
  "scalingRecommendation": "proceed",
  "estimatedCapacity": {
    "requestsPerSecond": 15000,
    "headroom": 0.25
  },
  "downstreamImpact": []
}
```

## Core Files

| File | Purpose |
|------|---------|
| `src/failureSimulation.js` | Failure scenario logic |
| `src/scalingSimulation.js` | Scaling scenario logic |
| `src/graph.js` | Fetches topology data |
| `src/neo4j.js` | Neo4j fallback (read-only) |

## Simulation Flow

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│   Request    │────▶│  Validate    │────▶│ Fetch Graph  │
│   /simulate  │     │  Input       │     │  Topology    │
└──────────────┘     └──────────────┘     └──────────────┘
                                                 │
                     ┌──────────────┐            │
                     │   Return     │◀───────────┤
                     │   Results    │     ┌──────▼───────┐
                     └──────────────┘     │   Run        │
                                          │  Simulation  │
                                          └──────────────┘
```

## Adding a New Simulation Type

### Step 1: Create Simulation Module
```javascript
// src/newSimulation.js
async function simulateNewScenario(params, topology) {
  // 1. Validate params
  validateParams(params);
  
  // 2. Extract relevant graph data
  const affectedNodes = findAffectedNodes(topology, params);
  
  // 3. Run simulation logic
  const results = calculateImpact(affectedNodes, params);
  
  // 4. Return structured results
  return {
    scenario: 'new-scenario',
    input: params,
    results,
    timestamp: new Date().toISOString()
  };
}
```

### Step 2: Register Endpoint
```javascript
// index.js
app.post('/api/simulate/new-scenario', async (req, res) => {
  try {
    const topology = await getTopology();
    const result = await simulateNewScenario(req.body, topology);
    res.json(result);
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});
```

## Testing Simulations Locally

```bash
# Start the server
npm start

# Run a failure simulation
curl -X POST http://localhost:3000/api/simulate/failure \
  -H "Content-Type: application/json" \
  -d '{"serviceName": "payment-service", "failureType": "complete"}'

# Run a scaling simulation
curl -X POST http://localhost:3000/api/simulate/scaling \
  -H "Content-Type: application/json" \
  -d '{"serviceName": "api-gateway", "currentReplicas": 3, "targetReplicas": 6}'
```

## Validation Rules

All simulation inputs must be validated:

```javascript
function validateFailureParams(params) {
  if (!params.serviceName) {
    throw new Error('serviceName is required');
  }
  if (params.failureRate && (params.failureRate < 0 || params.failureRate > 1)) {
    throw new Error('failureRate must be between 0 and 1');
  }
}
```

## Graph Traversal Patterns

### Find Downstream Services (Cascade Analysis)
```javascript
function findDownstreamServices(topology, serviceName, depth = 3) {
  const visited = new Set();
  const queue = [{ name: serviceName, level: 0 }];
  const downstream = [];
  
  while (queue.length > 0) {
    const { name, level } = queue.shift();
    if (visited.has(name) || level > depth) continue;
    
    visited.add(name);
    const service = topology.services.find(s => s.name === name);
    
    if (service?.dependencies) {
      for (const dep of service.dependencies) {
        downstream.push({ name: dep, level: level + 1 });
        queue.push({ name: dep, level: level + 1 });
      }
    }
  }
  
  return downstream;
}
```

### Find Upstream Services (Impact Analysis)
```javascript
function findUpstreamServices(topology, serviceName) {
  return topology.services.filter(s => 
    s.dependencies?.includes(serviceName)
  );
}
```

## Performance Considerations

- **Cache topology data** — Don't re-fetch for every simulation
- **Limit cascade depth** — Default to 3-5 levels max
- **Use timeouts** — All external calls should timeout
- **Batch calculations** — Process multiple services in parallel when possible

## References

- [src/failureSimulation.js](../../../src/failureSimulation.js) — Failure simulation
- [src/scalingSimulation.js](../../../src/scalingSimulation.js) — Scaling simulation
- [test/simulation.test.js](../../../test/simulation.test.js) — Test examples
