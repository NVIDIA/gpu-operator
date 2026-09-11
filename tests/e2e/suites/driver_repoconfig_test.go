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

package suites

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	gpuclientset "github.com/NVIDIA/gpu-operator/api/versioned"
	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2elog "github.com/NVIDIA/gpu-operator/tests/e2e/framework/logs"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

const (
	nodeLocalRepoConfigMapName = "e2e-local-repo"
	nodeLocalRepoPath          = "/opt/local-packages"
	nodeLocalRepoVolumePrefix  = "node-local-repo-"
	driverDaemonSetName        = "nvidia-driver-daemonset"
	driverContainerName        = "nvidia-driver-ctr"
	driverConfigDigestEnvName  = "DRIVER_CONFIG_DIGEST"
)

// nodeLocalRepoVolumesFromDaemonSet returns the node-local repository hostPath volumes on
// the DaemonSet pod template, keyed by host path.
func nodeLocalRepoVolumesFromDaemonSet(ds *appsv1.DaemonSet) map[string]corev1.Volume {
	volumes := map[string]corev1.Volume{}
	for _, volume := range ds.Spec.Template.Spec.Volumes {
		if strings.HasPrefix(volume.Name, nodeLocalRepoVolumePrefix) && volume.HostPath != nil {
			volumes[volume.HostPath.Path] = volume
		}
	}
	return volumes
}

// nodeLocalRepoMountsFromDaemonSet returns the node-local repository volume mounts on the
// driver container, keyed by mount path.
func nodeLocalRepoMountsFromDaemonSet(ds *appsv1.DaemonSet) map[string]corev1.VolumeMount {
	mounts := map[string]corev1.VolumeMount{}
	for _, container := range ds.Spec.Template.Spec.Containers {
		if container.Name != driverContainerName {
			continue
		}
		for _, mount := range container.VolumeMounts {
			if strings.HasPrefix(mount.Name, nodeLocalRepoVolumePrefix) {
				mounts[mount.MountPath] = mount
			}
		}
	}
	return mounts
}

// driverConfigDigestFromDaemonSet returns the driver config digest env value on the driver
// container, which changes whenever the driver install configuration changes.
func driverConfigDigestFromDaemonSet(ds *appsv1.DaemonSet) string {
	for _, container := range ds.Spec.Template.Spec.Containers {
		if container.Name != driverContainerName {
			continue
		}
		for _, env := range container.Env {
			if env.Name == driverConfigDigestEnvName {
				return env.Value
			}
		}
	}
	return ""
}

// hasRepoConfigMapVolume reports whether the repo ConfigMap is still mounted alongside the
// node-local repository host paths.
func hasRepoConfigMapVolume(ds *appsv1.DaemonSet, configMapName string) bool {
	for _, volume := range ds.Spec.Template.Spec.Volumes {
		if volume.ConfigMap != nil && volume.ConfigMap.Name == configMapName {
			return true
		}
	}
	return false
}

var _ = Describe("Driver node-local package repository", Label("clusterPolicy"), func() {
	f := framework.NewFramework("gpu-operator")
	f.SkipNamespaceCreation = true

	var (
		clusterPolicyClient *helpers.ClusterPolicyClient
		daemonSetClient     *helpers.DaemonSetClient
		clientSet           kubernetes.Interface
		initialDigest       string
	)

	BeforeEach(func(ctx context.Context) {
		clientSet = f.ClientSet
		gpuClient, err := gpuclientset.NewForConfig(f.ClientConfig())
		Expect(err).NotTo(HaveOccurred())
		clusterPolicyClient = helpers.NewClusterPolicyClient(gpuClient)
		daemonSetClient = helpers.NewDaemonSetClient(clientSet)

		getClusterPolicyOrSkip(ctx, clusterPolicyClient, defaultPolicyName)

		// the repository configuration is required; nodeLocalPaths is only honoured with it
		_, err = clientSet.CoreV1().ConfigMaps(defaultNamespace).Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nodeLocalRepoConfigMapName,
				Namespace: defaultNamespace,
			},
			Data: map[string]string{
				"local-packages.sources": "Types: deb\nURIs: file://" + nodeLocalRepoPath + "\nSuites: ./\nTrusted: yes\n",
			},
		}, metav1.CreateOptions{})
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			Expect(err).NotTo(HaveOccurred())
		}

		ds, err := daemonSetClient.Get(ctx, defaultNamespace, driverDaemonSetName)
		Expect(err).NotTo(HaveOccurred())
		initialDigest = driverConfigDigestFromDaemonSet(ds)
	})

	AfterEach(func(ctx context.Context) {
		Expect(clusterPolicyClient.ClearDriverRepoConfig(ctx, defaultPolicyName)).To(Succeed())
		_ = clientSet.CoreV1().ConfigMaps(defaultNamespace).Delete(ctx,
			nodeLocalRepoConfigMapName, metav1.DeleteOptions{})
	})

	// Assertions are made on the DaemonSet object rather than pod readiness, so the test is
	// meaningful on a cluster where the driver cannot actually come up.
	It("should mount the node-local repository into the driver container", func(ctx context.Context) {
		Expect(clusterPolicyClient.SetDriverRepoConfig(ctx, defaultPolicyName,
			nodeLocalRepoConfigMapName, []string{nodeLocalRepoPath})).To(Succeed())

		Eventually(func(ctx context.Context) bool {
			ds, err := daemonSetClient.Get(ctx, defaultNamespace, driverDaemonSetName)
			if err != nil {
				e2elog.Logf("WARN: error getting driver daemonset: %v", err)
				return false
			}
			_, ok := nodeLocalRepoVolumesFromDaemonSet(ds)[nodeLocalRepoPath]
			return ok
		}).WithPolling(2 * time.Second).Within(specUpdateTimeout).WithContext(ctx).Should(BeTrue())

		ds, err := daemonSetClient.Get(ctx, defaultNamespace, driverDaemonSetName)
		Expect(err).NotTo(HaveOccurred())

		volume := nodeLocalRepoVolumesFromDaemonSet(ds)[nodeLocalRepoPath]
		Expect(volume.HostPath).NotTo(BeNil())
		Expect(volume.HostPath.Type).NotTo(BeNil())
		Expect(*volume.HostPath.Type).To(Equal(corev1.HostPathDirectory))

		mount, ok := nodeLocalRepoMountsFromDaemonSet(ds)[nodeLocalRepoPath]
		Expect(ok).To(BeTrue(), "driver container must mount the node-local repository")
		Expect(mount.ReadOnly).To(BeTrue())
		Expect(mount.Name).To(Equal(volume.Name))

		// the repo ConfigMap must still be mounted alongside the host path
		Expect(hasRepoConfigMapVolume(ds, nodeLocalRepoConfigMapName)).To(BeTrue())

		// changing the driver install configuration must roll the driver pods
		Expect(driverConfigDigestFromDaemonSet(ds)).NotTo(Equal(initialDigest))
	})

	It("should remove the mount when the node-local paths are cleared", func(ctx context.Context) {
		Expect(clusterPolicyClient.SetDriverRepoConfig(ctx, defaultPolicyName,
			nodeLocalRepoConfigMapName, []string{nodeLocalRepoPath})).To(Succeed())

		Eventually(func(ctx context.Context) bool {
			ds, err := daemonSetClient.Get(ctx, defaultNamespace, driverDaemonSetName)
			if err != nil {
				return false
			}
			return len(nodeLocalRepoVolumesFromDaemonSet(ds)) == 1
		}).WithPolling(2 * time.Second).Within(specUpdateTimeout).WithContext(ctx).Should(BeTrue())

		Expect(clusterPolicyClient.SetDriverRepoConfig(ctx, defaultPolicyName,
			nodeLocalRepoConfigMapName, nil)).To(Succeed())

		Eventually(func(ctx context.Context) bool {
			ds, err := daemonSetClient.Get(ctx, defaultNamespace, driverDaemonSetName)
			if err != nil {
				return false
			}
			return len(nodeLocalRepoVolumesFromDaemonSet(ds)) == 0 &&
				len(nodeLocalRepoMountsFromDaemonSet(ds)) == 0
		}).WithPolling(2 * time.Second).Within(specUpdateTimeout).WithContext(ctx).Should(BeTrue())
	})

	It("should not mount anything when node-local paths are unset", func(ctx context.Context) {
		Expect(clusterPolicyClient.SetDriverRepoConfig(ctx, defaultPolicyName,
			nodeLocalRepoConfigMapName, nil)).To(Succeed())

		Consistently(func(ctx context.Context) int {
			ds, err := daemonSetClient.Get(ctx, defaultNamespace, driverDaemonSetName)
			if err != nil {
				return -1
			}
			return len(nodeLocalRepoVolumesFromDaemonSet(ds))
		}).WithPolling(2 * time.Second).Within(15 * time.Second).WithContext(ctx).Should(Equal(0))
	})

	It("should reject node-local paths without a repository ConfigMap", func(ctx context.Context) {
		Expect(clusterPolicyClient.SetDriverRepoConfig(ctx, defaultPolicyName,
			"", []string{nodeLocalRepoPath})).To(Succeed())

		// the operator must surface the misconfiguration rather than mounting anything
		Consistently(func(ctx context.Context) int {
			ds, err := daemonSetClient.Get(ctx, defaultNamespace, driverDaemonSetName)
			if err != nil {
				return -1
			}
			return len(nodeLocalRepoVolumesFromDaemonSet(ds))
		}).WithPolling(2 * time.Second).Within(15 * time.Second).WithContext(ctx).Should(Equal(0))
	})
})
