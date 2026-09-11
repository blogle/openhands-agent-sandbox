# openhands-agent-sandbox-runtime

Adapts the [OpenHands](https://github.com/OpenHands/software-agent-sdk) Remote Runtime API to [Kubernetes SIG Apps Agent Sandbox](https://github.com/kubernetes-sigs/agent-sandbox).

> **This project does not implement sandbox lifecycle itself.** It adapts OpenHands' Remote Runtime API to Kubernetes SIG Apps Agent Sandbox.

## Architecture

```
OpenHands / APIRemoteWorkspace
    |
    | Remote Runtime HTTP API
    v
openhands-agent-sandbox-runtime  (this project)
    |
    | Kubernetes API
    v
agent-sandbox  (SandboxClaim -> Sandbox -> Pod)
    |
    v
Agent Server Pod  (/workspace PVC)
```

The adapter translates HTTP requests into `SandboxClaim` lifecycle operations. It does not create Pods directly.

## Quick Start

### 1. Install agent-sandbox

```bash
kubectl apply -f https://github.com/kubernetes-sigs/agent-sandbox/releases/latest/download/install.yaml
```

### 2. Apply SandboxTemplate + WarmPool

```bash
kubectl apply -k deploy/base/
```

### 3. Create Secrets

```bash
kubectl create secret generic openhands-runtime-secrets \
  -n openhands-sandboxes \
  --from-literal=api-key=$(openssl rand -hex 32) \
  --from-literal=bootstrap-secret=$(openssl rand -hex 32) \
  --from-literal=server-image=ghcr.io/openhands/agent-server:latest

kubectl create secret generic openhands-bootstrap-key \
  -n openhands-sandboxes \
  --from-literal=secret-key=<same-as-bootstrap-secret>
```

### 4. Deploy

```bash
kubectl apply -k deploy/base/
```

### 5. Configure OpenHands

```python
from openhands.sdk.workspace.remote_api.workspace import APIRemoteWorkspace

workspace = APIRemoteWorkspace(
    runtime_api_url="https://openhands-runtime.example.com",
    runtime_api_key="<your-api-key>",
    server_image="ghcr.io/openhands/agent-server:latest",
)
```

### 6. Inspect SandboxClaims

```bash
kubectl get sandboxclaims -n openhands-sandboxes
```

### 7. Optionally enable warm pool

```bash
kubectl apply -k deploy/overlays/warm/
```

## Development

```bash
nix develop     # Enter dev shell
make test       # Run unit tests (no cluster)
make lint       # Run linters
make build      # Build binary
```

## License

Apache-2.0
