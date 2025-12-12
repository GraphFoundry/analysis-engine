---
name: k8s-deployment
description: Guide for Kubernetes deployment using Minikube. Use this when asked about deployment, Kubernetes manifests, or local k8s testing.
license: MIT
---

# Kubernetes Deployment Skill

This skill helps you work with Kubernetes deployments for the predictive analysis engine, specifically targeting Minikube for local development.

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
│  │   analysis      │────▶│  Graph Engine   │           │
│  │    -engine      │     │   HTTP API      │           │
│  │   (Deployment)  │     │   (external)    │           │
│  └─────────────────┘     └─────────────────┘           │
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
docker build -t predictive-analysis-engine:local .

# Apply manifests
kubectl apply -k k8s/base/

# Verify deployment
kubectl get pods -l app=analysis-engine
kubectl get svc analysis-engine
```

### Access the Service
```bash
# Port forward for local access
kubectl port-forward svc/analysis-engine 3000:3000

# Or use Minikube service
minikube service analysis-engine --url
```

### View Logs
```bash
kubectl logs -l app=analysis-engine -f
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
  name: analysis-engine
  labels:
    app: analysis-engine
spec:
  replicas: 1
  selector:
    matchLabels:
      app: analysis-engine
  template:
    metadata:
      labels:
        app: analysis-engine
    spec:
      containers:
      - name: analysis-engine
        image: predictive-analysis-engine:local
        imagePullPolicy: Never  # Use local image
        ports:
        - containerPort: 3000
        env:
        - name: PORT
          value: "3000"
        - name: SERVICE_GRAPH_ENGINE_URL
          valueFrom:
            configMapKeyRef:
              name: graph-engine-config
              key: base-url
        - name: GRAPH_API_TIMEOUT_MS
          value: "20000"
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
  name: analysis-engine
spec:
  selector:
    app: analysis-engine
  ports:
  - port: 3000
    targetPort: 3000
  type: ClusterIP
```

## Secrets Management

### Create Graph Engine ConfigMap
```bash
kubectl create configmap graph-engine-config \
  --from-literal=base-url=http://service-graph-engine:3000
```

## Configuration Management

All external service configuration is stored in ConfigMaps (not Secrets, as URLs are not sensitive):

## Kustomize Pattern

```yaml
# k8s/base/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - deployment.yaml
  - service.yaml

commonLabels:
  app.kubernetes.io/name: analysis-engine
  app.kubernetes.io/component: backend
```

## Troubleshooting

### Pod Not Starting
```bash
# Check pod status
kubectl describe pod -l app=analysis-engine

# Check events
kubectl get events --sort-by='.lastTimestamp'
```

### Connection to Graph Engine Failing
```bash
# Verify Graph Engine is reachable from pod
kubectl exec -it <pod-name> -- nc -zv service-graph-engine 3000

# Check config is mounted
kubectl exec -it <pod-name> -- env | grep GRAPH

# Test HTTP connectivity
kubectl exec -it <pod-name> -- wget -O- http://service-graph-engine:3000/health
```

### Image Not Found
```bash
# Ensure using Minikube's Docker
eval $(minikube docker-env)
docker images | grep analysis-engine

# Rebuild if needed
docker build -t predictive-analysis-engine:local .
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
