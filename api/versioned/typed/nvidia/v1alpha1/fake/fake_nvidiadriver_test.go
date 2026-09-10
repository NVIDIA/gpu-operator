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

const nvidiaDriverName = "nvidia-driver"

// newNVIDIADriver builds an NVIDIADriver whose spec exercises every kind of
// field the generated DeepCopyInto handles separately: scalars, an enum, *bool
// pointers, a slice and a nested struct with a slice of its own.
func newNVIDIADriver(name string, labels map[string]string) *nvidiav1alpha1.NVIDIADriver {
	return &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			Default:          true,
			DriverType:       nvidiav1alpha1.GPU,
			UsePrecompiled:   new(false),
			KernelModuleType: "auto",
			Repository:       "nvcr.io/nvidia",
			Image:            "nvcr.io/nvidia/driver",
			Version:          "550.144.03",
			ImagePullPolicy:  "IfNotPresent",
			ImagePullSecrets: []string{"ngc-secret"},
			GPUDirectRDMA: &nvidiav1alpha1.GPUDirectRDMASpec{
				Enabled:      new(true),
				UseHostMOFED: new(false),
			},
			Manager: nvidiav1alpha1.DriverManagerSpec{
				Repository:       "nvcr.io/nvidia/cloud-native",
				Image:            "k8s-driver-manager",
				Version:          "v0.7.0",
				ImagePullSecrets: []string{"ngc-secret"},
			},
		},
	}
}

// nvidiaDriverUnderTest binds the shared verb matrix (see shared_verb_test.go)
// to the generated fake NVIDIADriver client.
var nvidiaDriverUnderTest = resourceUnderTest[*nvidiav1alpha1.NVIDIADriver, *nvidiav1alpha1.NVIDIADriverList]{
	groupVersionResource: nvidiaDriverGVR,
	groupVersionKind:     nvidiaDriverGVK,
	objectName:           nvidiaDriverName,

	newClient: func(fixture *fakeGroupFixture) typedResourceClient[*nvidiav1alpha1.NVIDIADriver, *nvidiav1alpha1.NVIDIADriverList] {
		return fixture.groupClient.NVIDIADrivers()
	},
	newObject: newNVIDIADriver,
	items: func(nvidiaDriverList *nvidiav1alpha1.NVIDIADriverList) []*nvidiav1alpha1.NVIDIADriver {
		nvidiaDrivers := make([]*nvidiav1alpha1.NVIDIADriver, 0, len(nvidiaDriverList.Items))
		for itemIndex := range nvidiaDriverList.Items {
			nvidiaDrivers = append(nvidiaDrivers, &nvidiaDriverList.Items[itemIndex])
		}
		return nvidiaDrivers
	},
}

func TestNVIDIADriversSharedVerbs(t *testing.T) {
	runSharedVerbTests(t, nvidiaDriverUnderTest)
}

// TestNVIDIADriversSpecRoundTrip is the one case that depends on the
// NVIDIADriver schema: the spec the caller submitted has to come back
// unchanged, pointers and slices included.
func TestNVIDIADriversSpecRoundTrip(t *testing.T) {
	fixture := newFakeGroupFixture(t)
	client := fixture.groupClient.NVIDIADrivers()
	ctx := t.Context()

	// expectedSpec must come from a separate object: comparing against the
	// submitted object's own spec would move with any in-place mutation the
	// client made to the caller's object, and pass vacuously.
	expectedSpec := newNVIDIADriver(nvidiaDriverName, nil).Spec

	submittedDriver := newNVIDIADriver(nvidiaDriverName, nil)
	createdDriver, err := client.Create(ctx, submittedDriver, metav1.CreateOptions{})
	require.NoError(t, err)
	assert.Equal(t, expectedSpec, createdDriver.Spec)

	gotDriver, err := client.Get(ctx, nvidiaDriverName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, expectedSpec, gotDriver.Spec)
}
