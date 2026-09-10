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

// metaV1GroupVersion is the group version that register.go passes to metav1.AddToGroupVersion.
var metaV1GroupVersion = schema.GroupVersion{Version: "v1"}

// nvidiaGroupVersionKinds is every kind localSchemeBuilder registers.
var nvidiaGroupVersionKinds = []schema.GroupVersionKind{
	nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicy"),
	nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicyList"),
	nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriver"),
	nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriverList"),
	nvidiav1alpha1.SchemeGroupVersion.WithKind("GPUCluster"),
	nvidiav1alpha1.SchemeGroupVersion.WithKind("GPUClusterList"),
}

// TestSchemeRecognizesNVIDIAKinds asserts that the package level Scheme, populated by
// init(), maps every NVIDIA type to its expected GroupVersionKind and back.
func TestSchemeRecognizesNVIDIAKinds(t *testing.T) {
	testCases := []struct {
		name             string
		object           runtime.Object
		groupVersionKind schema.GroupVersionKind
	}{
		{
			name:             "ClusterPolicy",
			object:           &nvidiav1.ClusterPolicy{},
			groupVersionKind: nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicy"),
		},
		{
			name:             "ClusterPolicyList",
			object:           &nvidiav1.ClusterPolicyList{},
			groupVersionKind: nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicyList"),
		},
		{
			name:             "NVIDIADriver",
			object:           &nvidiav1alpha1.NVIDIADriver{},
			groupVersionKind: nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriver"),
		},
		{
			name:             "NVIDIADriverList",
			object:           &nvidiav1alpha1.NVIDIADriverList{},
			groupVersionKind: nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriverList"),
		},
		{
			name:             "GPUCluster",
			object:           &nvidiav1alpha1.GPUCluster{},
			groupVersionKind: nvidiav1alpha1.SchemeGroupVersion.WithKind("GPUCluster"),
		},
		{
			name:             "GPUClusterList",
			object:           &nvidiav1alpha1.GPUClusterList{},
			groupVersionKind: nvidiav1alpha1.SchemeGroupVersion.WithKind("GPUClusterList"),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			groupVersionKinds, isUnversioned, err := Scheme.ObjectKinds(testCase.object)
			require.NoError(t, err)
			assert.False(t, isUnversioned, "NVIDIA types must not be registered as unversioned")
			assert.Equal(t, []schema.GroupVersionKind{testCase.groupVersionKind}, groupVersionKinds)

			gotObject, err := Scheme.New(testCase.groupVersionKind)
			require.NoError(t, err)
			assert.IsType(t, testCase.object, gotObject)
		})
	}
}

// TestSchemeKnownGroupVersions asserts both NVIDIA group versions, plus the meta "v1"
// group version added by init(), are part of the scheme's known versions.
func TestSchemeKnownGroupVersions(t *testing.T) {
	groupVersions := Scheme.PrioritizedVersionsAllGroups()

	assert.Contains(t, groupVersions, nvidiav1.SchemeGroupVersion)
	assert.Contains(t, groupVersions, nvidiav1alpha1.SchemeGroupVersion)
	assert.Contains(t, groupVersions, metaV1GroupVersion)

	// The nvidia.com group carries exactly these two versions and nothing else.
	assert.ElementsMatch(t, []string{"v1", "v1alpha1"}, versionsForGroup(groupVersions, "nvidia.com"))
}

// versionsForGroup returns every version the scheme knows for the given API group.
func versionsForGroup(groupVersions []schema.GroupVersion, group string) []string {
	var versions []string
	for _, groupVersion := range groupVersions {
		if groupVersion.Group == group {
			versions = append(versions, groupVersion.Version)
		}
	}
	return versions
}

// TestAddToSchemeComposesIntoAForeignScheme covers the documented composition use case: adding this
// clientset's types into a scheme owned by somebody else. It doubles as the coverage for
// localSchemeBuilder, which AddToScheme is a method value of: the order in which client-gen emits
// the registration functions, and how many there are, is not a contract, so only the resulting
// scheme is asserted.
func TestAddToSchemeComposesIntoAForeignScheme(t *testing.T) {
	targetScheme := runtime.NewScheme()

	// The fresh scheme knows nothing beforehand.
	require.False(t, targetScheme.Recognizes(nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicy")))
	require.False(t, targetScheme.Recognizes(nvidiav1alpha1.SchemeGroupVersion.WithKind("NVIDIADriver")))

	require.NoError(t, AddToScheme(targetScheme))

	for _, groupVersionKind := range nvidiaGroupVersionKinds {
		assert.True(t, targetScheme.Recognizes(groupVersionKind), "expected %s to be registered", groupVersionKind)
	}

	// The composed scheme is usable for encoding, not just lookups.
	groupVersionKinds, _, err := targetScheme.ObjectKinds(&nvidiav1.ClusterPolicy{})
	require.NoError(t, err)
	assert.Equal(t, []schema.GroupVersionKind{nvidiav1.SchemeGroupVersion.WithKind("ClusterPolicy")}, groupVersionKinds)
}

// TestAddToSchemeIsIdempotent asserts repeated registration into the same scheme neither
// errors nor panics, since generated clientsets are frequently composed more than once.
func TestAddToSchemeIsIdempotent(t *testing.T) {
	targetScheme := runtime.NewScheme()

	require.NoError(t, AddToScheme(targetScheme))
	knownTypeCount := len(targetScheme.AllKnownTypes())

	require.NoError(t, AddToScheme(targetScheme))
	require.NoError(t, AddToScheme(targetScheme))

	assert.Len(t, targetScheme.AllKnownTypes(), knownTypeCount, "re-registration must not add new kinds")
}

// TestParameterCodecEncodeParametersUnderNVIDIAGroupVersions exercises ParameterCodec under the
// group versions the generated clients actually pass, which is the resource's own group version
// rather than the bare meta "v1" one.
func TestParameterCodecEncodeParametersUnderNVIDIAGroupVersions(t *testing.T) {
	testCases := []struct {
		name         string
		groupVersion schema.GroupVersion
	}{
		{
			name:         "nvidia.com/v1",
			groupVersion: nvidiav1.SchemeGroupVersion,
		},
		{
			name:         "nvidia.com/v1alpha1",
			groupVersion: nvidiav1alpha1.SchemeGroupVersion,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			listOptions := &metav1.ListOptions{
				LabelSelector: "app=gpu-operator",
				Watch:         true,
				Limit:         10,
			}
			encodedParameters, err := ParameterCodec.EncodeParameters(listOptions, testCase.groupVersion)
			require.NoError(t, err)
			expectedListParameters := url.Values{
				"labelSelector": []string{"app=gpu-operator"},
				"watch":         []string{"true"},
				"limit":         []string{"10"},
			}
			assert.Equal(t, expectedListParameters, encodedParameters)
		})
	}
}

// schemeIdentityProbe is registered into the package level Scheme by
// TestCodecsAndParameterCodecShareOneScheme. Nothing else uses its group version.
type schemeIdentityProbe struct {
	metav1.TypeMeta
	metav1.ObjectMeta `json:"metadata"`
}

func (probe *schemeIdentityProbe) DeepCopyObject() runtime.Object {
	copiedProbe := *probe
	return &copiedProbe
}

var probeGroupVersion = schema.GroupVersion{Group: "probe.example.com", Version: "v1"}

// TestCodecsAndParameterCodecShareOneScheme asserts both are built on the very same Scheme value,
// not on two identically populated schemes. A registration made after init() is the only thing
// that tells those apart, so this deliberately mutates the package level Scheme; the probe lives
// in a group version no other test looks at. Any future assertion on the length of
// Scheme.AllKnownTypes() or PrioritizedVersionsAllGroups() would become order dependent.
func TestCodecsAndParameterCodecShareOneScheme(t *testing.T) {
	Scheme.AddKnownTypeWithName(probeGroupVersion.WithKind("SchemeIdentityProbe"), &schemeIdentityProbe{})
	metav1.AddToGroupVersion(Scheme, probeGroupVersion)

	serializedProbe := `{"apiVersion":"` + probeGroupVersion.String() + `","kind":"SchemeIdentityProbe","metadata":{"name":"probe"}}`
	decodedObject, _, err := Codecs.UniversalDeserializer().Decode([]byte(serializedProbe), nil, nil)
	require.NoError(t, err)
	probe, ok := decodedObject.(*schemeIdentityProbe)
	require.True(t, ok, "expected *schemeIdentityProbe, got %T", decodedObject)
	assert.Equal(t, "probe", probe.Name)

	encodedParameters, err := ParameterCodec.EncodeParameters(&metav1.ListOptions{LabelSelector: "app=gpu-operator"}, probeGroupVersion)
	require.NoError(t, err)
	assert.Equal(t, url.Values{"labelSelector": []string{"app=gpu-operator"}}, encodedParameters)
}
