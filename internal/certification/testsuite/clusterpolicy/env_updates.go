package clusterpolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/NVIDIA/gpu-operator-self-certification/internal/certification"
	"github.com/NVIDIA/gpu-operator-self-certification/internal/certification/k8s"
)

const (
	testEnvName  = "MY_TEST_ENV_NAME"
	testEnvValue = "test"
)

type EnvUpdates struct{}

func (t *EnvUpdates) Name() string {
	return "clusterpolicy-env-updates"
}

func (t *EnvUpdates) Run(ctx context.Context, clients *k8s.Clients, cfg certification.Config) *certification.TestResult {
	result := certification.NewTestResult(t.Name())
	defer result.Finalize()

	timeout := cfg.ParseTimeout()

	// Step 1: Add empty env array to devicePlugin
	result.AddDetail("adding env array to spec.devicePlugin.env")
	if err := t.addEmptyEnvArray(ctx, clients); err != nil {
		result.Fail("failed to add empty env array: %v", err)
		return result
	}

	// Step 2: Add test env var
	result.AddDetail("adding env var %s=%s to device-plugin", testEnvName, testEnvValue)
	if err := t.addEnvVar(ctx, clients, testEnvName, testEnvValue); err != nil {
		result.Fail("failed to add env var: %v", err)
		return result
	}

	// Step 3: Wait for operator to apply changes
	result.AddDetail("waiting for operator to apply changes")
	select {
	case <-time.After(10 * time.Second):
	case <-ctx.Done():
		result.Fail("context cancelled while waiting for changes")
		return result
	}

	// Step 4: Verify env is applied to device-plugin daemonset
	result.AddDetail("verifying env var is applied to device-plugin daemonset")
	envVars, err := clients.GetDaemonSetContainerEnv(ctx, cfg.Namespace, DevicePluginAppLabel)
	if err != nil {
		result.Fail("failed to get daemonset env vars: %v", err)
		return result
	}

	actualValue := k8s.GetEnvValue(envVars, testEnvName)
	if actualValue != testEnvValue {
		result.Fail("env var %s not found or has wrong value: expected %q, got %q", testEnvName, testEnvValue, actualValue)
		return result
	}
	result.AddDetail("env var %s=%s verified on daemonset", testEnvName, actualValue)

	// Step 5: Verify device-plugin pod is ready
	result.AddDetail("verifying device-plugin pod is ready")
	if err := clients.WaitForPodReady(ctx, cfg.Namespace, DevicePluginAppLabel, timeout); err != nil {
		result.Fail("device-plugin pod not ready: %v", err)
		return result
	}
	result.AddDetail("device-plugin pod ready after env update")

	return result
}

func (t *EnvUpdates) addEmptyEnvArray(ctx context.Context, clients *k8s.Clients) error {
	patch := []byte(`[{"op": "add", "path": "/spec/devicePlugin/env", "value": []}]`)
	return clients.ModifyClusterPolicy(ctx, patch)
}

func (t *EnvUpdates) addEnvVar(ctx context.Context, clients *k8s.Clients, name, value string) error {
	envVar := map[string]string{"name": name, "value": value}
	envVarJSON, err := json.Marshal(envVar)
	if err != nil {
		return fmt.Errorf("marshaling env var: %w", err)
	}
	patch := []byte(`[{"op": "add", "path": "/spec/devicePlugin/env/0", "value": ` + string(envVarJSON) + `}]`)
	return clients.ModifyClusterPolicy(ctx, patch)
}
