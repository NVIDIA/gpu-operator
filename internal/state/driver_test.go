/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package state

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/render"
	"github.com/NVIDIA/gpu-operator/internal/utils"
)

const (
	manifestDir       = "../../manifests"
	manifestResultDir = "./testdata/golden"
)

func getYAMLString(objs []*unstructured.Unstructured) (string, error) {
	s := json.NewSerializerWithOptions(json.DefaultMetaFactory, scheme.Scheme,
		scheme.Scheme, json.SerializerOptions{Yaml: true, Pretty: false, Strict: false})
	var sb strings.Builder
	for _, obj := range objs {
		var b bytes.Buffer
		err := s.Encode(obj, &b)
		if err != nil {
			return "", err
		}
		sb.WriteString(b.String())
		sb.WriteString("---\n")
	}
	return sb.String(), nil
}

func TestDriverRenderMinimal(t *testing.T) {
	// Construct a sample driver state manager
	const (
		testName = "driver-minimal"
	)

	state, err := NewStateDriver(nil, nil, manifestDir)
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	renderData := getMinimalDriverRenderData()

	objs, err := stateDriver.renderer.RenderObjects(
		&render.TemplatingData{
			Data: renderData,
		})
	require.Nil(t, err)
	require.NotEmpty(t, objs)

	actual, err := getYAMLString(objs)
	require.Nil(t, err)

	o, err := os.ReadFile(filepath.Join(manifestResultDir, testName+".yaml"))
	require.Nil(t, err)

	require.Equal(t, string(o), actual)
}

func TestDriverRenderRDMA(t *testing.T) {
	// Construct a sample driver state manager
	const (
		testName = "driver-rdma"
	)

	state, err := NewStateDriver(nil, nil, manifestDir)
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	renderData := getMinimalDriverRenderData()

	renderData.AdditionalConfigs = getSampleAdditionalConfigs()

	renderData.GPUDirectRDMA = &nvidiav1alpha1.GPUDirectRDMASpec{
		Enabled: utils.BoolPtr(true),
	}

	objs, err := stateDriver.renderer.RenderObjects(
		&render.TemplatingData{
			Data: renderData,
		})
	require.Nil(t, err)
	require.NotEmpty(t, objs)

	actual, err := getYAMLString(objs)
	require.Nil(t, err)

	o, err := os.ReadFile(filepath.Join(manifestResultDir, testName+".yaml"))
	require.Nil(t, err)

	require.Equal(t, string(o), actual)
}

func TestDriverRDMAHostMOFED(t *testing.T) {
	const (
		testName = "driver-rdma-hostmofed"
	)
	state, err := NewStateDriver(nil, nil, manifestDir)
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	renderData := getMinimalDriverRenderData()

	renderData.AdditionalConfigs = getSampleAdditionalConfigs()

	renderData.GPUDirectRDMA = &nvidiav1alpha1.GPUDirectRDMASpec{
		Enabled:      utils.BoolPtr(true),
		UseHostMOFED: utils.BoolPtr(true),
	}

	objs, err := stateDriver.renderer.RenderObjects(
		&render.TemplatingData{
			Data: renderData,
		})
	require.Nil(t, err)
	require.NotEmpty(t, objs)

	actual, err := getYAMLString(objs)
	require.Nil(t, err)

	o, err := os.ReadFile(filepath.Join(manifestResultDir, testName+".yaml"))
	require.Nil(t, err)

	require.Equal(t, string(o), actual)
}

func TestDriverSpec(t *testing.T) {
	const (
		testName = "driver-full-spec"
	)
	state, err := NewStateDriver(nil, nil, manifestDir)
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	// set every field in driverSpec
	driverSpec := &nvidiav1alpha1.NVIDIADriverSpec{
		Manager: nvidiav1alpha1.DriverManagerSpec{
			Repository:       "/path/to/repository",
			Image:            "image",
			Version:          "version",
			ImagePullPolicy:  "Always",
			ImagePullSecrets: []string{"manager-secret"},
			Env: []nvidiav1alpha1.EnvVar{
				{Name: "FOO", Value: "foo"},
				{Name: "BAR", Value: "bar"},
			},
		},
		StartupProbe:     getDefaultContainerProbeSpec(),
		LivenessProbe:    getDefaultContainerProbeSpec(),
		ReadinessProbe:   getDefaultContainerProbeSpec(),
		UsePrecompiled:   new(bool),
		ImagePullPolicy:  "Always",
		ImagePullSecrets: []string{"secret-a", "secret-b"},
		Resources: &nvidiav1alpha1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"memory": resource.MustParse("200Mi"),
				"cpu":    resource.MustParse("500m"),
			},
		},
		Args: []string{"--foo", "--bar"},
		Env: []nvidiav1alpha1.EnvVar{
			{Name: "FOO", Value: "foo"},
			{Name: "BAR", Value: "bar"},
		},
		NodeSelector: map[string]string{
			"example.com/foo": "foo",
			"example.com/bar": "bar",
		},
		Labels: map[string]string{
			"custom-label-1": "custom-value-1",
			"custom-label-2": "custom-value-2",
			// The below standard labels should not be overridden in the
			// DaemonSet that gets rendered
			"app":                       "foo",
			"app.kubernetes.io/part-of": "foo",
		},
		Annotations: map[string]string{
			"custom-annotation-1": "custom-value-1",
			"custom-annotation-2": "custom-value-2",
		},
		Tolerations: []corev1.Toleration{
			{
				Key:      "foo",
				Operator: "Equal",
				Value:    "bar",
				Effect:   "NoSchedule",
			},
		},
		PriorityClassName: "custom-priority-class-name",
		KernelModuleType:  "open",
	}

	driverSpec.Labels = sanitizeDriverLabels(driverSpec.Labels)

	renderData := getMinimalDriverRenderData()
	renderData.Driver.Spec = driverSpec

	objs, err := stateDriver.renderer.RenderObjects(
		&render.TemplatingData{
			Data: renderData,
		})
	require.Nil(t, err)

	actual, err := getYAMLString(objs)
	require.Nil(t, err)

	o, err := os.ReadFile(filepath.Join(manifestResultDir, testName+".yaml"))
	require.Nil(t, err)

	require.Equal(t, string(o), actual)
}

func TestDriverGDS(t *testing.T) {
	const (
		testName = "driver-gds"
	)

	state, err := NewStateDriver(nil, nil, manifestDir)
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	renderData := getMinimalDriverRenderData()

	renderData.AdditionalConfigs = getSampleAdditionalConfigs()

	renderData.GDS = &gdsDriverSpec{
		ImagePath: "nvcr.io/nvidia/cloud-native/nvidia-fs:2.16.1",
		Spec: &nvidiav1alpha1.GPUDirectStorageSpec{
			Enabled:          utils.BoolPtr(true),
			ImagePullSecrets: []string{"ngc-secrets"},
		},
	}

	objs, err := stateDriver.renderer.RenderObjects(
		&render.TemplatingData{
			Data: renderData,
		})
	require.Nil(t, err)
	require.NotEmpty(t, objs)

	actual, err := getYAMLString(objs)
	require.Nil(t, err)

	o, err := os.ReadFile(filepath.Join(manifestResultDir, testName+".yaml"))
	require.Nil(t, err)

	require.Equal(t, string(o), actual)
}

func TestDriverGDRCopy(t *testing.T) {
	const (
		testName = "driver-gdrcopy"
	)

	state, err := NewStateDriver(nil, nil, manifestDir)
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	renderData := getMinimalDriverRenderData()

	renderData.AdditionalConfigs = getSampleAdditionalConfigs()

	renderData.GDRCopy = &gdrcopyDriverSpec{
		ImagePath: "nvcr.io/nvidia/cloud-native/gdrdrv:v2.4.1",
		Spec: &nvidiav1alpha1.GDRCopySpec{
			Enabled:          utils.BoolPtr(true),
			ImagePullSecrets: []string{"ngc-secrets"},
		},
	}

	objs, err := stateDriver.renderer.RenderObjects(
		&render.TemplatingData{
			Data: renderData,
		})
	require.Nil(t, err)
	require.NotEmpty(t, objs)

	actual, err := getYAMLString(objs)
	require.Nil(t, err)

	o, err := os.ReadFile(filepath.Join(manifestResultDir, testName+".yaml"))
	require.Nil(t, err)

	require.Equal(t, string(o), actual)
}

func TestDriverGDRCopyOpenShift(t *testing.T) {
	const (
		testName     = "driver-gdrcopy-openshift"
		rhcosVersion = "413.92.202304252344-0"
		toolkitImage = "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:7fecaebc1d51b28bc3548171907e4d91823a031d7a6a694ab686999be2b4d867"
	)

	state, err := NewStateDriver(nil, nil, manifestDir)
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	renderData := getMinimalDriverRenderData()
	renderData.AdditionalConfigs = getSampleAdditionalConfigs()
	renderData.Driver.Name = "nvidia-gpu-driver-openshift"
	renderData.Driver.AppName = "nvidia-gpu-driver-openshift-79d6bd954f"
	renderData.Driver.ImagePath = "nvcr.io/nvidia/driver:525.85.03-rhel8.0"
	renderData.Driver.OSVersion = "rhel8.0"
	renderData.Openshift = &openshiftSpec{
		ToolkitImage: toolkitImage,
		RHCOSVersion: rhcosVersion,
	}
	renderData.Runtime.OpenshiftDriverToolkitEnabled = true
	renderData.Runtime.OpenshiftVersion = "4.13"
	renderData.Runtime.OpenshiftProxySpec = &configv1.ProxySpec{
		HTTPProxy:  "http://user:pass@example:8080",
		HTTPSProxy: "https://user:pass@example:8085",
		NoProxy:    "internal.example.com",
		TrustedCA: configv1.ConfigMapNameReference{
			Name: "gpu-operator-trusted-ca",
		},
	}

	renderData.GDRCopy = &gdrcopyDriverSpec{
		ImagePath: "nvcr.io/nvidia/cloud-native/gdrdrv:v2.4.1-rhcos4.13",
		Spec: &nvidiav1alpha1.GDRCopySpec{
			Enabled:          utils.BoolPtr(true),
			ImagePullSecrets: []string{"ngc-secret"},
		},
	}

	objs, err := stateDriver.renderer.RenderObjects(
		&render.TemplatingData{
			Data: renderData,
		})
	require.Nil(t, err)
	require.NotEmpty(t, objs)

	actual, err := getYAMLString(objs)
	require.Nil(t, err)

	o, err := os.ReadFile(filepath.Join(manifestResultDir, testName+".yaml"))
	require.Nil(t, err)

	require.Equal(t, string(o), actual)
}

func TestDriverAdditionalConfigs(t *testing.T) {
	const (
		testName = "driver-additional-configs"
	)

	state, err := NewStateDriver(nil, nil, manifestDir)
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	renderData := getMinimalDriverRenderData()

	renderData.AdditionalConfigs = getSampleAdditionalConfigs()

	objs, err := stateDriver.renderer.RenderObjects(
		&render.TemplatingData{
			Data: renderData,
		})
	require.Nil(t, err)

	actual, err := getYAMLString(objs)
	require.Nil(t, err)

	o, err := os.ReadFile(filepath.Join(manifestResultDir, testName+".yaml"))
	require.Nil(t, err)

	require.Equal(t, string(o), actual)
}

func TestDriverOpenshiftDriverToolkit(t *testing.T) {
	const (
		testName     = "driver-openshift-drivertoolkit"
		rhcosVersion = "413.92.202304252344-0"
		toolkitImage = "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:7fecaebc1d51b28bc3548171907e4d91823a031d7a6a694ab686999be2b4d867"
	)

	state, err := NewStateDriver(nil, nil, manifestDir)
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	renderData := getMinimalDriverRenderData()
	renderData.Driver.Name = "nvidia-gpu-driver-openshift"
	renderData.Driver.AppName = "nvidia-gpu-driver-openshift-79d6bd954f"
	renderData.Driver.ImagePath = "nvcr.io/nvidia/driver:525.85.03-rhel8.0"
	renderData.Driver.OSVersion = "rhel8.0"
	renderData.Openshift = &openshiftSpec{
		ToolkitImage: toolkitImage,
		RHCOSVersion: rhcosVersion,
	}
	renderData.Runtime.OpenshiftDriverToolkitEnabled = true
	renderData.Runtime.OpenshiftVersion = "4.13"
	renderData.Runtime.OpenshiftProxySpec = &configv1.ProxySpec{
		HTTPProxy:  "http://user:pass@example:8080",
		HTTPSProxy: "https://user:pass@example:8085",
		NoProxy:    "internal.example.com",
		TrustedCA: configv1.ConfigMapNameReference{
			Name: "gpu-operator-trusted-ca",
		},
	}

	objs, err := stateDriver.renderer.RenderObjects(
		&render.TemplatingData{
			Data: renderData,
		})
	require.Nil(t, err)

	actual, err := getYAMLString(objs)
	require.Nil(t, err)

	o, err := os.ReadFile(filepath.Join(manifestResultDir, testName+".yaml"))
	require.Nil(t, err)

	require.Equal(t, string(o), actual)
}

func TestDriverPrecompiled(t *testing.T) {
	const (
		testName = "driver-precompiled"
	)

	state, err := NewStateDriver(nil, nil, manifestDir)
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	renderData := getMinimalDriverRenderData()
	renderData.Driver.Spec.UsePrecompiled = utils.BoolPtr(true)
	renderData.Driver.Name = "nvidia-gpu-driver-ubuntu22.04"
	renderData.Driver.AppName = "nvidia-gpu-driver-ubuntu22.04-646cdfdb96"
	renderData.Driver.ImagePath = "nvcr.io/nvidia/driver:535-5.4.0-150-generic-ubuntu22.04"
	renderData.Precompiled = &precompiledSpec{
		KernelVersion:          "5.4.0-150-generic",
		SanitizedKernelVersion: "5.4.0-150-generic",
	}

	objs, err := stateDriver.renderer.RenderObjects(
		&render.TemplatingData{
			Data: renderData,
		})
	require.Nil(t, err)

	actual, err := getYAMLString(objs)
	require.Nil(t, err)

	o, err := os.ReadFile(filepath.Join(manifestResultDir, testName+".yaml"))
	require.Nil(t, err)

	require.Equal(t, string(o), actual)
}

func TestGetDriverAppName(t *testing.T) {
	cr := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{
			UID: apitypes.UID("bfac7359-6033-45ce-88d6-53db0078526e"),
		},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			DriverType: nvidiav1alpha1.GPU,
		},
	}

	pool := nodePool{
		osRelease: "ubuntu",
		osVersion: "20.04",
	}

	actual := getDriverAppName(cr, pool)
	expected := "nvidia-gpu-driver-ubuntu20.04-67cc6dbb79"
	assert.Equal(t, expected, actual)

	// Modify nodePool to include kernelVersion
	pool.kernel = "5.15.0-70-generic"

	actual = getDriverAppName(cr, pool)
	expected = "nvidia-gpu-driver-ubuntu20.04-59b779bcc5"
	assert.Equal(t, expected, actual)

	// Now set the osVersion to a really long string
	pool.osRelease = "redhatCoreOS"
	pool.osVersion = "4.14-414.92.202309282257"

	actual = getDriverAppName(cr, pool)
	expected = "nvidia-gpu-driver-redhatCoreOS4.14-414.92.2023092822-59b779bcc5"
	assert.Equal(t, expected, actual)
	assert.Equal(t, 63, len(actual))
}

func TestGetDriverAppNameRHCOS(t *testing.T) {
	cr := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{
			UID: apitypes.UID("d5b3a1f2-38a9-4b72-bff1-21fd569fd305"),
		},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			DriverType: nvidiav1alpha1.GPU,
		},
	}

	pool := nodePool{
		osRelease:    "rhcos",
		osVersion:    "4.14",
		rhcosVersion: "414.92.202309282257",
	}

	actual := getDriverAppName(cr, pool)
	expected := "nvidia-gpu-driver-rhcos4.14-6f4fc4fc6"
	assert.Equal(t, expected, actual)
}

func TestVGPUHostManagerDaemonset(t *testing.T) {
	const (
		testName = "driver-vgpu-host-manager"
	)
	state, err := NewStateDriver(nil, nil, manifestDir)
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	renderData := getMinimalDriverRenderData()
	renderData.Driver.Spec.DriverType = nvidiav1alpha1.VGPUHostManager
	renderData.Driver.Name = "nvidia-vgpu-manager-ubuntu22.04"
	renderData.Driver.AppName = "nvidia-vgpu-manager-ubuntu22.04-7c6d7bd86b"
	renderData.Driver.ImagePath = "nvcr.io/nvidia/vgpu-manager:525.85.03-ubuntu22.04"

	objs, err := stateDriver.renderer.RenderObjects(
		&render.TemplatingData{
			Data: renderData,
		})
	require.Nil(t, err)

	actual, err := getYAMLString(objs)
	require.Nil(t, err)

	o, err := os.ReadFile(filepath.Join(manifestResultDir, testName+".yaml"))
	require.Nil(t, err)

	require.Equal(t, string(o), actual)
}

func TestVGPUHostManagerDaemonsetOpenShift(t *testing.T) {
	const (
		testName     = "driver-vgpu-host-manager-openshift"
		rhcosVersion = "413.92.202304252344-0"
		toolkitImage = "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:7fecaebc1d51b28bc3548171907e4d91823a031d7a6a694ab686999be2b4d867"
	)
	state, err := NewStateDriver(nil, nil, manifestDir)
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	renderData := getMinimalDriverRenderData()
	renderData.Driver.Spec.DriverType = nvidiav1alpha1.VGPUHostManager
	renderData.Driver.Name = "nvidia-vgpu-manager-openshift"
	renderData.Driver.AppName = "nvidia-vgpu-manager-openshift-7c6d7bd86b"
	renderData.Driver.ImagePath = "nvcr.io/nvidia/vgpu-manager:525.85.03-rhel8.0"
	renderData.Driver.OSVersion = "rhel8.0"
	renderData.Openshift = &openshiftSpec{
		ToolkitImage: toolkitImage,
		RHCOSVersion: rhcosVersion,
	}
	renderData.Runtime.OpenshiftDriverToolkitEnabled = true
	renderData.Runtime.OpenshiftVersion = "4.13"

	objs, err := stateDriver.renderer.RenderObjects(
		&render.TemplatingData{
			Data: renderData,
		})
	require.Nil(t, err)

	actual, err := getYAMLString(objs)
	require.Nil(t, err)

	o, err := os.ReadFile(filepath.Join(manifestResultDir, testName+".yaml"))
	require.Nil(t, err)

	require.Equal(t, string(o), actual)
}

func getMinimalDriverRenderData() *driverRenderData {
	return &driverRenderData{
		Driver: &driverSpec{
			Spec: &nvidiav1alpha1.NVIDIADriverSpec{
				StartupProbe:   getDefaultContainerProbeSpec(),
				LivenessProbe:  getDefaultContainerProbeSpec(),
				ReadinessProbe: getDefaultContainerProbeSpec(),
				DriverType:     nvidiav1alpha1.GPU,
			},
			AppName:          "nvidia-gpu-driver-ubuntu22.04-7c6d7bd86b",
			Name:             "nvidia-gpu-driver-ubuntu22.04",
			ImagePath:        "nvcr.io/nvidia/driver:525.85.03-ubuntu22.04",
			ManagerImagePath: "nvcr.io/nvidia/cloud-native/k8s-driver-manager:devel",
			OSVersion:        "ubuntu22.04",
		},
		Runtime: &driverRuntimeSpec{
			Namespace:         "test-operator",
			KubernetesVersion: "1.28.0",
		},
		HostRoot: "",
	}
}

func getDefaultContainerProbeSpec() *nvidiav1alpha1.ContainerProbeSpec {
	return &nvidiav1alpha1.ContainerProbeSpec{
		InitialDelaySeconds: 60,
		TimeoutSeconds:      60,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		FailureThreshold:    120,
	}
}

func getSampleAdditionalConfigs() *additionalConfigs {
	return &additionalConfigs{
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "test-cm",
				ReadOnly:  true,
				MountPath: "/opt/config/test-file",
				SubPath:   "test-file",
			},
			{
				Name:      "test-host-path",
				MountPath: "/opt/config/test-host-path",
			},
			{
				Name:      "test-host-path-ro",
				MountPath: "/opt/config/test-host-path-ro",
				ReadOnly:  true,
			},
		},
		Volumes: []corev1.Volume{
			{
				Name: "test-cm",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-cm",
						},
						Items: []corev1.KeyToPath{
							{
								Key:  "test-file",
								Path: "test-file",
							},
						},
					},
				},
			},
			{
				Name: "test-host-path",
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: "/opt/config/test-host-path",
						Type: newHostPathType(corev1.HostPathDirectoryOrCreate),
					},
				},
			},
			{
				Name: "test-host-path-ro",
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: "/opt/config/test-host-path-ro",
						Type: newHostPathType(corev1.HostPathDirectoryOrCreate),
					},
				},
			},
		},
	}
}

func TestDriverVGPULicensing(t *testing.T) {
	const (
		testName = "driver-vgpu-licensing"
	)

	state, err := NewStateDriver(nil, nil, manifestDir)
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	renderData := getMinimalDriverRenderData()

	renderData.AdditionalConfigs = &additionalConfigs{
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "licensing-config",
				MountPath: "/drivers/gridd.conf",
				SubPath:   "gridd.conf",
			},
			{
				Name:      "licensing-config",
				MountPath: "/drivers/ClientConfigToken/client_configuration_token.tok",
				SubPath:   "client_configuration_token.tok",
			},
		},
		Volumes: []corev1.Volume{
			{
				Name: "licensing-config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "licensing-config-configmap",
						},
						Items: []corev1.KeyToPath{
							{
								Key:  "gridd.conf",
								Path: "gridd.conf",
							},
							{
								Key:  "client_configuration_token.tok",
								Path: "client_configuration_token.tok",
							},
						},
					},
				},
			},
		},
	}

	objs, err := stateDriver.renderer.RenderObjects(
		&render.TemplatingData{
			Data: renderData,
		})
	require.Nil(t, err)

	actual, err := getYAMLString(objs)
	require.Nil(t, err)

	o, err := os.ReadFile(filepath.Join(manifestResultDir, testName+".yaml"))
	require.Nil(t, err)

	require.Equal(t, string(o), actual)

}

func TestGetSanitizedKernelVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"5.14.0-427.37.1.el9_4.aarch64_64k", "5.14.0-427.37.1.el9.4"},
		{"5.14.0-427.37.1.el9_4.aarch64", "5.14.0-427.37.1.el9.4"},
		{"5.14.0-427.37.1.el9_4.x86_64_64k", "5.14.0-427.37.1.el9.4"},
		{"5.14.0-427.37.1.el9_4.x86_64", "5.14.0-427.37.1.el9.4"},
	}

	for _, test := range tests {
		result := getSanitizedKernelVersion(test.input)
		require.NotEmpty(t, result)
		require.Equal(t, test.expected, result)
	}
}
