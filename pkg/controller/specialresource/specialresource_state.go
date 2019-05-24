package specialresource

import (
	promv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	secv1 "github.com/openshift/api/security/v1"
	srov1alpha1 "github.com/zvonkok/special-resource-operator/pkg/apis/sro/v1alpha1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type state interface {
	init(*ReconcileSpecialResource, *srov1alpha1.SpecialResource)
	step()
	validate()
	last()
}

type SRO struct {
	resources []Resources
	controls  []controlFunc
	rec       *ReconcileSpecialResource
	ins       *srov1alpha1.SpecialResource
	idx       int
	clientset *kubernetes.Clientset
}

func addClient(n *SRO) {
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	n.clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
}

func addState(n *SRO, path string) error {

	// TODO check for path

	res, ctrl := addResourcesControls(path)

	n.controls = append(n.controls, ctrl)
	n.resources = append(n.resources, res)

	return nil
}

func addSchedulingType(n *SRO) error {

	if n.ins.Spec.Scheduling == "PriorityPreemption" {
		err := addPriorityPreemptionControls(n)
		if err != nil {
			return err
		}
		return nil
	}
	if n.ins.Spec.Scheduling == "TaintsTolerations" {
		err := addTaintsTolerationsControls(n)
		if err != nil {
			return err
		}
		return nil
	}
	return nil
}

func (n *SRO) init(r *ReconcileSpecialResource,
	i *srov1alpha1.SpecialResource) error {
	n.rec = r
	n.ins = i
	n.idx = 0

	promv1.AddToScheme(r.scheme)
	secv1.AddToScheme(r.scheme)

	addClient(n)

	addState(n, "/opt/sro/state-driver")
	addState(n, "/opt/sro/state-driver-validation")
	addState(n, "/opt/sro/state-device-plugin")
	addState(n, "/opt/sro/state-device-plugin-validation")
	addState(n, "/opt/sro/state-monitoring")

	addSchedulingType(n)

	return nil
}

func (n *SRO) step() (ResourceStatus, error) {

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

func (n SRO) validate() {
	// TODO add custom validation functions
}

func (n SRO) last() bool {
	if n.idx == len(n.controls) {
		return true
	}
	return false
}
