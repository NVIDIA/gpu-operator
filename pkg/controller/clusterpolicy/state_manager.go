package clusterpolicy

import (
	"context"
	"fmt"
	"strings"

	gpuv1 "github.com/NVIDIA/gpu-operator/pkg/apis/nvidia/v1"
	promv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	secv1 "github.com/openshift/api/security/v1"

	configv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	commonGPULabelKey   = "nvidia.com/gpu.present"
	commonGPULabelValue = "true"
)

var gpuNodeLabels = map[string]string{
	"feature.node.kubernetes.io/pci-10de.present":      "true",
	"feature.node.kubernetes.io/pci-0302_10de.present": "true",
	"feature.node.kubernetes.io/pci-0300_10de.present": "true",
}

type state interface {
	init(*ReconcileClusterPolicy, *gpuv1.ClusterPolicy)
	step()
	validate()
	last()
}

type ClusterPolicyController struct {
	singleton *gpuv1.ClusterPolicy

	resources []Resources
	controls  []controlFunc
	rec       *ReconcileClusterPolicy
	idx       int
	openshift string
}

func addState(n *ClusterPolicyController, path string) error {
	// TODO check for path
	res, ctrl := addResourcesControls(path, n.openshift)

	n.controls = append(n.controls, ctrl)
	n.resources = append(n.resources, res)

	return nil
}

func OpenshiftVersion() (string, error) {
	cfg := config.GetConfigOrDie()
	client, err := configv1.NewForConfig(cfg)
	if err != nil {
		return "", err
	}

	v, err := client.ClusterVersions().Get("version", metav1.GetOptions{})
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
		} else {
			return ocpV[0], nil
		}
	}

	return "", fmt.Errorf("Failed to find Completed Cluster Version")
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

// labelGPUNodes labels nodes with GPU's with Nvidia common label
func (n *ClusterPolicyController) labelGPUNodes() error {
	// fetch all nodes
	opts := []client.ListOption{}
	list := &corev1.NodeList{}
	err := n.rec.client.List(context.TODO(), list, opts...)
	if err != nil {
		return fmt.Errorf("Unable to list nodes to check labels, err %s", err.Error())
	}

	for _, node := range list.Items {
		// get node labels
		labels := node.GetLabels()
		if !hasCommonGPULabel(labels) && hasGPULabels(labels) {
			// label node with common Nvidia GPU label
			labels[commonGPULabelKey] = commonGPULabelValue
			node.SetLabels(labels)
			err = n.rec.client.Update(context.TODO(), &node)
			if err != nil {
				return fmt.Errorf("Unable to label node %s with nvidia.com/gpu.present=true, err %s", node.ObjectMeta.Name, err.Error())
			}
		} else if hasCommonGPULabel(labels) && !hasGPULabels(labels) {
			// previously labelled node and no longer has GPU's
			// label node to reset common Nvidia GPU label
			labels[commonGPULabelKey] = "false"
			node.SetLabels(labels)
			err = n.rec.client.Update(context.TODO(), &node)
			if err != nil {
				return fmt.Errorf("Unable to reset node label for %s with nvidia.com/gpu.present=false, err %s", node.ObjectMeta.Name, err.Error())
			}
		}
	}
	return nil
}

func (n *ClusterPolicyController) init(r *ReconcileClusterPolicy, i *gpuv1.ClusterPolicy) error {
	version, err := OpenshiftVersion()
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	n.openshift = version
	n.singleton = i

	n.rec = r
	n.idx = 0

	promv1.AddToScheme(r.scheme)
	secv1.AddToScheme(r.scheme)

	// fetch all nodes and label gpu nodes
	err = n.labelGPUNodes()
	if err != nil {
		return err
	}

	addState(n, "/opt/gpu-operator/state-driver")
	addState(n, "/opt/gpu-operator/state-container-toolkit")
	addState(n, "/opt/gpu-operator/state-device-plugin")
	addState(n, "/opt/gpu-operator/state-device-plugin-validation")
	addState(n, "/opt/gpu-operator/state-monitoring")
	// if requested, deploy GFD to utilitize Multi-Instance GPU's.
	if i.Spec.Operator.DeployGFD {
		addState(n, "/opt/gpu-operator/gpu-feature-discovery")
	}
	return nil
}

func (n *ClusterPolicyController) step() (gpuv1.State, error) {
	for _, fs := range n.controls[n.idx] {
		stat, err := fs(*n)
		if err != nil {
			return stat, err
		}

		if stat != gpuv1.Ready {
			return stat, nil
		}
	}

	n.idx = n.idx + 1

	return gpuv1.Ready, nil
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
