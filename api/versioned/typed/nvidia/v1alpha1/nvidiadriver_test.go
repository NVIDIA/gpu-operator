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

// nvidiaDriverResource is the plural resource name passed to newNVIDIADrivers.
const nvidiaDriverResource = "nvidiadrivers"

// nvidiaDriverName is the name every NVIDIADriver fixture in this package uses.
const nvidiaDriverName = "nvidia-driver-a"

func nvidiaDriverNamedPath(name string, subresources ...string) string {
	return resourceNamedPath(nvidiaDriverResource, name, subresources...)
}

// newNVIDIADriverServerAndClient stands up a recording API server and returns the typed
// NVIDIADriver client wired to it.
func newNVIDIADriverServerAndClient(t *testing.T, handler http.HandlerFunc) (*recordingServer, NVIDIADriverInterface) {
	t.Helper()

	server, client := newRecordingServerAndClient(t, handler)
	return server, client.NVIDIADrivers()
}

// newNVIDIADriver builds a fully typed NVIDIADriver, including the TypeMeta the
// client-side decoder needs to recognize the payload.
//
// NVIDIADriverSpec is by far the richer of this group's two specs, so the spec
// carries one of each shape the decoder has to handle — a pointer sub-struct, a
// slice of structs and a map — rather than scalars alone.
func newNVIDIADriver(name string) *nvidiav1alpha1.NVIDIADriver {
	apiVersion, kind := nvidiav1alpha1.SchemeGroupVersion.WithKind(nvidiav1alpha1.NVIDIADriverCRDName).ToAPIVersionAndKind()
	return &nvidiav1alpha1.NVIDIADriver{
		TypeMeta: metav1.TypeMeta{APIVersion: apiVersion, Kind: kind},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			ResourceVersion: "42",
			Labels:          map[string]string{"app": "nvidia-driver"},
		},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			Default:      true,
			DriverType:   nvidiav1alpha1.GPU,
			Image:        "nvcr.io/nvidia/driver",
			RepoConfig:   &nvidiav1alpha1.DriverRepoConfigSpec{Name: "custom-repo-config"},
			Env:          []nvidiav1alpha1.EnvVar{{Name: "OPEN_KERNEL_MODULES_ENABLED", Value: "true"}},
			NodeSelector: map[string]string{"nvidia.com/gpu.present": "true"},
		},
	}
}

// newNVIDIADriverList builds a decodable list payload holding items.
func newNVIDIADriverList(items ...*nvidiav1alpha1.NVIDIADriver) *nvidiav1alpha1.NVIDIADriverList {
	apiVersion, kind := nvidiav1alpha1.SchemeGroupVersion.WithKind(nvidiav1alpha1.NVIDIADriverCRDName + "List").ToAPIVersionAndKind()
	nvidiaDriverList := &nvidiav1alpha1.NVIDIADriverList{
		TypeMeta: metav1.TypeMeta{APIVersion: apiVersion, Kind: kind},
	}
	for _, item := range items {
		nvidiaDriverList.Items = append(nvidiaDriverList.Items, *item)
	}
	return nvidiaDriverList
}

// assertNVIDIADriverSpecDecoded checks the shapes a scalar-only fixture would
// leave unproven: the pointer sub-struct, the slice of structs and the map.
func assertNVIDIADriverSpecDecoded(t *testing.T, spec nvidiav1alpha1.NVIDIADriverSpec) {
	t.Helper()

	require.NotNil(t, spec.RepoConfig)
	assert.Equal(t, "custom-repo-config", spec.RepoConfig.Name)
	assert.Equal(t, []nvidiav1alpha1.EnvVar{{Name: "OPEN_KERNEL_MODULES_ENABLED", Value: "true"}}, spec.Env)
	assert.Equal(t, map[string]string{"nvidia.com/gpu.present": "true"}, spec.NodeSelector)
}

// TestNVIDIADriversHTTPPlumbing runs the verb tests every typed client in this
// group shares against the generated NVIDIADriver client. Everything that
// depends on the NVIDIADriver schema lives in the focused tests below.
func TestNVIDIADriversHTTPPlumbing(t *testing.T) {
	runSharedVerbTests(t, resourceFixture[*nvidiav1alpha1.NVIDIADriver, *nvidiav1alpha1.NVIDIADriverList, NVIDIADriverInterface]{
		resource:      nvidiaDriverResource,
		objectName:    nvidiaDriverName,
		labelSelector: "app=nvidia-driver",
		fieldSelector: "metadata.name=" + nvidiaDriverName,

		clientFor: (*NvidiaV1alpha1Client).NVIDIADrivers,
		newObject: newNVIDIADriver,
		newList:   newNVIDIADriverList,
		items: func(nvidiaDriverList *nvidiav1alpha1.NVIDIADriverList) []*nvidiav1alpha1.NVIDIADriver {
			nvidiaDrivers := make([]*nvidiav1alpha1.NVIDIADriver, 0, len(nvidiaDriverList.Items))
			for itemIndex := range nvidiaDriverList.Items {
				nvidiaDrivers = append(nvidiaDrivers, &nvidiaDriverList.Items[itemIndex])
			}
			return nvidiaDrivers
		},
	})
}

func TestNVIDIADriversRequestPaths(t *testing.T) {
	// Spelled out literally: the shared verb tests derive their expectations
	// from the same helpers the client uses, so this pins the actual URLs.
	assert.Equal(t, "/apis/nvidia.com/v1alpha1/nvidiadrivers", resourceCollectionPath(nvidiaDriverResource))
	assert.Equal(t, "/apis/nvidia.com/v1alpha1/nvidiadrivers/nvidia-driver-a", nvidiaDriverNamedPath(nvidiaDriverName))
	assert.Equal(t, "/apis/nvidia.com/v1alpha1/nvidiadrivers/nvidia-driver-a/status", nvidiaDriverNamedPath(nvidiaDriverName, "status"))

	server, client := newNVIDIADriverServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(responseWriter, http.StatusOK, newNVIDIADriver(nvidiaDriverName))
	})

	_, err := client.Get(t.Context(), nvidiaDriverName, metav1.GetOptions{})
	require.NoError(t, err)

	assert.Equal(t, "/apis/nvidia.com/v1alpha1/nvidiadrivers/nvidia-driver-a", server.onlyRequest(t).Path)
}

func TestNVIDIADriversGetDecodesSpec(t *testing.T) {
	expectedNVIDIADriver := newNVIDIADriver(nvidiaDriverName)

	_, client := newNVIDIADriverServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(responseWriter, http.StatusOK, expectedNVIDIADriver)
	})

	gotNVIDIADriver, err := client.Get(t.Context(), nvidiaDriverName, metav1.GetOptions{})
	require.NoError(t, err)

	require.NotNil(t, gotNVIDIADriver)
	assert.Equal(t, nvidiaDriverName, gotNVIDIADriver.Name)
	assert.Equal(t, "42", gotNVIDIADriver.ResourceVersion)
	assert.True(t, gotNVIDIADriver.Spec.Default)
	assert.Equal(t, nvidiav1alpha1.GPU, gotNVIDIADriver.Spec.DriverType)
	assert.Equal(t, "nvcr.io/nvidia/driver", gotNVIDIADriver.Spec.Image)
	assertNVIDIADriverSpecDecoded(t, gotNVIDIADriver.Spec)
}

func TestNVIDIADriversCreateSendsSpec(t *testing.T) {
	nvidiaDriverToCreate := newNVIDIADriver(nvidiaDriverName)

	server, client := newNVIDIADriverServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(responseWriter, http.StatusCreated, nvidiaDriverToCreate)
	})

	gotNVIDIADriver, err := client.Create(t.Context(), nvidiaDriverToCreate, metav1.CreateOptions{})
	require.NoError(t, err)

	// The spec must round-trip through the request body.
	var sentNVIDIADriver nvidiav1alpha1.NVIDIADriver
	require.NoError(t, json.Unmarshal(server.onlyRequest(t).Body, &sentNVIDIADriver))
	assert.Equal(t, nvidiaDriverName, sentNVIDIADriver.Name)
	assert.Equal(t, nvidiaDriverToCreate.Spec.Image, sentNVIDIADriver.Spec.Image)
	assert.Equal(t, nvidiaDriverToCreate.Spec.DriverType, sentNVIDIADriver.Spec.DriverType)
	assert.True(t, sentNVIDIADriver.Spec.Default)
	assertNVIDIADriverSpecDecoded(t, sentNVIDIADriver.Spec)
	assert.Equal(t, map[string]string{"app": "nvidia-driver"}, sentNVIDIADriver.Labels)

	assert.Equal(t, nvidiaDriverName, gotNVIDIADriver.Name)
}

func TestNVIDIADriversUpdateStatusRoundTripsStatus(t *testing.T) {
	nvidiaDriverToUpdate := newNVIDIADriver(nvidiaDriverName)
	nvidiaDriverToUpdate.Status.State = nvidiav1alpha1.NotReady
	nvidiaDriverToUpdate.Status.Namespace = "gpu-operator"
	nvidiaDriverToUpdate.Status.Conditions = []metav1.Condition{newReadyCondition(metav1.ConditionFalse, "DriverPodsPending")}

	server, client := newNVIDIADriverServerAndClient(t, func(responseWriter http.ResponseWriter, _ *http.Request) {
		writeJSONResponse(responseWriter, http.StatusOK, nvidiaDriverToUpdate)
	})

	gotNVIDIADriver, err := client.UpdateStatus(t.Context(), nvidiaDriverToUpdate, metav1.UpdateOptions{})
	require.NoError(t, err)

	request := server.onlyRequest(t)
	var sentNVIDIADriver nvidiav1alpha1.NVIDIADriver
	require.NoError(t, json.Unmarshal(request.Body, &sentNVIDIADriver))
	assert.Equal(t, nvidiav1alpha1.NotReady, sentNVIDIADriver.Status.State)
	assert.Equal(t, "gpu-operator", sentNVIDIADriver.Status.Namespace)

	assert.Equal(t, nvidiav1alpha1.NotReady, gotNVIDIADriver.Status.State)
	assert.Equal(t, "gpu-operator", gotNVIDIADriver.Status.Namespace)
	assertConditionRoundTripped(t, request.Body, gotNVIDIADriver.Status.Conditions,
		metav1.ConditionFalse, "DriverPodsPending")
}
