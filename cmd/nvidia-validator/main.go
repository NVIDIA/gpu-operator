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
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/NVIDIA/go-nvlib/pkg/nvmdev"
	"github.com/NVIDIA/go-nvlib/pkg/nvpci"
	devchar "github.com/NVIDIA/nvidia-container-toolkit/cmd/nvidia-ctk/system/create-dev-char-symlinks"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert/yaml"
	cli "github.com/urfave/cli/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/info"
)

// Component of GPU operator
type Component interface {
	validate() error
	createStatusFile() error
	deleteStatusFile() error
}

// Driver component
type Driver struct {
	ctx context.Context
}

// NvidiaFs GDS Driver component
type NvidiaFs struct{}

// GDRCopy driver component
type GDRCopy struct{}

// NvidiaPeermem driver component
type NvidiaPeermem struct{}

// CUDA represents spec to run cuda workload
type CUDA struct {
	ctx        context.Context
	kubeClient kubernetes.Interface
}

// Plugin component
type Plugin struct {
	ctx        context.Context
	kubeClient kubernetes.Interface
}

// Toolkit component
type Toolkit struct{}

// MOFED represents spec to validate MOFED driver installation
type MOFED struct {
	ctx        context.Context
	kubeClient kubernetes.Interface
}

// Metrics represents spec to run metrics exporter
type Metrics struct {
	ctx context.Context
}

// VfioPCI represents spec to validate vfio-pci driver
type VfioPCI struct {
	ctx context.Context
}

// VGPUManager represents spec to validate vGPU Manager installation
type VGPUManager struct {
	ctx context.Context
}

// VGPUDevices represents spec to validate vGPU device creation
type VGPUDevices struct {
	ctx context.Context
}

// CCManager represents spec to validate CC Manager installation
type CCManager struct {
	ctx        context.Context
	kubeClient kubernetes.Interface
}

var (
	kubeconfigFlag                string
	nodeNameFlag                  string
	namespaceFlag                 string
	withWaitFlag                  bool
	withWorkloadFlag              bool
	componentFlag                 string
	cleanupAllFlag                bool
	outputDirFlag                 string
	sleepIntervalSecondsFlag      int
	migStrategyFlag               string
	metricsPort                   int
	defaultGPUWorkloadConfigFlag  string
	disableDevCharSymlinkCreation bool
	hostRootFlag                  string
	driverInstallDirFlag          string
	driverInstallDirCtrPathFlag   string
)

// defaultGPUWorkloadConfig is "vm-passthrough" unless
// overridden by defaultGPUWorkloadConfigFlag
var defaultGPUWorkloadConfig = gpuWorkloadConfigVMPassthrough

const (
	// defaultStatusPath indicates directory to create all validation status files
	defaultStatusPath = "/run/nvidia/validations"
	// defaultSleepIntervalSeconds indicates sleep interval in seconds between validation command retries
	defaultSleepIntervalSeconds = 5
	// defaultMetricsPort indicates the port on which the metrics will be exposed.
	defaultMetricsPort = 0
	// hostDevCharPath indicates the path in the container where the host '/dev/char' directory is mounted to
	hostDevCharPath = "/host-dev-char"
	// nvidiaModuleRefcntPath is the path to check if the nvidia kernel module is loaded
	nvidiaModuleRefcntPath = "/sys/module/nvidia/refcnt"
	// defaultDriverInstallDir indicates the default path on the host where the driver container installation is made available
	defaultDriverInstallDir = "/run/nvidia/driver"
	// defaultDriverInstallDirCtrPath indicates the default path where the NVIDIA driver install dir is mounted in the container
	defaultDriverInstallDirCtrPath = "/run/nvidia/driver"
	// driverContainerStatusFilePath indicates the path to the driver container status file
	// which also contains additional drivers status flags
	driverContainerStatusFilePath = defaultStatusPath + "/.driver-ctr-ready"
	// driverStatusFile indicates status file for containerized driver readiness
	driverStatusFile = "driver-ready"
	// nvidiaFsStatusFile indicates status file for nvidia-fs driver readiness
	nvidiaFsStatusFile = "nvidia-fs-ready"
	// gdrCopyStatusFile indicates status file for GDRCopy driver (gdrdrv) readiness
	gdrCopyStatusFile = "gdrcopy-ready"
	// nvidiaPeermemStatusFile indicates status file for nvidia-peermem driver readiness
	nvidiaPeermemStatusFile = "nvidia-peermem-ready"
	// toolkitStatusFile indicates status file for toolkit readiness
	toolkitStatusFile = "toolkit-ready"
	// pluginStatusFile indicates status file for plugin readiness
	pluginStatusFile = "plugin-ready"
	// cudaStatusFile indicates status file for cuda readiness
	cudaStatusFile = "cuda-ready"
	// mofedStatusFile indicates status file for mofed driver readiness
	mofedStatusFile = "mofed-ready"
	// vfioPCIStatusFile indicates status file for vfio-pci driver readiness
	vfioPCIStatusFile = "vfio-pci-ready"
	// vGPUManagerStatusFile indicates status file for vGPU Manager driver readiness
	vGPUManagerStatusFile = "vgpu-manager-ready"
	// hostVGPUManagerStatusFile indicates status file for host vGPU Manager driver readiness
	hostVGPUManagerStatusFile = "host-vgpu-manager-ready"
	// vGPUDevicesStatusFile is name of the file which indicates vGPU Manager is installed and vGPU devices have been created
	vGPUDevicesStatusFile = "vgpu-devices-ready"
	// ccManagerStatusFile indicates status file for cc-manager readiness
	ccManagerStatusFile = "cc-manager-ready"
	// workloadTypeStatusFile is the name of the file which specifies the workload type configured for the node
	workloadTypeStatusFile = "workload-type"
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
	// migStrategySingle indicates mixed MIG strategy
	migStrategySingle = "single"
	// pluginWorkloadPodSpecPath indicates path to plugin validation pod definition
	pluginWorkloadPodSpecPath = "/opt/validator/manifests/plugin-workload-validation.yaml"
	// cudaWorkloadPodSpecPath indicates path to cuda validation pod definition
	cudaWorkloadPodSpecPath = "/opt/validator/manifests/cuda-workload-validation.yaml"
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
	// TODO: create a common package to share these variables between operator and validator
	gpuWorkloadConfigLabelKey      = "nvidia.com/gpu.workload.config"
	gpuWorkloadConfigContainer     = "container"
	gpuWorkloadConfigVMPassthrough = "vm-passthrough"
	gpuWorkloadConfigVMVgpu        = "vm-vgpu"
	// CCCapableLabelKey represents NFD label name to indicate if the node is capable to run CC workloads
	CCCapableLabelKey = "nvidia.com/cc.capable"
	// appComponentLabelKey indicates the label key of the component
	appComponentLabelKey = "app.kubernetes.io/component"
	// wslNvidiaSMIPath indicates the path to the nvidia-smi binary on WSL
	wslNvidiaSMIPath = "/usr/lib/wsl/lib/nvidia-smi"
	// shell indicates what shell to use when invoking commands in a subprocess
	shell = "sh"
	// defaultVFWaitTimeout is the default timeout for waiting for VFs to be created
	defaultVFWaitTimeout = 5 * time.Minute
	// constants for driver components
	GDRCOPY       = "gdrcopy"
	NVIDIAFS      = "nvidia-fs"
	NVIDIAPEERMEM = "nvidia-peermem"
)

func main() {
	c := cli.Command{}

	c.Before = validateFlags
	c.Action = start
	c.Version = info.GetVersionString()

	c.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:        "kubeconfig",
			Value:       "",
			Usage:       "absolute path to the kubeconfig file",
			Destination: &kubeconfigFlag,
			Sources:     cli.EnvVars("KUBECONFIG"),
		},
		&cli.StringFlag{
			Name:        "node-name",
			Aliases:     []string{"n"},
			Value:       "",
			Usage:       "the name of the node to deploy plugin validation pod",
			Destination: &nodeNameFlag,
			Sources:     cli.EnvVars("NODE_NAME"),
		},
		&cli.StringFlag{
			Name:        "namespace",
			Aliases:     []string{"ns"},
			Value:       "",
			Usage:       "the namespace in which the operator resources are deployed",
			Destination: &namespaceFlag,
			Sources:     cli.EnvVars("OPERATOR_NAMESPACE"),
		},
		&cli.BoolFlag{
			Name:        "with-wait",
			Aliases:     []string{"w"},
			Value:       false,
			Usage:       "indicates to wait for validation to complete successfully",
			Destination: &withWaitFlag,
			Sources:     cli.EnvVars("WITH_WAIT"),
		},
		&cli.BoolFlag{
			Name:        "with-workload",
			Aliases:     []string{"l"},
			Value:       true,
			Usage:       "indicates to validate with GPU workload",
			Destination: &withWorkloadFlag,
			Sources:     cli.EnvVars("WITH_WORKLOAD"),
		},
		&cli.StringFlag{
			Name:        "component",
			Aliases:     []string{"c"},
			Value:       "",
			Usage:       "the name of the operator component to validate",
			Destination: &componentFlag,
			Sources:     cli.EnvVars("COMPONENT"),
		},
		&cli.BoolFlag{
			Name:        "cleanup-all",
			Aliases:     []string{"r"},
			Value:       false,
			Usage:       "indicates to cleanup all previous validation status files",
			Destination: &cleanupAllFlag,
			Sources:     cli.EnvVars("CLEANUP_ALL"),
		},
		&cli.StringFlag{
			Name:        "output-dir",
			Aliases:     []string{"o"},
			Value:       defaultStatusPath,
			Usage:       "output directory where all validation status files are created",
			Destination: &outputDirFlag,
			Sources:     cli.EnvVars("OUTPUT_DIR"),
		},
		&cli.IntFlag{
			Name:        "sleep-interval-seconds",
			Aliases:     []string{"s"},
			Value:       defaultSleepIntervalSeconds,
			Usage:       "sleep interval in seconds between command retries",
			Destination: &sleepIntervalSecondsFlag,
			Sources:     cli.EnvVars("SLEEP_INTERVAL_SECONDS"),
		},
		&cli.StringFlag{
			Name:        "mig-strategy",
			Aliases:     []string{"m"},
			Value:       migStrategySingle,
			Usage:       "MIG Strategy",
			Destination: &migStrategyFlag,
			Sources:     cli.EnvVars("MIG_STRATEGY"),
		},
		&cli.IntFlag{
			Name:        "metrics-port",
			Aliases:     []string{"p"},
			Value:       defaultMetricsPort,
			Usage:       "port on which the metrics will be exposed. 0 means disabled.",
			Destination: &metricsPort,
			Sources:     cli.EnvVars("METRICS_PORT"),
		},
		&cli.StringFlag{
			Name:        "default-gpu-workload-config",
			Aliases:     []string{"g"},
			Value:       "",
			Usage:       "default GPU workload config. determines what components to validate by default when sandbox workloads are enabled in the cluster.",
			Destination: &defaultGPUWorkloadConfigFlag,
			Sources:     cli.EnvVars("DEFAULT_GPU_WORKLOAD_CONFIG"),
		},
		&cli.BoolFlag{
			Name:        "disable-dev-char-symlink-creation",
			Value:       false,
			Usage:       "disable creation of symlinks under /dev/char corresponding to NVIDIA character devices",
			Destination: &disableDevCharSymlinkCreation,
			Sources:     cli.EnvVars("DISABLE_DEV_CHAR_SYMLINK_CREATION"),
		},
		&cli.StringFlag{
			Name:        "host-root",
			Value:       "/",
			Usage:       "root path of the underlying host",
			Destination: &hostRootFlag,
			Sources:     cli.EnvVars("HOST_ROOT"),
		},
		&cli.StringFlag{
			Name:        "driver-install-dir",
			Value:       defaultDriverInstallDir,
			Usage:       "the path on the host where a containerized NVIDIA driver installation is made available",
			Destination: &driverInstallDirFlag,
			Sources:     cli.EnvVars("DRIVER_INSTALL_DIR"),
		},
		&cli.StringFlag{
			Name:        "driver-install-dir-ctr-path",
			Value:       defaultDriverInstallDirCtrPath,
			Usage:       "the path where the NVIDIA driver install dir is mounted in the container",
			Destination: &driverInstallDirCtrPathFlag,
			Sources:     cli.EnvVars("DRIVER_INSTALL_DIR_CTR_PATH"),
		},
	}

	// Log version info
	log.Infof("version: %s", c.Version)

	// Handle signals
	go handleSignal()

	// invoke command
	err := c.Run(context.Background(), os.Args)
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

func validateFlags(ctx context.Context, cli *cli.Command) (context.Context, error) {
	if componentFlag == "" {
		return ctx, fmt.Errorf("invalid -c <component-name> flag: must not be empty string")
	}
	if !isValidComponent() {
		return ctx, fmt.Errorf("invalid -c <component-name> flag value: %s", componentFlag)
	}
	if componentFlag == "plugin" {
		if nodeNameFlag == "" {
			return ctx, fmt.Errorf("invalid -n <node-name> flag: must not be empty string for plugin validation")
		}
		if namespaceFlag == "" {
			return ctx, fmt.Errorf("invalid -ns <namespace> flag: must not be empty string for plugin validation")
		}
	}
	if componentFlag == "cuda" && namespaceFlag == "" {
		return ctx, fmt.Errorf("invalid -ns <namespace> flag: must not be empty string for cuda validation")
	}
	if componentFlag == "metrics" {
		if metricsPort == defaultMetricsPort {
			return ctx, fmt.Errorf("invalid -p <port> flag: must not be empty or 0 for the metrics component")
		}
		if nodeNameFlag == "" {
			return ctx, fmt.Errorf("invalid -n <node-name> flag: must not be empty string for metrics exporter")
		}
	}
	if nodeNameFlag == "" && (componentFlag == "vfio-pci" || componentFlag == "vgpu-manager" || componentFlag == "vgpu-devices") {
		return ctx, fmt.Errorf("invalid -n <node-name> flag: must not be empty string for %s validation", componentFlag)
	}

	return ctx, nil
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
		fallthrough
	case "mofed":
		fallthrough
	case "vfio-pci":
		fallthrough
	case "vgpu-manager":
		fallthrough
	case "vgpu-devices":
		fallthrough
	case "cc-manager":
		fallthrough
	case NVIDIAFS:
		fallthrough
	case GDRCOPY:
		fallthrough
	case NVIDIAPEERMEM:
		return true
	default:
		return false
	}
}

func isValidWorkloadConfig(config string) bool {
	return config == gpuWorkloadConfigContainer ||
		config == gpuWorkloadConfigVMPassthrough ||
		config == gpuWorkloadConfigVMVgpu
}

func getWorkloadConfig(ctx context.Context) (string, error) {
	// check if default workload is overridden by flag
	if isValidWorkloadConfig(defaultGPUWorkloadConfigFlag) {
		defaultGPUWorkloadConfig = defaultGPUWorkloadConfigFlag
	}

	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		return "", fmt.Errorf("error getting cluster config - %s", err.Error())
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return "", fmt.Errorf("error getting k8s client - %w", err)
	}

	node, err := getNode(ctx, kubeClient)
	if err != nil {
		return "", fmt.Errorf("error getting node labels - %w", err)
	}

	labels := node.GetLabels()
	value, ok := labels[gpuWorkloadConfigLabelKey]
	if !ok {
		log.Infof("No %s label found; using default workload config: %s", gpuWorkloadConfigLabelKey, defaultGPUWorkloadConfig)
		return defaultGPUWorkloadConfig, nil
	}
	if !isValidWorkloadConfig(value) {
		log.Warnf("%s is an invalid workload config; using default workload config: %s", value, defaultGPUWorkloadConfig)
		return defaultGPUWorkloadConfig, nil
	}
	return value, nil
}

func start(ctx context.Context, cli *cli.Command) error {
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

	return validateComponent(ctx, componentFlag)
}

func validateComponent(ctx context.Context, componentFlag string) error {
	switch componentFlag {
	case "driver":
		driver := &Driver{
			ctx: ctx,
		}
		err := driver.validate()
		if err != nil {
			return fmt.Errorf("error validating driver installation: %w", err)
		}
		return nil
	case NVIDIAFS:
		nvidiaFs := &NvidiaFs{}
		err := nvidiaFs.validate()
		if err != nil {
			return fmt.Errorf("error validating nvidia-fs driver installation: %w", err)
		}
		return nil
	case GDRCOPY:
		gdrcopy := &GDRCopy{}
		err := gdrcopy.validate()
		if err != nil {
			return fmt.Errorf("error validating gdrcopy driver installation: %w", err)
		}
		return nil
	case NVIDIAPEERMEM:
		nvidiaPeermem := &NvidiaPeermem{}
		err := nvidiaPeermem.validate()
		if err != nil {
			return fmt.Errorf("error validating nvidia-peermem driver installation: %w", err)
		}
		return nil
	case "toolkit":
		toolkit := &Toolkit{}
		err := toolkit.validate()
		if err != nil {
			return fmt.Errorf("error validating toolkit installation: %w", err)
		}
		return nil
	case "cuda":
		cuda := &CUDA{
			ctx: ctx,
		}
		err := cuda.validate()
		if err != nil {
			return fmt.Errorf("error validating cuda workload: %w", err)
		}
		return nil
	case "plugin":
		plugin := &Plugin{
			ctx: ctx,
		}
		err := plugin.validate()
		if err != nil {
			return fmt.Errorf("error validating plugin installation: %w", err)
		}
		return nil
	case "mofed":
		mofed := &MOFED{
			ctx: ctx,
		}
		err := mofed.validate()
		if err != nil {
			return fmt.Errorf("error validating MOFED driver installation: %s", err)
		}
		return nil
	case "metrics":
		metrics := &Metrics{
			ctx: ctx,
		}
		err := metrics.run()
		if err != nil {
			return fmt.Errorf("error running validation-metrics exporter: %s", err)
		}
		return nil
	case "vfio-pci":
		vfioPCI := &VfioPCI{
			ctx: ctx,
		}
		err := vfioPCI.validate()
		if err != nil {
			return fmt.Errorf("error validating vfio-pci driver installation: %w", err)
		}
		return nil
	case "vgpu-manager":
		vGPUManager := &VGPUManager{
			ctx: ctx,
		}
		err := vGPUManager.validate()
		if err != nil {
			return fmt.Errorf("error validating vGPU Manager installation: %w", err)
		}
		return nil
	case "vgpu-devices":
		vGPUDevices := &VGPUDevices{
			ctx: ctx,
		}
		err := vGPUDevices.validate()
		if err != nil {
			return fmt.Errorf("error validating vGPU devices: %s", err)
		}
		return nil
	case "cc-manager":
		CCManager := &CCManager{
			ctx: ctx,
		}
		err := CCManager.validate()
		if err != nil {
			return fmt.Errorf("error validating CC Manager installation: %w", err)
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
			log.Warningf("error running command: %v", err)
			fmt.Printf("command failed, retrying after %d seconds\n", sleepSeconds)
			time.Sleep(time.Duration(sleepSeconds) * time.Second)
			continue
		}
		return nil
	}
}

// prependPathListEnvvar prepends a specified list of strings to a specified envvar and returns its value.
func prependPathListEnvvar(envvar string, prepend ...string) string {
	if len(prepend) == 0 {
		return os.Getenv(envvar)
	}
	current := filepath.SplitList(os.Getenv(envvar))
	return strings.Join(append(prepend, current...), string(filepath.ListSeparator))
}

// setEnvVar adds or updates an envar to the list of specified envvars and returns it.
func setEnvVar(envvars []string, key, value string) []string {
	var updated []string
	for _, envvar := range envvars {
		pair := strings.SplitN(envvar, "=", 2)
		if pair[0] == key {
			continue
		}
		updated = append(updated, envvar)
	}
	return append(updated, fmt.Sprintf("%s=%s", key, value))
}

// For driver container installs, check existence of .driver-ctr-ready to confirm running driver
// container has completed and is in Ready state.
func assertDriverContainerReady(silent bool) error {
	command := shell
	args := []string{"-c", fmt.Sprintf("stat %s", driverContainerStatusFilePath)}

	if withWaitFlag {
		return runCommandWithWait(command, args, sleepIntervalSecondsFlag, silent)
	}

	return runCommand(command, args, silent)
}

// isDriverManagedByOperator determines if the NVIDIA driver is managed by the GPU Operator.
// We check if at least one driver DaemonSet exists in the operator namespace that is
// owned by the ClusterPolicy or NVIDIADriver controllers.
func isDriverManagedByOperator(ctx context.Context) (bool, error) {
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		return false, fmt.Errorf("error getting cluster config: %w", err)
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return false, fmt.Errorf("error getting k8s client: %w", err)
	}

	opts := meta_v1.ListOptions{LabelSelector: labels.Set{appComponentLabelKey: "nvidia-driver"}.AsSelector().String()}
	dsList, err := kubeClient.AppsV1().DaemonSets(namespaceFlag).List(ctx, opts)
	if err != nil {
		return false, fmt.Errorf("error listing daemonsets: %w", err)
	}

	for i := range dsList.Items {
		ds := dsList.Items[i]
		owner := meta_v1.GetControllerOf(&ds)
		if owner == nil {
			continue
		}
		if strings.HasPrefix(owner.APIVersion, "nvidia.com/") && (owner.Kind == nvidiav1.ClusterPolicyCRDName || owner.Kind == nvidiav1alpha1.NVIDIADriverCRDName) {
			return true, nil
		}
	}

	return false, nil
}

func validateHostDriver(silent bool) error {
	log.Info("Attempting to validate a pre-installed driver on the host")
	if fileInfo, err := os.Lstat(filepath.Join("/host", wslNvidiaSMIPath)); err == nil && fileInfo.Size() != 0 {
		log.Infof("WSL2 system detected, assuming driver is pre-installed")
		disableDevCharSymlinkCreation = true
		return nil
	}
	fileInfo, err := os.Lstat("/host/usr/bin/nvidia-smi")
	if err != nil {
		return fmt.Errorf("no 'nvidia-smi' file present on the host: %w", err)
	}
	if fileInfo.Size() == 0 {
		return fmt.Errorf("empty 'nvidia-smi' file found on the host")
	}
	command := "chroot"
	args := []string{"/host", "nvidia-smi"}

	return runCommand(command, args, silent)
}

func validateDriverContainer(silent bool, ctx context.Context) error {
	driverManagedByOperator, err := isDriverManagedByOperator(ctx)
	if err != nil {
		return fmt.Errorf("error checking if driver is managed by GPU Operator: %w", err)
	}

	if driverManagedByOperator {
		log.Infof("Driver is not pre-installed on the host and is managed by GPU Operator. Checking driver container status.")
		if err := assertDriverContainerReady(silent); err != nil {
			return fmt.Errorf("error checking driver container status: %w", err)
		}
	}

	driverRoot := root(driverInstallDirCtrPathFlag)

	validateDriver := func(silent bool) error {
		driverLibraryPath, err := driverRoot.getDriverLibraryPath()
		if err != nil {
			return fmt.Errorf("failed to locate driver libraries: %w", err)
		}

		nvidiaSMIPath, err := driverRoot.getNvidiaSMIPath()
		if err != nil {
			return fmt.Errorf("failed to locate nvidia-smi: %w", err)
		}
		cmd := exec.Command(nvidiaSMIPath)
		// In order for nvidia-smi to run, we need to update LD_PRELOAD to include the path to libnvidia-ml.so.1.
		cmd.Env = setEnvVar(os.Environ(), "LD_PRELOAD", prependPathListEnvvar("LD_PRELOAD", driverLibraryPath))
		if !silent {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}
		return cmd.Run()
	}

	for {
		log.Info("Attempting to validate a driver container installation")
		err := validateDriver(silent)
		if err != nil {
			if !withWaitFlag {
				return fmt.Errorf("error validating driver: %w", err)
			}
			log.Warningf("failed to validate the driver, retrying after %d seconds\n", sleepIntervalSecondsFlag)
			time.Sleep(time.Duration(sleepIntervalSecondsFlag) * time.Second)
			continue
		}
		return nil
	}
}

func (d *Driver) runValidation(silent bool) (driverInfo, error) {
	err := validateHostDriver(silent)
	if err == nil {
		log.Info("Detected a pre-installed driver on the host")
		return getDriverInfo(true, hostRootFlag, hostRootFlag, "/host"), nil
	}

	err = validateDriverContainer(silent, d.ctx)
	if err != nil {
		return driverInfo{}, err
	}

	err = validateAdditionalDriverComponents(d.ctx, driverContainerStatusFilePath)
	if err != nil {
		return driverInfo{}, err
	}

	return getDriverInfo(false, hostRootFlag, driverInstallDirFlag, driverInstallDirCtrPathFlag), nil
}

func validateAdditionalDriverComponents(ctx context.Context, statusFilePath string) error {
	data, err := os.ReadFile(statusFilePath)
	if err != nil {
		return err
	}

	supportedFeatures := map[string]string{
		"GDRCOPY_ENABLED":         GDRCOPY,
		"GDS_ENABLED":             NVIDIAFS,
		"GPU_DIRECT_RDMA_ENABLED": NVIDIAPEERMEM,
	}

	features := map[string]bool{}
	if err := yaml.Unmarshal(data, &features); err != nil {
		return err
	}

	for k, enabled := range features {
		if !enabled {
			log.Debugf("%s is disabled, skipping...", k)
			continue
		}

		component, ok := supportedFeatures[k]
		if !ok {
			log.Infof("unsupported feature flag: %s, skipping...", k)
			continue
		}

		log.Infof("Validating additional driver component: %s", component)
		if err := validateComponent(ctx, component); err != nil {
			return err
		}
	}

	return nil
}

func (d *Driver) validate() error {
	// delete driver status file if already present
	err := deleteStatusFile(outputDirFlag + "/" + driverStatusFile)
	if err != nil {
		return err
	}

	driverInfo, err := d.runValidation(false)
	if err != nil {
		log.Errorf("driver is not ready: %v", err)
		return err
	}

	err = createDevCharSymlinks(driverInfo, disableDevCharSymlinkCreation)
	if err != nil {
		msg := strings.Join([]string{
			"Failed to create symlinks under /dev/char that point to all possible NVIDIA character devices.",
			"The existence of these symlinks is required to address the following bug:",
			"",
			"    https://github.com/NVIDIA/gpu-operator/issues/430",
			"",
			"This bug impacts container runtimes configured with systemd cgroup management enabled.",
			"To disable the symlink creation, set the following envvar in ClusterPolicy:",
			"",
			"    validator:",
			"      driver:",
			"        env:",
			"        - name: DISABLE_DEV_CHAR_SYMLINK_CREATION",
			"          value: \"true\""}, "\n")
		return fmt.Errorf("%w\n\n%s", err, msg)
	}

	return d.createStatusFile(driverInfo)
}

func (d *Driver) createStatusFile(driverInfo driverInfo) error {
	statusFileContent := strings.Join([]string{
		fmt.Sprintf("IS_HOST_DRIVER=%t", driverInfo.isHostDriver),
		fmt.Sprintf("NVIDIA_DRIVER_ROOT=%s", driverInfo.driverRoot),
		fmt.Sprintf("DRIVER_ROOT_CTR_PATH=%s", driverInfo.driverRootCtrPath),
		fmt.Sprintf("NVIDIA_DEV_ROOT=%s", driverInfo.devRoot),
		fmt.Sprintf("DEV_ROOT_CTR_PATH=%s", driverInfo.devRootCtrPath),
	}, "\n") + "\n"

	// create driver status file
	return createStatusFileWithContent(outputDirFlag+"/"+driverStatusFile, statusFileContent)
}

// isNvidiaModuleLoaded checks if NVIDIA kernel module is already loaded in kernel memory.
func isNvidiaModuleLoaded() bool {
	// Check if the nvidia module is loaded by checking if nvidiaModuleRefcntPath exists
	if _, err := os.Stat(nvidiaModuleRefcntPath); err == nil {
		refcntData, err := os.ReadFile(nvidiaModuleRefcntPath)
		if err == nil {
			refcnt := strings.TrimSpace(string(refcntData))
			log.Infof("NVIDIA kernel module already loaded in kernel memory (refcnt=%s)", refcnt)
			return true
		}
	}
	return false
}

// createDevCharSymlinks creates symlinks in /host-dev-char that point to all possible NVIDIA devices nodes.
func createDevCharSymlinks(driverInfo driverInfo, disableDevCharSymlinkCreation bool) error {
	if disableDevCharSymlinkCreation {
		log.WithField("disableDevCharSymlinkCreation", true).
			Info("skipping the creation of symlinks under /dev/char that correspond to NVIDIA character devices")
		return nil
	}

	log.Info("creating symlinks under /dev/char that correspond to NVIDIA character devices")

	// Check if NVIDIA module is already loaded in kernel memory.
	// If it is, we don't need to run modprobe (which would fail if modules aren't in /lib/modules/).
	// This handles the case where the driver container performed a userspace-only install
	// after detecting that module was already loaded from a previous boot.
	moduleAlreadyLoaded := isNvidiaModuleLoaded()

	// Only attempt to load NVIDIA kernel modules when:
	// 1. Module is not already loaded in kernel memory, AND
	// 2. We can chroot into driverRoot to run modprobe
	loadKernelModules := !moduleAlreadyLoaded && (driverInfo.isHostDriver || (driverInfo.devRoot == driverInfo.driverRoot))

	// driverRootCtrPath is the path of the driver install dir in the container. This will either be
	// driverInstallDirCtrPathFlag or '/host'.
	// Note, if we always mounted the driver install dir to '/driver-root' in the validation container
	// instead, then we could simplify to always use driverInfo.driverRootCtrPath -- which would be
	// either '/host' or '/driver-root', both paths would exist in the validation container.
	driverRootCtrPath := driverInstallDirCtrPathFlag
	if driverInfo.isHostDriver {
		driverRootCtrPath = "/host"
	}

	// We now create the symlinks in /dev/char.
	creator, err := devchar.NewSymlinkCreator(
		devchar.WithDriverRoot(driverRootCtrPath),
		devchar.WithDevRoot(driverInfo.devRoot),
		devchar.WithDevCharPath(hostDevCharPath),
		devchar.WithCreateAll(true),
		devchar.WithCreateDeviceNodes(true),
		devchar.WithLoadKernelModules(loadKernelModules),
	)
	if err != nil {
		return fmt.Errorf("error creating symlink creator: %w", err)
	}

	err = creator.CreateLinks()
	if err != nil {
		return fmt.Errorf("error creating symlinks: %w", err)
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

func createStatusFileWithContent(statusFile string, content string) error {
	dir := filepath.Dir(statusFile)
	tmpFile, err := os.CreateTemp(dir, filepath.Base(statusFile)+".*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary status file: %w", err)
	}
	_, err = tmpFile.WriteString(content)
	tmpFile.Close()
	if err != nil {
		return fmt.Errorf("failed to write temporary status file: %w", err)
	}
	defer func() {
		_ = os.Remove(tmpFile.Name())
	}()

	if err := os.Rename(tmpFile.Name(), statusFile); err != nil {
		return fmt.Errorf("error moving temporary file to '%s': %w", statusFile, err)
	}
	return nil
}

func deleteStatusFile(statusFile string) error {
	err := os.Remove(statusFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("unable to remove driver status file %s: %w", statusFile, err)
		}
		// status file already removed
	}
	return nil
}

func (n *NvidiaFs) validate() error {
	// delete driver status file if already present
	err := deleteStatusFile(outputDirFlag + "/" + nvidiaFsStatusFile)
	if err != nil {
		return err
	}

	err = n.runValidation(false)
	if err != nil {
		fmt.Println("nvidia-fs driver is not ready")
		return err
	}

	// create driver status file
	err = createStatusFile(outputDirFlag + "/" + nvidiaFsStatusFile)
	if err != nil {
		return err
	}
	return nil
}

func (n *NvidiaFs) runValidation(silent bool) error {
	// check for nvidia_fs module to be loaded
	command := shell
	args := []string{"-c", "lsmod | grep nvidia_fs"}

	if withWaitFlag {
		return runCommandWithWait(command, args, sleepIntervalSecondsFlag, silent)
	}
	return runCommand(command, args, silent)
}

func (g *GDRCopy) validate() error {
	// delete driver status file if already present
	err := deleteStatusFile(outputDirFlag + "/" + gdrCopyStatusFile)
	if err != nil {
		return err
	}

	err = g.runValidation(false)
	if err != nil {
		log.Info("gdrcopy driver is not ready")
		return err
	}

	// create driver status file
	err = createStatusFile(outputDirFlag + "/" + gdrCopyStatusFile)
	if err != nil {
		return err
	}
	return nil
}

func (g *GDRCopy) runValidation(silent bool) error {
	// check for gdrdrv module to be loaded
	command := shell
	args := []string{"-c", "lsmod | grep -E '^gdrdrv\\s'"}

	if withWaitFlag {
		return runCommandWithWait(command, args, sleepIntervalSecondsFlag, silent)
	}
	return runCommand(command, args, silent)
}

func (n *NvidiaPeermem) validate() error {
	// delete driver status file if already present
	err := deleteStatusFile(outputDirFlag + "/" + nvidiaPeermemStatusFile)
	if err != nil {
		return err
	}

	err = n.runValidation(false)
	if err != nil {
		log.Info("nvidia-peermem driver is not ready")
		return err
	}

	// create driver status file
	err = createStatusFile(outputDirFlag + "/" + nvidiaPeermemStatusFile)
	if err != nil {
		return err
	}
	return nil
}

func (n *NvidiaPeermem) runValidation(silent bool) error {
	// check for nvidia_peermem module to be loaded
	command := shell
	args := []string{"-c", "lsmod | grep -E '^nvidia_peermem\\s'"}

	if withWaitFlag {
		return runCommandWithWait(command, args, sleepIntervalSecondsFlag, silent)
	}
	return runCommand(command, args, silent)
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
		log.Errorf("Error trying to retrieve Mellanox device - %s\n", err.Error())
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
	// check for mlx5_core module to be loaded
	command := shell
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
	node, err := getNode(m.ctx, m.kubeClient)
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
	ctx := p.ctx
	// load podSpec
	pod, err := loadPodSpec(pluginWorkloadPodSpecPath)
	if err != nil {
		return err
	}

	pod.Namespace = namespaceFlag
	image := os.Getenv(validatorImageEnvName)
	pod.Spec.Containers[0].Image = image
	pod.Spec.InitContainers[0].Image = image

	imagePullPolicy := os.Getenv(validatorImagePullPolicyEnvName)
	if imagePullPolicy != "" {
		pod.Spec.Containers[0].ImagePullPolicy = corev1.PullPolicy(imagePullPolicy)
		pod.Spec.InitContainers[0].ImagePullPolicy = corev1.PullPolicy(imagePullPolicy)
	}

	if os.Getenv(validatorImagePullSecretsEnvName) != "" {
		pullSecrets := strings.Split(os.Getenv(validatorImagePullSecretsEnvName), ",")
		for _, secret := range pullSecrets {
			pod.Spec.ImagePullSecrets = append(pod.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: secret})
		}
	}
	if os.Getenv(validatorRuntimeClassEnvName) != "" {
		runtimeClass := os.Getenv(validatorRuntimeClassEnvName)
		pod.Spec.RuntimeClassName = &runtimeClass
	}

	validatorDaemonset, err := p.kubeClient.AppsV1().DaemonSets(namespaceFlag).Get(ctx, "nvidia-operator-validator", meta_v1.GetOptions{})
	if err != nil {
		return fmt.Errorf("unable to retrieve the operator validator daemonset: %w", err)
	}

	// update owner reference
	pod.SetOwnerReferences(validatorDaemonset.OwnerReferences)
	// set pod tolerations
	pod.Spec.Tolerations = validatorDaemonset.Spec.Template.Spec.Tolerations
	// update podSpec with node name, so it will just run on current node
	pod.Spec.NodeName = nodeNameFlag

	resourceName, err := p.getGPUResourceName()
	if err != nil {
		return err
	}

	gpuResource := corev1.ResourceList{
		resourceName: resource.MustParse("1"),
	}

	pod.Spec.InitContainers[0].Resources.Limits = gpuResource
	pod.Spec.InitContainers[0].Resources.Requests = gpuResource
	opts := meta_v1.ListOptions{LabelSelector: labels.Set{"app": pluginValidatorLabelValue}.AsSelector().String(),
		FieldSelector: fields.Set{"spec.nodeName": nodeNameFlag}.AsSelector().String()}

	// check if plugin validation pod is already running and cleanup.
	podList, err := p.kubeClient.CoreV1().Pods(namespaceFlag).List(ctx, opts)
	if err != nil {
		return fmt.Errorf("cannot list existing validation pods: %w", err)
	}

	if podList != nil && len(podList.Items) > 0 {
		propagation := meta_v1.DeletePropagationBackground
		gracePeriod := int64(0)
		options := meta_v1.DeleteOptions{PropagationPolicy: &propagation, GracePeriodSeconds: &gracePeriod}
		err = p.kubeClient.CoreV1().Pods(namespaceFlag).Delete(ctx, podList.Items[0].Name, options)
		if err != nil {
			return fmt.Errorf("cannot delete previous validation pod: %w", err)
		}
	}

	// wait for plugin validation pod to be ready.
	newPod, err := p.kubeClient.CoreV1().Pods(namespaceFlag).Create(ctx, pod, meta_v1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create plugin validation pod %s, err %w", pod.Name, err)
	}

	// make sure it's available
	err = waitForPod(ctx, p.kubeClient, newPod.Name, namespaceFlag)
	if err != nil {
		return err
	}
	return nil
}

// waits for the pod to be created
func waitForPod(ctx context.Context, kubeClient kubernetes.Interface, name string, namespace string) error {
	for i := 0; i < podCreationWaitRetries; i++ {
		// check for the existence of the resource
		pod, err := kubeClient.CoreV1().Pods(namespace).Get(ctx, name, meta_v1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get pod %s, err %w", name, err)
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

func loadPodSpec(podSpecPath string) (*corev1.Pod, error) {
	var pod corev1.Pod
	manifest, err := os.ReadFile(podSpecPath)
	if err != nil {
		panic(err)
	}
	s := json.NewSerializerWithOptions(json.DefaultMetaFactory, scheme.Scheme,
		scheme.Scheme, json.SerializerOptions{Yaml: true, Pretty: false, Strict: false})
	reg := regexp.MustCompile(`\b(\w*kind:\w*)\B.*\b`)

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
	node, err := getNode(p.ctx, p.kubeClient)
	if err != nil {
		return -1, fmt.Errorf("unable to fetch node by name %s to check for GPU resources: %w", nodeNameFlag, err)
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
		node, err := getNode(p.ctx, p.kubeClient)
		if err != nil {
			return fmt.Errorf("unable to fetch node by name %s to check for GPU resources: %s", nodeNameFlag, err)
		}

		if p.availableMIGResourceName(node.Status.Capacity) != "" {
			return nil
		}

		if p.availableGenericResourceName(node.Status.Capacity) != "" {
			return nil
		}

		log.Infof("GPU resources are not yet discovered by the node, retry: %d", retry)
		time.Sleep(gpuResourceDiscoveryIntervalSeconds * time.Second)
	}
	return fmt.Errorf("GPU resources are not discovered by the node")
}

func (p *Plugin) availableMIGResourceName(resources corev1.ResourceList) corev1.ResourceName {
	for resourceName, quantity := range resources {
		if strings.HasPrefix(string(resourceName), migGPUResourcePrefix) && quantity.Value() >= 1 {
			log.Debugf("Found MIG GPU resource name %s quantity %d", resourceName, quantity.Value())
			return resourceName
		}
	}
	return ""
}

func (p *Plugin) availableGenericResourceName(resources corev1.ResourceList) corev1.ResourceName {
	for resourceName, quantity := range resources {
		if strings.HasPrefix(string(resourceName), genericGPUResourceType) && quantity.Value() >= 1 {
			log.Debugf("Found GPU resource name %s quantity %d", resourceName, quantity.Value())
			return resourceName
		}
	}
	return ""
}

func (p *Plugin) getGPUResourceName() (corev1.ResourceName, error) {
	// get node info to check allocatable GPU resources
	node, err := getNode(p.ctx, p.kubeClient)
	if err != nil {
		return "", fmt.Errorf("unable to fetch node by name %s to check for GPU resources: %s", nodeNameFlag, err)
	}

	// use mig resource if one is available to run workload
	if resourceName := p.availableMIGResourceName(node.Status.Allocatable); resourceName != "" {
		return resourceName, nil
	}

	if resourceName := p.availableGenericResourceName(node.Status.Allocatable); resourceName != "" {
		return resourceName, nil
	}

	return "", fmt.Errorf("unable to find any allocatable GPU resource")
}

func (p *Plugin) setKubeClient(kubeClient kubernetes.Interface) {
	p.kubeClient = kubeClient
}

func getNode(ctx context.Context, kubeClient kubernetes.Interface) (*corev1.Node, error) {
	node, err := kubeClient.CoreV1().Nodes().Get(ctx, nodeNameFlag, meta_v1.GetOptions{})
	if err != nil {
		log.Errorf("unable to get node with name %s, err %v", nodeNameFlag, err)
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

	if withWorkloadFlag {
		// workload test
		err = c.runWorkload()
		if err != nil {
			return err
		}
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
	ctx := c.ctx

	// load podSpec
	pod, err := loadPodSpec(cudaWorkloadPodSpecPath)
	if err != nil {
		return err
	}
	pod.Namespace = namespaceFlag
	image := os.Getenv(validatorImageEnvName)
	pod.Spec.Containers[0].Image = image
	pod.Spec.InitContainers[0].Image = image

	imagePullPolicy := os.Getenv(validatorImagePullPolicyEnvName)
	if imagePullPolicy != "" {
		pod.Spec.Containers[0].ImagePullPolicy = corev1.PullPolicy(imagePullPolicy)
		pod.Spec.InitContainers[0].ImagePullPolicy = corev1.PullPolicy(imagePullPolicy)
	}

	if os.Getenv(validatorImagePullSecretsEnvName) != "" {
		pullSecrets := strings.Split(os.Getenv(validatorImagePullSecretsEnvName), ",")
		for _, secret := range pullSecrets {
			pod.Spec.ImagePullSecrets = append(pod.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: secret})
		}
	}
	if os.Getenv(validatorRuntimeClassEnvName) != "" {
		runtimeClass := os.Getenv(validatorRuntimeClassEnvName)
		pod.Spec.RuntimeClassName = &runtimeClass
	}

	validatorDaemonset, err := c.kubeClient.AppsV1().DaemonSets(namespaceFlag).Get(ctx, "nvidia-operator-validator", meta_v1.GetOptions{})
	if err != nil {
		return fmt.Errorf("unable to retrieve the operator validator daemonset: %w", err)
	}

	// update owner reference
	pod.SetOwnerReferences(validatorDaemonset.OwnerReferences)
	// set pod tolerations
	pod.Spec.Tolerations = validatorDaemonset.Spec.Template.Spec.Tolerations
	// update podSpec with node name, so it will just run on current node
	pod.Spec.NodeName = nodeNameFlag

	opts := meta_v1.ListOptions{LabelSelector: labels.Set{"app": cudaValidatorLabelValue}.AsSelector().String(),
		FieldSelector: fields.Set{"spec.nodeName": nodeNameFlag}.AsSelector().String()}

	// check if cuda workload pod is already running and cleanup.
	podList, err := c.kubeClient.CoreV1().Pods(namespaceFlag).List(ctx, opts)
	if err != nil {
		return fmt.Errorf("cannot list existing validation pods: %s", err)
	}

	if podList != nil && len(podList.Items) > 0 {
		propagation := meta_v1.DeletePropagationBackground
		gracePeriod := int64(0)
		options := meta_v1.DeleteOptions{PropagationPolicy: &propagation, GracePeriodSeconds: &gracePeriod}
		err = c.kubeClient.CoreV1().Pods(namespaceFlag).Delete(ctx, podList.Items[0].Name, options)
		if err != nil {
			return fmt.Errorf("cannot delete previous validation pod: %s", err)
		}
	}

	// wait for cuda workload pod to be ready.
	newPod, err := c.kubeClient.CoreV1().Pods(namespaceFlag).Create(ctx, pod, meta_v1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create cuda validation pod %s, err %+v", pod.Name, err)
	}

	// make sure it's available
	err = waitForPod(ctx, c.kubeClient, newPod.Name, namespaceFlag)
	if err != nil {
		return err
	}
	return nil
}

func (c *Metrics) run() error {
	m := NewNodeMetrics(c.ctx, metricsPort)

	return m.Run()
}

func (v *VfioPCI) validate() error {
	ctx := v.ctx

	gpuWorkloadConfig, err := getWorkloadConfig(ctx)
	if err != nil {
		return fmt.Errorf("error getting gpu workload config: %w", err)
	}
	log.Infof("GPU workload configuration: %s", gpuWorkloadConfig)

	err = createStatusFileWithContent(filepath.Join(outputDirFlag, workloadTypeStatusFile), gpuWorkloadConfig+"\n")
	if err != nil {
		return fmt.Errorf("error updating %s status file: %w", workloadTypeStatusFile, err)
	}

	if gpuWorkloadConfig != gpuWorkloadConfigVMPassthrough {
		log.WithFields(log.Fields{
			"gpuWorkloadConfig": gpuWorkloadConfig,
		}).Info("vfio-pci not required on the node. Skipping validation.")
		return nil
	}

	// delete status file if already present
	err = deleteStatusFile(outputDirFlag + "/" + vfioPCIStatusFile)
	if err != nil {
		return err
	}

	err = v.runValidation()
	if err != nil {
		return err
	}
	log.Info("Validation completed successfully - all devices are bound to vfio-pci")

	// delete status file is already present
	err = createStatusFile(outputDirFlag + "/" + vfioPCIStatusFile)
	if err != nil {
		return err
	}
	return nil
}

func (v *VfioPCI) runValidation() error {
	nvpci := nvpci.New()
	nvdevices, err := nvpci.GetGPUs()
	if err != nil {
		return fmt.Errorf("error getting NVIDIA PCI devices: %w", err)
	}

	for _, dev := range nvdevices {
		// TODO: Do not hardcode a list of VFIO driver names. This would be possible if we
		// added an API to go-nvlib which returns the most suitable VFIO driver for a GPU,
		// using logic similar to https://github.com/NVIDIA/k8s-driver-manager/commit/874c8cd26d775db437f16a42c3e44e74301b3a35
		if dev.Driver != "vfio-pci" && dev.Driver != "nvgrace_gpu_vfio_pci" {
			return fmt.Errorf("device not bound to 'vfio-pci'; device: %s driver: '%s'", dev.Address, dev.Driver)
		}
	}

	return nil
}

func (v *VGPUManager) validate() error {
	ctx := v.ctx

	gpuWorkloadConfig, err := getWorkloadConfig(ctx)
	if err != nil {
		return fmt.Errorf("error getting gpu workload config: %w", err)
	}
	log.Infof("GPU workload configuration: %s", gpuWorkloadConfig)

	err = createStatusFileWithContent(filepath.Join(outputDirFlag, workloadTypeStatusFile), gpuWorkloadConfig+"\n")
	if err != nil {
		return fmt.Errorf("error updating %s status file: %w", workloadTypeStatusFile, err)
	}

	if gpuWorkloadConfig != gpuWorkloadConfigVMVgpu {
		log.WithFields(log.Fields{
			"gpuWorkloadConfig": gpuWorkloadConfig,
		}).Info("vGPU Manager not required on the node. Skipping validation.")
		return nil
	}

	// delete status file if already present
	err = deleteStatusFile(outputDirFlag + "/" + vGPUManagerStatusFile)
	if err != nil {
		return err
	}

	// delete status file if already present
	err = deleteStatusFile(outputDirFlag + "/" + hostVGPUManagerStatusFile)
	if err != nil {
		return err
	}

	hostDriver, err := v.runValidation(false)
	if err != nil {
		fmt.Println("vGPU Manager is not ready")
		return err
	}

	log.Info("Waiting for VFs to be available...")
	if err := waitForVFs(ctx, defaultVFWaitTimeout); err != nil {
		return fmt.Errorf("vGPU Manager VFs not ready: %w", err)
	}

	statusFile := vGPUManagerStatusFile
	if hostDriver {
		statusFile = hostVGPUManagerStatusFile
	}

	// create driver status file
	err = createStatusFile(outputDirFlag + "/" + statusFile)
	if err != nil {
		return err
	}
	return nil
}

func (v *VGPUManager) runValidation(silent bool) (hostDriver bool, err error) {
	// invoke validation command
	command := "chroot"
	args := []string{"/run/nvidia/driver", "nvidia-smi"}

	// check if driver is pre-installed on the host and use host path for validation
	if _, err := os.Lstat("/host/usr/bin/nvidia-smi"); err == nil {
		args = []string{"/host", "nvidia-smi"}
		hostDriver = true
	}

	if withWaitFlag {
		return hostDriver, runCommandWithWait(command, args, sleepIntervalSecondsFlag, silent)
	}

	return hostDriver, runCommand(command, args, silent)
}

// waitForVFs waits for Virtual Functions to be created on all NVIDIA GPUs.
// It polls sriov_numvfs until all GPUs have their full VF count enabled.
func waitForVFs(ctx context.Context, timeout time.Duration) error {
	pollInterval := time.Duration(sleepIntervalSecondsFlag) * time.Second
	nvpciLib := nvpci.New()

	return wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		gpus, err := nvpciLib.GetGPUs()
		if err != nil {
			log.Warnf("Error getting GPUs: %v", err)
			return false, nil
		}

		var totalExpected, totalEnabled uint64
		var pfCount int
		for _, gpu := range gpus {
			sriovInfo := gpu.SriovInfo
			if sriovInfo.IsPF() {
				pfCount++
				totalExpected += sriovInfo.PhysicalFunction.TotalVFs
				totalEnabled += sriovInfo.PhysicalFunction.NumVFs
			}
		}

		if totalExpected == 0 {
			log.Info("No SR-IOV capable GPUs found, skipping VF wait")
			return true, nil
		}

		if totalEnabled == totalExpected {
			log.Infof("All %d VF(s) enabled on %d NVIDIA GPU(s)", totalEnabled, pfCount)
			return true, nil
		}

		log.Infof("Waiting for VFs: %d/%d enabled across %d GPU(s)", totalEnabled, totalExpected, pfCount)
		return false, nil
	})
}

func (c *CCManager) validate() error {
	// delete status file if already present
	err := deleteStatusFile(outputDirFlag + "/" + ccManagerStatusFile)
	if err != nil {
		return err
	}

	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("error getting cluster config - %w", err)
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		log.Errorf("Error getting k8s client - %v\n", err)
		return err
	}

	// update k8s client for fetching node labels
	c.setKubeClient(kubeClient)

	err = c.runValidation(false)
	if err != nil {
		fmt.Println("CC Manager is not ready")
		return err
	}

	// create driver status file
	err = createStatusFile(outputDirFlag + "/" + ccManagerStatusFile)
	if err != nil {
		return err
	}
	return nil
}

func (c *CCManager) runValidation(silent bool) error {
	node, err := getNode(c.ctx, c.kubeClient)
	if err != nil {
		return fmt.Errorf("unable to fetch node by name %s to check for %s label: %w",
			nodeNameFlag, CCCapableLabelKey, err)
	}

	// make sure this is a CC capable node
	nodeLabels := node.GetLabels()
	if enabled, ok := nodeLabels[CCCapableLabelKey]; !ok || enabled != "true" {
		log.Info("Not a CC capable node, skipping CC Manager validation")
		return nil
	}

	// check if the ccManager container is ready
	err = assertCCManagerContainerReady(silent, withWaitFlag)
	if err != nil {
		return err
	}
	return nil
}

func (c *CCManager) setKubeClient(kubeClient kubernetes.Interface) {
	c.kubeClient = kubeClient
}

// Check that the ccManager container is ready after applying required ccMode
func assertCCManagerContainerReady(silent, withWaitFlag bool) error {
	command := shell
	args := []string{"-c", "stat /run/nvidia/validations/.cc-manager-ctr-ready"}

	if withWaitFlag {
		return runCommandWithWait(command, args, sleepIntervalSecondsFlag, silent)
	}

	return runCommand(command, args, silent)
}

func (v *VGPUDevices) validate() error {
	ctx := v.ctx

	gpuWorkloadConfig, err := getWorkloadConfig(ctx)
	if err != nil {
		return fmt.Errorf("error getting gpu workload config: %w", err)
	}
	log.Infof("GPU workload configuration: %s", gpuWorkloadConfig)

	err = createStatusFileWithContent(filepath.Join(outputDirFlag, workloadTypeStatusFile), gpuWorkloadConfig+"\n")
	if err != nil {
		return fmt.Errorf("error updating %s status file: %w", workloadTypeStatusFile, err)
	}

	if gpuWorkloadConfig != gpuWorkloadConfigVMVgpu {
		log.WithFields(log.Fields{
			"gpuWorkloadConfig": gpuWorkloadConfig,
		}).Info("vgpu devices not required on the node. Skipping validation.")
		return nil
	}

	// delete status file if already present
	err = deleteStatusFile(outputDirFlag + "/" + vGPUDevicesStatusFile)
	if err != nil {
		return err
	}

	err = v.runValidation()
	if err != nil {
		return err
	}
	log.Info("Validation completed successfully - vGPU devices present on the host")

	// create status file
	err = createStatusFile(outputDirFlag + "/" + vGPUDevicesStatusFile)
	if err != nil {
		return err
	}

	return nil
}

func (v *VGPUDevices) runValidation() error {
	nvmdev := nvmdev.New()
	vGPUDevices, err := nvmdev.GetAllDevices()
	if err != nil {
		return fmt.Errorf("error checking for vGPU devices on the host: %w", err)
	}

	if !withWaitFlag {
		numDevices := len(vGPUDevices)
		if numDevices == 0 {
			return fmt.Errorf("no vGPU devices found")
		}

		log.Infof("Found %d vGPU devices", numDevices)
		return nil
	}

	for {
		numDevices := len(vGPUDevices)
		if numDevices > 0 {
			log.Infof("Found %d vGPU devices", numDevices)
			return nil
		}
		log.Infof("No vGPU devices found, retrying after %d seconds", sleepIntervalSecondsFlag)
		time.Sleep(time.Duration(sleepIntervalSecondsFlag) * time.Second)

		vGPUDevices, err = nvmdev.GetAllDevices()
		if err != nil {
			return fmt.Errorf("error checking for vGPU devices on the host: %w", err)
		}
	}
}
