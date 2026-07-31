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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/image"
)

const (
	// draDriverImageEnvName is the fallback env var for the DRA driver image when the
	// CR does not specify repository/image/version.
	draDriverImageEnvName = "DRA_DRIVER_IMAGE"
	// draValidatorImageEnvName is the env var for the gpu-operator image that ships the
	// nvidia-validator binary used by the kubelet-plugin driver-validation init container.
	draValidatorImageEnvName = "VALIDATOR_IMAGE"

	// Default gRPC health service ports of the kubelet-plugin containers, matching the
	// upstream k8s-dra-driver-gpu Helm chart.
	defaultGPUsHealthcheckPort           = int32(51516)
	defaultComputeDomainsHealthcheckPort = int32(51515)
)

// resolveHealthcheckPort returns the spec-provided health service port, or the
// container's default when unset; -1 (disabled) omits the probes at render time.
func resolveHealthcheckPort(hc *nvidiav1alpha1.DRADriverHealthcheckSpec, defaultPort int32) int32 {
	if hc != nil && hc.Enabled != nil && !*hc.Enabled {
		return -1
	}
	if hc != nil && hc.Port != nil {
		return *hc.Port
	}
	return defaultPort
}

type stateDRADriver struct {
	stateSkel
}

var _ State = (*stateDRADriver)(nil)

func NewStateDRADriver(
	k8sClient client.Client,
	namespace string,
	scheme *runtime.Scheme,
	manifestDir string) (State, error) {

	skel, err := newStateSkel(k8sClient, namespace, scheme, manifestDir,
		"state-dra-driver", "NVIDIA DRA driver deployed in the cluster")
	if err != nil {
		return nil, err
	}
	return &stateDRADriver{stateSkel: skel}, nil
}

func (s *stateDRADriver) Sync(ctx context.Context, customResource interface{}, infoCatalog InfoCatalog) (SyncState, error) {
	cr, ok := customResource.(*nvidiav1alpha1.GPUCluster)
	if !ok {
		return SyncStateError, fmt.Errorf("GPUCluster CR not provided as input to Sync()")
	}

	objs, err := s.getManifestObjects(ctx, cr, infoCatalog)
	if err != nil {
		return SyncStateNotReady, fmt.Errorf("failed to create k8s objects from manifests: %w", err)
	}

	if len(objs) == 0 {
		// The gpus capability renders unconditionally, so an empty render indicates a
		// bug (e.g. missing manifests); surface it rather than reporting ready.
		return SyncStateNotReady, fmt.Errorf("no objects rendered for the DRA driver state")
	}

	return s.syncObjects(ctx, cr, objs)
}

func (s *stateDRADriver) GetWatchSources(mgr ctrlManager) map[string]SyncingSource {
	return gpuClusterDaemonSetSource(mgr)
}

func (s *stateDRADriver) getManifestObjects(ctx context.Context, cr *nvidiav1alpha1.GPUCluster, infoCatalog InfoCatalog) ([]*unstructured.Unstructured, error) {
	apiVersion, err := draResourceAPIVersion(infoCatalog)
	if err != nil {
		return nil, fmt.Errorf("failed to get DRA resource apiVersion: %w", err)
	}

	draDriverSpec, err := getDRADriverSpec(&cr.Spec.DRADriver)
	if err != nil {
		return nil, fmt.Errorf("failed to construct DRA driver spec: %w", err)
	}

	hostPaths := cr.Spec.HostPaths
	daemonsets := cr.Spec.Daemonsets
	openshiftVersion, err := clusterOpenshiftVersion(infoCatalog)
	if err != nil {
		return nil, fmt.Errorf("failed to get OpenShift version: %w", err)
	}

	renderData := &draDriverRenderData{
		DRADriver:             draDriverSpec,
		HostPaths:             &hostPaths,
		Daemonsets:            &daemonsets,
		Namespace:             s.namespace,
		OpenshiftVersion:      openshiftVersion,
		DeviceClassAPIVersion: apiVersion,
		FeatureGates:          cr.Spec.DRADriver.FeatureGates,
		GPUsHealthcheckPort: resolveHealthcheckPort(
			cr.Spec.DRADriver.GPUs.KubeletPlugin.Healthcheck, defaultGPUsHealthcheckPort),
		ComputeDomainsHealthcheckPort: resolveHealthcheckPort(
			cr.Spec.DRADriver.ComputeDomains.KubeletPlugin.Healthcheck, defaultComputeDomainsHealthcheckPort),
	}

	return s.renderObjects(ctx, renderData)
}

// getDRADriverSpec builds the render-time DRA driver spec, resolving the DRA driver
// image (from the CR, falling back to DRA_DRIVER_IMAGE) and the init-container image
// (the gpu-operator image carrying nvidia-validator, from VALIDATOR_IMAGE).
func getDRADriverSpec(spec *nvidiav1alpha1.DRADriverSpec) (*draDriverSpec, error) {
	imagePath, err := image.ImagePath(spec.Repository, spec.Image, spec.Version, draDriverImageEnvName)
	if err != nil {
		return nil, fmt.Errorf("failed to construct DRA driver image path: %w", err)
	}

	initImagePath, err := image.ImagePath("", "", "", draValidatorImageEnvName)
	if err != nil {
		return nil, fmt.Errorf("failed to construct DRA driver validator image path: %w", err)
	}

	resourceRequirements := []*nvidiav1.ResourceRequirements{
		spec.GPUs.KubeletPlugin.Resources,
	}
	if spec.IsComputeDomainsEnabled() {
		resourceRequirements = append(resourceRequirements, spec.ComputeDomains.KubeletPlugin.Resources)
	}

	return &draDriverSpec{
		Spec:                   spec,
		ImagePath:              imagePath,
		InitImagePath:          initImagePath,
		InitContainerResources: maxResourceRequirements(resourceRequirements...),
	}, nil
}

func maxResourceRequirements(requirements ...*nvidiav1.ResourceRequirements) *nvidiav1.ResourceRequirements {
	result := &nvidiav1.ResourceRequirements{}

	for _, requirement := range requirements {
		if requirement == nil {
			continue
		}
		result.Requests = maxResourceList(result.Requests, requirement.Requests)
		result.Limits = maxResourceList(result.Limits, requirement.Limits)
	}

	// A request-only container can have a larger request than another container's
	// explicit limit. Keep the merged requirement valid without adding limits for
	// resources that were unbounded in every source container.
	for name, request := range result.Requests {
		limit, ok := result.Limits[name]
		if ok && request.Cmp(limit) > 0 {
			result.Limits[name] = request.DeepCopy()
		}
	}

	if len(result.Requests) == 0 && len(result.Limits) == 0 {
		return nil
	}
	return result
}

func maxResourceList(resourceLists ...corev1.ResourceList) corev1.ResourceList {
	var result corev1.ResourceList

	for _, resourceList := range resourceLists {
		for name, quantity := range resourceList {
			current, ok := result[name]
			if ok && quantity.Cmp(current) <= 0 {
				continue
			}
			if result == nil {
				result = corev1.ResourceList{}
			}
			result[name] = quantity.DeepCopy()
		}
	}

	return result
}
