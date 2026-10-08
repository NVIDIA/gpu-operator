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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

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

// newTestDCGMExporterStateWithObjects builds the state with a client that already holds
// the given objects, for the ServiceAccount checks that read cluster state. The DaemonSet
// status is a subresource so a pre-seeded status survives the sync's update -- that is how
// a test keeps the operand short of Ready.
func newTestDCGMExporterStateWithObjects(t *testing.T, objs ...client.Object) *configurableState {
	t.Helper()
	t.Setenv("DCGM_EXPORTER_IMAGE", "nvcr.io/nvidia/k8s/dcgm-exporter:test")

	testScheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(testScheme))
	require.NoError(t, appsv1.AddToScheme(testScheme))
	require.NoError(t, rbacv1.AddToScheme(testScheme))
	require.NoError(t, nvidiav1alpha1.AddToScheme(testScheme))

	// Disabled Sync must exercise real cleanup, rather than skip every kind as unserved.
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion, appsv1.SchemeGroupVersion})
	for _, gvk := range []schema.GroupVersionKind{
		corev1.SchemeGroupVersion.WithKind("ServiceAccount"),
		corev1.SchemeGroupVersion.WithKind("ConfigMap"),
		appsv1.SchemeGroupVersion.WithKind("DaemonSet"),
	} {
		mapper.Add(gvk, meta.RESTScopeNamespace)
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithRESTMapper(mapper).
		WithObjects(objs...).
		WithStatusSubresource(&appsv1.DaemonSet{}).
		Build()
	s, err := NewStateDCGMExporter(k8sClient, "test-operator", testScheme, dcgmExporterManifestDir)
	require.NoError(t, err)
	return s.(*configurableState)
}

// exporterCR returns a sample CR with dcgm-exporter enabled and the given exporter spec.
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

func TestDCGMExporterExternalServiceAccountValidation(t *testing.T) {
	for name, tc := range map[string]struct {
		configured, namespace, err string
		missing, terminating       bool
	}{
		"existing external account": {configured: "metrics", namespace: "test-operator"},
		"missing external account":  {configured: "metrics", missing: true, err: "cannot use"},
		"different namespace":       {configured: "metrics", namespace: "other", err: "cannot use"},
		"reserved name exists":      {configured: dcgmExporterDefaultServiceAccountName, namespace: "test-operator", err: "reserved"},
		"reserved name missing":     {configured: dcgmExporterDefaultServiceAccountName, missing: true, err: "reserved"},
		"terminating account":       {configured: "metrics", namespace: "test-operator", terminating: true, err: "being deleted"},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			cr := exporterCR(&nvidiav1.DCGMExporterSpec{ServiceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: tc.configured}})
			s := newTestDCGMExporterStateWithObjects(t)
			var before *corev1.ServiceAccount
			if !tc.missing {
				sa := selfLabelledServiceAccount(tc.configured)
				sa.Namespace = tc.namespace
				sa.Annotations = map[string]string{"example.com/identity": "keep"}
				sa.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "pull-secret"}}
				sa.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: "external-owner", UID: "external-owner"}}
				require.NoError(t, s.client.Create(ctx, sa))
				if tc.terminating {
					sa.Finalizers = []string{"example.com/keep"}
					require.NoError(t, s.client.Update(ctx, sa))
					require.NoError(t, s.client.Delete(ctx, sa))
				}
				before = &corev1.ServiceAccount{}
				require.NoError(t, s.client.Get(ctx, client.ObjectKeyFromObject(sa), before))
			}
			state, err := s.Sync(ctx, cr, draSupportedCatalog())
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				require.Equal(t, SyncState(SyncStateNotReady), state)
				daemonsets := &appsv1.DaemonSetList{}
				require.NoError(t, s.client.List(ctx, daemonsets))
				require.Empty(t, daemonsets.Items, "invalid identities must not reach operand creation")
			} else {
				require.NoError(t, err)
			}
			if before != nil {
				after := &corev1.ServiceAccount{}
				require.NoError(t, s.client.Get(ctx, client.ObjectKeyFromObject(before), after))
				require.Equal(t, before, after)
			}
			accounts := &corev1.ServiceAccountList{}
			require.NoError(t, s.client.List(ctx, accounts))
			if tc.missing {
				require.Empty(t, accounts.Items)
			} else {
				require.Len(t, accounts.Items, 1, "external mode never creates a ServiceAccount")
			}
		})
	}
}

func TestDCGMExporterServiceAccountTransitions(t *testing.T) {
	ctx := t.Context()
	cr := exporterCR(&nvidiav1.DCGMExporterSpec{})
	cr.UID = "cluster-uid"
	s := newTestDCGMExporterStateWithObjects(t, selfLabelledServiceAccount("metrics-a"), selfLabelledServiceAccount("metrics-b"))
	for _, name := range []string{"", "metrics-a", "metrics-b", ""} {
		cr.Spec.DCGMExporter.ServiceAccount = &nvidiav1.DCGMExporterServiceAccountConfig{Name: name}
		_, err := s.Sync(ctx, cr, draSupportedCatalog())
		require.NoError(t, err)
		ds := &appsv1.DaemonSet{}
		require.NoError(t, s.client.Get(ctx, client.ObjectKey{Namespace: "test-operator", Name: "nvidia-dcgm-exporter-dra"}, ds))
		expectedAccount := name
		if expectedAccount == "" {
			expectedAccount = "nvidia-dcgm-exporter-dra"
		}
		require.Equal(t, expectedAccount, ds.Spec.Template.Spec.ServiceAccountName)
		// Completing rollout must no longer reclaim the managed default.
		ds.Status = appsv1.DaemonSetStatus{ObservedGeneration: ds.Generation, DesiredNumberScheduled: 1, CurrentNumberScheduled: 1, UpdatedNumberScheduled: 1, NumberReady: 1, NumberAvailable: 1}
		require.NoError(t, s.client.Status().Update(ctx, ds))
		state, err := s.Sync(ctx, cr, draSupportedCatalog())
		require.NoError(t, err)
		require.Equal(t, SyncState(SyncStateReady), state)
		accounts := &corev1.ServiceAccountList{}
		require.NoError(t, s.client.List(ctx, accounts))
		require.Len(t, accounts.Items, 3)
		for _, sa := range accounts.Items {
			if sa.Name == dcgmExporterDefaultServiceAccountName {
				require.True(t, metav1.IsControlledBy(&sa, cr))
			} else {
				require.Empty(t, sa.OwnerReferences)
			}
		}
	}
	cr.Spec.DCGMExporter.ServiceAccount.Name = "metrics-a"
	_, err := s.Sync(ctx, cr, draSupportedCatalog())
	require.NoError(t, err)
	sa := &corev1.ServiceAccount{}
	key := client.ObjectKey{Namespace: "test-operator", Name: "metrics-a"}
	require.NoError(t, s.client.Get(ctx, key, sa))
	require.NoError(t, s.client.Delete(ctx, sa))
	state, err := s.Sync(ctx, cr, draSupportedCatalog())
	require.True(t, apierrors.IsNotFound(err))
	require.Equal(t, SyncState(SyncStateNotReady), state)
	require.NoError(t, s.client.Create(ctx, selfLabelledServiceAccount("metrics-a")))
	state, err = s.Sync(ctx, cr, draSupportedCatalog())
	require.NoError(t, err)
	require.Equal(t, SyncState(SyncStateReady), state)
}

func TestDCGMExporterDisabledSyncPreservesExternalAccounts(t *testing.T) {
	for name, spec := range map[string]*nvidiav1.DCGMExporterSpec{
		"spec removed":                nil,
		"default disabled":            {Enabled: new(false)},
		"external reference disabled": {Enabled: new(false), ServiceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: "metrics"}},
		"missing reference disabled":  {Enabled: new(false), ServiceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: "missing"}},
		"reserved reference disabled": {Enabled: new(false), ServiceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: dcgmExporterDefaultServiceAccountName}},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			cr := exporterCR(spec)
			cr.UID = "cluster-uid"
			managed := ownedServiceAccount(cr, dcgmExporterDefaultServiceAccountName)
			external := selfLabelledServiceAccount("metrics")
			external.Annotations = map[string]string{"example.com/identity": "keep"}
			other := selfLabelledServiceAccount("previous-external")
			sibling := otherStateServiceAccount(cr, "nvidia-dcgm-dra")
			cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "exporter-config", Namespace: "test-operator", Labels: map[string]string{consts.StateLabel: dcgmExporterStateName}}}
			s := newTestDCGMExporterStateWithObjects(t, managed, external, other, sibling, cm)
			before := &corev1.ServiceAccount{}
			require.NoError(t, s.client.Get(ctx, client.ObjectKeyFromObject(external), before))
			_, err := s.Sync(ctx, cr, draSupportedCatalog())
			require.NoError(t, err)
			require.True(t, apierrors.IsNotFound(s.client.Get(ctx, client.ObjectKeyFromObject(managed), &corev1.ServiceAccount{})))
			require.True(t, apierrors.IsNotFound(s.client.Get(ctx, client.ObjectKeyFromObject(cm), &corev1.ConfigMap{})))
			after := &corev1.ServiceAccount{}
			require.NoError(t, s.client.Get(ctx, client.ObjectKeyFromObject(external), after))
			require.Equal(t, before, after)
			require.NoError(t, s.client.Get(ctx, client.ObjectKeyFromObject(other), &corev1.ServiceAccount{}))
			require.NoError(t, s.client.Get(ctx, client.ObjectKeyFromObject(sibling), &corev1.ServiceAccount{}))
			state, err := s.Sync(ctx, cr, draSupportedCatalog())
			require.NoError(t, err)
			require.Equal(t, SyncState(SyncStateIgnore), state)
		})
	}
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

func TestDCGMExporterServiceMonitorRelabelings(t *testing.T) {
	s := newTestDCGMExporterState(t, true)
	cr := exporterCR(&nvidiav1.DCGMExporterSpec{
		ServiceMonitor: &nvidiav1.ServiceMonitorConfig{
			Enabled: new(true),
			Relabelings: []*promv1.RelabelConfig{{
				Action:       "replace",
				Regex:        "(.*)",
				SourceLabels: []promv1.LabelName{"__meta_kubernetes_pod_node_name"},
				TargetLabel:  "instance",
			}},
			MetricRelabelings: []*promv1.RelabelConfig{{
				Action:       "replace",
				Regex:        "(.+)",
				SourceLabels: []promv1.LabelName{"exported_namespace"},
				TargetLabel:  "namespace",
			}},
		},
	})

	objs, err := s.getManifestObjects(context.Background(), cr, draSupportedCatalog())
	require.NoError(t, err)

	sm := findByKind(objs, "ServiceMonitor")
	require.NotNil(t, sm)

	endpoints, _, err := unstructured.NestedSlice(sm.Object, "spec", "endpoints")
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	endpoint := endpoints[0].(map[string]any)

	relabelings, ok := endpoint["relabelings"].([]any)
	require.True(t, ok, "relabelings must be rendered")
	require.Len(t, relabelings, 1)
	assert.Equal(t, "instance", relabelings[0].(map[string]any)["targetLabel"])

	metricRelabelings, ok := endpoint["metricRelabelings"].([]any)
	require.True(t, ok, "metricRelabelings must be rendered")
	require.Len(t, metricRelabelings, 1)
	assert.Equal(t, "namespace", metricRelabelings[0].(map[string]any)["targetLabel"])
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
		"an empty block retains the GPUCluster default": {
			serviceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{},
			created:        "nvidia-dcgm-exporter-dra",
			referenced:     "nvidia-dcgm-exporter-dra",
		},
		"a configured name is referenced without creation": {
			serviceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: customName},
			referenced:     customName,
		},
		"a numeric-looking name remains a string": {
			serviceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: "123"},
			referenced:     "123",
		},
		"a boolean-looking name remains a string": {
			serviceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: "true"},
			referenced:     "true",
		},
		"external accounts are not rendered": {
			serviceAccount: &nvidiav1.DCGMExporterServiceAccountConfig{Name: byoName},
			created:        "",
			referenced:     byoName,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			s := newTestDCGMExporterState(t, false)
			cr := exporterCR(&nvidiav1.DCGMExporterSpec{ServiceAccount: tc.serviceAccount})

			objs, err := s.getManifestObjects(context.Background(), cr, draSupportedOpenshiftCatalog())
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
			scc := findByKind(objs, "SecurityContextConstraints")
			require.NotNil(t, scc)
			users, found, err := unstructured.NestedStringSlice(scc.Object, "users")
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, []string{"system:serviceaccount:test-operator:" + tc.referenced}, users)
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

// selfLabelledServiceAccount returns a ServiceAccount the user created and labelled with
// this state's label themselves. The operator never owned it, so it is not ours to mutate.
func selfLabelledServiceAccount(name string) *corev1.ServiceAccount {
	sa := unownedServiceAccount(name)
	sa.Labels = map[string]string{consts.StateLabel: "state-dcgm-exporter"}
	return sa
}

// otherStateServiceAccount returns a ServiceAccount another state of the same GPUCluster
// manages: it carries the CR's controller reference like every operand object does, but
// not this state's label.
func otherStateServiceAccount(cr *nvidiav1alpha1.GPUCluster, name string) *corev1.ServiceAccount {
	sa := ownedServiceAccount(cr, name)
	sa.Labels[consts.StateLabel] = "state-driver"
	return sa
}

func TestDCGMExporterDisabledSyncRetriesMetadataConflict(t *testing.T) {
	ctx := t.Context()
	cr := exporterCR(&nvidiav1.DCGMExporterSpec{Enabled: new(false)})
	old := ownedServiceAccount(cr, dcgmExporterDefaultServiceAccountName)
	s := newTestDCGMExporterStateWithObjects(t, old)
	deletes := 0
	s.client = interceptor.NewClient(s.client.(client.WithWatch), interceptor.Funcs{Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
		deletes++
		if deletes == 1 {
			current := &corev1.ServiceAccount{}
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(old), current))
			current.Annotations = map[string]string{"example.com/last-audit": "updated"}
			require.NoError(t, c.Update(ctx, current))
		}
		return c.Delete(ctx, obj, opts...)
	}})
	status, err := s.Sync(ctx, cr, draSupportedCatalog())
	require.True(t, apierrors.IsConflict(err))
	require.Equal(t, SyncState(SyncStateError), status)
	status, err = s.Sync(ctx, cr, draSupportedCatalog())
	require.NoError(t, err)
	require.Equal(t, SyncState(SyncStateNotReady), status)
	status, err = s.Sync(ctx, cr, draSupportedCatalog())
	require.NoError(t, err)
	require.Equal(t, SyncState(SyncStateIgnore), status)
	require.Equal(t, 2, deletes)
}

func TestDCGMExporterDisabledSyncPreservesDefaultReplacement(t *testing.T) {
	cr := exporterCR(&nvidiav1.DCGMExporterSpec{Enabled: new(false)})
	replacement := selfLabelledServiceAccount(dcgmExporterDefaultServiceAccountName)
	s := newTestDCGMExporterStateWithObjects(t, replacement)
	state, err := s.Sync(t.Context(), cr, draSupportedCatalog())
	require.NoError(t, err)
	require.Equal(t, SyncState(SyncStateIgnore), state)
	found := &corev1.ServiceAccount{}
	require.NoError(t, s.client.Get(t.Context(), client.ObjectKeyFromObject(replacement), found))
	require.Empty(t, found.OwnerReferences)
}

func TestDCGMExporterDeletionFilterUsesNameAndOwner(t *testing.T) {
	cr := exporterCR(&nvidiav1.DCGMExporterSpec{Enabled: new(false)})
	cr.UID = "cluster-uid"
	for name, tc := range map[string]struct {
		name       string
		owned      bool
		wantDelete bool
	}{
		"controlled default without a marker": {name: dcgmExporterDefaultServiceAccountName, owned: true, wantDelete: true},
		"unowned default":                     {name: dcgmExporterDefaultServiceAccountName},
		"another operand's account":           {name: "nvidia-dcgm-dra", owned: true},
		"external account":                    {name: "metrics"},
	} {
		t.Run(name, func(t *testing.T) {
			sa := unownedServiceAccount(tc.name)
			if tc.owned {
				sa = ownedServiceAccount(cr, tc.name)
			}
			// The generic state sweep already selects by state label. The account
			// filter itself only needs the reserved name and controller reference.
			sa.Labels = nil
			obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(sa)
			require.NoError(t, err)
			u := &unstructured.Unstructured{Object: obj}
			u.SetAPIVersion("v1")
			u.SetKind("ServiceAccount")
			require.Equal(t, tc.wantDelete, canDeleteDCGMExporterObject(cr, u))
		})
	}
}
