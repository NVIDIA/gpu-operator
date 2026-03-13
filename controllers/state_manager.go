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

package controllers

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	apiconfigv1 "github.com/openshift/api/config/v1"
	configv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	"golang.org/x/mod/semver"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

const (
	commonGPULabelKey                   = "nvidia.com/gpu.present"
	commonGPULabelValue                 = "true"
	commonOperandsLabelKey              = "nvidia.com/gpu.deploy.operands"
	commonOperandsLabelValue            = "true"
	migManagerLabelKey                  = "nvidia.com/gpu.deploy.mig-manager"
	migManagerLabelValue                = "true"
	migCapableLabelKey                  = "nvidia.com/mig.capable"
	migCapableLabelValue                = "true"
	migConfigLabelKey                   = "nvidia.com/mig.config"
	migConfigDisabledValue              = "all-disabled"
	vgpuHostDriverLabelKey              = "nvidia.com/vgpu.host-driver-version"
	gpuProductLabelKey                  = "nvidia.com/gpu.product"
	nfdLabelPrefix                      = "feature.node.kubernetes.io/"
	nfdKernelLabelKey                   = "feature.node.kubernetes.io/kernel-version.full"
	nfdOSTreeVersionLabelKey            = "feature.node.kubernetes.io/system-os_release.OSTREE_VERSION"
	nfdOSReleaseIDLabelKey              = "feature.node.kubernetes.io/system-os_release.ID"
	nfdOSVersionIDLabelKey              = "feature.node.kubernetes.io/system-os_release.VERSION_ID"
	ocpDriverToolkitVersionLabel        = "openshift.driver-toolkit.rhcos"
	ocpDriverToolkitIdentificationLabel = "openshift.driver-toolkit"
	appLabelKey                         = "app"
	ocpDriverToolkitIdentificationValue = "true"
	ocpNamespaceMonitoringLabelKey      = "openshift.io/cluster-monitoring"
	ocpNamespaceMonitoringLabelValue    = "true"
	precompiledIdentificationLabelKey   = "nvidia.com/precompiled"
	precompiledIdentificationLabelValue = "true"
	// see bundle/manifests/gpu-operator.clusterserviceversion.yaml
	//     --> ClusterServiceVersion.metadata.annotations.operatorframework.io/suggested-namespace
	ocpSuggestedNamespace              = "nvidia-gpu-operator"
	gpuWorkloadConfigLabelKey          = "nvidia.com/gpu.workload.config"
	gpuWorkloadConfigContainer         = "container"
	gpuWorkloadConfigVMPassthrough     = "vm-passthrough"
	gpuWorkloadConfigVMVgpu            = "vm-vgpu"
	kubevirtDevicePluginDeployLabelKey = "nvidia.com/gpu.deploy.sandbox-device-plugin"
	kataDevicePluginDeployLabelKey     = "nvidia.com/gpu.deploy.kata-sandbox-device-plugin"
	podSecurityLabelPrefix             = "pod-security.kubernetes.io/"
	podSecurityLevelPrivileged         = "privileged"
	driverAutoUpgradeAnnotationKey     = "nvidia.com/gpu-driver-upgrade-enabled"
	commonDriverDaemonsetName          = "nvidia-driver-daemonset"
	commonVGPUManagerDaemonsetName     = "nvidia-vgpu-manager-daemonset"
)

var (
	defaultGPUWorkloadConfig = gpuWorkloadConfigContainer
	podSecurityModes         = []string{"enforce", "audit", "warn"}
)

var gpuStateLabels = map[string]map[string]string{
	gpuWorkloadConfigContainer: {
		"nvidia.com/gpu.deploy.driver":                "true",
		"nvidia.com/gpu.deploy.gpu-feature-discovery": "true",
		"nvidia.com/gpu.deploy.container-toolkit":     "true",
		"nvidia.com/gpu.deploy.device-plugin":         "true",
		"nvidia.com/gpu.deploy.dcgm":                  "true",
		"nvidia.com/gpu.deploy.dcgm-exporter":         "true",
		"nvidia.com/gpu.deploy.node-status-exporter":  "true",
		"nvidia.com/gpu.deploy.operator-validator":    "true",
	},
	gpuWorkloadConfigVMPassthrough: {
		"nvidia.com/gpu.deploy.sandbox-device-plugin": "true",
		"nvidia.com/gpu.deploy.sandbox-validator":     "true",
		"nvidia.com/gpu.deploy.vfio-manager":          "true",
		"nvidia.com/gpu.deploy.kata-manager":          "true",
		"nvidia.com/gpu.deploy.cc-manager":            "true",
	},
	gpuWorkloadConfigVMVgpu: {
		"nvidia.com/gpu.deploy.sandbox-device-plugin": "true",
		"nvidia.com/gpu.deploy.vgpu-manager":          "true",
		"nvidia.com/gpu.deploy.vgpu-device-manager":   "true",
		"nvidia.com/gpu.deploy.sandbox-validator":     "true",
		"nvidia.com/gpu.deploy.cc-manager":            "true",
	},
}

var gpuNodeLabels = map[string]string{
	"feature.node.kubernetes.io/pci-10de.present":      "true",
	"feature.node.kubernetes.io/pci-0302_10de.present": "true",
	"feature.node.kubernetes.io/pci-0300_10de.present": "true",
}

type gpuWorkloadConfiguration struct {
	config      string
	sandboxMode string // SandboxWorkloads.Mode (e.g. "kubevirt", "kata") — only affects vm-passthrough labels
	node        string
	log         logr.Logger
}

// OpenShiftDriverToolkit contains the values required to deploy
// OpenShift DriverToolkit DaemonSet.
type OpenShiftDriverToolkit struct {
	// true if the cluster runs OpenShift and
	// Operator.UseOpenShiftDriverToolkit is turned on in the
	// ClusterPolicy
	requested bool
	// true of the DriverToolkit is requested and the cluster has all
	// the required components (NFD RHCOS OSTree label + OCP
	// DriverToolkit imagestream)
	enabled bool

	currentRhcosVersion      string
	rhcosVersions            map[string]bool
	rhcosDriverToolkitImages map[string]string
}

// ClusterPolicyController represents clusterpolicy controller spec for GPU operator
type ClusterPolicyController struct {
	client client.Client

	ctx               context.Context
	singleton         *gpuv1.ClusterPolicy
	logger            logr.Logger
	scheme            *runtime.Scheme
	operatorNamespace string

	resources            []Resources
	controls             []controlFunc
	stateNames           []string
	operatorMetrics      *OperatorMetrics
	idx                  int
	kernelVersionMap     map[string]string
	currentKernelVersion string

	k8sVersion       string
	openshift        string
	ocpDriverToolkit OpenShiftDriverToolkit

	runtime        gpuv1.Runtime
	gpuNodeOSTag   string
	hasGPUNodes    bool
	hasNFDLabels   bool
	sandboxEnabled bool
}

func addState(n *ClusterPolicyController, path string) {
	// TODO check for path
	res, ctrl := addResourcesControls(n, path)

	n.controls = append(n.controls, ctrl)
	n.resources = append(n.resources, res)
	n.stateNames = append(n.stateNames, filepath.Base(path))
}

// OpenshiftVersion fetches OCP version
func OpenshiftVersion(ctx context.Context) (string, error) {
	cfg := config.GetConfigOrDie()
	client, err := configv1.NewForConfig(cfg)
	if err != nil {
		return "", err
	}

	v, err := client.ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	for _, condition := range v.Status.History {
		if condition.State != "Completed" {
			continue
		}

		ocpV := strings.Split(condition.Version, ".")
		if len(ocpV) > 1 {
			return ocpV[0] + "." + ocpV[1], nil
		}
		return ocpV[0], nil
	}

	return "", fmt.Errorf("failed to find Completed Cluster Version")
}

// KubernetesVersion fetches the Kubernetes API server version
func KubernetesVersion() (string, error) {
	cfg := config.GetConfigOrDie()
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return "", fmt.Errorf("error building discovery client: %v", err)
	}

	info, err := discoveryClient.ServerVersion()
	if err != nil {
		return "", fmt.Errorf("unable to fetch server version information: %v", err)
	}

	return info.GitVersion, nil
}

// GetClusterWideProxy returns cluster wide proxy object setup in OCP
func GetClusterWideProxy(ctx context.Context) (*apiconfigv1.Proxy, error) {
	cfg := config.GetConfigOrDie()
	client, err := configv1.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	proxy, err := client.Proxies().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return proxy, nil
}

func hasMIGConfigLabel(labels map[string]string) bool {
	if _, ok := labels[migConfigLabelKey]; ok {
		if labels[migConfigLabelKey] != "" {
			return true
		}
	}
	return false
}

// hasCommonGPULabel returns true if common Nvidia GPU label exists among provided node labels
func hasCommonGPULabel(labels map[string]string) bool {
	if _, ok := labels[commonGPULabelKey]; ok {
		if labels[commonGPULabelKey] == commonGPULabelValue {
			// node is already labelled with common label
			return true
		}
	}
	return false
}

// hasGPULabels return true if node labels contain Nvidia GPU labels
func hasGPULabels(labels map[string]string) bool {
	for key, val := range labels {
		if _, ok := gpuNodeLabels[key]; ok {
			if gpuNodeLabels[key] == val {
				return true
			}
		}
	}
	return false
}

// hasNFDLabels return true if node labels contain NFD labels
func hasNFDLabels(labels map[string]string) bool {
	for key := range labels {
		if strings.HasPrefix(key, nfdLabelPrefix) {
			return true
		}
	}
	return false
}

// hasMIGCapableGPU returns true if this node has GPU capable of MIG partitioning.
func hasMIGCapableGPU(labels map[string]string) bool {
	if value, exists := labels[vgpuHostDriverLabelKey]; exists && value != "" {
		// vGPU node
		return false
	}

	if value, exists := labels[migCapableLabelKey]; exists {
		return value == migCapableLabelValue
	}

	// check product label if mig.capable label does not exist
	if value, exists := labels[gpuProductLabelKey]; exists {
		if strings.Contains(strings.ToLower(value), "h100") ||
			strings.Contains(strings.ToLower(value), "a100") ||
			strings.Contains(strings.ToLower(value), "a30") {
			return true
		}
	}

	return false
}

func hasMIGManagerLabel(labels map[string]string) bool {
	for key := range labels {
		if key == migManagerLabelKey {
			return true
		}
	}
	return false
}

func hasOperandsDisabled(labels map[string]string) bool {
	if value, ok := labels[commonOperandsLabelKey]; ok {
		if value == "false" {
			return true
		}
	}
	return false
}

func isValidWorkloadConfig(workloadConfig string) bool {
	_, ok := gpuStateLabels[workloadConfig]
	return ok
}

// getWorkloadConfig returns the GPU workload configured for the node.
// If an error occurs when searching for the workload config,
// return defaultGPUWorkloadConfig.
func getWorkloadConfig(labels map[string]string, sandboxEnabled bool) (string, error) {
	if !sandboxEnabled {
		return gpuWorkloadConfigContainer, nil
	}
	if workloadConfig, ok := labels[gpuWorkloadConfigLabelKey]; ok {
		if isValidWorkloadConfig(workloadConfig) {
			return workloadConfig, nil
		}
		return defaultGPUWorkloadConfig, fmt.Errorf("invalid GPU workload config: %v", workloadConfig)
	}
	return defaultGPUWorkloadConfig, fmt.Errorf("no GPU workload config found")
}

// getEffectiveStateLabels returns the state labels to apply for the given workload config and sandbox mode.
// When config is vm-passthrough and mode is "kata", returns labels with kata-device-plugin instead of sandbox-device-plugin.
func getEffectiveStateLabels(config, mode string) map[string]string {
	labels, ok := gpuStateLabels[config]
	if !ok {
		return nil
	}

	if config != gpuWorkloadConfigVMPassthrough {
		return labels
	}

	// update labels for the sandbox modes for passthrough
	switch gpuv1.SandboxWorkloadsMode(mode) {
	case gpuv1.Kata:
		delete(labels, kubevirtDevicePluginDeployLabelKey)
		labels[kataDevicePluginDeployLabelKey] = "true"
	case gpuv1.KubeVirt:
		delete(labels, kataDevicePluginDeployLabelKey)
		labels[kubevirtDevicePluginDeployLabelKey] = "true"
	}
	return labels
}

// removeAllGPUStateLabels removes all gpuStateLabels from the provided map of node labels.
// removeAllGPUStateLabels returns true if the labels map has been modified.
func removeAllGPUStateLabels(labels map[string]string) bool {
	modified := false
	for _, labelsMap := range gpuStateLabels {
		for key := range labelsMap {
			if _, ok := labels[key]; ok {
				delete(labels, key)
				modified = true
			}
		}
	}
	if _, ok := labels[kataDevicePluginDeployLabelKey]; ok {
		delete(labels, kataDevicePluginDeployLabelKey)
		modified = true
	}
	if _, ok := labels[migManagerLabelKey]; ok {
		delete(labels, migManagerLabelKey)
		modified = true
	}
	return modified
}

// updateGPUStateLabels applies the correct GPU state labels for the GPU workload configuration.
// updateGPUStateLabels returns true if the input labels map is modified.
func (w *gpuWorkloadConfiguration) updateGPUStateLabels(labels map[string]string) bool {
	if hasOperandsDisabled(labels) {
		// Operands are disabled, delete all GPU state labels
		w.log.Info("Operands are disabled for node", "NodeName", w.node, "Label", commonOperandsLabelKey, "Value", "false")
		w.log.Info("Disabling all operands for node", "NodeName", w.node)
		return removeAllGPUStateLabels(labels)
	}
	removed := w.removeGPUStateLabels(labels)
	added := w.addGPUStateLabels(labels)
	return removed || added
}

// addGPUStateLabels adds GPU state labels needed for the GPU workload configuration.
// If a required state label already exists on the node, honor the current value.
// For vm-passthrough, uses kata-device-plugin when mode is "kata", otherwise sandbox-device-plugin.
func (w *gpuWorkloadConfiguration) addGPUStateLabels(labels map[string]string) bool {
	modified := false
	effective := getEffectiveStateLabels(w.config, w.sandboxMode)
	for key, value := range effective {
		if _, ok := labels[key]; !ok {
			w.log.Info("Setting node label", "NodeName", w.node, "Label", key, "Value", value)
			labels[key] = value
			modified = true
		}
	}
	if w.config == gpuWorkloadConfigContainer && hasMIGCapableGPU(labels) && !hasMIGManagerLabel(labels) {
		w.log.Info("Setting node label", "NodeName", w.node, "Label", migManagerLabelKey, "Value", migManagerLabelValue)
		labels[migManagerLabelKey] = migManagerLabelValue
		modified = true
	}
	return modified
}

// removeGPUStateLabels removes GPU state labels not needed for the GPU workload configuration.
// Uses effective labels for (config, mode) so vm-passthrough+kata keeps kata-device-plugin, not sandbox-device-plugin.
func (w *gpuWorkloadConfiguration) removeGPUStateLabels(labels map[string]string) bool {
	modified := false
	effective := getEffectiveStateLabels(w.config, w.sandboxMode)
	// Collect all keys that are ever used as state labels (from static map + mode-dependent key)
	allStateKeys := make(map[string]bool)
	for _, labelsMap := range gpuStateLabels {
		for key := range labelsMap {
			allStateKeys[key] = true
		}
	}
	allStateKeys[kataDevicePluginDeployLabelKey] = true
	for key := range labels {
		if !allStateKeys[key] {
			continue
		}
		if _, keep := effective[key]; !keep {
			w.log.Info("Deleting node label", "NodeName", w.node, "Label", key)
			delete(labels, key)
			modified = true
		}
	}
	if w.config != gpuWorkloadConfigContainer {
		if _, ok := labels[migManagerLabelKey]; ok {
			w.log.Info("Deleting node label", "NodeName", w.node, "Label", migManagerLabelKey)
			delete(labels, migManagerLabelKey)
			modified = true
		}
	}
	return modified
}

func (n *ClusterPolicyController) applyDriverAutoUpgradeAnnotation() error {
	// fetch all nodes
	opts := []client.ListOption{}
	list := &corev1.NodeList{}
	err := n.client.List(n.ctx, list, opts...)
	if err != nil {
		return fmt.Errorf("unable to list nodes to check annotations, err %s", err.Error())
	}
	for _, node := range list.Items {
		node := node
		labels := node.GetLabels()
		if !hasCommonGPULabel(labels) {
			// not a gpu node
			continue
		}
		// set node annotation for driver auto-upgrade
		updateRequired := false
		value := "true"
		annotationValue, annotationExists := node.Annotations[driverAutoUpgradeAnnotationKey]
		if (n.singleton.Spec.Driver.IsEnabled() || n.singleton.Spec.Driver.UseNvidiaDriverCRDType()) &&
			n.singleton.Spec.Driver.UpgradePolicy != nil &&
			n.singleton.Spec.Driver.UpgradePolicy.AutoUpgrade &&
			!n.sandboxEnabled {
			// check if we need to add the annotation
			if !annotationExists {
				updateRequired = true
			} else if annotationValue != "true" {
				updateRequired = true
			}
		} else {
			// check if we need to remove the annotation
			if annotationExists {
				updateRequired = true
			}
			value = "null"
		}
		if !updateRequired {
			continue
		}
		// update annotation
		node.Annotations[driverAutoUpgradeAnnotationKey] = value
		if value == "null" {
			// remove annotation if value is null
			delete(node.Annotations, driverAutoUpgradeAnnotationKey)
		}
		err := n.client.Update(n.ctx, &node)
		if err != nil {
			n.logger.Info("Failed to update node state annotation on a node",
				"node", node.Name,
				"annotationKey", driverAutoUpgradeAnnotationKey,
				"annotationValue", value, "error", err)
			return err
		}
	}
	return nil
}

// labelGPUNodes labels nodes with GPU's with NVIDIA common label
// it return clusterHasNFDLabels (bool), gpuNodesTotal (int), error
func (n *ClusterPolicyController) labelGPUNodes() (bool, int, error) {
	ctx := n.ctx
	// fetch all nodes
	opts := []client.ListOption{}
	list := &corev1.NodeList{}
	err := n.client.List(ctx, list, opts...)
	if err != nil {
		return false, 0, fmt.Errorf("unable to list nodes to check labels: %w", err)
	}
	clusterHasNFDLabels := false
	gpuNodesTotal := 0
	for _, node := range list.Items {
		node := node
		updateLabels := false
		nodeOriginal := node.DeepCopy()
		// get node labels
		labels := node.GetLabels()
		if !clusterHasNFDLabels {
			clusterHasNFDLabels = hasNFDLabels(labels)
		}
		config, err := getWorkloadConfig(labels, n.sandboxEnabled)
		if err != nil {
			n.logger.Info("WARNING: failed to get GPU workload config for node; using default",
				"NodeName", node.Name, "SandboxEnabled", n.sandboxEnabled,
				"Error", err, "defaultGPUWorkloadConfig", defaultGPUWorkloadConfig)
		}
		n.logger.Info("GPU workload configuration", "NodeName", node.Name, "GpuWorkloadConfig", config)
		mode := n.singleton.Spec.SandboxWorkloads.Mode
		gpuWorkloadConfig := &gpuWorkloadConfiguration{config: config, sandboxMode: mode, node: node.Name, log: n.logger}
		if !hasCommonGPULabel(labels) && hasGPULabels(labels) {
			n.logger.Info("Node has GPU(s)", "NodeName", node.Name)
			// label the node with common Nvidia GPU label
			n.logger.Info("Setting node label", "NodeName", node.Name, "Label", commonGPULabelKey, "Value", commonGPULabelValue)
			labels[commonGPULabelKey] = commonGPULabelValue
			// update node labels
			node.SetLabels(labels)
			updateLabels = true
		} else if hasCommonGPULabel(labels) && !hasGPULabels(labels) {
			// previously labelled node and no longer has GPUs
			// label node to reset common Nvidia GPU label
			n.logger.Info("Node no longer has GPUs", "NodeName", node.Name)
			n.logger.Info("Setting node label", "Label", commonGPULabelKey, "Value", "false")
			labels[commonGPULabelKey] = "false"
			n.logger.Info("Disabling all operands for node", "NodeName", node.Name)
			removeAllGPUStateLabels(labels)
			// update node labels
			node.SetLabels(labels)
			updateLabels = true
		}

		if hasCommonGPULabel(labels) {
			// If node has GPU, then add state labels as per the workload type
			n.logger.Info("Checking GPU state labels on the node", "NodeName", node.Name)
			if gpuWorkloadConfig.updateGPUStateLabels(labels) {
				n.logger.Info("Applying correct GPU state labels to the node", "NodeName", node.Name)
				node.SetLabels(labels)
				updateLabels = true
			}
			// Disable MIG on the node explicitly where no MIG config is specified
			if n.singleton.Spec.MIGManager.IsEnabled() && hasMIGCapableGPU(labels) && !hasMIGConfigLabel(labels) {
				if n.singleton.Spec.MIGManager.Config != nil && n.singleton.Spec.MIGManager.Config.Default == migConfigDisabledValue {
					n.logger.Info("Setting MIG config label", "NodeName", node.Name, "Label", migConfigLabelKey, "Value", migConfigDisabledValue)
					labels[migConfigLabelKey] = migConfigDisabledValue
					node.SetLabels(labels)
					updateLabels = true
				}
			}
			// increment GPU node count
			gpuNodesTotal++

			// add GPU node CoreOS version for OCP
			if n.ocpDriverToolkit.requested {
				rhcosVersion, ok := labels[nfdOSTreeVersionLabelKey]
				if ok {
					n.ocpDriverToolkit.rhcosVersions[rhcosVersion] = true
					n.logger.V(1).Info("GPU node running RHCOS",
						"nodeName", node.Name,
						"RHCOS version", rhcosVersion,
					)
				} else {
					n.logger.Info("node doesn't have the proper NFD RHCOS version label.",
						"nodeName", node.Name,
						"nfdLabel", nfdOSTreeVersionLabelKey,
					)
				}
			}
		}

		// update node with the latest labels
		if updateLabels {
			err = n.client.Patch(ctx, &node, client.MergeFrom(nodeOriginal))
			if err != nil {
				return false, 0, fmt.Errorf("unable to label node %s for the GPU Operator deployment, err %s",
					node.Name, err.Error())
			}
		}
	} // end node loop

	n.logger.Info("Number of nodes with GPU label", "NodeCount", gpuNodesTotal)
	n.operatorMetrics.gpuNodesTotal.Set(float64(gpuNodesTotal))
	return clusterHasNFDLabels, gpuNodesTotal, nil
}

func getRuntimeString(node corev1.Node) (gpuv1.Runtime, error) {
	// ContainerRuntimeVersion string will look like <runtime>://<x.y.z>
	runtimeVer := node.Status.NodeInfo.ContainerRuntimeVersion
	var runtime gpuv1.Runtime
	switch {
	case strings.HasPrefix(runtimeVer, "docker"):
		runtime = gpuv1.Docker
	case strings.HasPrefix(runtimeVer, "containerd"):
		runtime = gpuv1.Containerd
	case strings.HasPrefix(runtimeVer, "cri-o"):
		runtime = gpuv1.CRIO
	default:
		return "", fmt.Errorf("runtime not recognized: %s", runtimeVer)
	}
	return runtime, nil
}

func (n *ClusterPolicyController) getGPUNodeOSTag() (string, error) {
	ctx := n.ctx
	opts := []client.ListOption{
		client.MatchingLabels(map[string]string{commonGPULabelKey: commonGPULabelValue}),
		client.Limit(1),
	}
	nodeList := &corev1.NodeList{}
	err := n.client.List(ctx, nodeList, opts...)
	if err != nil {
		return "", fmt.Errorf("unable to list nodes with GPU present: %w", err)
	}
	if len(nodeList.Items) == 0 {
		return "", fmt.Errorf("no nodes found with GPU present")
	}

	labels := nodeList.Items[0].Labels
	osName, ok := labels[nfdOSReleaseIDLabelKey]
	if !ok {
		return "", fmt.Errorf("unable to retrieve OS name from label %s", nfdOSReleaseIDLabelKey)
	}
	osVersion, ok := labels[nfdOSVersionIDLabelKey]
	if !ok {
		return "", fmt.Errorf("unable to retrieve OS version from label %s", nfdOSVersionIDLabelKey)
	}
	osMajorVersion := strings.Split(osVersion, ".")[0]
	osMajorNumber, err := strconv.Atoi(osMajorVersion)
	if err != nil {
		return "", fmt.Errorf("error processing OS major version %s: %w", osMajorVersion, err)
	}

	// If the OS is RockyLinux or RHEL 10 & above, we will omit the minor version when constructing the os image tag
	if osName == "rocky" || (osName == "rhel" && osMajorNumber >= 10) {
		osVersion = osMajorVersion
	}
	osTag := fmt.Sprintf("%s%s", osName, osVersion)

	return osTag, nil
}

func (n *ClusterPolicyController) setPodSecurityLabelsForNamespace() error {
	ctx := n.ctx
	namespaceName := clusterPolicyCtrl.operatorNamespace

	if n.openshift != "" && namespaceName != ocpSuggestedNamespace {
		// The GPU Operator is not installed in the suggested
		// namespace, so the namespace may be shared with other
		// untrusted operators.  Do not set Pod Security Admission labels.
		n.logger.Info("GPU Operator is not installed in the suggested namespace. Not setting Pod Security Admission labels for namespace",
			"namespace", namespaceName,
			"suggested namespace", ocpSuggestedNamespace)
		return nil
	}

	ns := &corev1.Namespace{}
	opts := client.ObjectKey{Name: namespaceName}
	err := n.client.Get(ctx, opts, ns)
	if err != nil {
		return fmt.Errorf("ERROR: could not get Namespace %s from client: %v", namespaceName, err)
	}

	patch := client.MergeFrom(ns.DeepCopy())
	modified := false
	// On K8s<1.21, namespaces are not automatically labeled with an immutable label. Initialize
	// a labels map if needed before adding PSA labels.
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/#automatic-labelling
	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
		modified = true
	}
	for _, mode := range podSecurityModes {
		key := podSecurityLabelPrefix + mode
		if val, ok := ns.Labels[key]; !ok || (val != podSecurityLevelPrivileged) {
			ns.Labels[key] = podSecurityLevelPrivileged
			modified = true
		}
	}

	if !modified {
		return nil
	}

	err = n.client.Patch(ctx, ns, patch)
	if err != nil {
		return fmt.Errorf("unable to label namespace %s with pod security levels: %v", namespaceName, err)
	}

	return nil
}

func (n *ClusterPolicyController) ocpEnsureNamespaceMonitoring() error {
	ctx := n.ctx
	namespaceName := clusterPolicyCtrl.operatorNamespace

	if namespaceName != ocpSuggestedNamespace {
		// The GPU Operator is not installed in the suggested
		// namespace, so the namespace may be shared with other
		// untrusted operators.  Do not enable namespace monitoring in
		// this case, as per OpenShift/Prometheus best practices.
		n.logger.Info("GPU Operator not installed in the suggested namespace, skipping namespace monitoring verification",
			"namespace", namespaceName,
			"suggested namespace", ocpSuggestedNamespace)
		return nil
	}

	ns := &corev1.Namespace{}
	opts := client.ObjectKey{Name: namespaceName}
	err := n.client.Get(ctx, opts, ns)
	if err != nil {
		return fmt.Errorf("ERROR: could not get Namespace %s from client: %v", namespaceName, err)
	}

	val, ok := ns.Labels[ocpNamespaceMonitoringLabelKey]
	if ok {
		// label already defined, do not change it
		var msg string
		if val == ocpNamespaceMonitoringLabelValue {
			msg = "OpenShift monitoring is enabled on the GPU Operator namespace"
		} else {
			msg = "WARNING: OpenShift monitoring currently disabled on user request"
		}
		n.logger.Info(msg,
			"namespace", namespaceName,
			"label", ocpNamespaceMonitoringLabelKey,
			"value", val,
			"excepted value", ocpNamespaceMonitoringLabelValue)

		return nil
	}

	// label not defined, enable monitoring
	n.logger.Info("Enabling OpenShift monitoring")
	n.logger.V(1).Info("Adding monitoring label to the operator namespace",
		"namespace", namespaceName,
		"label", ocpNamespaceMonitoringLabelKey,
		"value", ocpNamespaceMonitoringLabelValue)
	n.logger.Info("Monitoring can be disabled by setting the namespace label " +
		ocpNamespaceMonitoringLabelKey + "=false")
	patch := client.MergeFrom(ns.DeepCopy())
	ns.Labels[ocpNamespaceMonitoringLabelKey] = ocpNamespaceMonitoringLabelValue
	err = n.client.Patch(ctx, ns, patch)
	if err != nil {
		return fmt.Errorf("unable to label namespace %s for the GPU Operator monitoring, err %s",
			namespaceName, err.Error())
	}

	return nil
}

// getRuntime will detect the container runtime used by nodes in the
// cluster and correctly set the value for clusterPolicyController.runtime
// For openshift, set runtime to crio. Otherwise, the default runtime is
// containerd -- if >=1 node is configured with containerd, set
// clusterPolicyController.runtime = containerd
func (n *ClusterPolicyController) getRuntime() error {
	ctx := n.ctx
	// assume crio for openshift clusters
	if n.openshift != "" {
		n.runtime = gpuv1.CRIO
		return nil
	}

	opts := []client.ListOption{
		client.MatchingLabels{commonGPULabelKey: "true"},
	}
	list := &corev1.NodeList{}
	err := n.client.List(ctx, list, opts...)
	if err != nil {
		return fmt.Errorf("unable to list nodes prior to checking container runtime: %v", err)
	}

	var runtime gpuv1.Runtime
	for _, node := range list.Items {
		rt, err := getRuntimeString(node)
		if err != nil {
			n.logger.Info(fmt.Sprintf("Unable to get runtime info for node %s: %v", node.Name, err))
			continue
		}
		runtime = rt
		if runtime == gpuv1.Containerd {
			// default to containerd if >=1 node running containerd
			break
		}
	}

	if runtime.String() == "" {
		n.logger.Info("Unable to get runtime info from the cluster, defaulting to containerd")
		runtime = gpuv1.Containerd
	}
	n.runtime = runtime
	return nil
}

func (n *ClusterPolicyController) init(ctx context.Context, reconciler *ClusterPolicyReconciler, clusterPolicy *gpuv1.ClusterPolicy) error {
	n.singleton = clusterPolicy
	n.ctx = ctx
	n.idx = 0
	n.logger = reconciler.Log
	n.client = reconciler.Client
	n.scheme = reconciler.Scheme

	if len(n.controls) == 0 {
		clusterPolicyCtrl.operatorNamespace = reconciler.Namespace

		version, err := OpenshiftVersion(ctx)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		n.openshift = version

		k8sVersion, err := KubernetesVersion()
		if err != nil {
			return err
		}
		if !semver.IsValid(k8sVersion) {
			return fmt.Errorf("k8s version detected '%s' is not a valid semantic version", k8sVersion)
		}
		n.k8sVersion = k8sVersion
		n.logger.Info("Kubernetes version detected", "version", k8sVersion)

		err = validateClusterPolicySpec(&clusterPolicy.Spec)
		if err != nil {
			return fmt.Errorf("error validating clusterpolicy: %w", err)
		}

		n.operatorMetrics = initOperatorMetrics()
		n.logger.Info("Operator metrics initialized.")

		addState(n, "/opt/gpu-operator/pre-requisites")
		addState(n, "/opt/gpu-operator/state-operator-metrics")
		addState(n, "/opt/gpu-operator/state-driver")
		addState(n, "/opt/gpu-operator/state-container-toolkit")
		addState(n, "/opt/gpu-operator/state-operator-validation")
		addState(n, "/opt/gpu-operator/state-device-plugin")
		addState(n, "/opt/gpu-operator/state-mps-control-daemon")
		addState(n, "/opt/gpu-operator/state-dcgm")
		addState(n, "/opt/gpu-operator/state-dcgm-exporter")
		addState(n, "/opt/gpu-operator/gpu-feature-discovery")
		addState(n, "/opt/gpu-operator/state-mig-manager")
		addState(n, "/opt/gpu-operator/state-node-status-exporter")
		// add sandbox workload states
		addState(n, "/opt/gpu-operator/state-vgpu-manager")
		addState(n, "/opt/gpu-operator/state-vgpu-device-manager")
		addState(n, "/opt/gpu-operator/state-sandbox-validation")
		addState(n, "/opt/gpu-operator/state-vfio-manager")
		addState(n, "/opt/gpu-operator/state-sandbox-device-plugin")
		addState(n, "/opt/gpu-operator/state-kata-device-plugin")
		addState(n, "/opt/gpu-operator/state-kata-manager")
		addState(n, "/opt/gpu-operator/state-cc-manager")
	}

	if clusterPolicy.Spec.SandboxWorkloads.IsEnabled() {
		n.sandboxEnabled = true
		// defaultGPUWorkloadConfig is container, unless
		// user overrides in ClusterPolicy with a valid GPU
		// workload configuration
		defaultWorkload := clusterPolicy.Spec.SandboxWorkloads.DefaultWorkload
		if isValidWorkloadConfig(defaultWorkload) {
			n.logger.Info("Default GPU workload is overridden in ClusterPolicy", "DefaultWorkload", defaultWorkload)
			defaultGPUWorkloadConfig = defaultWorkload
		}
	} else {
		n.sandboxEnabled = false
	}
	n.logger.Info("Sandbox workloads", "Enabled", n.sandboxEnabled, "DefaultWorkload", defaultGPUWorkloadConfig)

	if n.openshift != "" && (n.singleton.Spec.Operator.UseOpenShiftDriverToolkit == nil ||
		*n.singleton.Spec.Operator.UseOpenShiftDriverToolkit) {
		// DTK is enabled by default on OpenShift
		n.ocpDriverToolkit.requested = true

		// mind that this is executed at every reconciliation loop,
		// do not assume "permanent" data storage.
		n.ocpDriverToolkit.rhcosVersions = make(map[string]bool)
		n.ocpDriverToolkit.rhcosDriverToolkitImages = make(map[string]string)
	} else {
		// DTK is disabled on non-OpenShift deployments or when operator.use_ocp_driver_toolkit
		// is explicitly set to 'false' in ClusterPolicy
		n.ocpDriverToolkit.requested = false
		n.ocpDriverToolkit.enabled = false

		n.operatorMetrics.openshiftDriverToolkitEnabled.Set(openshiftDriverToolkitDisabled)
	}

	if clusterPolicy.Spec.PSA.IsEnabled() {
		// label namespace with Pod Security Admission levels
		n.logger.Info("Pod Security is enabled. Adding labels to GPU Operator namespace", "namespace", n.operatorNamespace)
		err := n.setPodSecurityLabelsForNamespace()
		if err != nil {
			return err
		}
		n.logger.Info("Pod Security Admission labels added to GPU Operator namespace", "namespace", n.operatorNamespace)
	}

	// fetch all nodes and label gpu nodes
	hasNFDLabels, gpuNodeCount, err := n.labelGPUNodes()
	if err != nil {
		return err
	}
	n.hasGPUNodes = gpuNodeCount != 0
	n.hasNFDLabels = hasNFDLabels

	if n.hasGPUNodes {
		gpuNodeOSTag, err := n.getGPUNodeOSTag()
		if err != nil {
			return fmt.Errorf("failed to retrieve GPU node OS tag: %w", err)
		}
		n.gpuNodeOSTag = gpuNodeOSTag
	}
	// fetch all nodes and annotate gpu nodes
	err = n.applyDriverAutoUpgradeAnnotation()
	if err != nil {
		return err
	}

	// detect the container runtime on worker nodes
	err = n.getRuntime()
	if err != nil {
		return err
	}
	n.logger.Info(fmt.Sprintf("Using container runtime: %s", n.runtime.String()))

	// fetch all kernel versions from the GPU nodes in the cluster
	if n.singleton.Spec.Driver.IsEnabled() && n.singleton.Spec.Driver.UsePrecompiledDrivers() {
		kernelVersionMap, err := n.getKernelVersionsMap()
		if err != nil {
			n.logger.Info("Unable to obtain all kernel versions of the GPU nodes in the cluster", "err", err)
			return err
		}
		n.kernelVersionMap = kernelVersionMap
	}

	if n.openshift != "" {
		// initialize openshift specific parameters
		err = n.initOCPParams()
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *ClusterPolicyController) initOCPParams() error {
	// initialize openshift specific parameters
	if n.singleton.Spec.Driver.UsePrecompiledDrivers() {
		// disable DTK for OCP when already pre-compiled drivers are used
		n.ocpDriverToolkit.enabled = false
	} else if n.ocpDriverToolkit.requested {
		hasImageStream, err := ocpHasDriverToolkitImageStream(n)
		if err != nil {
			n.logger.Info("ocpHasDriverToolkitImageStream", "err", err)
			return err
		}
		hasCompatibleNFD := len(n.ocpDriverToolkit.rhcosVersions) != 0
		n.ocpDriverToolkit.enabled = hasImageStream && hasCompatibleNFD

		if n.ocpDriverToolkit.enabled {
			n.operatorMetrics.openshiftDriverToolkitEnabled.Set(openshiftDriverToolkitEnabled)
		} else {
			n.operatorMetrics.openshiftDriverToolkitEnabled.Set(openshiftDriverToolkitNotPossible)
		}
		n.logger.Info("OpenShift Driver Toolkit requested",
			"hasCompatibleNFD", hasCompatibleNFD,
			"hasDriverToolkitImageStream", hasImageStream)

		n.logger.Info("OpenShift Driver Toolkit",
			"enabled", n.ocpDriverToolkit.enabled)

		if hasImageStream {
			n.operatorMetrics.openshiftDriverToolkitIsMissing.Set(0)
		} else {
			n.operatorMetrics.openshiftDriverToolkitIsMissing.Set(1)
		}
		if n.hasGPUNodes && !hasCompatibleNFD {
			n.operatorMetrics.openshiftDriverToolkitNfdTooOld.Set(1)
		} else {
			n.operatorMetrics.openshiftDriverToolkitNfdTooOld.Set(0)
		}
	}
	// enable monitoring for the gpu-operator namespace
	if err := n.ocpEnsureNamespaceMonitoring(); err != nil {
		return err
	}
	return nil
}

func (n *ClusterPolicyController) step() (gpuv1.State, error) {
	result := gpuv1.Ready

	// Skip state-driver if NVIDIADriver CRD is enabled
	// TODO:
	//   - Properly clean up any k8s object associated with 'state-driver'
	//     and owned by the Clusterpolicy controller.
	//   - In object_controls.go, check the OwnerRef for existing objects
	//     before managing them. Clusterpolicy controller should not be creating /
	//     updating / deleting objects owned by another controller.
	if (n.stateNames[n.idx] == "state-driver" || n.stateNames[n.idx] == "state-vgpu-manager") &&
		n.singleton.Spec.Driver.UseNvidiaDriverCRDType() {
		n.logger.Info("NVIDIADriver CRD is enabled, cleaning up all NVIDIA driver daemonsets owned by ClusterPolicy")
		n.idx++
		// Cleanup all driver daemonsets owned by ClusterPolicy.
		err := n.cleanupAllDriverDaemonSets(n.ctx)
		if err != nil {
			return gpuv1.NotReady, fmt.Errorf("failed to cleanup all NVIDIA driver daemonsets owned by ClusterPolicy: %w", err)
		}
		return gpuv1.Disabled, nil
	}

	for _, fs := range n.controls[n.idx] {
		stat, err := fs(*n)
		if err != nil {
			return stat, err
		}
		// successfully deployed resource, now check if its ready
		if stat != gpuv1.Ready {
			// mark overall status of this component as not-ready and continue with other resources, while this becomes ready
			result = stat
		}
	}

	// move to next state
	n.idx++

	return result, nil
}

// TODO
// func (n ClusterPolicyController) validate() {
//	 add custom validation functions
// }

func (n ClusterPolicyController) last() bool {
	return n.idx == len(n.controls)
}

func (n ClusterPolicyController) isStateEnabled(stateName string) bool {
	clusterPolicySpec := &n.singleton.Spec

	switch stateName {
	case "pre-requisites":
		return !clusterPolicySpec.CDI.IsNRIPluginEnabled()
	case "state-driver":
		return clusterPolicySpec.Driver.IsEnabled()
	case "state-container-toolkit":
		return clusterPolicySpec.Toolkit.IsEnabled()
	case "state-device-plugin":
		return clusterPolicySpec.DevicePlugin.IsEnabled()
	case "state-mps-control-daemon":
		return clusterPolicySpec.DevicePlugin.IsEnabled()
	case "state-dcgm":
		return clusterPolicySpec.DCGM.IsEnabled()
	case "state-dcgm-exporter":
		return clusterPolicySpec.DCGMExporter.IsEnabled()
	case "state-mig-manager":
		return clusterPolicySpec.MIGManager.IsEnabled()
	case "gpu-feature-discovery":
		return clusterPolicySpec.GPUFeatureDiscovery.IsEnabled()
	case "state-node-status-exporter":
		return clusterPolicySpec.NodeStatusExporter.IsEnabled()
	case "state-sandbox-device-plugin":
		return n.sandboxEnabled && clusterPolicySpec.SandboxDevicePlugin.IsEnabled() && clusterPolicySpec.SandboxWorkloads.Mode == string(gpuv1.KubeVirt)
	case "state-kata-device-plugin":
		return n.sandboxEnabled && clusterPolicySpec.KataSandboxDevicePlugin.IsEnabled() && clusterPolicySpec.SandboxWorkloads.Mode == string(gpuv1.Kata)
	case "state-kata-manager":
		return n.sandboxEnabled && clusterPolicySpec.KataManager.IsEnabled()
	case "state-vfio-manager":
		return n.sandboxEnabled && clusterPolicySpec.VFIOManager.IsEnabled()
	case "state-vgpu-device-manager":
		return n.sandboxEnabled && clusterPolicySpec.VGPUDeviceManager.IsEnabled()
	case "state-vgpu-manager":
		return n.sandboxEnabled && clusterPolicySpec.VGPUManager.IsEnabled()
	case "state-cc-manager":
		return n.sandboxEnabled && clusterPolicySpec.CCManager.IsEnabled() && clusterPolicySpec.SandboxWorkloads.Mode == string(gpuv1.Kata)
	case "state-sandbox-validation":
		return n.sandboxEnabled
	case "state-operator-validation":
		return true
	case "state-operator-metrics":
		return true
	default:
		n.logger.Error(nil, "invalid state passed", "stateName", stateName)
		return false
	}
}

func validateClusterPolicySpec(spec *gpuv1.ClusterPolicySpec) error {
	if !spec.CDI.IsEnabled() && spec.CDI.IsNRIPluginEnabled() {
		return fmt.Errorf("the NRI Plugin cannot be enabled when CDI is disabled")
	}
	return nil
}
