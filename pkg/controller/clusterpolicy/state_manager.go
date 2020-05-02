package clusterpolicy

import (
	"fmt"

	gpuv1 "github.com/NVIDIA/gpu-operator/pkg/apis/nvidia/v1"
	promv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	secv1 "github.com/openshift/api/security/v1"

	configv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

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

		return condition.Version, nil
	}

	return "", fmt.Errorf("Failed to find Completed Cluster Version")
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

	addState(n, "/opt/gpu-operator/state-container-toolkit")
	addState(n, "/opt/gpu-operator/state-driver")
	addState(n, "/opt/gpu-operator/state-driver-validation")
	addState(n, "/opt/gpu-operator/state-device-plugin")
	addState(n, "/opt/gpu-operator/state-device-plugin-validation")
	addState(n, "/opt/gpu-operator/state-monitoring")

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
