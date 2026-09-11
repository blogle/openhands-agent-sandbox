# Architecture

## Overview

`openhands-agent-sandbox-runtime` is a stateless adapter that translates the OpenHands Remote Runtime HTTP API into Kubernetes `agent-sandbox` lifecycle operations.

```
OpenHands / APIRemoteWorkspace
    |
    | OpenHands Remote Runtime HTTP API
    v
+------------------------------------------+
| openhands-agent-sandbox-runtime          |
|                                          |
| REST compatibility layer                 |
| Runtime <-> SandboxClaim mapping          |
| Secret/init-payload persistence          |
| Agent Server deferred-init activation    |
| Reverse proxy to runtime                 |
| Metrics / health                         |
+----------------------+-------------------+
                       |
                       | Kubernetes API
                       v
+------------------------------------------+
| kubernetes-sigs/agent-sandbox            |
|                                          |
| SandboxClaim                             |
| SandboxWarmPool                          |
| SandboxTemplate                          |
| Sandbox                                  |
| PVC                                      |
| lifecycle / reconciliation               |
+----------------------+-------------------+
                       |
                       v
                 Agent Server Pod
                  /workspace PVC
```

## Ownership Boundaries

### This repository owns

- **OpenHands Remote Runtime HTTP compatibility**: All `/start`, `/stop`, `/pause`, `/resume`, `/list`, `/sessions/*` endpoints.
- **Runtime/session identity mapping**: UUID generation, claim naming, session hashing.
- **Deferred-init coordination**: POST `/api/init` to Agent Server after claim becomes ready.
- **Per-runtime init configuration**: Kubernetes Secrets containing session keys, secret keys, init payloads.
- **Stable reverse proxy URL**: `/sandbox/<runtime-id>/*` routes to current pod IP.
- **Protocol translation**: Converts OpenHands HTTP requests to agent-sandbox Kubernetes API calls.

### Agent Sandbox owns

- **Pods**: Created, reconciled, and destroyed by the agent-sandbox controller.
- **Sandbox reconciliation**: The controller watches Sandbox objects and maintains desired state.
- **Restart behavior**: Agent-sandbox handles pod restarts per Sandbox spec.
- **PVC lifecycle**: Volume provisioning, attachment across suspend/resume.
- **Warm pools**: SandboxWarmPool pre-provisions sandboxes for fast allocation.
- **Claim adoption**: SandboxClaim binds to pre-warmed Sandbox.
- **Suspension**: `Sandbox.spec.operatingMode = Suspended` terminates the pod.
- **Resumption**: `Sandbox.spec.operatingMode = Running` restarts the pod.
- **Sandbox identity**: The Sandbox object is the authoritative identity.

### OpenHands owns

- **Agent Server**: The in-sandbox process that serves conversations.
- **Agent/session semantics**: Session management, conversation lifecycle.
- **Session API authentication**: `X-Session-API-Key` header validation.
- **`/api/init`**: Deferred initialization endpoint.
- **Conversations/tools/files**: All agent workspace operations.

## Design Decisions

### Stateless adapter

The adapter has no database, no in-memory state, and no leader election. All runtime state is derived from Kubernetes resources (SandboxClaims, Sandboxes, Secrets). Multiple replicas can serve requests concurrently.

### SandboxClaim for provisioning

Each OpenHands runtime maps to exactly one `SandboxClaim` pointing at a `SandboxWarmPool`. The claim's name is derived from a cryptographically random UUID: `oh-<first 20 hex chars>`.

### operatingMode for pause/resume

Instead of deleting/recreating pods, pause patches `Sandbox.spec.operatingMode = Suspended` and resume patches it back to `Running`. The PVC persists across suspension.

### Deferred init

Warm Agent Server pods start in dormant mode (`OH_DEFERRED_INIT=true`). After a claim binds to a sandbox and the pod is ready, the adapter POSTs `/api/init` with session-specific credentials and environment.

### Stable proxy URL

No per-session Ingress objects. One external hostname serves all runtimes via path-based routing: `https://openhands-runtime.example.com/sandbox/<runtime-id>/*`.
