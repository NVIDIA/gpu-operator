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

package scheme

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

// metaV1GV is the group version that register.go passes to metav1.AddToGroupVersion.
var metaV1GV = schema.GroupVersion{Version: "v1"}

func boolPtr(b bool) *bool { return &b }

// newClusterPolicy returns a ClusterPolicy with enough populated fields to make a
// serialization round trip meaningful.
func newClusterPolicy() *nvidiav1.ClusterPolicy {
	return &nvidiav1.ClusterPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "cluster-policy",
			Labels: map[string]string{"app": "gpu-operator"},
		},
		Spec: nvidiav1.ClusterPolicySpec{
			Operator: nvidiav1.OperatorSpec{
				RuntimeClass: "nvidia",
			},
			Driver: nvidiav1.DriverSpec{
				Enabled:    boolPtr(true),
				Repository: "nvcr.io/nvidia",
			},
		},
		Status: nvidiav1.ClusterPolicyStatus{
			State:     nvidiav1.Ready,
			Namespace: "gpu-operator",
		},
	}
}

// newNVIDIADriver returns an NVIDIADriver with enough populated fields to make a
// serialization round trip meaningful.
func newNVIDIADriver() *nvidiav1alpha1.NVIDIADriver {
	return &nvidiav1alpha1.NVIDIADriver{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "nvidia-driver",
			Labels: map[string]string{"app": "nvidia-driver"},
		},
		Spec: nvidiav1alpha1.NVIDIADriverSpec{
			Default:      true,
			DriverType:   nvidiav1alpha1.GPU,
			Image:        "driver",
			Repository:   "nvcr.io/nvidia",
			Version:      "550.54.14",
			NodeSelector: map[string]string{"nvidia.com/gpu.present": "true"},
		},
		Status: nvidiav1alpha1.NVIDIADriverStatus{
			State:     nvidiav1alpha1.Ready,
			Namespace: "gpu-operator",
		},
	}
}

func newGPUCluster() *nvidiav1alpha1.GPUCluster {
	return &nvidiav1alpha1.GPUCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "gpu-cluster",
			Labels: map[string]string{"app": "gpu-cluster"},
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

// TestSchemeRecognizesNVIDIAKinds asserts that the package level Scheme, populated by
// init(), maps every NVIDIA type to its expected GroupVersionKind.
func TestSchemeRecognizesNVIDIAKinds(t *testing.T) {
	tests := []struct {
		name string
		obj  runtime.Object
		gvk  schema.GroupVersionKind
	}{
		{
			name: "ClusterPolicy",
			obj:  &nvidiav1.ClusterPolicy{},
			gvk:  nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicy"),
		},
		{
			name: "ClusterPolicyList",
			obj:  &nvidiav1.ClusterPolicyList{},
			gvk:  nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicyList"),
		},
		{
			name: "NVIDIADriver",
			obj:  &nvidiav1alpha1.NVIDIADriver{},
			gvk:  nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriver"),
		},
		{
			name: "NVIDIADriverList",
			obj:  &nvidiav1alpha1.NVIDIADriverList{},
			gvk:  nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriverList"),
		},
		{
			name: "GPUCluster",
			obj:  &nvidiav1alpha1.GPUCluster{},
			gvk:  nvidiav1alpha1.SchemeGroupVersion.WithKind("GPUCluster"),
		},
		{
			name: "GPUClusterList",
			obj:  &nvidiav1alpha1.GPUClusterList{},
			gvk:  nvidiav1alpha1.SchemeGroupVersion.WithKind("GPUClusterList"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Go type -> GVK
			gvks, unversioned, err := Scheme.ObjectKinds(tt.obj)
			require.NoError(t, err)
			assert.False(t, unversioned, "NVIDIA types must not be registered as unversioned")
			assert.Equal(t, []schema.GroupVersionKind{tt.gvk}, gvks)

			// GVK -> Go type
			out, err := Scheme.New(tt.gvk)
			require.NoError(t, err)
			assert.IsType(t, tt.obj, out)
			assert.True(t, Scheme.Recognizes(tt.gvk))
		})
	}
}

// TestSchemeRejectsUnknownKind asserts that kinds that were never registered are not
// silently accepted by the Scheme.
func TestSchemeRejectsUnknownKind(t *testing.T) {
	tests := []schema.GroupVersionKind{
		nvidiav1.SchemeGroupVersion.WithKind("NotAThing"),
		// NVIDIADriver only exists in v1alpha1, not in v1.
		nvidiav1.SchemeGroupVersion.WithKind("NVIDIADriver"),
		// ClusterPolicy only exists in v1, not in v1alpha1.
		nvidiav1alpha1.SchemeGroupVersion.WithKind("ClusterPolicy"),
		{Group: "other.com", Version: "v1", Kind: "ClusterPolicy"},
	}

	for _, gvk := range tests {
		t.Run(gvk.String(), func(t *testing.T) {
			assert.False(t, Scheme.Recognizes(gvk))
			_, err := Scheme.New(gvk)
			require.Error(t, err)
			assert.True(t, runtime.IsNotRegisteredError(err), "expected a not-registered error, got %v", err)
		})
	}
}

// TestMetaV1TypesRegistered asserts the effects of metav1.AddToGroupVersion: the shared
// meta types are resolvable under the bare "v1" group version.
func TestMetaV1TypesRegistered(t *testing.T) {
	tests := []struct {
		name string
		kind string
		obj  runtime.Object
	}{
		{name: "ListOptions", kind: "ListOptions", obj: &metav1.ListOptions{}},
		{name: "GetOptions", kind: "GetOptions", obj: &metav1.GetOptions{}},
		{name: "DeleteOptions", kind: "DeleteOptions", obj: &metav1.DeleteOptions{}},
		{name: "CreateOptions", kind: "CreateOptions", obj: &metav1.CreateOptions{}},
		{name: "UpdateOptions", kind: "UpdateOptions", obj: &metav1.UpdateOptions{}},
		{name: "PatchOptions", kind: "PatchOptions", obj: &metav1.PatchOptions{}},
		{name: "WatchEvent", kind: "WatchEvent", obj: &metav1.WatchEvent{}},
		{name: "Status", kind: "Status", obj: &metav1.Status{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gvk := metaV1GV.WithKind(tt.kind)
			require.True(t, Scheme.Recognizes(gvk), "%s should be registered under %s", tt.kind, metaV1GV)

			out, err := Scheme.New(gvk)
			require.NoError(t, err)
			assert.IsType(t, tt.obj, out)

			gvks, _, err := Scheme.ObjectKinds(tt.obj)
			require.NoError(t, err)
			assert.Contains(t, gvks, gvk)
		})
	}
}

// TestStatusIsUnversioned asserts that metav1.Status is registered as an unversioned type,
// which is what allows API error responses to decode regardless of the request group version.
func TestStatusIsUnversioned(t *testing.T) {
	_, unversioned, err := Scheme.ObjectKinds(&metav1.Status{})
	require.NoError(t, err)
	assert.True(t, unversioned)
}

// TestCodecsRoundTrip encodes each NVIDIA object with the legacy codec for its own group
// version and decodes it back, asserting both the TypeMeta stamped on the wire format and
// full fidelity of the payload.
func TestCodecsRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		gv   schema.GroupVersion
		obj  runtime.Object
		gvk  schema.GroupVersionKind
	}{
		{
			name: "ClusterPolicy",
			gv:   nvidiav1.SchemeGroupVersion,
			obj:  newClusterPolicy(),
			gvk:  nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicy"),
		},
		{
			name: "ClusterPolicyList",
			gv:   nvidiav1.SchemeGroupVersion,
			obj: &nvidiav1.ClusterPolicyList{
				Items: []nvidiav1.ClusterPolicy{*newClusterPolicy()},
			},
			gvk: nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicyList"),
		},
		{
			name: "NVIDIADriver",
			gv:   nvidiav1alpha1.SchemeGroupVersion,
			obj:  newNVIDIADriver(),
			gvk:  nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriver"),
		},
		{
			name: "NVIDIADriverList",
			gv:   nvidiav1alpha1.SchemeGroupVersion,
			obj: &nvidiav1alpha1.NVIDIADriverList{
				Items: []nvidiav1alpha1.NVIDIADriver{*newNVIDIADriver()},
			},
			gvk: nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriverList"),
		},
		{
			name: "GPUCluster",
			gv:   nvidiav1alpha1.SchemeGroupVersion,
			obj:  newGPUCluster(),
			gvk:  nvidiav1alpha1.SchemeGroupVersion.WithKind(nvidiav1alpha1.GPUClusterCRDName),
		},
		{
			name: "GPUClusterList",
			gv:   nvidiav1alpha1.SchemeGroupVersion,
			obj: &nvidiav1alpha1.GPUClusterList{
				Items: []nvidiav1alpha1.GPUCluster{*newGPUCluster()},
			},
			gvk: nvidiav1alpha1.SchemeGroupVersion.WithKind("GPUClusterList"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := tt.obj.DeepCopyObject()

			data, err := runtime.Encode(Codecs.LegacyCodec(tt.gv), tt.obj)
			require.NoError(t, err)
			assert.Contains(t, string(data), `"apiVersion":"`+tt.gv.String()+`"`)
			assert.Contains(t, string(data), `"kind":"`+tt.gvk.Kind+`"`)

			// Encoding must not mutate the object handed to the codec.
			assert.Equal(t, original, tt.obj)

			decoded, gvk, err := Codecs.UniversalDeserializer().Decode(data, nil, nil)
			require.NoError(t, err)
			require.NotNil(t, gvk)
			assert.Equal(t, tt.gvk, *gvk)
			assert.IsType(t, tt.obj, decoded)

			// The decoded object carries the TypeMeta it was serialized with.
			assert.Equal(t, tt.gvk, decoded.GetObjectKind().GroupVersionKind())

			// Once TypeMeta is stripped the decoded object is identical to the input.
			decoded.GetObjectKind().SetGroupVersionKind(schema.GroupVersionKind{})
			assert.Equal(t, original, decoded)
		})
	}
}

// TestCodecsUniversalDecoderIntoTypedObject asserts decoding into a caller supplied,
// already typed object works through the versioning decoder.
func TestCodecsUniversalDecoderIntoTypedObject(t *testing.T) {
	original := newNVIDIADriver()

	data, err := runtime.Encode(Codecs.LegacyCodec(nvidiav1alpha1.SchemeGroupVersion), original)
	require.NoError(t, err)

	into := &nvidiav1alpha1.NVIDIADriver{}
	decoder := Codecs.UniversalDecoder(nvidiav1alpha1.SchemeGroupVersion)
	decoded, gvk, err := decoder.Decode(data, nil, into)
	require.NoError(t, err)
	require.NotNil(t, gvk)
	assert.Equal(t, nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriver"), *gvk)
	assert.Same(t, into, decoded)
	assert.Equal(t, original.Spec, into.Spec)
	assert.Equal(t, original.Status, into.Status)
	assert.Equal(t, original.ObjectMeta, into.ObjectMeta)
}

// TestCodecsDecodeUnknownKind asserts the serializer refuses payloads whose apiVersion/kind
// are not part of this clientset's scheme.
func TestCodecsDecodeUnknownKind(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{
			name: "unknown kind in a known group version",
			data: `{"apiVersion":"nvidia.com/v1","kind":"NotAThing","metadata":{"name":"x"}}`,
		},
		{
			name: "known kind in an unknown group",
			data: `{"apiVersion":"example.com/v1","kind":"ClusterPolicy","metadata":{"name":"x"}}`,
		},
		{
			name: "kind registered only in another version",
			data: `{"apiVersion":"nvidia.com/v1","kind":"NVIDIADriver","metadata":{"name":"x"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Codecs.UniversalDeserializer().Decode([]byte(tt.data), nil, nil)
			require.Error(t, err)
			assert.True(t, runtime.IsNotRegisteredError(err), "expected a not-registered error, got %v", err)
		})
	}
}

// TestCodecsDecodeMissingKind asserts payloads without apiVersion/kind cannot be decoded
// without a default GVK.
func TestCodecsDecodeMissingKind(t *testing.T) {
	_, _, err := Codecs.UniversalDeserializer().Decode([]byte(`{"metadata":{"name":"x"}}`), nil, nil)
	require.Error(t, err)
	assert.True(t, runtime.IsMissingKind(err), "expected a missing-kind error, got %v", err)
}

// TestParameterCodecEncodeParameters asserts ListOptions are converted to the query string
// form the generated clients rely on.
func TestParameterCodecEncodeParameters(t *testing.T) {
	timeout := int64(42)
	opts := &metav1.ListOptions{
		LabelSelector:   "app=gpu-operator",
		FieldSelector:   "metadata.name=cluster-policy",
		ResourceVersion: "1234",
		TimeoutSeconds:  &timeout,
		Watch:           true,
		Limit:           10,
		Continue:        "token",
	}

	values, err := ParameterCodec.EncodeParameters(opts, metaV1GV)
	require.NoError(t, err)

	expected := url.Values{
		"labelSelector":   []string{"app=gpu-operator"},
		"fieldSelector":   []string{"metadata.name=cluster-policy"},
		"resourceVersion": []string{"1234"},
		"timeoutSeconds":  []string{"42"},
		"watch":           []string{"true"},
		"limit":           []string{"10"},
		"continue":        []string{"token"},
	}
	assert.Equal(t, expected, values)
}

// TestParameterCodecEncodeParametersOmitsEmpty asserts that unset optional fields are not
// emitted as empty query parameters.
func TestParameterCodecEncodeParametersOmitsEmpty(t *testing.T) {
	values, err := ParameterCodec.EncodeParameters(&metav1.ListOptions{}, metaV1GV)
	require.NoError(t, err)
	assert.Empty(t, values)

	values, err = ParameterCodec.EncodeParameters(&metav1.GetOptions{ResourceVersion: "0"}, metaV1GV)
	require.NoError(t, err)
	assert.Equal(t, url.Values{"resourceVersion": []string{"0"}}, values)
}

// TestParameterCodecRoundTrip asserts EncodeParameters/DecodeParameters are inverses.
func TestParameterCodecRoundTrip(t *testing.T) {
	timeout := int64(7)
	original := &metav1.ListOptions{
		LabelSelector:   "nvidia.com/gpu.present=true",
		FieldSelector:   "status.phase=Running",
		ResourceVersion: "99",
		TimeoutSeconds:  &timeout,
		Watch:           true,
	}

	values, err := ParameterCodec.EncodeParameters(original, metaV1GV)
	require.NoError(t, err)

	decoded := &metav1.ListOptions{}
	require.NoError(t, ParameterCodec.DecodeParameters(values, metaV1GV, decoded))

	assert.Equal(t, original.LabelSelector, decoded.LabelSelector)
	assert.Equal(t, original.FieldSelector, decoded.FieldSelector)
	assert.Equal(t, original.ResourceVersion, decoded.ResourceVersion)
	assert.Equal(t, original.Watch, decoded.Watch)
	require.NotNil(t, decoded.TimeoutSeconds)
	assert.Equal(t, timeout, *decoded.TimeoutSeconds)
}

// TestParameterCodecUnregisteredType asserts the ParameterCodec is backed by this package's
// Scheme and therefore rejects types it does not know.
func TestParameterCodecUnregisteredType(t *testing.T) {
	_, err := ParameterCodec.EncodeParameters(&unregisteredOptions{}, metaV1GV)
	require.Error(t, err)
	assert.True(t, runtime.IsNotRegisteredError(err), "expected a not-registered error, got %v", err)
}

type unregisteredOptions struct {
	metav1.TypeMeta
}

func (o *unregisteredOptions) DeepCopyObject() runtime.Object {
	out := *o
	return &out
}

// TestAddToSchemeComposition covers the documented composition use case: adding this
// clientset's types into a scheme owned by somebody else.
func TestAddToSchemeComposition(t *testing.T) {
	target := runtime.NewScheme()

	// The fresh scheme knows nothing beforehand.
	require.False(t, target.Recognizes(nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicy")))
	require.False(t, target.Recognizes(nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriver")))

	require.NoError(t, AddToScheme(target))

	for _, gvk := range []schema.GroupVersionKind{
		nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicy"),
		nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicyList"),
		nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriver"),
		nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriverList"),
		nvidiav1alpha1.SchemeGroupVersion.WithKind("GPUCluster"),
		nvidiav1alpha1.SchemeGroupVersion.WithKind("GPUClusterList"),
	} {
		assert.True(t, target.Recognizes(gvk), "expected %s to be registered", gvk)
	}

	// The composed scheme is usable for encoding, not just lookups.
	cp := newClusterPolicy()
	gvks, _, err := target.ObjectKinds(cp)
	require.NoError(t, err)
	assert.Equal(t, []schema.GroupVersionKind{nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicy")}, gvks)
}

// TestAddToSchemeIsIdempotent asserts repeated registration into the same scheme neither
// errors nor panics, since generated clientsets are frequently composed more than once.
func TestAddToSchemeIsIdempotent(t *testing.T) {
	target := runtime.NewScheme()

	require.NoError(t, AddToScheme(target))
	before := len(target.AllKnownTypes())

	assert.NotPanics(t, func() {
		require.NoError(t, AddToScheme(target))
		require.NoError(t, AddToScheme(target))
	})

	assert.Equal(t, before, len(target.AllKnownTypes()), "re-registration must not add new kinds")

	// Registering again into the package level Scheme is also safe.
	assert.NotPanics(t, func() {
		require.NoError(t, AddToScheme(Scheme))
	})
}

// TestLocalSchemeBuilderContents asserts the generated builder wires up exactly the two
// NVIDIA API groups that make up this clientset.
func TestLocalSchemeBuilderContents(t *testing.T) {
	// Asserted through the public AddToScheme result rather than by indexing
	// localSchemeBuilder: the order in which client-gen emits the registration
	// functions, and how many there are, is not a contract. Adding another API
	// group or reordering the generated slice should only fail this test if the
	// resulting scheme is wrong.
	target := runtime.NewScheme()
	require.NoError(t, AddToScheme(target))

	for _, gvk := range []schema.GroupVersionKind{
		nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicy"),
		nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicyList"),
		nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriver"),
		nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriverList"),
		nvidiav1alpha1.SchemeGroupVersion.WithKind(nvidiav1alpha1.GPUClusterCRDName),
		nvidiav1alpha1.SchemeGroupVersion.WithKind("GPUClusterList"),
	} {
		assert.True(t, target.Recognizes(gvk), "AddToScheme must register %s", gvk)
	}
}

// TestSchemeObservedGroupVersions asserts both NVIDIA group versions, plus the meta "v1"
// group version added by init(), are part of the scheme's known versions.
func TestSchemeObservedGroupVersions(t *testing.T) {
	gvs := Scheme.PrioritizedVersionsAllGroups()

	assert.Contains(t, gvs, nvidiav1.SchemeGroupVersion)
	assert.Contains(t, gvs, nvidiav1alpha1.SchemeGroupVersion)
	assert.Contains(t, gvs, metaV1GV)

	assert.Equal(t, []string{nvidiav1.SchemeGroupVersion.Version}, versionsForGroup(gvs, "nvidia.com", "v1"))
	assert.Equal(t, []string{nvidiav1alpha1.SchemeGroupVersion.Version}, versionsForGroup(gvs, "nvidia.com", "v1alpha1"))
}

// versionsForGroup returns the versions observed for group that match want.
func versionsForGroup(gvs []schema.GroupVersion, group, want string) []string {
	var out []string
	for _, gv := range gvs {
		if gv.Group == group && gv.Version == want {
			out = append(out, gv.Version)
		}
	}
	return out
}

// TestCodecsIsBackedByScheme asserts Codecs is wired to this package's Scheme by driving a
// full encode/decode through the top level runtime helpers the generated clients use.
func TestCodecsIsBackedByScheme(t *testing.T) {
	data, err := runtime.Encode(Codecs.LegacyCodec(nvidiav1.SchemeGroupVersion), newClusterPolicy())
	require.NoError(t, err)

	obj, err := runtime.Decode(Codecs.UniversalDeserializer(), data)
	require.NoError(t, err)

	cp, ok := obj.(*nvidiav1.ClusterPolicy)
	require.True(t, ok)
	assert.Equal(t, "cluster-policy", cp.Name)
	assert.Equal(t, "nvidia", cp.Spec.Operator.RuntimeClass)
	require.NotNil(t, cp.Spec.Driver.Enabled)
	assert.True(t, *cp.Spec.Driver.Enabled)
}
