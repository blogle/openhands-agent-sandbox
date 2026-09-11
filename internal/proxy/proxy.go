// Package proxy implements the reverse proxy for Agent Server traffic.
package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

// PodResolver resolves a runtime ID to the current pod IP and port.
type PodResolver interface {
	ResolvePodIP(ctx context.Context, runtimeID string) (ip string, port int, err error)
}

// Proxy is a reverse proxy that routes /sandbox/<runtime-id>/* requests
// to the corresponding Agent Server pod.
type Proxy struct {
	resolver PodResolver
	mu       sync.RWMutex
}

// New creates a new Proxy.
func New(resolver PodResolver) *Proxy {
	return &Proxy{resolver: resolver}
}

// ServeHTTP implements http.Handler.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract runtime ID from path: /sandbox/<runtime-id>/...
	path := strings.TrimPrefix(r.URL.Path, "/sandbox/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "missing runtime ID", http.StatusBadRequest)
		return
	}

	runtimeID := parts[0]
	remainingPath := ""
	if len(parts) > 1 {
		remainingPath = "/" + parts[1]
	}

	// Resolve current pod IP
	podIP, port, err := p.resolver.ResolvePodIP(r.Context(), runtimeID)
	if err != nil {
		slog.Error("failed to resolve pod IP", "runtime_id", runtimeID, "error", err)
		http.Error(w, "runtime not found", http.StatusNotFound)
		return
	}

	// Build target URL
	targetHost := net.JoinHostPort(podIP, fmt.Sprintf("%d", port))
	target := &url.URL{
		Scheme: "http",
		Host:   targetHost,
	}

	// Create reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = remainingPath
		req.URL.RawPath = ""
		req.Host = target.Host

		// Remove management headers that shouldn't be forwarded
		req.Header.Del("X-API-Key")
	}

	// Handle WebSocket upgrades
	proxy.ModifyResponse = func(resp *http.Response) error {
		// Pass through streaming/WebSocket responses
		return nil
	}

	// Custom error handler
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		slog.Error("proxy error", "runtime_id", runtimeID, "error", err)
		http.Error(w, "proxy error: "+err.Error(), http.StatusBadGateway)
	}

	proxy.ServeHTTP(w, r)
}

// WaitForPodReady waits until a runtime's pod is resolvable and healthy.
func (p *Proxy) WaitForPodReady(ctx context.Context, runtimeID string, timeout time.Duration) (ip string, port int, err error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", 0, fmt.Errorf("timeout waiting for pod ready: %w", ctx.Err())
		case <-ticker.C:
			ip, port, err = p.resolver.ResolvePodIP(ctx, runtimeID)
			if err == nil && ip != "" {
				return ip, port, nil
			}
		}
	}
}
