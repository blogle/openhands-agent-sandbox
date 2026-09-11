// Package agentsandbox implements the Kubernetes agent-sandbox backend.
package agentsandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	sandboxv1beta1 "sigs.k8s.io/agent-sandbox/api/v1beta1"
	agentsclientset "sigs.k8s.io/agent-sandbox/clients/k8s/clientset/versioned"
	agentsv1beta1 "sigs.k8s.io/agent-sandbox/clients/k8s/clientset/versioned/typed/api/v1beta1"
	extensionsclientset "sigs.k8s.io/agent-sandbox/clients/k8s/extensions/clientset/versioned"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/clients/k8s/extensions/clientset/versioned/typed/api/v1beta1"
	extv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"

	"github.com/openhands-agent-sandbox/openhands-agent-sandbox/internal/config"
	"github.com/openhands-agent-sandbox/openhands-agent-sandbox/internal/identity"
	initclient "github.com/openhands-agent-sandbox/openhands-agent-sandbox/internal/init"
	"github.com/openhands-agent-sandbox/openhands-agent-sandbox/internal/runtime"
)

const (
	managedByLabel   = "app.kubernetes.io/managed-by"
	managedByValue   = "openhands-agent-sandbox-runtime"
	runtimeIDLabel   = "openhands.dev/runtime-id"
	sessionHashLabel = "openhands.dev/session-hash"
	sessionIDAnnot   = "openhands.dev/session-id"
	runtimeIDAnnot   = "openhands.dev/runtime-id"
	serverImageAnnot = "openhands.dev/server-image"
	createdByAnnot   = "openhands.dev/created-by"
	secretPrefix     = "openhands-runtime-"
)

// Backend implements runtime.RuntimeBackend using agent-sandbox.
type Backend struct {
	cfg          *config.Config
	agentsClient agentsv1beta1.AgentsV1beta1Interface
	extClient    extensionsv1beta1.ExtensionsV1beta1Interface
	coreClient   kubernetes.Interface
	initClient   *initclient.Client
	logger       *slog.Logger
}

// NewBackend creates a new agent-sandbox backend.
func NewBackend(cfg *config.Config, logger *slog.Logger) (*Backend, error) {
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		restConfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			clientcmd.NewDefaultClientConfigLoadingRules(),
			&clientcmd.ConfigOverrides{},
		).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
		}
	}

	httpClient, err := rest.HTTPClientFor(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	agentsCS, err := agentsclientset.NewForConfigAndClient(restConfig, httpClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create agents clientset: %w", err)
	}

	extensionsCS, err := extensionsclientset.NewForConfigAndClient(restConfig, httpClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create extensions clientset: %w", err)
	}

	coreCS, err := kubernetes.NewForConfigAndClient(restConfig, httpClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create core clientset: %w", err)
	}

	return &Backend{
		cfg:          cfg,
		agentsClient: agentsCS.AgentsV1beta1(),
		extClient:    extensionsCS.ExtensionsV1beta1(),
		coreClient:   coreCS,
		initClient:   initclient.NewClient(cfg.AgentServerInitTimeout),
		logger:       logger,
	}, nil
}

// NewBackendWithClients creates a backend with pre-constructed clients (for testing).
func NewBackendWithClients(
	cfg *config.Config,
	agentsClient agentsv1beta1.AgentsV1beta1Interface,
	extClient extensionsv1beta1.ExtensionsV1beta1Interface,
	coreClient kubernetes.Interface,
	logger *slog.Logger,
) *Backend {
	return &Backend{
		cfg:          cfg,
		agentsClient: agentsClient,
		extClient:    extClient,
		coreClient:   coreClient,
		initClient:   initclient.NewClient(cfg.AgentServerInitTimeout),
		logger:       logger,
	}
}

// Start creates a new runtime. Idempotent for existing sessions.
func (b *Backend) Start(ctx context.Context, req runtime.StartRequest) (*runtime.Runtime, error) {
	// Check for existing session — return all errors except typed not-found
	existing, err := b.GetBySession(ctx, req.SessionID)
	if err == nil && existing != nil {
		// Existing runtime found — return it (idempotent)
		b.logger.Info("session already exists",
			"session_id", req.SessionID,
			"runtime_id", existing.RuntimeID)
		return existing, nil
	}
	if err != nil && !isNotFoundError(err) {
		return nil, fmt.Errorf("failed to check existing session: %w", err)
	}

	// Validate resource factor
	poolName, ok := b.cfg.PoolForResourceFactor(req.ResourceFactor)
	if !ok {
		return nil, fmt.Errorf("unsupported resource_factor %d", req.ResourceFactor)
	}

	// Validate image matches profile
	if req.Image != b.cfg.OpenHandsServerImage {
		return nil, fmt.Errorf("requested image does not match configured profile image")
	}

	// Validate runtime class policy
	if req.RuntimeClass != "" && b.cfg.RuntimeClassPolicy == "require-match" {
		return nil, fmt.Errorf("runtime_class is controlled by the cluster profile")
	}

	// Generate identity — use deterministic claim name from session hash
	// to prevent duplicate claims for the same session
	runtimeID, err := identity.RuntimeID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate runtime ID: %w", err)
	}

	sessionAPIKey, err := identity.SessionAPIKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session API key: %w", err)
	}

	runtimeSecretKey, err := identity.RuntimeSecretKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate runtime secret key: %w", err)
	}

	// Use deterministic claim name from session hash to prevent duplicates
	sessionHash := identity.SessionHash(req.SessionID)
	claimName := "oh-" + sessionHash[:20]

	// Build init payload
	initPayload := InitPayload{
		SessionAPIKeys: []string{sessionAPIKey},
		SecretKey:      runtimeSecretKey,
		Env:            req.Environment,
	}

	// Create SandboxClaim — if AlreadyExists, another replica won the race
	claim := &extv1beta1.SandboxClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      claimName,
			Namespace: b.cfg.Namespace,
			Labels: map[string]string{
				managedByLabel:   managedByValue,
				runtimeIDLabel:   runtimeID,
				sessionHashLabel: sessionHash,
			},
			Annotations: map[string]string{
				sessionIDAnnot:   req.SessionID,
				runtimeIDAnnot:   runtimeID,
				serverImageAnnot: req.Image,
				createdByAnnot:   managedByValue,
			},
		},
		Spec: extv1beta1.SandboxClaimSpec{
			WarmPoolRef: extv1beta1.SandboxWarmPoolRef{
				Name: poolName,
			},
			Env: buildClaimEnv(req),
		},
	}

	createdClaim, err := b.extClient.SandboxClaims(b.cfg.Namespace).Create(ctx, claim, metav1.CreateOptions{})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			// Another replica created it — fetch and return
			return b.GetBySession(ctx, req.SessionID)
		}
		return nil, fmt.Errorf("failed to create SandboxClaim: %w", err)
	}

	b.logger.Info("SandboxClaim created",
		"claim_name", createdClaim.Name,
		"runtime_id", runtimeID,
		"session_hash", sessionHash,
		"warm_pool", poolName)

	// Create owned Secret
	if err := b.createRuntimeSecret(ctx, createdClaim, runtimeID, initPayload); err != nil {
		_ = b.extClient.SandboxClaims(b.cfg.Namespace).Delete(ctx, createdClaim.Name, metav1.DeleteOptions{})
		return nil, fmt.Errorf("failed to create runtime secret: %w", err)
	}

	// Wait for SandboxClaim Ready
	_, podIP, err := b.waitForClaimReady(ctx, createdClaim.Name)
	if err != nil {
		_ = b.extClient.SandboxClaims(b.cfg.Namespace).Delete(ctx, createdClaim.Name, metav1.DeleteOptions{})
		return nil, fmt.Errorf("SandboxClaim did not become ready: %w", err)
	}

	// Perform deferred init
	if err := b.initClient.Init(ctx, podIP, b.cfg.AgentServerPort, b.cfg.OpenHandsBootstrapSecret, initclient.InitRequest{
		SessionAPIKeys: []string{sessionAPIKey},
		SecretKey:      runtimeSecretKey,
		Env:            req.Environment,
	}); err != nil {
		_ = b.extClient.SandboxClaims(b.cfg.Namespace).Delete(ctx, createdClaim.Name, metav1.DeleteOptions{})
		return nil, fmt.Errorf("deferred init failed: %w", err)
	}

	// Wait for health
	if err := b.initClient.WaitForHealth(ctx, podIP, b.cfg.AgentServerPort, b.cfg.AgentServerInitTimeout); err != nil {
		_ = b.extClient.SandboxClaims(b.cfg.Namespace).Delete(ctx, createdClaim.Name, metav1.DeleteOptions{})
		return nil, fmt.Errorf("Agent Server health check failed: %w", err)
	}

	return &runtime.Runtime{
		RuntimeID:     runtimeID,
		SessionID:     req.SessionID,
		URL:           fmt.Sprintf("%s/sandbox/%s", b.cfg.PublicBaseURL, runtimeID),
		SessionAPIKey: sessionAPIKey,
		Status:        runtime.StatusRunning,
		PodStatus:     runtime.PodStatusReady,
		WorkHosts:     map[string]int{},
	}, nil
}

// findClaimByRuntimeID looks up a SandboxClaim by runtime ID label.
func (b *Backend) findClaimByRuntimeID(ctx context.Context, runtimeID string) (*extv1beta1.SandboxClaim, error) {
	claims, err := b.extClient.SandboxClaims(b.cfg.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s,%s=%s", managedByLabel, managedByValue, runtimeIDLabel, runtimeID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list SandboxClaims: %w", err)
	}
	if len(claims.Items) == 0 {
		return nil, fmt.Errorf("runtime not found: %s", runtimeID)
	}
	return &claims.Items[0], nil
}

// Stop deletes the SandboxClaim.
func (b *Backend) Stop(ctx context.Context, runtimeID string) error {
	claim, err := b.findClaimByRuntimeID(ctx, runtimeID)
	if err != nil {
		if isNotFoundError(err) {
			return nil // Already gone
		}
		return err
	}
	err = b.extClient.SandboxClaims(b.cfg.Namespace).Delete(ctx, claim.Name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete SandboxClaim: %w", err)
	}
	return nil
}

// Pause patches the Sandbox to Suspended and waits for suspension.
func (b *Backend) Pause(ctx context.Context, runtimeID string) error {
	claim, err := b.findClaimByRuntimeID(ctx, runtimeID)
	if err != nil {
		return err
	}

	sandboxName := claim.Status.SandboxStatus.Name
	if sandboxName == "" {
		return fmt.Errorf("SandboxClaim has no bound Sandbox")
	}

	// Patch to Suspended
	patch := fmt.Sprintf(`{"spec":{"operatingMode":"%s"}}`, sandboxv1beta1.SandboxOperatingModeSuspended)
	_, err = b.agentsClient.Sandboxes(b.cfg.Namespace).Patch(ctx, sandboxName,
		types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to patch Sandbox to Suspended: %w", err)
	}

	// Wait for suspension to be observed
	if err := b.waitForSuspension(ctx, sandboxName); err != nil {
		return fmt.Errorf("suspension not confirmed: %w", err)
	}

	b.logger.Info("runtime paused", "runtime_id", runtimeID, "sandbox_name", sandboxName)
	return nil
}

// Resume patches the Sandbox to Running and re-initializes.
func (b *Backend) Resume(ctx context.Context, runtimeID string) (*runtime.Runtime, error) {
	claim, err := b.findClaimByRuntimeID(ctx, runtimeID)
	if err != nil {
		return nil, err
	}

	sandboxName := claim.Status.SandboxStatus.Name
	if sandboxName == "" {
		return nil, fmt.Errorf("SandboxClaim has no bound Sandbox")
	}

	sandbox, err := b.agentsClient.Sandboxes(b.cfg.Namespace).Get(ctx, sandboxName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get Sandbox: %w", err)
	}

	// If already running and ready, check if init is needed
	if sandbox.Spec.OperatingMode == sandboxv1beta1.SandboxOperatingModeRunning && isSandboxReady(sandbox) {
		podIP := selectPodIP(sandbox.Status.PodIPs)
		if podIP != "" {
			// Check init state — if already ready, just return
			initStatus, initErr := b.initClient.GetStatus(ctx, podIP, b.cfg.AgentServerPort)
			if initErr == nil && initStatus.State == "ready" {
				return b.buildRuntimeResponse(ctx, runtimeID, claim.Annotations[sessionIDAnnot], podIP)
			}
			// Not initialized — fall through to init
			return b.doResumeInit(ctx, runtimeID, claim, podIP)
		}
	}

	// Patch to Running
	patch := fmt.Sprintf(`{"spec":{"operatingMode":"%s"}}`, sandboxv1beta1.SandboxOperatingModeRunning)
	_, err = b.agentsClient.Sandboxes(b.cfg.Namespace).Patch(ctx, sandboxName,
		types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to patch Sandbox to Running: %w", err)
	}

	// Wait for ready
	_, podIP, err := b.waitForSandboxReady(ctx, sandboxName)
	if err != nil {
		return nil, fmt.Errorf("Sandbox did not become ready after resume: %w", err)
	}

	return b.doResumeInit(ctx, runtimeID, claim, podIP)
}

// doResumeInit reloads credentials and performs deferred init.
func (b *Backend) doResumeInit(ctx context.Context, runtimeID string, claim *extv1beta1.SandboxClaim, podIP string) (*runtime.Runtime, error) {
	secretName := secretPrefix + runtimeID
	secret, err := b.coreClient.CoreV1().Secrets(b.cfg.Namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to reload runtime secret: %w", err)
	}

	payloadJSON, ok := secret.Data["init-payload.json"]
	if !ok {
		return nil, fmt.Errorf("runtime secret missing init-payload.json")
	}

	var initPayload InitPayload
	if err := json.Unmarshal(payloadJSON, &initPayload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal init payload: %w", err)
	}

	if len(initPayload.SessionAPIKeys) == 0 {
		return nil, fmt.Errorf("init payload has no session API keys")
	}

	if err := b.initClient.Init(ctx, podIP, b.cfg.AgentServerPort, b.cfg.OpenHandsBootstrapSecret, initclient.InitRequest{
		SessionAPIKeys: initPayload.SessionAPIKeys,
		SecretKey:      initPayload.SecretKey,
		Env:            initPayload.Env,
	}); err != nil {
		return nil, fmt.Errorf("deferred init failed on resume: %w", err)
	}

	if err := b.initClient.WaitForHealth(ctx, podIP, b.cfg.AgentServerPort, b.cfg.AgentServerInitTimeout); err != nil {
		return nil, fmt.Errorf("health check failed after resume: %w", err)
	}

	sessionID := claim.Annotations[sessionIDAnnot]
	return b.buildRuntimeResponse(ctx, runtimeID, sessionID, podIP)
}

// Get retrieves a runtime by ID.
func (b *Backend) Get(ctx context.Context, runtimeID string) (*runtime.Runtime, error) {
	claim, err := b.findClaimByRuntimeID(ctx, runtimeID)
	if err != nil {
		return nil, err
	}
	return b.runtimeFromClaim(ctx, claim)
}

// GetBySession retrieves a runtime by session ID.
func (b *Backend) GetBySession(ctx context.Context, sessionID string) (*runtime.Runtime, error) {
	sessionHash := identity.SessionHash(sessionID)
	claims, err := b.extClient.SandboxClaims(b.cfg.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s,%s=%s", managedByLabel, managedByValue, sessionHashLabel, sessionHash),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list SandboxClaims: %w", err)
	}

	for i := range claims.Items {
		if claims.Items[i].Annotations[sessionIDAnnot] == sessionID {
			return b.runtimeFromClaim(ctx, &claims.Items[i])
		}
	}
	return nil, fmt.Errorf("runtime not found for session: %s", sessionID)
}

// List returns all known runtimes.
func (b *Backend) List(ctx context.Context) ([]runtime.Runtime, error) {
	claims, err := b.extClient.SandboxClaims(b.cfg.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", managedByLabel, managedByValue),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list SandboxClaims: %w", err)
	}

	var runtimes []runtime.Runtime
	for i := range claims.Items {
		rt, err := b.runtimeFromClaim(ctx, &claims.Items[i])
		if err != nil {
			b.logger.Warn("skipping malformed claim", "error", err)
			continue
		}
		runtimes = append(runtimes, *rt)
	}
	return runtimes, nil
}

// ResolvePodIP resolves a runtime ID to pod IP and port (used by proxy).
func (b *Backend) ResolvePodIP(ctx context.Context, runtimeID string) (string, int, error) {
	claim, err := b.findClaimByRuntimeID(ctx, runtimeID)
	if err != nil {
		return "", 0, err
	}

	sandboxName := claim.Status.SandboxStatus.Name
	if sandboxName == "" {
		return "", 0, fmt.Errorf("runtime has no bound Sandbox")
	}

	sandbox, err := b.agentsClient.Sandboxes(b.cfg.Namespace).Get(ctx, sandboxName, metav1.GetOptions{})
	if err != nil {
		return "", 0, fmt.Errorf("failed to get Sandbox: %w", err)
	}

	// Only return IP if sandbox is running and ready
	if sandbox.Spec.OperatingMode != sandboxv1beta1.SandboxOperatingModeRunning {
		return "", 0, fmt.Errorf("sandbox is not running")
	}
	if !isSandboxReady(sandbox) {
		return "", 0, fmt.Errorf("sandbox is not ready")
	}

	podIP := selectPodIP(sandbox.Status.PodIPs)
	if podIP == "" {
		return "", 0, fmt.Errorf("sandbox has no pod IP")
	}
	return podIP, b.cfg.AgentServerPort, nil
}

// Helper methods

func (b *Backend) runtimeFromClaim(ctx context.Context, claim *extv1beta1.SandboxClaim) (*runtime.Runtime, error) {
	runtimeID := claim.Annotations[runtimeIDAnnot]
	sessionID := claim.Annotations[sessionIDAnnot]
	if runtimeID == "" || sessionID == "" {
		return nil, fmt.Errorf("claim %s missing required annotations", claim.Name)
	}

	status, podStatus := mapStatus(claim)

	var sessionAPIKey string
	if status == runtime.StatusRunning || status == runtime.StatusPaused {
		secretName := secretPrefix + runtimeID
		secret, err := b.coreClient.CoreV1().Secrets(b.cfg.Namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get runtime secret for %s: %w", runtimeID, err)
		}
		keyBytes, ok := secret.Data["session-api-key"]
		if !ok || len(keyBytes) == 0 {
			return nil, fmt.Errorf("runtime secret for %s missing session-api-key", runtimeID)
		}
		sessionAPIKey = string(keyBytes)
	}

	return &runtime.Runtime{
		RuntimeID:     runtimeID,
		SessionID:     sessionID,
		URL:           fmt.Sprintf("%s/sandbox/%s", b.cfg.PublicBaseURL, runtimeID),
		SessionAPIKey: sessionAPIKey,
		Status:        status,
		PodStatus:     podStatus,
		WorkHosts:     map[string]int{},
	}, nil
}

func mapStatus(claim *extv1beta1.SandboxClaim) (runtime.RuntimeStatus, runtime.PodStatus) {
	for _, cond := range claim.Status.Conditions {
		if cond.Type == "Ready" {
			switch cond.Status {
			case metav1.ConditionTrue:
				return runtime.StatusRunning, runtime.PodStatusReady
			case metav1.ConditionFalse:
				if cond.Reason == "SandboxSuspended" {
					return runtime.StatusPaused, runtime.PodStatusNotFound
				}
				if cond.Reason == "PodFailed" || cond.Reason == "SandboxExpired" {
					return runtime.StatusFailed, runtime.PodStatusFailed
				}
				return runtime.StatusStarting, runtime.PodStatusPending
			}
		}
	}
	if claim.Status.SandboxStatus.Name == "" {
		return runtime.StatusStarting, runtime.PodStatusPending
	}
	return runtime.StatusStarting, runtime.PodStatusPending
}

func (b *Backend) buildRuntimeResponse(ctx context.Context, runtimeID, sessionID, podIP string) (*runtime.Runtime, error) {
	secretName := secretPrefix + runtimeID
	secret, err := b.coreClient.CoreV1().Secrets(b.cfg.Namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get runtime secret: %w", err)
	}

	return &runtime.Runtime{
		RuntimeID:     runtimeID,
		SessionID:     sessionID,
		URL:           fmt.Sprintf("%s/sandbox/%s", b.cfg.PublicBaseURL, runtimeID),
		SessionAPIKey: string(secret.Data["session-api-key"]),
		Status:        runtime.StatusRunning,
		PodStatus:     runtime.PodStatusReady,
		WorkHosts:     map[string]int{},
	}, nil
}

func (b *Backend) waitForClaimReady(ctx context.Context, claimName string) (sandboxName, podIP string, err error) {
	ctx, cancel := context.WithTimeout(ctx, b.cfg.StartTimeout)
	defer cancel()

	for {
		claim, err := b.extClient.SandboxClaims(b.cfg.Namespace).Get(ctx, claimName, metav1.GetOptions{})
		if err != nil {
			return "", "", err
		}

		if claim.Status.SandboxStatus.Name != "" {
			sandboxName = claim.Status.SandboxStatus.Name
			for _, cond := range claim.Status.Conditions {
				if cond.Type == "Ready" && cond.Status == metav1.ConditionTrue {
					sandbox, err := b.agentsClient.Sandboxes(b.cfg.Namespace).Get(ctx, sandboxName, metav1.GetOptions{})
					if err != nil {
						return "", "", err
					}
					ip := selectPodIP(sandbox.Status.PodIPs)
					if ip != "" {
						return sandboxName, ip, nil
					}
				}
			}
		}

		select {
		case <-ctx.Done():
			return "", "", fmt.Errorf("timeout waiting for claim ready: %w", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

func (b *Backend) waitForSandboxReady(ctx context.Context, sandboxName string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, b.cfg.ResumeTimeout)
	defer cancel()

	for {
		sandbox, err := b.agentsClient.Sandboxes(b.cfg.Namespace).Get(ctx, sandboxName, metav1.GetOptions{})
		if err != nil {
			return "", "", err
		}
		if isSandboxReady(sandbox) {
			podIP := selectPodIP(sandbox.Status.PodIPs)
			if podIP != "" {
				return sandboxName, podIP, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", "", fmt.Errorf("timeout waiting for sandbox ready: %w", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

func (b *Backend) waitForSuspension(ctx context.Context, sandboxName string) error {
	ctx, cancel := context.WithTimeout(ctx, b.cfg.SuspendTimeout)
	defer cancel()

	for {
		sandbox, err := b.agentsClient.Sandboxes(b.cfg.Namespace).Get(ctx, sandboxName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		// Check Suspended condition
		for _, cond := range sandbox.Status.Conditions {
			if cond.Type == string(sandboxv1beta1.SandboxConditionSuspended) {
				if cond.Status == metav1.ConditionTrue {
					return nil // Suspension complete
				}
			}
		}

		// Also check if operatingMode is Suspended and pod is gone
		if sandbox.Spec.OperatingMode == sandboxv1beta1.SandboxOperatingModeSuspended {
			if len(sandbox.Status.PodIPs) == 0 {
				return nil // Pod terminated
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for suspension: %w", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

func (b *Backend) createRuntimeSecret(ctx context.Context, claim *extv1beta1.SandboxClaim, runtimeID string, payload InitPayload) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal init payload: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretPrefix + runtimeID,
			Namespace: b.cfg.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "extensions.agents.x-k8s.io/v1beta1",
					Kind:       "SandboxClaim",
					Name:       claim.Name,
					UID:        claim.UID,
					Controller: boolPtr(true),
				},
			},
		},
		Data: map[string][]byte{
			"session-api-key":    []byte(payload.SessionAPIKeys[0]),
			"runtime-secret-key": []byte(payload.SecretKey),
			"init-payload.json":  payloadJSON,
		},
	}

	_, err = b.coreClient.CoreV1().Secrets(b.cfg.Namespace).Create(ctx, secret, metav1.CreateOptions{})
	return err
}

func buildClaimEnv(req runtime.StartRequest) []extv1beta1.EnvVar {
	var envVars []extv1beta1.EnvVar
	for k, v := range req.Environment {
		envVars = append(envVars, extv1beta1.EnvVar{Name: k, Value: v})
	}
	return envVars
}

func isSandboxReady(sandbox *sandboxv1beta1.Sandbox) bool {
	for _, cond := range sandbox.Status.Conditions {
		if cond.Type == string(sandboxv1beta1.SandboxConditionReady) && cond.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func selectPodIP(ips []string) string {
	if len(ips) > 0 {
		return ips[0]
	}
	return ""
}

// InitPayload is stored in the Kubernetes Secret for resume.
type InitPayload struct {
	SessionAPIKeys []string          `json:"session_api_keys"`
	SecretKey      string            `json:"secret_key"`
	Env            map[string]string `json:"env,omitempty"`
}

func boolPtr(b bool) *bool { return &b }

// isNotFoundError checks if an error represents a "not found" condition.
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	e := err.Error()
	return contains(e, "not found")
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Ensure Backend implements runtime.RuntimeBackend.
var _ runtime.RuntimeBackend = (*Backend)(nil)
