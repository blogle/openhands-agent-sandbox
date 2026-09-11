# Upstream Versions and References

This document records the upstream projects, versions, and reference
implementations used to build `openhands-agent-sandbox-runtime`.

## Kubernetes SIG Apps Agent Sandbox

- **Repository:** [kubernetes-sigs/agent-sandbox](https://github.com/kubernetes-sigs/agent-sandbox)
- **Release used:** `v1.0.2` (September 11, 2026)
- **Go module:** `sigs.k8s.io/agent-sandbox v1.0.2`
- **API groups:**
  - `agents.x-k8s.io/v1beta1` (`Sandbox`)
  - `extensions.agents.x-k8s.io/v1beta1` (`SandboxClaim`, `SandboxTemplate`, `SandboxWarmPool`)
- **Go version required:** `1.26.0` (toolchain `go1.26.4`)
- **Key dependencies:**
  - `k8s.io/client-go v0.37.0`
  - `sigs.k8s.io/controller-runtime v0.25.0`

## OpenHands Software Agent SDK

- **Repository:** [OpenHands/software-agent-sdk](https://github.com/OpenHands/software-agent-sdk)
- **Files inspected:**
  - `openhands-workspace/openhands/workspace/remote_api/workspace.py` (`APIRemoteWorkspace`)
  - `openhands-agent-server/openhands/agent_server/init_router.py` (deferred `/api/init`)
  - `openhands-agent-server/openhands/agent_server/models.py` (`InitRequest`, `InitStatus` models)
- **Key findings:**
  - `/api/init` uses the `X-Init-API-Key` header for bootstrap authentication.
  - `InitRequest` supports: `session_api_keys`, `secret_key`, `env`, `conversations_path`, `bash_events_dir`, `conversation_worktree_root`.
  - Init states: `dormant`, `initializing`, `ready`.
  - `GET /api/init` returns `InitStatus` without authentication.
  - Agent Server uses `SESSION_API_KEY` or `X-Session-API-Key` for runtime authentication.

## zparnold/openhands-kubernetes-remote-runtime

- **Repository:** [zparnold/openhands-kubernetes-remote-runtime](https://github.com/zparnold/openhands-kubernetes-remote-runtime)
- **Revision:** `main` branch (behavioral reference only)
- **Module:** `github.com/zparnold/openhands-kubernetes-remote-runtime`
- **Key findings:**
  - Uses `gorilla/mux` for routing.
  - In-memory state protected by `sync.RWMutex`.
  - Creates a `Pod`, `Service`, and `Ingress` for each runtime.
  - **Pause** deletes the pod while keeping state.
  - **Resume** recreates the pod from stored state.
  - Proxy mode via `PROXY_BASE_URL` using in-cluster service DNS.
  - `StartRequest` fields: `image`, `command` (`FlexibleCommand`), `working_dir`, `environment`, `session_id`, `resource_factor`, `runtime_class`.
  - `RuntimeResponse` fields: `runtime_id`, `session_id`, `url`, `session_api_key`, `status`, `pod_status`, `work_hosts`.

## Discrepancies from Specification

- The `zparnold` implementation uses in-memory state, while this project uses Kubernetes custom resources.
- The `zparnold` implementation pauses by deleting pods, while this project uses `Sandbox.spec.operatingMode=Suspended`.
- The `zparnold` implementation exposes each runtime on a per-session `Ingress`, while this project uses a stable proxy URL.
- Agent Server's `X-Session-API-Key` header is used for runtime authentication, not `X-API-Key`.
