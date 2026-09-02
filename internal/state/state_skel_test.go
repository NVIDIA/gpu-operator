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
	"fmt"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/internal/utils"
)

func newDeploymentUnstructured(name, namespace string) *unstructured.Unstructured {
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})
	object.SetName(name)
	object.SetNamespace(namespace)
	return object
}

// skelTestScheme deliberately omits NVIDIADriver: TestSyncObjects relies on
// SetControllerReference failing for an owner whose type is not registered.
func skelTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	return scheme
}

func newTestSkel(t *testing.T, k8sClient client.Client) *stateSkel {
	t.Helper()
	return &stateSkel{
		name:        "test-state",
		description: "test description",
		namespace:   "test-ns",
		client:      k8sClient,
		scheme:      skelTestScheme(t),
	}
}

func newConfigMapUnstructured(name, namespace string) *unstructured.Unstructured {
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	object.SetName(name)
	object.SetNamespace(namespace)
	_ = unstructured.SetNestedStringMap(object.Object, map[string]string{"key": "value"}, "data")
	return object
}

func newServiceAccountUnstructured(name, namespace string) *unstructured.Unstructured {
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ServiceAccount"})
	object.SetName(name)
	object.SetNamespace(namespace)
	return object
}

func newDaemonSetUnstructured(name, namespace string) *unstructured.Unstructured {
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DaemonSet"})
	object.SetName(name)
	object.SetNamespace(namespace)
	return object
}

func setDaemonSetStatusCounts(object *unstructured.Unstructured, desiredNumberScheduled, numberAvailable, updatedNumberScheduled int64) {
	_ = unstructured.SetNestedField(object.Object, desiredNumberScheduled, "status", "desiredNumberScheduled")
	_ = unstructured.SetNestedField(object.Object, numberAvailable, "status", "numberAvailable")
	_ = unstructured.SetNestedField(object.Object, updatedNumberScheduled, "status", "updatedNumberScheduled")
}

func TestSkelNameAndDescription(t *testing.T) {
	skel := newTestSkel(t, fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build())
	assert.Equal(t, "test-state", skel.Name())
	assert.Equal(t, "test description", skel.Description())
}

func TestGetObj(t *testing.T) {
	existingConfigMap := newConfigMapUnstructured("cm-a", "test-ns")
	require.NoError(t, unstructured.SetNestedStringMap(existingConfigMap.Object, map[string]string{"key": "stored"}, "data"))
	fakeClient := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(existingConfigMap).Build()
	skel := newTestSkel(t, fakeClient)

	fetchedConfigMap := newConfigMapUnstructured("cm-a", "test-ns")
	require.NoError(t, skel.getObj(context.Background(), fetchedConfigMap))
	configMapData, found, err := unstructured.NestedStringMap(fetchedConfigMap.Object, "data")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "stored", configMapData["key"])

	missingConfigMap := newConfigMapUnstructured("cm-missing", "test-ns")
	assert.True(t, apierrors.IsNotFound(skel.getObj(context.Background(), missingConfigMap)))
}

func TestCreateObj(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build()
	skel := newTestSkel(t, fakeClient)

	configMap := newConfigMapUnstructured("cm-new", "test-ns")
	require.NoError(t, skel.createObj(context.Background(), configMap))

	assert.True(t, apierrors.IsAlreadyExists(skel.createObj(context.Background(), configMap)))
}

func TestCheckDeleteSupported(t *testing.T) {
	unsupportedKindObject := &unstructured.Unstructured{}
	unsupportedKindObject.SetGroupVersionKind(schema.GroupVersionKind{Group: "custom.io", Version: "v1", Kind: "Widget"})
	unsupportedKindObject.SetName("widget")
	unsupportedKindObject.SetNamespace("test-ns")

	testCases := []struct {
		name          string
		object        *unstructured.Unstructured
		expectWarning bool
	}{
		{
			name:   "supported GVK",
			object: newConfigMapUnstructured("cm", "test-ns"),
		},
		{
			name:          "unsupported GVK",
			object:        unsupportedKindObject,
			expectWarning: true,
		},
	}

	skel := newTestSkel(t, fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build())
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var logs strings.Builder
			logger := funcr.New(func(_, args string) { logs.WriteString(args + "\n") }, funcr.Options{})
			skel.checkDeleteSupported(log.IntoContext(context.Background(), logger), testCase.object)

			if testCase.expectWarning {
				assert.Contains(t, logs.String(), "Object will not be deleted if needed")
				return
			}
			assert.Empty(t, logs.String())
		})
	}
}

func TestUpdateObj(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).
		WithObjects(newConfigMapUnstructured("cm-a", "test-ns")).Build()
	skel := newTestSkel(t, fakeClient)

	// The fake client rejects an update carrying no resourceVersion, so the object
	// has to be read back before it is written.
	currentConfigMap := newConfigMapUnstructured("cm-a", "test-ns")
	require.NoError(t, skel.getObj(context.Background(), currentConfigMap))
	require.NoError(t, unstructured.SetNestedStringMap(currentConfigMap.Object, map[string]string{"key": "updated"}, "data"))
	require.NoError(t, skel.updateObj(context.Background(), currentConfigMap))

	storedConfigMap := newConfigMapUnstructured("cm-a", "test-ns")
	require.NoError(t, skel.getObj(context.Background(), storedConfigMap))
	configMapData, found, err := unstructured.NestedStringMap(storedConfigMap.Object, "data")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "updated", configMapData["key"])

	failingUpdateClient := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).
		WithObjects(newConfigMapUnstructured("cm-a", "test-ns")).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.UpdateOption) error {
				return fmt.Errorf("injected update error")
			},
		}).Build()
	failingUpdateSkel := newTestSkel(t, failingUpdateClient)
	err = failingUpdateSkel.updateObj(context.Background(), newConfigMapUnstructured("cm-a", "test-ns"))
	require.ErrorContains(t, err, "failed to update resource")
}

func TestAddStateSpecificLabels(t *testing.T) {
	skel := newTestSkel(t, fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build())
	configMap := newConfigMapUnstructured("cm", "test-ns")
	skel.addStateSpecificLabels(configMap)
	assert.Equal(t, "test-state", configMap.GetLabels()[consts.StateLabel])
}

func TestMergeObjectsResourceVersion(t *testing.T) {
	skel := newTestSkel(t, fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build())

	updatedConfigMap := newConfigMapUnstructured("cm", "test-ns")
	currentConfigMap := newConfigMapUnstructured("cm", "test-ns")
	currentConfigMap.SetResourceVersion("1234")

	require.NoError(t, skel.mergeObjects(updatedConfigMap, currentConfigMap))
	assert.Equal(t, "1234", updatedConfigMap.GetResourceVersion())
}

func TestMergeServiceAccount(t *testing.T) {
	skel := newTestSkel(t, fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build())

	updatedServiceAccount := newServiceAccountUnstructured("sa", "test-ns")
	currentServiceAccount := newServiceAccountUnstructured("sa", "test-ns")
	currentServiceAccount.SetResourceVersion("42")
	require.NoError(t, unstructured.SetNestedSlice(currentServiceAccount.Object,
		[]any{map[string]any{"name": "sa-token"}}, "secrets"))
	require.NoError(t, unstructured.SetNestedSlice(currentServiceAccount.Object,
		[]any{map[string]any{"name": "pull-secret"}}, "imagePullSecrets"))

	require.NoError(t, skel.mergeObjects(updatedServiceAccount, currentServiceAccount))

	secrets, found, err := unstructured.NestedSlice(updatedServiceAccount.Object, "secrets")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []any{map[string]any{"name": "sa-token"}}, secrets)

	pullSecrets, found, err := unstructured.NestedSlice(updatedServiceAccount.Object, "imagePullSecrets")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []any{map[string]any{"name": "pull-secret"}}, pullSecrets)
}

func TestCreateOrUpdateObjsCreatesNewObject(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build()
	skel := newTestSkel(t, fakeClient)

	desiredDaemonSet := newDaemonSetUnstructured("ds-a", "test-ns")

	require.NoError(t, skel.createOrUpdateObjs(context.Background(), noopSetControllerReference,
		[]*unstructured.Unstructured{desiredDaemonSet}))

	storedDaemonSet := newDaemonSetUnstructured("ds-a", "test-ns")
	require.NoError(t, skel.getObj(context.Background(), storedDaemonSet))
	assert.NotEmpty(t, storedDaemonSet.GetAnnotations()[consts.NvidiaAnnotationHashKey])
	assert.Equal(t, "test-state", storedDaemonSet.GetLabels()[consts.StateLabel])
}

func TestCreateOrUpdateObjsUpdatesExistingObject(t *testing.T) {
	existingConfigMap := newConfigMapUnstructured("cm-a", "test-ns")
	fakeClient := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(existingConfigMap).Build()
	skel := newTestSkel(t, fakeClient)

	desiredConfigMap := newConfigMapUnstructured("cm-a", "test-ns")
	require.NoError(t, unstructured.SetNestedStringMap(desiredConfigMap.Object, map[string]string{"key": "new"}, "data"))

	require.NoError(t, skel.createOrUpdateObjs(context.Background(), noopSetControllerReference,
		[]*unstructured.Unstructured{desiredConfigMap}))

	storedConfigMap := newConfigMapUnstructured("cm-a", "test-ns")
	require.NoError(t, skel.getObj(context.Background(), storedConfigMap))
	configMapData, found, err := unstructured.NestedStringMap(storedConfigMap.Object, "data")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "new", configMapData["key"])
}

func TestCreateOrUpdateObjsSkipsUnchangedDaemonSet(t *testing.T) {
	// createOrUpdateObjs hashes a DaemonSet after labelling it and before writing the
	// hash annotation, so the fixture has to be hashed at exactly that point.
	desiredDaemonSet := newDaemonSetUnstructured("ds-a", "test-ns")
	newTestSkel(t, nil).addStateSpecificLabels(desiredDaemonSet)
	desiredHash := utils.GetObjectHash(desiredDaemonSet)

	currentDaemonSet := newDaemonSetUnstructured("ds-a", "test-ns")
	currentDaemonSet.SetLabels(map[string]string{consts.StateLabel: "test-state"})
	currentDaemonSet.SetAnnotations(map[string]string{consts.NvidiaAnnotationHashKey: desiredHash})

	// A matching hash must short-circuit before updateObj is reached; the interceptor
	// is what asserts that.
	fakeClient := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(currentDaemonSet).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.UpdateOption) error {
				return fmt.Errorf("unexpected update of an unchanged object")
			},
		}).Build()

	skel := newTestSkel(t, fakeClient)
	require.NoError(t, skel.createOrUpdateObjs(context.Background(), noopSetControllerReference,
		[]*unstructured.Unstructured{desiredDaemonSet}))
}

func TestCreateOrUpdateObjsSetControllerReferenceError(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build()
	skel := newTestSkel(t, fakeClient)

	configMap := newConfigMapUnstructured("cm-a", "test-ns")
	failingSetControllerReference := func(_ *unstructured.Unstructured) error { return fmt.Errorf("ref error") }

	err := skel.createOrUpdateObjs(context.Background(), failingSetControllerReference,
		[]*unstructured.Unstructured{configMap})
	require.ErrorContains(t, err, "failed to set controller reference")
}

func TestCreateOrUpdateObjsCreateError(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
				return fmt.Errorf("injected create error")
			},
		}).Build()
	skel := newTestSkel(t, fakeClient)

	configMap := newConfigMapUnstructured("cm-a", "test-ns")
	err := skel.createOrUpdateObjs(context.Background(), noopSetControllerReference,
		[]*unstructured.Unstructured{configMap})
	require.ErrorContains(t, err, "injected create error")
}

func TestGetSyncState(t *testing.T) {
	t.Run("all objects ready", func(t *testing.T) {
		configMap := newConfigMapUnstructured("cm-a", "test-ns")
		daemonSet := newDaemonSetUnstructured("ds-a", "test-ns")
		setDaemonSetStatusCounts(daemonSet, 2, 2, 2)
		fakeClient := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(configMap, daemonSet).Build()
		skel := newTestSkel(t, fakeClient)

		syncState, err := skel.getSyncState(context.Background(),
			[]*unstructured.Unstructured{newConfigMapUnstructured("cm-a", "test-ns"), newDaemonSetUnstructured("ds-a", "test-ns")})
		require.NoError(t, err)
		assert.Equal(t, SyncState(SyncStateReady), syncState)
	})

	t.Run("object not found is not ready", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).Build()
		skel := newTestSkel(t, fakeClient)
		syncState, err := skel.getSyncState(context.Background(),
			[]*unstructured.Unstructured{newConfigMapUnstructured("cm-missing", "test-ns")})
		require.NoError(t, err)
		assert.Equal(t, SyncState(SyncStateNotReady), syncState)
	})

	t.Run("daemonset not ready", func(t *testing.T) {
		daemonSet := newDaemonSetUnstructured("ds-a", "test-ns")
		setDaemonSetStatusCounts(daemonSet, 3, 1, 1)
		fakeClient := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(daemonSet).Build()
		skel := newTestSkel(t, fakeClient)
		syncState, err := skel.getSyncState(context.Background(),
			[]*unstructured.Unstructured{newDaemonSetUnstructured("ds-a", "test-ns")})
		require.NoError(t, err)
		assert.Equal(t, SyncState(SyncStateNotReady), syncState)
	})

	t.Run("get error propagates", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return fmt.Errorf("injected get error")
				},
			}).Build()
		skel := newTestSkel(t, fakeClient)
		syncState, err := skel.getSyncState(context.Background(),
			[]*unstructured.Unstructured{newConfigMapUnstructured("cm-a", "test-ns")})
		require.Error(t, err)
		assert.Equal(t, SyncState(SyncStateNotReady), syncState)
	})

	t.Run("deployment ready", func(t *testing.T) {
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "dep-a", Namespace: "test-ns"},
			Spec:       appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)},
			Status:     appsv1.DeploymentStatus{Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(deployment).Build()
		skel := newTestSkel(t, fakeClient)
		syncState, err := skel.getSyncState(context.Background(),
			[]*unstructured.Unstructured{newDeploymentUnstructured("dep-a", "test-ns")})
		require.NoError(t, err)
		assert.Equal(t, SyncState(SyncStateReady), syncState)
	})

	t.Run("deployment not ready", func(t *testing.T) {
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "dep-a", Namespace: "test-ns"},
			Spec:       appsv1.DeploymentSpec{Replicas: ptr.To[int32](2)},
			Status:     appsv1.DeploymentStatus{Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(skelTestScheme(t)).WithObjects(deployment).Build()
		skel := newTestSkel(t, fakeClient)
		syncState, err := skel.getSyncState(context.Background(),
			[]*unstructured.Unstructured{newDeploymentUnstructured("dep-a", "test-ns")})
		require.NoError(t, err)
		assert.Equal(t, SyncState(SyncStateNotReady), syncState)
	})
}

func TestSyncObjects(t *testing.T) {
	ownerDriverCR := &nvidiav1alpha1.NVIDIADriver{ObjectMeta: metav1.ObjectMeta{Name: "driver-a", UID: "uid-a"}}

	t.Run("creates objects with owner reference", func(t *testing.T) {
		// driverTestScheme registers NVIDIADriver, so SetControllerReference resolves the owner.
		scheme := driverTestScheme(t)
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		skel := &stateSkel{name: "state-driver", namespace: "test-ns", client: fakeClient, scheme: scheme}

		_, err := skel.syncObjects(context.Background(), ownerDriverCR,
			[]*unstructured.Unstructured{newDaemonSetUnstructured("ds-a", "test-ns")})
		require.NoError(t, err)

		daemonSetList := &appsv1.DaemonSetList{}
		require.NoError(t, fakeClient.List(context.Background(), daemonSetList))
		require.Len(t, daemonSetList.Items, 1)
		require.Len(t, daemonSetList.Items[0].OwnerReferences, 1)
		assert.Equal(t, "driver-a", daemonSetList.Items[0].OwnerReferences[0].Name)
	})

	t.Run("set controller reference error", func(t *testing.T) {
		// skelTestScheme omits NVIDIADriver, so SetControllerReference on the owner fails.
		scheme := skelTestScheme(t)
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		skel := &stateSkel{name: "state-driver", namespace: "test-ns", client: fakeClient, scheme: scheme}

		_, err := skel.syncObjects(context.Background(), ownerDriverCR,
			[]*unstructured.Unstructured{newDaemonSetUnstructured("ds-a", "test-ns")})
		require.ErrorContains(t, err, "failed to create/update objects")
	})
}

func toUnstructuredDaemonSet(t *testing.T, daemonSet *appsv1.DaemonSet) *unstructured.Unstructured {
	t.Helper()
	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(daemonSet)
	require.NoError(t, err)
	return &unstructured.Unstructured{Object: object}
}

func TestIsDaemonSetReady(t *testing.T) {
	testCases := []struct {
		name          string
		daemonSet     *appsv1.DaemonSet
		expectedReady bool
	}{
		{
			name: "zero desired pods after the DaemonSet controller processed the object",
			daemonSet: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     appsv1.DaemonSetStatus{ObservedGeneration: 1},
			},
			expectedReady: true,
		},
		{
			name: "zero desired pods but the DaemonSet controller has not processed the object yet",
			daemonSet: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     appsv1.DaemonSetStatus{ObservedGeneration: 0},
			},
			expectedReady: false,
		},
		{
			name: "zero desired pods with a misscheduled pod still running",
			daemonSet: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: appsv1.DaemonSetStatus{
					ObservedGeneration: 1,
					NumberMisscheduled: 1,
				},
			},
			expectedReady: false,
		},
		{
			name: "zero desired pods with a pod still scheduled (draining)",
			daemonSet: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: appsv1.DaemonSetStatus{
					ObservedGeneration:     1,
					CurrentNumberScheduled: 1,
				},
			},
			expectedReady: false,
		},
		{
			name: "all desired pods available and updated",
			daemonSet: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
				Status: appsv1.DaemonSetStatus{
					ObservedGeneration:     2,
					DesiredNumberScheduled: 2,
					CurrentNumberScheduled: 2,
					NumberAvailable:        2,
					UpdatedNumberScheduled: 2,
				},
			},
			expectedReady: true,
		},
		{
			name: "desired pods not yet available",
			daemonSet: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: appsv1.DaemonSetStatus{
					ObservedGeneration:     1,
					DesiredNumberScheduled: 1,
					CurrentNumberScheduled: 1,
					NumberAvailable:        0,
				},
			},
			expectedReady: false,
		},
	}

	skel := &stateSkel{}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ready, err := skel.isDaemonSetReady(toUnstructuredDaemonSet(t, testCase.daemonSet), logr.Discard())
			require.NoError(t, err)
			assert.Equal(t, testCase.expectedReady, ready)
		})
	}
}

func TestGetSupportedGVKs(t *testing.T) {
	// deleteStateRelatedObjects deletes every kind in this list, so a kind dropped
	// from it silently leaks objects: assert the whole set, not a sample of it.
	expectedGVKs := []schema.GroupVersionKind{
		{Group: "", Version: "v1", Kind: "ServiceAccount"},
		{Group: "", Version: "v1", Kind: "ConfigMap"},
		{Group: "apps", Version: "v1", Kind: "DaemonSet"},
		{Group: "apps", Version: "v1", Kind: "Deployment"},
		{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"},
		{Group: "k8s.cni.cncf.io", Version: "v1", Kind: "NetworkAttachmentDefinition"},
		{Group: "batch", Version: "v1", Kind: "CronJob"},
		{Group: "security.openshift.io", Version: "v1", Kind: "SecurityContextConstraints"},
		{Group: "", Version: "v1", Kind: "Pod"},
		{Group: "", Version: "v1", Kind: "Service"},
		{Group: "monitoring.coreos.com", Version: "v1", Kind: "ServiceMonitor"},
		{Group: "scheduling.k8s.io", Version: "v1", Kind: "PriorityClass"},
		{Group: "", Version: "v1", Kind: "Taint"},
		{Group: "policy", Version: "v1beta1", Kind: "PodSecurityPolicy"},
		{Group: "node.k8s.io", Version: "v1", Kind: "RuntimeClass"},
		{Group: "monitoring.coreos.com", Version: "v1", Kind: "PrometheusRule"},
		{Group: "resource.k8s.io", Version: "v1", Kind: "ResourceClaimTemplate"},
		{Group: "resource.k8s.io", Version: "v1beta2", Kind: "ResourceClaimTemplate"},
		{Group: "resource.k8s.io", Version: "v1beta1", Kind: "ResourceClaimTemplate"},
	}
	assert.ElementsMatch(t, expectedGVKs, getSupportedGVKs())
}
