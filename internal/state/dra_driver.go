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

	// Metrics ports of the kubelet-plugin containers, mirroring the hardcoded
	// HTTP_ENDPOINT values in manifests/state-dra-driver/0500_daemonset.yaml.
	gpusMetricsPort           = int32(8080)
	computeDomainsMetricsPort = int32(8081)
)

// reservedKubeletPluginEnv lists the environment variables the kubelet-plugin
// template owns. A later entry with the same name in kubeletPlugin.env would win
// inside the container and move a listener behind the back of the port
// validation, so they are rejected.
var reservedKubeletPluginEnv = []string{"HEALTHCHECK_PORT", "HTTP_ENDPOINT"}

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

// validateKubeletPluginPorts rejects a GPUCluster whose kubelet-plugin containers
// would bind the same TCP port twice. Every listener in the pod, health services and
// metrics endpoints alike, shares one network namespace, so the second bind fails
// with EADDRINUSE and that container crash-loops; between containers, which one
// loses is a startup race. The check runs on resolved health ports so a user who
// sets only one port to the other container's default is caught too. A health port
// <= 0 means the service is disabled, so it never binds.
func validateKubeletPluginPorts(computeDomainsEnabled bool, gpusPort, computeDomainsPort int32) error {
	type listener struct {
		name string
		port int32
	}
	listeners := []listener{
		{"the gpus container metrics endpoint", gpusMetricsPort},
		{"spec.draDriver.gpus.kubeletPlugin.healthcheck.port", gpusPort},
	}
	if computeDomainsEnabled {
		listeners = append(listeners,
			listener{"the compute-domains container metrics endpoint", computeDomainsMetricsPort},
			listener{"spec.draDriver.computeDomains.kubeletPlugin.healthcheck.port", computeDomainsPort},
		)
	}

	owner := map[int32]string{}
	for _, l := range listeners {
		if l.port <= 0 {
			continue
		}
		if prev, ok := owner[l.port]; ok {
			return fmt.Errorf("%s and %s must differ; both resolve to %d", prev, l.name, l.port)
		}
		owner[l.port] = l.name
	}
	return nil
}

// validateKubeletPluginEnv rejects a GPUCluster whose kubeletPlugin.env would
// override a listener address the template renders. The gpus container is always
// rendered; the compute-domains container only when enabled, so its env is only
// checked then.
func validateKubeletPluginEnv(computeDomainsEnabled bool, gpusEnv, computeDomainsEnv []nvidiav1.EnvVar) error {
	type envList struct {
		field string
		env   []nvidiav1.EnvVar
	}
	lists := []envList{{"spec.draDriver.gpus.kubeletPlugin.env", gpusEnv}}
	if computeDomainsEnabled {
		lists = append(lists, envList{"spec.draDriver.computeDomains.kubeletPlugin.env", computeDomainsEnv})
	}

	for _, l := range lists {
		for _, e := range l.env {
			for _, reserved := range reservedKubeletPluginEnv {
				if e.Name == reserved {
					return fmt.Errorf("%s must not set %s; it is managed by the operator "+
						"(use the healthcheck.port field for the health service port)", l.field, reserved)
				}
			}
		}
	}
	return nil
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

func (s *stateDRADriver) Sync(ctx context.Context, customResource any, infoCatalog InfoCatalog) (SyncState, error) {
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

	gpusHealthcheckPort := resolveHealthcheckPort(
		cr.Spec.DRADriver.GPUs.KubeletPlugin.Healthcheck, defaultGPUsHealthcheckPort)
	computeDomainsHealthcheckPort := resolveHealthcheckPort(
		cr.Spec.DRADriver.ComputeDomains.KubeletPlugin.Healthcheck, defaultComputeDomainsHealthcheckPort)
	if err := validateKubeletPluginPorts(cr.Spec.DRADriver.IsComputeDomainsEnabled(),
		gpusHealthcheckPort, computeDomainsHealthcheckPort); err != nil {
		return nil, fmt.Errorf("invalid DRA driver kubelet-plugin port configuration: %w", err)
	}
	if err := validateKubeletPluginEnv(cr.Spec.DRADriver.IsComputeDomainsEnabled(),
		cr.Spec.DRADriver.GPUs.KubeletPlugin.Env, cr.Spec.DRADriver.ComputeDomains.KubeletPlugin.Env); err != nil {
		return nil, fmt.Errorf("invalid DRA driver kubelet-plugin env configuration: %w", err)
	}

	renderData := &draDriverRenderData{
		DRADriver:                     draDriverSpec,
		HostPaths:                     &hostPaths,
		Daemonsets:                    &daemonsets,
		Namespace:                     s.namespace,
		OpenshiftVersion:              openshiftVersion,
		DeviceClassAPIVersion:         apiVersion,
		FeatureGates:                  cr.Spec.DRADriver.FeatureGates,
		GPUsHealthcheckPort:           gpusHealthcheckPort,
		ComputeDomainsHealthcheckPort: computeDomainsHealthcheckPort,
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
