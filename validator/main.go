/*
 * Copyright (c) 2021, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	cli "github.com/urfave/cli/v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

// Component of GPU operator
type Component interface {
	validate() error
	createStatusFile() error
	deleteStatusFile() error
}

// Driver component
type Driver struct{}

// CUDA represents spec to run cuda workload
type CUDA struct {
	kubeClient kubernetes.Interface
}

// Plugin component
type Plugin struct {
	kubeClient kubernetes.Interface
}

// Toolkit component
type Toolkit struct{}

// MOFED represents spec to validate MOFED driver installation
type MOFED struct {
	kubeClient kubernetes.Interface
}

// Metrics represents spec to run metrics exporter
type Metrics struct {
	kubeClient kubernetes.Interface
}

var (
	kubeconfigFlag           string
	nodeNameFlag             string
	namespaceFlag            string
	withWaitFlag             bool
	withWorkloadFlag         bool
	componentFlag            string
	cleanupAllFlag           bool
	outputDirFlag            string
	sleepIntervalSecondsFlag int
	migStrategyFlag          string
	metricsPort              int
)

const (
	// defaultStatusPath indicates directory to create all validation status files
	defaultStatusPath = "/run/nvidia/validations"
	// defaultSleepIntervalSeconds indicates sleep interval in seconds between validation command retries
	defaultSleepIntervalSeconds = 5
	// defaultMetricsPort indicates the port on which the metrics will be exposed.
	defaultMetricsPort = 0
	// driverStatusFile indicates status file for driver readiness
	driverStatusFile = "driver-ready"
	// toolkitStatusFile indicates status file for toolkit readiness
	toolkitStatusFile = "toolkit-ready"
	// pluginStatusFile indicates status file for plugin readiness
	pluginStatusFile = "plugin-ready"
	// cudaStatusFile indicates status file for cuda readiness
	cudaStatusFile = "cuda-ready"
	// mofedStatusFile indicates status file for mofed driver readiness
	mofedStatusFile = "mofed-ready"
	// podCreationWaitRetries indicates total retries to wait for plugin validation pod creation
	podCreationWaitRetries = 60
	// podCreationSleepIntervalSeconds indicates sleep interval in seconds between checking for plugin validation pod readiness
	podCreationSleepIntervalSeconds = 5
	// gpuResourceDiscoveryWaitRetries indicates total retries to wait for node to discovery GPU resources
	gpuResourceDiscoveryWaitRetries = 30
	// gpuResourceDiscoveryIntervalSeconds indicates sleep interval in seconds between checking for available GPU resources
	gpuResourceDiscoveryIntervalSeconds = 5
	// genericGPUResourceType indicates the generic name of the GPU exposed by NVIDIA DevicePlugin
	genericGPUResourceType = "nvidia.com/gpu"
	// migGPUResourcePrefix indicates the prefix of the MIG resources exposed by NVIDIA DevicePlugin
	migGPUResourcePrefix = "nvidia.com/mig-"
	// devicePluginEnvMigStrategy indicates the name of the DevicePlugin Env variable used to configure the MIG strategy
	devicePluginEnvMigStrategy = "MIG_STRATEGY"
	// migStrategyMixed indicates mixed MIG strategy
	migStrategyMixed = "mixed"
	// migStrategySingle indicates mixed MIG strategy
	migStrategySingle = "single"
	// pluginWorkloadPodSpecPath indicates path to plugin validation pod definition
	pluginWorkloadPodSpecPath = "/var/nvidia/manifests/plugin-workload-validation.yaml"
	// cudaWorkloadPodSpecPath indicates path to cuda validation pod definition
	cudaWorkloadPodSpecPath = "/var/nvidia/manifests/cuda-workload-validation.yaml"
	// NodeSelectorKey indicates node label key to use as node selector for plugin validation pod
	nodeSelectorKey = "kubernetes.io/hostname"
	// validatorImageEnvName indicates env name for validator image passed
	validatorImageEnvName = "VALIDATOR_IMAGE"
	// validatorImagePullPolicyEnvName indicates env name for validator image pull policy passed
	validatorImagePullPolicyEnvName = "VALIDATOR_IMAGE_PULL_POLICY"
	// validatorImagePullSecretsEnvName indicates env name for validator image pull secrets passed
	validatorImagePullSecretsEnvName = "VALIDATOR_IMAGE_PULL_SECRETS"
	// validatorRuntimeClassEnvName indicates env name for validator runtimeclass passed
	validatorRuntimeClassEnvName = "VALIDATOR_RUNTIME_CLASS"
	// cudaValidatorLabelValue represents label for cuda workload validation pod
	cudaValidatorLabelValue = "nvidia-cuda-validator"
	// pluginValidatorLabelValue represents label for device-plugin workload validation pod
	pluginValidatorLabelValue = "nvidia-device-plugin-validator"
	// MellanoxDeviceLabelKey represents NFD label name for Mellanox devices
	MellanoxDeviceLabelKey = "feature.node.kubernetes.io/pci-15b3.present"
	// GPUDirectRDMAEnabledEnvName represents env name to indicate if GPUDirect RDMA is enabled through GPU Operator
	GPUDirectRDMAEnabledEnvName = "GPU_DIRECT_RDMA_ENABLED"
	// UseHostMOFEDEnvname represents env name to indicate if MOFED is pre-installed on host
	UseHostMOFEDEnvname = "USE_HOST_MOFED"
)

func main() {
	c := cli.NewApp()
	c.Before = validateFlags
	c.Action = start

	c.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:        "kubeconfig",
			Value:       "",
			Usage:       "absolute path to the kubeconfig file",
			Destination: &kubeconfigFlag,
			EnvVars:     []string{"KUBECONFIG"},
		},
		&cli.StringFlag{
			Name:        "node-name",
			Aliases:     []string{"n"},
			Value:       "",
			Usage:       "the name of the node to deploy plugin validation pod",
			Destination: &nodeNameFlag,
			EnvVars:     []string{"NODE_NAME"},
		},
		&cli.StringFlag{
			Name:        "namespace",
			Aliases:     []string{"ns"},
			Value:       "",
			Usage:       "the namespace in which the operator resources are deployed",
			Destination: &namespaceFlag,
			EnvVars:     []string{"OPERATOR_NAMESPACE"},
		},
		&cli.BoolFlag{
			Name:        "with-wait",
			Aliases:     []string{"w"},
			Value:       false,
			Usage:       "indicates to wait for validation to complete successfully",
			Destination: &withWaitFlag,
			EnvVars:     []string{"WITH_WAIT"},
		},
		&cli.BoolFlag{
			Name:        "with-workload",
			Aliases:     []string{"l"},
			Value:       false,
			Usage:       "indicates to validate with GPU workload",
			Destination: &withWorkloadFlag,
			EnvVars:     []string{"WITH_WORKLOAD"},
		},
		&cli.StringFlag{
			Name:        "component",
			Aliases:     []string{"c"},
			Value:       "",
			Usage:       "the name of the operator component to validate",
			Destination: &componentFlag,
			EnvVars:     []string{"COMPONENT"},
		},
		&cli.BoolFlag{
			Name:        "cleanup-all",
			Aliases:     []string{"r"},
			Value:       false,
			Usage:       "indicates to cleanup all previous validation status files",
			Destination: &cleanupAllFlag,
			EnvVars:     []string{"CLEANUP_ALL"},
		},
		&cli.StringFlag{
			Name:        "output-dir",
			Aliases:     []string{"o"},
			Value:       defaultStatusPath,
			Usage:       "output directory where all validation status files are created",
			Destination: &outputDirFlag,
			EnvVars:     []string{"OUTPUT_DIR"},
		},
		&cli.IntFlag{
			Name:        "sleep-interval-seconds",
			Aliases:     []string{"s"},
			Value:       defaultSleepIntervalSeconds,
			Usage:       "sleep interval in seconds between command retries",
			Destination: &sleepIntervalSecondsFlag,
			EnvVars:     []string{"SLEEP_INTERVAL_SECONDS"},
		},
		&cli.StringFlag{
			Name:        "mig-strategy",
			Aliases:     []string{"m"},
			Value:       migStrategySingle,
			Usage:       "MIG Strategy",
			Destination: &migStrategyFlag,
			EnvVars:     []string{"MIG_STRATEGY"},
		},
		&cli.IntFlag{
			Name:        "metrics-port",
			Aliases:     []string{"p"},
			Value:       defaultMetricsPort,
			Usage:       "port on which the metrics will be exposed. 0 means disabled.",
			Destination: &metricsPort,
			EnvVars:     []string{"METRICS_PORT"},
		},
	}

	// Handle signals
	go handleSignal()

	// invoke command
	err := c.Run(os.Args)
	if err != nil {
		log.SetOutput(os.Stderr)
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
}

func handleSignal() {
	// Handle signals
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt,
		syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT)

	s := <-stop
	log.Fatalf("Exiting due to signal [%v] notification for pid [%d]", s.String(), os.Getpid())
}

func validateFlags(c *cli.Context) error {
	if componentFlag == "" {
		return fmt.Errorf("invalid -c <component-name> flag: must not be empty string")
	}
	if !isValidComponent() {
		return fmt.Errorf("invalid -c <component-name> flag value: %s", componentFlag)
	}
	if componentFlag == "plugin" {
		if nodeNameFlag == "" {
			return fmt.Errorf("invalid -n <node-name> flag: must not be empty string for plugin validation")
		}
		if namespaceFlag == "" {
			return fmt.Errorf("invalid -ns <namespace> flag: must not be empty string for plugin validation")
		}
	}
	if componentFlag == "cuda" && namespaceFlag == "" {
		return fmt.Errorf("invalid -ns <namespace> flag: must not be empty string for cuda validation")
	}
	if componentFlag == "metrics" {
		if metricsPort == defaultMetricsPort {
			return fmt.Errorf("invalid -p <port> flag: must not be empty or 0 for the metrics component")
		}
		if nodeNameFlag == "" {
			return fmt.Errorf("invalid -n <node-name> flag: must not be empty string for metrics exporter")
		}
	}

	return nil
}

func isValidComponent() bool {
	switch componentFlag {
	case "driver":
		fallthrough
	case "toolkit":
		fallthrough
	case "cuda":
		fallthrough
	case "metrics":
		fallthrough
	case "plugin":
		return true
	case "mofed":
		return true
	default:
		return false
	}
}

func start(c *cli.Context) error {
	// if cleanup is requested, delete all existing status files(default)
	if cleanupAllFlag {
		// cleanup output directory and create again each time
		err := os.RemoveAll(outputDirFlag)
		if err != nil {
			if !os.IsNotExist(err) {
				return err
			}
		}
	}

	// create status directory
	err := os.Mkdir(outputDirFlag, 0755)
	if err != nil && !os.IsExist(err) {
		return err
	}

	switch componentFlag {
	case "driver":
		driver := &Driver{}
		err := driver.validate()
		if err != nil {
			return fmt.Errorf("error validating driver installation: %s", err)
		}
		return nil
	case "toolkit":
		toolkit := &Toolkit{}
		err := toolkit.validate()
		if err != nil {
			return fmt.Errorf("error validating toolkit installation: %s", err)
		}
		return nil
	case "cuda":
		cuda := &CUDA{}
		err := cuda.validate()
		if err != nil {
			return fmt.Errorf("error validating cuda workload: %s", err)
		}
		return nil
	case "plugin":
		plugin := &Plugin{}
		err := plugin.validate()
		if err != nil {
			return fmt.Errorf("error validating plugin installation: %s", err)
		}
		return nil
	case "mofed":
		mofed := &MOFED{}
		err := mofed.validate()
		if err != nil {
			return fmt.Errorf("error validating MOFED driver installation: %s", err)
		}
		return nil
	case "metrics":
		metrics := &Metrics{}
		err := metrics.run()
		if err != nil {
			return fmt.Errorf("error running validation-metrics exporter: %s", err)
		}
		return nil
	default:
		return fmt.Errorf("invalid component specified for validation: %s", componentFlag)
	}
}

func runCommand(command string, args []string, silent bool) error {
	cmd := exec.Command(command, args...)
	if !silent {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

func runCommandWithWait(command string, args []string, sleepSeconds int, silent bool) error {
	for {
		cmd := exec.Command(command, args...)
		if !silent {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}
		fmt.Printf("running command %s with args %v\n", command, args)
		err := cmd.Run()
		if err != nil {
			fmt.Printf("command failed, retrying after %d seconds\n", sleepSeconds)
			time.Sleep(time.Duration(sleepSeconds) * time.Second)
			continue
		}
		return nil
	}
}

func cleanupStatusFiles() error {
	command := "rm"
	args := []string{"-f", fmt.Sprintf("%s/*-ready", outputDirFlag)}
	err := runCommand(command, args, false)
	if err != nil {
		return fmt.Errorf("unable to cleanup status files: %s", err)
	}
	return nil
}

func (d *Driver) runValidation(silent bool) error {
	// invoke validation command
	command := "chroot"
	args := []string{"/run/nvidia/driver", "nvidia-smi"}

	if withWaitFlag {
		return runCommandWithWait(command, args, sleepIntervalSecondsFlag, silent)
	}

	return runCommand(command, args, silent)
}

func (d *Driver) validate() error {
	// delete status file is already present
	err := deleteStatusFile(outputDirFlag + "/" + driverStatusFile)
	if err != nil {
		return err
	}

	err = d.runValidation(false)
	if err != nil {
		fmt.Println("driver is not ready")
		return err
	}

	// create driver status file
	err = createStatusFile(outputDirFlag + "/" + driverStatusFile)
	if err != nil {
		return err
	}
	return nil
}

func createStatusFile(statusFile string) error {
	_, err := os.Create(statusFile)
	if err != nil {
		return fmt.Errorf("unable to create status file %s: %s", statusFile, err)
	}
	return nil
}

func deleteStatusFile(statusFile string) error {
	err := os.Remove(statusFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("unable to remove driver status file %s: %s", statusFile, err)
		}
		// status file already removed
	}
	return nil
}

func (t *Toolkit) validate() error {
	// delete status file is already present
	err := deleteStatusFile(outputDirFlag + "/" + toolkitStatusFile)
	if err != nil {
		return err
	}

	// invoke nvidia-smi command to check if container run with toolkit injected files
	command := "nvidia-smi"
	args := []string{}
	if withWaitFlag {
		err = runCommandWithWait(command, args, sleepIntervalSecondsFlag, false)
	} else {
		err = runCommand(command, args, false)
	}
	if err != nil {
		fmt.Println("toolkit is not ready")
		return err
	}

	// create toolkit status file
	err = createStatusFile(outputDirFlag + "/" + toolkitStatusFile)
	if err != nil {
		return err
	}
	return nil
}

func (p *Plugin) validate() error {
	// delete status file is already present
	err := deleteStatusFile(outputDirFlag + "/" + pluginStatusFile)
	if err != nil {
		return err
	}

	// enumerate node resources and ensure GPU devices are discovered.
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Errorf("Error getting config cluster - %s\n", err.Error())
		return err
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		log.Errorf("Error getting k8s client - %s\n", err.Error())
		return err
	}

	// update k8s client for the plugin
	p.setKubeClient(kubeClient)

	err = p.validateGPUResource()
	if err != nil {
		return err
	}

	if withWorkloadFlag {
		// workload test
		err = p.runWorkload()
		if err != nil {
			return err
		}
	}

	// create plugin status file
	err = createStatusFile(outputDirFlag + "/" + pluginStatusFile)
	if err != nil {
		return err
	}
	return nil
}

func (m *MOFED) validate() error {
	// If GPUDirectRDMA is disabled, skip validation
	if os.Getenv(GPUDirectRDMAEnabledEnvName) != "true" {
		log.Info("GPUDirect RDMA is disabled, skipping MOFED driver validation...")
		return nil
	}

	// Check node labels for Mellanox devices and MOFED driver status file
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Errorf("Error getting config cluster - %s\n", err.Error())
		return err
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		log.Errorf("Error getting k8s client - %s\n", err.Error())
		return err
	}

	// update k8s client for the mofed driver validation
	m.setKubeClient(kubeClient)

	present, err := m.isMellanoxDevicePresent()
	if err != nil {
		log.Errorf(err.Error())
		return err
	}
	if !present {
		log.Info("No Mellanox device label found, skipping MOFED driver validation...")
		return nil
	}

	// delete status file is already present
	err = deleteStatusFile(outputDirFlag + "/" + mofedStatusFile)
	if err != nil {
		return err
	}

	err = m.runValidation(false)
	if err != nil {
		return err
	}

	// delete status file is already present
	err = createStatusFile(outputDirFlag + "/" + mofedStatusFile)
	if err != nil {
		return err
	}
	return nil
}

func (m *MOFED) runValidation(silent bool) error {
	//check for mlx5_core module to be loaded
	command := "bash"
	args := []string{"-c", "lsmod | grep mlx5_core"}

	// If MOFED container is running then use readiness flag set by the driver container instead
	if os.Getenv(UseHostMOFEDEnvname) != "true" {
		args = []string{"-c", "stat /run/mellanox/drivers/.driver-ready"}
	}
	if withWaitFlag {
		return runCommandWithWait(command, args, sleepIntervalSecondsFlag, silent)
	}
	return runCommand(command, args, silent)
}

func (m *MOFED) setKubeClient(kubeClient kubernetes.Interface) {
	m.kubeClient = kubeClient
}

func (m *MOFED) isMellanoxDevicePresent() (bool, error) {
	node, err := getNode(m.kubeClient)
	if err != nil {
		return false, fmt.Errorf("unable to fetch node by name %s to check for Mellanox device label: %s", nodeNameFlag, err)
	}
	for key, value := range node.GetLabels() {
		if key == MellanoxDeviceLabelKey && value == "true" {
			return true, nil
		}
	}
	return false, nil
}

func (p *Plugin) runWorkload() error {
	// load podSpec
	pod, err := loadPodSpec(pluginWorkloadPodSpecPath)
	if err != nil {
		return err
	}

	pod.ObjectMeta.Namespace = namespaceFlag
	image := os.Getenv(validatorImageEnvName)
	pod.Spec.Containers[0].Image = image
	pod.Spec.InitContainers[0].Image = image

	imagePullPolicy := os.Getenv(validatorImagePullPolicyEnvName)
	if imagePullPolicy != "" {
		pod.Spec.Containers[0].ImagePullPolicy = v1.PullPolicy(imagePullPolicy)
		pod.Spec.InitContainers[0].ImagePullPolicy = v1.PullPolicy(imagePullPolicy)
	}

	if os.Getenv(validatorImagePullSecretsEnvName) != "" {
		pullSecrets := strings.Split(os.Getenv(validatorImagePullSecretsEnvName), ",")
		for _, secret := range pullSecrets {
			pod.Spec.ImagePullSecrets = append(pod.Spec.ImagePullSecrets, v1.LocalObjectReference{Name: secret})
		}
	}
	if os.Getenv(validatorRuntimeClassEnvName) != "" {
		runtimeClass := os.Getenv(validatorRuntimeClassEnvName)
		pod.Spec.RuntimeClassName = &runtimeClass
	}

	// update owner reference
	setOwnerReference(p.kubeClient, pod)

	// update podSpec with node name so it will just run on current node
	pod.Spec.NodeName = nodeNameFlag

	resourceName, err := p.getGPUResourceName()
	if err != nil {
		return err
	}

	gpuResource := v1.ResourceList{
		resourceName: resource.MustParse("1"),
	}

	pod.Spec.InitContainers[0].Resources.Limits = gpuResource
	pod.Spec.InitContainers[0].Resources.Requests = gpuResource
	opts := meta_v1.ListOptions{LabelSelector: labels.Set{"app": pluginValidatorLabelValue}.AsSelector().String(),
		FieldSelector: fields.Set{"spec.nodeName": nodeNameFlag}.AsSelector().String()}

	// check if plugin validation pod is already running and cleanup.
	podList, err := p.kubeClient.CoreV1().Pods(namespaceFlag).List(context.TODO(), opts)
	if err != nil {
		return fmt.Errorf("cannot list existing validation pods: %s", err)
	}

	if podList != nil && len(podList.Items) > 0 {
		propagation := meta_v1.DeletePropagationBackground
		gracePeriod := int64(0)
		options := meta_v1.DeleteOptions{PropagationPolicy: &propagation, GracePeriodSeconds: &gracePeriod}
		err = p.kubeClient.CoreV1().Pods(namespaceFlag).Delete(context.TODO(), podList.Items[0].ObjectMeta.Name, options)
		if err != nil {
			return fmt.Errorf("cannot delete previous validation pod: %s", err)
		}
	}

	// wait for plugin validation pod to be ready.
	newPod, err := p.kubeClient.CoreV1().Pods(namespaceFlag).Create(context.TODO(), pod, meta_v1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create plugin validation pod %s, err %+v", pod.ObjectMeta.Name, err)
	}

	// make sure its available
	err = waitForPod(p.kubeClient, newPod.ObjectMeta.Name, namespaceFlag)
	if err != nil {
		return err
	}
	return nil
}

func setOwnerReference(kubeClient kubernetes.Interface, pod *v1.Pod) error {
	// get owner of validator daemonset (which is ClusterPolicy)
	validatorDaemonset, err := kubeClient.AppsV1().DaemonSets(namespaceFlag).Get(context.TODO(), "nvidia-operator-validator", meta_v1.GetOptions{})
	if err != nil {
		return err
	}

	// update owner reference of plugin workload validation pod as ClusterPolicy for cleanup
	pod.SetOwnerReferences(validatorDaemonset.ObjectMeta.OwnerReferences)
	return nil
}

// waits for the pod to be created
func waitForPod(kubeClient kubernetes.Interface, name string, namespace string) error {
	for i := 0; i < podCreationWaitRetries; i++ {
		// check for the existence of the resource
		pod, err := kubeClient.CoreV1().Pods(namespace).Get(context.TODO(), name, meta_v1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get pod %s, err %+v", name, err)
		}
		if pod.Status.Phase != "Succeeded" {
			log.Infof("pod %s is curently in %s phase", name, pod.Status.Phase)
			time.Sleep(podCreationSleepIntervalSeconds * time.Second)
			continue
		}
		log.Infof("pod %s have run successfully", name)
		// successfully running
		return nil
	}
	return fmt.Errorf("gave up waiting for pod %s to be available", name)
}

func loadPodSpec(podSpecPath string) (*v1.Pod, error) {
	var pod v1.Pod
	manifest, err := ioutil.ReadFile(podSpecPath)
	if err != nil {
		panic(err)
	}
	s := json.NewYAMLSerializer(json.DefaultMetaFactory, scheme.Scheme,
		scheme.Scheme)
	reg, _ := regexp.Compile(`\b(\w*kind:\w*)\B.*\b`)

	kind := reg.FindString(string(manifest))
	slice := strings.Split(kind, ":")
	kind = strings.TrimSpace(slice[1])

	log.Debugf("Decoding for Kind %s in path: %s", kind, podSpecPath)
	_, _, err = s.Decode(manifest, nil, &pod)
	if err != nil {
		return nil, err
	}
	return &pod, nil
}

func (p *Plugin) countGPUResources() (int64, error) {
	// get node info to check discovered GPU resources
	node, err := getNode(p.kubeClient)
	if err != nil {
		return -1, fmt.Errorf("unable to fetch node by name %s to check for GPU resources: %s", nodeNameFlag, err)
	}

	count := int64(0)

	for resourceName, quantity := range node.Status.Capacity {
		if !strings.HasPrefix(string(resourceName), migGPUResourcePrefix) && !strings.HasPrefix(string(resourceName), genericGPUResourceType) {
			continue
		}

		count += quantity.Value()
	}
	return count, nil
}

func (p *Plugin) validateGPUResource() error {
	for retry := 1; retry <= gpuResourceDiscoveryWaitRetries; retry++ {
		// get node info to check discovered GPU resources
		node, err := getNode(p.kubeClient)
		if err != nil {
			return fmt.Errorf("unable to fetch node by name %s to check for GPU resources: %s", nodeNameFlag, err)
		}

		if _, present := p.isMIGResourcePresent(node.Status.Capacity); present {
			return nil
		}

		if p.isFullGPUResourcePresent(node.Status.Capacity) {
			return nil
		}

		log.Infof("GPU resources are not yet discovered by the node, retry: %d", retry)
		time.Sleep(gpuResourceDiscoveryIntervalSeconds * time.Second)
	}
	return fmt.Errorf("GPU resources are not discovered by the node")
}

func (p *Plugin) isMIGResourcePresent(resources v1.ResourceList) (v1.ResourceName, bool) {
	for resourceName, quantity := range resources {
		if strings.HasPrefix(string(resourceName), migGPUResourcePrefix) && quantity.Value() >= 1 {
			log.Debugf("Found MIG GPU resource name %s quantity %d", resourceName, quantity.Value())
			return resourceName, true
		}
	}
	return "", false
}

func (p *Plugin) isFullGPUResourcePresent(resources v1.ResourceList) bool {
	for resourceName, quantity := range resources {
		if strings.HasPrefix(string(resourceName), genericGPUResourceType) && quantity.Value() >= 1 {
			log.Debugf("Found GPU resource name %s quantity %d", resourceName, quantity.Value())
			return true
		}
	}
	return false
}

func (p *Plugin) getGPUResourceName() (v1.ResourceName, error) {
	// get node info to check allocatable GPU resources
	node, err := getNode(p.kubeClient)
	if err != nil {
		return "", fmt.Errorf("unable to fetch node by name %s to check for GPU resources: %s", nodeNameFlag, err)
	}

	// use mig resource if one is available to run workload
	if resourceName, present := p.isMIGResourcePresent(node.Status.Allocatable); present {
		return resourceName, nil
	}

	if p.isFullGPUResourcePresent(node.Status.Allocatable) {
		return genericGPUResourceType, nil
	}

	return "", fmt.Errorf("Unable to find any allocatable GPU resource")
}

func (p *Plugin) setKubeClient(kubeClient kubernetes.Interface) {
	p.kubeClient = kubeClient
}

func getNode(kubeClient kubernetes.Interface) (*v1.Node, error) {
	node, err := kubeClient.CoreV1().Nodes().Get(context.TODO(), nodeNameFlag, meta_v1.GetOptions{})
	if err != nil {
		log.Errorf("unable to get node with name %s, err %s", nodeNameFlag, err.Error())
		return nil, err
	}
	return node, nil
}

func (c *CUDA) validate() error {
	// delete status file is already present
	err := deleteStatusFile(outputDirFlag + "/" + cudaStatusFile)
	if err != nil {
		return err
	}

	// deploy workload pod for cuda validation
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Errorf("Error getting config cluster - %s\n", err.Error())
		return err
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		log.Errorf("Error getting k8s client - %s\n", err.Error())
		return err
	}

	// update k8s client for the plugin
	c.setKubeClient(kubeClient)

	// workload test
	err = c.runWorkload()
	if err != nil {
		return err
	}

	// create plugin status file
	err = createStatusFile(outputDirFlag + "/" + cudaStatusFile)
	if err != nil {
		return err
	}
	return nil
}

func (c *CUDA) setKubeClient(kubeClient kubernetes.Interface) {
	c.kubeClient = kubeClient
}

func (c *CUDA) runWorkload() error {
	// load podSpec
	pod, err := loadPodSpec(cudaWorkloadPodSpecPath)
	if err != nil {
		return err
	}
	pod.ObjectMeta.Namespace = namespaceFlag
	image := os.Getenv(validatorImageEnvName)
	pod.Spec.Containers[0].Image = image
	pod.Spec.InitContainers[0].Image = image

	imagePullPolicy := os.Getenv(validatorImagePullPolicyEnvName)
	if imagePullPolicy != "" {
		pod.Spec.Containers[0].ImagePullPolicy = v1.PullPolicy(imagePullPolicy)
		pod.Spec.InitContainers[0].ImagePullPolicy = v1.PullPolicy(imagePullPolicy)
	}

	if os.Getenv(validatorImagePullSecretsEnvName) != "" {
		pullSecrets := strings.Split(os.Getenv(validatorImagePullSecretsEnvName), ",")
		for _, secret := range pullSecrets {
			pod.Spec.ImagePullSecrets = append(pod.Spec.ImagePullSecrets, v1.LocalObjectReference{Name: secret})
		}
	}
	if os.Getenv(validatorRuntimeClassEnvName) != "" {
		runtimeClass := os.Getenv(validatorRuntimeClassEnvName)
		pod.Spec.RuntimeClassName = &runtimeClass
	}

	// update owner reference
	setOwnerReference(c.kubeClient, pod)

	// update podSpec with node name so it will just run on current node
	pod.Spec.NodeName = nodeNameFlag

	opts := meta_v1.ListOptions{LabelSelector: labels.Set{"app": cudaValidatorLabelValue}.AsSelector().String(),
		FieldSelector: fields.Set{"spec.nodeName": nodeNameFlag}.AsSelector().String()}

	// check if cuda workload pod is already running and cleanup.
	podList, err := c.kubeClient.CoreV1().Pods(namespaceFlag).List(context.TODO(), opts)
	if err != nil {
		return fmt.Errorf("cannot list existing validation pods: %s", err)
	}

	if podList != nil && len(podList.Items) > 0 {
		propagation := meta_v1.DeletePropagationBackground
		gracePeriod := int64(0)
		options := meta_v1.DeleteOptions{PropagationPolicy: &propagation, GracePeriodSeconds: &gracePeriod}
		err = c.kubeClient.CoreV1().Pods(namespaceFlag).Delete(context.TODO(), podList.Items[0].ObjectMeta.Name, options)
		if err != nil {
			return fmt.Errorf("cannot delete previous validation pod: %s", err)
		}
	}

	// wait for cuda workload pod to be ready.
	newPod, err := c.kubeClient.CoreV1().Pods(namespaceFlag).Create(context.TODO(), pod, meta_v1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create cuda validation pod %s, err %+v", pod.ObjectMeta.Name, err)
	}

	// make sure its available
	err = waitForPod(c.kubeClient, newPod.ObjectMeta.Name, namespaceFlag)
	if err != nil {
		return err
	}
	return nil
}

func (c *Metrics) run() error {
	m := NewNodeMetrics(metricsPort)

	return m.Run()
}
