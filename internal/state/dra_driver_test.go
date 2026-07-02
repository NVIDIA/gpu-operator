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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

const draManifestDir = "../../manifests/state-dra-driver"

func newTestDRAState(t *testing.T) *stateDRADriver {
	t.Helper()
	t.Setenv("VALIDATOR_IMAGE", "nvcr.io/nvidia/gpu-operator-validator:test")
	s, err := NewStateDRADriver(fake.NewClientBuilder().Build(), "test-operator", runtime.NewScheme(), draManifestDir)
	require.NoError(t, err)
	return s.(*stateDRADriver)
}

func sampleGPUCluster() *nvidiav1alpha1.GPUCluster {
	return &nvidiav1alpha1.GPUCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "gpucluster-sample"},
		Spec: nvidiav1alpha1.GPUClusterSpec{
			DRADriver: nvidiav1alpha1.DRADriverSpec{
				Repository:      "nvcr.io/nvidia",
				Image:           "k8s-dra-driver-gpu",
				Version:         "v0.1.0",
				ImagePullPolicy: "IfNotPresent",
				FeatureGates:    map[string]bool{"MPSSupport": true, "AdminAccess": false},
			},
			Daemonsets: nvidiav1.DaemonsetsSpec{
				Tolerations: []corev1.Toleration{{
					Key:      "nvidia.com/gpu",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				}},
				PriorityClassName: "system-node-critical",
			},
			HostPaths: nvidiav1.HostPathsSpec{
				RootFS:           "/",
				DriverInstallDir: "/run/nvidia/driver",
				KubeletRootDir:   "/var/lib/kubelet",
			},
		},
	}
}

func draSupportedCatalog() InfoCatalog {
	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeClusterInfo, testClusterInfo{
		draSupported:   true,
		draResourceGVR: schema.GroupVersionResource{Group: "resource.k8s.io", Version: "v1", Resource: "deviceclasses"},
	})
	return catalog
}

func TestDRADriverRenderGPUsCut(t *testing.T) {
	s := newTestDRAState(t)

	objs, err := s.getManifestObjects(context.Background(), sampleGPUCluster(), draSupportedCatalog())
	require.NoError(t, err)
	require.NotEmpty(t, objs)

	kinds := map[string]int{}
	for _, o := range objs {
		kinds[o.GetKind()]++
	}
	assert.Equal(t, 1, kinds["ServiceAccount"])
	assert.Equal(t, 1, kinds["ClusterRole"])
	assert.Equal(t, 1, kinds["ClusterRoleBinding"])
	assert.Equal(t, 3, kinds["DeviceClass"], "expected gpu.nvidia.com, mig.nvidia.com and vfio.gpu.nvidia.com")
	assert.Equal(t, 1, kinds["DaemonSet"])

	// DeviceClass apiVersion is injected from discovery, not hardcoded.
	for _, o := range objs {
		if o.GetKind() == "DeviceClass" {
			assert.Equal(t, "resource.k8s.io/v1", o.GetAPIVersion())
		}
	}

	ds := findDaemonSet(t, objs)
	podSpec := ds.Spec.Template.Spec

	// Init container is the DRA validator, privileged, with the verified mount plan
	// and no /driver-root symlink machinery.
	require.Len(t, podSpec.InitContainers, 1)
	initCtr := podSpec.InitContainers[0]
	assert.Equal(t, "driver-validation", initCtr.Name)
	assert.Equal(t, "nvcr.io/nvidia/gpu-operator-validator:test", initCtr.Image)
	require.NotNil(t, initCtr.SecurityContext)
	require.NotNil(t, initCtr.SecurityContext.Privileged)
	assert.True(t, *initCtr.SecurityContext.Privileged)
	assert.ElementsMatch(t,
		[]string{"host-root", "driver-install-dir", "validations"},
		mountNames(initCtr.VolumeMounts))

	// Single gpus container in this cut.
	require.Len(t, podSpec.Containers, 1)
	gpus := podSpec.Containers[0]
	assert.Equal(t, "gpus", gpus.Name)
	assert.Equal(t, "nvcr.io/nvidia/k8s-dra-driver-gpu:v0.1.0", gpus.Image)

	// Sources the driver-ready contract and execs the plugin (no prestart.sh).
	args := strings.Join(gpus.Args, "\n")
	assert.Contains(t, args, "source /run/nvidia/validations/driver-ready")
	assert.Contains(t, args, "exec gpu-kubelet-plugin")

	env := envMap(gpus.Env)
	assert.Equal(t, "nvcr.io/nvidia/k8s-dra-driver-gpu:v0.1.0", env["IMAGE_NAME"])
	// FEATURE_GATES is sorted so reconciles do not churn the pod spec.
	assert.Equal(t, "AdminAccess=false,MPSSupport=true", env["FEATURE_GATES"])
	// NVIDIA_DRIVER_ROOT / DRIVER_ROOT_CTR_PATH must be sourced, never hardcoded.
	_, hasRoot := env["NVIDIA_DRIVER_ROOT"]
	assert.False(t, hasRoot, "NVIDIA_DRIVER_ROOT must come from driver-ready, not the pod spec")
	_, hasCtrPath := env["DRIVER_ROOT_CTR_PATH"]
	assert.False(t, hasCtrPath, "DRIVER_ROOT_CTR_PATH must come from driver-ready, not the pod spec")

	// No /driver-root or /driver-root-parent volumes; validations is the host's
	// /run/nvidia/validations so the validator can see .driver-ctr-ready.
	for _, v := range podSpec.Volumes {
		assert.NotEqual(t, "driver-root", v.Name)
		assert.NotEqual(t, "driver-root-parent", v.Name)
	}
	assert.NotNil(t, findVolume(t, ds, "validations").HostPath)
	// The host-vs-containerized driver branch is a per-node runtime probe, so the
	// plugin container must carry both mounts.
	assert.NotNil(t, findVolume(t, ds, "host-root").HostPath)
	assert.NotNil(t, findVolume(t, ds, "driver-install-dir").HostPath)

	// Shared daemonsets toleration is applied.
	require.NotEmpty(t, podSpec.Tolerations)
	assert.Equal(t, "nvidia.com/gpu", podSpec.Tolerations[0].Key)
}

func TestDRADriverHealthcheckPortOverride(t *testing.T) {
	s := newTestDRAState(t)
	cr := sampleGPUCluster()
	cr.Spec.DRADriver.GPUs.KubeletPlugin.Healthcheck = &nvidiav1alpha1.DRADriverHealthcheckSpec{
		Port: ptr.To(int32(52000)),
	}

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)

	gpus := findDaemonSet(t, objs).Spec.Template.Spec.Containers[0]
	assert.Equal(t, "52000", envMap(gpus.Env)["HEALTHCHECK_PORT"])
	require.NotNil(t, gpus.StartupProbe)
	assert.Equal(t, int32(52000), gpus.StartupProbe.GRPC.Port)
	require.NotNil(t, gpus.LivenessProbe)
	assert.Equal(t, int32(52000), gpus.LivenessProbe.GRPC.Port)
}

func TestDRADriverHealthcheckDisabled(t *testing.T) {
	s := newTestDRAState(t)
	cr := sampleGPUCluster()
	cr.Spec.DRADriver.GPUs.KubeletPlugin.Healthcheck = &nvidiav1alpha1.DRADriverHealthcheckSpec{
		Enabled: ptr.To(false),
	}

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)

	// The negative port still reaches the plugin (which disables its health service);
	// the probes are omitted so the container is not restarted for lacking one.
	gpus := findDaemonSet(t, objs).Spec.Template.Spec.Containers[0]
	assert.Equal(t, "-1", envMap(gpus.Env)["HEALTHCHECK_PORT"])
	assert.Nil(t, gpus.StartupProbe)
	assert.Nil(t, gpus.LivenessProbe)
}

func TestDRADriverRenderDRAUnsupported(t *testing.T) {
	s := newTestDRAState(t)

	catalog := NewInfoCatalog()
	catalog.Add(InfoTypeClusterInfo, testClusterInfo{draSupported: false})

	_, err := s.getManifestObjects(context.Background(), sampleGPUCluster(), catalog)
	require.Error(t, err, "rendering must fail when the resource.k8s.io DeviceClass API is absent")
}

func findDaemonSet(t *testing.T, objs []*unstructured.Unstructured) *appsv1.DaemonSet {
	t.Helper()
	for _, o := range objs {
		if o.GetKind() == "DaemonSet" {
			ds := &appsv1.DaemonSet{}
			require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, ds))
			return ds
		}
	}
	t.Fatal("DaemonSet not found in rendered objects")
	return nil
}

func findVolume(t *testing.T, ds *appsv1.DaemonSet, name string) corev1.Volume {
	t.Helper()
	for _, v := range ds.Spec.Template.Spec.Volumes {
		if v.Name == name {
			return v
		}
	}
	t.Fatalf("volume %q not found", name)
	return corev1.Volume{}
}

func mountNames(mounts []corev1.VolumeMount) []string {
	names := make([]string, 0, len(mounts))
	for _, m := range mounts {
		names = append(names, m.Name)
	}
	return names
}

func tolKeys(tols []corev1.Toleration) []string {
	keys := make([]string, 0, len(tols))
	for _, tol := range tols {
		keys = append(keys, tol.Key)
	}
	return keys
}

func envMap(env []corev1.EnvVar) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		m[e.Name] = e.Value
	}
	return m
}

func findDeployment(t *testing.T, objs []*unstructured.Unstructured) *appsv1.Deployment {
	t.Helper()
	for _, o := range objs {
		if o.GetKind() == "Deployment" {
			dep := &appsv1.Deployment{}
			require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, dep))
			return dep
		}
	}
	t.Fatal("Deployment not found in rendered objects")
	return nil
}

func containerByName(t *testing.T, ds *appsv1.DaemonSet, name string) corev1.Container {
	t.Helper()
	for _, c := range ds.Spec.Template.Spec.Containers {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("container %q not found", name)
	return corev1.Container{}
}

func TestDRADriverRenderComputeDomains(t *testing.T) {
	s := newTestDRAState(t)
	cr := sampleGPUCluster()
	cr.Spec.DRADriver.ComputeDomains.Enabled = ptr.To(true)

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)

	kinds := map[string]int{}
	names := map[string]bool{}
	for _, o := range objs {
		kinds[o.GetKind()]++
		names[o.GetKind()+"/"+o.GetName()] = true
	}
	// gpu + mig + vfio + compute-domain-daemon + compute-domain-default-channel
	assert.Equal(t, 5, kinds["DeviceClass"])
	assert.True(t, names["DeviceClass/compute-domain-daemon.nvidia.com"])
	assert.True(t, names["DeviceClass/compute-domain-default-channel.nvidia.com"])
	// compute-domain controller Deployment + its SA/RBAC and the daemon SA/RBAC
	assert.Equal(t, 1, kinds["Deployment"])
	assert.True(t, names["Deployment/nvidia-dra-driver-controller"])
	assert.True(t, names["ServiceAccount/nvidia-dra-driver-controller"])
	assert.True(t, names["ServiceAccount/compute-domain-daemon-service-account"])
	assert.True(t, names["ServiceAccount/nvidia-dra-driver-kubeletplugin"])
	// kubelet-plugin computedomaincliques Role appears only when computeDomains is on
	assert.True(t, names["Role/nvidia-dra-driver-kubeletplugin"])

	// DaemonSet now hosts both containers.
	ds := findDaemonSet(t, objs)
	require.Len(t, ds.Spec.Template.Spec.Containers, 2)
	cnames := make([]string, 0, 2)
	for _, c := range ds.Spec.Template.Spec.Containers {
		cnames = append(cnames, c.Name)
	}
	assert.ElementsMatch(t, []string{"gpus", "compute-domains"}, cnames)
}

func TestDRADriverComputeDomainsContainerAndController(t *testing.T) {
	s := newTestDRAState(t)
	cr := sampleGPUCluster()
	cr.Spec.DRADriver.ComputeDomains.Enabled = ptr.To(true)

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)

	ds := findDaemonSet(t, objs)
	cd := containerByName(t, ds, "compute-domains")
	assert.Equal(t, "nvcr.io/nvidia/k8s-dra-driver-gpu:v0.1.0", cd.Image)
	args := strings.Join(cd.Args, "\n")
	assert.Contains(t, args, "source /run/nvidia/validations/driver-ready")
	assert.Contains(t, args, "exec compute-domain-kubelet-plugin")
	assert.Equal(t, "51515", envMap(cd.Env)["HEALTHCHECK_PORT"])

	dep := findDeployment(t, objs)
	depCtr := dep.Spec.Template.Spec.Containers[0]
	assert.Equal(t, "nvcr.io/nvidia/k8s-dra-driver-gpu:v0.1.0", depCtr.Image)
	assert.Equal(t, "nvidia-dra-driver-controller", dep.Spec.Template.Spec.ServiceAccountName)
	depEnv := envMap(depCtr.Env)
	assert.Equal(t, "false", depEnv["LEADER_ELECTION_ENABLED"])
	assert.Equal(t, "test-operator", depEnv["LEADER_ELECTION_LEASE_LOCK_NAMESPACE"])
}

func TestDRADriverDaemonsetsLabels(t *testing.T) {
	s := newTestDRAState(t)
	cr := sampleGPUCluster()
	cr.Spec.Daemonsets.Labels = map[string]string{"team": "platform", "env": "test"}

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)

	ds := findDaemonSet(t, objs)
	assert.Equal(t, "platform", ds.Labels["team"])
	assert.Equal(t, "test", ds.Labels["env"])
	assert.Equal(t, "platform", ds.Spec.Template.Labels["team"])
	assert.Equal(t, "test", ds.Spec.Template.Labels["env"])
}

func TestDRADriverDaemonsetsAnnotations(t *testing.T) {
	s := newTestDRAState(t)
	cr := sampleGPUCluster()
	cr.Spec.Daemonsets.Annotations = map[string]string{"prometheus.io/scrape": "true"}

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)

	ds := findDaemonSet(t, objs)
	assert.Equal(t, "true", ds.Annotations["prometheus.io/scrape"])
	assert.Equal(t, "true", ds.Spec.Template.Annotations["prometheus.io/scrape"])
}

func TestDRADriverControllerInheritsGlobalConfig(t *testing.T) {
	s := newTestDRAState(t)
	cr := sampleGPUCluster()
	cr.Spec.DRADriver.ComputeDomains.Enabled = ptr.To(true)
	cr.Spec.Daemonsets.Labels = map[string]string{"team": "platform"}
	cr.Spec.Daemonsets.Annotations = map[string]string{"monitoring": "enabled"}
	cr.Spec.Daemonsets.Tolerations = []corev1.Toleration{{
		Key:      "nvidia.com/gpu",
		Operator: corev1.TolerationOpExists,
		Effect:   corev1.TaintEffectNoSchedule,
	}}
	cr.Spec.Daemonsets.PriorityClassName = "global-priority"

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)

	dep := findDeployment(t, objs)
	// Global labels/annotations appear on both Deployment metadata and pod template.
	assert.Equal(t, "platform", dep.Labels["team"])
	assert.Equal(t, "platform", dep.Spec.Template.Labels["team"])
	assert.Equal(t, "enabled", dep.Annotations["monitoring"])
	assert.Equal(t, "enabled", dep.Spec.Template.Annotations["monitoring"])

	// Global tolerations appear alongside the hardcoded control-plane tolerations.
	assert.Contains(t, tolKeys(dep.Spec.Template.Spec.Tolerations), "nvidia.com/gpu")
	assert.Contains(t, tolKeys(dep.Spec.Template.Spec.Tolerations), "node-role.kubernetes.io/control-plane")

	// priorityClassName comes from the shared daemonsets spec.
	assert.Equal(t, "global-priority", dep.Spec.Template.Spec.PriorityClassName)
}

func TestDRADriverKubeletPluginScheduling(t *testing.T) {
	s := newTestDRAState(t)
	cr := sampleGPUCluster()
	cr.Spec.DRADriver.ComputeDomains.Enabled = ptr.To(true)
	cr.Spec.Daemonsets.Tolerations = []corev1.Toleration{{Key: "ds-tol", Operator: corev1.TolerationOpExists}}
	cr.Spec.Daemonsets.PriorityClassName = "ds-priority"

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)
	ds := findDaemonSet(t, objs)

	// priorityClassName and tolerations come from the shared daemonsets spec.
	assert.Equal(t, "ds-priority", ds.Spec.Template.Spec.PriorityClassName)
	assert.Subset(t, tolKeys(ds.Spec.Template.Spec.Tolerations), []string{"ds-tol"})
	// The kubelet-plugin gates on the deploy label so k8s-driver-manager can evict it.
	assert.Equal(t, "true", ds.Spec.Template.Spec.NodeSelector["nvidia.com/gpu.deploy.dra-driver"])
}
