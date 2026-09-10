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

package fake

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

// gpuClusterName is the only name the CRD's CEL singleton rule allows. The fake
// performs no CEL validation, so the list and selector cases legitimately hold
// several objects derived from it at once.
const gpuClusterName = "gpu-cluster"

// newGPUCluster builds a GPUCluster whose spec exercises every kind of field
// the generated DeepCopyInto handles separately: a scalar, a slice, a map, a
// nested struct and a nested *bool.
func newGPUCluster(name string, labels map[string]string) *nvidiav1alpha1.GPUCluster {
	return &nvidiav1alpha1.GPUCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Spec: nvidiav1alpha1.GPUClusterSpec{
			DRADriver: nvidiav1alpha1.DRADriverSpec{
				Repository:       "nvcr.io/nvidia/cloud-native",
				Image:            "k8s-dra-driver-gpu",
				Version:          "v25.3.0",
				ImagePullPolicy:  "IfNotPresent",
				ImagePullSecrets: []string{"ngc-secret"},
				FeatureGates:     map[string]bool{"NvidiaDRADriver": true},
				GPUs: nvidiav1alpha1.DRADriverGPUsSpec{
					KubeletPlugin: nvidiav1alpha1.DRADriverKubeletPluginSpec{
						Healthcheck: &nvidiav1alpha1.DRADriverHealthcheckSpec{
							Enabled: new(true),
							Port:    new(int32(51516)),
						},
					},
				},
				ComputeDomains: nvidiav1alpha1.DRADriverComputeDomainsSpec{
					Enabled: new(false),
				},
			},
		},
	}
}

// gpuClusterUnderTest binds the shared verb matrix (see shared_verb_test.go) to
// the generated fake GPUCluster client.
var gpuClusterUnderTest = resourceUnderTest[*nvidiav1alpha1.GPUCluster, *nvidiav1alpha1.GPUClusterList]{
	groupVersionResource: gpuClusterGVR,
	groupVersionKind:     gpuClusterGVK,
	objectName:           gpuClusterName,

	newClient: func(fixture *fakeGroupFixture) typedResourceClient[*nvidiav1alpha1.GPUCluster, *nvidiav1alpha1.GPUClusterList] {
		return fixture.groupClient.GPUClusters()
	},
	newObject: newGPUCluster,
	items: func(gpuClusterList *nvidiav1alpha1.GPUClusterList) []*nvidiav1alpha1.GPUCluster {
		gpuClusters := make([]*nvidiav1alpha1.GPUCluster, 0, len(gpuClusterList.Items))
		for itemIndex := range gpuClusterList.Items {
			gpuClusters = append(gpuClusters, &gpuClusterList.Items[itemIndex])
		}
		return gpuClusters
	},
}

func TestGPUClustersSharedVerbs(t *testing.T) {
	runSharedVerbTests(t, gpuClusterUnderTest)
}

// TestGPUClustersSpecRoundTrip is the one case that depends on the GPUCluster
// schema: the spec the caller submitted has to come back unchanged, pointers,
// slices and maps included.
func TestGPUClustersSpecRoundTrip(t *testing.T) {
	fixture := newFakeGroupFixture(t)
	client := fixture.groupClient.GPUClusters()
	ctx := t.Context()

	// expectedSpec must come from a separate object: comparing against the
	// submitted object's own spec would move with any in-place mutation the
	// client made to the caller's object, and pass vacuously.
	expectedSpec := newGPUCluster(gpuClusterName, nil).Spec

	submittedCluster := newGPUCluster(gpuClusterName, nil)
	createdCluster, err := client.Create(ctx, submittedCluster, metav1.CreateOptions{})
	require.NoError(t, err)
	assert.Equal(t, expectedSpec, createdCluster.Spec)

	gotCluster, err := client.Get(ctx, gpuClusterName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, expectedSpec, gotCluster.Spec)
}
