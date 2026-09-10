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
	rest "k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"

	typednvidiav1 "github.com/NVIDIA/gpu-operator/api/versioned/typed/nvidia/v1"
)

// FakeNvidiaV1 must remain a drop-in replacement for the real typed client.
var _ typednvidiav1.NvidiaV1Interface = &FakeNvidiaV1{}

// The ClusterPolicies() getter must also satisfy the getter interface.
var _ typednvidiav1.ClusterPoliciesGetter = &FakeNvidiaV1{}

func TestFakeNvidiaV1ClusterPolicies(t *testing.T) {
	fakeClient := &FakeNvidiaV1{Fake: &k8stesting.Fake{}}

	clusterPolicyClient := fakeClient.ClusterPolicies()
	require.NotNil(t, clusterPolicyClient)

	// The returned value is the package-private fake implementation, wired back
	// to the same FakeNvidiaV1 so that reactors/actions are shared.
	clusterPolicies, ok := clusterPolicyClient.(*fakeClusterPolicies)
	require.True(t, ok, "ClusterPolicies() should return *fakeClusterPolicies, got %T", clusterPolicyClient)
	assert.Same(t, fakeClient, clusterPolicies.Fake, "fakeClusterPolicies must point back at its FakeNvidiaV1")
	assert.Same(t, fakeClient.Fake, clusterPolicies.FakeClientWithList.Fake,
		"the embedded gentype client must share the same testing.Fake as the group client")
}

func TestFakeNvidiaV1ClusterPoliciesReturnsDistinctClients(t *testing.T) {
	fakeClient := &FakeNvidiaV1{Fake: &k8stesting.Fake{}}

	firstClient, ok := fakeClient.ClusterPolicies().(*fakeClusterPolicies)
	require.True(t, ok)
	secondClient, ok := fakeClient.ClusterPolicies().(*fakeClusterPolicies)
	require.True(t, ok)

	// Each call constructs a new value (the generated code does not memoize), but
	// both are backed by the very same testing.Fake, so actions/reactors are shared.
	assert.NotSame(t, firstClient, secondClient)
	assert.Same(t, firstClient.FakeClientWithList.Fake, secondClient.FakeClientWithList.Fake)
}

// TestFakeNvidiaV1RESTClientIsTypedNil documents a sharp edge of the generated
// stub in this repo: RESTClient() declares `var ret *rest.RESTClient` and returns
// it, so the caller receives a NON-nil rest.Interface whose dynamic value is a nil
// *rest.RESTClient. A plain `if c.RESTClient() == nil` check therefore does NOT
// fire.
//
// The returned client has no usable transport (no config, no base URL, no
// round-tripper), so it cannot be used to issue requests. Exactly how client-go's
// rest package reacts to that is an upstream detail and is not asserted here.
func TestFakeNvidiaV1RESTClientIsTypedNil(t *testing.T) {
	fakeClient := &FakeNvidiaV1{Fake: &k8stesting.Fake{}}

	gotRESTClient := fakeClient.RESTClient()

	// The interface value itself is not the untyped nil...
	var nilRESTInterface rest.Interface
	assert.NotEqual(t, nilRESTInterface, gotRESTClient,
		"RESTClient() returns a non-nil interface holding a nil pointer")

	// ...but the dynamic value is a nil *rest.RESTClient.
	restClient, ok := gotRESTClient.(*rest.RESTClient)
	require.True(t, ok, "expected dynamic type *rest.RESTClient, got %T", gotRESTClient)
	assert.Nil(t, restClient)

	// testify's assert.Nil is reflection based, so it agrees the value is nil.
	assert.Nil(t, gotRESTClient)
}

func TestFakeNvidiaV1RESTClientRecordsNoAction(t *testing.T) {
	fakeClient := &FakeNvidiaV1{Fake: &k8stesting.Fake{}}

	// Repeated calls are pure: each one yields the same typed nil and none of
	// them records an action. Comparing the two returned values instead would
	// assert nothing, since any two equal-valued clients compare equal.
	for range 2 {
		restClient, ok := fakeClient.RESTClient().(*rest.RESTClient)
		require.True(t, ok, "expected dynamic type *rest.RESTClient")
		assert.Nil(t, restClient)
	}
	assert.Empty(t, fakeClient.Actions(), "RESTClient() must not record an action")
}
