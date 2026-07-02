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
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/source"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
)

type ctrlManager ctrl.Manager
type SyncingSource source.SyncingSource

// driverSpec is a wrapper of NVIDIADriverSpec with an additional ImagePath field
// which is to be populated with the fully-qualified image path.
type driverSpec struct {
	Spec             *nvidiav1alpha1.NVIDIADriverSpec
	AppName          string
	Name             string
	ImagePath        string
	ManagerImagePath string
	OSVersion        string
}

// gdsDriverSpec is a wrapper of GPUDirectStorageSpec with an additional ImagePath field
// which is to be populated with the fully-qualified image path.
type gdsDriverSpec struct {
	Spec      *nvidiav1alpha1.GPUDirectStorageSpec
	ImagePath string
}

// gdrcopyDriverSpec is a wrapper of GDRCopySpec with an additional ImagePath field
// which is to be populated with the fully-qualified image path.
type gdrcopyDriverSpec struct {
	Spec      *nvidiav1alpha1.GDRCopySpec
	ImagePath string
}

// draDriverSpec is a wrapper of DRADriverSpec with the fully-qualified image paths
// populated: ImagePath for the DRA driver containers and InitImagePath for the
// driver-validation init container (shipped in the gpu-operator image).
type draDriverSpec struct {
	Spec          *nvidiav1alpha1.DRADriverSpec
	ImagePath     string
	InitImagePath string
}

// dcgmSpec is a wrapper of DCGMSpec with the resolved image path.
type dcgmSpec struct {
	Spec      *nvidiav1.DCGMSpec
	ImagePath string
}

// dcgmRenderData is the templating data for the standalone DCGM hostengine manifests.
type dcgmRenderData struct {
	DCGM       *dcgmSpec
	Daemonsets *nvidiav1.DaemonsetsSpec
	Namespace  string
	// OpenshiftVersion gates OpenShift-only objects (SecurityContextConstraints); empty
	// on vanilla Kubernetes.
	OpenshiftVersion string
	// ResourceClaimAPIVersion is the apiVersion to render on ResourceClaimTemplate
	// objects, determined from the resource.k8s.io version served by the cluster.
	ResourceClaimAPIVersion string
}

// dcgmExporterSpec is a wrapper of DCGMExporterSpec with the resolved image path.
type dcgmExporterSpec struct {
	Spec      *nvidiav1.DCGMExporterSpec
	ImagePath string
}

// dcgmExporterRenderData is the templating data for the dcgm-exporter manifests. Values
// that mirror the ClusterPolicy TransformDCGMExporter logic are precomputed in
// getManifestObjects so the templates stay declarative.
type dcgmExporterRenderData struct {
	DCGMExporter *dcgmExporterSpec
	Daemonsets   *nvidiav1.DaemonsetsSpec
	Namespace    string
	// OpenshiftVersion gates OpenShift-only objects (SecurityContextConstraints); empty
	// on vanilla Kubernetes.
	OpenshiftVersion string
	// ResourceClaimAPIVersion is the apiVersion to render on ResourceClaimTemplate objects.
	ResourceClaimAPIVersion string
	RemoteHostEngine        string
	Collectors              string
	HPCJobMappingDir        string
	PodLabelAllowlistRegex  string
	// PodMetadataEnabled mounts the ServiceAccount token and binds the pods-read ClusterRole.
	PodMetadataEnabled           bool
	EnablePodLabels              bool
	EnablePodUID                 bool
	HostPID                      bool
	HostNetwork                  bool
	MetricsConfigName            string
	ServiceMonitorEnabled        bool
	PodResourcesDir              string
	ServiceType                  string
	ServiceInternalTrafficPolicy string
}

// validatorRenderData is the templating data for the DRA validator manifests. It
// reuses draDriverSpec so .Validator.ImagePath carries the gpu-operator image (which
// runs in the validator) and .Validator.Spec exposes the image pull settings.
type validatorRenderData struct {
	Validator  *draDriverSpec
	Daemonsets *nvidiav1.DaemonsetsSpec
	Namespace  string
	// OpenshiftVersion gates OpenShift-only objects (SecurityContextConstraints); empty
	// on vanilla Kubernetes.
	OpenshiftVersion string
	// ResourceClaimAPIVersion is the apiVersion to render on the ResourceClaimTemplate,
	// determined from the resource.k8s.io version served by the cluster.
	ResourceClaimAPIVersion string
}

// draDriverRenderData is the templating data for the DRA driver manifests.
type draDriverRenderData struct {
	DRADriver  *draDriverSpec
	HostPaths  *nvidiav1.HostPathsSpec
	Daemonsets *nvidiav1.DaemonsetsSpec
	Namespace  string
	// OpenshiftVersion gates OpenShift-only objects (SecurityContextConstraints); empty
	// on vanilla Kubernetes.
	OpenshiftVersion string
	// DeviceClassAPIVersion is the apiVersion to render on DeviceClass objects,
	// determined from the resource.k8s.io version served by the cluster.
	DeviceClassAPIVersion string
	// FeatureGates is the pre-rendered FEATURE_GATES env value (empty when none).
	FeatureGates string
	// priorityClassName and tolerations for the kubelet-plugin DaemonSet, taken from the
	// shared daemonsets spec.
	KubeletPluginPriorityClassName string
	KubeletPluginTolerations       []corev1.Toleration
	// GPUsHealthcheckPort and ComputeDomainsHealthcheckPort are the resolved gRPC health
	// service ports of the two kubelet-plugin containers (the spec value, or the
	// per-container default); a negative value omits the startup and liveness probes.
	GPUsHealthcheckPort           int32
	ComputeDomainsHealthcheckPort int32
}
