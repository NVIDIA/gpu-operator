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
	"maps"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/controllers/clusterinfo"
	"github.com/NVIDIA/gpu-operator/internal/consts"
	"github.com/NVIDIA/gpu-operator/internal/image"
	"github.com/NVIDIA/gpu-operator/internal/render"
	"github.com/NVIDIA/gpu-operator/internal/utils"
)

const (
	// draDriverImageEnvName is the fallback env var for the DRA driver image when the
	// CR does not specify repository/image/version.
	draDriverImageEnvName = "DRA_DRIVER_IMAGE"
	// draValidatorImageEnvName is the env var for the gpu-operator image that ships the
	// dra-driver-validator binary used by the kubelet-plugin init container.
	draValidatorImageEnvName = "VALIDATOR_IMAGE"
)

type stateDRADriver struct {
	stateSkel
}

var _ State = (*stateDRADriver)(nil)

func NewStateDRADriver(
	k8sClient client.Client,
	namespace string,
	scheme *runtime.Scheme,
	manifestDir string) (State, error) {

	files, err := utils.GetFilesWithSuffix(manifestDir, render.ManifestFileSuffix...)
	if err != nil {
		return nil, fmt.Errorf("failed to get files from manifest directory: %v", err)
	}

	renderer := render.NewRenderer(files)
	state := &stateDRADriver{
		stateSkel: stateSkel{
			name:        "state-dra-driver",
			description: "NVIDIA DRA driver deployed in the cluster",
			client:      k8sClient,
			namespace:   namespace,
			scheme:      scheme,
			renderer:    renderer,
		},
	}
	return state, nil
}

func (s *stateDRADriver) Sync(ctx context.Context, customResource interface{}, infoCatalog InfoCatalog) (SyncState, error) {
	cr, ok := customResource.(*nvidiav1alpha1.GPUClusterConfig)
	if !ok {
		return SyncStateError, fmt.Errorf("GPUClusterConfig CR not provided as input to Sync()")
	}

	objs, err := s.getManifestObjects(ctx, cr, infoCatalog)
	if err != nil {
		return SyncStateNotReady, fmt.Errorf("failed to create k8s objects from manifests: %w", err)
	}

	if len(objs) == 0 {
		// No DRA capability is enabled; nothing to render.
		return SyncStateIgnore, nil
	}

	// Create objects if they don't exist, update objects if they do exist. Owner
	// references make every object (including the cluster-scoped DeviceClasses and
	// ClusterRoles) garbage-collected when the GPUClusterConfig CR is deleted.
	err = s.createOrUpdateObjs(ctx, func(obj *unstructured.Unstructured) error {
		if err := controllerutil.SetControllerReference(cr, obj, s.scheme); err != nil {
			return fmt.Errorf("failed to set controller reference for object: %w", err)
		}
		return nil
	}, objs)
	if err != nil {
		return SyncStateNotReady, fmt.Errorf("failed to create/update objects: %w", err)
	}

	syncState, err := s.getSyncState(ctx, objs)
	if err != nil {
		return SyncStateNotReady, fmt.Errorf("failed to get sync state: %w", err)
	}
	return syncState, nil
}

func (s *stateDRADriver) GetWatchSources(mgr ctrlManager) map[string]SyncingSource {
	wr := make(map[string]SyncingSource)
	wr["DaemonSet"] = source.Kind(
		mgr.GetCache(),
		&appsv1.DaemonSet{},
		handler.TypedEnqueueRequestForOwner[*appsv1.DaemonSet](mgr.GetScheme(), mgr.GetRESTMapper(),
			&nvidiav1alpha1.GPUClusterConfig{}, handler.OnlyControllerOwner()),
	)
	return wr
}

func (s *stateDRADriver) getManifestObjects(ctx context.Context, cr *nvidiav1alpha1.GPUClusterConfig, infoCatalog InfoCatalog) ([]*unstructured.Unstructured, error) {
	logger := log.FromContext(ctx)

	// No DRA capability enabled: nothing to render.
	if !cr.Spec.DRADriver.IsGPUsEnabled() && !cr.Spec.DRADriver.IsComputeDomainsEnabled() {
		logger.V(consts.LogLevelInfo).Info("No DRA driver capability is enabled, skipping render")
		return []*unstructured.Unstructured{}, nil
	}

	info := infoCatalog.Get(InfoTypeClusterInfo)
	if info == nil {
		return nil, fmt.Errorf("failed to get cluster info from info catalog")
	}
	clusterInfo := info.(clusterinfo.Interface)

	gvr, draSupported, err := clusterInfo.GetDRAResourceGVR()
	if err != nil {
		return nil, fmt.Errorf("failed to determine DRA support: %w", err)
	}
	if !draSupported {
		return nil, fmt.Errorf("the resource.k8s.io DeviceClass API is not served by the cluster; " +
			"ensure Dynamic Resource Allocation is enabled on the API server and kubelet")
	}

	draDriverSpec, err := getDRADriverSpec(&cr.Spec.DRADriver)
	if err != nil {
		return nil, fmt.Errorf("failed to construct DRA driver spec: %w", err)
	}

	hostPaths := cr.Spec.HostPaths
	daemonsets := cr.Spec.Daemonsets
	priorityClassName, nodeSelector, tolerations, affinity := mergeKubeletPluginScheduling(&cr.Spec.DRADriver, &daemonsets)
	renderData := &draDriverRenderData{
		DRADriver:                      draDriverSpec,
		HostPaths:                      &hostPaths,
		Daemonsets:                     &daemonsets,
		Namespace:                      s.namespace,
		DeviceClassAPIVersion:          gvr.Group + "/" + gvr.Version,
		FeatureGates:                   renderDRAFeatureGates(cr.Spec.DRADriver.FeatureGates),
		KubeletPluginPriorityClassName: priorityClassName,
		KubeletPluginNodeSelector:      nodeSelector,
		KubeletPluginTolerations:       tolerations,
		KubeletPluginAffinity:          affinity,
	}

	objs, err := s.renderManifestObjects(ctx, renderData)
	if err != nil {
		return nil, fmt.Errorf("failed to render manifests: %w", err)
	}
	return objs, nil
}

func (s *stateDRADriver) renderManifestObjects(ctx context.Context, renderData *draDriverRenderData) ([]*unstructured.Unstructured, error) {
	logger := log.FromContext(ctx)
	logger.V(consts.LogLevelDebug).Info("Rendering DRA driver objects", "data", renderData)

	objs, err := s.renderer.RenderObjects(
		&render.TemplatingData{
			Data: renderData,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to render kubernetes manifests: %w", err)
	}
	return objs, nil
}

// getDRADriverSpec builds the render-time DRA driver spec, resolving the DRA driver
// image (from the CR, falling back to DRA_DRIVER_IMAGE) and the init-container image
// (the gpu-operator image carrying dra-driver-validator, from VALIDATOR_IMAGE).
func getDRADriverSpec(spec *nvidiav1alpha1.DRADriverSpec) (*draDriverSpec, error) {
	imagePath, err := image.ImagePath(spec.Repository, spec.Image, spec.Version, draDriverImageEnvName)
	if err != nil {
		return nil, fmt.Errorf("failed to construct DRA driver image path: %w", err)
	}

	initImagePath, err := image.ImagePath("", "", "", draValidatorImageEnvName)
	if err != nil {
		return nil, fmt.Errorf("failed to construct DRA driver validator image path: %w", err)
	}

	return &draDriverSpec{
		Spec:          spec,
		ImagePath:     imagePath,
		InitImagePath: initImagePath,
	}, nil
}

// mergeKubeletPluginScheduling computes the pod-level scheduling for the single
// kubelet-plugin DaemonSet, which hosts both the gpus and computeDomains containers.
// Tolerations are the union of the daemonsets defaults and each enabled capability's
// kubeletPlugin tolerations; priorityClassName and affinity use the precedence
// computeDomains > gpus > daemonsets default (a disabled capability does not
// contribute). A nil affinity lets the template fall back to the default GPU-present
// node affinity.
func mergeKubeletPluginScheduling(spec *nvidiav1alpha1.DRADriverSpec, daemonsets *nvidiav1.DaemonsetsSpec) (string, map[string]string, []corev1.Toleration, *corev1.Affinity) {
	gpus := spec.GPUs.KubeletPlugin
	cd := spec.ComputeDomains.KubeletPlugin
	gpusEnabled := spec.IsGPUsEnabled()
	cdEnabled := spec.IsComputeDomainsEnabled()

	priorityClassName := "system-node-critical"
	if daemonsets.PriorityClassName != "" {
		priorityClassName = daemonsets.PriorityClassName
	}
	if gpusEnabled && gpus.PriorityClassName != "" {
		priorityClassName = gpus.PriorityClassName
	}
	if cdEnabled && cd.PriorityClassName != "" {
		priorityClassName = cd.PriorityClassName
	}

	tolerations := append([]corev1.Toleration{}, daemonsets.Tolerations...)
	if gpusEnabled {
		tolerations = append(tolerations, gpus.Tolerations...)
	}
	if cdEnabled {
		tolerations = append(tolerations, cd.Tolerations...)
	}

	nodeSelector := map[string]string{}
	if gpusEnabled {
		maps.Copy(nodeSelector, gpus.NodeSelector)
	}
	if cdEnabled {
		maps.Copy(nodeSelector, cd.NodeSelector)
	}
	if len(nodeSelector) == 0 {
		nodeSelector = nil
	}

	var affinity *corev1.Affinity
	if gpusEnabled && gpus.Affinity != nil {
		affinity = gpus.Affinity
	}
	if cdEnabled && cd.Affinity != nil {
		affinity = cd.Affinity
	}

	return priorityClassName, nodeSelector, tolerations, affinity
}

// renderDRAFeatureGates renders the feature-gate map as the FEATURE_GATES env value
// (comma-separated Key=Value with a trailing comma, matching the upstream
// k8s-dra-driver-gpu Helm chart). Keys are sorted so the rendered value is a pure
// function of the input and reconciles do not churn the pod spec. Empty when none.
func renderDRAFeatureGates(gates map[string]bool) string {
	if len(gates) == 0 {
		return ""
	}
	names := make([]string, 0, len(gates))
	for name := range gates {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "%s=%t,", name, gates[name])
	}
	return b.String()
}
