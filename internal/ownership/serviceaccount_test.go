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

package ownership

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestDeleteObserved(t *testing.T) {
	for name, test := range map[string]struct {
		changed, absent bool
		failure         error
	}{
		"unchanged object is deleted":                 {},
		"concurrent handoff on same UID is preserved": {changed: true},
		"already deleted is harmless":                 {absent: true},
		"permission failure is returned":              {failure: apierrors.NewForbidden(corev1.Resource("serviceaccounts"), "metrics", fmt.Errorf("denied"))},
		"transport failure is returned":               {failure: fmt.Errorf("connection reset")},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "metrics", Namespace: "operator", UID: "original-uid"}}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sa).Build()
			observed := &corev1.ServiceAccount{}
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(sa), observed))
			if test.changed {
				current := observed.DeepCopy()
				current.Annotations = map[string]string{"owner": "user"}
				require.NoError(t, c.Update(ctx, current))
			}
			if test.absent {
				require.NoError(t, c.Delete(ctx, sa))
			}
			wrapped := interceptor.NewClient(c, interceptor.Funcs{Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				options := (&client.DeleteOptions{}).ApplyOptions(opts)
				require.NotNil(t, options.Preconditions)
				require.Equal(t, &observed.UID, options.Preconditions.UID)
				require.Equal(t, &observed.ResourceVersion, options.Preconditions.ResourceVersion)
				if test.failure != nil {
					return test.failure
				}
				return c.Delete(ctx, obj, opts...)
			}})
			err := DeleteObserved(ctx, wrapped, observed)
			switch {
			case test.failure != nil:
				require.ErrorIs(t, err, test.failure)
			case test.changed:
				require.True(t, apierrors.IsConflict(err), "a conflict must trigger reconciliation, not mark cleanup successful")
			default:
				require.NoError(t, err)
			}
			current := &corev1.ServiceAccount{}
			err = c.Get(ctx, client.ObjectKeyFromObject(sa), current)
			if test.changed || test.failure != nil {
				require.NoError(t, err)
				if test.changed {
					require.Equal(t, "user", current.Annotations["owner"])
				}
			} else {
				require.True(t, apierrors.IsNotFound(err))
			}
		})
	}
}
