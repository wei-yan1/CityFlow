package xxljob

import (
	"context"
	"strings"
	"testing"

	xxl "github.com/xxl-job/xxl-job-executor-go"
)

func TestSettingsValidateDisabledSkipsRequiredFields(t *testing.T) {
	if err := (Settings{}).WithEnv().Validate(); err != nil {
		t.Fatalf("disabled config should be valid: %v", err)
	}
}

func TestSettingsValidateEnabled(t *testing.T) {
	valid := Settings{
		Enabled: true, ServerAddr: "http://127.0.0.1:8080/xxl-job-admin",
		RegistryKey: "cityflow-task", ExecutorPort: "9999", TimeoutSeconds: 3,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	invalid := valid
	invalid.ExecutorPort = "not-a-port"
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid port should be rejected")
	}
}

func TestSettingsWithEnvOverridesAndDefaults(t *testing.T) {
	t.Setenv("XXL_JOB_ENABLED", "true")
	t.Setenv("XXL_JOB_SERVER_ADDR", "http://scheduler.example/xxl-job-admin")
	t.Setenv("XXL_JOB_REGISTRY_KEY", "cityflow-notification")
	t.Setenv("XXL_JOB_EXECUTOR_PORT", "10001")
	t.Setenv("XXL_JOB_TIMEOUT_SECONDS", "9")

	settings := (Settings{}).WithEnv()
	if !settings.Enabled || settings.ServerAddr != "http://scheduler.example/xxl-job-admin" ||
		settings.RegistryKey != "cityflow-notification" || settings.ExecutorPort != "10001" ||
		settings.TimeoutSeconds != 9 {
		t.Fatalf("environment overrides were not applied: %+v", settings)
	}

	for _, key := range []string{
		"XXL_JOB_ENABLED", "XXL_JOB_SERVER_ADDR", "XXL_JOB_REGISTRY_KEY",
		"XXL_JOB_EXECUTOR_PORT", "XXL_JOB_TIMEOUT_SECONDS",
	} {
		t.Setenv(key, "")
	}
	empty := (Settings{}).WithEnv()
	if empty.ExecutorPort != defaultExecutorPort || empty.TimeoutSeconds != defaultTimeoutSeconds {
		t.Fatalf("defaults were not applied: %+v", empty)
	}
}

func TestSettingsWithScopedEnv(t *testing.T) {
	t.Setenv("XXL_JOB_ENABLED", "false")
	t.Setenv("XXL_JOB_REGISTRY_KEY", "cityflow-task")
	t.Setenv("PAYMENT_XXL_JOB_ENABLED", "true")
	t.Setenv("PAYMENT_XXL_JOB_SERVER_ADDR", "http://scheduler.example/xxl-job-admin")
	t.Setenv("PAYMENT_XXL_JOB_REGISTRY_KEY", "cityflow-payment")
	t.Setenv("PAYMENT_XXL_JOB_EXECUTOR_PORT", "10000")

	settings := (Settings{}).WithEnv("payment")
	if !settings.Enabled || settings.RegistryKey != "cityflow-payment" ||
		settings.ExecutorPort != "10000" || settings.EnvPrefix != "PAYMENT_" {
		t.Fatalf("scoped environment overrides were not applied: %+v", settings)
	}

	// New re-applies environment values through NewRuntime. It must keep the
	// scoped keys instead of falling back to the global task group.
	reapplied := settings.WithEnv()
	if !reapplied.Enabled || reapplied.RegistryKey != "cityflow-payment" ||
		reapplied.ExecutorPort != "10000" {
		t.Fatalf("scope was lost on re-apply: %+v", reapplied)
	}
}

func TestNewDisabledReturnsNil(t *testing.T) {
	executor, err := New(Settings{}, nil)
	if err != nil || executor != nil {
		t.Fatalf("disabled executor should be nil: executor=%v err=%v", executor, err)
	}
}

func TestNewRuntimeDisabledReturnsNil(t *testing.T) {
	runtime, err := NewRuntime(Settings{}, nil)
	if err != nil || runtime != nil {
		t.Fatalf("disabled runtime should be nil: runtime=%v err=%v", runtime, err)
	}
}

func TestRuntimeRejectsNilHandler(t *testing.T) {
	runtime := &Runtime{}
	if err := runtime.Register("rank.build", nil); err == nil {
		t.Fatal("nil handler should be rejected")
	}
}

func TestRuntimeNilSafe(t *testing.T) {
	var runtime *Runtime
	if err := runtime.Register("rank.build", nil); err != nil {
		t.Fatalf("nil runtime register should be a no-op: %v", err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("nil runtime start should be a no-op: %v", err)
	}
	runtime.Stop()
}

func TestHandlerMapsErrorToPanic(t *testing.T) {
	handler := Handler(func(context.Context, *xxl.RunReq) (string, error) {
		return "", context.Canceled
	})
	defer func() {
		value := recover()
		if value == nil {
			t.Fatal("handler error should panic so the SDK reports failure")
		}
		if !strings.Contains(value.(error).Error(), "canceled") {
			t.Fatalf("unexpected panic: %v", value)
		}
	}()
	_ = handler(context.Background(), &xxl.RunReq{})
}
