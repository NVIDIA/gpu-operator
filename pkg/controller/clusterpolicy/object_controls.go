package clusterpolicy

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	gpuv1 "github.com/NVIDIA/gpu-operator/pkg/apis/nvidia/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type controlFunc []func(n ClusterPolicyController) (gpuv1.State, error)

func ServiceAccount(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].ServiceAccount.DeepCopy()
	logger := log.WithValues("ServiceAccount", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

func Role(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].Role.DeepCopy()
	logger := log.WithValues("Role", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

func RoleBinding(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].RoleBinding.DeepCopy()
	logger := log.WithValues("RoleBinding", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

func ClusterRole(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].ClusterRole.DeepCopy()
	logger := log.WithValues("ClusterRole", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

func ClusterRoleBinding(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].ClusterRoleBinding.DeepCopy()
	logger := log.WithValues("ClusterRoleBinding", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

func ConfigMap(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].ConfigMap.DeepCopy()
	logger := log.WithValues("ConfigMap", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

func kernelFullVersion(n ClusterPolicyController) (string, string, string) {
	logger := log.WithValues("Request.Namespace", "default", "Request.Name", "Node")
	// We need the node labels to fetch the correct container
	opts := []client.ListOption{
		client.MatchingLabels{"feature.node.kubernetes.io/pci-10de.present": "true"},
	}

	list := &corev1.NodeList{}
	err := n.rec.client.List(context.TODO(), list, opts...)
	if err != nil {
		logger.Info("Could not get NodeList", "ERROR", err)
		return "", "", ""
	}

	if len(list.Items) == 0 {
		// none of the nodes matched a pci-10de label
		// either the nodes do not have GPUs, or NFD is not running
		logger.Info("Could not get any nodes to match pci-0302_10de.present=true label", "ERROR", "")
		return "", "", ""
	}

	// Assuming all nodes are running the same kernel version,
	// One could easily add driver-kernel-versions for each node.
	node := list.Items[0]
	labels := node.GetLabels()

	var ok bool
	kvers, ok := labels["feature.node.kubernetes.io/kernel-version.full"]
	if ok {
		logger.Info(kvers)
	} else {
		err := errors.NewNotFound(schema.GroupResource{Group: "Node", Resource: "Label"},
			"feature.node.kubernetes.io/kernel-version.full")
		logger.Info("Couldn't get kernelVersion, did you run the node feature discovery?", err)
		return "", "", ""
	}

	osName, ok := labels["feature.node.kubernetes.io/system-os_release.ID"]
	if !ok {
		return kvers, "", ""
	}
	osVersion, ok := labels["feature.node.kubernetes.io/system-os_release.VERSION_ID"]
	if !ok {
		return kvers, "", ""
	}

	return kvers, osName, osVersion
}

func getDcgmExporter() string {
	dcgmExporter := os.Getenv("NVIDIA_DCGM_EXPORTER")
	if dcgmExporter == "" {
		log.Info(fmt.Sprintf("ERROR: Could not find environment variable NVIDIA_DCGM_EXPORTER"))
		os.Exit(1)
	}
	return dcgmExporter
}

func preProcessDaemonSet(obj *appsv1.DaemonSet, n ClusterPolicyController) {
	transformations := map[string]func(*appsv1.DaemonSet, *gpuv1.ClusterPolicySpec, ClusterPolicyController) error{
		"nvidia-driver-daemonset":            TransformDriver,
		"nvidia-container-toolkit-daemonset": TransformToolkit,
		"nvidia-device-plugin-daemonset":     TransformDevicePlugin,
		"nvidia-dcgm-exporter":               TransformDCGMExporter,
	}

	t, ok := transformations[obj.Name]
	if !ok {
		log.Info(fmt.Sprintf("No transformation for Daemonset '%s'", obj.Name))
		return
	}

	err := t(obj, &n.singleton.Spec, n)
	if err != nil {
		log.Info(fmt.Sprintf("Failed to apply transformation '%s' with error: '%v'", obj.Name, err))
		os.Exit(1)
	}
}

// Read and parse os-release file
func parseOSRelease() (map[string]string, error) {
	release := map[string]string{}

	f, err := os.Open("/host-etc/os-release")
	if err != nil {
		return nil, err
	}

	re := regexp.MustCompile(`^(?P<key>\w+)=(?P<value>.+)`)

	// Read line-by-line
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if m := re.FindStringSubmatch(line); m != nil {
			release[m[1]] = strings.Trim(m[2], `"`)
		}
	}

	return release, nil
}

func TransformDriver(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	kvers, osName, osVer := kernelFullVersion(n)
	if kvers == "" {
		return fmt.Errorf("ERROR: Could not find kernel full version: ('%s', '%s')", kvers, osName)
	}

	img := fmt.Sprintf("%s-%s%s", config.Driver.ImagePath(), osName, osVer)
	obj.Spec.Template.Spec.Containers[0].Image = img

	// Inject EUS kernel RPM's as an override to the entrypoint
	// Add Env Vars needed by nvidia-driver to enable the right releasever and rpm repo
	release, err := parseOSRelease()
	if err != nil {
		return fmt.Errorf("ERROR: failed to get os-release: %s", err)
	}
	rhel_version := corev1.EnvVar{Name: "RHEL_VERSION", Value: release["RHEL_VERSION"]}
	ocp_version := corev1.EnvVar{Name: "VERSION_ID", Value: osVer}

	obj.Spec.Template.Spec.Containers[0].Env = append(obj.Spec.Template.Spec.Containers[0].Env, rhel_version)
	obj.Spec.Template.Spec.Containers[0].Env = append(obj.Spec.Template.Spec.Containers[0].Env, ocp_version)
	// Overlay volume
	//	volName, overlayPath := "overlay", "/tmp/overlay"
	//	volMount := corev1.VolumeMount{
	//		Name:      volName,
	//		ReadOnly:  true,
	//		MountPath: overlayPath,
	//	}
	//	obj.Spec.Template.Spec.Containers[0].VolumeMounts = append(obj.Spec.Template.Spec.Containers[0].VolumeMounts, volMount)
	//
	//	vol := corev1.Volume{
	//		Name: volName,
	//		VolumeSource: corev1.VolumeSource{
	//			HostPath: &corev1.HostPathVolumeSource{Path: "/var/lib/containers/storage/overlay"}},
	//	}
	//	obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, vol)

	// EUS entrypoint script
	//	volName, entrypointPath, configMapName := "entrypoint", "/bin/entrypoint.sh", "nvidia-driver"
	//	configMapMode := int32(0700)
	//	volMount = corev1.VolumeMount{
	//		Name:      volName,
	//		ReadOnly:  true,
	//		MountPath: entrypointPath,
	//		SubPath:   "entrypoint.sh",
	//	}
	//	obj.Spec.Template.Spec.Containers[0].VolumeMounts = append(obj.Spec.Template.Spec.Containers[0].VolumeMounts, volMount)
	//
	//	volSourceCm := corev1.VolumeSource{
	//		ConfigMap: &corev1.ConfigMapVolumeSource{
	//			LocalObjectReference: corev1.LocalObjectReference{Name: configMapName},
	//			Items: []corev1.KeyToPath{
	//				corev1.KeyToPath{Key: "entrypoint.sh", Path: "entrypoint.sh"}},
	//			DefaultMode: &configMapMode,
	//		}}
	//	volEUS := corev1.Volume{
	//		Name:         volName,
	//		VolumeSource: volSourceCm,
	//	}
	//	obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, volEUS)
	//
	// Use the injected entrypoint script
	//	obj.Spec.Template.Spec.Containers[0].Command = []string{"/bin/entrypoint.sh"}

	return nil
}

func TransformToolkit(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	obj.Spec.Template.Spec.Containers[0].Image = config.Toolkit.ImagePath()
	runtime := string(config.Operator.DefaultRuntime)

	setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), "RUNTIME", runtime)
	if runtime == "docker" {
		setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), "RUNTIME_ARGS",
			"--socket /var/run/docker.sock")
	}

	return nil
}

func TransformDevicePlugin(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	obj.Spec.Template.Spec.Containers[0].Image = config.DevicePlugin.ImagePath()

	return nil
}

func TransformDCGMExporter(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	obj.Spec.Template.Spec.Containers[0].Image = config.DCGMExporter.ImagePath()

	kvers, osTag, _ := kernelFullVersion(n)
	if osTag == "" {
		return fmt.Errorf("ERROR: Could not find kernel full version: ('%s', '%s')", kvers, osTag)
	}

	if osTag != "rhel" {
		return nil
	}

	initContainerImage, initContainerName, initContainerCmd := "ubuntu:18.04", "init-pod-nvidia-metrics-exporter", "/bin/entrypoint.sh"
	obj.Spec.Template.Spec.InitContainers[0].Image = initContainerImage
	obj.Spec.Template.Spec.InitContainers[0].Name = initContainerName
	obj.Spec.Template.Spec.InitContainers[0].Command[0] = initContainerCmd

	volMountSockName, volMountSockPath := "pod-gpu-resources", "/var/lib/kubelet/pod-resources"
	volMountSock := corev1.VolumeMount{Name: volMountSockName, MountPath: volMountSockPath}
	obj.Spec.Template.Spec.InitContainers[0].VolumeMounts = append(obj.Spec.Template.Spec.InitContainers[0].VolumeMounts, volMountSock)

	volMountConfigName, volMountConfigPath, volMountConfigSubPath := "init-config", "/bin/entrypoint.sh", "entrypoint.sh"
	volMountConfig := corev1.VolumeMount{Name: volMountConfigName, ReadOnly: true, MountPath: volMountConfigPath, SubPath: volMountConfigSubPath}
	obj.Spec.Template.Spec.InitContainers[0].VolumeMounts = append(obj.Spec.Template.Spec.InitContainers[0].VolumeMounts, volMountConfig)

	volMountConfigKey, volMountConfigDefaultMode := "nvidia-dcgm-exporter", int32(0700)
	initVol := corev1.Volume{Name: volMountConfigName, VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: volMountConfigKey}, DefaultMode: &volMountConfigDefaultMode}}}
	obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, initVol)

	return nil
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

func isDaemonSetReady(name string, n ClusterPolicyController) gpuv1.State {
	opts := []client.ListOption{
		client.MatchingLabels{"app": name},
	}
	log.Info("DEBUG: DaemonSet", "LabelSelector", fmt.Sprintf("app=%s", name))
	list := &appsv1.DaemonSetList{}
	err := n.rec.client.List(context.TODO(), list, opts...)
	if err != nil {
		log.Info("Could not get DaemonSetList", err)
	}
	log.Info("DEBUG: DaemonSet", "NumberOfDaemonSets", len(list.Items))
	if len(list.Items) == 0 {
		return gpuv1.NotReady
	}

	ds := list.Items[0]
	log.Info("DEBUG: DaemonSet", "NumberUnavailable", ds.Status.NumberUnavailable)

	if ds.Status.NumberUnavailable != 0 {
		return gpuv1.NotReady
	}

	return isPodReady(name, n, "Running")
}

func DaemonSet(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].DaemonSet.DeepCopy()

	preProcessDaemonSet(obj, n)
	logger := log.WithValues("DaemonSet", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return isDaemonSetReady(obj.Name, n), nil
}

// The operator starts two pods in different stages to validate
// the correct working of the DaemonSets (driver and dp). Therefore
// the operator waits until the Pod completes and checks the error status
// to advance to the next state.
func isPodReady(name string, n ClusterPolicyController, phase corev1.PodPhase) gpuv1.State {
	opts := []client.ListOption{&client.MatchingLabels{"app": name}}

	log.Info("DEBUG: Pod", "LabelSelector", fmt.Sprintf("app=%s", name))
	list := &corev1.PodList{}
	err := n.rec.client.List(context.TODO(), list, opts...)
	if err != nil {
		log.Info("Could not get PodList", err)
	}
	log.Info("DEBUG: Pod", "NumberOfPods", len(list.Items))
	if len(list.Items) == 0 {
		return gpuv1.NotReady
	}

	pd := list.Items[0]

	if pd.Status.Phase != phase {
		log.Info("DEBUG: Pod", "Phase", pd.Status.Phase, "!=", phase)
		return gpuv1.NotReady
	}
	log.Info("DEBUG: Pod", "Phase", pd.Status.Phase, "==", phase)
	return gpuv1.Ready
}

func Pod(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].Pod.DeepCopy()
	logger := log.WithValues("Pod", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return isPodReady(obj.Name, n, "Succeeded"), nil
}

func SecurityContextConstraints(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].SecurityContextConstraints.DeepCopy()
	logger := log.WithValues("SecurityContextConstraints", obj.Name, "Namespace", "default")

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

func Service(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].Service.DeepCopy()
	logger := log.WithValues("Service", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

func ServiceMonitor(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].ServiceMonitor.DeepCopy()
	logger := log.WithValues("ServiceMonitor", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}
