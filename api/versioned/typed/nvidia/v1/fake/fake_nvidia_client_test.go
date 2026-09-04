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
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	rest "k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/versioned/typed/nvidia/v1"
)

// FakeNvidiaV1 must remain a drop-in replacement for the real typed client.
var _ nvidiav1.NvidiaV1Interface = &FakeNvidiaV1{}

// The ClusterPolicies() getter must also satisfy the getter interface.
var _ nvidiav1.ClusterPoliciesGetter = &FakeNvidiaV1{}

func TestFakeNvidiaV1ClusterPolicies(t *testing.T) {
	c := &FakeNvidiaV1{Fake: &k8stesting.Fake{}}

	cps := c.ClusterPolicies()
	require.NotNil(t, cps)

	// The returned value is the package-private fake implementation, wired back
	// to the same FakeNvidiaV1 so that reactors/actions are shared.
	impl, ok := cps.(*fakeClusterPolicies)
	require.True(t, ok, "ClusterPolicies() should return *fakeClusterPolicies, got %T", cps)
	assert.Same(t, c, impl.Fake, "fakeClusterPolicies must point back at its FakeNvidiaV1")
	assert.Same(t, c.Fake, impl.FakeClientWithList.Fake,
		"the embedded gentype client must share the same testing.Fake as the group client")
}

func TestFakeNvidiaV1ClusterPoliciesReturnsFreshClients(t *testing.T) {
	c := &FakeNvidiaV1{Fake: &k8stesting.Fake{}}

	first := c.ClusterPolicies()
	second := c.ClusterPolicies()

	require.NotNil(t, first)
	require.NotNil(t, second)
	// Each call constructs a new value (the generated code does not memoize), but
	// both are backed by the very same testing.Fake, so actions/reactors are shared.
	assert.NotSame(t, first, second)
	assert.Same(t,
		first.(*fakeClusterPolicies).FakeClientWithList.Fake,
		second.(*fakeClusterPolicies).FakeClientWithList.Fake,
	)
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
	c := &FakeNvidiaV1{Fake: &k8stesting.Fake{}}

	got := c.RESTClient()

	// The interface value itself is not the untyped nil...
	assert.False(t, got == nil,
		"RESTClient() returns a non-nil interface holding a nil pointer")

	// ...but the dynamic value is a nil *rest.RESTClient.
	restClient, ok := got.(*rest.RESTClient)
	require.True(t, ok, "expected dynamic type *rest.RESTClient, got %T", got)
	assert.Nil(t, restClient)

	v := reflect.ValueOf(got)
	require.Equal(t, reflect.Pointer, v.Kind())
	assert.True(t, v.IsNil(), "underlying *rest.RESTClient must be nil")

	// testify's assert.Nil is reflection based, so it agrees the value is nil.
	assert.Nil(t, got)
}

func TestFakeNvidiaV1RESTClientIsStable(t *testing.T) {
	c := &FakeNvidiaV1{Fake: &k8stesting.Fake{}}

	// Repeated calls are pure: no state, no recorded actions.
	assert.Equal(t, c.RESTClient(), c.RESTClient())
	assert.Empty(t, c.Actions(), "RESTClient() must not record an action")
}
