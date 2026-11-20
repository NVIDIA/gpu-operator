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
	"fmt"
	"maps"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
		dp, dra, invalid           bool
	}{
		"sole CP":          {cp: true, want: coexistence.DP, dp: true},
		"sole GC":          {gc: true, want: coexistence.DRA, dra: true},
		"both default DP":  {cp: true, gc: true, want: coexistence.DP, dp: true},
		"both default DRA": {cp: true, gc: true, defaultStack: coexistence.DRA, want: coexistence.DRA, dra: true},
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
	node := selectedNode("")
	c := lifecycleClient(t, cp, node)
	pending, err := sourceAdoptionPending(t.Context(), c, cp)
	require.NoError(t, err)
	require.True(t, pending)
	nlc := &nodeLabelingController{client: c, clusterPolicy: cp, logger: logr.Discard()}
	_, err = nlc.labelGPUNodes(t.Context())
	require.NoError(t, err)
	pending, err = sourceAdoptionPending(t.Context(), c, cp)
	require.NoError(t, err)
	require.False(t, pending)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(node), node))
	node.Labels[draDriverDeployLabelKey] = "false"
	require.NoError(t, c.Update(t.Context(), node))
	pending, err = sourceAdoptionPending(t.Context(), c, cp)
	require.NoError(t, err)
	require.True(t, pending)
	delete(node.Labels, draDriverDeployLabelKey)
	node.Labels[devicePluginDeployLabelKey] = "false"
	require.NoError(t, c.Update(t.Context(), node))
	pending, err = sourceAdoptionPending(t.Context(), c, cp)
	require.NoError(t, err)
	require.False(t, pending)
	node.Labels[coexistence.ModeLabel] = coexistence.DRA
	delete(node.Annotations, coexistence.SelectionAnnotation)
	require.NoError(t, c.Update(t.Context(), node))
	pending, err = sourceAdoptionPending(t.Context(), c, cp)
	require.NoError(t, err)
	require.False(t, pending, "intentional wait must not block CP adoption")
}

func TestDRAClaimsCannotLeaveBothAllocatorsEnabled(t *testing.T) {
	claim := &resourcev1.ResourceClaim{ObjectMeta: metav1.ObjectMeta{Name: "claim", Namespace: "test"}, Status: resourcev1.ResourceClaimStatus{Allocation: &resourcev1.AllocationResult{Devices: resourcev1.DeviceAllocationResult{Results: []resourcev1.DeviceRequestAllocationResult{{Driver: NVIDIAGPUDRADriverName}}}}}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "consumer", Namespace: "test"}, Spec: corev1.PodSpec{NodeName: "node", ResourceClaims: []corev1.PodResourceClaim{{Name: "gpu", ResourceClaimName: new("claim")}}}}
	c := lifecycleClient(t, claim, pod)
	nlc := &nodeLabelingController{client: c, clusterPolicy: testSourceCP(), gpuCluster: testSourceGC(), logger: logr.Discard()}
	node := selectedNode(coexistence.DP)
	node.Labels[draDriverDeployLabelKey] = "true"
	node.Labels[devicePluginDeployLabelKey] = "true"
	_, err := nlc.selectNode(t.Context(), node)
	require.NoError(t, err)
	require.Equal(t, "true", node.Labels[draDriverDeployLabelKey])
	require.NotContains(t, node.Labels, devicePluginDeployLabelKey)
	require.True(t, nlc.draPluginRemovalDeferred)
	require.NoError(t, c.Delete(t.Context(), pod))
	_, err = nlc.selectNode(t.Context(), node)
	require.NoError(t, err)
	require.NotContains(t, node.Labels, draDriverDeployLabelKey)
	require.Equal(t, "true", node.Labels[devicePluginDeployLabelKey])
}

func TestSelectionPatchRejectsConcurrentModeChange(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node", Labels: map[string]string{commonGPULabelKey: "true", "feature.node.kubernetes.io/pci-10de.present": "true"}}}
	cp := testSourceCP()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).WithInterceptorFuncs(interceptor.Funcs{Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
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
	kata := getEffectiveStateLabels(gpuWorkloadConfigVMPassthrough, string(gpuv1.Kata))
	require.Contains(t, kata, kataDevicePluginDeployLabelKey)
	require.Equal(t, original, gpuStateLabels[gpuWorkloadConfigVMPassthrough])
}

func TestUpgradeDoesNotWithdrawOperandsBeforeFinalizerRegistration(t *testing.T) {
	for _, stack := range []string{coexistence.DP, coexistence.DRA} {
		t.Run(stack, func(t *testing.T) {
			nlc := &nodeLabelingController{client: lifecycleClient(t)}
			key := devicePluginDeployLabelKey
			if stack == coexistence.DP {
				nlc.clusterPolicy = testSourceCP()
				nlc.clusterPolicy.Finalizers = nil
			} else {
				nlc.gpuCluster = testSourceGC()
				nlc.gpuCluster.Finalizers = nil
				key = draDriverDeployLabelKey
			}
			node := selectedNode("")
			node.Labels[key] = "true"
			changed, err := nlc.selectNode(t.Context(), node)
			require.NoError(t, err)
			require.False(t, changed)
			require.Equal(t, "true", node.Labels[key])
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
	for _, discovered := range []bool{true, false} {
		t.Run(fmt.Sprint(discovered), func(t *testing.T) {
			cp := testSourceCP()
			gc := testSourceGC()
			node := selectedNode(coexistence.DP)
			node.Labels[draDriverDeployLabelKey] = "true"
			if !discovered {
				delete(node.Labels, "feature.node.kubernetes.io/pci-10de.present")
				node.Labels[commonGPULabelKey] = "false"
			}
			c := lifecycleClient(t, cp, gc, node)
			pending, err := sourceAdoptionPending(t.Context(), c, gc)
			require.NoError(t, err)
			require.True(t, pending, "DRA must not start on stale labels outside its selected pool")
			delete(node.Labels, draDriverDeployLabelKey)
			require.NoError(t, c.Update(t.Context(), node))
			pending, err = sourceAdoptionPending(t.Context(), c, gc)
			require.NoError(t, err)
			require.False(t, pending)
		})
	}
}
