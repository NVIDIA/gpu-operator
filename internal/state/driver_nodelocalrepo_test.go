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
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/internal/render"
	"github.com/NVIDIA/gpu-operator/internal/utils"
)

// newNodeLocalRepoDriverState returns a stateDriver with a repo ConfigMap already present.
func newNodeLocalRepoDriverState(t *testing.T) *stateDriver {
	t.Helper()

	repoConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-repo-config",
			Namespace: "test-ns",
		},
		Data: map[string]string{
			"local-packages.sources": "Types: deb",
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(driverTestScheme(t)).WithObjects(repoConfigMap).Build()
	return &stateDriver{stateSkel: stateSkel{client: fakeClient, namespace: "test-ns"}}
}

func nodeLocalRepoDriverCR(configMapName string, nodeLocalPaths []string, usePrecompiled bool) *nvidiav1alpha1.NVIDIADriver {
	cr := &nvidiav1alpha1.NVIDIADriver{
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			RepoConfig: &nvidiav1alpha1.DriverRepoConfigSpec{
				Name:           configMapName,
				NodeLocalPaths: nodeLocalPaths,
			},
		},
	}
	if usePrecompiled {
		cr.Spec.UsePrecompiled = new(true)
	}
	return cr
}

func nodeLocalRepoConfigs(t *testing.T, cfgs *additionalConfigs) (map[string]corev1.Volume, map[string]corev1.VolumeMount) {
	t.Helper()

	volumes := map[string]corev1.Volume{}
	for _, volume := range cfgs.Volumes {
		if !strings.HasPrefix(volume.Name, "node-local-repo-") {
			continue
		}
		require.NotNil(t, volume.HostPath, "volume %s must be a hostPath volume", volume.Name)
		volumes[volume.HostPath.Path] = volume
	}

	mounts := map[string]corev1.VolumeMount{}
	for _, mount := range cfgs.VolumeMounts {
		if !strings.HasPrefix(mount.Name, "node-local-repo-") {
			continue
		}
		mounts[mount.MountPath] = mount
	}
	return volumes, mounts
}

func TestGetDriverAdditionalConfigsRepoConfigNodeLocalPaths(t *testing.T) {
	driverState := newNodeLocalRepoDriverState(t)

	cfgs, err := driverState.getDriverAdditionalConfigs(context.Background(),
		nodeLocalRepoDriverCR("test-repo-config", []string{"/opt/local-packages"}, false),
		fakeClusterInfo{containerRuntime: consts.Containerd},
		nodePool{osRelease: "ubuntu", osVersion: "24.04"})
	require.NoError(t, err)

	volumes, mounts := nodeLocalRepoConfigs(t, cfgs)
	require.Len(t, volumes, 1)
	volume := volumes["/opt/local-packages"]
	require.Equal(t, "node-local-repo-0", volume.Name)
	require.NotNil(t, volume.HostPath.Type)
	require.Equal(t, corev1.HostPathDirectory, *volume.HostPath.Type)

	require.Len(t, mounts, 1)
	mount := mounts["/opt/local-packages"]
	require.Equal(t, "node-local-repo-0", mount.Name)
	require.True(t, mount.ReadOnly)

	// the repo ConfigMap volume must still be present alongside the host path
	var sawConfigMapVolume bool
	for _, volume := range cfgs.Volumes {
		if volume.ConfigMap != nil && volume.ConfigMap.Name == "test-repo-config" {
			sawConfigMapVolume = true
		}
	}
	require.True(t, sawConfigMapVolume, "repo ConfigMap volume must still be applied")
}

func TestGetDriverAdditionalConfigsRepoConfigMultipleNodeLocalPathsSorted(t *testing.T) {
	driverState := newNodeLocalRepoDriverState(t)

	cfgs, err := driverState.getDriverAdditionalConfigs(context.Background(),
		nodeLocalRepoDriverCR("test-repo-config", []string{"/srv/pkgs", "/opt/local-packages"}, false),
		fakeClusterInfo{containerRuntime: consts.Containerd},
		nodePool{osRelease: "ubuntu", osVersion: "24.04"})
	require.NoError(t, err)

	volumes, _ := nodeLocalRepoConfigs(t, cfgs)
	require.Len(t, volumes, 2)
	require.Equal(t, "node-local-repo-0", volumes["/opt/local-packages"].Name)
	require.Equal(t, "node-local-repo-1", volumes["/srv/pkgs"].Name)
}

func TestGetDriverAdditionalConfigsRepoConfigNodeLocalPathsWithoutConfigMap(t *testing.T) {
	driverState := newNodeLocalRepoDriverState(t)

	_, err := driverState.getDriverAdditionalConfigs(context.Background(),
		nodeLocalRepoDriverCR("", []string{"/opt/local-packages"}, false),
		fakeClusterInfo{containerRuntime: consts.Containerd},
		nodePool{osRelease: "ubuntu", osVersion: "24.04"})
	require.ErrorContains(t, err, "repoConfig.name is empty")
}

func TestGetDriverAdditionalConfigsRepoConfigNodeLocalPathsInvalid(t *testing.T) {
	driverState := newNodeLocalRepoDriverState(t)

	_, err := driverState.getDriverAdditionalConfigs(context.Background(),
		nodeLocalRepoDriverCR("test-repo-config", []string{"/opt/local-packages/"}, false),
		fakeClusterInfo{containerRuntime: consts.Containerd},
		nodePool{osRelease: "ubuntu", osVersion: "24.04"})
	require.ErrorContains(t, err, "not a clean path")
}

func TestGetDriverAdditionalConfigsRepoConfigNodeLocalPathsPrecompiled(t *testing.T) {
	driverState := newNodeLocalRepoDriverState(t)

	cfgs, err := driverState.getDriverAdditionalConfigs(context.Background(),
		nodeLocalRepoDriverCR("test-repo-config", []string{"/opt/local-packages"}, true),
		fakeClusterInfo{containerRuntime: consts.Containerd},
		nodePool{osRelease: "ubuntu", osVersion: "24.04"})
	require.NoError(t, err)

	volumes, mounts := nodeLocalRepoConfigs(t, cfgs)
	require.Empty(t, volumes)
	require.Empty(t, mounts)
}

// TestGetDriverAdditionalConfigsNoNodeLocalPaths is the regression guard for the feature
// being opt-in.
func TestGetDriverAdditionalConfigsNoNodeLocalPaths(t *testing.T) {
	driverState := newNodeLocalRepoDriverState(t)

	cfgs, err := driverState.getDriverAdditionalConfigs(context.Background(),
		nodeLocalRepoDriverCR("test-repo-config", nil, false),
		fakeClusterInfo{containerRuntime: consts.Containerd},
		nodePool{osRelease: "ubuntu", osVersion: "24.04"})
	require.NoError(t, err)

	volumes, mounts := nodeLocalRepoConfigs(t, cfgs)
	require.Empty(t, volumes)
	require.Empty(t, mounts)
}

// TestDriverNodeLocalRepoVolumesRender renders the driver DaemonSet with the volumes and
// mounts produced by the real helper, proving the manifest template emits them correctly
// without requiring a golden-file update.
func TestDriverNodeLocalRepoVolumesRender(t *testing.T) {
	volumes, mounts, err := utils.NodeLocalRepoVolumes([]string{"/opt/local-packages"})
	require.NoError(t, err)

	state, err := NewStateDriver(nil, "", nil, manifestDir)
	require.NoError(t, err)
	stateDriver, ok := state.(*stateDriver)
	require.True(t, ok)

	renderData := getMinimalDriverRenderData()
	renderData.AdditionalConfigs = &additionalConfigs{Volumes: volumes, VolumeMounts: mounts}

	objs, err := stateDriver.renderer.RenderObjects(&render.TemplatingData{Data: renderData})
	require.NoError(t, err)

	actual, err := getYAMLString(objs)
	require.NoError(t, err)

	require.Contains(t, actual, "- mountPath: /opt/local-packages\n          name: node-local-repo-0\n          readOnly: true",
		"driver container must mount the node-local repository read-only")
	require.Contains(t, actual, "- hostPath:\n          path: /opt/local-packages\n          type: Directory\n        name: node-local-repo-0",
		"pod spec must declare the node-local repository as a Directory hostPath volume")
}

// TestDriverRepoConfigDeb822SourcesOverride pins the documented air-gap recipe: a repo
// ConfigMap key named after the base image's own default deb822 source file is mounted
// with a subPath, and therefore overrides the defaults rather than adding to them.
func TestDriverRepoConfigDeb822SourcesOverride(t *testing.T) {
	repoConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "local-package-repo",
			Namespace: "test-ns",
		},
		Data: map[string]string{
			"ubuntu.sources": "Types: deb\nURIs: file:///opt/local-packages\nSuites: ./\n",
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(driverTestScheme(t)).WithObjects(repoConfigMap).Build()
	driverState := &stateDriver{stateSkel: stateSkel{client: fakeClient, namespace: "test-ns"}}

	mounts, items, err := driverState.createConfigMapVolumeMounts(context.Background(), "test-ns",
		"local-package-repo", "/etc/apt/sources.list.d")
	require.NoError(t, err)

	require.Len(t, mounts, 1)
	require.Equal(t, "/etc/apt/sources.list.d/ubuntu.sources", mounts[0].MountPath)
	require.Equal(t, "ubuntu.sources", mounts[0].SubPath,
		"the key must be mounted with a subPath so it replaces the base image's default sources")
	require.True(t, mounts[0].ReadOnly)

	require.Len(t, items, 1)
	require.Equal(t, "ubuntu.sources", items[0].Key)
	require.Equal(t, "ubuntu.sources", items[0].Path)
}

// TestGetDriverAdditionalConfigsEmptyNodeLocalPathsWithoutConfigMap covers what a default
// helm install now produces: an empty but non-nil nodeLocalPaths list with no repo
// ConfigMap. This must be a no-op, not an error.
func TestGetDriverAdditionalConfigsEmptyNodeLocalPathsWithoutConfigMap(t *testing.T) {
	driverState := newNodeLocalRepoDriverState(t)

	cfgs, err := driverState.getDriverAdditionalConfigs(context.Background(),
		nodeLocalRepoDriverCR("", []string{}, false),
		fakeClusterInfo{containerRuntime: consts.Containerd},
		nodePool{osRelease: "ubuntu", osVersion: "24.04"})
	require.NoError(t, err)

	volumes, mounts := nodeLocalRepoConfigs(t, cfgs)
	require.Empty(t, volumes)
	require.Empty(t, mounts)
}
