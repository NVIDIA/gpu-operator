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
	"fmt"
	"path/filepath"
	"testing"
	"text/template"

	"github.com/NVIDIA/gpu-operator/internal/render"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/v1alpha1"
)

func TestDriverRenderMinimal(t *testing.T) {
	// Construct a sample driver state manager
	state, err := NewStateDriver(nil, nil, "./testdata")
	require.Nil(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	renderData := getMinimalDriverRenderData()

	_, err = stateDriver.renderer.RenderObjects(
		&render.TemplatingData{
			Data: renderData,
			Funcs: template.FuncMap{
				"Deref": func(b *bool) bool {
					if b == nil {
						return false
					}
					return *b
				},
			},
		})
	require.Nil(t, err)
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
		ManagerImagePath: "nvcr.io/nvida/cloud-native/k8s-driver-manager:devel",
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
				Limits: v1.ResourceList{
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
			Funcs: template.FuncMap{
				"Deref": func(b *bool) bool {
					if b == nil {
						return false
					}
					return *b
				},
			},
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
	require.Nilf(t, err, "nvidia-driver-ctr should be in the list of containers")

	require.Equal(t, renderData.Driver.ImagePath, nvidiaDriverCtr.Image)
	checkImagePullPolicy(t, renderData.Driver.Spec.ImagePullPolicy, string(nvidiaDriverCtr.ImagePullPolicy))
	checkEnv(t, renderData.Driver.Spec.Env, nvidiaDriverCtr.Env)
	checkResources(t, renderData.Driver.Spec.Resources, nvidiaDriverCtr.Resources)
	checkArgs(t, []string{"init"}, renderData.Driver.Spec.Args, nvidiaDriverCtr.Args)
}

func newTrue() *bool {
	b := true
	return &b
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

func getContainerObj(containers []v1.Container, name string) (v1.Container, error) {
	for _, c := range containers {
		if c.Name == name {
			return c, nil
		}
	}
	return v1.Container{}, fmt.Errorf("failed to find container with name '%s'", name)
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
			ManagerImagePath: "nvcr.io/nvida/cloud-native/k8s-driver-manager:devel",
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

func getAdditionalVolumeMounts(names ...string) additionalVolumeMounts {
	vms := additionalVolumeMounts{}
	for _, name := range names {
		vms.VolumeMounts = append(vms.VolumeMounts, corev1.VolumeMount{
			Name:      name,
			ReadOnly:  true,
			MountPath: filepath.Join("/path/to/", name),
			SubPath:   filepath.Join("/path/to/", name, "subpath"),
		})

		vms.Volumes = append(vms.Volumes, corev1.Volume{
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
	return vms
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

func checkPullSecrets(t *testing.T, input []string, output []v1.LocalObjectReference) {
	secrets := []string{}
	for _, secret := range output {
		secrets = append(secrets, secret.Name)
	}
	for _, secret := range input {
		require.Contains(t, secrets, secret)
	}
}

func checkEnv(t *testing.T, input []gpuv1.EnvVar, output []v1.EnvVar) {
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

func checkResources(t *testing.T, input *gpuv1.ResourceRequirements, output v1.ResourceRequirements) {
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
