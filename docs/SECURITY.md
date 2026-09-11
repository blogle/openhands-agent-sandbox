# Security

## Authentication

### Management API

All management endpoints (except `/health`, `/liveness`, `/readiness`, `/metrics`) require:

```
X-API-Key: <runtime-api-key>
```

The API key is compared using constant-time comparison. It is never logged.

### Agent Server Session Auth

Agent Server protects its API with:

```
X-Session-API-Key: <session-api-key>
```

Session keys are generated per-runtime and delivered via deferred init. They are stored in Kubernetes Secrets and never appear in logs or metrics.

### Bootstrap Auth

The deferred init endpoint uses:

```
X-Init-API-Key: <bootstrap-key>
```

This key is known to the adapter and to the dormant Agent Server. It is replaced by the session API key after initialization.

## Credential Storage

- **Kubernetes Secrets**: Per-runtime credentials are stored in Secrets named `openhands-runtime-<runtime-id>`.
- **Owner references**: Each Secret is owned by its SandboxClaim, so deleting the claim cascades to the Secret.
- **Never in annotations/labels**: Credentials are never placed in Kubernetes annotations or labels.
- **Never in logs**: The structured logger excludes all secret values.

## Sandbox Security Context

Default SandboxTemplate security:

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 10001
  runAsGroup: 10001
  fsGroup: 10001
  seccompProfile:
    type: RuntimeDefault
```

Container security:

```yaml
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
      - ALL
```

```yaml
automountServiceAccountToken: false
```

## No Credentials in Image

The container image contains only the static Go binary. No kubeconfig, API keys, or bootstrap secrets are baked in.

## Concurrency Safety

- No global process mutexes
- No local caches assumed authoritative
- Kubernetes API optimistic concurrency prevents duplicate claims
- Duplicate `/start` for the same session returns the existing runtime
