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

package v1alpha1

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

// gpuClusterResource is the plural resource name passed to newGPUClusters.
const gpuClusterResource = "gpuclusters"

// gpuClusterSingletonName is the only name the CRD's CEL rule admits. No CEL
// validation runs at this client layer, but the fixtures use the real name.
const gpuClusterSingletonName = "gpu-cluster"

func gpuClusterNamedPath(name string, subresources ...string) string {
	return resourceNamedPath(gpuClusterResource, name, subresources...)
}

// newGPUClusterServerAndClient stands up a recording API server and returns the typed
// GPUCluster client wired to it.
func newGPUClusterServerAndClient(t *testing.T, handler http.HandlerFunc) (*recordingServer, GPUClusterInterface) {
	t.Helper()

	server, client := newRecordingServerAndClient(t, handler)
	return server, client.GPUClusters()
}

// newGPUCluster builds a fully typed GPUCluster, including the TypeMeta the
// client-side decoder needs to recognize the payload.
func newGPUCluster(name string) *nvidiav1alpha1.GPUCluster {
	apiVersion, kind := nvidiav1alpha1.SchemeGroupVersion.WithKind(nvidiav1alpha1.GPUClusterCRDName).ToAPIVersionAndKind()
	return &nvidiav1alpha1.GPUCluster{
		TypeMeta: metav1.TypeMeta{APIVersion: apiVersion, Kind: kind},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			ResourceVersion: "7",
			Labels:          map[string]string{"app": "gpu-cluster"},
		},
		Spec: nvidiav1alpha1.GPUClusterSpec{
			DRADriver: nvidiav1alpha1.DRADriverSpec{
				Repository:   "nvcr.io/nvidia/cloud-native",
				Image:        "k8s-dra-driver-gpu",
				Version:      "v25.3.0",
				FeatureGates: map[string]bool{"ComputeDomains": true},
			},
		},
	}
}

// newGPUClusterList builds a decodable list payload holding items.
func newGPUClusterList(items ...*nvidiav1alpha1.GPUCluster) *nvidiav1alpha1.GPUClusterList {
	apiVersion, kind := nvidiav1alpha1.SchemeGroupVersion.WithKind(nvidiav1alpha1.GPUClusterCRDName + "List").ToAPIVersionAndKind()
	gpuClusterList := &nvidiav1alpha1.GPUClusterList{
		TypeMeta: metav1.TypeMeta{APIVersion: apiVersion, Kind: kind},
	}
	for _, item := range items {
		gpuClusterList.Items = append(gpuClusterList.Items, *item)
	}
	return gpuClusterList
}

// TestGPUClustersHTTPPlumbing runs the verb tests every typed client in this
// group shares against the generated GPUCluster client. Everything that
// depends on the GPUCluster schema lives in the focused tests below.
func TestGPUClustersHTTPPlumbing(t *testing.T) {
	runSharedVerbTests(t, resourceFixture[*nvidiav1alpha1.GPUCluster, *nvidiav1alpha1.GPUClusterList, GPUClusterInterface]{
		resource:      gpuClusterResource,
		objectName:    gpuClusterSingletonName,
		labelSelector: "app=gpu-cluster",
		fieldSelector: "metadata.name=gpu-cluster",

		clientFor: (*NvidiaV1alpha1Client).GPUClusters,
		newObject: newGPUCluster,
		newList:   newGPUClusterList,
		items: func(gpuClusterList *nvidiav1alpha1.GPUClusterList) []*nvidiav1alpha1.GPUCluster {
			gpuClusters := make([]*nvidiav1alpha1.GPUCluster, 0, len(gpuClusterList.Items))
			for itemIndex := range gpuClusterList.Items {
				gpuClusters = append(gpuClusters, &gpuClusterList.Items[itemIndex])
			}
			return gpuClusters
		},
	})
}

func TestGPUClustersRequestPaths(t *testing.T) {
	// Spelled out literally: the shared verb tests derive their expectations
	// from the same helpers the client uses, so this pins the actual URLs.
	assert.Equal(t, "/apis/nvidia.com/v1alpha1/gpuclusters", resourceCollectionPath(gpuClusterResource))
	assert.Equal(t, "/apis/nvidia.com/v1alpha1/gpuclusters/gpu-cluster", gpuClusterNamedPath(gpuClusterSingletonName))
	assert.Equal(t, "/apis/nvidia.com/v1alpha1/gpuclusters/gpu-cluster/status", gpuClusterNamedPath(gpuClusterSingletonName, "status"))

	server, client := newGPUClusterServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(responseWriter, http.StatusOK, newGPUCluster(gpuClusterSingletonName))
	})

	_, err := client.Get(t.Context(), gpuClusterSingletonName, metav1.GetOptions{})
	require.NoError(t, err)

	assert.Equal(t, "/apis/nvidia.com/v1alpha1/gpuclusters/gpu-cluster", server.onlyRequest(t).Path)
}

func TestGPUClustersGetDecodesSpec(t *testing.T) {
	expectedGPUCluster := newGPUCluster(gpuClusterSingletonName)

	_, client := newGPUClusterServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(responseWriter, http.StatusOK, expectedGPUCluster)
	})

	gotGPUCluster, err := client.Get(t.Context(), gpuClusterSingletonName, metav1.GetOptions{})
	require.NoError(t, err)

	require.NotNil(t, gotGPUCluster)
	assert.Equal(t, gpuClusterSingletonName, gotGPUCluster.Name)
	assert.Equal(t, "7", gotGPUCluster.ResourceVersion)
	assert.Equal(t, "nvcr.io/nvidia/cloud-native", gotGPUCluster.Spec.DRADriver.Repository)
	assert.Equal(t, "k8s-dra-driver-gpu", gotGPUCluster.Spec.DRADriver.Image)
	assert.Equal(t, "v25.3.0", gotGPUCluster.Spec.DRADriver.Version)
	assert.Equal(t, map[string]bool{"ComputeDomains": true}, gotGPUCluster.Spec.DRADriver.FeatureGates)
}

func TestGPUClustersCreateSendsSpec(t *testing.T) {
	gpuClusterToCreate := newGPUCluster(gpuClusterSingletonName)

	server, client := newGPUClusterServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(responseWriter, http.StatusCreated, gpuClusterToCreate)
	})

	gotGPUCluster, err := client.Create(t.Context(), gpuClusterToCreate, metav1.CreateOptions{})
	require.NoError(t, err)

	// The spec must round-trip through the request body.
	var sentGPUCluster nvidiav1alpha1.GPUCluster
	require.NoError(t, json.Unmarshal(server.onlyRequest(t).Body, &sentGPUCluster))
	assert.Equal(t, gpuClusterSingletonName, sentGPUCluster.Name)
	assert.Equal(t, gpuClusterToCreate.Spec.DRADriver.Repository, sentGPUCluster.Spec.DRADriver.Repository)
	assert.Equal(t, gpuClusterToCreate.Spec.DRADriver.Image, sentGPUCluster.Spec.DRADriver.Image)
	assert.Equal(t, gpuClusterToCreate.Spec.DRADriver.Version, sentGPUCluster.Spec.DRADriver.Version)
	assert.Equal(t, map[string]bool{"ComputeDomains": true}, sentGPUCluster.Spec.DRADriver.FeatureGates)
	assert.Equal(t, map[string]string{"app": "gpu-cluster"}, sentGPUCluster.Labels)

	assert.Equal(t, gpuClusterSingletonName, gotGPUCluster.Name)
}

func TestGPUClustersUpdateStatusRoundTripsStatus(t *testing.T) {
	gpuClusterToUpdate := newGPUCluster(gpuClusterSingletonName)
	gpuClusterToUpdate.Status.State = nvidiav1alpha1.Ready
	gpuClusterToUpdate.Status.Namespace = "gpu-operator"
	gpuClusterToUpdate.Status.Conditions = []metav1.Condition{newReadyCondition(metav1.ConditionTrue, "AllOperandsReady")}

	server, client := newGPUClusterServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(responseWriter, http.StatusOK, gpuClusterToUpdate)
	})

	gotGPUCluster, err := client.UpdateStatus(t.Context(), gpuClusterToUpdate, metav1.UpdateOptions{})
	require.NoError(t, err)

	request := server.onlyRequest(t)
	var sentGPUCluster nvidiav1alpha1.GPUCluster
	require.NoError(t, json.Unmarshal(request.Body, &sentGPUCluster))
	assert.Equal(t, nvidiav1alpha1.Ready, sentGPUCluster.Status.State)
	assert.Equal(t, "gpu-operator", sentGPUCluster.Status.Namespace)

	assert.Equal(t, nvidiav1alpha1.Ready, gotGPUCluster.Status.State)
	assert.Equal(t, "gpu-operator", gotGPUCluster.Status.Namespace)
	assertConditionRoundTripped(t, request.Body, gotGPUCluster.Status.Conditions,
		metav1.ConditionTrue, "AllOperandsReady")
}
