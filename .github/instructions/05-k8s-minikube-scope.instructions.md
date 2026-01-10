---
applyTo: "k8s/**/*,**/Dockerfile,**/*.yaml"
description: 'Kubernetes deployment context - Kustomize structure, Minikube local development, and manifest guidelines'
---

# Kubernetes & Minikube Scope

This document defines the Kubernetes deployment context for this service.

---

## Deployment Structure

The repo uses Kustomize for K8s manifests:

```
k8s/
└── base/
    ├── kustomization.yaml
    ├── deployment.yaml
    └── service.yaml
```

---

## Supported Profiles

### Profile A: Remote Graph Engine

- **Graph Engine:** Cloud-hosted or remote cluster
- **Connection:** HTTP URL via environment variable
- **Use case:** Production-like, staging environments

```yaml
# Environment config
SERVICE_GRAPH_ENGINE_URL: http://service-graph-engine.production.svc.cluster.local:8080
```

### Profile B: Local Graph Engine (Minikube)

- **Graph Engine:** Local instance in Minikube
- **Connection:** Local cluster DNS
- **Use case:** Local development, testing

```yaml
# Environment config for local
SERVICE_GRAPH_ENGINE_URL: http://service-graph-engine:8080
```

---

## Configuration Management

### Pattern (from deployment.yaml)

```yaml
env:
  - name: SERVICE_GRAPH_ENGINE_URL
    value: "http://service-graph-engine:8080"
  - name: GRAPH_ENGINE_TIMEOUT_MS
    value: "5000"
```

### Using ConfigMap (alternative)

```bash
# For production
kubectl create configmap predictive-engine-config \
  --from-literal=SERVICE_GRAPH_ENGINE_URL='http://service-graph-engine.production.svc.cluster.local:8080'

# For local development
kubectl create configmap predictive-engine-config \
  --from-literal=SERVICE_GRAPH_ENGINE_URL='http://service-graph-engine:8080'
```

---

## Resource Limits

From `deployment.yaml`:

```yaml
resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 300m
    memory: 256Mi
```

Copilot must preserve these limits unless explicitly asked to change them.

---

## Health Probes

From `deployment.yaml`:

```yaml
readinessProbe:
  httpGet:
    path: /health
    port: http
  initialDelaySeconds: 5
  periodSeconds: 10

livenessProbe:
  httpGet:
    path: /health
    port: http
  initialDelaySeconds: 10
  periodSeconds: 30
```

Copilot must preserve health probe configuration.

---

## Security Context

From `deployment.yaml`:

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 1001
  runAsGroup: 1001
  fsGroup: 1001
```

Copilot must preserve security context settings.

---

## Scope Limitations

### Copilot CAN

- Modify deployment.yaml for configuration changes
- Add new environment variables (non-secret)
- Adjust resource limits (if requested)
- Update labels/annotations

### Copilot CANNOT

- Add Helm charts (use existing Kustomize)
- Add CI/CD workflows
- Create new overlay directories without approval
- Modify secrets management patterns

---

## Local Development (Non-K8s)

For local development without K8s:

```bash
# 1. Copy .env.example to .env
cp .env.example .env

# 2. Configure Graph Engine URL
# SERVICE_GRAPH_ENGINE_URL=http://localhost:8080 (local)
# OR
# SERVICE_GRAPH_ENGINE_URL=http://service-graph-engine.production:8080 (remote)

# 3. Start server
npm start
```

---

## Quick Reference

| Environment | Graph Engine URL Pattern | Config Source |
|-------------|--------------------------|---------------|
| Local dev | http://localhost:8080 | .env file |
| Minikube | http://service-graph-engine:8080 | ConfigMap or env |
| Production | http://service-graph-engine.prod:8080 | ConfigMap or env |
