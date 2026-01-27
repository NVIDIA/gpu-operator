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

package state

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/internal/utils"
)

func TestCreateOrUpdateObjs_DaemonSetUnchangedDoesNotShortCircuit(t *testing.T) {
	ctx := context.Background()

	stateSkel := &stateSkel{
		name: "test-state",
	}

	ds := &unstructured.Unstructured{}
	ds.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DaemonSet"})
	ds.SetName("test-ds")
	ds.SetNamespace("default")

	cm := &unstructured.Unstructured{}
	cm.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	cm.SetName("test-cm")
	cm.SetNamespace("default")

	// Mirror the mutation order used by createOrUpdateObjs to compute the expected hash,
	// so we can seed a pre-existing DaemonSet with a matching hash and trigger the
	// "Object is unchanged" short-circuit path.
	stateSkel.addStateSpecificLabels(ds)
	dsHash := utils.GetObjectHash(ds)

	existingDs := ds.DeepCopy()
	existingDs.SetAnnotations(map[string]string{
		consts.NvidiaAnnotationHashKey: dsHash,
	})

	k8sClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(existingDs).Build()
	stateSkel.client = k8sClient

	err := stateSkel.createOrUpdateObjs(ctx, func(_ *unstructured.Unstructured) error { return nil }, []*unstructured.Unstructured{ds, cm})
	require.NoError(t, err)

	gotCm := &unstructured.Unstructured{}
	gotCm.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	err = k8sClient.Get(ctx, client.ObjectKey{Name: "test-cm", Namespace: "default"}, gotCm)
	require.NoError(t, err)
}
