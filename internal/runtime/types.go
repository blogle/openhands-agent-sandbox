// Package runtime defines the core types and backend interface for the
// OpenHands Remote Runtime adapter.
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// RuntimeStatus represents the high-level status of a runtime.
type RuntimeStatus string

const (
	StatusRunning  RuntimeStatus = "running"
	StatusPaused   RuntimeStatus = "paused"
	StatusStarting RuntimeStatus = "starting"
	StatusStopping RuntimeStatus = "stopping"
	StatusFailed   RuntimeStatus = "error"
)

// PodStatus represents the observed pod status.
type PodStatus string

const (
	PodStatusReady    PodStatus = "ready"
	PodStatusRunning  PodStatus = "running"
	PodStatusPending  PodStatus = "pending"
	PodStatusFailed   PodStatus = "failed"
	PodStatusNotFound PodStatus = "not found"
)

// StartRequest is the payload sent by OpenHands APIRemoteWorkspace.
type StartRequest struct {
	Image           string            `json:"image"`
	Command         FlexibleCommand   `json:"command"`
	WorkingDir      string            `json:"working_dir"`
	Environment     map[string]string `json:"environment"`
	SessionID       string            `json:"session_id"`
	RunAsUser       *int64            `json:"run_as_user,omitempty"`
	FSGroup         *int64            `json:"fs_group,omitempty"`
	ImagePullPolicy string            `json:"image_pull_policy,omitempty"`
	RuntimeClass    string            `json:"runtime_class,omitempty"`
	ResourceFactor  int               `json:"resource_factor,omitempty"`
}

// FlexibleCommand accepts command as either a JSON string or array of strings.
type FlexibleCommand []string

// UnmarshalJSON implements custom JSON unmarshaling for FlexibleCommand.
func (fc *FlexibleCommand) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*fc = FlexibleCommand{s}
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*fc = FlexibleCommand(arr)
		return nil
	}
	return fmt.Errorf("command must be a string or array of strings")
}

// MarshalJSON implements custom JSON marshaling for FlexibleCommand.
func (fc FlexibleCommand) MarshalJSON() ([]byte, error) {
	if len(fc) == 1 {
		return json.Marshal(fc[0])
	}
	return json.Marshal([]string(fc))
}

// StopRequest is the payload for stopping a runtime.
type StopRequest struct {
	RuntimeID string `json:"runtime_id"`
}

// PauseRequest is the payload for pausing a runtime.
type PauseRequest struct {
	RuntimeID string `json:"runtime_id"`
}

// ResumeRequest is the payload for resuming a runtime.
type ResumeRequest struct {
	RuntimeID string `json:"runtime_id"`
}

// Runtime is the canonical runtime representation returned to OpenHands.
type Runtime struct {
	RuntimeID     string         `json:"runtime_id"`
	SessionID     string         `json:"session_id"`
	URL           string         `json:"url"`
	SessionAPIKey string         `json:"session_api_key,omitempty"`
	Status        RuntimeStatus  `json:"status"`
	PodStatus     PodStatus      `json:"pod_status"`
	WorkHosts     map[string]int `json:"work_hosts,omitempty"`
}

// ListResponse is the response for GET /list.
type ListResponse struct {
	Runtimes []Runtime `json:"runtimes"`
}

// BatchConversationsRequest is the request for POST /sessions/batch.
type BatchConversationsRequest struct {
	Sandboxes map[string]BatchConversationSandbox `json:"sandboxes"`
}

// BatchConversationSandbox holds session data for a batch lookup.
type BatchConversationSandbox struct {
	SessionID       string   `json:"session_id"`
	ConversationIDs []string `json:"conversation_ids"`
}

// RegistryPrefixResponse is the response for GET /registry_prefix.
type RegistryPrefixResponse struct {
	RegistryPrefix string `json:"registry_prefix"`
}

// ImageExistsResponse is the response for GET /image_exists.
type ImageExistsResponse struct {
	Exists bool `json:"exists"`
}

// ErrorResponse is a standard error response.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// RuntimeBackend is the interface that concrete backend implementations
// (such as the agent-sandbox backend) must satisfy.
type RuntimeBackend interface {
	// Start creates a new runtime. Must be idempotent for an existing session.
	Start(ctx context.Context, req StartRequest) (*Runtime, error)

	// Stop deletes the runtime and associated resources.
	Stop(ctx context.Context, runtimeID string) error

	// Pause suspends the runtime, preserving state.
	Pause(ctx context.Context, runtimeID string) error

	// Resume restores a paused runtime.
	Resume(ctx context.Context, runtimeID string) (*Runtime, error)

	// Get retrieves a runtime by its ID.
	Get(ctx context.Context, runtimeID string) (*Runtime, error)

	// GetBySession retrieves a runtime by OpenHands session ID.
	GetBySession(ctx context.Context, sessionID string) (*Runtime, error)

	// List returns all known runtimes.
	List(ctx context.Context) ([]Runtime, error)
}

// WaitForReady polls until a condition is met or the context is cancelled.
func WaitForReady(ctx context.Context, check func(ctx context.Context) (bool, error), interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			ready, err := check(ctx)
			if err != nil {
				return err
			}
			if ready {
				return nil
			}
		}
	}
}
