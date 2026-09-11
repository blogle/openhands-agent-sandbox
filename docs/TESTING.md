# Testing

## Unit Tests (no cluster required)

```bash
make test
```

Runs all unit tests using:
- Go standard test framework
- `httptest.Server` for HTTP handler tests
- Fake Kubernetes clients for backend tests
- No Docker, kind, k3d, minikube, or kubeconfig required

## Race Detector

```bash
make test-race
```

Runs tests with `-race` to detect data races.

## Linting

```bash
make lint
```

Runs `golangci-lint` for code quality checks.

## Build

```bash
make build
```

Builds the `runtime-api` binary to `bin/`.

## All Quality Gates

```bash
make all
```

Runs lint, test, and build in sequence.

## Optional Live E2E Tests

These require an existing Kubernetes cluster with agent-sandbox installed.

```bash
export RUN_LIVE_E2E=1
export LIVE_TEST_NAMESPACE=openhands-runtime-e2e
make test-live
```

## Optional OpenHands Compatibility Test

Tests the real `APIRemoteWorkspace` against the deployed adapter:

```bash
export RUN_LIVE_E2E=1
make test-openhands-live
```

## Optional Warm Pool Test

Tests warm pool allocation and deferred init:

```bash
export RUN_LIVE_E2E=1
make test-warm-live
```

## Nix Development Environment

```bash
nix develop
```

Provides: Go, gopls, golangci-lint, gotools, kubectl, kustomize, jq, curl.
