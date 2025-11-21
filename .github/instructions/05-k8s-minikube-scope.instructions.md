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

### Profile A: AuraDB Remote

- **Neo4j:** Cloud-hosted (Neo4j AuraDB)
- **Credentials:** Provided via K8s secrets
- **Use case:** Production-like, staging environments

```yaml
# K8s secret for AuraDB
NEO4J_URI: neo4j+s://xxxx.databases.neo4j.io
NEO4J_USER: neo4j
NEO4J_PASSWORD: <aura-password>
```

### Profile B: Local Neo4j (Minikube)

- **Neo4j:** Local instance in Minikube
- **Credentials:** Local dev credentials
- **Use case:** Local development, testing

```yaml
# K8s secret for local Neo4j
NEO4J_URI: bolt://neo4j:7687
NEO4J_USER: neo4j
NEO4J_PASSWORD: <local-password>
```

---

## Secret Management

### Pattern (from deployment.yaml)

```yaml
env:
  - name: NEO4J_URI
    valueFrom:
      secretKeyRef:
        name: neo4j-credentials
        key: NEO4J_URI
  - name: NEO4J_PASSWORD
    valueFrom:
      secretKeyRef:
        name: neo4j-credentials
        key: NEO4J_PASSWORD
```

### Creating Secrets

```bash
# For AuraDB
kubectl create secret generic neo4j-credentials \
  --from-literal=NEO4J_URI='neo4j+s://xxxx.databases.neo4j.io' \
  --from-literal=NEO4J_USER='neo4j' \
  --from-literal=NEO4J_PASSWORD='your-password'

# For local Neo4j
kubectl create secret generic neo4j-credentials \
  --from-literal=NEO4J_URI='bolt://neo4j:7687' \
  --from-literal=NEO4J_USER='neo4j' \
  --from-literal=NEO4J_PASSWORD='local-password'
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

# 2. Fill in credentials
# NEO4J_URI=neo4j+s://... (AuraDB)
# OR
# NEO4J_URI=bolt://localhost:7687 (local Neo4j)

# 3. Start server
npm start
```

---

## Quick Reference

| Environment | Neo4j URI Pattern | Secret Source |
|-------------|-------------------|---------------|
| Local dev | .env file | .env |
| Minikube | bolt://neo4j:7687 | K8s secret |
| AuraDB | neo4j+s://xxx.databases.neo4j.io | K8s secret |
