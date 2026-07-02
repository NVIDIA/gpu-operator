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
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	}, nil
}

func buildDCGMExporterRenderData(ctx context.Context, s *configurableState, cr *nvidiav1alpha1.GPUCluster, imagePath, apiVersion, openshiftVersion string) (interface{}, error) {
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
		PodMetadataEnabled:           spec.IsKubernetesPodMetadataEnabled(),
		EnablePodLabels:              spec.IsPodLabelsEnabled(),
		EnablePodUID:                 spec.IsPodUIDEnabled(),
		HostPID:                      spec.IsHostPIDEnabled(),
		HostNetwork:                  spec.IsHostNetworkEnabled(),
		MetricsConfigName:            metricsConfigName,
		ServiceMonitorEnabled:        serviceMonitorEnabled,
		PodResourcesDir:              filepath.Join(kubeletRootDir, "pod-resources"),
		ServiceType:                  serviceType,
		ServiceInternalTrafficPolicy: serviceInternalTrafficPolicy,
	}, nil
}

// serviceMonitorCRDServed reports whether the cluster serves the monitoring.coreos.com
// ServiceMonitor kind (i.e. the Prometheus Operator CRDs are installed).
func serviceMonitorCRDServed(k8sClient client.Client) bool {
	_, err := k8sClient.RESTMapper().RESTMapping(
		schema.GroupKind{Group: "monitoring.coreos.com", Kind: "ServiceMonitor"}, "v1")
	return err == nil
}
