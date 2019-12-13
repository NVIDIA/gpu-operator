package specialresource

import (
	"context"
	"fmt"
	"os"

	promv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	secv1 "github.com/openshift/api/security/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedv1 "k8s.io/api/scheduling/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type controlFunc []func(n SRO) (ResourceStatus, error)

type ResourceStatus string

const (
	Ready    ResourceStatus = "Ready"
	NotReady ResourceStatus = "NotReady"
)

func ServiceAccount(n SRO) (ResourceStatus, error) {

	state := n.idx
	obj := &n.resources[state].ServiceAccount
	found := &corev1.ServiceAccount{}

	logger := log.WithValues("ServiceAccount", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.ins, obj, n.rec.scheme); err != nil {
		return NotReady, err
	}

	logger.Info("Looking for")
	err := n.rec.client.Get(context.TODO(), types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Not found, creating")
		err = n.rec.client.Create(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return NotReady, err
		}
		return Ready, nil
	} else if err != nil {
		return NotReady, err
	}

	logger.Info("Found")

	return Ready, nil
}

func Role(n SRO) (ResourceStatus, error) {

	state := n.idx
	obj := &n.resources[state].Role

	found := &rbacv1.Role{}
	logger := log.WithValues("Role", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.ins, obj, n.rec.scheme); err != nil {
		return NotReady, err
	}

	logger.Info("Looking for")
	err := n.rec.client.Get(context.TODO(), types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Not found, creating")
		err = n.rec.client.Create(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return NotReady, err
		}
		return Ready, nil
	} else if err != nil {
		return NotReady, err
	}

	logger.Info("Found")

	return Ready, nil
}

func RoleBinding(n SRO) (ResourceStatus, error) {

	state := n.idx
	obj := &n.resources[state].RoleBinding

	found := &rbacv1.RoleBinding{}
	logger := log.WithValues("RoleBinding", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.ins, obj, n.rec.scheme); err != nil {
		return NotReady, err
	}

	logger.Info("Looking for")
	err := n.rec.client.Get(context.TODO(), types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Not found, creating")
		err = n.rec.client.Create(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return NotReady, err
		}
		return Ready, nil
	} else if err != nil {
		return NotReady, err
	}

	logger.Info("Found")

	return Ready, nil
}

func ClusterRole(n SRO) (ResourceStatus, error) {

	state := n.idx
	obj := &n.resources[state].ClusterRole

	found := &rbacv1.ClusterRole{}
	logger := log.WithValues("ClusterRole", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.ins, obj, n.rec.scheme); err != nil {
		return NotReady, err
	}

	logger.Info("Looking for")
	err := n.rec.client.Get(context.TODO(), types.NamespacedName{Namespace: "", Name: obj.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Not found, creating")
		err = n.rec.client.Create(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return NotReady, err
		}
		return Ready, nil
	} else if err != nil {
		return NotReady, err
	}

	logger.Info("Found")

	return Ready, nil
}

func ClusterRoleBinding(n SRO) (ResourceStatus, error) {

	state := n.idx
	obj := &n.resources[state].ClusterRoleBinding

	found := &rbacv1.ClusterRoleBinding{}
	logger := log.WithValues("ClusterRoleBinding", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.ins, obj, n.rec.scheme); err != nil {
		return NotReady, err
	}

	logger.Info("Looking for")
	err := n.rec.client.Get(context.TODO(), types.NamespacedName{Namespace: "", Name: obj.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Not found, creating")
		err = n.rec.client.Create(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return NotReady, err
		}
		return Ready, nil
	} else if err != nil {
		return NotReady, err
	}

	logger.Info("Found")

	return Ready, nil
}

func ConfigMap(n SRO) (ResourceStatus, error) {

	state := n.idx
	obj := &n.resources[state].ConfigMap

	found := &corev1.ConfigMap{}
	logger := log.WithValues("ConfigMap", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.ins, obj, n.rec.scheme); err != nil {
		return NotReady, err
	}

	logger.Info("Looking for")
	err := n.rec.client.Get(context.TODO(), types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Not found, creating")
		err = n.rec.client.Create(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return NotReady, err
		}
		return Ready, nil
	} else if err != nil {
		return NotReady, err
	}

	logger.Info("Found")

	return Ready, nil
}

func kernelFullVersion(n SRO) (string, string) {

	logger := log.WithValues("Request.Namespace", "default", "Request.Name", "Node")
	// We need the node labels to fetch the correct container
	opts := &client.ListOptions{}
	opts.SetLabelSelector("feature.node.kubernetes.io/pci-10de.present=true")
	list := &corev1.NodeList{}
	err := n.rec.client.List(context.TODO(), opts, list)
	if err != nil {
		logger.Info("Could not get NodeList", "ERROR", err)
		return "", ""
	}

	if len(list.Items) == 0 {
		// none of the nodes matched a pci-10de label
		// either the nodes do not have GPUs, or NFD is not running
		logger.Info("Could not get any nodes to match pci-0302_10de.present=true label", "ERROR", "")
		return "", ""
	}

	// Assuming all nodes are running the same kernel version,
	// One could easily add driver-kernel-versions for each node.
	node := list.Items[0]
	labels := node.GetLabels()

	var ok bool
	kernelFullVersion, ok := labels["feature.node.kubernetes.io/kernel-version.full"]
	if ok {
		logger.Info(kernelFullVersion)
	} else {
		err := errors.NewNotFound(schema.GroupResource{Group: "Node", Resource: "Label"},
			"feature.node.kubernetes.io/kernel-version.full")
		logger.Info("Couldn't get kernelVersion", err)
		return "", ""
	}

	osName, ok := labels["feature.node.kubernetes.io/system-os_release.ID"]
	if !ok {
		return kernelFullVersion, ""
	}
	osVersion, ok := labels["feature.node.kubernetes.io/system-os_release.VERSION_ID"]
	if !ok {
		return kernelFullVersion, ""
	}
	osTag := fmt.Sprintf("%s%s", osName, osVersion)

	return kernelFullVersion, osTag
}

func getDriver() string {
	driver := os.Getenv("NVIDIA_DRIVER")
	if driver == "" {
		log.Info(fmt.Sprintf("ERROR: Could not find environment variable NVIDIA_DRIVER"))
		os.Exit(1)
	}
	return driver
}

func getToolkit() string {
	toolkit := os.Getenv("NVIDIA_TOOLKIT")
	if toolkit == "" {
		log.Info(fmt.Sprintf("ERROR: Could not find environment variable NVIDIA_TOOLKIT"))
		os.Exit(1)
	}
	return toolkit
}

func getDevicePlugin() string {
	devicePlugin := os.Getenv("NVIDIA_DEVICE_PLUGIN")
	if devicePlugin == "" {
		log.Info(fmt.Sprintf("ERROR: Could not find environment variable NVIDIA_DEVICE_PLUGIN"))
		os.Exit(1)
	}
	return devicePlugin
}

func getRuntimeValue() string {
	runtime := os.Getenv("NVIDIA_TOOLKIT_DEFAULT_RUNTIME")
	if runtime == "" {
		log.Info(fmt.Sprintf("ERROR: Could not find environment variable NVIDIA_TOOLKIT_DEFAULT_RUNTIME"))
		os.Exit(1)
	}
	return runtime
}

func getDcgmExporter() string {
	dcgmExporter := os.Getenv("NVIDIA_DCGM_EXPORTER")
	if dcgmExporter == "" {
		log.Info(fmt.Sprintf("ERROR: Could not find environment variable NVIDIA_DCGM_EXPORTER"))
		os.Exit(1)
	}
	return dcgmExporter
}

func getDcgmPodExporter() string {
	dcgmPodExporter := os.Getenv("NVIDIA_DCGM_POD_EXPORTER")
	if dcgmPodExporter == "" {
		log.Info(fmt.Sprintf("ERROR: Could not find environment variable NVIDIA_DCGM_POD_EXPORTER"))
		os.Exit(1)
	}
	return dcgmPodExporter
}

func preProcessDaemonSet(obj *appsv1.DaemonSet, n SRO) {
	if obj.Name == "nvidia-driver-daemonset" {
		kernelVersion, osTag := kernelFullVersion(n)
		if osTag != "" {
			img := fmt.Sprintf("%s-%s", getDriver(), osTag)
			obj.Spec.Template.Spec.Containers[0].Image = img
			if osTag == "rhel" {
				entitlementPath := "/etc/pki/entitlements"
				if _, err := os.Stat(entitlementPath); os.IsNotExist(err) {
					log.Info(fmt.Sprintf("ERROR: Could not find RedHat entitlements at %s", entitlementPath))
					os.Exit(1)
				}
				volName, volSecretName := "openshift-entitlements", "entitlement"
				volMount := corev1.VolumeMount{Name: volName, ReadOnly: true, MountPath: entitlementPath}
				obj.Spec.Template.Spec.Containers[0].VolumeMounts = append(obj.Spec.Template.Spec.Containers[0].VolumeMounts, volMount)

				vol := corev1.Volume{Name: volName, VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: volSecretName}}}
				obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, vol)
			}
		}
		sel := "feature.node.kubernetes.io/kernel-version.full"
		obj.Spec.Template.Spec.NodeSelector[sel] = kernelVersion
	} else if obj.Name == "nvidia-container-toolkit-daemonset" {
		obj.Spec.Template.Spec.Containers[0].Image = getToolkit()
		runtime := getRuntimeValue()

		setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), "RUNTIME", runtime)
		if runtime == "docker" {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), "RUNTIME_ARGS",
				"--socket /var/run/docker.sock")
		}
	} else if obj.Name == "nvidia-device-plugin-daemonset" {
		obj.Spec.Template.Spec.Containers[0].Image = getDevicePlugin()
	} else if obj.Name == "nvidia-dcgm-exporter" {
		obj.Spec.Template.Spec.Containers[0].Image = getDcgmPodExporter()
		obj.Spec.Template.Spec.Containers[1].Image = getDcgmExporter()
	}
}

func setContainerEnv(c *corev1.Container, key, value string) {
	for i, val := range c.Env {
		if val.Name != key {
			continue
		}

		c.Env[i].Value = value
		return
	}

	log.Info(fmt.Sprintf("Info: Could not find environment variable %s in container %s, appending it", key, c.Name))
	c.Env = append(c.Env, corev1.EnvVar{Name: key, Value: value})
}

func isDaemonSetReady(name string, n SRO) ResourceStatus {

	opts := &client.ListOptions{}
	opts.SetLabelSelector(fmt.Sprintf("app=%s", name))
	log.Info("DEBUG: DaemonSet", "LabelSelector", fmt.Sprintf("app=%s", name))
	list := &appsv1.DaemonSetList{}
	err := n.rec.client.List(context.TODO(), opts, list)
	if err != nil {
		log.Info("Could not get DaemonSetList", err)
	}
	log.Info("DEBUG: DaemonSet", "NumberOfDaemonSets", len(list.Items))
	if len(list.Items) == 0 {
		return NotReady
	}

	ds := list.Items[0]
	log.Info("DEBUG: DaemonSet", "NumberUnavailable", ds.Status.NumberUnavailable)

	if ds.Status.NumberUnavailable != 0 {
		return NotReady
	}

	return isPodReady(name, n, "Running")
}

func DaemonSet(n SRO) (ResourceStatus, error) {

	state := n.idx
	obj := &n.resources[state].DaemonSet

	preProcessDaemonSet(obj, n)

	found := &appsv1.DaemonSet{}
	logger := log.WithValues("DaemonSet", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.ins, obj, n.rec.scheme); err != nil {
		return NotReady, err
	}

	logger.Info("Looking for")
	err := n.rec.client.Get(context.TODO(), types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Not found, creating")
		err = n.rec.client.Create(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return NotReady, err
		}
		return isDaemonSetReady(obj.Name, n), nil
	} else if err != nil {
		return NotReady, err
	}

	logger.Info("Found")

	return isDaemonSetReady(obj.Name, n), nil
}

// The operator starts two pods in different stages to validate
// the correct working of the DaemonSets (driver and dp). Therefore
// the operator waits until the Pod completes and checks the error status
// to advance to the next state.
func isPodReady(name string, n SRO, phase corev1.PodPhase) ResourceStatus {
	opts := &client.ListOptions{}
	opts.SetLabelSelector(fmt.Sprintf("app=%s", name))
	log.Info("DEBUG: Pod", "LabelSelector", fmt.Sprintf("app=%s", name))
	list := &corev1.PodList{}
	err := n.rec.client.List(context.TODO(), opts, list)
	if err != nil {
		log.Info("Could not get PodList", err)
	}
	log.Info("DEBUG: Pod", "NumberOfPods", len(list.Items))
	if len(list.Items) == 0 {
		return NotReady
	}

	pd := list.Items[0]

	if pd.Status.Phase != phase {
		log.Info("DEBUG: Pod", "Phase", pd.Status.Phase, "!=", phase)
		return NotReady
	}
	log.Info("DEBUG: Pod", "Phase", pd.Status.Phase, "==", phase)
	return Ready
}

func Pod(n SRO) (ResourceStatus, error) {

	state := n.idx
	obj := &n.resources[state].Pod

	found := &corev1.Pod{}
	logger := log.WithValues("Pod", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.ins, obj, n.rec.scheme); err != nil {
		return NotReady, err
	}

	logger.Info("Looking for")
	err := n.rec.client.Get(context.TODO(), types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Not found, creating")
		err = n.rec.client.Create(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return NotReady, err
		}
		return isPodReady(obj.Name, n, "Succeeded"), nil
	} else if err != nil {
		return NotReady, err
	}

	logger.Info("Found")

	return isPodReady(obj.Name, n, "Succeeded"), nil
}

func SecurityContextConstraints(n SRO) (ResourceStatus, error) {

	state := n.idx
	obj := &n.resources[state].SecurityContextConstraints

	found := &secv1.SecurityContextConstraints{}
	logger := log.WithValues("SecurityContextConstraints", obj.Name, "Namespace", "default")

	if err := controllerutil.SetControllerReference(n.ins, obj, n.rec.scheme); err != nil {
		return NotReady, err
	}

	logger.Info("Looking for")
	err := n.rec.client.Get(context.TODO(), types.NamespacedName{Namespace: "", Name: obj.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Not found, creating")
		err = n.rec.client.Create(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return NotReady, err
		}
		return Ready, nil
	} else if err != nil {
		return NotReady, err
	}

	logger.Info("Found")

	return Ready, nil
}

func Service(n SRO) (ResourceStatus, error) {

	state := n.idx
	obj := &n.resources[state].Service

	found := &corev1.Service{}
	logger := log.WithValues("Service", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.ins, obj, n.rec.scheme); err != nil {
		return NotReady, err
	}

	logger.Info("Looking for")
	err := n.rec.client.Get(context.TODO(), types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Not found, creating")
		err = n.rec.client.Create(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return NotReady, err
		}
		return Ready, nil
	} else if err != nil {
		return NotReady, err
	}

	logger.Info("Found")

	return Ready, nil
}

func ServiceMonitor(n SRO) (ResourceStatus, error) {

	state := n.idx
	obj := &n.resources[state].ServiceMonitor

	found := &promv1.ServiceMonitor{}
	logger := log.WithValues("ServiceMonitor", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.ins, obj, n.rec.scheme); err != nil {
		return NotReady, err
	}

	logger.Info("Looking for")
	err := n.rec.client.Get(context.TODO(), types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Not found, creating")
		err = n.rec.client.Create(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return NotReady, err
		}
		return Ready, nil
	} else if err != nil {
		return NotReady, err
	}

	logger.Info("Found")

	return Ready, nil
}

func PriorityClass(n SRO) (ResourceStatus, error) {

	state := n.idx
	obj := &n.resources[state].PriorityClass

	found := &schedv1.PriorityClass{}
	logger := log.WithValues("PriorityClass", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.ins, obj, n.rec.scheme); err != nil {
		return NotReady, err
	}

	logger.Info("Looking for")
	err := n.rec.client.Get(context.TODO(), types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Not found, creating")
		err = n.rec.client.Create(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return NotReady, err
		}
		return Ready, nil
	} else if err != nil {
		return NotReady, err
	}

	logger.Info("Found")

	return Ready, nil
}

func Taint(n SRO) (ResourceStatus, error) {

	state := n.idx
	obj := &n.resources[state].Taint

	logger := log.WithValues("Taint", obj.Key, "Namespace", "default")

	logger.Info("Looking for")
	opts := &client.ListOptions{}
	opts.SetLabelSelector("feature.node.kubernetes.io/pci-10de.present=true")
	list := &corev1.NodeList{}
	err := n.rec.client.List(context.TODO(), opts, list)
	if err != nil {
		logger.Info("Could not get NodeList", "ERROR", err)
	}

	for _, node := range list.Items {
		if gotTaint(n, obj, node) {
			logger.Info("Found")
			return Ready, nil
		}
		logger.Info("Not found, creating")
		err := setTaint(n, *obj, node)
		if err != nil {
			logger.Info("Could not set Taint", "ERROR", err)
			return NotReady, nil
		}
	}
	return Ready, nil
}

func gotTaint(n SRO, taint *corev1.Taint, node corev1.Node) bool {
	for _, existing := range node.Spec.Taints {
		if existing.Key == taint.Key {
			return true
		}
	}
	return false
}

func setTaint(n SRO, t corev1.Taint, node corev1.Node) error {
	node.Spec.Taints = append(node.Spec.Taints, t)
	update, err := n.clientset.CoreV1().Nodes().Update(&node)
	if err != nil || update == nil {
		return err
	}
	return nil
}
