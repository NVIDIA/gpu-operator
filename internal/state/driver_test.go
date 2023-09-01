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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes/scheme"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/render"
	"github.com/NVIDIA/gpu-operator/internal/utils"
)

const (
	manifestResultDir = "./golden"
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

	state, err := NewStateDriver(nil, nil, "./testdata")
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	renderData := getMinimalDriverRenderData()

	objs, err := stateDriver.renderer.RenderObjects(
		&render.TemplatingData{
			Data: renderData,
		})
	require.NotEmpty(t, objs)
	require.Nil(t, err)

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

	state, err := NewStateDriver(nil, nil, "./testdata")
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	renderData := getMinimalDriverRenderData()

	renderData.GPUDirectRDMA = &gpuv1.GPUDirectRDMASpec{
		Enabled: utils.BoolPtr(true),
	}

	objs, err := stateDriver.renderer.RenderObjects(
		&render.TemplatingData{
			Data: renderData,
		})
	require.Nil(t, err)
	require.NotEmpty(t, objs)

	ds, err := getDaemonSetObj(objs)
	require.Nil(t, err)
	require.NotNil(t, ds)

	nvidiaDriverCtr, err := getContainerObj(ds.Spec.Template.Spec.Containers, "nvidia-driver-ctr")
	require.Nil(t, err, "nvidia-driver-ctr should be in the list of containers")

	driverEnvars := []corev1.EnvVar{
		{
			Name:  "NVIDIA_VISIBLE_DEVICES",
			Value: "void",
		},
		{
			Name:  "GPU_DIRECT_RDMA_ENABLED",
			Value: "true",
		},
	}
	checkEnv(t, driverEnvars, nvidiaDriverCtr.Env)

	nvidiaPeermemCtr, err := getContainerObj(ds.Spec.Template.Spec.Containers, "nvidia-peermem-ctr")
	require.Nil(t, err, "nvidia-peermem-ctr should be in the list of containers")

	peermemEnvars := []corev1.EnvVar{
		{
			Name:  "NVIDIA_VISIBLE_DEVICES",
			Value: "void",
		},
	}

	checkEnv(t, peermemEnvars, nvidiaPeermemCtr.Env)

	mofedValidationCtr, err := getContainerObj(ds.Spec.Template.Spec.InitContainers, "mofed-validation")
	require.Nil(t, err, "mofed-validation should be in the list of containers")

	mofedValidationEnvars := getMofedValidationEnvars()

	checkEnv(t, mofedValidationEnvars, mofedValidationCtr.Env)

	expectedVolumes := getDriverVolumes()
	expectedVolumes = append(expectedVolumes, corev1.Volume{
		Name: "mlnx-ofed-usr-src",
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/run/mellanox/drivers/usr/src",
				Type: newHostPathType(corev1.HostPathDirectoryOrCreate),
			},
		},
	})

	checkVolumes(t, expectedVolumes, ds.Spec.Template.Spec.Volumes)

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
	state, err := NewStateDriver(nil, nil, "./testdata")
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	renderData := getMinimalDriverRenderData()

	renderData.GPUDirectRDMA = &gpuv1.GPUDirectRDMASpec{
		Enabled: utils.BoolPtr(true),
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
	// Construct a sample driver state manager
	state, err := NewStateDriver(nil, nil, "./testdata")
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	// set every field in driverSpec
	driverSpec := &driverSpec{
		ImagePath:        "nvcr.io/nvidia/driver:525.85.03-ubuntu22.04",
		ManagerImagePath: "nvcr.io/nvidia/cloud-native/k8s-driver-manager:devel",
		Spec: &nvidiav1alpha1.NVIDIADriverSpec{
			Manager: gpuv1.DriverManagerSpec{
				Repository:       "/path/to/repository",
				Image:            "image",
				Version:          "version",
				ImagePullPolicy:  "Always",
				ImagePullSecrets: []string{"manager-secret"},
				Env: []gpuv1.EnvVar{
					{Name: "FOO", Value: "foo"},
					{Name: "BAR", Value: "bar"},
				},
			},
			StartupProbe:     getDefaultContainerProbeSpec(),
			LivenessProbe:    getDefaultContainerProbeSpec(),
			ReadinessProbe:   getDefaultContainerProbeSpec(),
			UsePrecompiled:   new(bool),
			Repository:       "/path/to/repo",
			Image:            "image",
			Version:          "version",
			ImagePullPolicy:  "Always",
			ImagePullSecrets: []string{"secret-a", "secret-b"},
			Resources: &gpuv1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"memory": resource.MustParse("200Mi"),
					"cpu":    resource.MustParse("500m"),
				},
			},
			Args: []string{"--foo", "--bar"},
			Env: []gpuv1.EnvVar{
				{Name: "FOO", Value: "foo"},
				{Name: "BAR", Value: "bar"},
			},
			NodeSelector: map[string]string{
				"example.com/foo": "foo",
				"example.com/bar": "bar",
			},
		},
	}

	renderData := getMinimalDriverRenderData()
	renderData.Driver = driverSpec

	objs, err := stateDriver.renderer.RenderObjects(
		&render.TemplatingData{
			Data: renderData,
		})
	require.Nil(t, err)

	ds, err := getDaemonSetObj(objs)
	require.Nil(t, err)

	require.Equal(t, renderData.RuntimeSpec.Namespace, ds.Namespace)
	checkNodeSelector(t, renderData.Driver.Spec.NodeSelector, ds.Spec.Template.Spec.NodeSelector)
	checkPrecompiledLabel(t, renderData.Driver.Spec.UsePrecompiled, ds.Labels)
	checkPrecompiledLabel(t, renderData.Driver.Spec.UsePrecompiled, ds.Spec.Template.Labels)
	checkPullSecrets(t, renderData.Driver.Spec.ImagePullSecrets, ds.Spec.Template.Spec.ImagePullSecrets)

	nvidiaDriverCtr, err := getContainerObj(ds.Spec.Template.Spec.Containers, "nvidia-driver-ctr")
	require.Nil(t, err, "nvidia-driver-ctr should be in the list of containers")

	require.Equal(t, renderData.Driver.ImagePath, nvidiaDriverCtr.Image)
	checkImagePullPolicy(t, renderData.Driver.Spec.ImagePullPolicy, string(nvidiaDriverCtr.ImagePullPolicy))
	checkEnv(t, toCoreV1Envars(renderData.Driver.Spec.Env), nvidiaDriverCtr.Env)
	checkResources(t, renderData.Driver.Spec.Resources, nvidiaDriverCtr.Resources)
	checkArgs(t, []string{"init"}, renderData.Driver.Spec.Args, nvidiaDriverCtr.Args)
}

func TestDriverGDS(t *testing.T) {
	const (
		testName = "driver-gds"
	)

	state, err := NewStateDriver(nil, nil, "./testdata")
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	renderData := getMinimalDriverRenderData()

	renderData.GDS = &gdsDriverSpec{
		ImagePath: "nvcr.io/nvidia/cloud-native/nvidia-fs:2.16.1",
		Spec: &gpuv1.GPUDirectStorageSpec{
			Enabled: utils.BoolPtr(true),
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

func TestDriverAdditionalConfigs(t *testing.T) {
	const (
		testName = "driver-additional-configs"
	)

	state, err := NewStateDriver(nil, nil, "./testdata")
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	renderData := getMinimalDriverRenderData()

	renderData.AdditionalConfigs = &additionalConfigs{
		VolumeMounts: []corev1.VolumeMount{
			{
				Name: "test-cm",
				ReadOnly: true,
				MountPath: "/opt/config/test-file",
				SubPath: "test-file",
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
								Key: "test-file",
								Path: "test-file",
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

	actual, err := getYAMLString(objs)
	require.Nil(t, err)

	o, err := os.ReadFile(filepath.Join(manifestResultDir, testName+".yaml"))
	require.Nil(t, err)

	require.Equal(t, string(o), actual)
}

func getDaemonSetObj(objs []*unstructured.Unstructured) (*appsv1.DaemonSet, error) {
	ds := &appsv1.DaemonSet{}

	for _, obj := range objs {
		if obj.GetKind() == "DaemonSet" {
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, ds)
			if err != nil {
				return nil, err
			}
			return ds, nil
		}
	}

	return nil, fmt.Errorf("could not find object of kind 'DaemonSet'")
}

func getContainerObj(containers []corev1.Container, name string) (corev1.Container, error) {
	for _, c := range containers {
		if c.Name == name {
			return c, nil
		}
	}
	return corev1.Container{}, fmt.Errorf("failed to find container with name '%s'", name)
}

func getMinimalDriverRenderData() *driverRenderData {
	return &driverRenderData{
		Driver: &driverSpec{
			Spec: &nvidiav1alpha1.NVIDIADriverSpec{
				StartupProbe:   getDefaultContainerProbeSpec(),
				LivenessProbe:  getDefaultContainerProbeSpec(),
				ReadinessProbe: getDefaultContainerProbeSpec(),
			},
			ImagePath:        "nvcr.io/nvidia/driver:525.85.03-ubuntu22.04",
			ManagerImagePath: "nvcr.io/nvidia/cloud-native/k8s-driver-manager:devel",
		},
		Validator: &validatorSpec{
			Spec:      &gpuv1.ValidatorSpec{},
			ImagePath: "nvcr.io/nvidia/cloud-native/gpu-operator-validator:devel",
		},
		RuntimeSpec: driverRuntimeSpec{
			Namespace:         "test-operator",
			KubernetesVersion: "1.28.0",
		},
	}
}

func getAdditionalVolumeMounts(names ...string) additionalConfigs {
	cfgs := additionalConfigs{}
	for _, name := range names {
		cfgs.VolumeMounts = append(cfgs.VolumeMounts, corev1.VolumeMount{
			Name:      name,
			ReadOnly:  true,
			MountPath: filepath.Join("/path/to/", name),
			SubPath:   filepath.Join("/path/to/", name, "subpath"),
		})

		cfgs.Volumes = append(cfgs.Volumes, corev1.Volume{
			Name: name,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: name,
					},
					Items: []corev1.KeyToPath{
						{Key: "subpath", Path: "subpath"},
					},
				},
			},
		})
	}
	return cfgs
}

func getDefaultContainerProbeSpec() *gpuv1.ContainerProbeSpec {
	return &gpuv1.ContainerProbeSpec{
		InitialDelaySeconds: 60,
		TimeoutSeconds:      60,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		FailureThreshold:    120,
	}
}

func checkNodeSelector(t *testing.T, input map[string]string, output map[string]string) {
	for k, v := range input {
		observedValue, exists := output[k]
		require.True(t, exists)
		require.Equal(t, v, observedValue)
	}

}

func checkPrecompiledLabel(t *testing.T, usePrecompiled *bool, labels map[string]string) {
	value, exists := labels["nvidia.com/precompiled"]
	require.True(t, exists, "'nvidia.com/precompiled' label should always be set")

	if usePrecompiled == nil || *usePrecompiled == false {
		require.Equal(t, "false", value)
	} else {
		require.Equal(t, "true", value)
	}
}

func checkImagePullPolicy(t *testing.T, input string, output string) {
	if input != "" {
		require.Equal(t, input, output)
	} else {
		require.Equal(t, "IfNotPresent", output)
	}
}

func checkPullSecrets(t *testing.T, input []string, output []corev1.LocalObjectReference) {
	secrets := []string{}
	for _, secret := range output {
		secrets = append(secrets, secret.Name)
	}
	for _, secret := range input {
		require.Contains(t, secrets, secret)
	}
}

func checkEnv(t *testing.T, input []corev1.EnvVar, output []corev1.EnvVar) {
	inputMap := map[string]string{}
	for _, env := range input {
		inputMap[env.Name] = env.Value
	}

	outputMap := map[string]string{}
	for _, env := range output {
		outputMap[env.Name] = env.Value
	}

	for key, value := range inputMap {
		outputValue, exists := outputMap[key]
		require.True(t, exists)
		require.Equal(t, value, outputValue)
	}
}

func checkVolumes(t *testing.T, expected []corev1.Volume, actual []corev1.Volume) {
	expectedMap := volumeSliceToMap(expected)
	actualMap := volumeSliceToMap(actual)

	require.Equal(t, len(expectedMap), len(actualMap))

	for k, vol := range expectedMap {
		expectedVol, exists := actualMap[k]
		require.True(t, exists)
		require.Equal(t, expectedVol.HostPath.Path, vol.HostPath.Path,
			"Mismatch in Host Path value for volume %s", vol.Name)
		require.Equal(t, expectedVol.HostPath.Type, vol.HostPath.Type,
			"Mismatch in Host Path type for volume %s", vol.Name)
	}
}

func volumeSliceToMap(volumes []corev1.Volume) map[string]corev1.Volume {
	volumeMap := map[string]corev1.Volume{}
	for _, v := range volumes {
		volumeMap[v.Name] = v
	}

	return volumeMap
}

func checkResources(t *testing.T, input *gpuv1.ResourceRequirements, output corev1.ResourceRequirements) {
	if input == nil {
		return
	}

	for k, v := range input.Limits {
		outputValue, exists := output.Limits[k]
		require.True(t, exists, fmt.Sprintf("resource '%v' should exist in resource limits", k))
		require.Zero(t, v.Cmp(outputValue))
	}

	for k, v := range input.Requests {
		outputValue, exists := output.Requests[k]
		require.True(t, exists, fmt.Sprintf("resource '%v' should exist in resource requests", k))
		require.Zero(t, v.Cmp(outputValue))
	}

}

func checkArgs(t *testing.T, defaultArgs []string, input []string, output []string) {
	require.Equal(t, append(defaultArgs, input...), output)
}

// TODO: make this a public utils method
func newHostPathType(pathType corev1.HostPathType) *corev1.HostPathType {
	hostPathType := new(corev1.HostPathType)
	*hostPathType = pathType
	return hostPathType
}

func getDriverVolumes() []corev1.Volume {
	return []corev1.Volume{
		{
			Name: "run-nvidia",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/run/nvidia",
					Type: newHostPathType(corev1.HostPathDirectoryOrCreate),
				},
			},
		},
		{
			Name: "var-log",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/var/log",
				},
			},
		},
		{
			Name: "dev-log",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/dev/log",
				},
			},
		},
		{
			Name: "host-os-release",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/etc/os-release",
				},
			},
		},
		{
			Name: "run-nvidia-topologyd",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/run/nvidia-topologyd",
					Type: newHostPathType(corev1.HostPathDirectoryOrCreate),
				},
			},
		},
		{
			Name: "run-mellanox-drivers",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/run/mellanox/drivers",
					Type: newHostPathType(corev1.HostPathDirectoryOrCreate),
				},
			},
		},
		{
			Name: "run-nvidia-validations",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/run/nvidia/validations",
					Type: newHostPathType(corev1.HostPathDirectoryOrCreate),
				},
			},
		},
		{
			Name: "host-root",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/",
				},
			},
		},
		{
			Name: "host-sys",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/sys",
					Type: newHostPathType(corev1.HostPathDirectory),
				},
			},
		},
	}
}

func getMofedValidationEnvars() []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "WITH_WAIT",
			Value: "true",
		},
		{
			Name:  "COMPONENT",
			Value: "mofed",
		},
		{
			Name:  "NVIDIA_VISIBLE_DEVICES",
			Value: "void",
		},
		{
			Name:  "GPU_DIRECT_RDMA_ENABLED",
			Value: "true",
		},
	}
}

// TODO: make this a public utils method
func toCoreV1Envars(envs []gpuv1.EnvVar) []corev1.EnvVar {
	if len(envs) == 0 {
		return []corev1.EnvVar{}
	}
	var res []corev1.EnvVar
	for _, e := range envs {
		resEnv := corev1.EnvVar{
			Name:  e.Name,
			Value: e.Value,
		}
		res = append(res, resEnv)
	}
	return res
}
