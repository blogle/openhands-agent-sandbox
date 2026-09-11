package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/openhands-agent-sandbox/openhands-agent-sandbox/internal/runtime"
)

const testAPIKey = "test-api-key"

type fakeBackend struct {
	mu        sync.Mutex
	runtimes  map[string]*runtime.Runtime
	bySession map[string]string
	nextID    int
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		runtimes:  make(map[string]*runtime.Runtime),
		bySession: make(map[string]string),
	}
}

func (f *fakeBackend) Start(_ context.Context, req runtime.StartRequest) (*runtime.Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	rtID := fmt.Sprintf("rt-%d", f.nextID)
	rt := &runtime.Runtime{
		RuntimeID: rtID,
		SessionID: req.SessionID,
		URL:       fmt.Sprintf("https://example.com/sandbox/%s", rtID),
		Status:    runtime.StatusRunning,
		PodStatus: runtime.PodStatusReady,
		WorkHosts: map[string]int{},
	}
	f.runtimes[rtID] = rt
	f.bySession[req.SessionID] = rtID
	return rt, nil
}

func (f *fakeBackend) Stop(_ context.Context, runtimeID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.runtimes[runtimeID]; !ok {
		return errors.New("runtime not found")
	}
	delete(f.runtimes, runtimeID)
	return nil
}

func (f *fakeBackend) Pause(_ context.Context, _ string) error { return nil }

func (f *fakeBackend) Resume(_ context.Context, runtimeID string) (*runtime.Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rt, ok := f.runtimes[runtimeID]
	if !ok {
		return nil, errors.New("runtime not found")
	}
	return rt, nil
}

func (f *fakeBackend) Get(_ context.Context, runtimeID string) (*runtime.Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rt, ok := f.runtimes[runtimeID]
	if !ok {
		return nil, errors.New("runtime not found")
	}
	return rt, nil
}

func (f *fakeBackend) GetBySession(_ context.Context, sessionID string) (*runtime.Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rtID, ok := f.bySession[sessionID]
	if !ok {
		return nil, fmt.Errorf("runtime not found for session: %s", sessionID)
	}
	return f.runtimes[rtID], nil
}

func (f *fakeBackend) List(_ context.Context) ([]runtime.Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	list := make([]runtime.Runtime, 0, len(f.runtimes))
	for _, rt := range f.runtimes {
		list = append(list, *rt)
	}
	return list, nil
}

func setupTest() (*Handler, *fakeBackend, *http.ServeMux) {
	backend := newFakeBackend()
	h := NewHandler(backend, testAPIKey, "ghcr.io/openhands", map[string]bool{
		"ghcr.io/openhands/agent-server:latest": true,
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return h, backend, mux
}

func authReq(method, target, body string) *http.Request {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	req.Header.Set("X-API-Key", testAPIKey)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestHealth(t *testing.T) {
	_, _, mux := setupTest()
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/health", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Fatalf("expected status ok, got %+v", resp)
	}
}

func TestHealth_NoAuth(t *testing.T) {
	_, _, mux := setupTest()
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/health", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("health should not require auth, got %d", rr.Code)
	}
}

func TestStartNoAuth(t *testing.T) {
	_, _, mux := setupTest()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/start", strings.NewReader(`{"session_id":"s1"}`))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestStartValid(t *testing.T) {
	_, backend, mux := setupTest()
	rr := httptest.NewRecorder()
	req := authReq("POST", "/start", `{"session_id":"s1","image":"ghcr.io/openhands/agent-server:latest"}`)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var rt runtime.Runtime
	_ = json.Unmarshal(rr.Body.Bytes(), &rt)
	if rt.SessionID != "s1" || rt.RuntimeID == "" {
		t.Fatalf("unexpected runtime: %+v", rt)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.runtimes) != 1 {
		t.Fatalf("expected 1 runtime, got %d", len(backend.runtimes))
	}
}

func TestStartMissingSessionID(t *testing.T) {
	_, _, mux := setupTest()
	rr := httptest.NewRecorder()
	req := authReq("POST", "/start", `{}`)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGetSession(t *testing.T) {
	_, backend, mux := setupTest()
	_, _ = backend.Start(context.Background(), runtime.StartRequest{SessionID: "s2"})

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, authReq("GET", "/sessions/s2", ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var rt runtime.Runtime
	_ = json.Unmarshal(rr.Body.Bytes(), &rt)
	if rt.SessionID != "s2" {
		t.Fatalf("expected session s2, got %s", rt.SessionID)
	}
}

func TestGetSessionUnknown(t *testing.T) {
	_, _, mux := setupTest()
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, authReq("GET", "/sessions/unknown", ""))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestRegistryPrefix(t *testing.T) {
	_, _, mux := setupTest()
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, authReq("GET", "/registry_prefix", ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp runtime.RegistryPrefixResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.RegistryPrefix != "ghcr.io/openhands" {
		t.Fatalf("unexpected prefix: %s", resp.RegistryPrefix)
	}
}

func TestImageExistsKnown(t *testing.T) {
	_, _, mux := setupTest()
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, authReq("GET", "/image_exists?image=ghcr.io/openhands/agent-server:latest", ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp runtime.ImageExistsResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if !resp.Exists {
		t.Fatal("expected exists=true for known image")
	}
}

func TestImageExistsUnknown(t *testing.T) {
	_, _, mux := setupTest()
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, authReq("GET", "/image_exists?image=unknown/image:v1", ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp runtime.ImageExistsResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Exists {
		t.Fatal("expected exists=false for unknown image")
	}
}

func TestStop(t *testing.T) {
	_, backend, mux := setupTest()
	rt, _ := backend.Start(context.Background(), runtime.StartRequest{SessionID: "s3"})

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, authReq("POST", "/stop", `{"runtime_id":"`+rt.RuntimeID+`"}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.runtimes) != 0 {
		t.Fatalf("expected 0 runtimes after stop, got %d", len(backend.runtimes))
	}
}

func TestList(t *testing.T) {
	_, backend, mux := setupTest()
	_, _ = backend.Start(context.Background(), runtime.StartRequest{SessionID: "s4"})
	_, _ = backend.Start(context.Background(), runtime.StartRequest{SessionID: "s5"})

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, authReq("GET", "/list", ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp runtime.ListResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Runtimes) != 2 {
		t.Fatalf("expected 2 runtimes, got %d", len(resp.Runtimes))
	}
}
