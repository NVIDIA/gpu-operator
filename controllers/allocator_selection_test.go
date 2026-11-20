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
	"encoding/json"
	"maps"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/NVIDIA/gpu-operator/internal/coexistence"
)

func selectedNode(record string) *corev1.Node {
	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node", Labels: map[string]string{commonGPULabelKey: "true", "feature.node.kubernetes.io/pci-10de.present": "true"}}}
	if record != "" {
		n.Annotations = map[string]string{coexistence.SelectionAnnotation: record}
	}
	return n
}

func TestNodeSelectionPolicy(t *testing.T) {
	for name, tc := range map[string]struct {
		cp, gc                     bool
		mode                       *string
		record, defaultStack, want string
		hostRoot                   string
		dp, dra, invalid           bool
	}{
		"sole CP":          {cp: true, want: coexistence.DP, dp: true},
		"sole GC":          {gc: true, want: coexistence.DRA, dra: true},
		"both default DP":  {cp: true, gc: true, want: coexistence.DP, dp: true},
		"both default DRA": {cp: true, gc: true, defaultStack: coexistence.DRA, want: coexistence.DRA, dra: true},
		"custom CP host root blocks DRA placement":         {cp: true, gc: true, hostRoot: "/cp-root", mode: new(coexistence.DRA)},
		"custom CP host root preserves DP placement":       {cp: true, gc: true, hostRoot: "/cp-root", record: coexistence.DP, want: coexistence.DP, dp: true},
		"unavailable DRA default stays unrecorded":         {cp: true, defaultStack: coexistence.DRA},
		"unavailable DP default stays unrecorded":          {gc: true, defaultStack: coexistence.DP},
		"existing selection ignores new default":           {cp: true, gc: true, record: coexistence.DP, defaultStack: coexistence.DRA, want: coexistence.DP, dp: true},
		"sole GC replaces stale DP":                        {gc: true, record: coexistence.DP, want: coexistence.DRA, dra: true},
		"removed mode keeps DRA":                           {cp: true, gc: true, record: coexistence.DRA, want: coexistence.DRA, dra: true},
		"explicit missing source":                          {gc: true, mode: new(coexistence.DP)},
		"explicit missing source keeps older valid record": {gc: true, mode: new(coexistence.DP), record: coexistence.DRA, want: coexistence.DRA},
		"explicit repairs record":                          {gc: true, mode: new(coexistence.DRA), record: "bad", want: coexistence.DRA, dra: true},
		"repair waits for eligible source":                 {cp: true, mode: new(coexistence.DRA), record: "bad", want: "bad"},
		"invalid mode":                                     {cp: true, mode: new("bad"), invalid: true},
		"invalid record":                                   {cp: true, record: "bad", want: "bad", invalid: true},
	} {
		t.Run(name, func(t *testing.T) {
			nlc := &nodeLabelingController{client: lifecycleClient(t), logger: logr.Discard(), defaultStack: tc.defaultStack}
			if tc.cp {
				nlc.clusterPolicy = testSourceCP()
				nlc.clusterPolicy.Spec.HostPaths.RootFS = tc.hostRoot
			}
			if tc.gc {
				nlc.gpuCluster = testSourceGC()
			}
			node := selectedNode(tc.record)
			if tc.mode != nil {
				node.Labels[coexistence.ModeLabel] = *tc.mode
			}
			_, err := nlc.selectNode(t.Context(), node)
			require.Equal(t, tc.invalid, err != nil)
			require.Equal(t, tc.want, node.Annotations[coexistence.SelectionAnnotation])
			require.Equal(t, tc.dp, node.Labels[devicePluginDeployLabelKey] == "true")
			require.Equal(t, tc.dra, node.Labels[draDriverDeployLabelKey] == "true")
			changed, _ := nlc.selectNode(t.Context(), node)
			require.False(t, changed, "reconciliation must converge")
		})
	}
}

func TestDefaultChangeDiscoveryAndDisableRecovery(t *testing.T) {
	nlc := &nodeLabelingController{client: lifecycleClient(t), clusterPolicy: testSourceCP(), gpuCluster: testSourceGC(), logger: logr.Discard()}
	node := selectedNode("")
	_, err := nlc.selectNode(t.Context(), node)
	require.NoError(t, err)
	nlc.defaultStack = coexistence.DRA
	node.Labels[commonOperandsLabelKey] = "false"
	_, err = nlc.selectNode(t.Context(), node)
	require.NoError(t, err)
	require.NotContains(t, node.Labels, devicePluginDeployLabelKey)
	require.Equal(t, coexistence.DP, node.Annotations[coexistence.SelectionAnnotation])
	delete(node.Labels, commonOperandsLabelKey)
	node.Labels[commonGPULabelKey] = "false"
	_, err = nlc.selectNode(t.Context(), node)
	require.NoError(t, err)
	node.Labels[commonGPULabelKey] = "true"
	_, err = nlc.selectNode(t.Context(), node)
	require.NoError(t, err)
	require.Equal(t, "true", node.Labels[devicePluginDeployLabelKey])
	require.NotContains(t, node.Labels, draDriverDeployLabelKey)
	node.Labels[coexistence.ModeLabel] = coexistence.DRA
	node.Labels[commonOperandsLabelKey] = "false"
	_, err = nlc.selectNode(t.Context(), node)
	require.NoError(t, err)
	require.Equal(t, "true", node.Labels[draDriverDeployLabelKey], "operands=false remains CP-only")
}

func TestMissingDefaultDoesNotStick(t *testing.T) {
	nlc := &nodeLabelingController{client: lifecycleClient(t), clusterPolicy: testSourceCP(), defaultStack: coexistence.DRA, logger: logr.Discard()}
	node := selectedNode("")
	_, err := nlc.selectNode(t.Context(), node)
	require.NoError(t, err)
	require.Empty(t, node.Annotations)
	nlc.defaultStack = coexistence.DP
	_, err = nlc.selectNode(t.Context(), node)
	require.NoError(t, err)
	require.Equal(t, coexistence.DP, node.Annotations[coexistence.SelectionAnnotation])
}

func TestAdoptionWaitsForCompatiblePlacement(t *testing.T) {
	cp := testSourceCP()
	cp.Status.Conditions = nil
	unrecorded := selectedNode("")
	c := lifecycleClient(t, cp, unrecorded)
	nlc := &nodeLabelingController{client: c, clusterPolicy: cp, logger: logr.Discard()}
	_, err := nlc.labelGPUNodes(t.Context())
	require.NoError(t, err)
	adopted := &corev1.Node{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(unrecorded), adopted))
	waitingForDRA := adopted.DeepCopy()
	waitingForDRA.Labels[coexistence.ModeLabel] = coexistence.DRA
	waitingForDRA.Labels[devicePluginDeployLabelKey] = "false"
	delete(waitingForDRA.Annotations, coexistence.SelectionAnnotation)

	for name, tc := range map[string]struct {
		node           *corev1.Node
		labelOverrides map[string]string
		defaultStack   string
		wantPending    bool
	}{
		"unrecorded eligible node":    {node: unrecorded, wantPending: true},
		"unavailable default":         {node: unrecorded, defaultStack: coexistence.DRA},
		"adopted node":                {node: adopted},
		"stale DRA deploy label":      {node: adopted, labelOverrides: map[string]string{draDriverDeployLabelKey: "false"}, wantPending: true},
		"paused device plugin":        {node: adopted, labelOverrides: map[string]string{devicePluginDeployLabelKey: "false"}},
		"explicit mode waits for DRA": {node: waitingForDRA},
	} {
		t.Run(name, func(t *testing.T) {
			node := tc.node.DeepCopy()
			maps.Copy(node.Labels, tc.labelOverrides)
			c := lifecycleClient(t, cp, node)
			pending, err := sourceAdoptionPending(t.Context(), c, cp, tc.defaultStack)
			require.NoError(t, err)
			require.Equal(t, tc.wantPending, pending)
		})
	}
}

func TestAdoptionUsesClusterPolicySandboxDefault(t *testing.T) {
	for workload, tc := range map[string]struct {
		mode       gpuv1.SandboxWorkloadsMode
		deployKey  string
		staleState bool
	}{
		gpuWorkloadConfigVMPassthrough: {gpuv1.Kata, kataDevicePluginDeployLabelKey, true},
		gpuWorkloadConfigVMVgpu:        {gpuv1.KubeVirt, vgpuManagerDeployLabelKey, false},
	} {
		t.Run(workload, func(t *testing.T) {
			ctx := t.Context()
			cp := testSourceCP()
			cp.Status.Conditions = nil
			cp.Spec.SandboxWorkloads = gpuv1.SandboxWorkloadsSpec{
				Enabled:         new(true),
				DefaultWorkload: workload,
				Mode:            string(tc.mode),
			}
			node := selectedNode("")
			if tc.staleState {
				node.Annotations = map[string]string{coexistence.SelectionAnnotation: coexistence.DP}
				maps.Copy(node.Labels, gpuStateLabels[gpuWorkloadConfigContainer])
			}
			c := lifecycleClient(t, cp, node)
			pending, err := sourceAdoptionPending(ctx, c, cp, "")
			require.NoError(t, err)
			require.True(t, pending)

			nlc := &nodeLabelingController{client: c, clusterPolicy: cp, logger: logr.Discard()}
			_, err = nlc.labelGPUNodes(ctx)
			require.NoError(t, err)
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(node), node))
			require.Equal(t, "true", node.Labels[tc.deployKey])
			require.NotContains(t, node.Labels, devicePluginDeployLabelKey)
			require.NotContains(t, node.Labels, gpuWorkloadConfigLabelKey)

			pending, err = sourceAdoptionPending(ctx, c, cp, "")
			require.NoError(t, err)
			require.False(t, pending)
		})
	}
}

func TestAdoptionContinuesWithInvalidAllocatorSelection(t *testing.T) {
	for name, tc := range map[string]struct {
		stack       string
		deployKey   string
		wantPending bool
	}{
		"invalid annotation with stale device-plugin placement": {coexistence.DP, devicePluginDeployLabelKey, true},
		"invalid mode with stale DRA plugin placement":          {coexistence.DRA, draDriverDeployLabelKey, true},
		"invalid mode with stale DRA validator placement":       {coexistence.DRA, draValidatorDeployLabelKey, false},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			invalid := selectedNode("")
			invalid.Name = "invalid"
			invalid.Labels[tc.deployKey] = "true"
			recorder := events.NewFakeRecorder(1)
			nlc := &nodeLabelingController{logger: logr.Discard(), recorder: recorder, defaultStack: tc.stack}
			var source client.Object
			if tc.stack == coexistence.DP {
				nlc.clusterPolicy = testSourceCP()
				nlc.clusterPolicy.Status.Conditions = nil
				source = nlc.clusterPolicy
				invalid.Annotations = map[string]string{coexistence.SelectionAnnotation: ""}
			} else {
				nlc.gpuCluster = testSourceGC()
				source = nlc.gpuCluster
				invalid.Labels[coexistence.ModeLabel] = "invalid"
			}
			c := lifecycleClient(t, source, invalid)
			nlc.client = c
			healthy := selectedNode("")
			_, err := nlc.selectNode(ctx, healthy)
			require.NoError(t, err)
			require.NoError(t, c.Create(ctx, healthy))

			pending, err := sourceAdoptionPending(ctx, c, source, tc.stack)
			require.NoError(t, err)
			require.Equal(t, tc.wantPending, pending, "only stale placement that can still schedule must block adoption")

			_, err = nlc.labelGPUNodes(ctx)
			require.NoError(t, err)
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(invalid), invalid))
			require.NotContains(t, invalid.Labels, tc.deployKey)
			require.Len(t, recorder.Events, 1)
			require.Contains(t, <-recorder.Events, "Warning InvalidAllocatorSelection")

			ready, err := observeSourceAdoption(ctx, c, source, tc.stack)
			require.NoError(t, err)
			require.False(t, ready, "the adoption condition must be observed in another pass")
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(source), source))
			require.True(t, nodeSelectionInitializedTrue(source))
		})
	}
}

func TestDRAHandoffWaitsForClaimsAndPlugin(t *testing.T) {
	for _, driver := range []string{NVIDIAGPUDRADriverName, nvidiaComputeDomainDRADriverName} {
		t.Run(driver, func(t *testing.T) {
			ctx := t.Context()
			namespace := "gpu-operator"
			node := selectedNode(coexistence.DRA)
			node.Labels[coexistence.ModeLabel] = coexistence.DP
			node.Labels[draDriverDeployLabelKey] = "true"
			node.Labels[draValidatorDeployLabelKey] = "true"
			node.Labels[driverDeployLabelKey] = "true"

			claim := &resourcev1.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{Name: "claim", Namespace: namespace},
				Status: resourcev1.ResourceClaimStatus{Allocation: &resourcev1.AllocationResult{
					Devices: resourcev1.DeviceAllocationResult{Results: []resourcev1.DeviceRequestAllocationResult{{
						Driver: driver, AdminAccess: new(driver == NVIDIAGPUDRADriverName),
					}}},
				}},
			}
			consumer := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "validator", Namespace: namespace},
				Spec: corev1.PodSpec{NodeName: node.Name, ResourceClaims: []corev1.PodResourceClaim{{
					Name: "gpu", ResourceClaimName: &claim.Name,
				}}},
			}
			plugin := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dra-plugin", Namespace: namespace, Finalizers: []string{"test.nvidia.com/hold"},
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion: "apps/v1", Kind: "DaemonSet", Name: draKubeletPluginDaemonSetName, Controller: new(true),
					}},
				},
				Spec: corev1.PodSpec{NodeName: node.Name},
			}
			pending := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pending-dra-plugin", Namespace: namespace, OwnerReferences: plugin.OwnerReferences},
				Spec: corev1.PodSpec{Affinity: &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchFields: []corev1.NodeSelectorRequirement{{
							Key: "metadata.name", Operator: corev1.NodeSelectorOpIn, Values: []string{node.Name},
						}}}},
					},
				}}},
			}
			c := lifecycleClient(t, claim, consumer, plugin, pending)
			nlc := &nodeLabelingController{
				client: c, namespace: namespace, clusterPolicy: testSourceCP(), gpuCluster: testSourceGC(), logger: logr.Discard(),
			}

			checkWaiting := func(pluginLabelPresent bool) {
				t.Helper()
				nlc.draPluginRemovalDeferred = false
				_, err := nlc.selectNode(ctx, node)
				require.NoError(t, err)
				require.Equal(t, coexistence.DRA, node.Annotations[coexistence.SelectionAnnotation])
				require.Equal(t, "true", node.Labels[driverDeployLabelKey])
				require.NotContains(t, node.Labels, draValidatorDeployLabelKey)
				require.NotContains(t, node.Labels, devicePluginDeployLabelKey)
				if pluginLabelPresent {
					require.Equal(t, "true", node.Labels[draDriverDeployLabelKey])
				} else {
					require.NotContains(t, node.Labels, draDriverDeployLabelKey)
				}
				require.True(t, nlc.draPluginRemovalDeferred)
			}

			checkWaiting(true)
			require.NoError(t, c.Delete(ctx, consumer))
			checkWaiting(false)

			require.NoError(t, c.Delete(ctx, plugin))
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(plugin), plugin))
			require.NotNil(t, plugin.DeletionTimestamp)
			checkWaiting(false)

			plugin.Finalizers = nil
			require.NoError(t, c.Update(ctx, plugin))
			checkWaiting(false)
			require.NoError(t, c.Delete(ctx, pending))
			_, err := nlc.selectNode(ctx, node)
			require.NoError(t, err)
			require.NotContains(t, node.Labels, draDriverDeployLabelKey)
			require.Equal(t, "true", node.Labels[devicePluginDeployLabelKey])
			require.Equal(t, coexistence.DP, node.Annotations[coexistence.SelectionAnnotation])
		})
	}
}

func TestSelectionPatchRejectsConcurrentModeChange(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node", Labels: map[string]string{commonGPULabelKey: "true", "feature.node.kubernetes.io/pci-10de.present": "true"}}}
	cp := testSourceCP()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).
		WithIndex(&corev1.Pod{}, podNodeNameIndexKey, podNodeNameIndexer).WithInterceptorFuncs(interceptor.Funcs{Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
		data, err := patch.Data(obj)
		require.NoError(t, err)
		var body map[string]any
		require.NoError(t, json.Unmarshal(data, &body))
		require.NotEmpty(t, body["metadata"].(map[string]any)["resourceVersion"])
		latest := &corev1.Node{}
		require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj), latest))
		latest.Labels[coexistence.ModeLabel] = coexistence.DRA
		require.NoError(t, c.Update(ctx, latest))
		return c.Patch(ctx, obj, patch, opts...)
	}}).Build()
	nlc := &nodeLabelingController{client: c, clusterPolicy: cp, logger: logr.Discard()}
	_, err := nlc.labelGPUNodes(context.Background())
	require.True(t, apierrors.IsConflict(err))
	actual := &corev1.Node{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(node), actual))
	require.Equal(t, coexistence.DRA, actual.Labels[coexistence.ModeLabel])
	require.NotContains(t, actual.Annotations, coexistence.SelectionAnnotation)
	require.NotContains(t, actual.Labels, "nvidia.com/gpu.deploy.device-plugin")
}

func TestSandboxLabelDerivationDoesNotMutateSharedMap(t *testing.T) {
	original := maps.Clone(gpuStateLabels[gpuWorkloadConfigVMPassthrough])
	for mode, pluginLabel := range map[gpuv1.SandboxWorkloadsMode]string{
		gpuv1.Kata:     kataDevicePluginDeployLabelKey,
		gpuv1.KubeVirt: kubevirtDevicePluginDeployLabelKey,
	} {
		t.Run(string(mode), func(t *testing.T) {
			labels := getEffectiveStateLabels(gpuWorkloadConfigVMPassthrough, string(mode))
			require.Contains(t, labels, pluginLabel)
			require.Equal(t, original, gpuStateLabels[gpuWorkloadConfigVMPassthrough])
		})
	}
}

func TestUpgradeDoesNotWithdrawOperandsBeforeFinalizerRegistration(t *testing.T) {
	for stack, deployLabel := range map[string]string{
		coexistence.DP:  devicePluginDeployLabelKey,
		coexistence.DRA: draDriverDeployLabelKey,
	} {
		t.Run(stack, func(t *testing.T) {
			nlc := &nodeLabelingController{client: lifecycleClient(t)}
			if stack == coexistence.DP {
				nlc.clusterPolicy = testSourceCP()
				nlc.clusterPolicy.Finalizers = nil
			} else {
				nlc.gpuCluster = testSourceGC()
				nlc.gpuCluster.Finalizers = nil
			}
			node := selectedNode("")
			node.Labels[deployLabel] = "true"
			changed, err := nlc.selectNode(t.Context(), node)
			require.NoError(t, err)
			require.False(t, changed)
			require.Equal(t, "true", node.Labels[deployLabel])
			require.NotContains(t, node.Annotations, coexistence.SelectionAnnotation)
			fresh := selectedNode("")
			changed, err = nlc.selectNode(t.Context(), fresh)
			require.NoError(t, err)
			require.False(t, changed)
			require.NotContains(t, fresh.Annotations, coexistence.SelectionAnnotation)
		})
	}
}

func TestAdoptionBlocksStaleLabelsOutsideSelectedPool(t *testing.T) {
	for name, discovered := range map[string]bool{
		"discovered node selected for device plugin": true,
		"node no longer discovered":                  false,
	} {
		t.Run(name, func(t *testing.T) {
			cp := testSourceCP()
			gc := testSourceGC()
			node := selectedNode(coexistence.DP)
			node.Labels[coexistence.ModeLabel] = coexistence.DP
			node.Labels[draDriverDeployLabelKey] = "true"
			if !discovered {
				delete(node.Labels, "feature.node.kubernetes.io/pci-10de.present")
				node.Labels[commonGPULabelKey] = "false"
			}
			c := lifecycleClient(t, cp, gc, node)
			pending, err := sourceAdoptionPending(t.Context(), c, gc, "")
			require.NoError(t, err)
			require.True(t, pending, "DRA must not start on stale labels outside its selected pool")
			delete(node.Labels, draDriverDeployLabelKey)
			require.NoError(t, c.Update(t.Context(), node))
			pending, err = sourceAdoptionPending(t.Context(), c, gc, "")
			require.NoError(t, err)
			require.False(t, pending)
		})
	}
}
