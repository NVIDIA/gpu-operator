package environment

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"

	"github.com/NVIDIA/gpu-operator-self-certification/internal/certification/k8s"
)

type policyCollector struct{}

func (c *policyCollector) Name() string { return "policy" }

func (c *policyCollector) Collect(ctx context.Context, clients *k8s.Clients, namespace string, s *EnvironmentSnapshot) error {
	cp, err := clients.GetClusterPolicy(ctx)
	if err != nil {
		return nil
	}

	s.Policy.Name = cp.Name
	s.Policy.State = string(cp.Status.State)

	specBytes, err := json.Marshal(cp.Spec)
	if err == nil {
		s.Policy.SpecHash = fmt.Sprintf("%x", sha256.Sum256(specBytes))
		s.Policy.RawSpec = specBytes
	}

	s.Policy.Config = parseConfig(&cp.Spec)
	s.Policy.Conditions = convertConditions(cp.Status.Conditions)

	return nil
}

func parseConfig(spec *nvidiav1.ClusterPolicySpec) PolicyConfig {
	cfg := PolicyConfig{}

	// Driver
	cfg.DriverEnabled = spec.Driver.IsEnabled()
	cfg.UseNvidiaDriverCRD = spec.Driver.UseNvidiaDriverCRDType()
	cfg.DriverVersion = spec.Driver.Version
	cfg.DriverRepository = spec.Driver.Repository
	cfg.DriverImage = spec.Driver.Image
	cfg.UsePrecompiledDrivers = spec.Driver.UsePrecompiledDrivers()
	cfg.UseOpenKernelModules = spec.Driver.OpenKernelModulesEnabled()

	// Driver install dir from manager env
	if spec.Driver.Manager.Env != nil {
		for _, env := range spec.Driver.Manager.Env {
			if env.Name == "DRIVER_ROOT" {
				cfg.DriverInstallDir = env.Value
				break
			}
		}
	}

	// Licensing
	if spec.Driver.LicensingConfig != nil && spec.Driver.LicensingConfig.SecretName != "" {
		cfg.LicensingConfigured = true
	}

	// Fabric Manager
	if spec.Driver.GPUDirectRDMA != nil && spec.Driver.GPUDirectRDMA.IsEnabled() {
		cfg.FabricManagerEnabled = true
	}

	// Toolkit
	if spec.Toolkit.IsEnabled() {
		cfg.ToolkitEnabled = true
	}
	cfg.ToolkitInstallDir = spec.Toolkit.InstallDir

	// Core operands
	if spec.DevicePlugin.IsEnabled() {
		cfg.DevicePluginEnabled = true
	}
	if spec.DCGMExporter.IsEnabled() {
		cfg.DCGMExporterEnabled = true
	}
	if spec.DCGM.IsEnabled() {
		cfg.DCGMEnabled = true
	}
	if spec.GPUFeatureDiscovery.IsEnabled() {
		cfg.GFDEnabled = true
	}
	if spec.MIGManager.IsEnabled() {
		cfg.MIGManagerEnabled = true
	}
	// Validator is always enabled (no Enabled field in spec)
	cfg.ValidatorEnabled = true

	// Optional features
	if spec.CDI.IsEnabled() {
		cfg.CDIEnabled = true
	}
	if spec.GPUDirectStorage != nil && spec.GPUDirectStorage.IsEnabled() {
		cfg.GDSEnabled = true
	}
	if spec.VFIOManager.IsEnabled() {
		cfg.VFIOManagerEnabled = true
	}
	if spec.VGPUManager.IsEnabled() {
		cfg.VGPUManagerEnabled = true
	}
	if spec.CCManager.IsEnabled() {
		cfg.CCManagerEnabled = true
	}

	// Sandbox workloads
	if spec.SandboxWorkloads.IsEnabled() {
		cfg.SandboxWorkloadsEnabled = true
		cfg.SandboxDefaultWorkload = string(spec.SandboxWorkloads.DefaultWorkload)
	}

	// Sharing configs
	if spec.DevicePlugin.Config != nil {
		cfg.TimeSlicingConfigName = spec.DevicePlugin.Config.Name
	}
	if spec.MIGManager.Config != nil {
		cfg.MIGConfigName = spec.MIGManager.Config.Name
	}

	return cfg
}

func convertConditions(conditions []metav1.Condition) []PolicyCondition {
	if len(conditions) == 0 {
		return nil
	}
	result := make([]PolicyCondition, len(conditions))
	for i, cond := range conditions {
		result[i] = PolicyCondition{
			Type:    cond.Type,
			Status:  string(cond.Status),
			Reason:  cond.Reason,
			Message: cond.Message,
		}
	}
	return result
}
