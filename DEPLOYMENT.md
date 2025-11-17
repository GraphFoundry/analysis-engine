# Deployment Guide

## Local Demo (Current Phase)

### Prerequisites

- Node.js >= 18.x
- Neo4j AuraDB instance (populated by `service-graph-engine`)
- Neo4j credentials (URI + password)

### Setup

```bash
# 1. Install dependencies
npm install

# 2. Configure environment
cp .env.example .env

# 3. Edit .env with your Neo4j credentials
#    Required: NEO4J_URI, NEO4J_PASSWORD
```

### Start Server

```bash
npm start
```

**Expected output:**
```
[2025-12-27T10:00:00.000Z] What-if Simulation Engine started
Port: 7000
Max traversal depth: 2
Default latency metric: p95
Scaling model: bounded_sqrt (alpha: 0.5)
Timeout: 8000ms
```

### Verify Connection

```bash
curl http://localhost:7000/health
```

**Expected response:**
```json
{
  "status": "ok",
  "neo4j": {
    "connected": true,
    "services": 11
  },
  "config": {
    "maxTraversalDepth": 2,
    "defaultLatencyMetric": "p95"
  },
  "uptimeSeconds": 5.2
}
```

---

## Demo Commands

### 1. Health Check

```bash
curl http://localhost:7000/health
```

### 2. Simulate Service Failure

Simulates what happens if `checkoutservice` becomes unavailable.

```bash
curl -X POST http://localhost:7000/simulate/failure \
  -H "Content-Type: application/json" \
  -d '{"serviceId": "default:checkoutservice"}'
```

**Expected response (abbreviated):**
```json
{
  "target": {
    "serviceId": "default:checkoutservice",
    "name": "checkoutservice",
    "namespace": "default"
  },
  "neighborhood": {
    "serviceCount": 3,
    "edgeCount": 2,
    "depthUsed": 2
  },
  "affectedCallers": [
    {
      "serviceId": "default:frontend",
      "lostTrafficRps": 0.178,
      "edgeErrorRate": 0.0
    }
  ],
  "criticalPathsToTarget": [
    {
      "path": ["default:loadgenerator", "default:frontend", "default:checkoutservice"],
      "pathRps": 0.178
    }
  ],
  "totalLostTrafficRps": 0.178
}
```

### 3. Simulate Scaling

Simulates scaling `frontend` from 2 to 6 pods and predicts latency impact.

```bash
curl -X POST http://localhost:7000/simulate/scale \
  -H "Content-Type: application/json" \
  -d '{
    "serviceId": "default:frontend",
    "currentPods": 2,
    "newPods": 6
  }'
```

**Expected response (abbreviated):**
```json
{
  "target": {
    "serviceId": "default:frontend",
    "name": "frontend",
    "namespace": "default"
  },
  "latencyMetric": "p95",
  "currentPods": 2,
  "newPods": 6,
  "latencyEstimate": {
    "baselineMs": 34.67,
    "projectedMs": 24.89,
    "deltaMs": -9.78
  },
  "affectedCallers": {
    "items": [
      {
        "serviceId": "default:loadgenerator",
        "hopDistance": 1,
        "baselineMs": 34.67,
        "projectedMs": 24.89,
        "deltaMs": -9.78
      }
    ]
  }
}
```

---

## Troubleshooting

### "Missing required environment variables"

```
❌ Missing required environment variables:
   - NEO4J_URI is required
   - NEO4J_PASSWORD is required
```

**Solution:** Ensure `.env` file exists with valid credentials.

### "Service not found"

**Cause:** Target service doesn't exist in Neo4j graph.

**Solution:** Verify `service-graph-engine` has synced data:
```bash
node verify-schema.js
```

### "Query timeout exceeded"

**Solution:** Reduce `maxDepth` in request or increase `TIMEOUT_MS` in `.env`.

---

## Kubernetes Deployment (Future Phase)

Kubernetes manifests are provided in `k8s/base/` for future in-cluster deployment.

### Build Container Image

```bash
docker build -t what-if-simulation-engine:latest .
```

### Load Image into Minikube

The cluster cannot pull `what-if-simulation-engine:latest` from a registry—you must load it:

**Option A (recommended):**
```bash
minikube image load what-if-simulation-engine:latest
```

**Option B (build inside minikube's Docker):**
```bash
eval $(minikube docker-env)
docker build -t what-if-simulation-engine:latest .
```

### Deploy to Cluster

```bash
# Create secret first (example)
kubectl create secret generic neo4j-credentials \
  --from-literal=NEO4J_URI='neo4j+s://xxx.databases.neo4j.io' \
  --from-literal=NEO4J_USER='neo4j' \
  --from-literal=NEO4J_PASSWORD='your-password'

# Apply manifests
kubectl apply -k k8s/base/

# Port-forward for local access (use 7001 to avoid host conflicts)
kubectl port-forward svc/what-if-simulation-engine 7001:7000
```

Then test via `http://localhost:7001/health`.

### Resource Configuration

| Resource | Request | Limit |
|----------|---------|-------|
| CPU | 100m | 300m |
| Memory | 128Mi | 256Mi |

These are conservative defaults suitable for demo workloads.
