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
	"os"
	"regexp"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/v1alpha1"
	"github.com/NVIDIA/gpu-operator/controllers/clusterinfo"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/internal/image"
	"github.com/NVIDIA/gpu-operator/internal/render"
	"github.com/NVIDIA/gpu-operator/internal/utils"
)

const (
	nfdOSReleaseIDLabelKey = "feature.node.kubernetes.io/system-os_release.ID"
	nfdOSVersionIDLabelKey = "feature.node.kubernetes.io/system-os_release.VERSION_ID"
)

type stateDriver struct {
	stateSkel
}

var _ State = (*stateDriver)(nil)

type driverRuntimeSpec struct {
	Namespace                     string
	KubernetesVersion             string
	OpenshiftVersion              string
	OpenshiftDriverToolkitEnabled bool
	OpenshiftDriverToolkitImages  map[string]string
	OpenshiftProxySpec            *configv1.ProxySpec
	NodePools                     []nodePool
}

type openshiftSpec struct {
	ToolkitImage string
	RHCOSVersion string
}

type precompiledSpec struct {
	KernelVersion          string
	SanitizedKernelVersion string
}

type additionalConfigs struct {
	VolumeMounts []corev1.VolumeMount
	Volumes      []corev1.Volume
}

type driverRenderData struct {
	Driver            *driverSpec
	Operator          *gpuv1.OperatorSpec
	Validator         *validatorSpec
	GDS               *gdsDriverSpec
	GPUDirectRDMA     *gpuv1.GPUDirectRDMASpec
	Runtime           *driverRuntimeSpec
	Openshift         *openshiftSpec
	Precompiled       *precompiledSpec
	AdditionalConfigs *additionalConfigs
}

func NewStateDriver(
	k8sClient client.Client,
	scheme *runtime.Scheme,
	manifestDir string) (State, error) {

	files, err := utils.GetFilesWithSuffix(manifestDir, render.ManifestFileSuffix...)
	if err != nil {
		return nil, fmt.Errorf("failed to get files from manifest directory: %v", err)
	}

	renderer := render.NewRenderer(files)
	state := &stateDriver{
		stateSkel: stateSkel{
			name:        "state-driver",
			description: "NVIDIA driver deployed in the cluster",
			client:      k8sClient,
			scheme:      scheme,
			renderer:    renderer,
		},
	}
	return state, nil
}

func (s *stateDriver) Sync(ctx context.Context, customResource interface{}, infoCatalog InfoCatalog) (SyncState, error) {
	cr, ok := customResource.(*nvidiav1alpha1.NVIDIADriver)
	if !ok {
		return SyncStateError, fmt.Errorf("NVIDIADriver CR not provided as input to Sync()")
	}

	info := infoCatalog.Get(InfoTypeClusterPolicyCR)
	if info == nil {
		return SyncStateError, fmt.Errorf("failed to get ClusterPolicy CR from info catalog")
	}
	clusterPolicy, ok := info.(gpuv1.ClusterPolicy)
	if !ok {
		return SyncStateError, fmt.Errorf("failed to get ClusterPolicy CR from info catalog")
	}

	info = infoCatalog.Get(InfoTypeClusterInfo)
	if info == nil {
		return SyncStateNotReady, fmt.Errorf("failed to get cluster info from info catalog")
	}
	clusterInfo := info.(clusterinfo.Interface)

	objs, err := s.getManifestObjects(ctx, cr, &clusterPolicy, clusterInfo)
	if err != nil {
		return SyncStateNotReady, fmt.Errorf("failed to create k8s objects from manifests: %v", err)
	}

	// Create objects if they dont exist, Update objects if they do exist
	err = s.createOrUpdateObjs(ctx, func(obj *unstructured.Unstructured) error {
		if err := controllerutil.SetControllerReference(cr, obj, s.scheme); err != nil {
			return fmt.Errorf("failed to set controller reference for object: %v", err)
		}
		return nil
	}, objs)
	if err != nil {
		return SyncStateNotReady, fmt.Errorf("failed to create/update objects: %v", err)
	}

	// Check objects status
	syncState, err := s.getSyncState(ctx, objs)
	if err != nil {
		return SyncStateNotReady, fmt.Errorf("failed to get sync state: %v", err)
	}
	return syncState, nil
}

func (s *stateDriver) GetWatchSources(mgr ctrlManager) map[string]SyncingSource {
	wr := make(map[string]SyncingSource)
	wr["DaemonSet"] = source.Kind(
		mgr.GetCache(),
		&appsv1.DaemonSet{},
	)
	return wr
}

func (s *stateDriver) getManifestObjects(ctx context.Context, cr *nvidiav1alpha1.NVIDIADriver, clusterPolicy *gpuv1.ClusterPolicy, clusterInfo clusterinfo.Interface) ([]*unstructured.Unstructured, error) {
	logger := log.FromContext(ctx)

	runtimeSpec, err := getRuntimeSpec(ctx, s.client, clusterInfo, &cr.Spec)
	if err != nil {
		return nil, fmt.Errorf("failed to construct cluster runtime spec: %w", err)
	}

	validatorSpec, err := getValidatorSpec(&clusterPolicy.Spec.Validator)
	if err != nil {
		return nil, fmt.Errorf("failed to construct validator spec: %v", err)
	}

	gdsSpec, err := getGDSSpec(cr.Spec.GPUDirectStorage)
	if err != nil {
		return nil, fmt.Errorf("failed to construct GDS spec: %v", err)
	}

	operatorSpec := &clusterPolicy.Spec.Operator
	gpuDirectRDMASpec := clusterPolicy.Spec.Driver.GPUDirectRDMA

	renderData := &driverRenderData{
		Operator:          operatorSpec,
		Validator:         validatorSpec,
		GDS:               gdsSpec,
		GPUDirectRDMA:     gpuDirectRDMASpec,
		Runtime:           runtimeSpec,
		AdditionalConfigs: &additionalConfigs{},
	}

	// Render kubernetes objects for each node pool.
	// We deploy one DaemonSet per node pool.
	var objs []*unstructured.Unstructured
	for _, nodePool := range runtimeSpec.NodePools {
		// Construct a unique driver spec per node pool. Each node pool
		// should have a unique nodeSelector and name.
		driverSpec, err := getDriverSpec(cr, nodePool)
		if err != nil {
			return nil, fmt.Errorf("failed to construct driver spec: %v", err)
		}
		renderData.Driver = driverSpec

		if cr.Spec.UsePrecompiledDrivers() {
			renderData.Precompiled = &precompiledSpec{
				KernelVersion:          nodePool.kernel,
				SanitizedKernelVersion: getSanitizedKernelVersion(nodePool.kernel),
			}
		}

		if !cr.Spec.UsePrecompiledDrivers() && runtimeSpec.OpenshiftDriverToolkitEnabled {
			renderData.Openshift = &openshiftSpec{
				RHCOSVersion: nodePool.rhcosVersion,
				ToolkitImage: runtimeSpec.OpenshiftDriverToolkitImages[nodePool.rhcosVersion],
			}
		}

		logger.Info("Rendering manifests for node pool", "NodePool", nodePool.name)
		manifestObjs, err := s.renderManifestObjects(ctx, renderData)
		if err != nil {
			logger.Error(err, "error rendering manifests for node pool", "NodePool", nodePool.name)
			return nil, err
		}
		objs = append(objs, manifestObjs...)
	}
	return objs, nil
}

func (s *stateDriver) renderManifestObjects(ctx context.Context, renderData *driverRenderData) ([]*unstructured.Unstructured, error) {
	logger := log.FromContext(ctx)

	logger.V(consts.LogLevelDebug).Info("Rendering objects", "data:", renderData)

	objs, err := s.renderer.RenderObjects(
		&render.TemplatingData{
			Data: renderData,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to render kubernetes manifests: %v", err)
	}
	logger.V(consts.LogLevelDebug).Info("Rendered", "objects:", objs)

	return objs, nil
}

// getDriverName returns a unique name for an NVIDIA driver instance in the format
// nvidia-<driverType>-driver-<crName>-<osVersion>
//
// In the manifest templates, a '-<kernelVersion>' or '-<rhcosVersion>' suffix may be
// appended if precompiled drivers are enabled or the OpenShift Driver Toolkit is used.
func getDriverName(cr *nvidiav1alpha1.NVIDIADriver, osVersion string) string {
	const (
		nameFormat = "nvidia-%s-driver-%s-%s"
		// https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#dns-subdomain-names
		// https://github.com/kubernetes/apimachinery/blob/v0.28.1/pkg/util/validation/validation.go#L209
		nameMaxLength = 253
	)

	name := fmt.Sprintf(nameFormat, cr.Spec.DriverType, cr.Name, osVersion)
	// truncate name if it exceeds the maximum length
	if len(name) > nameMaxLength {
		name = name[:nameMaxLength]
	}
	return name
}

func getDriverImagePath(spec *nvidiav1alpha1.NVIDIADriverSpec, nodePool nodePool) (string, error) {
	if spec.UsePrecompiledDrivers() {
		return spec.GetPrecompiledImagePath(nodePool.os, nodePool.kernel)
	}

	return spec.GetImagePath(nodePool.os)
}

func getDriverSpec(cr *nvidiav1alpha1.NVIDIADriver, nodePool nodePool) (*driverSpec, error) {
	if cr == nil {
		return nil, fmt.Errorf("no NVIDIADriver CR provided")
	}

	nvidiaDriverName := getDriverName(cr, nodePool.os)

	spec := &cr.Spec
	imagePath, err := getDriverImagePath(spec, nodePool)
	if err != nil {
		return nil, fmt.Errorf("failed to get driver image path: %v", err)
	}

	spec.NodeSelector = nodePool.nodeSelector

	managerImagePath, err := image.ImagePath(spec.Manager.Repository, spec.Manager.Image, spec.Manager.Version, "DRIVER_MANAGER_IMAGE")
	if err != nil {
		return nil, fmt.Errorf("failed to construct image path for driver manager: %w", err)
	}

	return &driverSpec{
		Spec:             spec,
		Name:             nvidiaDriverName,
		ImagePath:        imagePath,
		ManagerImagePath: managerImagePath,
	}, nil
}

func getValidatorSpec(spec *gpuv1.ValidatorSpec) (*validatorSpec, error) {
	if spec == nil {
		return nil, fmt.Errorf("no validator spec provided")
	}
	imagePath, err := image.ImagePath(spec.Repository, spec.Image, spec.Version, "VALIDATOR_IMAGE")
	if err != nil {
		return nil, fmt.Errorf("failed to construct image path for validator: %v", err)
	}

	return &validatorSpec{
		spec,
		imagePath,
	}, nil
}

func getGDSSpec(spec *gpuv1.GPUDirectStorageSpec) (*gdsDriverSpec, error) {
	if spec == nil || !spec.IsEnabled() {
		// note: GDS is optional in Clusterpolicy CRD
		return nil, nil
	}
	imagePath, err := image.ImagePath(spec.Repository, spec.Image, spec.Version, "GDS_IMAGE")
	if err != nil {
		return nil, fmt.Errorf("failed to construct image path for the GDS container: %v", err)
	}

	return &gdsDriverSpec{
		spec,
		imagePath,
	}, nil
}

func getRuntimeSpec(ctx context.Context, k8sClient client.Client, info clusterinfo.Interface, spec *nvidiav1alpha1.NVIDIADriverSpec) (*driverRuntimeSpec, error) {
	k8sVersion, err := info.GetKubernetesVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes version: %v", err)
	}
	openshiftVersion, err := info.GetOpenshiftVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get openshift version: %v", err)
	}
	openshift := (openshiftVersion != "")

	operatorNamespace := os.Getenv("OPERATOR_NAMESPACE")
	if operatorNamespace == "" {
		return nil, fmt.Errorf("OPERATOR_NAMESPACE environment variable not set")
	}

	nodePools, err := getNodePools(ctx, k8sClient, spec.NodeSelector, spec.UsePrecompiledDrivers(), openshift)
	if err != nil {
		return nil, fmt.Errorf("failed to get node pools: %v", err)
	}

	rs := &driverRuntimeSpec{
		Namespace:         operatorNamespace,
		KubernetesVersion: k8sVersion,
		OpenshiftVersion:  openshiftVersion,
		NodePools:         nodePools,
	}

	// Only get information needed for Openshift DriverToolkit if we are
	// running on an Openshift cluster and precompiled drivers are disabled.
	if openshiftVersion != "" && !spec.UsePrecompiledDrivers() {

		openshiftProxySpec, err := info.GetOpenshiftProxySpec()
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve proxy settings for openshift cluster: %w", err)
		}
		rs.OpenshiftProxySpec = openshiftProxySpec

		openshiftDTKMap := info.GetOpenshiftDriverToolkitImages()

		rs.OpenshiftDriverToolkitEnabled = len(openshiftDTKMap) > 0
		rs.OpenshiftDriverToolkitImages = openshiftDTKMap
	}

	return rs, nil
}

// getSanitizedKernelVersion returns kernelVersion with following changes
// 1. Remove arch suffix (as we use multi-arch images) and
// 2. ensure to meet k8s constraints for metadata.name, i.e it
// must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character
func getSanitizedKernelVersion(kernelVersion string) string {
	archRegex := regexp.MustCompile("x86_64|aarch64")
	// remove arch strings, "_" and any trailing "." from the kernel version
	sanitizedVersion := strings.TrimSuffix(strings.ReplaceAll(archRegex.ReplaceAllString(kernelVersion, ""), "_", "."), ".")
	return strings.ToLower(sanitizedVersion)
}
