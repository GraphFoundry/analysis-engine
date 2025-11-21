---
name: k8s-deployment
description: Guide for Kubernetes deployment using Minikube. Use this when asked about deployment, Kubernetes manifests, or local k8s testing.
---

# Kubernetes Deployment Skill

This skill helps you work with Kubernetes deployments for the what-if simulation engine, specifically targeting Minikube for local development.

## When to Use This Skill

Use this skill when you need to:
- Deploy the application to Minikube
- Modify Kubernetes manifests
- Debug deployment issues
- Understand the k8s architecture

## Deployment Architecture

```
┌─────────────────────────────────────────────────────────┐
│                      Minikube                           │
│  ┌─────────────────┐     ┌─────────────────┐           │
│  │   simulation    │────▶│     Neo4j       │           │
│  │    -engine      │     │   (read-only)   │           │
│  │   (Deployment)  │     │                 │           │
│  └────────┬────────┘     └─────────────────┘           │
│           │                                             │
│           │              ┌─────────────────┐           │
│           └─────────────▶│    Graph API    │           │
│                          │   (external)    │           │
│                          └─────────────────┘           │
└─────────────────────────────────────────────────────────┘
```

## File Structure

```
k8s/
└── base/
    ├── kustomization.yaml  # Kustomize configuration
    ├── deployment.yaml     # Main deployment
    └── service.yaml        # Service exposure
```

## Quick Commands

### Deploy to Minikube
```bash
# Start Minikube (if not running)
minikube start

# Build image in Minikube's Docker
eval $(minikube docker-env)
docker build -t what-if-simulation-engine:local .

# Apply manifests
kubectl apply -k k8s/base/

# Verify deployment
kubectl get pods -l app=simulation-engine
kubectl get svc simulation-engine
```

### Access the Service
```bash
# Port forward for local access
kubectl port-forward svc/simulation-engine 3000:3000

# Or use Minikube service
minikube service simulation-engine --url
```

### View Logs
```bash
kubectl logs -l app=simulation-engine -f
```

### Delete Deployment
```bash
kubectl delete -k k8s/base/
```

## Deployment Manifest Pattern

```yaml
# k8s/base/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: simulation-engine
  labels:
    app: simulation-engine
spec:
  replicas: 1
  selector:
    matchLabels:
      app: simulation-engine
  template:
    metadata:
      labels:
        app: simulation-engine
    spec:
      containers:
      - name: simulation-engine
        image: what-if-simulation-engine:local
        imagePullPolicy: Never  # Use local image
        ports:
        - containerPort: 3000
        env:
        - name: PORT
          value: "3000"
        - name: NEO4J_URI
          valueFrom:
            secretKeyRef:
              name: neo4j-credentials
              key: uri
        - name: NEO4J_USER
          valueFrom:
            secretKeyRef:
              name: neo4j-credentials
              key: username
        - name: NEO4J_PASSWORD
          valueFrom:
            secretKeyRef:
              name: neo4j-credentials
              key: password
        resources:
          requests:
            memory: "128Mi"
            cpu: "100m"
          limits:
            memory: "256Mi"
            cpu: "500m"
        livenessProbe:
          httpGet:
            path: /health
            port: 3000
          initialDelaySeconds: 10
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /health
            port: 3000
          initialDelaySeconds: 5
          periodSeconds: 10
```

## Service Manifest Pattern

```yaml
# k8s/base/service.yaml
apiVersion: v1
kind: Service
metadata:
  name: simulation-engine
spec:
  selector:
    app: simulation-engine
  ports:
  - port: 3000
    targetPort: 3000
  type: ClusterIP
```

## Secrets Management

### Create Neo4j Secret
```bash
kubectl create secret generic neo4j-credentials \
  --from-literal=uri=bolt://neo4j:7687 \
  --from-literal=username=neo4j \
  --from-literal=password=<password>
```

### Create Graph API Secret (if needed)
```bash
kubectl create secret generic graph-api-config \
  --from-literal=base-url=http://graph-api:8080
```

## Kustomize Pattern

```yaml
# k8s/base/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - deployment.yaml
  - service.yaml

commonLabels:
  app.kubernetes.io/name: simulation-engine
  app.kubernetes.io/component: backend
```

## Troubleshooting

### Pod Not Starting
```bash
# Check pod status
kubectl describe pod -l app=simulation-engine

# Check events
kubectl get events --sort-by='.lastTimestamp'
```

### Connection to Neo4j Failing
```bash
# Verify Neo4j is reachable from pod
kubectl exec -it <pod-name> -- nc -zv neo4j 7687

# Check secret is mounted
kubectl exec -it <pod-name> -- env | grep NEO4J
```

### Image Not Found
```bash
# Ensure using Minikube's Docker
eval $(minikube docker-env)
docker images | grep simulation-engine

# Rebuild if needed
docker build -t what-if-simulation-engine:local .
```

## Scope Limitations

**This project's k8s scope is LIMITED to Minikube for local development.**

❌ NOT in scope:
- Production cluster deployments
- Helm charts
- Cloud-specific configurations (EKS, GKE, AKS)
- Service mesh configurations
- Ingress controllers (beyond basic)

## References

- [k8s/base/deployment.yaml](../../../k8s/base/deployment.yaml)
- [k8s/base/service.yaml](../../../k8s/base/service.yaml)
- [DEPLOYMENT.md](../../../DEPLOYMENT.md) — Deployment documentation
- [Dockerfile](../../../Dockerfile) — Container build
