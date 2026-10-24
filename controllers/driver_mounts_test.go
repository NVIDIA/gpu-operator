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
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

func TestDriverRootParentMounts(t *testing.T) {
	operands := map[string]string{
		"toolkit":           "state-container-toolkit/0500_daemonset.yaml",
		"device-plugin":     "state-device-plugin/0500_daemonset.yaml",
		"mig-manager":       "state-mig-manager/0600_daemonset.yaml",
		"validator":         "state-operator-validation/0500_daemonset.yaml",
		"vgpu-manager":      "state-vgpu-device-manager/0600_daemonset.yaml",
		"sandbox-validator": "state-sandbox-validation/0500_daemonset.yaml",
	}
	for name, manifest := range operands {
		for _, configuredRoot := range []string{"", DefaultDriverInstallDir, "/opt/nvidia/rootfs", "/opt/nvidia/rootfs/", "/custom-root"} {
			t.Run(name+"/"+configuredRoot, func(t *testing.T) {
				data, err := os.ReadFile(filepath.Join("..", "assets", manifest))
				require.NoError(t, err)
				var ds appsv1.DaemonSet
				require.NoError(t, yaml.Unmarshal(data, &ds))
				transformForDriverInstallDir(&ds, configuredRoot)

				root := configuredRoot
				if root == "" {
					root = DefaultDriverInstallDir
				}
				root = path.Clean(root)
				parent := path.Dir(root)
				customRoot := configuredRoot != "" && configuredRoot != DefaultDriverInstallDir
				volumeNames := make(map[string]bool)
				for _, volume := range ds.Spec.Template.Spec.Volumes {
					if volume.Name == "driver-install-dir" || volume.Name == DriverInstallPathVolName {
						require.NotNil(t, volume.HostPath)
						require.Equal(t, parent, volume.HostPath.Path)
						require.NotEqual(t, root, volume.HostPath.Path, "the replaceable root must remain a child mount")
						volumeNames[volume.Name] = true
					}
				}
				require.NotEmpty(t, volumeNames)

				for _, containers := range [][]corev1.Container{ds.Spec.Template.Spec.InitContainers, ds.Spec.Template.Spec.Containers} {
					for _, ctr := range containers {
						for _, mount := range ctr.VolumeMounts {
							if !volumeNames[mount.Name] {
								continue
							}
							require.NotNil(t, mount.MountPropagation)
							require.Equal(t, corev1.MountPropagationHostToContainer, *mount.MountPropagation)
							require.Empty(t, mount.SubPath)
							require.Empty(t, mount.SubPathExpr)
							if ctr.Name == "driver-validation" || ctr.Name == "vgpu-manager-validation" {
								if customRoot {
									require.Equal(t, "/driver-root", mount.MountPath)
								} else {
									require.Equal(t, parent, mount.MountPath)
								}
							} else {
								require.Equal(t, "/driver-root", mount.MountPath)
							}
						}
						if ctr.Name == "driver-validation" || ctr.Name == "vgpu-manager-validation" {
							if customRoot {
								require.Contains(t, ctr.Env, corev1.EnvVar{Name: DriverInstallDirEnvName, Value: root})
								require.Contains(t, ctr.Env, corev1.EnvVar{Name: DriverInstallDirCtrPathEnvName, Value: path.Join("/driver-root", path.Base(root))})
							}
						}
						if ctr.Name == "nvidia-vgpu-device-manager" {
							require.Contains(t, ctr.Env, corev1.EnvVar{Name: "DRIVER_ROOT_CTR_PATH", Value: path.Join("/driver-root", path.Base(root))})
						}
					}
				}
			})
		}
	}
}
