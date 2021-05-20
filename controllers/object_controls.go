package controllers

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/v1"
	apiconfigv1 "github.com/openshift/api/config/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// DefaultContainerdConfigFile indicates default config file path for containerd
	DefaultContainerdConfigFile = "/etc/containerd/config.toml"
	// DefaultContainerdSocketFile indicates default containerd socket file
	DefaultContainerdSocketFile = "/run/containerd/containerd.sock"
	// DefaultDockerConfigFile indicates default config file path for docker
	DefaultDockerConfigFile = "/etc/docker/daemon.json"
	// DefaultDockerSocketFile indicates default docker socket file
	DefaultDockerSocketFile = "/var/run/docker.sock"
	// TrustedCAConfigMapName indicates configmap with custom user CA injected
	TrustedCAConfigMapName = "gpu-operator-trusted-ca"
	// TrustedCABundleFileName indicates custom user ca certificate filename
	TrustedCABundleFileName = "ca-bundle.crt"
	// TrustedCABundleMountDir indicates target mount directory of user ca bundle
	TrustedCABundleMountDir = "/etc/pki/ca-trust/extracted/pem"
	// TrustedCACertificate indicates injected CA certificate name
	TrustedCACertificate = "tls-ca-bundle.pem"
	// VGPULicensingConfigMountPath indicates target mount path for vGPU licensing configuration file
	VGPULicensingConfigMountPath = "/drivers/gridd.conf"
	// VGPULicensingFileName is the vGPU licensing configuration filename
	VGPULicensingFileName = "gridd.conf"
	// DefaultRuntimeClass represents "nvidia" RuntimeClass
	DefaultRuntimeClass = "nvidia"
	// NvidiaDriverRootEnvName represents env name for indicating root directory of driver installation
	NvidiaDriverRootEnvName = "NVIDIA_DRIVER_ROOT"
	// DriverInstallPathVolName represents volume name for driver install path provided to toolkit
	DriverInstallPathVolName = "driver-install-path"
	// DefaultRuntimeSocketTargetDir represents target directory where runtime socket dirctory will be mounted
	DefaultRuntimeSocketTargetDir = "/runtime/sock-dir/"
	// DefaultRuntimeConfigTargetDir represents target directory where runtime socket dirctory will be mounted
	DefaultRuntimeConfigTargetDir = "/runtime/config-dir/"
	// ValidatorImageEnvName indicates env name for validator image passed
	ValidatorImageEnvName = "VALIDATOR_IMAGE"
	// ValidatorImagePullPolicyEnvName indicates env name for validator image pull policy passed
	ValidatorImagePullPolicyEnvName = "VALIDATOR_IMAGE_PULL_POLICY"
	// ValidatorImagePullSecretsEnvName indicates env name for validator image pull secrets passed
	ValidatorImagePullSecretsEnvName = "VALIDATOR_IMAGE_PULL_SECRETS"
	// ValidatorRuntimeClassEnvName indicates env name for validator image pull secrets passed
	ValidatorRuntimeClassEnvName = "VALIDATOR_RUNTIMECLASS"
	// MigStrategyEnvName indicates env name for passing MIG strategy
	MigStrategyEnvName = "MIG_STRATEGY"
)

type controlFunc []func(n ClusterPolicyController) (gpuv1.State, error)

// ServiceAccount creates ServiceAccount resource
func ServiceAccount(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].ServiceAccount.DeepCopy()
	logger := n.rec.Log.WithValues("ServiceAccount", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

// Role creates Role resource
func Role(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].Role.DeepCopy()
	logger := n.rec.Log.WithValues("Role", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

// RoleBinding creates RoleBinding resource
func RoleBinding(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].RoleBinding.DeepCopy()
	logger := n.rec.Log.WithValues("RoleBinding", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

// ClusterRole creates ClusterRole resource
func ClusterRole(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].ClusterRole.DeepCopy()
	logger := n.rec.Log.WithValues("ClusterRole", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

// ClusterRoleBinding creates ClusterRoleBinding resource
func ClusterRoleBinding(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].ClusterRoleBinding.DeepCopy()
	logger := n.rec.Log.WithValues("ClusterRoleBinding", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

// ConfigMap creates ConfigMap resource
func ConfigMap(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].ConfigMap.DeepCopy()
	logger := n.rec.Log.WithValues("ConfigMap", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
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
	logger := n.rec.Log.WithValues("Request.Namespace", "default", "Request.Name", "Node")
	// We need the node labels to fetch the correct container
	opts := []client.ListOption{
		client.MatchingLabels{"nvidia.com/gpu.present": "true"},
	}

	list := &corev1.NodeList{}
	err := n.rec.Client.List(context.TODO(), list, opts...)
	if err != nil {
		logger.Info("Could not get NodeList", "ERROR", err)
		return "", "", ""
	}

	if len(list.Items) == 0 {
		// none of the nodes matched nvidia GPU label
		// either the nodes do not have GPUs, or NFD is not running
		logger.Info("Could not get any nodes to match nvidia.com/gpu.present label", "ERROR", "")
		return "", "", ""
	}

	// Assuming all nodes are running the same kernel version,
	// One could easily add driver-kernel-versions for each node.
	node := list.Items[0]
	labels := node.GetLabels()

	var ok bool
	kFVersion, ok := labels["feature.node.kubernetes.io/kernel-version.full"]
	if ok {
		logger.Info(kFVersion)
	} else {
		err := errors.NewNotFound(schema.GroupResource{Group: "Node", Resource: "Label"},
			"feature.node.kubernetes.io/kernel-version.full")
		logger.Info("Couldn't get kernelVersion, did you run the node feature discovery?", err)
		return "", "", ""
	}

	osName, ok := labels["feature.node.kubernetes.io/system-os_release.ID"]
	if !ok {
		return kFVersion, "", ""
	}
	osVersion, ok := labels["feature.node.kubernetes.io/system-os_release.VERSION_ID"]
	if !ok {
		return kFVersion, "", ""
	}
	osTag := fmt.Sprintf("%s%s", osName, osVersion)

	return kFVersion, osTag, osVersion
}

func preProcessDaemonSet(obj *appsv1.DaemonSet, n ClusterPolicyController) error {
	logger := n.rec.Log.WithValues("Daemonset", obj.Name)
	transformations := map[string]func(*appsv1.DaemonSet, *gpuv1.ClusterPolicySpec, ClusterPolicyController) error{
		"nvidia-driver-daemonset":            TransformDriver,
		"nvidia-container-toolkit-daemonset": TransformToolkit,
		"nvidia-device-plugin-daemonset":     TransformDevicePlugin,
		"nvidia-dcgm-exporter":               TransformDCGMExporter,
		"gpu-feature-discovery":              TransformGPUDiscoveryPlugin,
		"nvidia-mig-manager":                 TransformMIGManager,
		"nvidia-operator-validator":          TransformValidator,
	}

	t, ok := transformations[obj.Name]
	if !ok {
		logger.Info(fmt.Sprintf("No transformation for Daemonset '%s'", obj.Name))
		return nil
	}

	err := t(obj, &n.singleton.Spec, n)
	if err != nil {
		logger.Info(fmt.Sprintf("Failed to apply transformation '%s' with error: '%v'", obj.Name, err))
		return err
	}

	return nil
}

// TransformGPUDiscoveryPlugin transforms GPU discovery daemonset with required config as per ClusterPolicy
func TransformGPUDiscoveryPlugin(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update validation container
	updateValidationInitContainer(obj, config)

	// update image
	img, err := gpuv1.ImagePath(&config.GPUFeatureDiscovery)
	if err != nil {
		return err
	}
	obj.Spec.Template.Spec.Containers[0].Image = img

	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.GPUFeatureDiscovery.ImagePullPolicy)

	// set image pull secrets
	if len(config.GPUFeatureDiscovery.ImagePullSecrets) > 0 {
		for _, secret := range config.GPUFeatureDiscovery.ImagePullSecrets {
			obj.Spec.Template.Spec.ImagePullSecrets = append(obj.Spec.Template.Spec.ImagePullSecrets, v1.LocalObjectReference{Name: secret})
		}
	}

	// update PriorityClass
	if config.GPUFeatureDiscovery.PriorityClassName != "" {
		obj.Spec.Template.Spec.PriorityClassName = config.GPUFeatureDiscovery.PriorityClassName
	}

	// set node selector if specified
	if len(config.GPUFeatureDiscovery.NodeSelector) > 0 {
		obj.Spec.Template.Spec.NodeSelector = config.GPUFeatureDiscovery.NodeSelector
	}

	// set node affinity if specified
	if config.GPUFeatureDiscovery.Affinity != nil {
		obj.Spec.Template.Spec.Affinity = config.GPUFeatureDiscovery.Affinity
	}

	// set tolerations if specified
	if len(config.GPUFeatureDiscovery.Tolerations) > 0 {
		obj.Spec.Template.Spec.Tolerations = config.GPUFeatureDiscovery.Tolerations
	}

	// set resource limits
	if config.GPUFeatureDiscovery.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources = *config.GPUFeatureDiscovery.Resources
		}
	}

	// set arguments if specified for driver container
	if len(config.GPUFeatureDiscovery.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.GPUFeatureDiscovery.Args
	}

	// set/append environment variables for exporter container
	if len(config.GPUFeatureDiscovery.Env) > 0 {
		for _, env := range config.GPUFeatureDiscovery.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}

	// set RuntimeClass for supported runtimes
	setRuntimeClass(&obj.Spec.Template.Spec, config.Operator.DefaultRuntime)

	// update env required for MIG support
	applyMIGConfiguration(&(obj.Spec.Template.Spec.Containers[0]), config.MIG.Strategy, true)

	return nil
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

// TransformDriver transforms Nvidia driver daemonset with required config as per ClusterPolicy
func TransformDriver(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	logger := n.rec.Log.WithValues("Daemonset", obj.Name)
	kvers, osTag, _ := kernelFullVersion(n)
	if kvers == "" {
		return fmt.Errorf("ERROR: Could not find kernel full version: ('%s', '%s')", kvers, osTag)
	}

	img, err := gpuv1.ImagePath(&config.Driver)
	if err != nil {
		return err
	}
	// if image digest is specified, use it directly
	if !strings.HasPrefix(config.Driver.Version, "sha256:") {
		// append os-tag to the provided driver version
		img = fmt.Sprintf("%s-%s", img, osTag)
	}
	obj.Spec.Template.Spec.Containers[0].Image = img

	// update PriorityClass
	if config.Driver.PriorityClassName != "" {
		obj.Spec.Template.Spec.PriorityClassName = config.Driver.PriorityClassName
	}
	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.Driver.ImagePullPolicy)

	// set image pull secrets
	if len(config.Driver.ImagePullSecrets) > 0 {
		for _, secret := range config.Driver.ImagePullSecrets {
			obj.Spec.Template.Spec.ImagePullSecrets = append(obj.Spec.Template.Spec.ImagePullSecrets, v1.LocalObjectReference{Name: secret})
		}
	}
	// set node selector if specified
	if len(config.Driver.NodeSelector) > 0 {
		obj.Spec.Template.Spec.NodeSelector = config.Driver.NodeSelector
	}
	// set node affinity if specified
	if config.Driver.Affinity != nil {
		obj.Spec.Template.Spec.Affinity = config.Driver.Affinity
	}
	// set tolerations if specified
	if len(config.Driver.Tolerations) > 0 {
		obj.Spec.Template.Spec.Tolerations = config.Driver.Tolerations
	}
	// set resource limits
	if config.Driver.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources = *config.Driver.Resources
		}
	}
	// set arguments if specified for driver container
	if len(config.Driver.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.Driver.Args
	}
	// set/append environment variables for exporter container
	if len(config.Driver.Env) > 0 {
		for _, env := range config.Driver.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}
	// set any custom repo configuration provided
	if config.Driver.RepoConfig != nil && config.Driver.RepoConfig.ConfigMapName != "" && config.Driver.RepoConfig.DestinationDir != "" {
		repoConfigVolMount := corev1.VolumeMount{Name: "repo-config", ReadOnly: true, MountPath: config.Driver.RepoConfig.DestinationDir}
		obj.Spec.Template.Spec.Containers[0].VolumeMounts = append(obj.Spec.Template.Spec.Containers[0].VolumeMounts, repoConfigVolMount)

		repoConfigVolumeSource := corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: config.Driver.RepoConfig.ConfigMapName,
				},
			},
		}
		repoConfigVol := corev1.Volume{Name: "repo-config", VolumeSource: repoConfigVolumeSource}
		obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, repoConfigVol)
	}

	// set any licensing configuration required
	if config.Driver.LicensingConfig != nil && config.Driver.LicensingConfig.ConfigMapName != "" {
		licensingConfigVolMount := corev1.VolumeMount{Name: "licensing-config", ReadOnly: true, MountPath: VGPULicensingConfigMountPath, SubPath: VGPULicensingFileName}
		obj.Spec.Template.Spec.Containers[0].VolumeMounts = append(obj.Spec.Template.Spec.Containers[0].VolumeMounts, licensingConfigVolMount)

		licensingConfigVolumeSource := corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: config.Driver.LicensingConfig.ConfigMapName,
				},
				Items: []corev1.KeyToPath{
					{
						Key:  VGPULicensingFileName,
						Path: VGPULicensingFileName,
					},
				},
			},
		}
		licensingConfigVol := corev1.Volume{Name: "licensing-config", VolumeSource: licensingConfigVolumeSource}
		obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, licensingConfigVol)
	}

	// Inject EUS kernel RPM's as an override to the entrypoint
	// Add Env Vars needed by nvidia-driver to enable the right releasever and rpm repo
	if !strings.Contains(osTag, "rhel") && !strings.Contains(osTag, "rhcos") {
		return nil
	}

	release, err := parseOSRelease()
	if err != nil {
		return fmt.Errorf("ERROR: failed to get os-release: %s", err)
	}

	ocpV, err := OpenshiftVersion()
	if err != nil {
		// might be RHEL node using upstream k8s, don't error out.
		logger.Info(fmt.Sprintf("ERROR: failed to get OpenShift version: %s", err))
	}

	rhelVersion := corev1.EnvVar{Name: "RHEL_VERSION", Value: release["RHEL_VERSION"]}
	ocpVersion := corev1.EnvVar{Name: "OPENSHIFT_VERSION", Value: ocpV}

	obj.Spec.Template.Spec.Containers[0].Env = append(obj.Spec.Template.Spec.Containers[0].Env, rhelVersion)
	obj.Spec.Template.Spec.Containers[0].Env = append(obj.Spec.Template.Spec.Containers[0].Env, ocpVersion)

	if ocpV != "" {
		// Automatically apply proxy settings for OCP and inject custom CA if configured by user
		// https://docs.openshift.com/container-platform/4.6/networking/configuring-a-custom-pki.html
		err = applyOCPProxySpec(n, &obj.Spec.Template.Spec)
		if err != nil {
			return err
		}

		// indicate driver container to automatically resolve OCP/RHEL versions to better handle node/cluster upgrades
		autoResolveVersion := corev1.EnvVar{Name: "RESOLVE_OCP_VERSION", Value: "true"}
		obj.Spec.Template.Spec.Containers[0].Env = append(obj.Spec.Template.Spec.Containers[0].Env, autoResolveVersion)
	}
	return nil
}

// applyOCPProxySpec applies proxy settings to podSpec
func applyOCPProxySpec(n ClusterPolicyController, podSpec *corev1.PodSpec) error {
	// Pass HTTPS_PROXY, HTTP_PROXY and NO_PROXY env if set in clusterwide proxy for OCP
	proxy, err := GetClusterWideProxy()
	if err != nil {
		return fmt.Errorf("ERROR: failed to get clusterwide proxy object: %s", err)
	}

	if proxy == nil {
		// no clusterwide proxy configured
		return nil
	}

	proxyEnv := getProxyEnv(proxy)
	if len(proxyEnv) != 0 {
		podSpec.Containers[0].Env = append(podSpec.Containers[0].Env, proxyEnv...)
	}

	// if user-ca-bundle is setup in proxy,  create a trusted-ca configmap and add volume mount
	if proxy.Spec.TrustedCA.Name == "" {
		return nil
	}

	// create trusted-ca configmap to inject custom user ca bundle into it
	_, err = getOrCreateTrustedCAConfigMap(n, TrustedCAConfigMapName)
	if err != nil {
		return err
	}

	// mount trusted-ca configmap
	podSpec.Containers[0].VolumeMounts = append(podSpec.Containers[0].VolumeMounts,
		corev1.VolumeMount{
			Name:      TrustedCAConfigMapName,
			ReadOnly:  true,
			MountPath: TrustedCABundleMountDir,
		})
	podSpec.Volumes = append(podSpec.Volumes,
		v1.Volume{
			Name: TrustedCAConfigMapName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{
						Name: TrustedCAConfigMapName,
					},
					Items: []v1.KeyToPath{
						{
							Key:  TrustedCABundleFileName,
							Path: TrustedCACertificate,
						},
					},
				},
			},
		})
	return nil
}

// getOrCreateTrustedCAConfigMap creates or returns an existing Trusted CA Bundle ConfigMap.
func getOrCreateTrustedCAConfigMap(n ClusterPolicyController, name string) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "gpu-operator-resources",
		},
		Data: map[string]string{
			TrustedCABundleFileName: "",
		},
	}

	// apply label "config.openshift.io/inject-trusted-cabundle: true", so that cert is automatically filled/updated.
	configMap.ObjectMeta.Labels = make(map[string]string)
	configMap.ObjectMeta.Labels["config.openshift.io/inject-trusted-cabundle"] = "true"

	logger := n.rec.Log.WithValues("ConfigMap", configMap.ObjectMeta.Name, "Namespace", configMap.ObjectMeta.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, configMap, n.rec.Scheme); err != nil {
		return nil, err
	}

	found := &corev1.ConfigMap{}
	err := n.rec.Client.Get(context.TODO(), types.NamespacedName{Namespace: configMap.ObjectMeta.Namespace, Name: configMap.ObjectMeta.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Not found, creating")
		err = n.rec.Client.Create(context.TODO(), configMap)
		if err != nil {
			logger.Info("Couldn't create")
			return nil, fmt.Errorf("failed to create trusted CA bundle config map %q: %s", name, err)
		}
		return configMap, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get trusted CA bundle config map %q: %s", name, err)
	}

	return found, nil
}

// get proxy env variables from cluster wide proxy in OCP
func getProxyEnv(proxyConfig *apiconfigv1.Proxy) []v1.EnvVar {
	envVars := []v1.EnvVar{}
	if proxyConfig == nil {
		return envVars
	}
	proxies := map[string]string{
		"HTTPS_PROXY": proxyConfig.Spec.HTTPSProxy,
		"HTTP_PROXY":  proxyConfig.Spec.HTTPProxy,
		"NO_PROXY":    proxyConfig.Spec.NoProxy,
	}
	for e, v := range proxies {
		if len(v) == 0 {
			continue
		}
		upperCaseEnvvar := v1.EnvVar{
			Name:  strings.ToUpper(e),
			Value: v,
		}
		lowerCaseEnvvar := v1.EnvVar{
			Name:  strings.ToLower(e),
			Value: v,
		}
		envVars = append(envVars, upperCaseEnvvar, lowerCaseEnvvar)
	}

	return envVars
}

// TransformToolkit transforms Nvidia container-toolkit daemonset with required config as per ClusterPolicy
func TransformToolkit(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {

	// update image
	image, err := gpuv1.ImagePath(&config.Toolkit)
	if err != nil {
		return err
	}
	obj.Spec.Template.Spec.Containers[0].Image = image

	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.Toolkit.ImagePullPolicy)

	// set image pull secrets
	if len(config.Toolkit.ImagePullSecrets) > 0 {
		for _, secret := range config.Toolkit.ImagePullSecrets {
			obj.Spec.Template.Spec.ImagePullSecrets = append(obj.Spec.Template.Spec.ImagePullSecrets, v1.LocalObjectReference{Name: secret})
		}
	}
	// update PriorityClass
	if config.Toolkit.PriorityClassName != "" {
		obj.Spec.Template.Spec.PriorityClassName = config.Toolkit.PriorityClassName
	}
	// set node selector if specified
	if len(config.Toolkit.NodeSelector) > 0 {
		obj.Spec.Template.Spec.NodeSelector = config.Toolkit.NodeSelector
	}
	// set node affinity if specified
	if config.Toolkit.Affinity != nil {
		obj.Spec.Template.Spec.Affinity = config.Toolkit.Affinity
	}
	// set tolerations if specified
	if len(config.Toolkit.Tolerations) > 0 {
		obj.Spec.Template.Spec.Tolerations = config.Toolkit.Tolerations
	}
	// set resource limits
	if config.Toolkit.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources = *config.Toolkit.Resources
		}
	}
	// set arguments if specified for toolkit container
	if len(config.Toolkit.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.Toolkit.Args
	}
	// set/append environment variables for toolkit container
	if len(config.Toolkit.Env) > 0 {
		for _, env := range config.Toolkit.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}

	// configure root directory of driver installation for toolkit if not already provided by user
	nvidiaDriverRoot := config.Driver.Root()
	setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), NvidiaDriverRootEnvName, nvidiaDriverRoot)

	// configure volume driver-install-path to use host root path if installed outside of operator
	for _, volume := range obj.Spec.Template.Spec.Volumes {
		if volume.Name == DriverInstallPathVolName && !config.Driver.IsDriverEnabled() {
			// set host root path as driver-install-path
			volume.HostPath.Path = "/"
			break
		}
	}

	// configure runtime
	runtime := string(config.Operator.DefaultRuntime)
	setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), "RUNTIME", runtime)

	// setup mounts for runtime config file and socket file
	if runtime == gpuv1.Docker.String() || runtime == gpuv1.Containerd.String() {
		runtimeConfigFile := getRuntimeConfigFile(&(obj.Spec.Template.Spec.Containers[0]), runtime)
		runtimeSocketFile := getRuntimeSocketFile(&(obj.Spec.Template.Spec.Containers[0]), runtime)

		sourceSocketFileName := path.Base(runtimeSocketFile)
		sourceConfigFileName := path.Base(runtimeConfigFile)

		// docker needs socket file as runtime arg
		setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), "RUNTIME_ARGS",
			"--socket "+DefaultRuntimeSocketTargetDir+sourceSocketFileName+" --config "+DefaultRuntimeConfigTargetDir+sourceConfigFileName)

		// setup config file mount
		volMountConfigName := fmt.Sprintf("%s-config", runtime)
		volMountConfig := corev1.VolumeMount{Name: volMountConfigName, MountPath: DefaultRuntimeConfigTargetDir}
		obj.Spec.Template.Spec.Containers[0].VolumeMounts = append(obj.Spec.Template.Spec.Containers[0].VolumeMounts, volMountConfig)

		configVol := corev1.Volume{Name: volMountConfigName, VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: path.Dir(runtimeConfigFile)}}}
		obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, configVol)

		// setup socket file mount
		volMountSocketName := fmt.Sprintf("%s-socket", runtime)
		volMountSocket := corev1.VolumeMount{Name: volMountSocketName, MountPath: DefaultRuntimeSocketTargetDir}
		obj.Spec.Template.Spec.Containers[0].VolumeMounts = append(obj.Spec.Template.Spec.Containers[0].VolumeMounts, volMountSocket)

		socketVol := corev1.Volume{Name: volMountSocketName, VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: path.Dir(runtimeSocketFile)}}}
		obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, socketVol)
	}

	// update init container spec for waiting on driver-initialization
	for i, initContainer := range obj.Spec.Template.Spec.InitContainers {
		// skip if not toolkit-init container
		if initContainer.Name != "driver-validation" {
			continue
		}

		// update initContainer image
		image, err := gpuv1.ImagePath(&config.Validator)
		if err != nil {
			return err
		}

		obj.Spec.Template.Spec.InitContainers[i].Image = image
		// update initContainer image pull policy
		obj.Spec.Template.Spec.InitContainers[i].ImagePullPolicy = gpuv1.ImagePullPolicy(config.Validator.ImagePullPolicy)

		// set/append environment variables for validation init container
		if len(config.Validator.Toolkit.Env) > 0 {
			for _, env := range config.Validator.Toolkit.Env {
				setContainerEnv(&(obj.Spec.Template.Spec.InitContainers[i]), env.Name, env.Value)
			}
		}
	}

	return nil
}

// TransformDevicePlugin transforms k8s-device-plugin daemonset with required config as per ClusterPolicy
func TransformDevicePlugin(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update validation container
	updateValidationInitContainer(obj, config)
	// update image
	image, err := gpuv1.ImagePath(&config.DevicePlugin)
	if err != nil {
		return err
	}
	obj.Spec.Template.Spec.Containers[0].Image = image
	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.DevicePlugin.ImagePullPolicy)
	// set image pull secrets
	if len(config.DevicePlugin.ImagePullSecrets) > 0 {
		for _, secret := range config.DevicePlugin.ImagePullSecrets {
			obj.Spec.Template.Spec.ImagePullSecrets = append(obj.Spec.Template.Spec.ImagePullSecrets, v1.LocalObjectReference{Name: secret})
		}
	}
	// update PriorityClass
	if config.DevicePlugin.PriorityClassName != "" {
		obj.Spec.Template.Spec.PriorityClassName = config.DevicePlugin.PriorityClassName
	}
	// set node selector if specified
	if len(config.DevicePlugin.NodeSelector) > 0 {
		obj.Spec.Template.Spec.NodeSelector = config.DevicePlugin.NodeSelector
	}
	// set node affinity if specified
	if config.DevicePlugin.Affinity != nil {
		obj.Spec.Template.Spec.Affinity = config.DevicePlugin.Affinity
	}
	// set tolerations if specified
	if len(config.DevicePlugin.Tolerations) > 0 {
		obj.Spec.Template.Spec.Tolerations = config.DevicePlugin.Tolerations
	}
	// set resource limits
	if config.DevicePlugin.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources = *config.DevicePlugin.Resources
		}
	}
	// set arguments if specified for device-plugin container
	if len(config.DevicePlugin.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.DevicePlugin.Args
	}
	// set/append environment variables for device-plugin container
	if len(config.DevicePlugin.Env) > 0 {
		for _, env := range config.DevicePlugin.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}
	// configure root directory of driver installation for device-plugin if not already provided by user
	nvidiaDriverRoot := config.Driver.Root()
	setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), NvidiaDriverRootEnvName, nvidiaDriverRoot)
	// set RuntimeClass for supported runtimes
	setRuntimeClass(&obj.Spec.Template.Spec, config.Operator.DefaultRuntime)
	// update env required for MIG support
	applyMIGConfiguration(&(obj.Spec.Template.Spec.Containers[0]), config.MIG.Strategy, false)
	return nil
}

// TransformDCGMExporter transforms dcgm exporter daemonset with required config as per ClusterPolicy
func TransformDCGMExporter(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update validation container
	updateValidationInitContainer(obj, config)
	// update image
	image, err := gpuv1.ImagePath(&config.DCGMExporter)
	if err != nil {
		return err
	}
	obj.Spec.Template.Spec.Containers[0].Image = image
	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.DCGMExporter.ImagePullPolicy)
	// set image pull secrets
	if len(config.DCGMExporter.ImagePullSecrets) > 0 {
		for _, secret := range config.DCGMExporter.ImagePullSecrets {
			obj.Spec.Template.Spec.ImagePullSecrets = append(obj.Spec.Template.Spec.ImagePullSecrets, v1.LocalObjectReference{Name: secret})
		}
	}
	// update PriorityClass
	if config.DCGMExporter.PriorityClassName != "" {
		obj.Spec.Template.Spec.PriorityClassName = config.DCGMExporter.PriorityClassName
	}
	// set node selector if specified
	if len(config.DCGMExporter.NodeSelector) > 0 {
		obj.Spec.Template.Spec.NodeSelector = config.DCGMExporter.NodeSelector
	}
	// set node affinity if specified
	if config.DCGMExporter.Affinity != nil {
		obj.Spec.Template.Spec.Affinity = config.DCGMExporter.Affinity
	}
	// set tolerations if specified
	if len(config.DCGMExporter.Tolerations) > 0 {
		obj.Spec.Template.Spec.Tolerations = config.DCGMExporter.Tolerations
	}
	// set resource limits
	if config.DCGMExporter.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources = *config.DCGMExporter.Resources
		}
	}
	// set arguments if specified for exporter container
	if len(config.DCGMExporter.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.DCGMExporter.Args
	}
	// set/append environment variables for exporter container
	if len(config.DCGMExporter.Env) > 0 {
		for _, env := range config.DCGMExporter.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}
	// set RuntimeClass for supported runtimes
	setRuntimeClass(&obj.Spec.Template.Spec, config.Operator.DefaultRuntime)

	kvers, osTag, _ := kernelFullVersion(n)
	if kvers == "" {
		return fmt.Errorf("ERROR: Could not find kernel full version: ('%s', '%s')", kvers, osTag)
	}

	if !strings.Contains(osTag, "rhel") && !strings.Contains(osTag, "rhcos") {
		return nil
	}

	// update init container config for per pod specific resources
	initImage, err := gpuv1.ImagePath(&config.Operator.InitContainer)
	if err != nil {
		return err
	}

	initContainer := v1.Container{}
	initContainer.Image = initImage
	initContainer.Name = "init-pod-nvidia-metrics-exporter"
	initContainer.ImagePullPolicy = gpuv1.ImagePullPolicy(config.Operator.InitContainer.ImagePullPolicy)
	initContainer.Command = []string{"/bin/entrypoint.sh"}

	// need CAP_SYS_ADMIN privileges for collecting pod specific resources
	privileged := true
	securityContext := &corev1.SecurityContext{
		Privileged: &privileged,
	}

	// Add initContainer for OCP to set proper SELinux context on /var/lib/kubelet/pod-resources
	initContainer.SecurityContext = securityContext

	volMountSockName, volMountSockPath := "pod-gpu-resources", "/var/lib/kubelet/pod-resources"
	volMountSock := corev1.VolumeMount{Name: volMountSockName, MountPath: volMountSockPath}
	initContainer.VolumeMounts = append(initContainer.VolumeMounts, volMountSock)

	volMountConfigName, volMountConfigPath, volMountConfigSubPath := "init-config", "/bin/entrypoint.sh", "entrypoint.sh"
	volMountConfig := corev1.VolumeMount{Name: volMountConfigName, ReadOnly: true, MountPath: volMountConfigPath, SubPath: volMountConfigSubPath}
	initContainer.VolumeMounts = append(initContainer.VolumeMounts, volMountConfig)

	obj.Spec.Template.Spec.InitContainers = append(obj.Spec.Template.Spec.InitContainers, initContainer)

	volMountConfigKey, volMountConfigDefaultMode := "nvidia-dcgm-exporter", int32(0700)
	initVol := corev1.Volume{Name: volMountConfigName, VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: volMountConfigKey}, DefaultMode: &volMountConfigDefaultMode}}}
	obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, initVol)

	return nil
}

// TransformMIGManager transforms MIG Manager daemonset with required config as per ClusterPolicy
func TransformMIGManager(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update validation container
	updateValidationInitContainer(obj, config)

	// update image
	image, err := gpuv1.ImagePath(&config.MIGManager)
	if err != nil {
		return err
	}
	obj.Spec.Template.Spec.Containers[0].Image = image

	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.MIGManager.ImagePullPolicy)

	// set image pull secrets
	if len(config.MIGManager.ImagePullSecrets) > 0 {
		for _, secret := range config.MIGManager.ImagePullSecrets {
			obj.Spec.Template.Spec.ImagePullSecrets = append(obj.Spec.Template.Spec.ImagePullSecrets, v1.LocalObjectReference{Name: secret})
		}
	}

	// update PriorityClass
	if config.MIGManager.PriorityClassName != "" {
		obj.Spec.Template.Spec.PriorityClassName = config.MIGManager.PriorityClassName
	}

	// set node selector if specified
	if len(config.MIGManager.NodeSelector) > 0 {
		obj.Spec.Template.Spec.NodeSelector = config.MIGManager.NodeSelector
	}

	// set node affinity if specified
	if config.MIGManager.Affinity != nil {
		obj.Spec.Template.Spec.Affinity = config.MIGManager.Affinity
	}

	// set tolerations if specified
	if len(config.MIGManager.Tolerations) > 0 {
		obj.Spec.Template.Spec.Tolerations = config.MIGManager.Tolerations
	}

	// set resource limits
	if config.MIGManager.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources = *config.MIGManager.Resources
		}
	}

	// set arguments if specified for driver container
	if len(config.MIGManager.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.MIGManager.Args
	}

	// set/append environment variables for exporter container
	if len(config.MIGManager.Env) > 0 {
		for _, env := range config.MIGManager.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}

	// set RuntimeClass for supported runtimes
	setRuntimeClass(&obj.Spec.Template.Spec, config.Operator.DefaultRuntime)

	return nil
}

// TransformValidator transforms nvidia-operator-validator daemonset with required config as per ClusterPolicy
func TransformValidator(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update image
	image, err := gpuv1.ImagePath(&config.Validator)
	if err != nil {
		return err
	}
	obj.Spec.Template.Spec.Containers[0].Image = image
	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.Validator.ImagePullPolicy)
	// set image pull secrets
	if len(config.Validator.ImagePullSecrets) > 0 {
		for _, secret := range config.Validator.ImagePullSecrets {
			obj.Spec.Template.Spec.ImagePullSecrets = append(obj.Spec.Template.Spec.ImagePullSecrets, v1.LocalObjectReference{Name: secret})
		}
	}
	// update PriorityClass
	if config.Validator.PriorityClassName != "" {
		obj.Spec.Template.Spec.PriorityClassName = config.Validator.PriorityClassName
	}
	// set node selector if specified
	if len(config.Validator.NodeSelector) > 0 {
		obj.Spec.Template.Spec.NodeSelector = config.Validator.NodeSelector
	}
	// set node affinity if specified
	if config.Validator.Affinity != nil {
		obj.Spec.Template.Spec.Affinity = config.Validator.Affinity
	}
	// set tolerations if specified
	if len(config.Validator.Tolerations) > 0 {
		obj.Spec.Template.Spec.Tolerations = config.Validator.Tolerations
	}
	// set resource limits
	if config.Validator.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources = *config.Validator.Resources
		}
	}
	// set arguments if specified for device-plugin container
	if len(config.Validator.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.Validator.Args
	}
	// set/append environment variables for device-plugin container
	if len(config.Validator.Env) > 0 {
		for _, env := range config.Validator.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}

	// set RuntimeClass for supported runtimes
	setRuntimeClass(&obj.Spec.Template.Spec, config.Operator.DefaultRuntime)

	// configure volume driver-install-path to use host root path if installed outside of operator
	for _, volume := range obj.Spec.Template.Spec.Volumes {
		if volume.Name == DriverInstallPathVolName && !config.Driver.IsDriverEnabled() {
			// set host root path as driver-install-path
			volume.HostPath.Path = "/"
			break
		}
	}

	// apply changes for individual component validators(initContainers)
	TransformValidatorComponent(config, &obj.Spec.Template.Spec, "driver")
	TransformValidatorComponent(config, &obj.Spec.Template.Spec, "toolkit")
	TransformValidatorComponent(config, &obj.Spec.Template.Spec, "cuda")
	TransformValidatorComponent(config, &obj.Spec.Template.Spec, "plugin")

	return nil
}

// TransformValidatorComponent applies changes to given validator component
func TransformValidatorComponent(config *gpuv1.ClusterPolicySpec, podSpec *corev1.PodSpec, component string) error {
	for i, initContainer := range podSpec.InitContainers {
		// skip if not component validation initContainer
		if !strings.Contains(initContainer.Name, fmt.Sprintf("%s-validation", component)) {
			continue
		}
		// update validation image
		image, err := gpuv1.ImagePath(&config.Validator)
		if err != nil {
			return err
		}
		podSpec.InitContainers[i].Image = image
		// update validation image pull policy
		if config.Validator.ImagePullPolicy != "" {
			podSpec.InitContainers[i].ImagePullPolicy = gpuv1.ImagePullPolicy(config.Validator.ImagePullPolicy)
		}
		switch component {
		case "cuda":
			// set/append environment variables for cuda-validation container
			if len(config.Validator.CUDA.Env) > 0 {
				for _, env := range config.Validator.CUDA.Env {
					setContainerEnv(&(podSpec.InitContainers[i]), env.Name, env.Value)
				}
			}
			// set additional env to indicate image, pullSecrets to spin-off cuda validation workload pod.
			setContainerEnv(&(podSpec.InitContainers[i]), ValidatorImageEnvName, image)
			setContainerEnv(&(podSpec.InitContainers[i]), ValidatorImagePullPolicyEnvName, config.Validator.ImagePullPolicy)
			var pullSecrets string
			if len(config.Validator.ImagePullSecrets) > 0 {
				pullSecrets = strings.Join(config.Validator.ImagePullSecrets, ",")
				setContainerEnv(&(podSpec.InitContainers[i]), ValidatorImagePullSecretsEnvName, pullSecrets)
			}
			if podSpec.RuntimeClassName != nil {
				setContainerEnv(&(podSpec.InitContainers[i]), ValidatorRuntimeClassEnvName, *podSpec.RuntimeClassName)
			}
		case "plugin":
			// set/append environment variables for plugin-validation container
			if len(config.Validator.Plugin.Env) > 0 {
				for _, env := range config.Validator.Plugin.Env {
					setContainerEnv(&(podSpec.InitContainers[i]), env.Name, env.Value)
				}
			}
			// set additional env to indicate image, pullSecrets to spin-off plugin validation workload pod.
			setContainerEnv(&(podSpec.InitContainers[i]), ValidatorImageEnvName, image)
			setContainerEnv(&(podSpec.InitContainers[i]), ValidatorImagePullPolicyEnvName, config.Validator.ImagePullPolicy)
			var pullSecrets string
			if len(config.Validator.ImagePullSecrets) > 0 {
				pullSecrets = strings.Join(config.Validator.ImagePullSecrets, ",")
				setContainerEnv(&(podSpec.InitContainers[i]), ValidatorImagePullSecretsEnvName, pullSecrets)
			}
			if podSpec.RuntimeClassName != nil {
				setContainerEnv(&(podSpec.InitContainers[i]), ValidatorRuntimeClassEnvName, *podSpec.RuntimeClassName)
			}
			// apply mig-strategy env to spin off plugin-validation workload pod
			setContainerEnv(&(podSpec.InitContainers[i]), MigStrategyEnvName, string(config.MIG.Strategy))
		case "driver":
			// set/append environment variables for driver-validation container
			if len(config.Validator.Driver.Env) > 0 {
				for _, env := range config.Validator.Driver.Env {
					setContainerEnv(&(podSpec.InitContainers[i]), env.Name, env.Value)
				}
			}
		case "toolkit":
			// set/append environment variables for toolkit-validation container
			if len(config.Validator.Toolkit.Env) > 0 {
				for _, env := range config.Validator.Toolkit.Env {
					setContainerEnv(&(podSpec.InitContainers[i]), env.Name, env.Value)
				}
			}
		default:
			return fmt.Errorf("invalid component provided to apply validator changes")
		}
	}
	return nil
}

// get runtime(docker, containerd) config file path based on toolkit container env or default
func getRuntimeConfigFile(c *corev1.Container, runtime string) (runtimeConfigFile string) {
	if runtime == gpuv1.Docker.String() {
		runtimeConfigFile = DefaultDockerConfigFile
		if getContainerEnv(c, "DOCKER_CONFIG") != "" {
			runtimeConfigFile = getContainerEnv(c, "DOCKER_CONFIG")
		}
	} else if runtime == gpuv1.Containerd.String() {
		runtimeConfigFile = DefaultContainerdConfigFile
		if getContainerEnv(c, "CONTAINERD_CONFIG") != "" {
			runtimeConfigFile = getContainerEnv(c, "CONTAINERD_CONFIG")
		}
	}
	return runtimeConfigFile
}

// get runtime(docker, containerd) socket file path based on toolkit container env or default
func getRuntimeSocketFile(c *corev1.Container, runtime string) (runtimeSocketFile string) {
	if runtime == gpuv1.Docker.String() {
		runtimeSocketFile = DefaultDockerSocketFile
		if getContainerEnv(c, "DOCKER_SOCKET") != "" {
			runtimeSocketFile = getContainerEnv(c, "DOCKER_SOCKET")
		}
	} else if runtime == gpuv1.Containerd.String() {
		runtimeSocketFile = DefaultContainerdSocketFile
		if getContainerEnv(c, "CONTAINERD_SOCKET") != "" {
			runtimeSocketFile = getContainerEnv(c, "CONTAINERD_SOCKET")
		}
	}
	return runtimeSocketFile
}

func getContainerEnv(c *corev1.Container, key string) string {
	for _, val := range c.Env {
		if val.Name == key {
			return val.Value
		}
	}
	return ""
}

func setContainerEnv(c *corev1.Container, key, value string) {
	for i, val := range c.Env {
		if val.Name != key {
			continue
		}

		c.Env[i].Value = value
		return
	}
	c.Env = append(c.Env, corev1.EnvVar{Name: key, Value: value})
}

func setRuntimeClass(podSpec *corev1.PodSpec, runtime gpuv1.Runtime) {
	if runtime == gpuv1.Containerd {
		nvidiaRuntimeClass := DefaultRuntimeClass
		podSpec.RuntimeClassName = &nvidiaRuntimeClass
	}
}

// applies MIG related configuration env to container spec
func applyMIGConfiguration(c *corev1.Container, strategy gpuv1.MIGStrategy, isGFD bool) {
	// if not set then default to "none" strategy
	if strategy == "" {
		strategy = gpuv1.MIGStrategyNone
	}

	if isGFD {
		// this is temporary until we align env name for GFD with device-plugin
		setContainerEnv(c, "GFD_MIG_STRATEGY", string(strategy))
	} else {
		setContainerEnv(c, "MIG_STRATEGY", string(strategy))
	}
	if strategy != gpuv1.MIGStrategyNone {
		setContainerEnv(c, "NVIDIA_MIG_MONITOR_DEVICES", "all")
	}
}

func updateValidationInitContainer(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec) error {
	for i, initContainer := range obj.Spec.Template.Spec.InitContainers {
		// skip if not validation initContainer
		if initContainer.Name != "toolkit-validation" {
			continue
		}
		// update validation image
		image, err := gpuv1.ImagePath(&config.Validator)
		if err != nil {
			return err
		}
		obj.Spec.Template.Spec.InitContainers[i].Image = image
		// update validation image pull policy
		if config.Validator.ImagePullPolicy != "" {
			obj.Spec.Template.Spec.InitContainers[i].ImagePullPolicy = gpuv1.ImagePullPolicy(config.Validator.ImagePullPolicy)
		}
	}
	return nil
}

func isDeploymentReady(name string, n ClusterPolicyController) gpuv1.State {
	opts := []client.ListOption{
		client.MatchingLabels{"app": name},
	}
	n.rec.Log.Info("DEBUG: Deployment", "LabelSelector", fmt.Sprintf("app=%s", name))
	list := &appsv1.DeploymentList{}
	err := n.rec.Client.List(context.TODO(), list, opts...)
	if err != nil {
		n.rec.Log.Info("Could not get DeploymentList", err)
	}
	n.rec.Log.Info("DEBUG: Deployment", "NumberOfDeployment", len(list.Items))
	if len(list.Items) == 0 {
		return gpuv1.NotReady
	}

	ds := list.Items[0]
	n.rec.Log.Info("DEBUG: Deployment", "NumberUnavailable", ds.Status.UnavailableReplicas)

	if ds.Status.UnavailableReplicas != 0 {
		return gpuv1.NotReady
	}

	return isPodReady(name, n, "Running")
}

func isDaemonSetReady(name string, n ClusterPolicyController) gpuv1.State {
	opts := []client.ListOption{
		client.MatchingLabels{"app": name},
	}
	n.rec.Log.Info("DEBUG: DaemonSet", "LabelSelector", fmt.Sprintf("app=%s", name))
	list := &appsv1.DaemonSetList{}
	err := n.rec.Client.List(context.TODO(), list, opts...)
	if err != nil {
		n.rec.Log.Info("Could not get DaemonSetList", err)
	}
	n.rec.Log.Info("DEBUG: DaemonSet", "NumberOfDaemonSets", len(list.Items))
	if len(list.Items) == 0 {
		return gpuv1.NotReady
	}

	ds := list.Items[0]
	n.rec.Log.Info("DEBUG: DaemonSet", "NumberUnavailable", ds.Status.NumberUnavailable)

	if ds.Status.NumberUnavailable != 0 {
		return gpuv1.NotReady
	}

	return gpuv1.Ready
}

// Deployment creates Deployment resource
func Deployment(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].Deployment.DeepCopy()

	logger := n.rec.Log.WithValues("Deployment", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return isDeploymentReady(obj.Name, n), nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return isDeploymentReady(obj.Name, n), nil
}

// DaemonSet creates Daemonset resource
func DaemonSet(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].DaemonSet.DeepCopy()

	logger := n.rec.Log.WithValues("DaemonSet", obj.Name, "Namespace", obj.Namespace)

	err := preProcessDaemonSet(obj, n)
	if err != nil {
		logger.Info("Could not pre-process", "Error", err)
		return gpuv1.NotReady, err
	}

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return isDaemonSetReady(obj.Name, n), nil
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

	n.rec.Log.Info("DEBUG: Pod", "LabelSelector", fmt.Sprintf("app=%s", name))
	list := &corev1.PodList{}
	err := n.rec.Client.List(context.TODO(), list, opts...)
	if err != nil {
		n.rec.Log.Info("Could not get PodList", err)
	}
	n.rec.Log.Info("DEBUG: Pod", "NumberOfPods", len(list.Items))
	if len(list.Items) == 0 {
		return gpuv1.NotReady
	}

	pd := list.Items[0]

	if pd.Status.Phase != phase {
		n.rec.Log.Info("DEBUG: Pod", "Phase", pd.Status.Phase, "!=", phase)
		return gpuv1.NotReady
	}
	n.rec.Log.Info("DEBUG: Pod", "Phase", pd.Status.Phase, "==", phase)
	return gpuv1.Ready
}

// SecurityContextConstraints creates SCC resources
func SecurityContextConstraints(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].SecurityContextConstraints.DeepCopy()
	logger := n.rec.Log.WithValues("SecurityContextConstraints", obj.Name, "Namespace", "default")

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

// PodSecurityPolicy creates PSP resources
func PodSecurityPolicy(n ClusterPolicyController) (gpuv1.State, error) {
	// check if PSP's are disabled and ignore PSP creation
	if !n.singleton.Spec.PSP.IsEnabled() {
		return gpuv1.Ready, nil
	}

	state := n.idx
	obj := n.resources[state].PodSecurityPolicy.DeepCopy()
	logger := n.rec.Log.WithValues("PodSecurityPolicies", obj.Name)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

// Service creates Service object
func Service(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].Service.DeepCopy()
	logger := n.rec.Log.WithValues("Service", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

// ServiceMonitor creates ServiceMonitor object
func ServiceMonitor(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].ServiceMonitor.DeepCopy()
	logger := n.rec.Log.WithValues("ServiceMonitor", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

// Namespace creates Namespace object
func Namespace(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].Namespace.DeepCopy()
	logger := n.rec.Log.WithValues("Namespace", obj.Name)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

// RuntimeClass creates RuntimeClass object
func RuntimeClass(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].RuntimeClass.DeepCopy()
	logger := n.rec.Log.WithValues("RuntimeClass", obj.Name)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource")
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}
