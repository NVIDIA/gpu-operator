package clusterpolicy

import (
	gpuv1 "github.com/NVIDIA/gpu-operator/pkg/apis/nvidia/v1"
	promv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	secv1 "github.com/openshift/api/security/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type state interface {
	init(*ReconcileClusterPolicy, *gpuv1.ClusterPolicy)
	step()
	validate()
	last()
}

type ClusterPolicyController struct {
	resources []Resources
	controls  []controlFunc
	rec       *ReconcileClusterPolicy
	ins       *gpuv1.ClusterPolicy
	idx       int
	clientset *kubernetes.Clientset
}

func addClient(n *ClusterPolicyController) {
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	n.clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
}

func addState(n *ClusterPolicyController, path string) error {

	// TODO check for path

	res, ctrl := addResourcesControls(path)

	n.controls = append(n.controls, ctrl)
	n.resources = append(n.resources, res)

	return nil
}

func (n *ClusterPolicyController) init(r *ReconcileClusterPolicy, i *gpuv1.ClusterPolicy) error {
	n.rec = r
	n.ins = i
	n.idx = 0

	promv1.AddToScheme(r.scheme)
	secv1.AddToScheme(r.scheme)

	addClient(n)

	addState(n, "/opt/gpu-operator/state-container-toolkit")
	addState(n, "/opt/gpu-operator/state-driver")
	addState(n, "/opt/gpu-operator/state-driver-validation")
	addState(n, "/opt/gpu-operator/state-device-plugin")
	addState(n, "/opt/gpu-operator/state-device-plugin-validation")
	addState(n, "/opt/gpu-operator/state-monitoring")

	return nil
}

func (n *ClusterPolicyController) step() (ResourceStatus, error) {

	for _, fs := range n.controls[n.idx] {

		stat, err := fs(*n)
		if err != nil {
			return stat, err
		}
		if stat != Ready {
			return stat, nil
		}
	}

	n.idx = n.idx + 1

	return Ready, nil
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
