//go:build live

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	agentsclientset "sigs.k8s.io/agent-sandbox/clients/k8s/clientset/versioned"
	extensionsclientset "sigs.k8s.io/agent-sandbox/clients/k8s/extensions/clientset/versioned"
	extv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
)

type runtimeResponse struct {
	RuntimeID     string `json:"runtime_id"`
	SessionID     string `json:"session_id"`
	URL           string `json:"url"`
	SessionAPIKey string `json:"session_api_key"`
	Status        string `json:"status"`
	PodStatus     string `json:"pod_status"`
}

type liveConfig struct {
	apiURL      string
	proxyBase   string
	apiKey      string
	serverImage string
	namespace   string
}

func TestLiveLifecycle(t *testing.T) {
	cfg := requireLiveConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()

	coreClient, agentsClient, extensionsClient := kubeClients(t)
	beforePVCs := pvcNames(t, ctx, coreClient, cfg.namespace)

	sessionID := fmt.Sprintf("e2e-%d", time.Now().UnixNano())
	startPayload := map[string]any{
		"image":             cfg.serverImage,
		"command":           "/usr/local/bin/openhands-agent-server --port 60000",
		"working_dir":       "/",
		"environment":       map[string]string{"OPENHANDS_RUNTIME_E2E": sessionID},
		"session_id":        sessionID,
		"run_as_user":       10001,
		"fs_group":          10001,
		"image_pull_policy": "IfNotPresent",
		"resource_factor":   1,
	}

	var runtime runtimeResponse
	managementRequest(t, ctx, cfg, http.MethodPost, "/start", startPayload, &runtime)
	if runtime.RuntimeID == "" || runtime.SessionAPIKey == "" || runtime.Status != "running" || runtime.PodStatus != "ready" {
		t.Fatalf("invalid start response: %+v", runtime)
	}
	if runtime.SessionID != sessionID {
		t.Fatalf("start returned session %q, want %q", runtime.SessionID, sessionID)
	}

	defer func() {
		var ignored map[string]any
		managementRequest(t, context.Background(), cfg, http.MethodPost, "/stop", map[string]string{"runtime_id": runtime.RuntimeID}, &ignored)
		waitForClaimDeleted(t, context.Background(), extensionsClient, cfg.namespace, runtime.RuntimeID)
	}()

	claim := waitForClaim(t, ctx, extensionsClient, cfg.namespace, runtime.RuntimeID)
	if claim.Status.SandboxStatus.Name == "" {
		t.Fatal("ready claim has no bound sandbox")
	}

	assertProxyReady(t, ctx, cfg, runtime)
	podName := sandboxPodName(t, ctx, coreClient, agentsClient, cfg.namespace, claim.Status.SandboxStatus.Name)
	sentinel := "/workspace/openhands-agent-sandbox-e2e"
	kubectlExec(t, ctx, cfg.namespace, podName, "printf %s "+shellQuote(sessionID)+" > "+shellQuote(sentinel))

	createdPVCs := newPVCNames(t, ctx, coreClient, cfg.namespace, beforePVCs)
	if len(createdPVCs) == 0 {
		t.Fatal("start did not create a PVC for the sandbox workspace")
	}

	var pauseResponse map[string]string
	managementRequest(t, ctx, cfg, http.MethodPost, "/pause", map[string]string{"runtime_id": runtime.RuntimeID}, &pauseResponse)
	if pauseResponse["status"] != "paused" {
		t.Fatalf("unexpected pause response: %+v", pauseResponse)
	}

	pausedClaim := waitForPausedClaim(t, ctx, extensionsClient, cfg.namespace, runtime.RuntimeID)
	if pausedClaim.Status.SandboxStatus.Name != claim.Status.SandboxStatus.Name {
		t.Fatalf("pause changed sandbox identity from %q to %q", claim.Status.SandboxStatus.Name, pausedClaim.Status.SandboxStatus.Name)
	}
	for pvc := range createdPVCs {
		if _, err := coreClient.CoreV1().PersistentVolumeClaims(cfg.namespace).Get(ctx, pvc, metav1.GetOptions{}); err != nil {
			t.Fatalf("workspace PVC %q missing after pause: %v", pvc, err)
		}
	}

	var resumed runtimeResponse
	managementRequest(t, ctx, cfg, http.MethodPost, "/resume", map[string]string{"runtime_id": runtime.RuntimeID}, &resumed)
	if resumed.Status != "running" || resumed.PodStatus != "ready" {
		t.Fatalf("unexpected resume response: %+v", resumed)
	}
	assertProxyReady(t, ctx, cfg, resumed)

	claim = waitForClaim(t, ctx, extensionsClient, cfg.namespace, runtime.RuntimeID)
	podName = sandboxPodName(t, ctx, coreClient, agentsClient, cfg.namespace, claim.Status.SandboxStatus.Name)
	output := kubectlExec(t, ctx, cfg.namespace, podName, "cat "+shellQuote(sentinel))
	if strings.TrimSpace(output) != sessionID {
		t.Fatalf("workspace sentinel lost after resume: got %q, want %q", output, sessionID)
	}
}

func requireLiveConfig(t *testing.T) liveConfig {
	t.Helper()
	if os.Getenv("RUN_LIVE_E2E") != "1" {
		t.Skip("set RUN_LIVE_E2E=1 to run live lifecycle tests")
	}
	cfg := liveConfig{
		apiURL:      strings.TrimRight(os.Getenv("RUNTIME_API_URL"), "/"),
		proxyBase:   strings.TrimRight(os.Getenv("RUNTIME_PROXY_URL"), "/"),
		apiKey:      os.Getenv("RUNTIME_API_KEY"),
		serverImage: os.Getenv("OPENHANDS_SERVER_IMAGE"),
		namespace:   os.Getenv("LIVE_TEST_NAMESPACE"),
	}
	if cfg.namespace == "" {
		cfg.namespace = "openhands-sandboxes"
	}
	for key, value := range map[string]string{
		"RUNTIME_API_URL":      cfg.apiURL,
		"RUNTIME_API_KEY":      cfg.apiKey,
		"OPENHANDS_SERVER_IMAGE": cfg.serverImage,
	} {
		if value == "" {
			t.Fatalf("%s is required for live tests", key)
		}
	}
	return cfg
}

func kubeClients(t *testing.T) (kubernetes.Interface, *agentsclientset.Clientset, *extensionsclientset.Clientset) {
	t.Helper()
	cfg, err := rest.InClusterConfig()
	if err != nil {
		cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			clientcmd.NewDefaultClientConfigLoadingRules(),
			&clientcmd.ConfigOverrides{},
		).ClientConfig()
	}
	if err != nil {
		t.Fatalf("load kubeconfig: %v", err)
	}
	coreClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("create core client: %v", err)
	}
	agentsClient, err := agentsclientset.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("create agents client: %v", err)
	}
	extensionsClient, err := extensionsclientset.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("create extensions client: %v", err)
	}
	return coreClient, agentsClient, extensionsClient
}

func managementRequest(t *testing.T, ctx context.Context, cfg liveConfig, method, path string, payload any, out any) {
	t.Helper()
	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal %s %s: %v", method, path, err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, cfg.apiURL+path, body)
	if err != nil {
		t.Fatalf("create %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", cfg.apiKey)
	resp, err := (&http.Client{Timeout: 6 * time.Minute}).Do(req)
	if err != nil {
		t.Fatalf("send %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		var responseBody bytes.Buffer
		_, _ = responseBody.ReadFrom(resp.Body)
		t.Fatalf("%s %s returned %d: %s", method, path, resp.StatusCode, responseBody.String())
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s %s: %v", method, path, err)
		}
	}
}

func assertProxyReady(t *testing.T, ctx context.Context, cfg liveConfig, runtime runtimeResponse) {
	t.Helper()
	base := runtime.URL
	if cfg.proxyBase != "" {
		base = cfg.proxyBase + "/sandbox/" + runtime.RuntimeID
	}
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
		if err != nil {
			t.Fatalf("create proxy health request: %v", err)
		}
		resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("runtime proxy never became healthy: %v", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

func waitForClaim(t *testing.T, ctx context.Context, client *extensionsclientset.Clientset, namespace, runtimeID string) *extv1beta1.SandboxClaim {
	t.Helper()
	for {
		claims, err := client.ExtensionsV1beta1().SandboxClaims(namespace).List(ctx, metav1.ListOptions{LabelSelector: "openhands.dev/runtime-id=" + runtimeID})
		if err == nil && len(claims.Items) == 1 && claimReady(&claims.Items[0]) {
			return &claims.Items[0]
		}
		select {
		case <-ctx.Done():
			t.Fatalf("claim %q did not become ready: %v", runtimeID, ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

func waitForPausedClaim(t *testing.T, ctx context.Context, client *extensionsclientset.Clientset, namespace, runtimeID string) *extv1beta1.SandboxClaim {
	t.Helper()
	for {
		claims, err := client.ExtensionsV1beta1().SandboxClaims(namespace).List(ctx, metav1.ListOptions{LabelSelector: "openhands.dev/runtime-id=" + runtimeID})
		if err == nil && len(claims.Items) == 1 && !claimReady(&claims.Items[0]) && len(claims.Items[0].Status.SandboxStatus.PodIPs) == 0 {
			return &claims.Items[0]
		}
		select {
		case <-ctx.Done():
			t.Fatalf("claim %q did not pause: %v", runtimeID, ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

func waitForClaimDeleted(t *testing.T, ctx context.Context, client *extensionsclientset.Clientset, namespace, runtimeID string) {
	t.Helper()
	deadline, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	for {
		claims, err := client.ExtensionsV1beta1().SandboxClaims(namespace).List(deadline, metav1.ListOptions{LabelSelector: "openhands.dev/runtime-id=" + runtimeID})
		if err == nil && len(claims.Items) == 0 {
			return
		}
		select {
		case <-deadline.Done():
			t.Fatalf("claim %q was not deleted: %v", runtimeID, deadline.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

func claimReady(claim *extv1beta1.SandboxClaim) bool {
	for _, condition := range claim.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func sandboxPodName(t *testing.T, ctx context.Context, coreClient kubernetes.Interface, agentsClient *agentsclientset.Clientset, namespace, sandboxName string) string {
	t.Helper()
	sandbox, err := agentsClient.AgentsV1beta1().Sandboxes(namespace).Get(ctx, sandboxName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get sandbox %q: %v", sandboxName, err)
	}
	pod, err := coreClient.CoreV1().Pods(namespace).Get(ctx, sandbox.Name, metav1.GetOptions{})
	if err == nil {
		return pod.Name
	}
	pods, listErr := coreClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: sandbox.Status.LabelSelector})
	if listErr != nil || len(pods.Items) != 1 {
		t.Fatalf("resolve pod for sandbox %q: get=%v list=%v pods=%d", sandboxName, err, listErr, len(pods.Items))
	}
	return pods.Items[0].Name
}

func pvcNames(t *testing.T, ctx context.Context, coreClient kubernetes.Interface, namespace string) map[string]struct{} {
	t.Helper()
	list, err := coreClient.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list PVCs: %v", err)
	}
	names := make(map[string]struct{}, len(list.Items))
	for _, pvc := range list.Items {
		names[pvc.Name] = struct{}{}
	}
	return names
}

func newPVCNames(t *testing.T, ctx context.Context, coreClient kubernetes.Interface, namespace string, before map[string]struct{}) map[string]struct{} {
	t.Helper()
	list, err := coreClient.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list PVCs: %v", err)
	}
	created := make(map[string]struct{})
	for _, pvc := range list.Items {
		if _, existed := before[pvc.Name]; !existed {
			created[pvc.Name] = struct{}{}
		}
	}
	return created
}

func kubectlExec(t *testing.T, ctx context.Context, namespace, podName, command string) string {
	t.Helper()
	cmd := exec.CommandContext(ctx, "kubectl", "exec", "-n", namespace, podName, "--", "sh", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exec in pod %q: %v: %s", podName, err, output)
	}
	return string(output)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
