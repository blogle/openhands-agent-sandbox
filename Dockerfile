# syntax=docker/dockerfile:1.7

# Build on the runner's native architecture. Go cross-compiles this static
# binary, avoiding slow QEMU emulation for the arm64 release image.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/runtime-api \
      ./cmd/runtime-api

FROM gcr.io/distroless/static:nonroot

COPY --from=builder /out/runtime-api /runtime-api

USER 65532:65532

ENTRYPOINT ["/runtime-api"]
