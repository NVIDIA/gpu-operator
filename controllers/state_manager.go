package controllers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/v1"
	secv1 "github.com/openshift/api/security/v1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	apiconfigv1 "github.com/openshift/api/config/v1"
	configv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	commonGPULabelKey    = "nvidia.com/gpu.present"
	commonGPULabelValue  = "true"
	migManagerLabelKey   = "nvidia.com/gpu.deploy.mig-manager"
	migManagerLabelValue = "true"
	gpuProductLabelKey   = "nvidia.com/gpu.product"
	nfdLabelPrefix       = "feature.node.kubernetes.io/"
)

var gpuStateLabels = map[string]string{
	"nvidia.com/gpu.deploy.driver":                "true",
	"nvidia.com/gpu.deploy.gpu-feature-discovery": "true",
	"nvidia.com/gpu.deploy.container-toolkit":     "true",
	"nvidia.com/gpu.deploy.device-plugin":         "true",
	"nvidia.com/gpu.deploy.dcgm":                  "true",
	"nvidia.com/gpu.deploy.dcgm-exporter":         "true",
	"nvidia.com/gpu.deploy.node-status-exporter":  "true",
	"nvidia.com/gpu.deploy.operator-validator":    "true",
}

var gpuNodeLabels = map[string]string{
	"feature.node.kubernetes.io/pci-10de.present":      "true",
	"feature.node.kubernetes.io/pci-0302_10de.present": "true",
	"feature.node.kubernetes.io/pci-0300_10de.present": "true",
}

type state interface {
	init(*ClusterPolicyReconciler, *gpuv1.ClusterPolicy)
	step()
	validate()
	last()
}

// ClusterPolicyController represents clusterpolicy controller spec for GPU operator
type ClusterPolicyController struct {
	singleton         *gpuv1.ClusterPolicy
	operatorNamespace string

	resources       []Resources
	controls        []controlFunc
	stateNames      []string
	operatorMetrics OperatorMetrics
	rec             *ClusterPolicyReconciler
	idx             int
	openshift       string
	runtime         gpuv1.Runtime

	hasGPUNodes  bool
	hasNFDLabels bool
}

func addState(n *ClusterPolicyController, path string) error {
	// TODO check for path
	res, ctrl := addResourcesControls(n, path, n.openshift)

	n.controls = append(n.controls, ctrl)
	n.resources = append(n.resources, res)
	n.stateNames = append(n.stateNames, filepath.Base(path))

	return nil
}

// OpenshiftVersion fetches OCP version
func OpenshiftVersion() (string, error) {
	cfg := config.GetConfigOrDie()
	client, err := configv1.NewForConfig(cfg)
	if err != nil {
		return "", err
	}

	v, err := client.ClusterVersions().Get(context.TODO(), "version", metav1.GetOptions{})
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

	return "", fmt.Errorf("Failed to find Completed Cluster Version")
}

// GetClusterWideProxy returns cluster wide proxy object setup in OCP
func GetClusterWideProxy() (*apiconfigv1.Proxy, error) {
	cfg := config.GetConfigOrDie()
	client, err := configv1.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	proxy, err := client.Proxies().Get(context.TODO(), "cluster", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return proxy, nil
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

// addMissingGPUStateLabels checks if the nodeLabels contain the GPUState labels,
// and it adds them when missing (the label value is *not* checked, only the key)
// addMissingGPUStateLabels returns true if the nodeLabels map has been updated
func (n *ClusterPolicyController) addMissingGPUStateLabels(nodeLabels map[string]string) bool {
	modified := false
	for key, value := range gpuStateLabels {
		if _, ok := nodeLabels[key]; !ok {
			nodeLabels[key] = value
			modified = true
		}
		n.rec.Log.Info(" - ", "Label=", key, " value=", nodeLabels[key])
	}

	// add mig-manager label if missing
	if hasMIGCapableGPU(nodeLabels) && !hasMIGManagerLabel(nodeLabels) {
		nodeLabels[migManagerLabelKey] = migManagerLabelValue
		modified = true
		n.rec.Log.Info(" - ", "Label=", migManagerLabelKey, " value=", migManagerLabelValue)
	}
	return modified
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
	for key, value := range labels {
		if strings.Contains(key, "vgpu.host-driver-version") && value != "" {
			// vGPU node
			return false
		}
		// update this once GFD supports mig.capable label
		if key == gpuProductLabelKey {
			if strings.Contains(strings.ToLower(value), "a100") || strings.Contains(strings.ToLower(value), "a30") {
				return true
			}
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

// labelGPUNodes labels nodes with GPU's with Nvidia common label
// it return clusterHasNFDLabels (bool), gpuNodesTotal (int), error
func (n *ClusterPolicyController) labelGPUNodes() (bool, int, error) {
	// fetch all nodes
	opts := []client.ListOption{}
	list := &corev1.NodeList{}
	err := n.rec.Client.List(context.TODO(), list, opts...)
	if err != nil {
		return false, 0, fmt.Errorf("Unable to list nodes to check labels, err %s", err.Error())
	}

	clusterHasNFDLabels := false
	gpuNodesTotal := 0
	for _, node := range list.Items {
		// get node labels
		labels := node.GetLabels()
		if !clusterHasNFDLabels {
			clusterHasNFDLabels = hasNFDLabels(labels)
		}
		if !hasCommonGPULabel(labels) && hasGPULabels(labels) {
			// label the node with common Nvidia GPU label
			labels[commonGPULabelKey] = commonGPULabelValue
			// label the node with the state GPU labels
			for key, value := range gpuStateLabels {
				labels[key] = value
			}
			// add mig-manager label
			if hasMIGCapableGPU(labels) {
				labels[migManagerLabelKey] = migManagerLabelValue
			}
			// update node labels
			node.SetLabels(labels)
			err = n.rec.Client.Update(context.TODO(), &node)
			if err != nil {
				return false, 0, fmt.Errorf("Unable to label node %s for the GPU Operator deployment, err %s",
					node.ObjectMeta.Name, err.Error())
			}
		} else if hasCommonGPULabel(labels) && !hasGPULabels(labels) {
			// previously labelled node and no longer has GPU's
			// label node to reset common Nvidia GPU label
			labels[commonGPULabelKey] = "false"
			for key := range gpuStateLabels {
				delete(labels, key)
			}
			// delete mig-manager label
			delete(labels, migManagerLabelKey)
			// update node labels
			node.SetLabels(labels)
			err = n.rec.Client.Update(context.TODO(), &node)
			if err != nil {
				return false, 0, fmt.Errorf("Unable to reset the GPU Operator labels for node %s, err %s",
					node.ObjectMeta.Name, err.Error())
			}
		}
		if hasCommonGPULabel(labels) {
			n.rec.Log.Info("Checking GPU state labels on the node", "NodeName", node.ObjectMeta.Name)
			if n.addMissingGPUStateLabels(labels) {
				n.rec.Log.Info("Applying GPU state labels to the node", "NodeName", node.ObjectMeta.Name)
				err = n.rec.Client.Update(context.TODO(), &node)
				if err != nil {
					return false, 0, fmt.Errorf("Unable to update the GPU Operator labels for node %s, err %s",
						node.ObjectMeta.Name, err.Error())
				}
			}
			gpuNodesTotal++
		}
	}
	n.rec.Log.Info("Number of nodes with GPU label", "NodeCount", gpuNodesTotal)
	n.operatorMetrics.gpuNodesTotal.Set(float64(gpuNodesTotal))

	return clusterHasNFDLabels, gpuNodesTotal, nil
}

func getRuntimeString(node corev1.Node) (gpuv1.Runtime, error) {
	// ContainerRuntimeVersion string will look like <runtime>://<x.y.z>
	runtimeVer := node.Status.NodeInfo.ContainerRuntimeVersion
	var runtime gpuv1.Runtime
	if strings.HasPrefix(runtimeVer, gpuv1.Docker.String()) {
		runtime = gpuv1.Docker
	} else if strings.HasPrefix(runtimeVer, gpuv1.Containerd.String()) {
		runtime = gpuv1.Containerd
	} else if strings.HasPrefix(runtimeVer, gpuv1.CRIO.String()) {
		runtime = gpuv1.CRIO
	} else {
		return "", fmt.Errorf("runtime not recognized: %s", runtimeVer)
	}
	return runtime, nil
}

// getRuntime will detect the container runtime used by nodes in the
// cluster and correctly set the value for clusterPolicyController.runtime
// For openshift, set runtime to crio. Otherwise, the default runtime is
// containerd -- if >=1 node is configured with containerd, set
// clusterPolicyController.runtime = containerd
func (n *ClusterPolicyController) getRuntime() error {
	// assume crio for openshift clusters
	if n.openshift != "" {
		n.runtime = gpuv1.CRIO
		return nil
	}

	opts := []client.ListOption{
		client.MatchingLabels{commonGPULabelKey: "true"},
	}
	list := &corev1.NodeList{}
	err := n.rec.Client.List(context.TODO(), list, opts...)
	if err != nil {
		return fmt.Errorf("Unable to list nodes prior to checking container runtime: %v", err)
	}

	var runtime gpuv1.Runtime
	for _, node := range list.Items {
		rt, err := getRuntimeString(node)
		if err != nil {
			log.Warnf("Unable to get runtime info for node %s: %v", node.Name, err)
			continue
		}
		runtime = rt
		if runtime == gpuv1.Containerd {
			// default to containerd if >=1 node running containerd
			break
		}
	}

	if runtime.String() == "" {
		log.Warn("Unable to get runtime info from the cluster, defaulting to containerd")
		runtime = gpuv1.Containerd
	}
	n.runtime = runtime
	return nil
}

func (n *ClusterPolicyController) init(reconciler *ClusterPolicyReconciler, clusterPolicy *gpuv1.ClusterPolicy) error {
	n.singleton = clusterPolicy

	n.rec = reconciler
	n.idx = 0

	if len(n.controls) == 0 {
		clusterPolicyCtrl.operatorNamespace = os.Getenv("OPERATOR_NAMESPACE")

		if clusterPolicyCtrl.operatorNamespace == "" {
			n.rec.Log.Error(nil, "OPERATOR_NAMESPACE environment variable not set, cannot proceed")
			// we cannot do anything without the operator namespace,
			// let the operator Pod run into `CrashloopBackOff`

			os.Exit(1)
		}

		version, err := OpenshiftVersion()
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
		n.openshift = version

		promv1.AddToScheme(reconciler.Scheme)
		secv1.AddToScheme(reconciler.Scheme)

		n.operatorMetrics = initOperatorMetrics(n)
		n.rec.Log.Info("Operator metrics initialized.")

		addState(n, "/opt/gpu-operator/pre-requisites")

		addState(n, "/opt/gpu-operator/state-operator-metrics")

		if clusterPolicy.Spec.NodeStatusExporter.IsNodeStatusExporterEnabled() {
			addState(n, "/opt/gpu-operator/state-node-status-exporter")
		}

		if clusterPolicy.Spec.Driver.IsDriverEnabled() {
			addState(n, "/opt/gpu-operator/state-driver")
		}
		if clusterPolicy.Spec.Toolkit.IsToolkitEnabled() {
			addState(n, "/opt/gpu-operator/state-container-toolkit")
		}
		addState(n, "/opt/gpu-operator/state-operator-validation")
		addState(n, "/opt/gpu-operator/state-device-plugin")
		if clusterPolicy.Spec.DCGM.IsEnabled() {
			addState(n, "/opt/gpu-operator/state-dcgm")
		}
		addState(n, "/opt/gpu-operator/state-dcgm-exporter")

		addState(n, "/opt/gpu-operator/gpu-feature-discovery")
		if clusterPolicy.Spec.MIGManager.IsMIGManagerEnabled() {
			addState(n, "/opt/gpu-operator/state-mig-manager")
		}
	}

	// fetch all nodes and label gpu nodes
	hasNFDLabels, gpuNodeCount, err := n.labelGPUNodes()
	if err != nil {
		return err
	}
	n.hasGPUNodes = gpuNodeCount != 0
	n.hasNFDLabels = hasNFDLabels

	// detect the container runtime on worker nodes
	err = n.getRuntime()
	if err != nil {
		return err
	}
	log.Info("Using container runtime: ", n.runtime.String())
	return nil
}

func (n *ClusterPolicyController) step() (gpuv1.State, error) {
	result := gpuv1.Ready
	for _, fs := range n.controls[n.idx] {
		stat, err := fs(*n)
		if err != nil {
			return stat, err
		}
		// successfully deployed resource, now check if its ready
		if stat != gpuv1.Ready {
			// mark overall status of this component as not-ready and continue with other resources, while this becomes ready
			result = gpuv1.NotReady
		}
	}

	// move to next state
	n.idx = n.idx + 1

	return result, nil
}

func (n ClusterPolicyController) validate() {
	// TODO add custom validation functions
}

func (n ClusterPolicyController) last() bool {
	if n.idx == len(n.controls) {
		return true
	}
	return false
}
