// Package httpapi implements the OpenHands Remote Runtime HTTP API.
package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/openhands-agent-sandbox/openhands-agent-sandbox/internal/metrics"
	"github.com/openhands-agent-sandbox/openhands-agent-sandbox/internal/runtime"
)

// Handler handles OpenHands Remote Runtime HTTP requests.
type Handler struct {
	backend        runtime.RuntimeBackend
	apiKey         string
	registryPrefix string
	knownImages    map[string]bool
}

// NewHandler creates a new HTTP handler.
func NewHandler(
	backend runtime.RuntimeBackend,
	apiKey string,
	registryPrefix string,
	knownImages map[string]bool,
) *Handler {
	return &Handler{
		backend:        backend,
		apiKey:         apiKey,
		registryPrefix: registryPrefix,
		knownImages:    knownImages,
	}
}

// RegisterRoutes registers all HTTP routes on the provided mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Health endpoints (no auth)
	mux.HandleFunc("GET /health", h.handleHealth)
	mux.HandleFunc("GET /liveness", h.handleHealth)
	mux.HandleFunc("GET /readiness", h.handleHealth)
	// NOTE: /metrics is registered by main.go with promhttp.Handler()

	// Authenticated management endpoints
	mux.HandleFunc("POST /start", h.withAuth(h.handleStart))
	mux.HandleFunc("POST /stop", h.withAuth(h.handleStop))
	mux.HandleFunc("POST /pause", h.withAuth(h.handlePause))
	mux.HandleFunc("POST /resume", h.withAuth(h.handleResume))
	mux.HandleFunc("GET /list", h.withAuth(h.handleList))
	mux.HandleFunc("GET /runtime/{runtime_id}", h.withAuth(h.handleGetRuntime))
	mux.HandleFunc("GET /sessions/{session_id}", h.withAuth(h.handleGetSession))
	mux.HandleFunc("POST /sessions/batch", h.withAuth(h.handleSessionsBatch))
	mux.HandleFunc("GET /sessions/batch", h.withAuth(h.handleSessionsBatchGet))
	mux.HandleFunc("GET /registry_prefix", h.withAuth(h.handleRegistryPrefix))
	mux.HandleFunc("GET /image_exists", h.withAuth(h.handleImageExists))
}

// RegisterProxy registers the reverse proxy route. This is separate because
// it does not require management auth.
func (h *Handler) RegisterProxy(mux *http.ServeMux, proxy http.Handler) {
	mux.Handle("/sandbox/", proxy)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *Handler) handleStart(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req runtime.StartRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		metrics.RequestsTotal.WithLabelValues("start", "error").Inc()
		return
	}

	if req.SessionID == "" {
		h.writeError(w, http.StatusBadRequest, "session_id is required")
		metrics.RequestsTotal.WithLabelValues("start", "error").Inc()
		return
	}

	if req.ResourceFactor == 0 {
		req.ResourceFactor = 1
	}

	rt, err := h.backend.Start(r.Context(), req)
	if err != nil {
		h.handleBackendError(w, "start", err)
		return
	}

	metrics.RequestsTotal.WithLabelValues("start", "success").Inc()
	metrics.RequestDuration.WithLabelValues("start").Observe(time.Since(start).Seconds())
	metrics.StartDuration.Observe(time.Since(start).Seconds())

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(rt)
}

func (h *Handler) handleStop(w http.ResponseWriter, r *http.Request) {
	var req runtime.StopRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RuntimeID == "" {
		h.writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}

	if err := h.backend.Stop(r.Context(), req.RuntimeID); err != nil {
		h.handleBackendError(w, "stop", err)
		return
	}

	metrics.RequestsTotal.WithLabelValues("stop", "success").Inc()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

func (h *Handler) handlePause(w http.ResponseWriter, r *http.Request) {
	var req runtime.PauseRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RuntimeID == "" {
		h.writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}

	if err := h.backend.Pause(r.Context(), req.RuntimeID); err != nil {
		h.handleBackendError(w, "pause", err)
		return
	}

	metrics.RequestsTotal.WithLabelValues("pause", "success").Inc()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "paused"})
}

func (h *Handler) handleResume(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req runtime.ResumeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RuntimeID == "" {
		h.writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}

	rt, err := h.backend.Resume(r.Context(), req.RuntimeID)
	if err != nil {
		h.handleBackendError(w, "resume", err)
		return
	}

	metrics.RequestsTotal.WithLabelValues("resume", "success").Inc()
	metrics.RequestDuration.WithLabelValues("resume").Observe(time.Since(start).Seconds())

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rt)
}

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
	runtimes, err := h.backend.List(r.Context())
	if err != nil {
		h.handleBackendError(w, "list", err)
		return
	}
	if runtimes == nil {
		runtimes = []runtime.Runtime{}
	}

	metrics.RequestsTotal.WithLabelValues("list", "success").Inc()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(runtime.ListResponse{Runtimes: runtimes})
}

func (h *Handler) handleGetRuntime(w http.ResponseWriter, r *http.Request) {
	runtimeID := r.PathValue("runtime_id")
	if runtimeID == "" {
		h.writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}

	rt, err := h.backend.Get(r.Context(), runtimeID)
	if err != nil {
		h.handleBackendError(w, "get_runtime", err)
		return
	}

	metrics.RequestsTotal.WithLabelValues("get_runtime", "success").Inc()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rt)
}

func (h *Handler) handleGetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	if sessionID == "" {
		h.writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	rt, err := h.backend.GetBySession(r.Context(), sessionID)
	if err != nil {
		h.handleBackendError(w, "get_session", err)
		return
	}

	metrics.RequestsTotal.WithLabelValues("get_session", "success").Inc()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rt)
}

func (h *Handler) handleSessionsBatch(w http.ResponseWriter, r *http.Request) {
	var req runtime.BatchConversationsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.Sandboxes) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(runtime.ListResponse{Runtimes: []runtime.Runtime{}})
		return
	}

	// Collect unique session IDs
	sessionIDs := make(map[string]bool)
	for _, sb := range req.Sandboxes {
		if sb.SessionID != "" {
			sessionIDs[sb.SessionID] = true
		}
	}

	// Look up each session
	runtimes := make([]runtime.Runtime, 0)
	for sessionID := range sessionIDs {
		rt, err := h.backend.GetBySession(r.Context(), sessionID)
		if err != nil {
			continue // Skip unknown sessions
		}
		runtimes = append(runtimes, *rt)
	}

	metrics.RequestsTotal.WithLabelValues("sessions_batch", "success").Inc()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(runtime.ListResponse{Runtimes: runtimes})
}

func (h *Handler) handleSessionsBatchGet(w http.ResponseWriter, r *http.Request) {
	requestedIDs := r.URL.Query()["ids"]
	seen := make(map[string]struct{}, len(requestedIDs))
	sessionIDs := make([]string, 0, len(requestedIDs))
	for _, sessionID := range requestedIDs {
		if sessionID == "" {
			continue
		}
		if _, ok := seen[sessionID]; ok {
			continue
		}
		seen[sessionID] = struct{}{}
		sessionIDs = append(sessionIDs, sessionID)
	}

	runtimes := make([]runtime.Runtime, 0, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		rt, err := h.backend.GetBySession(r.Context(), sessionID)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				continue
			}
			h.handleBackendError(w, "sessions_batch", err)
			return
		}
		runtimes = append(runtimes, *rt)
	}

	metrics.RequestsTotal.WithLabelValues("sessions_batch", "success").Inc()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(runtimes)
}

func (h *Handler) handleRegistryPrefix(w http.ResponseWriter, r *http.Request) {
	metrics.RequestsTotal.WithLabelValues("registry_prefix", "success").Inc()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(runtime.RegistryPrefixResponse{
		RegistryPrefix: h.registryPrefix,
	})
}

func (h *Handler) handleImageExists(w http.ResponseWriter, r *http.Request) {
	image := r.URL.Query().Get("image")
	if image == "" {
		h.writeError(w, http.StatusBadRequest, "image query parameter is required")
		return
	}

	exists := h.knownImages[image]

	metrics.RequestsTotal.WithLabelValues("image_exists", "success").Inc()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(runtime.ImageExistsResponse{Exists: exists})
}

func (h *Handler) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-API-Key")
		if !h.apiKeyIsCorrect(apiKey) {
			h.writeError(w, http.StatusUnauthorized, "invalid or missing API key")
			return
		}
		next(w, r)
	}
}

func (h *Handler) apiKeyIsCorrect(provided string) bool {
	if h.apiKey == "" {
		return false
	}
	// Use crypto/subtle for constant-time comparison
	return subtle.ConstantTimeCompare([]byte(provided), []byte(h.apiKey)) == 1
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(runtime.ErrorResponse{
		Error:   http.StatusText(status),
		Message: message,
	})
}

func (h *Handler) handleBackendError(w http.ResponseWriter, operation string, err error) {
	status := http.StatusInternalServerError
	publicMsg := "internal server error"

	errStr := err.Error()
	switch {
	case strings.Contains(errStr, "not found"):
		status = http.StatusNotFound
		publicMsg = "resource not found"
	case strings.Contains(errStr, "already exists"):
		status = http.StatusConflict
		publicMsg = "resource already exists"
	case strings.Contains(errStr, "unsupported resource_factor"):
		status = http.StatusBadRequest
		publicMsg = errStr // Safe to expose validation errors
	case strings.Contains(errStr, "does not match"):
		status = http.StatusConflict
		publicMsg = "image mismatch with configured profile"
	case strings.Contains(errStr, "runtime_class"):
		status = http.StatusConflict
		publicMsg = "runtime class conflict with cluster policy"
	default:
		slog.Error("backend error", "operation", operation, "error", err)
	}

	metrics.RequestsTotal.WithLabelValues(operation, "error").Inc()
	metrics.ErrorsTotal.WithLabelValues(operation, errorClass(err)).Inc()

	h.writeError(w, status, publicMsg)
}

func errorClass(err error) string {
	e := err.Error()
	switch {
	case strings.Contains(e, "not found"):
		return "not_found"
	case strings.Contains(e, "already exists"):
		return "conflict"
	case strings.Contains(e, "unsupported") || strings.Contains(e, "does not match"):
		return "bad_request"
	default:
		return "internal"
	}
}
