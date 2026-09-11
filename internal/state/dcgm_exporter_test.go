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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

const dcgmExporterManifestDir = "../../manifests/state-dcgm-exporter"

// restMapperWithServiceMonitor returns a RESTMapper that serves the ServiceMonitor kind when
// present is true, and an empty one otherwise (simulating a cluster without the Prometheus Operator).
func restMapperWithServiceMonitor(present bool) meta.RESTMapper {
	m := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "monitoring.coreos.com", Version: "v1"}})
	if present {
		m.Add(schema.GroupVersionKind{Group: "monitoring.coreos.com", Version: "v1", Kind: "ServiceMonitor"},
			meta.RESTScopeNamespace)
	}
	return m
}

func newTestDCGMExporterState(t *testing.T, serviceMonitorCRD bool) *configurableState {
	t.Helper()
	t.Setenv("DCGM_EXPORTER_IMAGE", "nvcr.io/nvidia/k8s/dcgm-exporter:test")
	client := fake.NewClientBuilder().
		WithRESTMapper(restMapperWithServiceMonitor(serviceMonitorCRD)).
		Build()
	s, err := NewStateDCGMExporter(client, "test-operator", runtime.NewScheme(), dcgmExporterManifestDir)
	require.NoError(t, err)
	return s.(*configurableState)
}

// exporterCR returns a sample CR with dcgm-exporter enabled and the given exporter spec.
// newTestDCGMExporterStateWithObjects builds the state with a client that already holds
// the given objects, for the ServiceAccount checks that read cluster state.
func newTestDCGMExporterStateWithObjects(t *testing.T, objs ...client.Object) *configurableState {
	t.Helper()
	t.Setenv("DCGM_EXPORTER_IMAGE", "nvcr.io/nvidia/k8s/dcgm-exporter:test")

	testScheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(testScheme))
	require.NoError(t, nvidiav1alpha1.AddToScheme(testScheme))

	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithRESTMapper(restMapperWithServiceMonitor(false)).
		WithObjects(objs...).
		Build()
	s, err := NewStateDCGMExporter(k8sClient, "test-operator", testScheme, dcgmExporterManifestDir)
	require.NoError(t, err)
	return s.(*configurableState)
}

func exporterCR(spec *nvidiav1.DCGMExporterSpec) *nvidiav1alpha1.GPUCluster {
	cr := sampleGPUCluster()
	cr.Spec.DCGMExporter = spec
	return cr
}

func TestDCGMExporterEnabledByDefault(t *testing.T) {
	s := newTestDCGMExporterState(t, false)
	// Enabled nil -> enabled by default for the exporter.
	cr := exporterCR(&nvidiav1.DCGMExporterSpec{Repository: "nvcr.io/nvidia/k8s", Image: "dcgm-exporter", Version: "4.5.3"})

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)

	kinds := kindCounts(objs)
	assert.Equal(t, 1, kinds["ServiceAccount"])
	assert.Equal(t, 1, kinds["Role"])
	assert.Equal(t, 1, kinds["RoleBinding"])
	assert.Equal(t, 1, kinds["ResourceClaimTemplate"])
	assert.Equal(t, 1, kinds["DaemonSet"])
	assert.Equal(t, 1, kinds["Service"])
	// DRA attribution needs the ResourceSlice informer, so the read ClusterRole is
	// always bound. No ServiceMonitor by default.
	assert.Equal(t, 1, kinds["ClusterRole"])
	assert.Equal(t, 1, kinds["ClusterRoleBinding"])
	assert.Equal(t, 0, kinds["ServiceMonitor"])

	claimHasAdminAccess(t, findByKind(objs, "ResourceClaimTemplate"))

	ds := findDaemonSet(t, objs)
	podSpec := ds.Spec.Template.Spec
	assert.Equal(t, "true", podSpec.NodeSelector["nvidia.com/gpu.deploy.dcgm-exporter-dra"])
	require.NotNil(t, podSpec.AutomountServiceAccountToken)
	assert.True(t, *podSpec.AutomountServiceAccountToken)

	ctr := podSpec.Containers[0]
	assert.Equal(t, "nvcr.io/nvidia/k8s/dcgm-exporter:4.5.3", ctr.Image)
	env := envMap(ctr.Env)
	assert.Equal(t, ":9400", env["DCGM_EXPORTER_LISTEN"])
	// Pod attribution on DRA nodes is on by default; pod labels and UID are not.
	assert.Equal(t, "true", env["KUBERNETES_ENABLE_DRA"])
	assert.NotContains(t, env, "DCGM_EXPORTER_KUBERNETES_ENABLE_POD_LABELS")
	assert.NotContains(t, env, "DCGM_EXPORTER_KUBERNETES_ENABLE_POD_UID")
	assert.Equal(t, "/etc/dcgm-exporter/dcp-metrics-included.csv", env["DCGM_EXPORTER_COLLECTORS"])
	// Embedded engine: no remote host-engine env.
	_, hasRemote := env["DCGM_REMOTE_HOSTENGINE_INFO"]
	assert.False(t, hasRemote, "exporter must run embedded when standalone DCGM is disabled")

	require.Len(t, ctr.Resources.Claims, 1)
	assert.Equal(t, "admin-gpus", ctr.Resources.Claims[0].Name)
}

func TestDCGMExporterDisabled(t *testing.T) {
	s := newTestDCGMExporterState(t, false)
	cr := exporterCR(&nvidiav1.DCGMExporterSpec{Enabled: new(false)})

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)
	assert.Empty(t, objs)
}

func TestDCGMExporterRemoteEngineWhenDCGMEnabled(t *testing.T) {
	s := newTestDCGMExporterState(t, false)
	cr := exporterCR(&nvidiav1.DCGMExporterSpec{})
	cr.Spec.DCGM = &nvidiav1.DCGMSpec{Enabled: new(true)}

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)

	ds := findDaemonSet(t, objs)
	env := envMap(ds.Spec.Template.Spec.Containers[0].Env)
	assert.Equal(t, "nvidia-dcgm-dra:5555", env["DCGM_REMOTE_HOSTENGINE_INFO"])
}

func TestDCGMExporterPodMetadataEnrichment(t *testing.T) {
	s := newTestDCGMExporterState(t, false)
	cr := exporterCR(&nvidiav1.DCGMExporterSpec{
		EnablePodLabels:        new(true),
		EnablePodUID:           new(true),
		PodLabelAllowlistRegex: []string{"^app$", "^team$"},
	})

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)

	ds := findDaemonSet(t, objs)
	podSpec := ds.Spec.Template.Spec
	env := envMap(podSpec.Containers[0].Env)
	assert.Equal(t, "true", env["DCGM_EXPORTER_KUBERNETES_ENABLE_POD_LABELS"])
	assert.Equal(t, "true", env["DCGM_EXPORTER_KUBERNETES_ENABLE_POD_UID"])
	assert.Equal(t, "^app$,^team$", env["DCGM_EXPORTER_KUBERNETES_POD_LABEL_ALLOWLIST_REGEX"])
}

func TestDCGMExporterCustomMetricsConfig(t *testing.T) {
	s := newTestDCGMExporterState(t, false)
	cr := exporterCR(&nvidiav1.DCGMExporterSpec{
		MetricsConfig: &nvidiav1.DCGMExporterMetricsConfig{Name: "custom-dcgm-exporter-metrics"},
	})

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)

	ds := findDaemonSet(t, objs)
	env := envMap(ds.Spec.Template.Spec.Containers[0].Env)
	assert.Equal(t, "/etc/dcgm-exporter/dcgm-metrics.csv", env["DCGM_EXPORTER_COLLECTORS"])

	vol := findVolume(t, ds, "metrics-config")
	require.NotNil(t, vol.ConfigMap)
	assert.Equal(t, "custom-dcgm-exporter-metrics", vol.ConfigMap.Name)
}

func TestDCGMExporterHPCJobMapping(t *testing.T) {
	s := newTestDCGMExporterState(t, false)
	cr := exporterCR(&nvidiav1.DCGMExporterSpec{
		HPCJobMapping: &nvidiav1.DCGMExporterHPCJobMappingConfig{Enabled: new(true)},
	})

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)

	ds := findDaemonSet(t, objs)
	env := envMap(ds.Spec.Template.Spec.Containers[0].Env)
	assert.Equal(t, "/var/lib/dcgm-exporter/job-mapping", env["DCGM_HPC_JOB_MAPPING_DIR"])
	vol := findVolume(t, ds, "hpc-job-mapping")
	require.NotNil(t, vol.HostPath)
	assert.Equal(t, "/var/lib/dcgm-exporter/job-mapping", vol.HostPath.Path)
}

func TestDCGMExporterServiceMonitorSkippedWithoutCRD(t *testing.T) {
	s := newTestDCGMExporterState(t, false) // ServiceMonitor CRD not served
	cr := exporterCR(&nvidiav1.DCGMExporterSpec{
		ServiceMonitor: &nvidiav1.ServiceMonitorConfig{Enabled: new(true)},
	})

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)
	assert.Equal(t, 0, kindCounts(objs)["ServiceMonitor"],
		"ServiceMonitor must be skipped when the Prometheus Operator CRD is absent")
}

func TestDCGMExporterServiceMonitorRendered(t *testing.T) {
	s := newTestDCGMExporterState(t, true) // ServiceMonitor CRD served
	cr := exporterCR(&nvidiav1.DCGMExporterSpec{
		ServiceMonitor: &nvidiav1.ServiceMonitorConfig{
			Enabled:  new(true),
			Interval: "15s",
		},
	})

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)

	sm := findByKind(objs, "ServiceMonitor")
	require.NotNil(t, sm, "ServiceMonitor must render when the CRD is served and it is enabled")
	assert.Equal(t, "nvidia-dcgm-exporter-dra", sm.GetName())
}

func TestDCGMExporterServiceType(t *testing.T) {
	s := newTestDCGMExporterState(t, false)
	cr := exporterCR(&nvidiav1.DCGMExporterSpec{
		ServiceSpec: &nvidiav1.DCGMExporterServiceConfig{
			Type:                  corev1.ServiceTypeNodePort,
			InternalTrafficPolicy: ptr.To(corev1.ServiceInternalTrafficPolicyLocal),
		},
	})

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)

	svc := findByKind(objs, "Service")
	require.NotNil(t, svc)
	svcType, _, _ := unstructured.NestedString(svc.Object, "spec", "type")
	assert.Equal(t, "NodePort", svcType)
	itpValue, _, _ := unstructured.NestedString(svc.Object, "spec", "internalTrafficPolicy")
	assert.Equal(t, "Local", itpValue)
}

// kindNames collects the names of every rendered object of the given kind.
func kindNames(objs []*unstructured.Unstructured, kind string) []string {
	var names []string
	for _, o := range objs {
		if o.GetKind() == kind {
			names = append(names, o.GetName())
		}
	}
	return names
}

// subjectNames collects the ServiceAccount subject names of a rendered RBAC binding.
func subjectNames(t *testing.T, objs []*unstructured.Unstructured, kind, name string) []string {
	t.Helper()
	for _, o := range objs {
		if o.GetKind() != kind || o.GetName() != name {
			continue
		}
		subjects, found, err := unstructured.NestedSlice(o.Object, "subjects")
		require.NoError(t, err)
		require.True(t, found)
		var names []string
		for _, raw := range subjects {
			subject, ok := raw.(map[string]any)
			require.True(t, ok)
			names = append(names, subject["name"].(string))
		}
		return names
	}
	t.Fatalf("%s %q not found in rendered objects", kind, name)
	return nil
}

// TestDCGMExporterServiceAccountRendering covers what the configured ServiceAccount does to
// the rendered manifests: which object is created, and which operands reference it.
func TestDCGMExporterServiceAccountRendering(t *testing.T) {
	const (
		customName = "metrics-identity"
		byoName    = "byo-sa"
	)

	testCases := map[string]struct {
		serviceAccount *nvidiav1.DCGMExporterServiceAccountConfig
		// created is the ServiceAccount the operator renders, empty when it renders none.
		created string
		// referenced is the name every operand has to point at.
		referenced string
	}{
		"the default configuration creates and references the operator default": {
			created:    dcgmExporterDefaultServiceAccountName,
			referenced: dcgmExporterDefaultServiceAccountName,
		},
		"a configured name is created and referenced under that name": {
			serviceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: customName},
			created:        customName,
			referenced:     customName,
		},
		"create=false references the ServiceAccount without rendering it": {
			serviceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: byoName, Create: new(false)},
			created:        "",
			referenced:     byoName,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			s := newTestDCGMExporterState(t, false)
			cr := exporterCR(&nvidiav1.DCGMExporterSpec{ServiceAccount: tc.serviceAccount})

			objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
			require.NoError(t, err)

			if tc.created == "" {
				assert.Empty(t, kindNames(objs, "ServiceAccount"),
					"the operator must not render a ServiceAccount it does not own")
			} else {
				assert.Equal(t, []string{tc.created}, kindNames(objs, "ServiceAccount"))
			}

			assert.Equal(t, tc.referenced, findDaemonSet(t, objs).Spec.Template.Spec.ServiceAccountName)
			assert.Equal(t, []string{tc.referenced},
				subjectNames(t, objs, "RoleBinding", "nvidia-dcgm-exporter-dra"))
			assert.Equal(t, []string{tc.referenced},
				subjectNames(t, objs, "ClusterRoleBinding", "nvidia-dcgm-exporter-dra-read-pods"))
			// Only the subjects follow the ServiceAccount; the binding objects keep their names.
			assert.Equal(t, []string{"nvidia-dcgm-exporter-dra"}, kindNames(objs, "RoleBinding"))
		})
	}
}

// ownedServiceAccount returns a ServiceAccount in the operand namespace, controlled by cr
// and labelled as belonging to this state, the way the sync would have left it.
func ownedServiceAccount(cr *nvidiav1alpha1.GPUCluster, name string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-operator",
			Labels:    map[string]string{consts.StateLabel: "state-dcgm-exporter"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: nvidiav1alpha1.SchemeGroupVersion.String(),
				Kind:       "GPUCluster",
				Name:       cr.Name,
				UID:        cr.UID,
				Controller: new(true),
			}},
		},
	}
}

// unownedServiceAccount returns a ServiceAccount in the operand namespace that the
// operator did not create.
func unownedServiceAccount(name string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test-operator"},
	}
}

// TestDCGMExporterServiceAccountValidation covers the preSync hook, which only rejects a
// configuration the manifests cannot express. It never mutates cluster state -- reclaiming
// what a previous configuration left behind happens in the postSync hook, once the
// operands stopped referencing it.
func TestDCGMExporterServiceAccountValidation(t *testing.T) {
	ctx := context.Background()
	const (
		customName = "metrics-identity"
		byoName    = "byo-sa"
	)

	testCases := map[string]struct {
		serviceAccount *nvidiav1.DCGMExporterServiceAccountConfig
		existing       func(cr *nvidiav1alpha1.GPUCluster) []client.Object
		// expectedError is a substring of the error the hook must return; empty accepts.
		expectedError string
	}{
		"the default configuration needs nothing to exist": {},
		"create=false requires the ServiceAccount to exist": {
			serviceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: byoName, Create: new(false)},
			expectedError:  byoName,
		},
		"create=false accepts an existing ServiceAccount": {
			serviceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: byoName, Create: new(false)},
			existing: func(*nvidiav1alpha1.GPUCluster) []client.Object {
				return []client.Object{unownedServiceAccount(byoName)}
			},
		},
		"a configured name refuses to take over an unowned ServiceAccount": {
			serviceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: customName},
			existing: func(*nvidiav1alpha1.GPUCluster) []client.Object {
				return []client.Object{unownedServiceAccount(customName)}
			},
			expectedError: "not managed by this GPUCluster",
		},
		"a configured name accepts the ServiceAccount it already owns": {
			serviceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: customName},
			existing: func(cr *nvidiav1alpha1.GPUCluster) []client.Object {
				return []client.Object{ownedServiceAccount(cr, customName)}
			},
		},
		"renaming leaves the superseded default for the postSync reclaim": {
			serviceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: customName},
			existing: func(cr *nvidiav1alpha1.GPUCluster) []client.Object {
				return []client.Object{ownedServiceAccount(cr, dcgmExporterDefaultServiceAccountName)}
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			cr := exporterCR(&nvidiav1.DCGMExporterSpec{ServiceAccount: tc.serviceAccount})
			var objs []client.Object
			if tc.existing != nil {
				objs = tc.existing(cr)
			}
			s := newTestDCGMExporterStateWithObjects(t, objs...)

			err := checkDCGMExporterServiceAccount(ctx, s, cr)
			if tc.expectedError != "" {
				require.ErrorContains(t, err, tc.expectedError)
				return
			}
			require.NoError(t, err)

			// Validation only: whatever was there stays there.
			for _, obj := range objs {
				_, getErr := s.getServiceAccount(ctx, obj.GetName())
				require.NoError(t, getErr, "the preSync hook must not remove %q", obj.GetName())
			}
		})
	}
}

// TestDCGMExporterServiceAccountOwnershipReconcile covers the postSync hook: it runs after
// the manifests converged, so the operands already reference the new ServiceAccount and
// the superseded one can be reclaimed.
func TestDCGMExporterServiceAccountOwnershipReconcile(t *testing.T) {
	ctx := context.Background()
	const (
		customName = "metrics-identity"
		byoName    = "byo-sa"
	)

	testCases := map[string]struct {
		serviceAccount *nvidiav1.DCGMExporterServiceAccountConfig
		existing       func(cr *nvidiav1alpha1.GPUCluster) []client.Object
		// deleted names the ServiceAccounts that must be gone afterwards, kept those that
		// must survive, and released those that must survive without operator ownership.
		deleted  []string
		kept     []string
		released []string
	}{
		"the default configuration reclaims nothing": {
			existing: func(cr *nvidiav1alpha1.GPUCluster) []client.Object {
				return []client.Object{ownedServiceAccount(cr, dcgmExporterDefaultServiceAccountName)}
			},
			kept: []string{dcgmExporterDefaultServiceAccountName},
		},
		"renaming reclaims the superseded operator-owned default": {
			serviceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: customName},
			existing: func(cr *nvidiav1alpha1.GPUCluster) []client.Object {
				return []client.Object{
					ownedServiceAccount(cr, dcgmExporterDefaultServiceAccountName),
					ownedServiceAccount(cr, customName),
				}
			},
			deleted: []string{dcgmExporterDefaultServiceAccountName},
			kept:    []string{customName},
		},
		"renaming keeps a previous ServiceAccount the operator does not own": {
			serviceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: customName},
			existing: func(*nvidiav1alpha1.GPUCluster) []client.Object {
				return []client.Object{unownedServiceAccount(dcgmExporterDefaultServiceAccountName)}
			},
			kept: []string{dcgmExporterDefaultServiceAccountName},
		},
		"create=false reclaims the operator-owned default": {
			serviceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: byoName, Create: new(false)},
			existing: func(cr *nvidiav1alpha1.GPUCluster) []client.Object {
				return []client.Object{
					ownedServiceAccount(cr, dcgmExporterDefaultServiceAccountName),
					unownedServiceAccount(byoName),
				}
			},
			deleted: []string{dcgmExporterDefaultServiceAccountName},
			kept:    []string{byoName},
		},
		"create=false releases a ServiceAccount the operator used to own": {
			serviceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{
				Name: dcgmExporterDefaultServiceAccountName, Create: new(false),
			},
			existing: func(cr *nvidiav1alpha1.GPUCluster) []client.Object {
				return []client.Object{ownedServiceAccount(cr, dcgmExporterDefaultServiceAccountName)}
			},
			released: []string{dcgmExporterDefaultServiceAccountName},
		},
		"create=false on a ServiceAccount that was never owned changes nothing": {
			serviceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: byoName, Create: new(false)},
			existing: func(*nvidiav1alpha1.GPUCluster) []client.Object {
				return []client.Object{unownedServiceAccount(byoName)}
			},
			released: []string{byoName},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			cr := exporterCR(&nvidiav1.DCGMExporterSpec{ServiceAccount: tc.serviceAccount})
			s := newTestDCGMExporterStateWithObjects(t, tc.existing(cr)...)

			require.NoError(t, reconcileDCGMExporterServiceAccountOwnership(ctx, s, cr))

			for _, saName := range tc.deleted {
				_, err := s.getServiceAccount(ctx, saName)
				require.True(t, apierrors.IsNotFound(err), "%q must be reclaimed", saName)
			}
			for _, saName := range tc.kept {
				_, err := s.getServiceAccount(ctx, saName)
				require.NoError(t, err, "%q must not be reclaimed", saName)
			}
			for _, saName := range tc.released {
				sa, err := s.getServiceAccount(ctx, saName)
				require.NoError(t, err, "a user-provided ServiceAccount must never be deleted")
				assert.False(t, metav1.IsControlledBy(sa, cr),
					"the owner reference has to go, otherwise the user's ServiceAccount is garbage-collected with the GPUCluster")
				assert.NotContains(t, sa.Labels, consts.StateLabel,
					"the state label has to go, otherwise the state cleanup sweeps the user's ServiceAccount")
			}
		})
	}
}

// TestDCGMExporterServiceAccountAdoptionGuard covers the veto the sync applies when the
// create call reports AlreadyExists: between the preSync check and that call somebody else
// may have created the ServiceAccount, and stamping our controller reference onto it would
// hand it to garbage collection.
func TestDCGMExporterServiceAccountAdoptionGuard(t *testing.T) {
	cr := exporterCR(&nvidiav1.DCGMExporterSpec{})

	current := func(kind, name string, refs []metav1.OwnerReference) *unstructured.Unstructured {
		obj := &unstructured.Unstructured{}
		obj.SetKind(kind)
		obj.SetName(name)
		obj.SetNamespace("test-operator")
		obj.SetOwnerReferences(refs)
		return obj
	}
	ourRef := []metav1.OwnerReference{{
		APIVersion: nvidiav1alpha1.SchemeGroupVersion.String(),
		Kind:       "GPUCluster",
		Name:       cr.Name,
		UID:        cr.UID,
		Controller: new(true),
	}}
	foreignRef := []metav1.OwnerReference{{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Name:       "somebody-else",
		UID:        "other-uid",
		Controller: new(true),
	}}

	testCases := map[string]struct {
		current       *unstructured.Unstructured
		expectedError string
	}{
		"another kind is not this guard's business": {
			current: current("ConfigMap", "metrics-identity", foreignRef),
		},
		"a ServiceAccount this CR already controls is ours to update": {
			current: current("ServiceAccount", "metrics-identity", ourRef),
		},
		"a ServiceAccount somebody else controls is refused": {
			current:       current("ServiceAccount", "metrics-identity", foreignRef),
			expectedError: "not managed by this GPUCluster",
		},
		"an unowned ServiceAccount under a configured name is refused": {
			current:       current("ServiceAccount", "metrics-identity", nil),
			expectedError: "not managed by this GPUCluster",
		},
		"the operator default without owner references stays adoptable": {
			// It predates owner references, i.e. an upgrade from an older release.
			current: current("ServiceAccount", dcgmExporterDefaultServiceAccountName, nil),
		},
		"the operator default somebody else controls is refused": {
			current:       current("ServiceAccount", dcgmExporterDefaultServiceAccountName, foreignRef),
			expectedError: "not managed by this GPUCluster",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			err := guardDCGMExporterServiceAccountAdoption(cr, tc.current)
			if tc.expectedError != "" {
				require.ErrorContains(t, err, tc.expectedError)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestDCGMExporterServiceAccountAdoptionGuardWiring covers the guard where it matters: the
// AlreadyExists path of the sync, which is the only place the operator would ever write an
// owner reference onto an object it did not create.
func TestDCGMExporterServiceAccountAdoptionGuardWiring(t *testing.T) {
	ctx := context.Background()
	cr := exporterCR(&nvidiav1.DCGMExporterSpec{
		ServiceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: "metrics-identity"},
	})

	testScheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(testScheme))
	require.NoError(t, nvidiav1alpha1.AddToScheme(testScheme))

	// The ServiceAccount appeared after the preSync check accepted the configuration.
	existing := unownedServiceAccount("metrics-identity")
	k8sClient := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(existing).Build()

	skel := &stateSkel{
		name:          "state-dcgm-exporter",
		namespace:     "test-operator",
		client:        k8sClient,
		scheme:        testScheme,
		adoptionGuard: guardDCGMExporterServiceAccountAdoption,
	}

	desired := &unstructured.Unstructured{}
	desired.SetAPIVersion("v1")
	desired.SetKind("ServiceAccount")
	desired.SetName("metrics-identity")
	desired.SetNamespace("test-operator")

	err := skel.createOrUpdateObjs(ctx, cr, func(*unstructured.Unstructured) error { return nil },
		[]*unstructured.Unstructured{desired})
	require.ErrorContains(t, err, "not managed by this GPUCluster")

	// The object the guard refused must be left exactly as it was found.
	found, err := (&configurableState{stateSkel: *skel}).getServiceAccount(ctx, "metrics-identity")
	require.NoError(t, err)
	assert.Empty(t, found.OwnerReferences)
	assert.NotContains(t, found.Labels, consts.StateLabel)
}

// TestDCGMExporterServiceAccountReleasedOnDelete covers the combined transition: one
// update both hands the ServiceAccount to the user with create=false and disables the
// exporter. The generic state cleanup deletes every object carrying the state label, and
// the ServiceAccount still carries it from when the operator managed it, so ownership has
// to be released before that cleanup rather than in postSync -- which the disabled path
// never reaches.
func TestDCGMExporterServiceAccountReleasedOnDelete(t *testing.T) {
	ctx := context.Background()
	const byoName = "byo-sa"

	testCases := map[string]struct {
		serviceAccount *nvidiav1.DCGMExporterServiceAccountConfig
		existing       func(cr *nvidiav1alpha1.GPUCluster) []client.Object
		// released must survive with neither this CR's owner reference nor the state label.
		released []string
		// untouched must keep whatever the operator put on it, ready to be swept.
		untouched []string
	}{
		"create=false releases the ServiceAccount the operator used to own": {
			serviceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: byoName, Create: new(false)},
			existing: func(cr *nvidiav1alpha1.GPUCluster) []client.Object {
				return []client.Object{ownedServiceAccount(cr, byoName)}
			},
			released: []string{byoName},
		},
		"create=false on a ServiceAccount the operator never owned changes nothing": {
			serviceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: byoName, Create: new(false)},
			existing: func(*nvidiav1alpha1.GPUCluster) []client.Object {
				return []client.Object{unownedServiceAccount(byoName)}
			},
			released: []string{byoName},
		},
		"a managed ServiceAccount is left for the state cleanup to remove": {
			existing: func(cr *nvidiav1alpha1.GPUCluster) []client.Object {
				return []client.Object{ownedServiceAccount(cr, dcgmExporterDefaultServiceAccountName)}
			},
			untouched: []string{dcgmExporterDefaultServiceAccountName},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			cr := exporterCR(&nvidiav1.DCGMExporterSpec{ServiceAccount: tc.serviceAccount})
			s := newTestDCGMExporterStateWithObjects(t, tc.existing(cr)...)

			require.NoError(t, releaseDCGMExporterServiceAccountOnDelete(ctx, s, cr))

			for _, saName := range tc.released {
				sa, err := s.getServiceAccount(ctx, saName)
				require.NoError(t, err, "a user-provided ServiceAccount must never be deleted")
				assert.False(t, metav1.IsControlledBy(sa, cr),
					"the owner reference has to go, otherwise the user's ServiceAccount is garbage-collected with the GPUCluster")
				assert.NotContains(t, sa.Labels, consts.StateLabel,
					"the state label has to go, otherwise the state cleanup sweeps the user's ServiceAccount")
			}
			for _, saName := range tc.untouched {
				sa, err := s.getServiceAccount(ctx, saName)
				require.NoError(t, err)
				assert.True(t, metav1.IsControlledBy(sa, cr))
				assert.Contains(t, sa.Labels, consts.StateLabel)
			}
		})
	}
}

// TestDCGMExporterSyncReleasesBeforeDeletion drives Sync() on a disabled exporter and
// checks the hook actually runs on that path.
func TestDCGMExporterSyncReleasesBeforeDeletion(t *testing.T) {
	ctx := context.Background()
	const byoName = "byo-sa"

	cr := exporterCR(&nvidiav1.DCGMExporterSpec{
		Enabled:        new(false),
		ServiceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: byoName, Create: new(false)},
	})
	s := newTestDCGMExporterStateWithObjects(t, ownedServiceAccount(cr, byoName))

	_, err := s.Sync(ctx, cr, draSupportedCatalog())
	require.NoError(t, err)

	sa, err := s.getServiceAccount(ctx, byoName)
	require.NoError(t, err, "the user's ServiceAccount must survive disabling the exporter")
	assert.False(t, metav1.IsControlledBy(sa, cr))
	assert.NotContains(t, sa.Labels, consts.StateLabel)
}
