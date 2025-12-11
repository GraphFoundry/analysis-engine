# Deployment Guide

## ⚠️ Breaking Change (v2.0)

The Kubernetes resources have been renamed from `predictive-analysis-engine` to `predictive-analysis-engine`.

**Migration steps:**
1. Delete old resources: `kubectl delete deployment,svc predictive-analysis-engine`
2. Apply new manifests: `kubectl apply -k k8s/base/`
3. Update any clients/ingress referencing the old service name `predictive-analysis-engine`

The service DNS name changes from `predictive-analysis-engine.<namespace>.svc.cluster.local` to `predictive-analysis-engine.<namespace>.svc.cluster.local`.

---

## Local Demo (Current Phase)

### Prerequisites

- Node.js >= 18.x
- Running `service-graph-engine` instance (Graph Engine API)
- Graph Engine API URL configured

### Setup

```bash
# 1. Install dependencies
npm install

# 2. Configure environment
cp .env.example .env

# 3. Edit .env with Graph Engine API URL
#    Required: SERVICE_GRAPH_ENGINE_URL
```

### Start Server

```bash
npm start
```

**Expected output:**
```
[2025-12-27T10:00:00.000Z] Predictive Analysis Engine started
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
  "dataSource": "graph-engine",
  "provider": {
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
   - SERVICE_GRAPH_ENGINE_URL is required
```

**Solution:** Ensure `.env` file exists with valid Graph Engine API URL.

### "Service not found"

**Cause:** Target service doesn't exist in Graph Engine.

**Solution:** Verify `service-graph-engine` is running and has synced data:
```bash
curl http://localhost:8080/health
```

### "Query timeout exceeded"

**Solution:** Reduce `maxDepth` in request or increase `TIMEOUT_MS` in `.env`.

---

## Kubernetes Deployment (Future Phase)

Kubernetes manifests are provided in `k8s/base/` for future in-cluster deployment.

### Build Container Image

```bash
docker build -t predictive-analysis-engine:latest .
```

### Load Image into Minikube

The cluster cannot pull `predictive-analysis-engine:latest` from a registry—you must load it:

**Option A (recommended):**
```bash
minikube image load predictive-analysis-engine:latest
```

**Option B (build inside minikube's Docker):**
```bash
eval $(minikube docker-env)
docker build -t predictive-analysis-engine:latest .
```

### Deploy to Cluster

```bash
# Create config (example - or use ConfigMap)
kubectl set env deployment/predictive-analysis-engine \
  SERVICE_GRAPH_ENGINE_URL='http://service-graph-engine:8080'

# Apply manifests
kubectl apply -k k8s/base/

# Port-forward for local access (use 7001 to avoid host conflicts)
kubectl port-forward svc/predictive-analysis-engine 7001:7000
```

Then test via `http://localhost:7001/health`.

### Resource Configuration

| Resource | Request | Limit |
|----------|---------|-------|
| CPU | 100m | 300m |
| Memory | 128Mi | 256Mi |

These are conservative defaults suitable for demo workloads.
