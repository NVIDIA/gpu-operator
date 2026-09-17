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
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/internal/ownership"
)

const (
	// dcgmExporterImageEnvName is the fallback env var for the dcgm-exporter image when the
	// CR does not specify repository/image/version.
	dcgmExporterImageEnvName = "DCGM_EXPORTER_IMAGE"

	// dcgmRemoteHostEngine points dcgm-exporter at the standalone nvidia-dcgm-dra
	// hostengine Service (manifests/state-dcgm/0600_service.yaml).
	dcgmRemoteHostEngine = "nvidia-dcgm-dra:5555"

	dcgmExporterDefaultCollectors     = "/etc/dcgm-exporter/dcp-metrics-included.csv"
	dcgmExporterCustomCollectors      = "/etc/dcgm-exporter/dcgm-metrics.csv"
	dcgmExporterDefaultKubeletRootDir = "/var/lib/kubelet"
	dcgmExporterDefaultJobMappingDir  = "/var/lib/dcgm-exporter/job-mapping"

	// dcgmExporterDefaultServiceAccountName is the ServiceAccount the DRA operands
	// reference unless the user configures a different one.
	dcgmExporterDefaultServiceAccountName = "nvidia-dcgm-exporter-dra"

	// dcgmExporterStateName is this state's name, and the value syncObjects writes into
	// the state label of every object it applies.
	dcgmExporterStateName = "state-dcgm-exporter"
)

// dcgmExporterServiceAccountMarker identifies the ServiceAccounts this state created.
// It reuses the state label rather than introducing a new one so that accounts created
// by an earlier release are still recognized after an upgrade.
var dcgmExporterServiceAccountMarker = ownership.Marker{
	Key:   consts.StateLabel,
	Value: dcgmExporterStateName,
}

func NewStateDCGMExporter(
	k8sClient client.Client,
	namespace string,
	scheme *runtime.Scheme,
	manifestDir string) (State, error) {

	skel, err := newStateSkel(k8sClient, namespace, scheme, manifestDir,
		"state-dcgm-exporter", "NVIDIA DCGM Exporter deployed in the cluster")
	if err != nil {
		return nil, err
	}
	skel.adoptionGuard = guardDCGMExporterServiceAccountAdoption
	return &configurableState{
		stateSkel: skel,
		isEnabled: func(cr *nvidiav1alpha1.GPUCluster) bool {
			return cr.Spec.DCGMExporter != nil && cr.Spec.DCGMExporter.IsEnabled()
		},
		imageOverride: func(cr *nvidiav1alpha1.GPUCluster) (string, string, string) {
			spec := cr.Spec.DCGMExporter
			return spec.Repository, spec.Image, spec.Version
		},
		imageEnvName:    dcgmExporterImageEnvName,
		buildRenderData: buildDCGMExporterRenderData,
		preSync:         checkDCGMExporterServiceAccount,
		postSync:        reclaimSupersededDCGMExporterServiceAccounts,
		preDelete:       releaseDCGMExporterServiceAccountOnDelete,
	}, nil
}

func buildDCGMExporterRenderData(ctx context.Context, s *configurableState, cr *nvidiav1alpha1.GPUCluster, imagePath, apiVersion, openshiftVersion string) (any, error) {
	spec := cr.Spec.DCGMExporter

	// When standalone DCGM is enabled the exporter targets it; otherwise it runs embedded.
	remoteHostEngine := ""
	if dcgmEnabled(cr) {
		remoteHostEngine = dcgmRemoteHostEngine
	}

	collectors := dcgmExporterDefaultCollectors
	metricsConfigName := ""
	if spec.MetricsConfig != nil && spec.MetricsConfig.Name != "" {
		metricsConfigName = spec.MetricsConfig.Name
		collectors = dcgmExporterCustomCollectors
	}

	hpcJobMappingDir := ""
	if spec.IsHPCJobMappingEnabled() {
		hpcJobMappingDir = spec.GetHPCJobMappingDirectory()
		if hpcJobMappingDir == "" {
			hpcJobMappingDir = dcgmExporterDefaultJobMappingDir
		}
	}

	kubeletRootDir := cr.Spec.HostPaths.KubeletRootDir
	if kubeletRootDir == "" {
		kubeletRootDir = dcgmExporterDefaultKubeletRootDir
	}

	// Skip the ServiceMonitor when its CRD is absent so a default install without the
	// Prometheus Operator does not fail (matches the ClusterPolicy path).
	serviceMonitorEnabled := spec.ServiceMonitor != nil &&
		spec.ServiceMonitor.Enabled != nil && *spec.ServiceMonitor.Enabled
	if serviceMonitorEnabled && !serviceMonitorCRDServed(s.client) {
		log.FromContext(ctx).V(consts.LogLevelInfo).Info(
			"ServiceMonitor CRD not served; skipping dcgm-exporter ServiceMonitor creation")
		serviceMonitorEnabled = false
	}

	serviceType := "ClusterIP"
	serviceInternalTrafficPolicy := ""
	if spec.ServiceSpec != nil {
		if spec.ServiceSpec.Type != "" {
			serviceType = string(spec.ServiceSpec.Type)
		}
		if spec.ServiceSpec.InternalTrafficPolicy != nil {
			serviceInternalTrafficPolicy = string(*spec.ServiceSpec.InternalTrafficPolicy)
		}
	}

	daemonsets := cr.Spec.Daemonsets
	return &dcgmExporterRenderData{
		DCGMExporter:                 &dcgmExporterSpec{Spec: spec, ImagePath: imagePath},
		Daemonsets:                   &daemonsets,
		Namespace:                    s.namespace,
		OpenshiftVersion:             openshiftVersion,
		ResourceClaimAPIVersion:      apiVersion,
		RemoteHostEngine:             remoteHostEngine,
		Collectors:                   collectors,
		HPCJobMappingDir:             hpcJobMappingDir,
		PodLabelAllowlistRegex:       strings.Join(spec.PodLabelAllowlistRegex, ","),
		EnablePodLabels:              spec.IsPodLabelsEnabled(),
		EnablePodUID:                 spec.IsPodUIDEnabled(),
		HostPID:                      spec.IsHostPIDEnabled(),
		HostNetwork:                  spec.IsHostNetworkEnabled(),
		MetricsConfigName:            metricsConfigName,
		ServiceMonitorEnabled:        serviceMonitorEnabled,
		PodResourcesDir:              filepath.Join(kubeletRootDir, "pod-resources"),
		ServiceType:                  serviceType,
		ServiceInternalTrafficPolicy: serviceInternalTrafficPolicy,
		ServiceAccountName:           spec.GetServiceAccountName(dcgmExporterDefaultServiceAccountName),
		CreateServiceAccount:         spec.IsServiceAccountCreateEnabled(),
	}, nil
}

// checkDCGMExporterServiceAccount runs before the manifests are applied and reconciles the
// parts of the ServiceAccount contract the templates cannot express: a ServiceAccount the
// user brings has to already exist and is handed back if the operator used to manage it,
// and one the operator would manage must not be an existing object owned by somebody else.
func checkDCGMExporterServiceAccount(ctx context.Context, s *configurableState, cr *nvidiav1alpha1.GPUCluster) error {
	spec := cr.Spec.DCGMExporter
	name := spec.GetServiceAccountName(dcgmExporterDefaultServiceAccountName)

	if !spec.IsServiceAccountCreateEnabled() {
		// The manifests omit the ServiceAccount entirely, so a missing one would leave
		// the DaemonSet pending without any signal.
		sa, err := s.getServiceAccount(ctx, name)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf(
					"ServiceAccount %q configured with create=false does not exist in namespace %q",
					name, s.namespace)
			}
			return err
		}
		// The same object may have been operator-managed before create was set to false.
		// It is handed back here, before the operands sync, rather than once they
		// converged: a DaemonSet that never becomes Ready would otherwise keep this CR's
		// controller reference on the ServiceAccount indefinitely, and deleting the
		// GPUCluster would garbage-collect an object the user now owns.
		return s.releaseServiceAccount(ctx, cr, sa)
	}

	if name == dcgmExporterDefaultServiceAccountName {
		return nil
	}

	// Adopting an object the operator did not create would hand it to garbage collection
	// on CR deletion, so a name that is already taken has to be opted into explicitly.
	// guardDCGMExporterServiceAccountAdoption re-checks this after the create call, for
	// an object that appears in between.
	existing, err := s.getServiceAccount(ctx, name)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err == nil && !ownership.IsManaged(existing, cr, dcgmExporterServiceAccountMarker) {
		// Being controlled by this GPUCluster is not enough: it controls every operand's
		// ServiceAccount, so a configured name pointing at a sibling operand's account
		// (nvidia-dcgm-dra, nvidia-dra-validator) would pass. Adopting one would stamp
		// this state's label onto it and hand its lifecycle to the exporter.
		return dcgmExporterServiceAccountTakeoverError(name, s.namespace)
	}

	return nil
}

// dcgmExporterServiceAccountTakeoverError is the error returned when the configured
// ServiceAccount exists but belongs to somebody else.
func dcgmExporterServiceAccountTakeoverError(name, namespace string) error {
	return ownership.ConflictError("GPUCluster", name, namespace)
}

// guardDCGMExporterServiceAccountAdoption stops createOrUpdateObjs from taking over a
// ServiceAccount that appeared between the preSync ownership check and the create call.
// Without it the AlreadyExists path would stamp this CR's controller reference onto an
// object somebody else owns, handing it to garbage collection with the GPUCluster.
func guardDCGMExporterServiceAccountAdoption(owner metav1.Object, current *unstructured.Unstructured) error {
	if current.GetKind() != "ServiceAccount" {
		return nil
	}
	// The marker is required alongside the controller reference: a sibling operand's
	// ServiceAccount carries the same reference, and adopting one would relabel it into
	// this state.
	if ownership.IsManaged(current, owner, dcgmExporterServiceAccountMarker) {
		return nil
	}
	if current.GetName() == dcgmExporterDefaultServiceAccountName && len(current.GetOwnerReferences()) == 0 {
		// The operator default may predate owner references (upgrade from an older
		// release), so it stays adoptable. An account with no owner reference at all
		// cannot be a sibling operand's, so this does not reopen the case above.
		return nil
	}
	return dcgmExporterServiceAccountTakeoverError(current.GetName(), current.GetNamespace())
}

// reclaimSupersededDCGMExporterServiceAccounts runs once the manifests converged and removes
// the ServiceAccounts this state created under a previous configuration. Waiting for
// convergence matters here, unlike for the hand-back in checkDCGMExporterServiceAccount:
// deleting a ServiceAccount the DaemonSet still referenced would leave its pods without an
// identity, whereas releasing ownership changes nothing the operands can observe.
func reclaimSupersededDCGMExporterServiceAccounts(ctx context.Context, s *configurableState, cr *nvidiav1alpha1.GPUCluster) error {
	return s.deleteSupersededServiceAccounts(ctx, cr,
		cr.Spec.DCGMExporter.GetServiceAccountName(dcgmExporterDefaultServiceAccountName))
}

// releaseDCGMExporterServiceAccountOnDelete hands a user-provided ServiceAccount back
// before the state is torn down. Disabling the exporter deletes every object carrying
// this state's label, and a ServiceAccount taken over with create=false still carries it
// from when the operator managed it -- without this, turning the exporter off would
// delete an object the operator no longer owns.
func releaseDCGMExporterServiceAccountOnDelete(ctx context.Context, s *configurableState, cr *nvidiav1alpha1.GPUCluster) error {
	spec := cr.Spec.DCGMExporter
	// A nil spec never named a ServiceAccount, and a managed one is meant to go with the
	// state; only the user-provided case has to survive.
	if spec == nil || spec.IsServiceAccountCreateEnabled() {
		return nil
	}
	sa, err := s.getServiceAccount(ctx, spec.GetServiceAccountName(dcgmExporterDefaultServiceAccountName))
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return s.releaseServiceAccount(ctx, cr, sa)
}

// releaseServiceAccount hands a ServiceAccount the user now owns back to them by dropping
// this GPUCluster's controller reference and the state label. Only a ServiceAccount this
// state managed is touched: every operand of the GPUCluster carries its controller
// reference, so checking ownership alone would also strip the reference and label off
// another state's ServiceAccount when the user points create=false at it.
func (s *configurableState) releaseServiceAccount(ctx context.Context, cr *nvidiav1alpha1.GPUCluster, sa *corev1.ServiceAccount) error {
	if !dcgmExporterServiceAccountMarker.Matches(sa.Labels) {
		return nil
	}
	ownership.ReleaseOwner(sa, cr.GetUID())
	delete(sa.Labels, consts.StateLabel)
	log.FromContext(ctx).V(consts.LogLevelInfo).Info(
		"Releasing ownership of a user-provided dcgm-exporter ServiceAccount", "Name", sa.Name)
	return s.client.Update(ctx, sa)
}

// getServiceAccount reads a ServiceAccount from the operand namespace.
func (s *configurableState) getServiceAccount(ctx context.Context, name string) (*corev1.ServiceAccount, error) {
	sa := &corev1.ServiceAccount{}
	err := s.client.Get(ctx, types.NamespacedName{Namespace: s.namespace, Name: name}, sa)
	return sa, err
}

// deleteSupersededServiceAccounts removes every ServiceAccount carrying this state's label
// that this CR controls, except the one named keep. Finding them by label rather than by
// name is what covers a rename from one custom name to another, or back to the default: no
// record of the previous configuration exists, but every ServiceAccount the state ever
// created still carries its label. A ServiceAccount the user provisioned under one of those
// names carries no controller reference from this CR and is left alone.
func (s *configurableState) deleteSupersededServiceAccounts(ctx context.Context, cr *nvidiav1alpha1.GPUCluster, keep string) error {
	list := &corev1.ServiceAccountList{}
	if err := s.client.List(ctx, list,
		client.InNamespace(s.namespace),
		dcgmExporterServiceAccountMarker.Selector()); err != nil {
		return err
	}
	for i := range list.Items {
		sa := &list.Items[i]
		if sa.Name == keep || !ownership.IsManaged(sa, cr, dcgmExporterServiceAccountMarker) {
			continue
		}
		log.FromContext(ctx).V(consts.LogLevelInfo).Info(
			"Removing a dcgm-exporter ServiceAccount superseded by the configured one", "Name", sa.Name)
		if err := s.client.Delete(ctx, sa); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// serviceMonitorCRDServed reports whether the cluster serves the monitoring.coreos.com
// ServiceMonitor kind (i.e. the Prometheus Operator CRDs are installed).
func serviceMonitorCRDServed(k8sClient client.Client) bool {
	_, err := k8sClient.RESTMapper().RESTMapping(
		schema.GroupKind{Group: "monitoring.coreos.com", Kind: "ServiceMonitor"}, "v1")
	return err == nil
}
