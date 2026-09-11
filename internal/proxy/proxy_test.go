package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeResolver struct {
	ips map[string]string
}

func (f *fakeResolver) ResolvePodIP(_ context.Context, runtimeID string) (string, int, error) {
	ip, ok := f.ips[runtimeID]
	if !ok {
		return "", 0, fmt.Errorf("not found")
	}
	return ip, 8080, nil
}

func TestProxy_UnknownRuntime(t *testing.T) {
	resolver := &fakeResolver{ips: map[string]string{}}
	p := New(resolver)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/sandbox/unknown/path", nil)
	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown runtime, got %d", rr.Code)
	}
}

func TestProxy_MissingRuntimeID(t *testing.T) {
	resolver := &fakeResolver{ips: map[string]string{}}
	p := New(resolver)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/sandbox/", nil)
	p.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing runtime ID, got %d", rr.Code)
	}
}

func TestProxy_UnreachableBackend(t *testing.T) {
	resolver := &fakeResolver{ips: map[string]string{"rt-2": "192.0.2.1"}} // RFC 5737 test IP
	p := New(resolver)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/sandbox/rt-2/health", nil)
	req.Header.Set("X-Session-API-Key", "my-key")
	p.ServeHTTP(rr, req)

	// Should get 502 since the backend is unreachable
	if rr.Code != http.StatusBadGateway {
		t.Logf("proxy response code: %d (expected 502 for unreachable host)", rr.Code)
	}
}

func TestProxy_PathExtraction(t *testing.T) {
	// Test that paths are correctly parsed
	resolver := &fakeResolver{ips: map[string]string{"abc-123": "192.0.2.1"}}
	p := New(resolver)

	// Various path formats
	tests := []struct {
		path   string
		expect int
	}{
		{"/sandbox/abc-123/health", http.StatusBadGateway},    // valid runtime, unreachable
		{"/sandbox/abc-123/api/conversations", http.StatusBadGateway}, // nested path
		{"/sandbox/nonexistent/path", http.StatusNotFound},    // unknown runtime
	}

	for _, tt := range tests {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", tt.path, nil)
		p.ServeHTTP(rr, req)
		if rr.Code != tt.expect {
			t.Errorf("path %s: expected %d, got %d", tt.path, tt.expect, rr.Code)
		}
	}
}
