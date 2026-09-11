// Package config handles environment-based configuration for the runtime adapter.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds all configuration for the runtime adapter.
type Config struct {
	ListenAddr string
	APIKey     string

	Namespace     string
	PublicBaseURL string

	RegistryPrefix      string
	OpenHandsServerImage string

	AgentServerPort int

	ResourceFactor1Pool string
	ResourceFactor2Pool string
	ResourceFactor4Pool string
	ResourceFactor8Pool string

	RuntimeClassPolicy string

	StartTimeout        time.Duration
	SuspendTimeout      time.Duration
	ResumeTimeout       time.Duration
	AgentServerInitTimeout time.Duration

	OpenHandsBootstrapSecret string

	LogLevel  string
	LogFormat string
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:         envOrDefault("LISTEN_ADDR", ":8080"),
		APIKey:             os.Getenv("API_KEY"),
		Namespace:          envOrDefault("NAMESPACE", "openhands-sandboxes"),
		PublicBaseURL:      os.Getenv("PUBLIC_BASE_URL"),
		RegistryPrefix:     envOrDefault("REGISTRY_PREFIX", "ghcr.io/openhands"),
		OpenHandsServerImage: os.Getenv("OPENHANDS_SERVER_IMAGE"),
		AgentServerPort:    envIntOrDefault("AGENT_SERVER_PORT", 60000),
		ResourceFactor1Pool: envOrDefault("RESOURCE_FACTOR_1_POOL", "openhands-default"),
		ResourceFactor2Pool: os.Getenv("RESOURCE_FACTOR_2_POOL"),
		ResourceFactor4Pool: os.Getenv("RESOURCE_FACTOR_4_POOL"),
		ResourceFactor8Pool: os.Getenv("RESOURCE_FACTOR_8_POOL"),
		RuntimeClassPolicy: envOrDefault("RUNTIME_CLASS_POLICY", "ignore"),
		StartTimeout:       envDurationOrDefault("START_TIMEOUT", 180*time.Second),
		SuspendTimeout:     envDurationOrDefault("SUSPEND_TIMEOUT", 120*time.Second),
		ResumeTimeout:      envDurationOrDefault("RESUME_TIMEOUT", 180*time.Second),
		AgentServerInitTimeout: envDurationOrDefault("AGENT_SERVER_INIT_TIMEOUT", 120*time.Second),
		OpenHandsBootstrapSecret: os.Getenv("OPENHANDS_BOOTSTRAP_SECRET"),
		LogLevel:           envOrDefault("LOG_LEVEL", "info"),
		LogFormat:          envOrDefault("LOG_FORMAT", "json"),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Normalize PublicBaseURL
	cfg.PublicBaseURL = strings.TrimRight(cfg.PublicBaseURL, "/")

	return cfg, nil
}

// Validate checks that required configuration is present and valid.
func (c *Config) Validate() error {
	if c.APIKey == "" {
		return fmt.Errorf("API_KEY is required")
	}
	if c.PublicBaseURL == "" {
		return fmt.Errorf("PUBLIC_BASE_URL is required")
	}
	if c.OpenHandsServerImage == "" {
		return fmt.Errorf("OPENHANDS_SERVER_IMAGE is required")
	}
	if c.OpenHandsBootstrapSecret == "" {
		return fmt.Errorf("OPENHANDS_BOOTSTRAP_SECRET is required")
	}
	if c.RuntimeClassPolicy != "ignore" && c.RuntimeClassPolicy != "require-match" {
		return fmt.Errorf("RUNTIME_CLASS_POLICY must be 'ignore' or 'require-match', got %q", c.RuntimeClassPolicy)
	}
	return nil
}

// PoolForResourceFactor returns the warm pool name for the given resource factor.
func (c *Config) PoolForResourceFactor(factor int) (string, bool) {
	switch factor {
	case 0, 1:
		return c.ResourceFactor1Pool, c.ResourceFactor1Pool != ""
	case 2:
		return c.ResourceFactor2Pool, c.ResourceFactor2Pool != ""
	case 4:
		return c.ResourceFactor4Pool, c.ResourceFactor4Pool != ""
	case 8:
		return c.ResourceFactor8Pool, c.ResourceFactor8Pool != ""
	default:
		return "", false
	}
}

// KnownImages returns the set of images that /image_exists should report as known.
func (c *Config) KnownImages() map[string]bool {
	return map[string]bool{
		c.OpenHandsServerImage: true,
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envIntOrDefault(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}

func envDurationOrDefault(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
