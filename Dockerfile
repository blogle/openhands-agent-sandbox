# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /build

# Copy go.mod and go.sum first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /runtime-api \
    ./cmd/runtime-api

# Runtime stage
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /runtime-api /runtime-api

USER 65532:65532

ENTRYPOINT ["/runtime-api"]
