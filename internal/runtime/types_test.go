package runtime

import (
	"encoding/json"
	"testing"
)

func TestFlexibleCommand_UnmarshalJSON_String(t *testing.T) {
	var fc FlexibleCommand
	if err := json.Unmarshal([]byte(`"echo hello"`), &fc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fc) != 1 || fc[0] != "echo hello" {
		t.Fatalf("expected [\"echo hello\"], got %v", fc)
	}
}

func TestFlexibleCommand_UnmarshalJSON_Array(t *testing.T) {
	var fc FlexibleCommand
	if err := json.Unmarshal([]byte(`["echo","hello"]`), &fc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fc) != 2 || fc[0] != "echo" || fc[1] != "hello" {
		t.Fatalf("expected [echo hello], got %v", fc)
	}
}

func TestFlexibleCommand_MarshalJSON_SingleElement(t *testing.T) {
	fc := FlexibleCommand{"echo hello"}
	data, err := json.Marshal(fc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != `"echo hello"` {
		t.Fatalf("expected \"echo hello\", got %s", data)
	}
}

func TestFlexibleCommand_MarshalJSON_MultipleElements(t *testing.T) {
	fc := FlexibleCommand{"echo", "hello"}
	data, err := json.Marshal(fc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(arr) != 2 || arr[0] != "echo" || arr[1] != "hello" {
		t.Fatalf("expected [echo hello], got %v", arr)
	}
}

func TestStartRequest_Unmarshal(t *testing.T) {
	input := `{
		"image": "ghcr.io/openhands/agent-server:latest",
		"command": "/usr/local/bin/openhands-agent-server --port 60000",
		"working_dir": "/",
		"environment": {"KEY": "value"},
		"session_id": "test-session",
		"run_as_user": 10001,
		"fs_group": 10001,
		"image_pull_policy": "IfNotPresent",
		"runtime_class": "sysbox-runc",
		"resource_factor": 2
	}`

	var req StartRequest
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Image != "ghcr.io/openhands/agent-server:latest" {
		t.Errorf("image: got %q", req.Image)
	}
	if req.SessionID != "test-session" {
		t.Errorf("session_id: got %q", req.SessionID)
	}
	if req.WorkingDir != "/" {
		t.Errorf("working_dir: got %q", req.WorkingDir)
	}
	if req.Environment["KEY"] != "value" {
		t.Errorf("environment: got %v", req.Environment)
	}
	if req.RunAsUser == nil || *req.RunAsUser != 10001 {
		t.Errorf("run_as_user: got %v", req.RunAsUser)
	}
	if req.ResourceFactor != 2 {
		t.Errorf("resource_factor: got %d", req.ResourceFactor)
	}
	if len(req.Command) != 1 {
		t.Errorf("command: expected 1 element, got %d", len(req.Command))
	}
}

func TestRuntime_JSON(t *testing.T) {
	rt := Runtime{
		RuntimeID: "abc-123",
		SessionID: "sess-456",
		URL:       "https://example.com/sandbox/abc-123",
		Status:    StatusRunning,
		PodStatus: PodStatusReady,
	}

	data, err := json.Marshal(rt)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var roundTrip Runtime
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if roundTrip.RuntimeID != rt.RuntimeID {
		t.Errorf("runtime_id mismatch")
	}
	if roundTrip.Status != StatusRunning {
		t.Errorf("status mismatch")
	}
}

func TestRuntimeFailedStatus_JSON(t *testing.T) {
	data, err := json.Marshal(Runtime{Status: StatusFailed})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if string(data) != `{"runtime_id":"","session_id":"","url":"","status":"error","pod_status":""}` {
		t.Fatalf("expected failed status to serialize as error, got %s", data)
	}
}

func TestErrorResponse_JSON(t *testing.T) {
	errResp := ErrorResponse{
		Error:   "Bad Request",
		Message: "session_id is required",
	}

	data, err := json.Marshal(errResp)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var roundTrip ErrorResponse
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if roundTrip.Error != "Bad Request" || roundTrip.Message != "session_id is required" {
		t.Errorf("unexpected: %+v", roundTrip)
	}
}
