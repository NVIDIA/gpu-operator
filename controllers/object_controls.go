package controllers

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"path/filepath"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/v1"
	"github.com/mitchellh/hashstructure"
	apiconfigv1 "github.com/openshift/api/config/v1"
	apiimagev1 "github.com/openshift/api/image/v1"
	secv1 "github.com/openshift/api/security/v1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1beta1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
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
	// NLSClientTokenMountPath inidicates the target mount path for NLS client config token file (.tok)
	NLSClientTokenMountPath = "/drivers/ClientConfigToken/client_configuration_token.tok"
	// NLSClientTokenFileName is the NLS client config token filename
	NLSClientTokenFileName = "client_configuration_token.tok"
	// VGPUTopologyConfigMountPath indicates target mount path for vGPU topology daemon configuration file
	VGPUTopologyConfigMountPath = "/etc/nvidia/nvidia-topologyd.conf"
	// VGPUTopologyConfigFileName is the vGPU topology daemon configuration filename
	VGPUTopologyConfigFileName = "nvidia-topologyd.conf"
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
	// ValidatorRuntimeClassEnvName indicates env name of runtime class to be applied to validator pods
	ValidatorRuntimeClassEnvName = "VALIDATOR_RUNTIME_CLASS"
	// MigStrategyEnvName indicates env name for passing MIG strategy
	MigStrategyEnvName = "MIG_STRATEGY"
	// MigPartedDefaultConfigMapName indicates name of ConfigMap containing default mig-parted config
	MigPartedDefaultConfigMapName = "default-mig-parted-config"
	// MigDefaultGPUClientsConfigMapName indicates name of ConfigMap containing default gpu-clients
	MigDefaultGPUClientsConfigMapName = "default-gpu-clients"
	// DCGMRemoteEngineEnvName indicates env name to specify remote DCGM host engine ip:port
	DCGMRemoteEngineEnvName = "DCGM_REMOTE_HOSTENGINE_INFO"
	// DCGMDefaultHostPort indicates default host port bound to DCGM host engine
	DCGMDefaultHostPort = 5555
	// GPUDirectRDMAEnabledEnvName indicates if GPU direct RDMA is enabled through GPU operator
	GPUDirectRDMAEnabledEnvName = "GPU_DIRECT_RDMA_ENABLED"
	// UseHostMOFEDEnvName indicates if MOFED driver is pre-installed on the host
	UseHostMOFEDEnvName = "USE_HOST_MOFED"
	// MetricsConfigMountPath indicates mount path for custom dcgm metrics file
	MetricsConfigMountPath = "/etc/dcgm-exporter/" + MetricsConfigFileName
	// MetricsConfigFileName indicates custom dcgm metrics file name
	MetricsConfigFileName = "dcgm-metrics.csv"
	// NvidiaAnnotationHashKey indicates annotation name for last applied hash by gpu-operator
	NvidiaAnnotationHashKey = "nvidia.com/last-applied-hash"
)

// RepoConfigPathMap indicates standard OS specific paths for repository configuration files
var RepoConfigPathMap = map[string]string{
	"centos": "/etc/yum.repos.d",
	"ubuntu": "/etc/apt/sources.list.d",
	"rhcos":  "/etc/yum.repos.d",
	"rhel":   "/etc/yum.repos.d",
}

// CertConfigPathMap indicates standard OS specific paths for ssl keys/certificates.
// Where Go looks for certs: https://golang.org/src/crypto/x509/root_linux.go
// Where OCP mounts proxy certs on RHCOS nodes:
// https://access.redhat.com/documentation/en-us/openshift_container_platform/4.3/html/authentication/ocp-certificates#proxy-certificates_ocp-certificates
var CertConfigPathMap = map[string]string{
	"centos": "/etc/pki/ca-trust/extracted/pem",
	"ubuntu": "/etc/ssl/certs",
	"rhcos":  "/etc/pki/ca-trust/extracted/pem",
	"rhel":   "/etc/pki/ca-trust/extracted/pem",
}

type controlFunc []func(n ClusterPolicyController) (gpuv1.State, error)

// ServiceAccount creates ServiceAccount resource
func ServiceAccount(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].ServiceAccount.DeepCopy()
	obj.Namespace = n.operatorNamespace

	logger := n.rec.Log.WithValues("ServiceAccount", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource, skipping update")
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
	obj.Namespace = n.operatorNamespace

	logger := n.rec.Log.WithValues("Role", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource, updating...")
			err = n.rec.Client.Update(context.TODO(), obj)
			if err != nil {
				logger.Info("Couldn't update", "Error", err)
				return gpuv1.NotReady, err
			}
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
	obj.Namespace = n.operatorNamespace

	logger := n.rec.Log.WithValues("RoleBinding", obj.Name, "Namespace", obj.Namespace)

	for idx := range obj.Subjects {
		// we don't want to update ALL the Subjects[].Namespace, eg we need to keep 'openshift-monitoring'
		// for allowing PrometheusOperator to scrape our metrics resources:
		// see in assets/state-dcgm-exporter, 0500_prom_rolebinding_openshift.yaml vs 0300_rolebinding.yaml
		if obj.Subjects[idx].Namespace != "FILLED BY THE OPERATOR" {
			continue
		}
		obj.Subjects[idx].Namespace = n.operatorNamespace
	}

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource, updating...")
			err = n.rec.Client.Update(context.TODO(), obj)
			if err != nil {
				logger.Info("Couldn't update", "Error", err)
				return gpuv1.NotReady, err
			}
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
	obj.Namespace = n.operatorNamespace

	logger := n.rec.Log.WithValues("ClusterRole", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource, updating...")
			err = n.rec.Client.Update(context.TODO(), obj)
			if err != nil {
				logger.Info("Couldn't update", "Error", err)
				return gpuv1.NotReady, err
			}
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
	obj.Namespace = n.operatorNamespace

	logger := n.rec.Log.WithValues("ClusterRoleBinding", obj.Name, "Namespace", obj.Namespace)

	for idx := range obj.Subjects {
		obj.Subjects[idx].Namespace = n.operatorNamespace
	}

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource, updating...")
			err = n.rec.Client.Update(context.TODO(), obj)
			if err != nil {
				logger.Info("Couldn't update", "Error", err)
				return gpuv1.NotReady, err
			}
			return gpuv1.Ready, nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return gpuv1.Ready, nil
}

// createConfigMap creates a ConfigMap resource
func createConfigMap(n ClusterPolicyController, configMapIdx int) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].ConfigMaps[configMapIdx].DeepCopy()
	obj.Namespace = n.operatorNamespace

	logger := n.rec.Log.WithValues("ConfigMap", obj.Name, "Namespace", obj.Namespace)

	// avoid creating default 'mig-parted-config' ConfigMap if custom one is provided
	if obj.Name == MigPartedDefaultConfigMapName {
		config := n.singleton.Spec
		if config.MIGManager.Config != nil && config.MIGManager.Config.Name != "" {
			logger.Info(fmt.Sprintf("Not creating resource, custom ConfigMap provided: %s", config.MIGManager.Config.Name))
			return gpuv1.Ready, nil
		}
	}

	// avoid creating default 'gpu-clients' ConfigMap if custom one is provided
	if obj.Name == MigDefaultGPUClientsConfigMapName {
		config := n.singleton.Spec
		if config.MIGManager.GPUClientsConfig != nil && config.MIGManager.GPUClientsConfig.Name != "" {
			logger.Info(fmt.Sprintf("Not creating resource, custom ConfigMap provided: %s", config.MIGManager.GPUClientsConfig.Name))
			return gpuv1.Ready, nil
		}
	}

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if !errors.IsAlreadyExists(err) {
			logger.Info("Couldn't create", "Error", err)
			return gpuv1.NotReady, err
		}

		logger.Info("Found Resource, updating...")
		err = n.rec.Client.Update(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't update", "Error", err)
			return gpuv1.NotReady, err
		}
	}

	return gpuv1.Ready, nil
}

// ConfigMaps creates ConfigMap resource(s)
func ConfigMaps(n ClusterPolicyController) (gpuv1.State, error) {
	status := gpuv1.Ready
	state := n.idx
	for i := range n.resources[state].ConfigMaps {
		stat, err := createConfigMap(n, i)
		if err != nil {
			return stat, err
		}
		if stat != gpuv1.Ready {
			status = gpuv1.NotReady
		}
	}
	return status, nil
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
	kFVersion, ok := labels[nfdKernelLabelKey]
	if ok {
		logger.Info(kFVersion)
	} else {
		err := errors.NewNotFound(schema.GroupResource{Group: "Node", Resource: "Label"}, nfdKernelLabelKey)
		logger.Info("Couldn't get kernelVersion, did you run the node feature discovery?", "Error", err)
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
		"nvidia-dcgm":                        TransformDCGM,
		"nvidia-dcgm-exporter":               TransformDCGMExporter,
		"nvidia-node-status-exporter":        TransformNodeStatusExporter,
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
	transformValidationInitContainer(obj, config)

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
	if config.Daemonsets.PriorityClassName != "" {
		obj.Spec.Template.Spec.PriorityClassName = config.Daemonsets.PriorityClassName
	}
	// set tolerations if specified
	if len(config.Daemonsets.Tolerations) > 0 {
		obj.Spec.Template.Spec.Tolerations = config.Daemonsets.Tolerations
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
	setRuntimeClass(&obj.Spec.Template.Spec, n.runtime, config.Operator.RuntimeClass)

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
	// update validation container
	transformValidationInitContainer(obj, config)

	// update driver-manager initContainer
	transformDriverManagerInitContainer(obj, config)

	// update nvidia-driver container
	transformDriverContainer(obj, config, n)

	// update nvidia-peermem sidecar container
	transformPeerMemoryContainer(obj, config, n)

	// update PriorityClass
	if config.Daemonsets.PriorityClassName != "" {
		obj.Spec.Template.Spec.PriorityClassName = config.Daemonsets.PriorityClassName
	}
	// set tolerations if specified
	if len(config.Daemonsets.Tolerations) > 0 {
		obj.Spec.Template.Spec.Tolerations = config.Daemonsets.Tolerations
	}

	// update OpenShift Driver Toolkit sidecar container
	err := transformOpenShiftDriverToolkitContainer(obj, config, n)
	if err != nil {
		return fmt.Errorf("ERROR: failed to transform the Driver Toolkit Container: %s", err)
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

	for i, container := range podSpec.Containers {
		// skip if not nvidia-driver container
		if !strings.Contains(container.Name, "nvidia-driver") {
			continue
		}

		proxyEnv := getProxyEnv(proxy)
		if len(proxyEnv) != 0 {
			podSpec.Containers[i].Env = append(podSpec.Containers[i].Env, proxyEnv...)
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
		podSpec.Containers[i].VolumeMounts = append(podSpec.Containers[i].VolumeMounts,
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
	}
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
			Namespace: n.operatorNamespace,
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
	var envs []string
	for k := range proxies {
		envs = append(envs, k)
	}
	// ensure ordering is preserved when we add these env to pod spec
	sort.Strings(envs)

	for _, e := range envs {
		v := proxies[e]
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
	// update validation container
	transformValidationInitContainer(obj, config)
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
	if config.Daemonsets.PriorityClassName != "" {
		obj.Spec.Template.Spec.PriorityClassName = config.Daemonsets.PriorityClassName
	}
	// set tolerations if specified
	if len(config.Daemonsets.Tolerations) > 0 {
		obj.Spec.Template.Spec.Tolerations = config.Daemonsets.Tolerations
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
	runtime := n.runtime.String()
	setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), "RUNTIME", runtime)

	if runtime == gpuv1.Containerd.String() {
		// Set the runtime class name that is to be configured for containerd
		setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), "CONTAINERD_RUNTIME_CLASS", getRuntimeClass(config))
	}

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
	return nil
}

// TransformDevicePlugin transforms k8s-device-plugin daemonset with required config as per ClusterPolicy
func TransformDevicePlugin(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update validation container
	transformValidationInitContainer(obj, config)
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
	if config.Daemonsets.PriorityClassName != "" {
		obj.Spec.Template.Spec.PriorityClassName = config.Daemonsets.PriorityClassName
	}
	// set tolerations if specified
	if len(config.Daemonsets.Tolerations) > 0 {
		obj.Spec.Template.Spec.Tolerations = config.Daemonsets.Tolerations
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
	setRuntimeClass(&obj.Spec.Template.Spec, n.runtime, config.Operator.RuntimeClass)
	// update env required for MIG support
	applyMIGConfiguration(&(obj.Spec.Template.Spec.Containers[0]), config.MIG.Strategy, false)
	return nil
}

// TransformDCGMExporter transforms dcgm exporter daemonset with required config as per ClusterPolicy
func TransformDCGMExporter(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update validation container
	transformValidationInitContainer(obj, config)
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
	if config.Daemonsets.PriorityClassName != "" {
		obj.Spec.Template.Spec.PriorityClassName = config.Daemonsets.PriorityClassName
	}
	// set tolerations if specified
	if len(config.Daemonsets.Tolerations) > 0 {
		obj.Spec.Template.Spec.Tolerations = config.Daemonsets.Tolerations
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
	// check if DCGM hostengine is enabled as a separate Pod and setup env accordingly
	if config.DCGM.IsEnabled() {
		// enable hostNetwork for communication with external DCGM using NODE_IP(localhost)
		obj.Spec.Template.Spec.HostNetwork = true
		// set DCGM host engine env. localhost will be substituted during pod runtime
		dcgmHostPort := int32(DCGMDefaultHostPort)
		if config.DCGM.HostPort != 0 {
			dcgmHostPort = config.DCGM.HostPort
		}
		setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), DCGMRemoteEngineEnvName, fmt.Sprintf("localhost:%d", dcgmHostPort))
	} else {
		// case for DCGM running on the host itself(DGX BaseOS)
		remoteEngine := getContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), DCGMRemoteEngineEnvName)
		if remoteEngine != "" && strings.HasPrefix(remoteEngine, "localhost") {
			// enable hostNetwork for communication with external DCGM using localhost
			obj.Spec.Template.Spec.HostNetwork = true
		}
	}
	// set RuntimeClass for supported runtimes
	setRuntimeClass(&obj.Spec.Template.Spec, n.runtime, config.Operator.RuntimeClass)

	// mount configmap for custom metrics if provided by user
	if config.DCGMExporter.MetricsConfig != nil && config.DCGMExporter.MetricsConfig.Name != "" {
		metricsConfigVolMount := corev1.VolumeMount{Name: "metrics-config", ReadOnly: true, MountPath: MetricsConfigMountPath, SubPath: MetricsConfigFileName}
		obj.Spec.Template.Spec.Containers[0].VolumeMounts = append(obj.Spec.Template.Spec.Containers[0].VolumeMounts, metricsConfigVolMount)

		metricsConfigVolumeSource := corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: config.DCGMExporter.MetricsConfig.Name,
				},
				Items: []corev1.KeyToPath{
					{
						Key:  MetricsConfigFileName,
						Path: MetricsConfigFileName,
					},
				},
			},
		}
		metricsConfigVol := corev1.Volume{Name: "metrics-config", VolumeSource: metricsConfigVolumeSource}
		obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, metricsConfigVol)
	}

	release, err := parseOSRelease()
	if err != nil {
		return fmt.Errorf("ERROR: failed to get os-release: %s", err)
	}

	// skip SELinux changes if not an OCP cluster
	if _, ok := release["OPENSHIFT_VERSION"]; !ok {
		return nil
	}

	// Add initContainer for OCP to set proper SELinux context on /var/lib/kubelet/pod-resources
	initImage, err := gpuv1.ImagePath(&config.Operator.InitContainer)
	if err != nil {
		return err
	}

	initContainer := v1.Container{}
	if initImage != "" {
		initContainer.Image = initImage
	}
	initContainer.Name = "init-pod-nvidia-node-status-exporter"
	initContainer.ImagePullPolicy = gpuv1.ImagePullPolicy(config.Operator.InitContainer.ImagePullPolicy)
	initContainer.Command = []string{"/bin/entrypoint.sh"}

	// need CAP_SYS_ADMIN privileges for collecting pod specific resources
	privileged := true
	securityContext := &corev1.SecurityContext{
		Privileged: &privileged,
	}

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

// TransformDCGM transforms dcgm daemonset with required config as per ClusterPolicy
func TransformDCGM(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update validation container
	transformValidationInitContainer(obj, config)
	// update image
	image, err := gpuv1.ImagePath(&config.DCGM)
	if err != nil {
		return err
	}
	obj.Spec.Template.Spec.Containers[0].Image = image
	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.DCGM.ImagePullPolicy)
	// set image pull secrets
	if len(config.DCGM.ImagePullSecrets) > 0 {
		for _, secret := range config.DCGM.ImagePullSecrets {
			obj.Spec.Template.Spec.ImagePullSecrets = append(obj.Spec.Template.Spec.ImagePullSecrets, v1.LocalObjectReference{Name: secret})
		}
	}
	// update PriorityClass
	if config.Daemonsets.PriorityClassName != "" {
		obj.Spec.Template.Spec.PriorityClassName = config.Daemonsets.PriorityClassName
	}
	// set tolerations if specified
	if len(config.Daemonsets.Tolerations) > 0 {
		obj.Spec.Template.Spec.Tolerations = config.Daemonsets.Tolerations
	}
	// set resource limits
	if config.DCGM.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources = *config.DCGM.Resources
		}
	}
	// set arguments if specified for exporter container
	if len(config.DCGM.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.DCGM.Args
	}
	// set/append environment variables for exporter container
	if len(config.DCGM.Env) > 0 {
		for _, env := range config.DCGM.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}

	// set host port to bind for DCGM engine
	for i, port := range obj.Spec.Template.Spec.Containers[0].Ports {
		if port.Name == "dcgm" {
			obj.Spec.Template.Spec.Containers[0].Ports[i].HostPort = DCGMDefaultHostPort
			if config.DCGM.HostPort != 0 {
				obj.Spec.Template.Spec.Containers[0].Ports[i].HostPort = config.DCGM.HostPort
			}
		}
	}

	// set RuntimeClass for supported runtimes
	setRuntimeClass(&obj.Spec.Template.Spec, n.runtime, config.Operator.RuntimeClass)

	return nil
}

// TransformMIGManager transforms MIG Manager daemonset with required config as per ClusterPolicy
func TransformMIGManager(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update validation container
	transformValidationInitContainer(obj, config)

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
	if config.Daemonsets.PriorityClassName != "" {
		obj.Spec.Template.Spec.PriorityClassName = config.Daemonsets.PriorityClassName
	}
	// set tolerations if specified
	if len(config.Daemonsets.Tolerations) > 0 {
		obj.Spec.Template.Spec.Tolerations = config.Daemonsets.Tolerations
	}

	// set resource limits
	if config.MIGManager.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources = *config.MIGManager.Resources
		}
	}

	// set arguments if specified for mig-manager container
	if len(config.MIGManager.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.MIGManager.Args
	}

	// set/append environment variables for mig-manager container
	if len(config.MIGManager.Env) > 0 {
		for _, env := range config.MIGManager.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}

	// set RuntimeClass for supported runtimes
	setRuntimeClass(&obj.Spec.Template.Spec, n.runtime, config.Operator.RuntimeClass)

	// set ConfigMap name for "mig-parted-config" Volume
	for i, vol := range obj.Spec.Template.Spec.Volumes {
		if !strings.Contains(vol.Name, "mig-parted-config") {
			continue
		}

		name := MigPartedDefaultConfigMapName
		if config.MIGManager.Config != nil && config.MIGManager.Config.Name != "" {
			name = config.MIGManager.Config.Name
		}
		obj.Spec.Template.Spec.Volumes[i].ConfigMap.Name = name
		break
	}

	// set ConfigMap name for "gpu-clients" Volume
	for i, vol := range obj.Spec.Template.Spec.Volumes {
		if !strings.Contains(vol.Name, "gpu-clients") {
			continue
		}

		name := MigDefaultGPUClientsConfigMapName
		if config.MIGManager.GPUClientsConfig != nil && config.MIGManager.GPUClientsConfig.Name != "" {
			name = config.MIGManager.GPUClientsConfig.Name
		}
		obj.Spec.Template.Spec.Volumes[i].ConfigMap.Name = name
		break
	}

	// If the nvidia driver is pre-installed, shutdown and restart gpu clients on the host
	// when applying mig mode/config changes. This requires the host pid and ipc namespaces.
	if !config.Driver.IsDriverEnabled() {
		setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), "WITH_SHUTDOWN_HOST_GPU_CLIENTS", "true")
		obj.Spec.Template.Spec.HostPID = true
		obj.Spec.Template.Spec.HostIPC = true
	}

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
	if config.Daemonsets.PriorityClassName != "" {
		obj.Spec.Template.Spec.PriorityClassName = config.Daemonsets.PriorityClassName
	}
	// set tolerations if specified
	if len(config.Daemonsets.Tolerations) > 0 {
		obj.Spec.Template.Spec.Tolerations = config.Daemonsets.Tolerations
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
	setRuntimeClass(&obj.Spec.Template.Spec, n.runtime, config.Operator.RuntimeClass)

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

// TransformNodeStatusExporter transforms the node-status-exporter daemonset with required config as per ClusterPolicy
func TransformNodeStatusExporter(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	// update validation container
	transformValidationInitContainer(obj, config)

	// update image
	image, err := gpuv1.ImagePath(&config.NodeStatusExporter)
	if err != nil {
		return err
	}
	obj.Spec.Template.Spec.Containers[0].Image = image

	// update image pull policy
	obj.Spec.Template.Spec.Containers[0].ImagePullPolicy = gpuv1.ImagePullPolicy(config.NodeStatusExporter.ImagePullPolicy)

	// set image pull secrets
	if len(config.NodeStatusExporter.ImagePullSecrets) > 0 {
		for _, secret := range config.NodeStatusExporter.ImagePullSecrets {
			obj.Spec.Template.Spec.ImagePullSecrets = append(obj.Spec.Template.Spec.ImagePullSecrets, v1.LocalObjectReference{Name: secret})
		}
	}

	// update PriorityClass
	if config.Daemonsets.PriorityClassName != "" {
		obj.Spec.Template.Spec.PriorityClassName = config.Daemonsets.PriorityClassName
	}
	// set tolerations if specified
	if len(config.Daemonsets.Tolerations) > 0 {
		obj.Spec.Template.Spec.Tolerations = config.Daemonsets.Tolerations
	}

	// set resource limits
	if config.NodeStatusExporter.Resources != nil {
		// apply resource limits to all containers
		for i := range obj.Spec.Template.Spec.Containers {
			obj.Spec.Template.Spec.Containers[i].Resources = *config.NodeStatusExporter.Resources
		}
	}

	// set arguments if specified for driver container
	if len(config.NodeStatusExporter.Args) > 0 {
		obj.Spec.Template.Spec.Containers[0].Args = config.NodeStatusExporter.Args
	}

	// set/append environment variables for exporter container
	if len(config.NodeStatusExporter.Env) > 0 {
		for _, env := range config.NodeStatusExporter.Env {
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[0]), env.Name, env.Value)
		}
	}

	// set RuntimeClass for supported runtimes
	setRuntimeClass(&obj.Spec.Template.Spec, n.runtime, config.Operator.RuntimeClass)

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

func getRuntimeClass(config *gpuv1.ClusterPolicySpec) string {
	if config.Operator.RuntimeClass != "" {
		return config.Operator.RuntimeClass
	}
	return DefaultRuntimeClass
}

func setRuntimeClass(podSpec *corev1.PodSpec, runtime gpuv1.Runtime, runtimeClass string) {
	if runtime == gpuv1.Containerd {
		if runtimeClass == "" {
			runtimeClass = DefaultRuntimeClass
		}
		podSpec.RuntimeClassName = &runtimeClass
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

func transformDriverManagerInitContainer(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec) error {
	for i, initContainer := range obj.Spec.Template.Spec.InitContainers {
		// skip if not validation initContainer
		if !strings.Contains(initContainer.Name, "k8s-driver-manager") {
			continue
		}
		// update driver-manager(initContainer) image and pull policy
		managerImage, err := gpuv1.ImagePath(&config.Driver.Manager)
		if err != nil {
			return err
		}
		obj.Spec.Template.Spec.InitContainers[i].Image = managerImage

		if config.Driver.ImagePullPolicy != "" {
			obj.Spec.Template.Spec.InitContainers[i].ImagePullPolicy = gpuv1.ImagePullPolicy(config.Driver.Manager.ImagePullPolicy)
		}

		// set/append environment variables for driver-manager initContainer
		if len(config.Driver.Manager.Env) > 0 {
			for _, env := range config.Driver.Manager.Env {
				setContainerEnv(&(obj.Spec.Template.Spec.InitContainers[i]), env.Name, env.Value)
			}
		}
	}
	return nil
}

func transformPeerMemoryContainer(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	for i, container := range obj.Spec.Template.Spec.Containers {
		// skip if not nvidia-peermem
		if !strings.Contains(container.Name, "nvidia-peermem") {
			continue
		}
		if config.Driver.GPUDirectRDMA == nil || !config.Driver.GPUDirectRDMA.IsEnabled() {
			// remove nvidia-peermem sidecar container from driver Daemonset if RDMA is not enabled
			obj.Spec.Template.Spec.Containers = append(obj.Spec.Template.Spec.Containers[:i], obj.Spec.Template.Spec.Containers[i+1:]...)
			return nil
		}
		// update nvidia-peermem driver image and pull policy to be same as gpu-driver image
		// as its installed as part of gpu-driver image
		driverImage, err := resolveDriverTag(n, &config.Driver)
		if err != nil {
			return err
		}
		if driverImage != "" {
			obj.Spec.Template.Spec.Containers[i].Image = driverImage
		}
		if config.Driver.ImagePullPolicy != "" {
			obj.Spec.Template.Spec.Containers[i].ImagePullPolicy = gpuv1.ImagePullPolicy(config.Driver.ImagePullPolicy)
		}
	}
	return nil
}

func transformOpenShiftDriverToolkitContainer(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	var err error

	getContainer := func(name string, remove bool) (*v1.Container, error) {
		for i, container := range obj.Spec.Template.Spec.Containers {
			if container.Name != name {
				continue
			}
			if !remove {
				return &obj.Spec.Template.Spec.Containers[i], nil
			}

			obj.Spec.Template.Spec.Containers = append(obj.Spec.Template.Spec.Containers[:i],
				obj.Spec.Template.Spec.Containers[i+1:]...)
			return nil, nil
		}
		return nil, fmt.Errorf(fmt.Sprintf("could not find the '%s' container", name))
	}

	if !n.ocpDriverToolkit.enabled {
		if n.ocpDriverToolkit.requested {
			n.rec.Log.Info("WARNING: OpenShift DriverToolkit was requested but could not be enabled (dependencies missing)")
		}

		/* remove OpenShift Driver Toolkit side-car container from the Driver DaemonSet */

		_, err = getContainer("openshift-driver-toolkit-ctr", true)

		return err
	}

	/* find the main container and driver-toolkit sidecar container */
	var mainContainer, driverToolkitContainer *v1.Container

	if mainContainer, err = getContainer("nvidia-driver-ctr", false); err != nil {
		return err
	}

	if driverToolkitContainer, err = getContainer("openshift-driver-toolkit-ctr", false); err != nil {
		return err
	}

	/* prepare the DaemonSet to be RHCOS-version specific */

	rhcosVersion := n.ocpDriverToolkit.currentRhcosVersion

	obj.ObjectMeta.Name += "-" + rhcosVersion
	obj.ObjectMeta.Labels["app"] = obj.ObjectMeta.Name
	obj.Spec.Selector.MatchLabels["app"] = obj.ObjectMeta.Name
	obj.Spec.Template.ObjectMeta.Labels["app"] = obj.ObjectMeta.Name

	obj.ObjectMeta.Labels[ocpDriverToolkitVersionLabel] = rhcosVersion

	obj.Spec.Template.Spec.NodeSelector[nfdOSTreeVersionLabelKey] = rhcosVersion

	/* prepare the DaemonSet to be searchable */

	obj.ObjectMeta.Labels[ocpDriverToolkitIdentificationLabel] = ocpDriverToolkitIdentificationValue
	obj.Spec.Template.ObjectMeta.Labels[ocpDriverToolkitIdentificationLabel] = ocpDriverToolkitIdentificationValue

	/* prepare the DriverToolkit container */

	setContainerEnv(driverToolkitContainer, "RHCOS_VERSION", rhcosVersion)

	image, _ := n.ocpDriverToolkit.rhcosDriverToolkitImages[n.ocpDriverToolkit.currentRhcosVersion]
	if image != "" {
		driverToolkitContainer.Image = image
		n.rec.Log.Info("INFO: DriverToolkit", "image", driverToolkitContainer.Image)
	} else {
		/* RHCOS tag missing in the Driver-Toolkit imagestream, setup fallback */
		obj.ObjectMeta.Labels["openshift.driver-toolkit.rhcos-image-missing"] = "true"
		obj.Spec.Template.ObjectMeta.Labels["openshift.driver-toolkit.rhcos-image-missing"] = "true"

		driverToolkitContainer.Image = mainContainer.Image
		setContainerEnv(mainContainer, "RHCOS_IMAGE_MISSING", "true")
		setContainerEnv(mainContainer, "RHCOS_VERSION", rhcosVersion)
		setContainerEnv(driverToolkitContainer, "RHCOS_IMAGE_MISSING", "true")

		n.rec.Log.Info("WARNING: DriverToolkit image tag missing. Version-specific fallback mode enabled.", "rhcosVersion", rhcosVersion)
	}

	/* prepare the shared volumes */

	// shared directory

	volSharedDirName, volSharedDirPath := "shared-nvidia-driver-toolkit", "/mnt/shared-nvidia-driver-toolkit"

	volMountSharedDir := corev1.VolumeMount{Name: volSharedDirName, MountPath: volSharedDirPath}
	mainContainer.VolumeMounts = append(mainContainer.VolumeMounts, volMountSharedDir)

	volSharedDir := corev1.Volume{
		Name: volSharedDirName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}

	obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, volSharedDir)

	/* prepare the main container to start from the DriverToolkit entrypoint */

	mainContainer.Command = []string{"ocp_dtk_entrypoint"}
	mainContainer.Args = []string{"nv-ctr-run-with-dtk"}

	return nil
}

// resolveDriverTag resolves driver image tag based on the OS of the worker node
func resolveDriverTag(n ClusterPolicyController, driverSpec *gpuv1.DriverSpec) (string, error) {
	kvers, osTag, _ := kernelFullVersion(n)
	if kvers == "" {
		return "", fmt.Errorf("ERROR: Could not find kernel full version: ('%s', '%s')", kvers, osTag)
	}

	image, err := gpuv1.ImagePath(driverSpec)
	if err != nil {
		return "", err
	}
	// if image digest is specified, use it directly
	if !strings.Contains(image, "sha256:") {
		// append os-tag to the provided driver version
		image = fmt.Sprintf("%s-%s", image, osTag)
	}
	return image, nil
}

// getRepoConfigPath returns the standard OS specific path for repository configuration files
func getRepoConfigPath() (string, error) {
	release, err := parseOSRelease()
	if err != nil {
		return "", err
	}

	os := release["ID"]
	if path, ok := RepoConfigPathMap[os]; ok {
		return path, nil
	}
	return "", fmt.Errorf("distribution not supported")
}

// getCertConfigPath returns the standard OS specific path for ssl keys/certificates
func getCertConfigPath() (string, error) {
	release, err := parseOSRelease()
	if err != nil {
		return "", err
	}

	os := release["ID"]
	if path, ok := CertConfigPathMap[os]; ok {
		return path, nil
	}
	return "", fmt.Errorf("distribution not supported")
}

// createConfigMapVolumeMounts creates a VolumeMount for each key
// in the ConfigMap. Use subPath to ensure original contents
// at destinationDir are not overwritten.
func createConfigMapVolumeMounts(n ClusterPolicyController, configMapName string, destinationDir string) ([]corev1.VolumeMount, []corev1.KeyToPath, error) {
	// get the ConfigMap
	cm := &corev1.ConfigMap{}
	opts := client.ObjectKey{Namespace: n.operatorNamespace, Name: configMapName}
	err := n.rec.Client.Get(context.TODO(), opts, cm)
	if err != nil {
		return nil, nil, fmt.Errorf("ERROR: could not get ConfigMap %s from client: %v", configMapName, err)
	}

	// create one volume mount per file in the ConfigMap and use subPath
	var filenames []string
	for filename := range cm.Data {
		filenames = append(filenames, filename)
	}
	// sort so volume mounts are added to spec in deterministic order
	sort.Strings(filenames)
	var itemsToInclude []corev1.KeyToPath
	var volumeMounts []corev1.VolumeMount
	for _, filename := range filenames {
		volumeMounts = append(volumeMounts,
			corev1.VolumeMount{Name: configMapName, ReadOnly: true, MountPath: filepath.Join(destinationDir, filename), SubPath: filename})
		itemsToInclude = append(itemsToInclude, corev1.KeyToPath{
			Key:  filename,
			Path: filename,
		})
	}
	return volumeMounts, itemsToInclude, nil
}

func createConfigMapVolume(configMapName string, itemsToInclude []corev1.KeyToPath) corev1.Volume {
	volumeSource := corev1.VolumeSource{
		ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: configMapName,
			},
			Items: itemsToInclude,
		},
	}
	return corev1.Volume{Name: configMapName, VolumeSource: volumeSource}
}

func transformDriverContainer(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec, n ClusterPolicyController) error {
	for i, container := range obj.Spec.Template.Spec.Containers {
		// skip if not nvidia-driver container
		if !strings.Contains(container.Name, "nvidia-driver") {
			continue
		}
		image, err := resolveDriverTag(n, &config.Driver)
		if err != nil {
			return err
		}
		if image != "" {
			obj.Spec.Template.Spec.Containers[i].Image = image
		}

		// update image pull policy
		obj.Spec.Template.Spec.Containers[i].ImagePullPolicy = gpuv1.ImagePullPolicy(config.Driver.ImagePullPolicy)

		// set image pull secrets
		if len(config.Driver.ImagePullSecrets) > 0 {
			for _, secret := range config.Driver.ImagePullSecrets {
				obj.Spec.Template.Spec.ImagePullSecrets = append(obj.Spec.Template.Spec.ImagePullSecrets, v1.LocalObjectReference{Name: secret})
			}
		}
		// set resource limits
		if config.Driver.Resources != nil {
			obj.Spec.Template.Spec.Containers[i].Resources = *config.Driver.Resources
		}
		// set arguments if specified for driver container
		if len(config.Driver.Args) > 0 {
			obj.Spec.Template.Spec.Containers[i].Args = config.Driver.Args
		}
		// set/append environment variables for exporter container
		if len(config.Driver.Env) > 0 {
			for _, env := range config.Driver.Env {
				setContainerEnv(&(obj.Spec.Template.Spec.Containers[i]), env.Name, env.Value)
			}
		}
		if config.Driver.GPUDirectRDMA != nil && config.Driver.GPUDirectRDMA.IsEnabled() {
			// set env indicating nvidia-peermem is enabled to compile module with required ib_* interfaces
			setContainerEnv(&(obj.Spec.Template.Spec.Containers[i]), GPUDirectRDMAEnabledEnvName, "true")
			// check if MOFED drives are directly installed on host and update source path accordingly
			// to build nvidia-peermem module
			if config.Driver.GPUDirectRDMA.UseHostMOFED != nil && *config.Driver.GPUDirectRDMA.UseHostMOFED {
				// mount /usr/src/ofa_kernel path directly from host to build using MOFED drivers installed on host
				for index, volume := range obj.Spec.Template.Spec.Volumes {
					if volume.Name == "mlnx-ofed-usr-src" {
						obj.Spec.Template.Spec.Volumes[index].HostPath.Path = "/usr/src"
					}
				}
				// set env indicating host-mofed is enabled
				setContainerEnv(&(obj.Spec.Template.Spec.Containers[i]), UseHostMOFEDEnvName, "true")
			}
		}

		// set any custom repo configuration provided
		if config.Driver.RepoConfig != nil && config.Driver.RepoConfig.ConfigMapName != "" {
			destinationDir, err := getRepoConfigPath()
			if err != nil {
				return fmt.Errorf("ERROR: failed to get destination directory for custom repo config: %v", err)
			}
			volumeMounts, itemsToInclude, err := createConfigMapVolumeMounts(n, config.Driver.RepoConfig.ConfigMapName, destinationDir)
			if err != nil {
				return fmt.Errorf("ERROR: failed to create ConfigMap VolumeMounts for custom repo config: %v", err)
			}
			obj.Spec.Template.Spec.Containers[i].VolumeMounts = append(obj.Spec.Template.Spec.Containers[i].VolumeMounts, volumeMounts...)
			obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, createConfigMapVolume(config.Driver.RepoConfig.ConfigMapName, itemsToInclude))

		}

		// set any custom ssl key/certificate configuration provided
		if config.Driver.CertConfig != nil && config.Driver.CertConfig.Name != "" {
			destinationDir, err := getCertConfigPath()
			if err != nil {
				return fmt.Errorf("ERROR: failed to get destination directory for custom repo config: %v", err)
			}
			volumeMounts, itemsToInclude, err := createConfigMapVolumeMounts(n, config.Driver.CertConfig.Name, destinationDir)
			if err != nil {
				return fmt.Errorf("ERROR: failed to create ConfigMap VolumeMounts for custom certs: %v", err)
			}
			obj.Spec.Template.Spec.Containers[i].VolumeMounts = append(obj.Spec.Template.Spec.Containers[i].VolumeMounts, volumeMounts...)
			obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, createConfigMapVolume(config.Driver.CertConfig.Name, itemsToInclude))
		}

		// set any licensing configuration required
		if config.Driver.LicensingConfig != nil && config.Driver.LicensingConfig.ConfigMapName != "" {
			licensingConfigVolMount := corev1.VolumeMount{Name: "licensing-config", ReadOnly: true, MountPath: VGPULicensingConfigMountPath, SubPath: VGPULicensingFileName}
			obj.Spec.Template.Spec.Containers[i].VolumeMounts = append(obj.Spec.Template.Spec.Containers[i].VolumeMounts, licensingConfigVolMount)

			// gridd.conf always mounted
			licenseItemsToInclude := []corev1.KeyToPath{
				{
					Key:  VGPULicensingFileName,
					Path: VGPULicensingFileName,
				},
			}
			// client config token only mounted when NLS is enabled
			if config.Driver.LicensingConfig.IsNLSEnabled() {
				licenseItemsToInclude = append(licenseItemsToInclude, corev1.KeyToPath{
					Key:  NLSClientTokenFileName,
					Path: NLSClientTokenFileName,
				})
				nlsTokenVolMount := corev1.VolumeMount{Name: "licensing-config", ReadOnly: true, MountPath: NLSClientTokenMountPath, SubPath: NLSClientTokenFileName}
				obj.Spec.Template.Spec.Containers[i].VolumeMounts = append(obj.Spec.Template.Spec.Containers[i].VolumeMounts, nlsTokenVolMount)
			}

			licensingConfigVolumeSource := corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: config.Driver.LicensingConfig.ConfigMapName,
					},
					Items: licenseItemsToInclude,
				},
			}
			licensingConfigVol := corev1.Volume{Name: "licensing-config", VolumeSource: licensingConfigVolumeSource}
			obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, licensingConfigVol)
		}

		// set virtual topology daemon configuration if specified for vGPU driver
		if config.Driver.VirtualTopology != nil && config.Driver.VirtualTopology.Config != "" {
			topologyConfigVolMount := corev1.VolumeMount{Name: "topology-config", ReadOnly: true, MountPath: VGPUTopologyConfigMountPath, SubPath: VGPUTopologyConfigFileName}
			obj.Spec.Template.Spec.Containers[i].VolumeMounts = append(obj.Spec.Template.Spec.Containers[i].VolumeMounts, topologyConfigVolMount)

			topologyConfigVolumeSource := corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: config.Driver.VirtualTopology.Config,
					},
					Items: []corev1.KeyToPath{
						{
							Key:  VGPUTopologyConfigFileName,
							Path: VGPUTopologyConfigFileName,
						},
					},
				},
			}
			topologyConfigVol := corev1.Volume{Name: "topology-config", VolumeSource: topologyConfigVolumeSource}
			obj.Spec.Template.Spec.Volumes = append(obj.Spec.Template.Spec.Volumes, topologyConfigVol)
		}

		release, err := parseOSRelease()
		if err != nil {
			return fmt.Errorf("ERROR: failed to get os-release: %s", err)
		}

		// skip proxy and env settings if not ocp cluster
		if _, ok := release["OPENSHIFT_VERSION"]; !ok {
			return nil
		}

		// Add env vars needed by nvidia-driver to enable the right releasever and EUS rpm repos
		rhelVersion := corev1.EnvVar{Name: "RHEL_VERSION", Value: release["RHEL_VERSION"]}
		ocpVersion := corev1.EnvVar{Name: "OPENSHIFT_VERSION", Value: release["OPENSHIFT_VERSION"]}

		obj.Spec.Template.Spec.Containers[i].Env = append(obj.Spec.Template.Spec.Containers[i].Env, rhelVersion)
		obj.Spec.Template.Spec.Containers[i].Env = append(obj.Spec.Template.Spec.Containers[i].Env, ocpVersion)

		// Automatically apply proxy settings for OCP and inject custom CA if configured by user
		// https://docs.openshift.com/container-platform/4.6/networking/configuring-a-custom-pki.html
		err = applyOCPProxySpec(n, &obj.Spec.Template.Spec)
		if err != nil {
			return err
		}
	}
	return nil
}

func transformValidationInitContainer(obj *appsv1.DaemonSet, config *gpuv1.ClusterPolicySpec) error {
	for i, initContainer := range obj.Spec.Template.Spec.InitContainers {
		// skip if not validation initContainer
		if !strings.Contains(initContainer.Name, "validation") {
			continue
		}
		if strings.Contains(initContainer.Name, "mofed-validation") {
			if config.Driver.GPUDirectRDMA == nil || !config.Driver.GPUDirectRDMA.IsEnabled() {
				// remove mofed-validation init container from driver Daemonset if RDMA is not enabled
				obj.Spec.Template.Spec.InitContainers = append(obj.Spec.Template.Spec.InitContainers[:i], obj.Spec.Template.Spec.InitContainers[i+1:]...)
				continue
			} else {
				// pass env for mofed-validation
				setContainerEnv(&(obj.Spec.Template.Spec.InitContainers[i]), GPUDirectRDMAEnabledEnvName, "true")
				if config.Driver.GPUDirectRDMA.UseHostMOFED != nil && *config.Driver.GPUDirectRDMA.UseHostMOFED {
					// set env indicating host-mofed is enabled
					setContainerEnv(&(obj.Spec.Template.Spec.InitContainers[i]), UseHostMOFEDEnvName, "true")
				}
			}
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
	obj.Namespace = n.operatorNamespace

	logger := n.rec.Log.WithValues("Deployment", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	if err := n.rec.Client.Create(context.TODO(), obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Found Resource, updating...")
			err = n.rec.Client.Update(context.TODO(), obj)
			if err != nil {
				logger.Info("Couldn't update", "Error", err)
				return gpuv1.NotReady, err
			}
			return isDeploymentReady(obj.Name, n), nil
		}

		logger.Info("Couldn't create", "Error", err)
		return gpuv1.NotReady, err
	}

	return isDeploymentReady(obj.Name, n), nil
}

func ocpHasDriverToolkitImageStream(n *ClusterPolicyController) (bool, error) {
	found := &apiimagev1.ImageStream{}
	name := "driver-toolkit"
	namespace := "openshift"
	err := n.rec.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			n.rec.Log.Info("ocpHasDriverToolkitImageStream: driver-toolkit imagestream not found",
				"Name", name,
				"Namespace", namespace)

			return false, nil
		}

		n.rec.Log.Info("Couldn't get the driver-toolkit imagestream", "Error", err)

		return false, err
	}
	n.rec.Log.Info("DEBUG: ocpHasDriverToolkitImageStream: driver-toolkit imagestream found")
	isBroken := false
	for _, tag := range found.Spec.Tags {
		if tag.Name == "" {
			isBroken = true
			continue
		}
		if tag.Name == "latest" || tag.From == nil {
			continue
		}
		n.rec.Log.Info("DEBUG: ocpHasDriverToolkitImageStream: tag", tag.Name, tag.From.Name)
		n.ocpDriverToolkit.rhcosDriverToolkitImages[tag.Name] = tag.From.Name
	}
	if isBroken {
		n.rec.Log.Info("WARNING: ocpHasDriverToolkitImageStream: driver-toolkit imagestream is broken, see RHBZ#2015024")

		n.operatorMetrics.openshiftDriverToolkitIsBroken.Set(1)
	} else {
		n.operatorMetrics.openshiftDriverToolkitIsBroken.Set(0)
	}

	return true, nil
}

// serviceAccountHasDockerCfg returns True if obj ServiceAccount
// exists and has its builder-dockercfg secret reference populated.
//
// With OpenShift DriverToolkit, we need to ensure that this secret is
// populated, otherwise, the Pod won't have the credentials to access
// the DriverToolkit image in the cluster registry.
func serviceAccountHasDockerCfg(obj *v1.ServiceAccount, n ClusterPolicyController) (bool, error) {
	logger := n.rec.Log.WithValues("ServiceAccount", obj.Name)

	err := n.rec.Client.Get(context.TODO(), types.NamespacedName{Namespace: n.operatorNamespace, Name: obj.Name}, obj)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ServiceAccount not found",
				"Namespace", n.operatorNamespace, "err", err)
			return false, nil
		}

		logger.Info("Couldn't get the ServiceAccount",
			"Name", obj.Name,
			"Error", err)

		return false, err
	}

	for _, secret := range obj.Secrets {
		if strings.HasPrefix(secret.Name, obj.Name+"-dockercfg-") {
			return true, nil
		}
	}

	logger.Info("ServiceAccount doesn't have dockercfg secret", "Name", obj.Name)

	return false, nil
}

// ocpDriverToolkitDaemonSets goes through all the RHCOS versions
// found in the cluster, sets `currentRhcosVersion` and calls the
// original DaemonSet() function to create/update the RHCOS-specific
// DaemonSet.
func ocpDriverToolkitDaemonSets(n ClusterPolicyController) (gpuv1.State, error) {
	ocpCleanupUnusedDriverToolkitDaemonSets(n)

	state := n.idx
	saObj := n.resources[state].ServiceAccount.DeepCopy()
	saReady, err := serviceAccountHasDockerCfg(saObj, n)
	if err != nil {
		return gpuv1.NotReady, err
	}
	if !saReady {
		n.rec.Log.Info("Driver ServiceAccount not ready, cannot create DriverToolkit DaemonSet")
		return gpuv1.NotReady, nil
	}

	n.rec.Log.Info("DEBUG: preparing DriverToolkit DaemonSet",
		"rhcos", n.ocpDriverToolkit.rhcosVersions)

	overallState := gpuv1.Ready
	var errs error

	for rhcosVersion := range n.ocpDriverToolkit.rhcosVersions {
		n.ocpDriverToolkit.currentRhcosVersion = rhcosVersion

		n.rec.Log.Info("DEBUG: preparing DriverToolkit DaemonSet",
			"rhcosVersion", n.ocpDriverToolkit.currentRhcosVersion)

		state, err := DaemonSet(n)

		n.rec.Log.Info("DEBUG: preparing DriverToolkit DaemonSet",
			"rhcosVersion", n.ocpDriverToolkit.currentRhcosVersion, "state", state)
		if state != gpuv1.Ready {
			overallState = state
		}

		if err != nil {
			if errs == nil {
				errs = err
			}
			errs = fmt.Errorf("failed to handle OpenShift Driver Toolkit Daemonset for version %s: %v", rhcosVersion, errs)
		}
	}

	n.ocpDriverToolkit.currentRhcosVersion = ""

	tagsMissing := false
	for rhcosVersion, image := range n.ocpDriverToolkit.rhcosDriverToolkitImages {
		if image != "" {
			continue
		}
		n.rec.Log.Info("WARNINGs: RHCOS driver-toolkit image missing. Version-specific fallback mode enabled.", "rhcosVersion", rhcosVersion)
		tagsMissing = true
	}
	if tagsMissing {
		n.operatorMetrics.openshiftDriverToolkitRhcosTagsMissing.Set(1)
	} else {
		n.operatorMetrics.openshiftDriverToolkitRhcosTagsMissing.Set(0)
	}

	return overallState, errs
}

// ocpCleanupUnusedDriverToolkitDaemonSets scans the DriverToolkit
// RHCOS-version specific DaemonSets, and deletes the unused one:
// - RHCOS version wasn't found in the node labels (upgrade finished)
// - RHCOS version marked for deletion ealier in the Reconciliation loop (currently unexpected)
// - no RHCOS version label (unexpected)
// The DaemonSet set is kept if:
// - RHCOS version was found in the node labels (most likely case)
func ocpCleanupUnusedDriverToolkitDaemonSets(n ClusterPolicyController) {
	opts := []client.ListOption{
		client.MatchingLabels{
			ocpDriverToolkitIdentificationLabel: ocpDriverToolkitIdentificationValue,
		},
	}

	list := &appsv1.DaemonSetList{}
	err := n.rec.Client.List(context.TODO(), list, opts...)
	if err != nil {
		n.rec.Log.Info("ERROR: Could not get DaemonSetList", "Error", err)
		return
	}

	for idx := range list.Items {
		name := list.Items[idx].ObjectMeta.Name
		dsRhcosVersion, versionOk := list.Items[idx].ObjectMeta.Labels[ocpDriverToolkitVersionLabel]
		clusterHasRhcosVersion, clusterOk := n.ocpDriverToolkit.rhcosVersions[dsRhcosVersion]
		desiredNumberScheduled := list.Items[idx].Status.DesiredNumberScheduled

		n.rec.Log.Info("DEBUG: Driver DaemonSet found",
			"Name", name,
			"dsRhcosVersion", dsRhcosVersion,
			"clusterHasRhcosVersion", clusterHasRhcosVersion,
			"desiredNumberScheduled", desiredNumberScheduled)

		if desiredNumberScheduled != 0 {
			n.rec.Log.Info("INFO: Driver DaemonSet active, keep it.",
				"Name", name, "Status.DesiredNumberScheduled", desiredNumberScheduled)
			continue
		}

		if !versionOk {
			n.rec.Log.Info("WARNING: Driver DaemonSet doesn't have DriverToolkit version label",
				"Name", name, "Label", ocpDriverToolkitVersionLabel,
			)
		} else {
			if !clusterOk {
				n.rec.Log.Info("DEBUG: Driver DaemonSet RHCOS version NOT part of the cluster",
					"Name", name, "RHCOS version", dsRhcosVersion,
				)
			} else if clusterHasRhcosVersion {
				n.rec.Log.Info("DEBUG: Driver DaemonSet RHCOS version is part of the cluster, keep it.",
					"Name", name, "RHCOS version", dsRhcosVersion,
				)

				// the version of RHCOS targetted by this DS is part of the cluster
				// keep it alive

				continue
			} else /* clusterHasRhcosVersion == false */ {
				// currently unexpected
				n.rec.Log.Info("DEBUG: Driver DaemonSet RHCOS version marked for deletion",
					"Name", name, "RHCOS version", dsRhcosVersion,
				)
			}
		}

		n.rec.Log.Info("INFO: Delete Driver DaemonSet", "Name", name)

		err = n.rec.Client.Delete(context.TODO(), &list.Items[idx])
		if err != nil {
			n.rec.Log.Info("ERROR: Could not get delete DaemonSet",
				"Name", name, "Error", err)

		}
	}
}

// cleanupUnusedDriverDaemonSets cleans up the driver DaemonSet(s)
// according to the UseOpenShiftDriverToolkit flag. This allows
// switching toggling the flag after the initial deployment.  If no
// error happens, returns the number of Pods belonging to these
// DaemonSets.
func cleanupUnusedDriverDaemonSets(n ClusterPolicyController) (int, error) {
	// in the future, these flags can be extended to also check the
	// value of n.singleton.Spec.Driver.IsDriverEnabled()

	cleanupNvidiaDriver := n.ocpDriverToolkit.enabled
	cleanupOcpDriverToolkitDriver := !cleanupNvidiaDriver

	var podCount = 0

	if cleanupNvidiaDriver {
		count, err := cleanupDaemonSets(n, "app", "nvidia-driver-daemonset")
		if err != nil {
			return 0, err
		}
		podCount += count
	}

	if cleanupOcpDriverToolkitDriver {
		count, err := cleanupDaemonSets(n,
			ocpDriverToolkitIdentificationLabel,
			ocpDriverToolkitIdentificationValue)
		if err != nil {
			return 0, err
		}
		podCount += count
	}

	return podCount, nil
}

// cleanupDaemonSets deletes the DaemonSets matching a given key/value
// pairs If no error happens, returns the number of Pods belonging to
// the DaemonSet.
func cleanupDaemonSets(n ClusterPolicyController, searchKey, searchValue string) (int, error) {
	var opts = []client.ListOption{client.MatchingLabels{searchKey: searchValue}}

	dsList := &appsv1.DaemonSetList{}
	if err := n.rec.Client.List(context.TODO(), dsList, opts...); err != nil {
		n.rec.Log.Info("ERROR: Could not get DaemonSetList", "Error", err)
		return 0, err
	}

	var lastErr error
	for idx := range dsList.Items {
		n.rec.Log.Info("INFO: Delete DaemonSet",
			"Name", dsList.Items[idx].ObjectMeta.Name,
		)

		if err := n.rec.Client.Delete(context.TODO(), &dsList.Items[idx]); err != nil {
			n.rec.Log.Info("ERROR: Could not get delete DaemonSet",
				"Name", dsList.Items[idx].ObjectMeta.Name,
				"Error", err)
			lastErr = err
		}
	}

	// return the last error that occured, if any
	if lastErr != nil {
		return 0, lastErr
	}

	podList := &corev1.PodList{}
	if err := n.rec.Client.List(context.TODO(), podList, opts...); err != nil {
		n.rec.Log.Info("ERROR: Could not get PodList", "Error", err)
		return 0, err
	}

	return len(podList.Items), nil
}

// DaemonSet creates Daemonset resource
func DaemonSet(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].DaemonSet.DeepCopy()
	obj.Namespace = n.operatorNamespace

	logger := n.rec.Log.WithValues("DaemonSet", obj.Name, "Namespace", obj.Namespace)

	if !n.hasGPUNodes {
		// multiple DaemonSets (eg, driver, dgcm-exporter) cannot be
		// deployed without knowing the OS name, so skip their
		// deployment for now. The operator will be notified
		// (addWatchNewGPUNode) when new nodes will join the cluster.
		logger.Info("No GPU node in the cluster, do not create DaemonSets")

		return gpuv1.Ready, nil
	}

	if n.resources[state].DaemonSet.ObjectMeta.Name == "nvidia-driver-daemonset" {
		podCount, err := cleanupUnusedDriverDaemonSets(n)
		if err != nil {
			return gpuv1.NotReady, err
		}
		if podCount != 0 {
			logger.Info("Driver DaemonSet cleanup in progress", "podCount", podCount)

			return gpuv1.NotReady, nil
		}

		if n.ocpDriverToolkit.enabled &&
			n.ocpDriverToolkit.currentRhcosVersion == "" {
			// OpenShift Driver Toolkit requires the creation of
			// one Driver DaemonSet per RHCOS version (stored in
			// n.ocpDriverToolkit.rhcosVersions).
			//
			// Here, we are at the top-most call of DaemonSet(),
			// as currentRhcosVersion is unset.
			//
			// Initiate the multi-DaemonSet OCP DriverToolkit
			// deployment.

			return ocpDriverToolkitDaemonSets(n)
		}
	}

	err := preProcessDaemonSet(obj, n)
	if err != nil {
		logger.Info("Could not pre-process", "Error", err)
		return gpuv1.NotReady, err
	}

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		logger.Info("SetControllerReference failed", "Error", err)

		return gpuv1.NotReady, err
	}

	// Daemonsets will always have at least one annotation applied, so allocate if necessary
	if obj.Annotations == nil {
		obj.Annotations = make(map[string]string)
	}

	found := &appsv1.DaemonSet{}
	err = n.rec.Client.Get(context.TODO(), types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("DaemonSet not found, creating",
			"Name", obj.Name,
		)
		// generate hash for the spec to create
		hashStr := getDaemonsetHash(obj)
		// add annotation to the Daemonset with hash value during creation
		obj.Annotations[NvidiaAnnotationHashKey] = hashStr
		err = n.rec.Client.Create(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't create DaemonSet",
				"Name", obj.Name,
				"Error", err,
			)
			return gpuv1.NotReady, err
		}
		return isDaemonSetReady(obj.Name, n), nil
	} else if err != nil {
		logger.Info("Failed to get DaemonSet from client",
			"Name", obj.Name,
			"Error", err.Error())
		return gpuv1.NotReady, err
	}

	changed := isDaemonsetSpecChanged(found, obj)
	if changed {
		logger.Info("DaemonSet is different, updating", "name", obj.ObjectMeta.Name)
		err = n.rec.Client.Update(context.TODO(), obj)
		if err != nil {
			return gpuv1.NotReady, err
		}
	} else {
		logger.Info("DaemonSet identical, skipping update", "name", obj.ObjectMeta.Name)
	}
	return isDaemonSetReady(obj.Name, n), nil
}

func getDaemonsetHash(daemonset *appsv1.DaemonSet) string {
	hash, err := hashstructure.Hash(daemonset, nil)
	if err != nil {
		panic(err.Error())
	}
	return strconv.FormatUint(hash, 16)
}

// isDaemonsetSpecChanged returns true if the spec has changed between existing one
// and new Daemonset spec compared by hash.
func isDaemonsetSpecChanged(current *appsv1.DaemonSet, new *appsv1.DaemonSet) bool {
	if current == nil && new != nil {
		return true
	}
	if current.Annotations == nil || new.Annotations == nil {
		panic("appsv1.DaemonSet.Annotations must be allocated prior to calling isDaemonsetSpecChanged()")
	}

	hashStr := getDaemonsetHash(new)
	foundHashAnnotation := false

	for annotation, value := range current.Annotations {
		if annotation == NvidiaAnnotationHashKey {
			if value != hashStr {
				// update annotation to be added to Daemonset as per new spec and indicate spec update is required
				new.Annotations[NvidiaAnnotationHashKey] = hashStr
				return true
			}
			foundHashAnnotation = true
			break
		}
	}

	if !foundHashAnnotation {
		// update annotation to be added to Daemonset as per new spec and indicate spec update is required
		new.Annotations[NvidiaAnnotationHashKey] = hashStr
		return true
	}
	return false
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
	obj.Namespace = n.operatorNamespace

	logger := n.rec.Log.WithValues("SecurityContextConstraints", obj.Name, "Namespace", "default")

	for idx := range obj.Users {
		if obj.Users[idx] != "FILLED BY THE OPERATOR" {
			continue
		}
		obj.Users[idx] = fmt.Sprintf("system:serviceaccount:%s:%s", obj.Namespace, obj.Name)
	}

	if obj.Name == "nvidia-mig-manager" && !n.singleton.Spec.Driver.IsDriverEnabled() {
		obj.AllowHostIPC = true
	}

	// Allow hostNetwork only when a separate standalone DCGM engine is deployed for communication
	if obj.Name == "nvidia-dcgm-exporter" && n.singleton.Spec.DCGM.IsEnabled() {
		obj.AllowHostNetwork = true
	}

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	found := &secv1.SecurityContextConstraints{}
	err := n.rec.Client.Get(context.TODO(), types.NamespacedName{Namespace: "", Name: obj.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Not found, creating...")
		err = n.rec.Client.Create(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Ready, nil
	} else if err != nil {
		return gpuv1.NotReady, err
	}

	logger.Info("Found Resource, updating...")
	obj.ResourceVersion = found.ResourceVersion

	err = n.rec.Client.Update(context.TODO(), obj)
	if err != nil {
		logger.Info("Couldn't update", "Error", err)
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
	obj.Namespace = n.operatorNamespace

	logger := n.rec.Log.WithValues("PodSecurityPolicies", obj.Name)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	found := &policyv1beta1.PodSecurityPolicy{}
	err := n.rec.Client.Get(context.TODO(), types.NamespacedName{Namespace: "", Name: obj.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Not found, creating...")
		err = n.rec.Client.Create(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Ready, nil
	} else if err != nil {
		return gpuv1.NotReady, err
	}

	logger.Info("Found Resource, updating...")
	obj.ResourceVersion = found.ResourceVersion

	err = n.rec.Client.Update(context.TODO(), obj)
	if err != nil {
		logger.Info("Couldn't update", "Error", err)
		return gpuv1.NotReady, err
	}
	return gpuv1.Ready, nil
}

// Service creates Service object
func Service(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].Service.DeepCopy()

	obj.Namespace = n.operatorNamespace

	logger := n.rec.Log.WithValues("Service", obj.Name, "Namespace", obj.Namespace)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	found := &corev1.Service{}
	err := n.rec.Client.Get(context.TODO(), types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Not found, creating...")
		err = n.rec.Client.Create(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Ready, nil
	} else if err != nil {
		return gpuv1.NotReady, err
	}

	logger.Info("Found Resource, updating...")
	obj.ResourceVersion = found.ResourceVersion
	obj.Spec.ClusterIP = found.Spec.ClusterIP

	err = n.rec.Client.Update(context.TODO(), obj)
	if err != nil {
		logger.Info("Couldn't update", "Error", err)
		return gpuv1.NotReady, err
	}
	return gpuv1.Ready, nil
}

// ServiceMonitor creates ServiceMonitor object
func ServiceMonitor(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].ServiceMonitor.DeepCopy()
	obj.Namespace = n.operatorNamespace

	if n.stateNames[state] == "state-operator-metrics" {
		obj.Spec.NamespaceSelector.MatchNames = []string{obj.Namespace}
	}

	logger := n.rec.Log.WithValues("ServiceMonitor", obj.Name, "Namespace", obj.Namespace)

	for idx := range obj.Spec.NamespaceSelector.MatchNames {
		if obj.Spec.NamespaceSelector.MatchNames[idx] != "FILLED BY THE OPERATOR" {
			continue
		}
		obj.Spec.NamespaceSelector.MatchNames[idx] = obj.Namespace
	}

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	found := &promv1.ServiceMonitor{}
	err := n.rec.Client.Get(context.TODO(), types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Not found, creating...")
		err = n.rec.Client.Create(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Ready, nil
	} else if err != nil {
		return gpuv1.NotReady, err
	}

	logger.Info("Found Resource, updating...")
	obj.ResourceVersion = found.ResourceVersion

	err = n.rec.Client.Update(context.TODO(), obj)
	if err != nil {
		logger.Info("Couldn't update", "Error", err)
		return gpuv1.NotReady, err
	}
	return gpuv1.Ready, nil
}

// RuntimeClass creates RuntimeClass object
func RuntimeClass(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].RuntimeClass.DeepCopy()

	// apply runtime class name as per ClusterPolicy
	obj.Name = getRuntimeClass(&n.singleton.Spec)
	obj.Handler = getRuntimeClass(&n.singleton.Spec)

	logger := n.rec.Log.WithValues("RuntimeClass", obj.Name)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	found := &nodev1.RuntimeClass{}
	err := n.rec.Client.Get(context.TODO(), types.NamespacedName{Namespace: "", Name: obj.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Not found, creating...")
		err = n.rec.Client.Create(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Ready, nil
	} else if err != nil {
		return gpuv1.NotReady, err
	}

	logger.Info("Found Resource, updating...")
	obj.ResourceVersion = found.ResourceVersion

	err = n.rec.Client.Update(context.TODO(), obj)
	if err != nil {
		logger.Info("Couldn't update", "Error", err)
		return gpuv1.NotReady, err
	}
	return gpuv1.Ready, nil
}

// PrometheusRule creates PrometheusRule object
func PrometheusRule(n ClusterPolicyController) (gpuv1.State, error) {
	state := n.idx
	obj := n.resources[state].PrometheusRule.DeepCopy()
	obj.Namespace = n.operatorNamespace

	logger := n.rec.Log.WithValues("PrometheusRule", obj.Name)

	if err := controllerutil.SetControllerReference(n.singleton, obj, n.rec.Scheme); err != nil {
		return gpuv1.NotReady, err
	}

	found := &promv1.PrometheusRule{}
	err := n.rec.Client.Get(context.TODO(), types.NamespacedName{Namespace: obj.Namespace, Name: obj.Name}, found)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("Not found, creating...")
		err = n.rec.Client.Create(context.TODO(), obj)
		if err != nil {
			logger.Info("Couldn't create", "Error", err)
			return gpuv1.NotReady, err
		}
		return gpuv1.Ready, nil
	} else if err != nil {
		return gpuv1.NotReady, err
	}

	logger.Info("Found Resource, updating...")
	obj.ResourceVersion = found.ResourceVersion

	err = n.rec.Client.Update(context.TODO(), obj)
	if err != nil {
		logger.Info("Couldn't update", "Error", err)
		return gpuv1.NotReady, err
	}
	return gpuv1.Ready, nil
}
