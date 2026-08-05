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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

// TestPackageSchemeRecognizesKinds asserts the package-level scheme built in
// init() knows every kind the fake clientset needs, including the metav1 helper
// types registered under GroupVersion{Version: "v1"}.
func TestPackageSchemeRecognizesKinds(t *testing.T) {
	tests := []struct {
		name string
		gvk  schema.GroupVersionKind
	}{
		{"ClusterPolicy", nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicy")},
		{"ClusterPolicyList", nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicyList")},
		{"NVIDIADriver", nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriver")},
		{"NVIDIADriverList", nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriverList")},
		{"GPUCluster", nvidiav1alpha1.SchemeGroupVersion.WithKind("GPUCluster")},
		{"GPUClusterList", nvidiav1alpha1.SchemeGroupVersion.WithKind("GPUClusterList")},
		// Added by init() via metav1.AddToGroupVersion(scheme, {Version: "v1"}).
		{"core v1 ListOptions", schema.GroupVersionKind{Version: "v1", Kind: "ListOptions"}},
		{"core v1 GetOptions", schema.GroupVersionKind{Version: "v1", Kind: "GetOptions"}},
		{"core v1 DeleteOptions", schema.GroupVersionKind{Version: "v1", Kind: "DeleteOptions"}},
		{"core v1 WatchEvent", schema.GroupVersionKind{Version: "v1", Kind: "WatchEvent"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, scheme.Recognizes(tt.gvk), "scheme should recognize %s", tt.gvk)
			obj, err := scheme.New(tt.gvk)
			require.NoError(t, err)
			assert.NotNil(t, obj)
		})
	}

	// Types outside this clientset must not be registered.
	assert.False(t, scheme.Recognizes(schema.GroupVersionKind{Version: "v1", Kind: "Pod"}))
	assert.False(t, scheme.Recognizes(nvidiav1.SchemeGroupVersion.WithKind("NVIDIADriver")))
}

// TestPackageSchemeObjectKinds verifies concrete Go types map back to the
// expected GroupVersionKind through the package scheme.
func TestPackageSchemeObjectKinds(t *testing.T) {
	tests := []struct {
		name string
		obj  runtime.Object
		want schema.GroupVersionKind
	}{
		{"ClusterPolicy", &nvidiav1.ClusterPolicy{}, nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicy")},
		{"ClusterPolicyList", &nvidiav1.ClusterPolicyList{}, nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicyList")},
		{"NVIDIADriver", &nvidiav1alpha1.NVIDIADriver{}, nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriver")},
		{"NVIDIADriverList", &nvidiav1alpha1.NVIDIADriverList{}, nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriverList")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gvks, unversioned, err := scheme.ObjectKinds(tt.obj)
			require.NoError(t, err)
			assert.False(t, unversioned)
			assert.Contains(t, gvks, tt.want)
		})
	}
}

// TestAddToSchemeOnFreshScheme verifies the exported AddToScheme registers the
// clientset's kinds into a caller-supplied scheme.
func TestAddToSchemeOnFreshScheme(t *testing.T) {
	fresh := runtime.NewScheme()

	// Nothing is known before AddToScheme.
	require.False(t, fresh.Recognizes(nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicy")))

	require.NoError(t, AddToScheme(fresh))

	for _, gvk := range []schema.GroupVersionKind{
		nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicy"),
		nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicyList"),
		nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriver"),
		nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriverList"),
		nvidiav1alpha1.SchemeGroupVersion.WithKind("GPUCluster"),
		nvidiav1alpha1.SchemeGroupVersion.WithKind("GPUClusterList"),
	} {
		assert.True(t, fresh.Recognizes(gvk), "fresh scheme should recognize %s", gvk)
	}

	// AddToScheme only registers metav1 helpers under the nvidia.com group versions;
	// the GroupVersion{Version: "v1"} registration is done by this package's init().
	assert.True(t, fresh.Recognizes(nvidiav1.SchemeGroupVersion.WithKind("ListOptions")))
	assert.False(t, fresh.Recognizes(schema.GroupVersionKind{Version: "v1", Kind: "ListOptions"}))

	// Applying it twice is a no-op rather than an error.
	assert.NoError(t, AddToScheme(fresh))
}

// TestLocalSchemeBuilderMembership asserts the builder wires up exactly the two
// API groups this clientset serves.
func TestLocalSchemeBuilderMembership(t *testing.T) {
	require.Len(t, localSchemeBuilder, 2)

	fresh := runtime.NewScheme()
	require.NoError(t, localSchemeBuilder.AddToScheme(fresh))
	assert.True(t, fresh.Recognizes(nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicy")))
	assert.True(t, fresh.Recognizes(nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriver")))
}

// TestCodecsUniversalDecoderDecodesClusterPolicy verifies the codec factory built
// over the package scheme round-trips a serialized ClusterPolicy.
func TestCodecsUniversalDecoderDecodesClusterPolicy(t *testing.T) {
	original := &nvidiav1.ClusterPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: nvidiav1.SchemeGroupVersion.String(),
			Kind:       "ClusterPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cluster-policy",
			Labels: map[string]string{"app": "gpu-operator"},
		},
		Spec: nvidiav1.ClusterPolicySpec{
			Operator: nvidiav1.OperatorSpec{RuntimeClass: "nvidia"},
		},
		Status: nvidiav1.ClusterPolicyStatus{
			State:     nvidiav1.Ready,
			Namespace: "gpu-operator",
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	decoder := codecs.UniversalDecoder()
	into := &nvidiav1.ClusterPolicy{}
	obj, gvk, err := decoder.Decode(data, nil, into)
	require.NoError(t, err)
	require.NotNil(t, gvk)
	assert.Equal(t, nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicy"), *gvk)

	decoded, ok := obj.(*nvidiav1.ClusterPolicy)
	require.True(t, ok, "expected *ClusterPolicy, got %T", obj)
	assert.Equal(t, "cluster-policy", decoded.Name)
	assert.Equal(t, "gpu-operator", decoded.Labels["app"])
	assert.Equal(t, "nvidia", decoded.Spec.Operator.RuntimeClass)
	assert.Equal(t, nvidiav1.Ready, decoded.Status.State)
	assert.Equal(t, "gpu-operator", decoded.Status.Namespace)
}

// TestCodecsUniversalDecoderRejectsUnknownKind verifies decoding an object whose
// kind is absent from the fake scheme fails instead of silently succeeding.
func TestCodecsUniversalDecoderRejectsUnknownKind(t *testing.T) {
	data := []byte(`{"apiVersion":"v1","kind":"Pod","metadata":{"name":"p"}}`)
	_, _, err := codecs.UniversalDecoder().Decode(data, nil, nil)
	require.Error(t, err)
	assert.True(t, runtime.IsNotRegisteredError(err), "expected a not-registered error, got %v", err)
}

// TestCodecsUniversalDeserializerDecodesNVIDIADriver covers the v1alpha1 group
// through the same codec factory.
func TestCodecsUniversalDeserializerDecodesNVIDIADriver(t *testing.T) {
	data := []byte(`{
		"apiVersion":"nvidia.com/v1alpha1",
		"kind":"NVIDIADriver",
		"metadata":{"name":"gpu-driver"},
		"spec":{"driverType":"gpu","default":true},
		"status":{"state":"ready"}
	}`)

	obj, gvk, err := codecs.UniversalDeserializer().Decode(data, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, gvk)
	assert.Equal(t, nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriver"), *gvk)

	drv, ok := obj.(*nvidiav1alpha1.NVIDIADriver)
	require.True(t, ok, "expected *NVIDIADriver, got %T", obj)
	assert.Equal(t, "gpu-driver", drv.Name)
	assert.Equal(t, nvidiav1alpha1.GPU, drv.Spec.DriverType)
	assert.True(t, drv.IsDefault())
	assert.Equal(t, nvidiav1alpha1.Ready, drv.Status.State)
}
