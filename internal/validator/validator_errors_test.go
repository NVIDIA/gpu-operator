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

package validator

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

func newValidatorScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, nvidiav1alpha1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))
	return s
}

func TestValidateReturnsErrorWhenDriverListFails(t *testing.T) {
	s := newValidatorScheme(t)

	requested := makeTestDriver("requested", nil, false)
	c := fake.NewClientBuilder().
		WithScheme(s).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*nvidiav1alpha1.NVIDIADriverList); ok {
					return errors.New("driver list boom")
				}
				return cl.List(ctx, list, opts...)
			},
		}).
		Build()
	nsv := NewNodeSelectorValidator(c)

	err := nsv.Validate(context.Background(), requested)
	require.Error(t, err)
	require.Contains(t, err.Error(), "driver list boom")
}

func TestValidateReturnsErrorWhenListedDriverHasInvalidSelector(t *testing.T) {
	s := newValidatorScheme(t)

	requested := makeTestDriver("requested", nil, false)
	invalid := makeTestDriver("invalid-default", map[string]string{"nodepool": "b"}, true)

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(invalid).
		Build()
	nsv := NewNodeSelectorValidator(c)

	err := nsv.Validate(context.Background(), requested)
	require.Error(t, err)
	require.Contains(t, err.Error(), "default NVIDIADriver")
	require.Contains(t, err.Error(), "cannot use nodeSelector")
}

func TestValidateReturnsErrorWhenNodeListFails(t *testing.T) {
	s := newValidatorScheme(t)

	requested := makeTestDriver("requested", map[string]string{"nodepool": "a"}, false)
	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(requested).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*corev1.NodeList); ok {
					return errors.New("node list boom")
				}
				return cl.List(ctx, list, opts...)
			},
		}).
		Build()
	nsv := NewNodeSelectorValidator(c)

	err := nsv.Validate(context.Background(), requested)
	require.Error(t, err)
	require.Contains(t, err.Error(), "node list boom")
}
