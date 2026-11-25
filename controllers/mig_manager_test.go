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

package controllers

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

func TestMIGManagerIndependentValidation(t *testing.T) {
	tests := map[string]struct {
		runtime       gpuv1.Runtime
		cdi           bool
		nri           bool
		hostRoot      string
		driverDir     string
		driverEnabled bool
	}{
		"containerd":         {runtime: gpuv1.Containerd, driverEnabled: true},
		"crio":               {runtime: gpuv1.CRIO, driverEnabled: true},
		"cdi":                {runtime: gpuv1.Containerd, cdi: true, driverEnabled: true},
		"nri":                {runtime: gpuv1.Containerd, cdi: true, nri: true, driverEnabled: true},
		"preinstalled stack": {runtime: gpuv1.Containerd},
		"custom paths":       {runtime: gpuv1.Containerd, driverEnabled: true, hostRoot: "/host-root", driverDir: "/custom/driver"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(cfg.root, migManagerAssetsPath, "0600_daemonset.yaml"))
			require.NoError(t, err)
			var ds appsv1.DaemonSet
			require.NoError(t, yaml.Unmarshal(data, &ds))

			cp := getMIGManagerTestInput("default")
			cp.Spec.CDI = gpuv1.CDIConfigSpec{Enabled: &tc.cdi, NRIPluginEnabled: &tc.nri}
			cp.Spec.Driver.Enabled = &tc.driverEnabled
			cp.Spec.Toolkit.Enabled = &tc.driverEnabled
			cp.Spec.HostPaths.RootFS = tc.hostRoot
			cp.Spec.HostPaths.DriverInstallDir = tc.driverDir
			cp.Spec.Validator.ImagePullPolicy = "Always"
			cp.Spec.Validator.ImagePullSecrets = []string{"validator-registry"}
			cp.Spec.Validator.Resources = &gpuv1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("32Mi")},
			}
			cp.Spec.Validator.Driver.Env = []gpuv1.EnvVar{{Name: "SLEEP_INTERVAL_SECONDS", Value: "2"}}
			cp.Spec.Validator.Toolkit.Env = []gpuv1.EnvVar{{Name: "SLEEP_INTERVAL_SECONDS", Value: "3"}}
			n := clusterPolicyController
			n.singleton, n.runtime = cp, tc.runtime
			require.NoError(t, preProcessDaemonSet(&ds, n))

			pod := ds.Spec.Template.Spec
			require.Len(t, pod.InitContainers, 2)
			require.Equal(t, "driver-validation", pod.InitContainers[0].Name)
			require.Equal(t, "toolkit-validation", pod.InitContainers[1].Name)
			image, err := gpuv1.ImagePath(&cp.Spec.Validator)
			require.NoError(t, err)
			localPath := "/run/nvidia/mig-manager/validations"
			for i, component := range []string{"driver", "toolkit"} {
				ctr := pod.InitContainers[i]
				require.Equal(t, []string{"nvidia-validator"}, ctr.Command)
				require.Empty(t, ctr.Args)
				require.Equal(t, component, getContainerEnv(&ctr, "COMPONENT"))
				wantWait := "true"
				if component == "toolkit" {
					wantWait = "false"
				}
				require.Equal(t, wantWait, getContainerEnv(&ctr, "WITH_WAIT"))
				require.Equal(t, localPath, getContainerEnv(&ctr, "OUTPUT_DIR"))
				require.Equal(t, image, ctr.Image)
				require.Equal(t, corev1.PullAlways, ctr.ImagePullPolicy)
				require.Equal(t, cp.Spec.Validator.Resources.Requests, ctr.Resources.Requests)
				require.NotNil(t, ctr.SecurityContext)
				require.EqualValues(t, 0, *ctr.SecurityContext.RunAsUser)
				require.True(t, *ctr.SecurityContext.Privileged)
				mount := findVolumeMountByName(ctr.VolumeMounts, "mig-manager-validations")
				require.NotNil(t, mount)
				require.Equal(t, localPath, mount.MountPath)
				require.Nil(t, mount.MountPropagation, "emptyDir must not use host mount propagation")
			}
			require.Contains(t, pod.ImagePullSecrets, corev1.LocalObjectReference{Name: "validator-registry"})
			driver, toolkit := pod.InitContainers[0], pod.InitContainers[1]
			require.Equal(t, "2", getContainerEnv(&driver, "SLEEP_INTERVAL_SECONDS"))
			require.Equal(t, "3", getContainerEnv(&toolkit, "SLEEP_INTERVAL_SECONDS"))
			require.Equal(t, "all", getContainerEnv(&toolkit, "NVIDIA_VISIBLE_DEVICES"))
			namespace := findEnvVarByName(driver.Env, "OPERATOR_NAMESPACE")
			require.NotNil(t, namespace)
			require.Equal(t, "metadata.namespace", namespace.ValueFrom.FieldRef.FieldPath)
			hostStatus := findVolumeMountByName(driver.VolumeMounts, "run-nvidia-validations")
			require.NotNil(t, hostStatus)
			require.Equal(t, "/run/nvidia/validations", hostStatus.MountPath)
			require.True(t, hostStatus.ReadOnly, "the host directory is only an input for .driver-ctr-ready")
			require.Nil(t, findVolumeMountByName(toolkit.VolumeMounts, "run-nvidia-validations"))
			manager := findContainerByName(pod.Containers, "nvidia-mig-manager")
			require.NotNil(t, manager)
			require.Nil(t, findVolumeMountByName(manager.VolumeMounts, "run-nvidia-validations"))
			local := findVolumeMountByName(manager.VolumeMounts, "mig-manager-validations")
			require.NotNil(t, local)
			require.Equal(t, localPath, local.MountPath)

			for _, vol := range pod.Volumes {
				switch vol.Name {
				case "mig-manager-validations":
					require.NotNil(t, vol.EmptyDir)
					require.Nil(t, vol.HostPath)
				case "host-root":
					require.Equal(t, filepath.Join("/", tc.hostRoot), vol.HostPath.Path)
				case "host-dev-char":
					require.Equal(t, filepath.Join("/", tc.hostRoot, "dev/char"), vol.HostPath.Path)
				case "driver-install-dir":
					driverDir := DefaultDriverInstallDir
					if tc.driverDir != "" {
						driverDir = tc.driverDir
					}
					require.Equal(t, driverDir, vol.HostPath.Path)
					require.Equal(t, driverDir, findVolumeMountByName(driver.VolumeMounts, vol.Name).MountPath)
				}
			}
			if tc.driverDir != "" {
				require.Equal(t, tc.driverDir, getContainerEnv(&driver, DriverInstallDirEnvName))
				require.Equal(t, tc.driverDir, getContainerEnv(&driver, DriverInstallDirCtrPathEnvName))
			}
			if tc.nri {
				require.Nil(t, pod.RuntimeClassName)
				key := NRIAnnotationDomain + "/container.toolkit-validation"
				require.Equal(t, "management.nvidia.com/gpu=all", ds.Spec.Template.Annotations[key])
			} else if tc.cdi || tc.runtime != gpuv1.CRIO {
				require.NotNil(t, pod.RuntimeClassName)
			}
		})
	}
}

func TestMIGManagerDriverValidationPermissions(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cfg.root, migManagerAssetsPath, "0200_role.yaml"))
	require.NoError(t, err)
	var role rbacv1.Role
	require.NoError(t, yaml.Unmarshal(data, &role))
	require.Contains(t, role.Rules, rbacv1.PolicyRule{APIGroups: []string{"apps"}, Resources: []string{"daemonsets"}, Verbs: []string{"list"}})
}

func TestMIGManagerEntrypointWithPausedValidator(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(cfg.root, migManagerAssetsPath, "0420_configmap.yaml"))
	require.NoError(t, err)
	var cm corev1.ConfigMap
	require.NoError(t, yaml.Unmarshal(data, &cm))
	for _, hostDriver := range []string{"true", "false"} {
		t.Run("host-driver="+hostDriver, func(t *testing.T) {
			dir := t.TempDir()
			// Only this pod's init output exists. Operator-validator has removed
			// its readiness files, as it does when paused for a MIG change.
			localPath := filepath.Join(dir, "local")
			require.NoError(t, os.Mkdir(localPath, 0755))
			settings := "IS_HOST_DRIVER=" + hostDriver + "\nNVIDIA_DRIVER_ROOT=/custom/driver\nDRIVER_ROOT_CTR_PATH=/driver-root\nNVIDIA_DEV_ROOT=/\nDEV_ROOT_CTR_PATH=/host\n"
			require.NoError(t, os.WriteFile(filepath.Join(localPath, "driver-ready"), []byte(settings), 0600))
			script := strings.ReplaceAll(cm.Data["entrypoint.sh"], "/run/nvidia/mig-manager/validations", localPath)
			script = strings.ReplaceAll(script, "/run/nvidia/validations", filepath.Join(dir, "absent-host-validations"))
			entrypoint := filepath.Join(dir, "entrypoint.sh")
			require.NoError(t, os.WriteFile(entrypoint, []byte(script), 0600))
			manager := "#!/bin/sh\nprintf 'settings=%s,%s,%s,%s,%s,%s\\n' \"$IS_HOST_DRIVER\" \"$NVIDIA_DRIVER_ROOT\" \"$DRIVER_ROOT_CTR_PATH\" \"$NVIDIA_DEV_ROOT\" \"$DEV_ROOT_CTR_PATH\" \"$WITH_SHUTDOWN_HOST_GPU_CLIENTS\"\n"
			require.NoError(t, os.WriteFile(filepath.Join(dir, "nvidia-mig-manager"), []byte(manager), 0700)) //nolint:gosec // The stub must be executable so the entrypoint can exec it.
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("WITH_SHUTDOWN_HOST_GPU_CLIENTS", "")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, "/bin/sh", entrypoint).CombinedOutput()
			require.NoError(t, err, "%s", out)
			require.Contains(t, string(out), "settings="+hostDriver+",/custom/driver,/driver-root,/,/host,"+hostDriver)
		})
	}
}
