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
	"errors"
	"testing"

	"github.com/NVIDIA/k8s-operator-libs/pkg/upgrade"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

// mergeLabels merges multiple label maps into one (last write wins).
func mergeLabels(maps ...map[string]string) map[string]string {
	out := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func TestNodeLabelingReconcileDefersDependentOperationsAfterGPULabelChanges(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, gpuv1.AddToScheme(scheme))
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))

	clusterPolicy := &gpuv1.ClusterPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-policy"},
		Spec: gpuv1.ClusterPolicySpec{
			Driver: gpuv1.DriverSpec{
				UseNvidiaDriverCRD: ptr.To(true),
			},
		},
	}
	driver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: consts.DefaultNVIDIADriverName},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			Default: true,
		},
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gpu-node",
			Labels: map[string]string{
				"feature.node.kubernetes.io/pci-10de.present": "true",
			},
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(clusterPolicy, driver, node).
		Build()

	reconciler := &NodeLabelingReconciler{
		Client:    fakeClient,
		Namespace: "test-ns",
		Log:       logr.Discard(),
	}

	result, err := reconciler.Reconcile(ctx, reconcile.Request{})
	require.NoError(t, err)
	assert.Zero(t, result.RequeueAfter)

	updatedNode := &corev1.Node{}
	require.NoError(t, fakeClient.Get(ctx, types.NamespacedName{Name: "gpu-node"}, updatedNode))
	assert.Equal(t, commonGPULabelValue, updatedNode.Labels[commonGPULabelKey])
	assert.NotContains(t, updatedNode.Labels, consts.NVIDIADriverOwnerLabel)

	result, err = reconciler.Reconcile(ctx, reconcile.Request{})
	require.NoError(t, err)
	assert.Zero(t, result.RequeueAfter)

	require.NoError(t, fakeClient.Get(ctx, types.NamespacedName{Name: "gpu-node"}, updatedNode))
	assert.Equal(t, consts.DefaultNVIDIADriverName, updatedNode.Labels[consts.NVIDIADriverOwnerLabel])
}

func TestNodeLabelingReconcileDoesNotDeferDependentOperationsForStateLabelChanges(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, gpuv1.AddToScheme(scheme))
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))

	clusterPolicy := &gpuv1.ClusterPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-policy"},
		Spec: gpuv1.ClusterPolicySpec{
			Driver: gpuv1.DriverSpec{
				UseNvidiaDriverCRD: ptr.To(true),
			},
		},
	}
	driver := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: consts.DefaultNVIDIADriverName},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			Default: true,
		},
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gpu-node",
			Labels: map[string]string{
				"feature.node.kubernetes.io/pci-10de.present": "true",
				commonGPULabelKey: commonGPULabelValue,
			},
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(clusterPolicy, driver, node).
		Build()

	reconciler := &NodeLabelingReconciler{
		Client:    fakeClient,
		Namespace: "test-ns",
		Log:       logr.Discard(),
	}

	result, err := reconciler.Reconcile(ctx, reconcile.Request{})
	require.NoError(t, err)
	assert.Zero(t, result.RequeueAfter)

	updatedNode := &corev1.Node{}
	require.NoError(t, fakeClient.Get(ctx, types.NamespacedName{Name: "gpu-node"}, updatedNode))
	assert.Equal(t, "true", updatedNode.Labels["nvidia.com/gpu.deploy.driver"])
	assert.Equal(t, consts.DefaultNVIDIADriverName, updatedNode.Labels[consts.NVIDIADriverOwnerLabel])
}

func TestNodeLabelUpdateReasonsDetectsLabelChanges(t *testing.T) {
	tests := []struct {
		name   string
		old    map[string]string
		new    map[string]string
		assert func(*testing.T, nodeLabelUpdateReasons)
	}{
		{
			name: "GPU common label changed",
			old: map[string]string{
				"feature.node.kubernetes.io/pci-10de.present": "true",
			},
			new: map[string]string{
				"feature.node.kubernetes.io/pci-10de.present": "true",
				commonGPULabelKey: commonGPULabelValue,
			},
			assert: func(t *testing.T, reasons nodeLabelUpdateReasons) {
				assert.True(t, reasons.gpuCommonLabelChanged)
			},
		},
		{
			name: "MIG capable label changed",
			old:  map[string]string{},
			new: map[string]string{
				migCapableLabelKey: migCapableLabelValue,
			},
			assert: func(t *testing.T, reasons nodeLabelUpdateReasons) {
				assert.True(t, reasons.migCapableLabelChanged)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reasons := getNodeLabelUpdateReasons(tc.old, tc.new)

			tc.assert(t, reasons)
			assert.True(t, reasons.needsUpdate())
		})
	}
}

func TestLabelGPUNodesReturnsPartialUpdateCountOnPatchError(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	patchErr := errors.New("patch failed")
	patchCalls := 0
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			&corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gpu-node-a",
					Labels: map[string]string{
						"feature.node.kubernetes.io/pci-10de.present": "true",
					},
				},
			},
			&corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gpu-node-b",
					Labels: map[string]string{
						"feature.node.kubernetes.io/pci-10de.present": "true",
					},
				},
			},
		).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				patchCalls++
				if patchCalls == 2 {
					return patchErr
				}
				return c.Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()

	nlc := &nodeLabelingController{
		client:        fakeClient,
		clusterPolicy: &gpuv1.ClusterPolicy{},
		logger:        logr.Discard(),
	}

	result, err := nlc.labelGPUNodes(ctx)
	require.ErrorIs(t, err, patchErr)
	assert.Equal(t, 1, result.totalPatchedNodeCount)
	assert.Equal(t, 1, result.gpuDiscoveryStateChangedNodeCount)
}

func TestReconcileCommonGPULabel(t *testing.T) {
	tests := []struct {
		description    string
		initialLabels  map[string]string
		expectedLabels map[string]string
	}{
		{
			description:    "empty",
			initialLabels:  map[string]string{},
			expectedLabels: map[string]string{},
		},
		{
			description: "GPU PCI label present, common GPU label missing",
			initialLabels: map[string]string{
				"feature.node.kubernetes.io/pci-10de.present": "true",
			},
			expectedLabels: map[string]string{
				"feature.node.kubernetes.io/pci-10de.present": "true",
				commonGPULabelKey: "true",
			},
		},
		{
			description: "GPU PCI label present, common GPU label is false",
			initialLabels: map[string]string{
				"feature.node.kubernetes.io/pci-10de.present": "true",
				commonGPULabelKey: "false",
			},
			expectedLabels: map[string]string{
				"feature.node.kubernetes.io/pci-10de.present": "true",
				commonGPULabelKey: "true",
			},
		},
		{
			description:    "GPU PCI label present, common GPU label is true",
			initialLabels:  map[string]string{commonGPULabelKey: "true"},
			expectedLabels: map[string]string{commonGPULabelKey: "false"},
		},
		{
			description: "GPU PCI label present, common GPU label is true",
			initialLabels: map[string]string{
				"feature.node.kubernetes.io/pci-10de.present": "true",
				commonGPULabelKey: "true",
			},
			expectedLabels: map[string]string{
				"feature.node.kubernetes.io/pci-10de.present": "true",
				commonGPULabelKey: "true",
			},
		},
		{
			description:    "GPU PCI label missing, common GPU label is false",
			initialLabels:  map[string]string{commonGPULabelKey: "false"},
			expectedLabels: map[string]string{commonGPULabelKey: "false"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			nlc := &nodeLabelingController{
				clusterPolicy: &gpuv1.ClusterPolicy{},
				logger:        logr.Discard(),
			}
			labels := tc.initialLabels
			nlc.reconcileCommonGPULabel(labels, "test-node")
			assert.Equal(t, tc.expectedLabels, labels)
		})
	}
}

func TestUpdateGPUStateLabels(t *testing.T) {
	tests := []struct {
		name           string
		clusterPolicy  *gpuv1.ClusterPolicy
		initialLabels  map[string]string
		expectedLabels map[string]string
	}{
		{
			name:           "empty",
			clusterPolicy:  &gpuv1.ClusterPolicy{},
			initialLabels:  map[string]string{},
			expectedLabels: map[string]string{},
		},
		{
			name:          "no common GPU label, all state labels removed, non-state labels preserved",
			clusterPolicy: &gpuv1.ClusterPolicy{},
			initialLabels: map[string]string{
				"nvidia.com/gpu.deploy.driver":        "true",
				"nvidia.com/gpu.deploy.device-plugin": "true",
				"foo":                                 "bar",
			},
			expectedLabels: map[string]string{
				"foo": "bar",
			},
		},
		{
			name:          "container workload, container state labels applied",
			clusterPolicy: &gpuv1.ClusterPolicy{},
			initialLabels: map[string]string{commonGPULabelKey: commonGPULabelValue},
			expectedLabels: mergeLabels(
				map[string]string{commonGPULabelKey: commonGPULabelValue},
				gpuStateLabels[gpuWorkloadConfigContainer],
			),
		},
		{
			name:          "operands disabled, all state labels removed",
			clusterPolicy: &gpuv1.ClusterPolicy{},
			initialLabels: mergeLabels(
				map[string]string{
					commonGPULabelKey:      commonGPULabelValue,
					commonOperandsLabelKey: "false",
				},
				gpuStateLabels[gpuWorkloadConfigContainer],
			),
			expectedLabels: map[string]string{
				commonGPULabelKey:      commonGPULabelValue,
				commonOperandsLabelKey: "false",
			},
		},
		{
			name: "sandboxWorkloads enabled, mode=kubevirt, workloadConfig=passthrough",
			clusterPolicy: &gpuv1.ClusterPolicy{
				Spec: gpuv1.ClusterPolicySpec{
					SandboxWorkloads: gpuv1.SandboxWorkloadsSpec{
						Enabled: ptr.To(true),
						Mode:    string(gpuv1.KubeVirt),
					},
				},
			},
			initialLabels: map[string]string{
				commonGPULabelKey:         commonGPULabelValue,
				gpuWorkloadConfigLabelKey: gpuWorkloadConfigVMPassthrough,
			},
			expectedLabels: mergeLabels(
				map[string]string{
					commonGPULabelKey:         commonGPULabelValue,
					gpuWorkloadConfigLabelKey: gpuWorkloadConfigVMPassthrough,
				},
				getEffectiveStateLabels(gpuWorkloadConfigVMPassthrough, string(gpuv1.KubeVirt)),
			),
		},
		{
			name: "sandboxWorkloads enabled, mode=kubevirt, workloadConfig=vm-vgpu",
			clusterPolicy: &gpuv1.ClusterPolicy{
				Spec: gpuv1.ClusterPolicySpec{
					SandboxWorkloads: gpuv1.SandboxWorkloadsSpec{
						Enabled: ptr.To(true),
						Mode:    string(gpuv1.KubeVirt),
					},
				},
			},
			initialLabels: map[string]string{
				commonGPULabelKey:         commonGPULabelValue,
				gpuWorkloadConfigLabelKey: gpuWorkloadConfigVMVgpu,
			},
			expectedLabels: mergeLabels(
				map[string]string{
					commonGPULabelKey:         commonGPULabelValue,
					gpuWorkloadConfigLabelKey: gpuWorkloadConfigVMVgpu,
				},
				getEffectiveStateLabels(gpuWorkloadConfigVMVgpu, string(gpuv1.KubeVirt)),
			),
		},
		{
			name: "sandboxWorkloads enabled, mode=kata, workloadConfig=passthrough",
			clusterPolicy: &gpuv1.ClusterPolicy{
				Spec: gpuv1.ClusterPolicySpec{
					SandboxWorkloads: gpuv1.SandboxWorkloadsSpec{
						Enabled: ptr.To(true),
						Mode:    string(gpuv1.Kata),
					},
				},
			},
			initialLabels: map[string]string{
				commonGPULabelKey:         commonGPULabelValue,
				gpuWorkloadConfigLabelKey: gpuWorkloadConfigVMPassthrough,
			},
			expectedLabels: mergeLabels(
				map[string]string{
					commonGPULabelKey:         commonGPULabelValue,
					gpuWorkloadConfigLabelKey: gpuWorkloadConfigVMPassthrough,
				},
				getEffectiveStateLabels(gpuWorkloadConfigVMPassthrough, string(gpuv1.Kata)),
			),
		},
		{
			name: "sandboxWorkloads enabled, mode=kata, workloadConfig=vm-vgpu",
			clusterPolicy: &gpuv1.ClusterPolicy{
				Spec: gpuv1.ClusterPolicySpec{
					SandboxWorkloads: gpuv1.SandboxWorkloadsSpec{
						Enabled: ptr.To(true),
						Mode:    string(gpuv1.Kata),
					},
				},
			},
			initialLabels: map[string]string{
				commonGPULabelKey:         commonGPULabelValue,
				gpuWorkloadConfigLabelKey: gpuWorkloadConfigVMVgpu,
			},
			expectedLabels: mergeLabels(
				map[string]string{
					commonGPULabelKey:         commonGPULabelValue,
					gpuWorkloadConfigLabelKey: gpuWorkloadConfigVMVgpu,
				},
				getEffectiveStateLabels(gpuWorkloadConfigVMVgpu, string(gpuv1.Kata)),
			),
		},
		{
			name: "sandboxWorkloads enabled, mode=kubevirt, workloadConfig switched from container to passthrough",
			clusterPolicy: &gpuv1.ClusterPolicy{
				Spec: gpuv1.ClusterPolicySpec{
					SandboxWorkloads: gpuv1.SandboxWorkloadsSpec{
						Enabled: ptr.To(true),
						Mode:    string(gpuv1.KubeVirt),
					},
				},
			},
			initialLabels: mergeLabels(
				map[string]string{
					commonGPULabelKey:         commonGPULabelValue,
					gpuWorkloadConfigLabelKey: gpuWorkloadConfigVMPassthrough,
				},
				getEffectiveStateLabels(gpuWorkloadConfigContainer, string(gpuv1.KubeVirt)),
			),
			expectedLabels: mergeLabels(
				map[string]string{
					commonGPULabelKey:         commonGPULabelValue,
					gpuWorkloadConfigLabelKey: gpuWorkloadConfigVMPassthrough,
				},
				getEffectiveStateLabels(gpuWorkloadConfigVMPassthrough, string(gpuv1.KubeVirt)),
			),
		},
		{
			name: "MIG-capable node, MIG manager deploy label added and mig.config set to all-disabled",
			clusterPolicy: &gpuv1.ClusterPolicy{
				Spec: gpuv1.ClusterPolicySpec{
					MIGManager: gpuv1.MIGManagerSpec{
						Enabled: ptr.To(true),
						Config:  &gpuv1.MIGPartedConfigSpec{Default: migConfigDisabledValue},
					},
				},
			},
			initialLabels: map[string]string{
				commonGPULabelKey:  commonGPULabelValue,
				migCapableLabelKey: migCapableLabelValue,
			},
			expectedLabels: mergeLabels(
				map[string]string{
					commonGPULabelKey:  commonGPULabelValue,
					migCapableLabelKey: migCapableLabelValue,
					migManagerLabelKey: migManagerLabelValue,
					migConfigLabelKey:  migConfigDisabledValue,
				},
				gpuStateLabels[gpuWorkloadConfigContainer],
			),
		},
		{
			name: "MIG-capable node with existing mig.config label",
			clusterPolicy: &gpuv1.ClusterPolicy{
				Spec: gpuv1.ClusterPolicySpec{
					MIGManager: gpuv1.MIGManagerSpec{
						Enabled: ptr.To(true),
						Config:  &gpuv1.MIGPartedConfigSpec{Default: migConfigDisabledValue},
					},
				},
			},
			initialLabels: map[string]string{
				commonGPULabelKey:  commonGPULabelValue,
				migCapableLabelKey: migCapableLabelValue,
				migConfigLabelKey:  "all-1g.10gb",
			},
			expectedLabels: mergeLabels(
				map[string]string{
					commonGPULabelKey:  commonGPULabelValue,
					migCapableLabelKey: migCapableLabelValue,
					migManagerLabelKey: migManagerLabelValue,
					migConfigLabelKey:  "all-1g.10gb",
				},
				gpuStateLabels[gpuWorkloadConfigContainer],
			),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nlc := &nodeLabelingController{
				clusterPolicy: tc.clusterPolicy,
				logger:        logr.Discard(),
			}
			labels := mergeLabels(tc.initialLabels)
			nlc.updateGPUStateLabels(labels, "test-node")
			assert.Equal(t, tc.expectedLabels, labels)
		})
	}
}

func TestSetDriverAutoUpgradeAnnotation(t *testing.T) {
	tests := []struct {
		name                string
		initialAnnotations  map[string]string
		autoUpgradeEnabled  bool
		expectedAnnotations map[string]string
	}{
		{
			name:                "autoUpgrade enabled, annotation absent → annotation set",
			initialAnnotations:  nil,
			autoUpgradeEnabled:  true,
			expectedAnnotations: map[string]string{driverAutoUpgradeAnnotationKey: "true"},
		},
		{
			name:                "autoUpgrade enabled, annotation already true → no patch",
			initialAnnotations:  map[string]string{driverAutoUpgradeAnnotationKey: "true"},
			autoUpgradeEnabled:  true,
			expectedAnnotations: map[string]string{driverAutoUpgradeAnnotationKey: "true"},
		},
		{
			name:                "autoUpgrade enabled, annotation is false → annotation set to true",
			initialAnnotations:  map[string]string{driverAutoUpgradeAnnotationKey: "false"},
			autoUpgradeEnabled:  true,
			expectedAnnotations: map[string]string{driverAutoUpgradeAnnotationKey: "true"},
		},
		{
			name:                "autoUpgrade disabled, annotation present → annotation removed",
			initialAnnotations:  map[string]string{driverAutoUpgradeAnnotationKey: "true"},
			autoUpgradeEnabled:  false,
			expectedAnnotations: nil,
		},
		{
			name:                "autoUpgrade disabled, annotation absent → no patch",
			initialAnnotations:  nil,
			autoUpgradeEnabled:  false,
			expectedAnnotations: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-node",
					Annotations: tc.initialAnnotations,
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()

			nlc := &nodeLabelingController{
				client: fakeClient,
				logger: logr.Discard(),
			}

			err := nlc.setDriverAutoUpgradeAnnotation(context.Background(), node, tc.autoUpgradeEnabled)
			require.NoError(t, err)

			updated := &corev1.Node{}
			require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{Name: "test-node"}, updated))
			assert.Equal(t, tc.expectedAnnotations, updated.Annotations)
		})
	}
}

// With no ClusterPolicy, the annotation is applied from NVIDIADriver upgrade policies.
func TestApplyDriverAutoUpgradeAnnotationNoClusterPolicy(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))

	nvd := &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: "gpu-driver"},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			UpgradePolicy: &nvidiav1alpha1.DriverUpgradePolicySpec{AutoUpgrade: true},
		},
	}
	owned := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "owned-node",
		Labels: map[string]string{consts.NVIDIADriverOwnerLabel: "gpu-driver"},
	}}
	unowned := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "unowned-node"}}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(nvd, owned, unowned).Build()
	nlc := &nodeLabelingController{client: fakeClient, logger: logr.Discard()}

	require.NoError(t, nlc.applyDriverAutoUpgradeAnnotation(context.Background()))

	updated := &corev1.Node{}
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{Name: "owned-node"}, updated))
	assert.Equal(t, "true", updated.Annotations[driverAutoUpgradeAnnotationKey])
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{Name: "unowned-node"}, updated))
	assert.Empty(t, updated.Annotations[driverAutoUpgradeAnnotationKey])
}

func TestLabelNodesWithOrphanedDriverPods(t *testing.T) {
	const namespace = "test-ns"
	const driverName = "gpu-driver"

	upgradeStateLabel := upgrade.GetUpgradeStateLabelKey()

	// liveDriver returns a NVIDIADriver with no deletion timestamp.
	liveDriver := func() *nvidiav1alpha1.NVIDIADriver {
		return &nvidiav1alpha1.NVIDIADriver{
			ObjectMeta: metav1.ObjectMeta{Name: driverName},
		}
	}

	// ownedNode returns a node that carries the NVIDIADriverOwnerLabel for driverName
	// and optionally an upgrade state label.
	ownedNode := func(name, upgradeState string) *corev1.Node {
		labels := map[string]string{consts.NVIDIADriverOwnerLabel: driverName}
		if upgradeState != "" {
			labels[upgradeStateLabel] = upgradeState
		}
		return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
	}

	// orphanedPod returns a Running driver pod with no owner references on the given node.
	orphanedPod := func(name, nodeName string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    map[string]string{AppComponentLabelKey: DriverAppComponentLabelValue},
			},
			Spec:   corev1.PodSpec{NodeName: nodeName},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		}
	}

	tests := []struct {
		name string
		// objects registered in the fake client
		nvidiaDrivers []*nvidiav1alpha1.NVIDIADriver
		nodes         []*corev1.Node
		pods          []*corev1.Pod
		// expected value of upgradeStateLabel on the named node after the call;
		// empty string means the label should be absent.
		expectedUpgradeState map[string]string
	}{
		{
			name:                 "no NVIDIADriver CRs → early return, node not labeled",
			nvidiaDrivers:        nil,
			nodes:                []*corev1.Node{ownedNode("node-1", "")},
			pods:                 []*corev1.Pod{orphanedPod("pod-1", "node-1")},
			expectedUpgradeState: map[string]string{"node-1": ""},
		},
		{
			name:                 "orphaned pod on owned node, no upgrade state → labeled upgrade-required",
			nvidiaDrivers:        []*nvidiav1alpha1.NVIDIADriver{liveDriver()},
			nodes:                []*corev1.Node{ownedNode("node-1", "")},
			pods:                 []*corev1.Pod{orphanedPod("pod-1", "node-1")},
			expectedUpgradeState: map[string]string{"node-1": upgrade.UpgradeStateUpgradeRequired},
		},
		{
			name:                 "orphaned pod on owned node, upgrade-done state → labeled upgrade-required",
			nvidiaDrivers:        []*nvidiav1alpha1.NVIDIADriver{liveDriver()},
			nodes:                []*corev1.Node{ownedNode("node-1", upgrade.UpgradeStateDone)},
			pods:                 []*corev1.Pod{orphanedPod("pod-1", "node-1")},
			expectedUpgradeState: map[string]string{"node-1": upgrade.UpgradeStateUpgradeRequired},
		},
		{
			name:                 "orphaned pod on owned node, active upgrade state → not relabeled",
			nvidiaDrivers:        []*nvidiav1alpha1.NVIDIADriver{liveDriver()},
			nodes:                []*corev1.Node{ownedNode("node-1", upgrade.UpgradeStatePodRestartRequired)},
			pods:                 []*corev1.Pod{orphanedPod("pod-1", "node-1")},
			expectedUpgradeState: map[string]string{"node-1": upgrade.UpgradeStatePodRestartRequired},
		},
		{
			name:                 "orphaned pod on owned node, failed upgrade state → not relabeled",
			nvidiaDrivers:        []*nvidiav1alpha1.NVIDIADriver{liveDriver()},
			nodes:                []*corev1.Node{ownedNode("node-1", upgrade.UpgradeStateFailed)},
			pods:                 []*corev1.Pod{orphanedPod("pod-1", "node-1")},
			expectedUpgradeState: map[string]string{"node-1": upgrade.UpgradeStateFailed},
		},
		{
			name:          "pod has owner references → skipped",
			nvidiaDrivers: []*nvidiav1alpha1.NVIDIADriver{liveDriver()},
			nodes:         []*corev1.Node{ownedNode("node-1", "")},
			pods: []*corev1.Pod{func() *corev1.Pod {
				p := orphanedPod("pod-1", "node-1")
				p.OwnerReferences = []metav1.OwnerReference{{Name: "daemonset-1"}}
				return p
			}()},
			expectedUpgradeState: map[string]string{"node-1": ""},
		},
		{
			name:          "pod not in Running phase → skipped",
			nvidiaDrivers: []*nvidiav1alpha1.NVIDIADriver{liveDriver()},
			nodes:         []*corev1.Node{ownedNode("node-1", "")},
			pods: []*corev1.Pod{func() *corev1.Pod {
				p := orphanedPod("pod-1", "node-1")
				p.Status.Phase = corev1.PodPending
				return p
			}()},
			expectedUpgradeState: map[string]string{"node-1": ""},
		},
		{
			name:          "pod has no NodeName → skipped",
			nvidiaDrivers: []*nvidiav1alpha1.NVIDIADriver{liveDriver()},
			nodes:         []*corev1.Node{ownedNode("node-1", "")},
			pods: []*corev1.Pod{func() *corev1.Pod {
				p := orphanedPod("pod-1", "node-1")
				p.Spec.NodeName = ""
				return p
			}()},
			expectedUpgradeState: map[string]string{"node-1": ""},
		},
		{
			name:          "node not owned by any NVIDIADriver → not labeled",
			nvidiaDrivers: []*nvidiav1alpha1.NVIDIADriver{liveDriver()},
			nodes: []*corev1.Node{{
				ObjectMeta: metav1.ObjectMeta{Name: "node-1"}, // no NVIDIADriverOwnerLabel
			}},
			pods:                 []*corev1.Pod{orphanedPod("pod-1", "node-1")},
			expectedUpgradeState: map[string]string{"node-1": ""},
		},
		{
			name: "NVIDIADriver with deletion timestamp → node treated as unowned, not labeled",
			nvidiaDrivers: []*nvidiav1alpha1.NVIDIADriver{{
				ObjectMeta: metav1.ObjectMeta{
					Name:              driverName,
					DeletionTimestamp: ptr.To(metav1.Now()),
					Finalizers:        []string{"test-finalizer"},
				},
			}},
			nodes:                []*corev1.Node{ownedNode("node-1", "")},
			pods:                 []*corev1.Pod{orphanedPod("pod-1", "node-1")},
			expectedUpgradeState: map[string]string{"node-1": ""},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))

			var objects []client.Object
			for _, d := range tc.nvidiaDrivers {
				objects = append(objects, d)
			}
			for _, n := range tc.nodes {
				objects = append(objects, n)
			}
			for _, p := range tc.pods {
				objects = append(objects, p)
			}

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			nlc := &nodeLabelingController{
				client:    fakeClient,
				namespace: namespace,
				logger:    logr.Discard(),
			}

			require.NoError(t, nlc.labelNodesWithOrphanedDriverPods(context.Background()))

			for nodeName, expectedUpgradeState := range tc.expectedUpgradeState {
				node := &corev1.Node{}
				require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{Name: nodeName}, node))
				assert.Equal(t, expectedUpgradeState, node.Labels[upgradeStateLabel], "node %q upgrade state", nodeName)
			}
		})
	}
}

const gpuPCILabelKey = "feature.node.kubernetes.io/pci-10de.present"

func TestUpdateGPUClusterStateLabels(t *testing.T) {
	tests := []struct {
		name           string
		initialLabels  map[string]string
		expectedLabels map[string]string
		expectModified bool
	}{
		{
			name:          "GPU node gets the DRA operand deploy labels",
			initialLabels: map[string]string{commonGPULabelKey: commonGPULabelValue},
			expectedLabels: map[string]string{
				commonGPULabelKey:          commonGPULabelValue,
				driverDeployLabelKey:       "true",
				draDriverDeployLabelKey:    "true",
				draValidatorDeployLabelKey: "true",
				dcgmDeployLabelKey:         "true",
				dcgmExporterDeployLabelKey: "true",
			},
			expectModified: true,
		},
		{
			name:           "node without the common GPU label has nothing to remove",
			initialLabels:  map[string]string{"kubernetes.io/hostname": "plain"},
			expectedLabels: map[string]string{"kubernetes.io/hostname": "plain"},
			expectModified: false,
		},
		{
			name: "node that lost its GPUs has the deploy labels removed",
			initialLabels: map[string]string{
				commonGPULabelKey:          "false",
				driverDeployLabelKey:       "true",
				draDriverDeployLabelKey:    "true",
				draValidatorDeployLabelKey: "true",
				dcgmDeployLabelKey:         "true",
				dcgmExporterDeployLabelKey: "true",
			},
			expectedLabels: map[string]string{commonGPULabelKey: "false"},
			expectModified: true,
		},
		{
			name: "GPU node missing some deploy labels is converged",
			initialLabels: map[string]string{
				commonGPULabelKey:    commonGPULabelValue,
				driverDeployLabelKey: "true",
			},
			expectedLabels: map[string]string{
				commonGPULabelKey:          commonGPULabelValue,
				driverDeployLabelKey:       "true",
				draDriverDeployLabelKey:    "true",
				draValidatorDeployLabelKey: "true",
				dcgmDeployLabelKey:         "true",
				dcgmExporterDeployLabelKey: "true",
			},
			expectModified: true,
		},
		{
			name: "paused deploy labels are honored, not overwritten",
			initialLabels: map[string]string{
				commonGPULabelKey:          commonGPULabelValue,
				driverDeployLabelKey:       "false",
				draDriverDeployLabelKey:    "false",
				draValidatorDeployLabelKey: "false",
				dcgmDeployLabelKey:         "false",
				dcgmExporterDeployLabelKey: "false",
			},
			expectedLabels: map[string]string{
				commonGPULabelKey:          commonGPULabelValue,
				driverDeployLabelKey:       "false",
				draDriverDeployLabelKey:    "false",
				draValidatorDeployLabelKey: "false",
				dcgmDeployLabelKey:         "false",
				dcgmExporterDeployLabelKey: "false",
			},
			expectModified: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			labels := mergeLabels(tc.initialLabels)
			modified := updateGPUClusterStateLabels(labels)
			assert.Equal(t, tc.expectModified, modified)
			assert.Equal(t, tc.expectedLabels, labels)
		})
	}
}

func TestReconcileGPUClusterNodeLabels(t *testing.T) {
	newReconciler := func(objs ...client.Object) (*NodeLabelingReconciler, client.Client) {
		scheme := runtime.NewScheme()
		require.NoError(t, corev1.AddToScheme(scheme))
		require.NoError(t, gpuv1.AddToScheme(scheme))
		require.NoError(t, nvidiav1alpha1.AddToScheme(scheme))
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
		return &NodeLabelingReconciler{Client: c, Scheme: scheme, Log: logr.Discard()}, c
	}
	gpuNode := func() *corev1.Node {
		return &corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name:   "gpu-node",
			Labels: map[string]string{gpuPCILabelKey: "true"},
		}}
	}

	t.Run("GPUCluster present and no ClusterPolicy labels the GPU node", func(t *testing.T) {
		gc := &nvidiav1alpha1.GPUCluster{ObjectMeta: metav1.ObjectMeta{Name: "config"}}
		r, c := newReconciler(gc, gpuNode())

		_, err := r.Reconcile(context.Background(), reconcile.Request{})
		require.NoError(t, err)

		node := &corev1.Node{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "gpu-node"}, node))
		assert.Equal(t, commonGPULabelValue, node.Labels[commonGPULabelKey])
		assert.Equal(t, "true", node.Labels[driverDeployLabelKey])
		assert.Equal(t, "true", node.Labels[draDriverDeployLabelKey])
		assert.Equal(t, "true", node.Labels[dcgmDeployLabelKey])
		assert.Equal(t, "true", node.Labels[dcgmExporterDeployLabelKey])
	})

	t.Run("no ClusterPolicy and no GPUCluster leaves the node untouched", func(t *testing.T) {
		r, c := newReconciler(gpuNode())

		_, err := r.Reconcile(context.Background(), reconcile.Request{})
		require.NoError(t, err)

		node := &corev1.Node{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "gpu-node"}, node))
		assert.NotContains(t, node.Labels, commonGPULabelKey)
		assert.NotContains(t, node.Labels, draDriverDeployLabelKey)
	})
}
