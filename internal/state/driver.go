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
	"regexp"
	"sort"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/controllers/clusterinfo"
	driverconfig "github.com/NVIDIA/gpu-operator/internal/config"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/internal/image"
	"github.com/NVIDIA/gpu-operator/internal/render"
	"github.com/NVIDIA/gpu-operator/internal/utils"
)

const (
	nfdOSReleaseIDLabelKey = "feature.node.kubernetes.io/system-os_release.ID"
	nfdOSVersionIDLabelKey = "feature.node.kubernetes.io/system-os_release.VERSION_ID"

	// AppComponentLabelKey indicates the label key of the component
	AppComponentLabelKey = "app.kubernetes.io/component"
	// AppComponentLabelValue indicates the label values of the nvidia-gpu-driver component
	AppComponentLabelValue = "nvidia-driver"
)

type stateDriver struct {
	stateSkel
}

var _ State = (*stateDriver)(nil)

type driverRuntimeSpec struct {
	Namespace                     string
	OpenshiftVersion              string
	OpenshiftDriverToolkitEnabled bool
	OpenshiftProxySpec            *configv1.ProxySpec
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
	GDS               *gdsDriverSpec
	GPUDirectRDMA     *nvidiav1alpha1.GPUDirectRDMASpec
	GDRCopy           *gdrcopyDriverSpec
	Runtime           *driverRuntimeSpec
	Openshift         *openshiftSpec
	Precompiled       *precompiledSpec
	AdditionalConfigs *additionalConfigs
	HostRoot          string
}

// ConfigDigest computes a hash of all driver-install-relevant fields.
// Called automatically by the Go template via {{ .ConfigDigest }}.
func (d *driverRenderData) ConfigDigest() string {
	return utils.GetObjectHashIgnoreEmptyKeys(buildDriverInstallConfig(d))
}

func NewStateDriver(
	k8sClient client.Client,
	namespace string,
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
			namespace:   namespace,
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

	objs, err := s.getManifestObjects(ctx, cr, infoCatalog)
	if err != nil {
		return SyncStateNotReady, fmt.Errorf("failed to create k8s objects from manifests: %w", err)
	}

	err = s.cleanupStaleDriverDaemonsets(ctx, cr, objs)
	if err != nil {
		return SyncStateNotReady, fmt.Errorf("failed to cleanup stale driver DaemonSets: %w", err)
	}

	// Create objects if they don't exist, Update objects if they do exist
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
	nvDriverPredicate := predicate.NewTypedPredicateFuncs(func(ds *appsv1.DaemonSet) bool {
		ls := metav1.LabelSelector{MatchLabels: map[string]string{AppComponentLabelKey: AppComponentLabelValue}}
		selector, _ := metav1.LabelSelectorAsSelector(&ls)
		return selector.Matches(labels.Set(ds.GetLabels()))
	})
	wr["DaemonSet"] = source.Kind(
		mgr.GetCache(),
		&appsv1.DaemonSet{},
		handler.TypedEnqueueRequestForOwner[*appsv1.DaemonSet](mgr.GetScheme(), mgr.GetRESTMapper(),
			&nvidiav1alpha1.NVIDIADriver{}, handler.OnlyControllerOwner()),
		nvDriverPredicate,
	)
	return wr
}

func (s *stateDriver) cleanupStaleDriverDaemonsets(ctx context.Context, cr *nvidiav1alpha1.NVIDIADriver, desiredObjs []*unstructured.Unstructured) error {
	logger := log.FromContext(ctx)
	logger.V(consts.LogLevelInfo).Info("Cleaning up stale driver DaemonSets")

	// Build a set of desired DaemonSet names from the manifest objects
	desiredDaemonSetNames := make(map[string]bool)
	for _, obj := range desiredObjs {
		if obj.GetKind() == "DaemonSet" {
			desiredDaemonSetNames[obj.GetName()] = true
		}
	}

	// List all DaemonSets owned by the CR instance
	list := &appsv1.DaemonSetList{}
	err := s.client.List(ctx, list, client.MatchingFields{consts.NVIDIADriverControllerIndexKey: cr.Name})
	if err != nil {
		return fmt.Errorf("failed to list all NVIDIA driver DaemonSets owned by NVIDIADriver instance: %w", err)
	}

	for _, ds := range list.Items {
		ds := ds
		// Delete DaemonSets that are not in the desired list. This handles the case where
		// the CR's nodeSelector changes and certain node pools no longer match.
		if _, exists := desiredDaemonSetNames[ds.Name]; !exists {
			logger.V(consts.LogLevelInfo).Info("Deleting DaemonSet no longer managed by this CR", "Name", ds.Name)
			err = s.client.Delete(ctx, &ds)
			if err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("error deleting DaemonSet %q: %w", ds.Name, err)
			}
			continue
		}

		// We consider a DaemonSet to be stale when all three conditions are true:
		//
		// 1. The desired number of pods reported by the DaemonSet controller is 0
		// 2. The number of mis-scheduled pods is 0. As per the Kubernetes docs, a DaemonSet pod is mis-scheduled when an
		//    already scheduled pod no longer satisfies node affinity constraints or has untolerated taints, e.g.
		//    "node.kubernetes.io/unreachable:NoSchedule"
		// 3. The DaemonSet's nodeSelector matches 0 nodes.
		//
		// #3 was added in response to https://github.com/NVIDIA/gpu-operator/issues/1368 where the NVIDIADriver controller
		// entered an endless loop of creating and deleting a DaemonSet. The DaemonSet's nodeSelector matched one or more nodes,
		// but DesiredNumberScheduled and NumberMisscheduled were both 0 because the DaemonSet did not tolerate a taint on all
		// the nodes.
		if ds.Status.DesiredNumberScheduled == 0 && ds.Status.NumberMisscheduled == 0 {
			nodeList := &corev1.NodeList{}
			err := s.client.List(ctx, nodeList, client.MatchingLabels(ds.Spec.Template.Spec.NodeSelector))
			if err != nil {
				return fmt.Errorf("failed to list nodes: %w", err)
			}
			if len(nodeList.Items) > 0 {
				continue
			}
			logger.V(consts.LogLevelInfo).Info("Deleting inactive driver DaemonSet", "Name", ds.Name)
			err = s.client.Delete(ctx, &ds)
			if err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("error deleting DaemonSet %q: %w", ds.Name, err)
			}
			continue
		}
		// TODO: cleanup precompiled / non-precompiled DaemonSets if spec.usePrecompiled is toggled.
		// TODO: cleanup DaemonSets of a particular type if spec.driverType is toggled.
	}
	return nil
}

func (s *stateDriver) getManifestObjects(ctx context.Context, cr *nvidiav1alpha1.NVIDIADriver, infoCatalog InfoCatalog) ([]*unstructured.Unstructured, error) {
	logger := log.FromContext(ctx)

	info := infoCatalog.Get(InfoTypeClusterPolicyCR)
	if info == nil {
		return nil, fmt.Errorf("failed to get ClusterPolicy CR from info catalog")
	}
	clusterPolicy := info.(gpuv1.ClusterPolicy)

	info = infoCatalog.Get(InfoTypeClusterInfo)
	if info == nil {
		return nil, fmt.Errorf("failed to get cluster info from info catalog")
	}
	clusterInfo := info.(clusterinfo.Interface)

	runtimeSpec, err := getRuntimeSpec(s.namespace, clusterInfo, &cr.Spec)
	if err != nil {
		return nil, fmt.Errorf("failed to construct cluster runtime spec: %w", err)
	}

	isOpenshift := runtimeSpec.OpenshiftVersion != ""
	nodePools, err := getNodePools(ctx, s.client, cr.Spec.NodeSelector, cr.Spec.UsePrecompiledDrivers(), isOpenshift)
	if err != nil {
		return nil, fmt.Errorf("failed to get node pools: %w", err)
	}

	gpuDirectRDMASpec := cr.Spec.GPUDirectRDMA

	renderData := &driverRenderData{
		GPUDirectRDMA: gpuDirectRDMASpec,
		Runtime:       runtimeSpec,
		HostRoot:      clusterPolicy.Spec.HostPaths.RootFS,
	}

	if len(nodePools) == 0 {
		logger.Info("No nodes matching the given node selector", "CR", cr.Name)
		return []*unstructured.Unstructured{}, nil
	}

	openshiftDTKMap := clusterInfo.GetOpenshiftDriverToolkitImages()

	// Render kubernetes objects for each node pool.
	// We deploy one DaemonSet per node pool.
	var objs []*unstructured.Unstructured
	for _, nodePool := range nodePools {
		// Construct a unique driver spec per node pool. Each node pool
		// should have a unique nodeSelector and name.
		driverSpec, err := getDriverSpec(cr, nodePool)
		if err != nil {
			return nil, fmt.Errorf("failed to construct driver spec: %w", err)
		}
		renderData.Driver = driverSpec

		if cr.Spec.UsePrecompiledDrivers() {
			renderData.Precompiled = &precompiledSpec{
				KernelVersion:          nodePool.kernel,
				SanitizedKernelVersion: getSanitizedKernelVersion(nodePool.kernel),
			}
		}

		gdsSpec, err := getGDSSpec(&cr.Spec, nodePool)
		if err != nil {
			return nil, fmt.Errorf("failed to construct GDS spec: %w", err)
		}
		renderData.GDS = gdsSpec

		gdrcopySpec, err := getGDRCopySpec(&cr.Spec, nodePool)
		if err != nil {
			return nil, fmt.Errorf("failed to construct GDRCopy spec: %w", err)
		}
		renderData.GDRCopy = gdrcopySpec

		if !cr.Spec.UsePrecompiledDrivers() && runtimeSpec.OpenshiftDriverToolkitEnabled {
			renderData.Openshift = &openshiftSpec{
				RHCOSVersion: nodePool.rhcosVersion,
				ToolkitImage: openshiftDTKMap[nodePool.rhcosVersion],
			}
		}

		renderData.AdditionalConfigs, err = s.getDriverAdditionalConfigs(ctx, cr, clusterInfo, nodePool)
		if err != nil {
			logger.Error(err, "error rendering addition driver volume", "NodePool", nodePool.name)
		}

		logger.Info("Rendering manifests for node pool", "NodePool", nodePool.name)
		manifestObjs, err := s.renderManifestObjects(ctx, renderData)
		if err != nil {
			logger.Error(err, "error rendering manifests for node pool", "NodePool", nodePool.name)
			return nil, err
		}
		manifestObjs, err = s.handleDefaultImagesInObjects(ctx, manifestObjs, cr, *renderData)
		if err != nil {
			logger.Error(err, "error handling default images in manifests", "NodePool", nodePool.name)
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
			Data: &renderData,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to render kubernetes manifests: %w", err)
	}

	logger.V(consts.LogLevelDebug).Info("Rendered", "objects:", objs)
	return objs, nil
}

func (s *stateDriver) handleDefaultImagesInObjects(
	ctx context.Context,
	desiredObjs []*unstructured.Unstructured,
	cr *nvidiav1alpha1.NVIDIADriver,
	renderData driverRenderData) ([]*unstructured.Unstructured, error) {
	logger := log.FromContext(ctx)

	// If 'image' field is not set in spec, then the driver image path
	// was determined via the DRIVER_MANAGER_IMAGE env var
	managerImageEnvVarUsed := (cr.Spec.Manager.Image == "")
	if !managerImageEnvVarUsed {
		return desiredObjs, nil
	}

	// If the default image is used for driver-manager, make sure this image
	// is only upgraded iff the driver spec has been updated. This avoids
	// triggering undesired driver upgrades when the operator itself is upgraded
	// but the NVIDIADriver spec is unmodified.
	logger.V(consts.LogLevelDebug).Info("Default env var is being used for k8s-driver-manager image, checking for an updated default image")

	desiredDs, err := getDaemonsetFromObjects(desiredObjs)
	if err != nil {
		return nil, fmt.Errorf("error getting DaemonSet from unstructured objects: %w", err)
	}

	currentDs := &appsv1.DaemonSet{}
	err = s.client.Get(ctx, types.NamespacedName{Namespace: desiredDs.Namespace, Name: desiredDs.Name}, currentDs)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return desiredObjs, nil
		}
		return nil, fmt.Errorf("failed to get current driver DaemonSet object: %w", err)
	}

	currentManagerImage := ""
	for _, container := range currentDs.Spec.Template.Spec.InitContainers {
		if container.Name == "k8s-driver-manager" {
			currentManagerImage = container.Image
			break
		}
	}

	logger.V(consts.LogLevelDebug).Info("Current k8s-driver-manager image", "image", currentManagerImage)
	if currentManagerImage == renderData.Driver.ManagerImagePath {
		logger.V(consts.LogLevelDebug).Info("Default k8s-driver-manager image has not been updated")
		return desiredObjs, nil
	}

	// Render manifests again but with the current k8s-driver-manager being used
	renderData.Driver.ManagerImagePath = currentManagerImage
	desiredObjsWithCurrentImages, err := s.renderManifestObjects(ctx, &renderData)
	if err != nil {
		return nil, fmt.Errorf("failed to render kubernetes manifests: %w", err)
	}

	obj, err := getObjectOfKind(desiredObjsWithCurrentImages, "DaemonSet")
	if err != nil {
		return nil, fmt.Errorf("failed to get DaemonSet object: %w", err)
	}

	// Apply common modifications to the DaemonSet object
	if err := controllerutil.SetControllerReference(cr, obj, s.scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference for object: %w", err)
	}
	s.addStateSpecificLabels(obj)

	// Compute the hash and compare with the hash of the current DaemonSet deployed
	newHash := utils.GetObjectHash(obj)
	currentHash := currentDs.GetAnnotations()[consts.NvidiaAnnotationHashKey]
	logger.V(consts.LogLevelDebug).Info("Calculating obj hash with old k8s-driver-manager image", "currentHash", currentHash, "newHash", newHash)
	if newHash == currentHash {
		// Hash is same when we use the same driver manager image.
		// Thus, the driver spec has not changed. Do not update
		// the driver-manager image.
		logger.V(consts.LogLevelDebug).Info("k8s-driver-manager image can be updated, but driver spec is unchanged. Avoiding update.")
		return desiredObjsWithCurrentImages, nil
	}

	logger.V(consts.LogLevelDebug).Info("Driver spec has changed, updating k8s-driver-manager image as well")
	return desiredObjs, nil
}

func getDaemonsetFromObjects(objs []*unstructured.Unstructured) (*appsv1.DaemonSet, error) {
	obj, err := getObjectOfKind(objs, "DaemonSet")
	if err != nil {
		return nil, err
	}

	ds := &appsv1.DaemonSet{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, ds)
	if err != nil {
		return nil, fmt.Errorf("error converting unstructured object to DaemonSet: %w", err)
	}
	return ds, nil
}

func getObjectOfKind(objs []*unstructured.Unstructured, kind string) (*unstructured.Unstructured, error) {
	for _, obj := range objs {
		if obj.GetKind() == kind {
			return obj, nil
		}
	}
	return nil, fmt.Errorf("did not find object of kind '%s' in Object list", kind)
}

// getDriverName returns a unique name for an NVIDIA driver instance in the format nvidia-<driverType>-driver-<crName>-<osVersion>
func getDriverName(cr *nvidiav1alpha1.NVIDIADriver, osVersion string) string {
	const (
		// https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#dns-subdomain-names
		// https://github.com/kubernetes/apimachinery/blob/v0.28.1/pkg/util/validation/validation.go#L209
		nameMaxLength = 253
	)

	name := fmt.Sprintf("nvidia-%s-driver-%s-%s", cr.Spec.DriverType, cr.Name, osVersion)

	if cr.Spec.DriverType == nvidiav1alpha1.VGPUHostManager {
		name = fmt.Sprintf("nvidia-vgpu-manager-%s-%s", cr.Name, osVersion)
	}

	// truncate name if it exceeds the maximum length
	if len(name) > nameMaxLength {
		name = name[:nameMaxLength]
	}
	return name
}

// getDriverAppName returns a unique name for an NVIDIA driver instance in the format nvidia-<driverType>-driver-<osVersion>-<hash>
// The hash string <string> is calculated from the NVIDIADriver CR UID.
//
// The '-<kernelVersion>' or '-<rhcosVersion>' suffix may also be used to calculate the hash if precompiled drivers
// are enabled or the OpenShift Driver Toolkit is used.
func getDriverAppName(cr *nvidiav1alpha1.NVIDIADriver, pool nodePool) string {
	const (
		appNamePrefixFormat = "nvidia-%s-driver-%s"
		// https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#dns-label-names
		// https://github.com/kubernetes/apimachinery/blob/v0.28.1/pkg/util/validation/validation.go#L182
		appNameMaxLength = 63
	)

	var hashBuilder strings.Builder

	appNamePrefix := fmt.Sprintf(appNamePrefixFormat, cr.Spec.DriverType, pool.osTag)
	uid := string(cr.UID)

	hashBuilder.WriteString(uid)
	if pool.kernel != "" {
		hashBuilder.WriteString("-" + pool.kernel)
	} else if pool.rhcosVersion != "" {
		hashBuilder.WriteString("-" + pool.rhcosVersion)
	}

	hash := utils.GetStringHash(hashBuilder.String())
	appName := fmt.Sprintf("%s-%s", appNamePrefix, hash)

	// truncate the prefix if the app name exceeds the maximum length
	if len(appName) > appNameMaxLength {
		appNamePrefixMaxLength := appNameMaxLength - (len(hash) + 1)
		appName = fmt.Sprintf("%s-%s", appNamePrefix[:appNamePrefixMaxLength], hash)
	}
	return appName
}

func getDefaultStartupProbe(spec *nvidiav1alpha1.NVIDIADriverSpec) *nvidiav1alpha1.ContainerProbeSpec {
	initialDelaySeconds := int32(60)
	if spec.UsePrecompiledDrivers() {
		initialDelaySeconds = 5
	}

	return &nvidiav1alpha1.ContainerProbeSpec{
		InitialDelaySeconds: initialDelaySeconds,
		TimeoutSeconds:      60,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		FailureThreshold:    120,
	}
}

func getDriverImagePath(spec *nvidiav1alpha1.NVIDIADriverSpec, nodePool nodePool) (string, error) {
	os := nodePool.osTag

	if spec.UsePrecompiledDrivers() {
		return spec.GetPrecompiledImagePath(os, nodePool.kernel)
	}

	return spec.GetImagePath(os)
}

func sanitizeDriverLabels(labels map[string]string) map[string]string {
	sanitizedLabels := make(map[string]string)
	for k, v := range labels {
		// if the user specifies an override of the "app" or any "app.kubernetes.io/" keys, we skip it.
		// DaemonSet pod selectors are immutable, so we still want the pods to be selectable as before and working
		// with the existing daemon set selectors.
		if k == "app" || strings.HasPrefix(k, "app.kubernetes.io/") {
			continue
		}
		sanitizedLabels[k] = v
	}
	return sanitizedLabels
}

func getDriverSpec(cr *nvidiav1alpha1.NVIDIADriver, nodePool nodePool) (*driverSpec, error) {
	if cr == nil {
		return nil, fmt.Errorf("no NVIDIADriver CR provided")
	}

	nvidiaDriverName := getDriverName(cr, nodePool.osTag)
	nvidiaDriverAppName := getDriverAppName(cr, nodePool)

	spec := cr.Spec.DeepCopy()
	imagePath, err := getDriverImagePath(spec, nodePool)
	if err != nil {
		return nil, fmt.Errorf("failed to get driver image path: %v", err)
	}

	spec.NodeSelector = nodePool.nodeSelector

	managerImagePath, err := image.ImagePath(spec.Manager.Repository, spec.Manager.Image, spec.Manager.Version, "DRIVER_MANAGER_IMAGE")
	if err != nil {
		return nil, fmt.Errorf("failed to construct image path for driver manager: %w", err)
	}

	if spec.StartupProbe == nil {
		spec.StartupProbe = getDefaultStartupProbe(spec)
	}

	spec.Labels = sanitizeDriverLabels(spec.Labels)

	return &driverSpec{
		Spec:             spec,
		AppName:          nvidiaDriverAppName,
		Name:             nvidiaDriverName,
		ImagePath:        imagePath,
		ManagerImagePath: managerImagePath,
		OSVersion:        nodePool.osTag,
	}, nil
}

func getGDSSpec(spec *nvidiav1alpha1.NVIDIADriverSpec, pool nodePool) (*gdsDriverSpec, error) {
	if spec == nil || !spec.IsGDSEnabled() {
		// note: GDS is optional in the NvidiaDriver CRD
		return nil, nil
	}
	gdsSpec := spec.GPUDirectStorage
	imagePath, err := gdsSpec.GetImagePath(pool.osTag)
	if err != nil {
		return nil, err
	}

	return &gdsDriverSpec{
		gdsSpec,
		imagePath,
	}, nil
}

func getGDRCopySpec(spec *nvidiav1alpha1.NVIDIADriverSpec, pool nodePool) (*gdrcopyDriverSpec, error) {
	if spec == nil || !spec.IsGDRCopyEnabled() {
		// note: GDRCopy is optional in the NvidiaDriver CRD
		return nil, nil
	}
	gdrcopySpec := spec.GDRCopy
	imagePath, err := gdrcopySpec.GetImagePath(pool.osTag)
	if err != nil {
		return nil, err
	}

	return &gdrcopyDriverSpec{
		gdrcopySpec,
		imagePath,
	}, nil
}

func getRuntimeSpec(namespace string, info clusterinfo.Interface, spec *nvidiav1alpha1.NVIDIADriverSpec) (*driverRuntimeSpec, error) {
	openshiftVersion, err := info.GetOpenshiftVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get openshift version: %v", err)
	}

	rs := &driverRuntimeSpec{
		Namespace:        namespace,
		OpenshiftVersion: openshiftVersion,
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
	}

	return rs, nil
}

// getSanitizedKernelVersion returns kernelVersion with following changes
// 1. Remove arch suffix (as we use multi-arch images) and
// 2. ensure to meet k8s constraints for metadata.name, i.e it
// must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character
func getSanitizedKernelVersion(kernelVersion string) string {
	archRegex := regexp.MustCompile("x86_64(?:_64k)?|aarch64(?:_64k)?")
	// remove arch strings, "_" and any trailing "." from the kernel version
	sanitizedVersion := strings.TrimSuffix(strings.ReplaceAll(archRegex.ReplaceAllString(kernelVersion, ""), "_", "."), ".")
	return strings.ToLower(sanitizedVersion)
}

func (s *stateDriver) createConfigMapVolumeMounts(ctx context.Context, namespace string, configMapName string,
	destinationDir string) ([]corev1.VolumeMount, []corev1.KeyToPath, error) {
	// get the ConfigMap
	cm := &corev1.ConfigMap{}
	opts := client.ObjectKey{Namespace: namespace, Name: configMapName}
	err := s.client.Get(ctx, opts, cm)
	if err != nil {
		return nil, nil, fmt.Errorf("ERROR: could not get ConfigMap %s from client: %v", configMapName, err)
	}

	// create one volume mount per file in the ConfigMap and use subPath
	var filenames []string
	for filename := range cm.Data {
		filenames = append(filenames, filename)
	}
	// sort so volume mounts are added to spec in deterministic order
	sort.Strings(filenames)
	var itemsToInclude []corev1.KeyToPath
	var volumeMounts []corev1.VolumeMount
	for _, filename := range filenames {
		volumeMounts = append(volumeMounts,
			corev1.VolumeMount{Name: configMapName, ReadOnly: true, MountPath: filepath.Join(destinationDir, filename),
				SubPath: filename})
		itemsToInclude = append(itemsToInclude, corev1.KeyToPath{
			Key:  filename,
			Path: filename,
		})
	}
	return volumeMounts, itemsToInclude, nil
}

func createConfigMapVolume(configMapName string, itemsToInclude []corev1.KeyToPath) corev1.Volume {
	volumeSource := corev1.VolumeSource{
		ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: configMapName,
			},
			Items: itemsToInclude,
		},
	}
	return corev1.Volume{Name: configMapName, VolumeSource: volumeSource}
}

// toConfigEnvVars converts API EnvVar slices to the config package's EnvVar type.
func toConfigEnvVars(envs []nvidiav1alpha1.EnvVar) []driverconfig.EnvVar {
	result := make([]driverconfig.EnvVar, len(envs))
	for i, e := range envs {
		result[i] = driverconfig.EnvVar{Name: e.Name, Value: e.Value}
	}
	return result
}

// buildDriverInstallConfig maps render data fields to a DriverInstallState
// for digest computation in the NVIDIADriver CRD path.
func buildDriverInstallConfig(data *driverRenderData) *driverconfig.DriverInstallState {
	config := &driverconfig.DriverInstallState{}

	if data.Driver != nil {
		config.DriverImage = data.Driver.ImagePath
		config.DriverManagerImage = data.Driver.ManagerImagePath
		config.DriverType = string(data.Driver.Spec.DriverType)
		config.KernelModuleType = data.Driver.Spec.KernelModuleType
		config.DriverArgs = data.Driver.Spec.Args
		config.SecretEnvSource = data.Driver.Spec.SecretEnv

		config.DriverEnv = toConfigEnvVars(data.Driver.Spec.Env)
		config.ManagerEnv = toConfigEnvVars(data.Driver.Spec.Manager.Env)

		if data.Driver.Spec.LicensingConfig != nil && data.Driver.Spec.LicensingConfig.SecretName != "" {
			config.LicensingConfigName = data.Driver.Spec.LicensingConfig.SecretName
		}
		if data.Driver.Spec.VirtualTopologyConfig != nil {
			config.VirtualTopologyConfig = data.Driver.Spec.VirtualTopologyConfig.Name
		}
		if data.Driver.Spec.KernelModuleConfig != nil {
			config.KernelModuleConfig = data.Driver.Spec.KernelModuleConfig.Name
		}
		if data.Driver.Spec.RepoConfig != nil {
			config.RepoConfig = data.Driver.Spec.RepoConfig.Name
		}
		if data.Driver.Spec.CertConfig != nil {
			config.CertConfig = data.Driver.Spec.CertConfig.Name
		}
	}

	if data.GPUDirectRDMA != nil && data.GPUDirectRDMA.Enabled != nil && *data.GPUDirectRDMA.Enabled {
		config.GPUDirectRDMAEnabled = true
		if data.Driver != nil {
			config.PeermemImage = data.Driver.ImagePath
		}
		if data.GPUDirectRDMA.UseHostMOFED != nil {
			config.UseHostMOFED = *data.GPUDirectRDMA.UseHostMOFED
		}
	}

	if data.GDS != nil {
		config.GDSImage = data.GDS.ImagePath
		if data.GDS.Spec != nil && data.GDS.Spec.Enabled != nil {
			config.GDSEnabled = *data.GDS.Spec.Enabled
		}
		if data.GDS.Spec != nil {
			config.GDSEnv = toConfigEnvVars(data.GDS.Spec.Env)
		}
	}

	if data.GDRCopy != nil {
		config.GDRCopyImage = data.GDRCopy.ImagePath
		if data.GDRCopy.Spec != nil && data.GDRCopy.Spec.Enabled != nil {
			config.GDRCopyEnabled = *data.GDRCopy.Spec.Enabled
		}
		if data.GDRCopy.Spec != nil {
			config.GDRCopyEnv = toConfigEnvVars(data.GDRCopy.Spec.Env)
		}
	}

	if data.Runtime != nil {
		config.OpenshiftVersion = data.Runtime.OpenshiftVersion
		config.DTKEnabled = data.Runtime.OpenshiftDriverToolkitEnabled
		if data.Runtime.OpenshiftProxySpec != nil {
			config.HTTPProxy = data.Runtime.OpenshiftProxySpec.HTTPProxy
			config.HTTPSProxy = data.Runtime.OpenshiftProxySpec.HTTPSProxy
			config.NoProxy = data.Runtime.OpenshiftProxySpec.NoProxy
			if data.Runtime.OpenshiftProxySpec.TrustedCA.Name != "" {
				config.TrustedCAConfigMapName = data.Runtime.OpenshiftProxySpec.TrustedCA.Name
			}
		}
	}

	if data.Openshift != nil {
		config.DTKImage = data.Openshift.ToolkitImage
		config.RHCOSVersion = data.Openshift.RHCOSVersion
	}

	if data.Precompiled != nil {
		config.UsePrecompiled = true
		config.KernelVersion = data.Precompiled.KernelVersion
	}

	if data.AdditionalConfigs != nil {
		config.AdditionalVolumeMounts = driverconfig.ExtractVolumeMounts(data.AdditionalConfigs.VolumeMounts)
		config.AdditionalVolumes = driverconfig.ExtractVolumes(data.AdditionalConfigs.Volumes)
	}

	config.HostRoot = data.HostRoot

	// Sort env var slices for deterministic hashing (copies to avoid mutating spec data).
	config.DriverEnv = driverconfig.SortEnvVars(config.DriverEnv)
	config.ManagerEnv = driverconfig.SortEnvVars(config.ManagerEnv)
	config.GDSEnv = driverconfig.SortEnvVars(config.GDSEnv)
	config.GDRCopyEnv = driverconfig.SortEnvVars(config.GDRCopyEnv)

	return config
}
