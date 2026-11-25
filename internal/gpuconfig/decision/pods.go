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

package decision

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Names of the operator objects the pod rules recognize.
const (
	// Gate pods are identified by their controller DaemonSet, never by an
	// app label: the DRA validator's pods carry the validator's app label
	// but run no gate.
	ValidatorDaemonSet        = "nvidia-operator-validator"
	DRAKubeletPluginDaemonSet = "nvidia-dra-driver-kubelet-plugin"

	GateContainer    = "gpu-config"
	toolkitContainer = "toolkit-validation"

	gpuDriver           = "gpu.nvidia.com"
	computeDomainDriver = "compute-domain.nvidia.com"

	// The validator's workload pods. They can reach a GPU with no allocator,
	// so they count whether or not they run.
	CUDAValidatorApp   = "nvidia-cuda-validator"
	PluginValidatorApp = "nvidia-device-plugin-validator"
)

var (
	// allocatorApps are the container-stack allocators. They wait for
	// toolkit-ready in their toolkit-validation init container.
	allocatorApps = []string{
		"nvidia-device-plugin-daemonset",
		"nvidia-device-plugin-mps-control-daemon",
		"nvidia-mig-manager",
	}
	validationApps = []string{CUDAValidatorApp, PluginValidatorApp}
	// managementPods identify the operator's DRA management pods: DCGM, its
	// exporter and the DRA validator, which only runs nvidia-smi -L. Their
	// claims use admin access, which reserves no device, and the container
	// stack's counterparts request no GPU resource, so the pod check ignores
	// them. A pod must carry the label and be owned by the DaemonSet.
	managementPods = []struct{ key, value, daemonSet string }{
		{"app", "nvidia-dcgm-dra", "nvidia-dcgm-dra"},
		{"app", "nvidia-dcgm-exporter-dra", "nvidia-dcgm-exporter-dra"},
		{"app.kubernetes.io/name", "nvidia-dra-validator", "nvidia-dra-validator"},
	}
)

// ErrClaimNotFound is returned by Cluster.ClaimDrivers for a claim that
// doesn't exist.
var ErrClaimNotFound = errors.New("claim not found")

// isGPUResource counts every nvidia.com/ extended resource, because the device
// plugin can rename its resources to any name in that domain.
func isGPUResource(name corev1.ResourceName) bool {
	return strings.HasPrefix(string(name), "nvidia.com/")
}

func controllerDaemonSet(pod *corev1.Pod) string {
	owner := metav1.GetControllerOf(pod)
	if owner == nil || owner.Kind != "DaemonSet" {
		return ""
	}
	return owner.Name
}

func statuses(pod *corev1.Pod) map[string]corev1.ContainerStatus {
	out := make(map[string]corev1.ContainerStatus, len(pod.Status.InitContainerStatuses)+len(pod.Status.ContainerStatuses))
	for _, s := range pod.Status.InitContainerStatuses {
		out[s.Name] = s
	}
	for _, s := range pod.Status.ContainerStatuses {
		out[s.Name] = s
	}
	return out
}

// exitedOK reports whether the container finished successfully. A wait
// container killed while waiting, as when its pod is deleted, ends non-zero
// without having got past the wait.
func exitedOK(s corev1.ContainerStatus) bool {
	return s.State.Terminated != nil && s.State.Terminated.ExitCode == 0
}

func hasLastState(s corev1.ContainerStatus) bool {
	l := s.LastTerminationState
	return l.Running != nil || l.Terminated != nil || l.Waiting != nil
}

func isRunning(pod *corev1.Pod) bool {
	for _, s := range statuses(pod) {
		if s.State.Running != nil {
			return true
		}
	}
	return false
}

func containerNames(pod *corev1.Pod) []string {
	var out []string
	for _, c := range pod.Spec.InitContainers {
		out = append(out, c.Name)
	}
	for _, c := range pod.Spec.Containers {
		out = append(out, c.Name)
	}
	return out
}

// allocatorPastWait is the allocator-pod rule: an allocator pod in the
// operator namespace that got past its wait for toolkit-ready.
func (d *Decider) allocatorPastWait(pod *corev1.Pod) bool {
	if pod.Namespace != d.Config.Namespace || !slices.Contains(allocatorApps, pod.Labels["app"]) {
		return false
	}
	st := statuses(pod)
	if s, ok := st[toolkitContainer]; ok && exitedOK(s) {
		return true
	}
	for name, s := range st {
		if name == toolkitContainer {
			continue
		}
		if s.State.Running != nil || (s.State.Waiting != nil && hasLastState(s)) {
			return true
		}
	}
	return false
}

func (d *Decider) isGatePod(pod *corev1.Pod) bool {
	if pod.Namespace != d.Config.Namespace {
		return false
	}
	ds := controllerDaemonSet(pod)
	return ds == ValidatorDaemonSet || ds == DRAKubeletPluginDaemonSet
}

// gatePassed is the gate-pod rule: a gate pod whose gate passed. A pod
// recreated for the current driver load still sits in its gate.
func (d *Decider) gatePassed(pod *corev1.Pod) bool {
	if !d.isGatePod(pod) {
		return false
	}
	st := statuses(pod)
	names := containerNames(pod)
	// Containers after the gate; all of them for a pod without one.
	later := names
	if i := slices.Index(names, GateContainer); i >= 0 {
		if s, ok := st[GateContainer]; ok && exitedOK(s) {
			return true
		}
		later = names[i+1:]
	}
	for _, name := range later {
		s, ok := st[name]
		if !ok {
			continue
		}
		if s.State.Running != nil || s.State.Terminated != nil || (s.State.Waiting != nil && hasLastState(s)) {
			return true
		}
	}
	return false
}

func (d *Decider) isApplierPod(pod *corev1.Pod) bool {
	return pod.Namespace == d.Config.Namespace && d.Config.ApplierDaemonSet != "" &&
		controllerDaemonSet(pod) == d.Config.ApplierDaemonSet
}

func (d *Decider) isValidationPod(pod *corev1.Pod) bool {
	return pod.Namespace == d.Config.Namespace && slices.Contains(validationApps, pod.Labels["app"])
}

// claimNames returns the names of the claims pod references. ok is false when
// a reference can't be resolved to a name yet: a template whose generated
// claim isn't in the pod's status, or a status entry with no claim name.
func claimNames(pod *corev1.Pod) (names []string, ok bool) {
	generated := make(map[string]*string, len(pod.Status.ResourceClaimStatuses))
	for _, s := range pod.Status.ResourceClaimStatuses {
		generated[s.Name] = s.ResourceClaimName
	}
	ok = true
	for _, rc := range pod.Spec.ResourceClaims {
		if rc.ResourceClaimName != nil {
			names = append(names, *rc.ResourceClaimName)
			continue
		}
		name := generated[rc.Name]
		if name == nil || *name == "" {
			ok = false
			continue
		}
		names = append(names, *name)
	}
	if s := pod.Status.ExtendedResourceClaimStatus; s != nil {
		if s.ResourceClaimName == "" {
			ok = false
		} else {
			names = append(names, s.ResourceClaimName)
		}
	}
	return names, ok
}

// claimInfo holds, per claim a pod references, the drivers of its allocated
// devices. unknown is set when a claim's state can't be determined: an
// unresolved reference or a claim that doesn't exist.
type claimInfo struct {
	drivers [][]string
	unknown bool
}

// claims reads each claim pod references, once per decision.
func (d *decision) claims(ctx context.Context, pod *corev1.Pod) (claimInfo, error) {
	names, ok := claimNames(pod)
	info := claimInfo{unknown: !ok}
	for _, name := range names {
		key := pod.Namespace + "/" + name
		drivers, cached := d.claimCache[key]
		if !cached {
			var err error
			drivers, err = d.Cluster.ClaimDrivers(ctx, pod.Namespace, name)
			switch {
			case errors.Is(err, ErrClaimNotFound):
				drivers = nil
				d.claimNotFound[key] = true
			case err != nil:
				return info, fmt.Errorf("%w: reading claim %s: %w", ErrAPI, key, err)
			}
			d.claimCache[key] = drivers
		}
		if d.claimNotFound[key] {
			info.unknown = true
			continue
		}
		info.drivers = append(info.drivers, drivers)
	}
	return info, nil
}

// isGPUPod reports whether pod requests an NVIDIA GPU resource, references an
// NVIDIA DRA claim or one whose state can't be determined, or is a validation
// workload pod.
func (d *decision) isGPUPod(ctx context.Context, pod *corev1.Pod) (bool, error) {
	if d.isValidationPod(pod) {
		return true, nil
	}
	for _, cs := range [][]corev1.Container{pod.Spec.InitContainers, pod.Spec.Containers} {
		for _, c := range cs {
			for name := range c.Resources.Requests {
				if isGPUResource(name) {
					return true, nil
				}
			}
			for name := range c.Resources.Limits {
				if isGPUResource(name) {
					return true, nil
				}
			}
		}
	}
	info, err := d.claims(ctx, pod)
	if err != nil {
		return false, err
	}
	if info.unknown {
		return true, nil
	}
	for _, drivers := range info.drivers {
		if slices.Contains(drivers, gpuDriver) || slices.Contains(drivers, computeDomainDriver) {
			return true, nil
		}
	}
	return false, nil
}

// isManagementPod reports whether pod belongs to one of the operator's DRA
// management DaemonSets.
func (d *decision) isManagementPod(pod *corev1.Pod) bool {
	if pod.Namespace != d.Config.Namespace {
		return false
	}
	ds := controllerDaemonSet(pod)
	return slices.ContainsFunc(managementPods, func(l struct{ key, value, daemonSet string }) bool {
		return pod.Labels[l.key] == l.value && ds == l.daemonSet
	})
}
