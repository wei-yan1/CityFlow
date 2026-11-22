package xxljob

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	defaultExecutorPort   = "9999"
	defaultTimeoutSeconds = 3
)

// Settings is the common executor configuration every service embeds in its
// own config. It deliberately mirrors the deployment fields rather than the
// SDK option struct so services never need to know how the executor is built.
type Settings struct {
	Enabled        bool   `json:",default=false"`
	ServerAddr     string `json:",optional"`
	AccessToken    string `json:",optional"`
	RegistryKey    string `json:",optional"`
	ExecutorIP     string `json:",optional"`
	ExecutorPort   string `json:",default=9999"`
	TimeoutSeconds int    `json:",default=3"`
	// EnvPrefix scopes environment overrides to one service. Task keeps the
	// unscoped XXL_JOB_* keys for compatibility; services with their own
	// executor use PAYMENT_XXL_JOB_* etc. so a single host can run several
	// executor groups without sharing RegistryKey or port settings.
	EnvPrefix string `json:"-"`
}

// WithEnv applies environment overrides. An optional scope (for example
// "PAYMENT") reads PAYMENT_XXL_JOB_* first; without a scope it keeps the
// original global XXL_JOB_* keys. The scope is retained so New can re-apply
// the same keys without falling back to the global ones.
func (s Settings) WithEnv(scope ...string) Settings {
	prefix := s.EnvPrefix
	if len(scope) > 0 {
		prefix = normalizeScope(scope[0])
	}
	s.EnvPrefix = prefix
	s.Enabled = envBool(envKey(prefix, "XXL_JOB_ENABLED"), s.Enabled)
	s.ServerAddr = envString(envKey(prefix, "XXL_JOB_SERVER_ADDR"), s.ServerAddr)
	s.AccessToken = envString(envKey(prefix, "XXL_JOB_ACCESS_TOKEN"), s.AccessToken)
	s.RegistryKey = envString(envKey(prefix, "XXL_JOB_REGISTRY_KEY"), s.RegistryKey)
	s.ExecutorIP = envString(envKey(prefix, "XXL_JOB_EXECUTOR_IP"), s.ExecutorIP)
	s.ExecutorPort = envString(envKey(prefix, "XXL_JOB_EXECUTOR_PORT"), s.ExecutorPort)
	if value, ok := envInt(envKey(prefix, "XXL_JOB_TIMEOUT_SECONDS")); ok {
		s.TimeoutSeconds = value
	}

	s.ServerAddr = strings.TrimSpace(s.ServerAddr)
	s.AccessToken = strings.TrimSpace(s.AccessToken)
	s.RegistryKey = strings.TrimSpace(s.RegistryKey)
	s.ExecutorIP = strings.TrimSpace(s.ExecutorIP)
	s.ExecutorPort = strings.TrimSpace(s.ExecutorPort)
	if s.ExecutorPort == "" {
		s.ExecutorPort = defaultExecutorPort
	}
	if s.TimeoutSeconds <= 0 {
		s.TimeoutSeconds = defaultTimeoutSeconds
	}
	return s
}

// Validate rejects only configurations that would make an enabled executor
// unusable. A disabled service is always valid and starts no listener.
func (s Settings) Validate() error {
	if !s.Enabled {
		return nil
	}
	if s.ServerAddr == "" {
		return errors.New("XXL-JOB server address is required when the executor is enabled")
	}
	if !strings.HasPrefix(s.ServerAddr, "http://") && !strings.HasPrefix(s.ServerAddr, "https://") {
		return errors.New("XXL-JOB server address must start with http:// or https://")
	}
	if s.RegistryKey == "" {
		return errors.New("XXL-JOB registry key is required when the executor is enabled")
	}
	value, err := strconv.Atoi(s.ExecutorPort)
	if err != nil || value <= 0 || value > 65535 {
		return fmt.Errorf("XXL-JOB executor port %q is invalid", s.ExecutorPort)
	}
	if s.TimeoutSeconds <= 0 {
		return errors.New("XXL-JOB timeout must be greater than zero")
	}
	if s.TimeoutSeconds > 60 {
		return errors.New("XXL-JOB timeout must be at most 60 seconds")
	}
	return nil
}

func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func normalizeScope(scope string) string {
	scope = strings.ToUpper(strings.TrimSpace(scope))
	scope = strings.TrimSuffix(scope, "_")
	if scope == "" {
		return ""
	}
	return scope + "_"
}

func envKey(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + name
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(name string) (int, bool) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return parsed, true
}
