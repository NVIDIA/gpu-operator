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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/consts"
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
)

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

// checkDCGMExporterServiceAccount reconciles the parts of the ServiceAccount contract the
// manifests cannot express: a ServiceAccount the user brings has to already exist, one the
// operator would manage must not be an existing object owned by somebody else, and the
// operator-owned default is removed once a different name takes over.
func checkDCGMExporterServiceAccount(ctx context.Context, s *configurableState, cr *nvidiav1alpha1.GPUCluster) error {
	spec := cr.Spec.DCGMExporter
	name := spec.GetServiceAccountName(dcgmExporterDefaultServiceAccountName)

	if !spec.IsServiceAccountCreateEnabled() {
		// The manifests omit the ServiceAccount entirely, so a missing one would leave
		// the DaemonSet pending without any signal.
		if _, err := s.getServiceAccount(ctx, name); err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf(
					"ServiceAccount %q configured with create=false does not exist in namespace %q",
					name, s.namespace)
			}
			return err
		}
		// Handing the exporter over to a user-provided ServiceAccount supersedes the one a
		// default install created. Skipped when the user brings the default name itself,
		// since that is the object now being referenced.
		if name == dcgmExporterDefaultServiceAccountName {
			return nil
		}
		return s.deleteOwnedServiceAccount(ctx, cr, dcgmExporterDefaultServiceAccountName)
	}

	if name == dcgmExporterDefaultServiceAccountName {
		return nil
	}

	// Adopting an object the operator did not create would hand it to garbage collection
	// on CR deletion, so a name that is already taken has to be opted into explicitly.
	existing, err := s.getServiceAccount(ctx, name)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err == nil && !metav1.IsControlledBy(existing, cr) {
		return fmt.Errorf(
			"ServiceAccount %q already exists in namespace %q and is not managed by this GPUCluster; "+
				"set dcgmExporter.serviceAccount.create to false to reference it",
			name, s.namespace)
	}

	return s.deleteOwnedServiceAccount(ctx, cr, dcgmExporterDefaultServiceAccountName)
}

// getServiceAccount reads a ServiceAccount from the operand namespace.
func (s *configurableState) getServiceAccount(ctx context.Context, name string) (*corev1.ServiceAccount, error) {
	sa := &corev1.ServiceAccount{}
	err := s.client.Get(ctx, types.NamespacedName{Namespace: s.namespace, Name: name}, sa)
	return sa, err
}

// deleteOwnedServiceAccount removes a ServiceAccount left behind by a previous
// configuration, but only when this CR owns it: an object the user provisioned under the
// same name is left alone. Renaming from one custom name to another is not tracked, so
// only the operator default is reclaimed here.
func (s *configurableState) deleteOwnedServiceAccount(ctx context.Context, cr *nvidiav1alpha1.GPUCluster, name string) error {
	sa, err := s.getServiceAccount(ctx, name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if !metav1.IsControlledBy(sa, cr) {
		return nil
	}
	log.FromContext(ctx).V(consts.LogLevelInfo).Info(
		"Removing the superseded dcgm-exporter ServiceAccount", "Name", name)
	if err := s.client.Delete(ctx, sa); err != nil && !apierrors.IsNotFound(err) {
		return err
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
