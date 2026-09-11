# Operations Guide

## Prerequisites

1. Kubernetes cluster with agent-sandbox CRDs and controller installed.
2. `kubectl` configured for the target cluster.

## Required CRDs

```bash
kubectl get crd | grep agents.x-k8s.io
```

Expected:
- `sandboxes.agents.x-k8s.io`
- `sandboxclaims.extensions.agents.x-k8s.io`
- `sandboxtemplates.extensions.agents.x-k8s.io`
- `sandboxwarmpools.extensions.agents.x-k8s.io`

If missing, install agent-sandbox first:
```bash
# See https://github.com/kubernetes-sigs/agent-sandbox
kubectl apply -f https://github.com/kubernetes-sigs/agent-sandbox/releases/latest/download/install.yaml
```

## Installation

### 1. Create Secrets

```bash
kubectl create secret generic openhands-runtime-secrets \
  -n openhands-sandboxes \
  --from-literal=api-key=$(openssl rand -hex 32) \
  --from-literal=bootstrap-secret=$(openssl rand -hex 32) \
  --from-literal=server-image=ghcr.io/openhands/agent-server:latest
```

Also create the bootstrap key secret referenced by the SandboxTemplate:

```bash
kubectl create secret generic openhands-bootstrap-key \
  -n openhands-sandboxes \
  --from-literal=secret-key=<same-as-bootstrap-secret>
```

### 2. Edit Configuration

Edit `deploy/base/configmap.yaml` to set:
- `PUBLIC_BASE_URL`: Your externally reachable runtime URL
- `OPENHANDS_SERVER_IMAGE`: The Agent Server image to use

### 3. Deploy

```bash
kubectl apply -k deploy/base/
```

### 4. Configure OpenHands

In your OpenHands configuration:
```python
from openhands.sdk.workspace.remote_api.workspace import APIRemoteWorkspace

workspace = APIRemoteWorkspace(
    runtime_api_url="https://openhands-runtime.example.com",
    runtime_api_key="<your-api-key>",
    server_image="ghcr.io/openhands/agent-server:latest",
)
```

## Cold Mode (default)

Default deployment uses `replicas: 0` for the warm pool. Runtimes are cold-started on demand. This consumes no idle resources.

## Enabling Warm Pool

```bash
kubectl apply -k deploy/overlays/warm/
```

This changes warm pool replicas from 0 to 1.

## Viewing Resources

```bash
# SandboxClaims
kubectl get sandboxclaims -n openhands-sandboxes

# Sandboxes
kubectl get sandboxes -n openhands-sandboxes

# Secrets (for debugging)
kubectl get secrets -n openhands-sandboxes -l app.kubernetes.io/managed-by=openhands-agent-sandbox-runtime
```

## Pause/Resume Debugging

```bash
# Check sandbox operatingMode
kubectl get sandbox <name> -n openhands-sandboxes -o jsonpath='{.spec.operatingMode}'

# Check conditions
kubectl get sandbox <name> -n openhands-sandboxes -o jsonpath='{.status.conditions}'
```

## Stuck Init Debugging

```bash
# Check pod logs
kubectl logs -n openhands-sandboxes <pod-name>

# Check init status
kubectl exec -n openhands-sandboxes <pod-name> -- wget -qO- http://localhost:60000/api/init
```

## PVC Inspection

```bash
kubectl get pvc -n openhands-sandboxes
```

## Metrics

```bash
kubectl port-forward svc/openhands-runtime-adapter 8080:80 -n openhands-sandboxes
curl http://localhost:8080/metrics
```

## Upgrading Agent Server Image

1. Update `OPENHANDS_SERVER_IMAGE` in the secrets
2. Update the image in `deploy/base/sandbox-template.yaml`
3. Reapply: `kubectl apply -k deploy/base/`
4. Existing runtimes will use the old image; new runtimes use the new one.
