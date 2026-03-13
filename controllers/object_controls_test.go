/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package controllers

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	secv1 "github.com/openshift/api/security/v1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	nodev1beta1 "k8s.io/api/node/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedv1 "k8s.io/api/scheduling/v1beta1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

const (
	clusterPolicyPath             = "config/samples/v1_clusterpolicy.yaml"
	clusterPolicyName             = "gpu-cluster-policy"
	driverAssetsPath              = "assets/state-driver/"
	vGPUManagerAssetsPath         = "assets/state-vgpu-manager/"
	sandboxDevicePluginAssetsPath = "assets/state-sandbox-device-plugin"
	kataDevicePluginAssetsPath    = "assets/state-kata-device-plugin"
	devicePluginAssetsPath        = "assets/state-device-plugin/"
	dcgmExporterAssetsPath        = "assets/state-dcgm-exporter/"
	migManagerAssetsPath          = "assets/state-mig-manager/"
	nfdNvidiaPCILabelKey          = "feature.node.kubernetes.io/pci-10de.present"
	upgradedKernel                = "5.4.135-generic"
)

type testConfig struct {
	root  string
	nodes int
}

var (
	cfg                     *testConfig
	clusterPolicyController ClusterPolicyController
	clusterPolicy           gpuv1.ClusterPolicy
	boolTrue                *bool
	boolFalse               *bool
)

var nfdLabels = map[string]string{
	nfdNvidiaPCILabelKey:   "true",
	nfdKernelLabelKey:      "5.4.0-generic",
	nfdOSReleaseIDLabelKey: "ubuntu",
	nfdOSVersionIDLabelKey: "22.04",
}

var kubernetesResources = []client.Object{
	&corev1.ServiceAccount{},
	&rbacv1.Role{},
	&rbacv1.RoleBinding{},
	&rbacv1.ClusterRole{},
	&rbacv1.ClusterRoleBinding{},
	&corev1.ConfigMap{},
	&corev1.Secret{},
	&appsv1.DaemonSet{},
	&appsv1.Deployment{},
	&corev1.Pod{},
	&corev1.Service{},
	&promv1.ServiceMonitor{},
	&schedv1.PriorityClass{},
	// &corev1.Taint{},
	&secv1.SecurityContextConstraints{},
	&corev1.Namespace{},
	&nodev1.RuntimeClass{},
	&promv1.PrometheusRule{},
}

type commonDaemonsetSpec struct {
	repository         string
	image              string
	version            string
	imagePullPolicy    string
	imagePullSecrets   []corev1.LocalObjectReference
	args               []string
	env                []gpuv1.EnvVar
	resources          *gpuv1.ResourceRequirements
	startupProbe       *gpuv1.ContainerProbeSpec
	kernelModuleConfig *gpuv1.KernelModuleConfigSpec
}

func TestMain(m *testing.M) {
	_, filename, _, _ := goruntime.Caller(0)
	moduleRoot, err := getModuleRoot(filename)
	if err != nil {
		log.Fatalf("error in test setup: could not get module root: %v", err)
	}
	cfg = &testConfig{root: moduleRoot, nodes: 2}

	err = setup()
	if err != nil {
		log.Fatalf("error in test setup: could not setup mock k8s: %v", err)
	}

	exitCode := m.Run()
	os.Exit(exitCode)
}

func getModuleRoot(dir string) (string, error) {
	if dir == "" || dir == "/" {
		return "", fmt.Errorf("module root not found")
	}

	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	if err != nil {
		return getModuleRoot(filepath.Dir(dir))
	}

	// go.mod was found in dir
	return dir, nil
}

// setup creates a mock kubernetes cluster and client. Nodes are labeled with the minimum
// required NFD labels to be detected as GPU nodes by the GPU Operator. A sample
// ClusterPolicy resource is applied to the cluster. The ClusterPolicyController
// object is initialized with the mock kubernetes client as well as other steps
// mimicking init() in state_manager.go
func setup() error {
	ctx := context.Background()
	// Used when updating ClusterPolicy spec
	boolFalse = new(bool)
	boolTrue = new(bool)
	*boolTrue = true

	s := scheme.Scheme
	if err := gpuv1.AddToScheme(s); err != nil {
		return fmt.Errorf("unable to add ClusterPolicy v1 schema: %v", err)
	}
	if err := promv1.AddToScheme(s); err != nil {
		return fmt.Errorf("unable to add promv1 schema: %v", err)
	}
	if err := apiextensionsv1.AddToScheme(s); err != nil {
		return fmt.Errorf("unable to add apiextensionsv1 schema: %v", err)
	}
	if err := secv1.Install(s); err != nil {
		return fmt.Errorf("unable to add secv1 schema: %v", err)
	}

	client, err := newCluster(cfg.nodes, s)
	if err != nil {
		return fmt.Errorf("unable to create cluster: %v", err)
	}

	// Get a sample ClusterPolicy manifest
	manifests := getAssetsFrom(&clusterPolicyController, filepath.Join(cfg.root, clusterPolicyPath), "")
	clusterPolicyManifest := manifests[0]
	ser := json.NewSerializerWithOptions(json.DefaultMetaFactory, scheme.Scheme, scheme.Scheme,
		json.SerializerOptions{Yaml: true, Pretty: false, Strict: false})
	_, _, err = ser.Decode(clusterPolicyManifest, nil, &clusterPolicy)
	if err != nil {
		return fmt.Errorf("failed to decode sample ClusterPolicy manifest: %v", err)
	}

	err = client.Create(ctx, &clusterPolicy)
	if err != nil {
		return fmt.Errorf("failed to create ClusterPolicy resource: %v", err)
	}

	// Confirm ClusterPolicy is deployed in mock cluster
	cp := &gpuv1.ClusterPolicy{}
	err = client.Get(ctx, types.NamespacedName{Namespace: "", Name: clusterPolicyName}, cp)
	if err != nil {
		return fmt.Errorf("unable to get ClusterPolicy from client: %v", err)
	}

	opts := zap.Options{
		Development: true,
	}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	clusterPolicyController = ClusterPolicyController{
		ctx:       ctx,
		singleton: cp,
		client:    client,
		logger:    ctrl.Log.WithName("controller").WithName("ClusterPolicy"),
		scheme:    s,
	}

	clusterPolicyController.operatorMetrics = initOperatorMetrics()

	hasNFDLabels, gpuNodeCount, err := clusterPolicyController.labelGPUNodes()
	if err != nil {
		return fmt.Errorf("unable to label nodes in cluster: %v", err)
	}
	if gpuNodeCount == 0 {
		return fmt.Errorf("no gpu nodes in mock cluster")
	}
	gpuNodeOSTag, err := clusterPolicyController.getGPUNodeOSTag()
	if err != nil {
		return fmt.Errorf("unable to get GPU node tag: %w", err)
	}

	clusterPolicyController.hasGPUNodes = gpuNodeCount != 0
	clusterPolicyController.hasNFDLabels = hasNFDLabels
	clusterPolicyController.gpuNodeOSTag = gpuNodeOSTag

	// setup kernelVersionMap for pre-compiled driver tests
	kernelVersionMap, err := clusterPolicyController.getKernelVersionsMap()
	if err != nil {
		return fmt.Errorf("Unable to obtain all kernel versions of the GPU nodes in the cluster: %v", err)
	}
	clusterPolicyController.kernelVersionMap = kernelVersionMap
	return nil
}

// newCluster creates a mock kubernetes cluster and returns the corresponding client object
func newCluster(nodes int, s *runtime.Scheme) (client.Client, error) {
	ctx := context.Background()
	// Build fake client
	cl := fake.NewClientBuilder().WithScheme(s).Build()

	for i := 0; i < nodes; i++ {
		ready := corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionTrue}
		name := fmt.Sprintf("node%d", i)
		n := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: nfdLabels,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					ready,
				},
			},
		}
		// set one node with different kernel for pre-compiled driver tests
		if nodes > 1 && i == nodes-1 {
			n.Labels[nfdKernelLabelKey] = upgradedKernel
		}
		err := cl.Create(ctx, n)
		if err != nil {
			return nil, fmt.Errorf("unable to create node in cluster: %v", err)
		}
	}

	return cl, nil
}

// updateClusterPolicy updates an existing ClusterPolicy instance
func updateClusterPolicy(n *ClusterPolicyController, cp *gpuv1.ClusterPolicy) error {
	n.singleton = cp
	err := n.client.Update(n.ctx, cp)
	if err != nil && !apierrors.IsConflict(err) {
		return fmt.Errorf("failed to update ClusterPolicy: %v", err)
	}
	return nil
}

// removeState deletes all resources, controls, and stateNames tracked
// by ClusterPolicyController at a specific index. It also deletes
// all objects from the mock k8s client
func removeState(n *ClusterPolicyController, idx int) error {
	var err error
	for _, res := range kubernetesResources {
		// TODO: use n.operatorNamespace once MR is merged
		err = n.client.DeleteAllOf(n.ctx, res)
		if err != nil {
			return fmt.Errorf("error deleting objects from k8s client: %v", err)
		}
	}
	n.resources = append(n.resources[:idx], n.resources[idx+1:]...)
	n.controls = append(n.controls[:idx], n.controls[idx+1:]...)
	n.stateNames = append(n.stateNames[:idx], n.stateNames[idx+1:]...)
	return nil
}

// getImagePullSecrets converts a slice of strings (pull secrets)
// to the corev1 type used by k8s
func getImagePullSecrets(secrets []string) []corev1.LocalObjectReference {
	var ret []corev1.LocalObjectReference
	for _, secret := range secrets {
		ret = append(ret, corev1.LocalObjectReference{Name: secret})
	}
	return ret
}

// testDaemonsetCommon executes one test case for a particular Daemonset,
// and checks the values for common fields used throughout all Daemonsets
// managed by the GPU Operator.
func testDaemonsetCommon(t *testing.T, cp *gpuv1.ClusterPolicy, component string, numDaemonsets int) (*appsv1.DaemonSet, error) {
	ctx := context.Background()

	var spec commonDaemonsetSpec
	var dsLabel, mainCtrName, manifestFile, mainCtrImage string
	var err error

	// TODO: add cases for all components
	switch component {
	case "Driver":
		spec = commonDaemonsetSpec{
			repository:       cp.Spec.Driver.Repository,
			image:            cp.Spec.Driver.Image,
			version:          cp.Spec.Driver.Version,
			imagePullPolicy:  cp.Spec.Driver.ImagePullPolicy,
			imagePullSecrets: getImagePullSecrets(cp.Spec.Driver.ImagePullSecrets),
			args:             cp.Spec.Driver.Args,
			env:              cp.Spec.Driver.Env,
			resources:        cp.Spec.Driver.Resources,
			startupProbe:     cp.Spec.Driver.StartupProbe,
		}
		dsLabel = "nvidia-driver-daemonset"
		mainCtrName = "nvidia-driver"
		manifestFile = filepath.Join(cfg.root, driverAssetsPath)
		mainCtrImage, err = resolveDriverTag(clusterPolicyController, &cp.Spec.Driver)
		if err != nil {
			return nil, fmt.Errorf("unable to get mainCtrImage for driver: %v", err)
		}
	case "DevicePlugin":
		spec = commonDaemonsetSpec{
			repository:       cp.Spec.DevicePlugin.Repository,
			image:            cp.Spec.DevicePlugin.Image,
			version:          cp.Spec.DevicePlugin.Version,
			imagePullPolicy:  cp.Spec.DevicePlugin.ImagePullPolicy,
			imagePullSecrets: getImagePullSecrets(cp.Spec.DevicePlugin.ImagePullSecrets),
			args:             cp.Spec.DevicePlugin.Args,
			env:              cp.Spec.DevicePlugin.Env,
			resources:        cp.Spec.DevicePlugin.Resources,
		}
		dsLabel = "nvidia-device-plugin-daemonset"
		mainCtrName = "nvidia-device-plugin"
		manifestFile = filepath.Join(cfg.root, devicePluginAssetsPath)
		mainCtrImage, err = gpuv1.ImagePath(&cp.Spec.DevicePlugin)
		if err != nil {
			return nil, fmt.Errorf("unable to get mainCtrImage for device-plugin: %v", err)
		}
	case "VGPUManager":
		spec = commonDaemonsetSpec{
			repository:         cp.Spec.VGPUManager.Repository,
			image:              cp.Spec.VGPUManager.Image,
			version:            cp.Spec.VGPUManager.Version,
			imagePullPolicy:    cp.Spec.VGPUManager.ImagePullPolicy,
			imagePullSecrets:   getImagePullSecrets(cp.Spec.VGPUManager.ImagePullSecrets),
			args:               cp.Spec.VGPUManager.Args,
			env:                cp.Spec.VGPUManager.Env,
			resources:          cp.Spec.VGPUManager.Resources,
			kernelModuleConfig: cp.Spec.VGPUManager.KernelModuleConfig,
		}
		dsLabel = "nvidia-vgpu-manager-daemonset"
		mainCtrName = "nvidia-vgpu-manager-ctr"
		manifestFile = filepath.Join(cfg.root, vGPUManagerAssetsPath)
		mainCtrImage, err = resolveDriverTag(clusterPolicyController, &cp.Spec.VGPUManager)
		if err != nil {
			return nil, fmt.Errorf("unable to get mainCtrImage for driver: %v", err)
		}
	case "SandboxDevicePlugin":
		spec = commonDaemonsetSpec{
			repository:       cp.Spec.SandboxDevicePlugin.Repository,
			image:            cp.Spec.SandboxDevicePlugin.Image,
			version:          cp.Spec.SandboxDevicePlugin.Version,
			imagePullPolicy:  cp.Spec.SandboxDevicePlugin.ImagePullPolicy,
			imagePullSecrets: getImagePullSecrets(cp.Spec.SandboxDevicePlugin.ImagePullSecrets),
			args:             cp.Spec.SandboxDevicePlugin.Args,
			env:              cp.Spec.SandboxDevicePlugin.Env,
			resources:        cp.Spec.SandboxDevicePlugin.Resources,
		}
		dsLabel = "nvidia-sandbox-device-plugin-daemonset"
		mainCtrName = "nvidia-sandbox-device-plugin-ctr"
		manifestFile = filepath.Join(cfg.root, sandboxDevicePluginAssetsPath)
		mainCtrImage, err = gpuv1.ImagePath(&cp.Spec.SandboxDevicePlugin)
		if err != nil {
			return nil, fmt.Errorf("unable to get mainCtrImage for sandbox-device-plugin: %v", err)
		}
	case "KataDevicePlugin":
		spec = commonDaemonsetSpec{
			repository:       cp.Spec.KataSandboxDevicePlugin.Repository,
			image:            cp.Spec.KataSandboxDevicePlugin.Image,
			version:          cp.Spec.KataSandboxDevicePlugin.Version,
			imagePullPolicy:  cp.Spec.KataSandboxDevicePlugin.ImagePullPolicy,
			imagePullSecrets: getImagePullSecrets(cp.Spec.KataSandboxDevicePlugin.ImagePullSecrets),
			args:             cp.Spec.KataSandboxDevicePlugin.Args,
			env:              cp.Spec.KataSandboxDevicePlugin.Env,
			resources:        cp.Spec.KataSandboxDevicePlugin.Resources,
		}
		dsLabel = "nvidia-kata-sandbox-device-plugin-daemonset"
		mainCtrName = "nvidia-kata-sandbox-device-plugin-ctr"
		manifestFile = filepath.Join(cfg.root, kataDevicePluginAssetsPath)
		mainCtrImage, err = gpuv1.ImagePath(&cp.Spec.KataSandboxDevicePlugin)
		if err != nil {
			return nil, fmt.Errorf("unable to get mainCtrImage for kata-device-plugin: %v", err)
		}
	case "DCGMExporter":
		spec = commonDaemonsetSpec{
			repository:       cp.Spec.DCGMExporter.Repository,
			image:            cp.Spec.DCGMExporter.Image,
			version:          cp.Spec.DCGMExporter.Version,
			imagePullPolicy:  cp.Spec.DCGMExporter.ImagePullPolicy,
			imagePullSecrets: getImagePullSecrets(cp.Spec.DCGMExporter.ImagePullSecrets),
			args:             cp.Spec.DCGMExporter.Args,
			env:              cp.Spec.DCGMExporter.Env,
			resources:        cp.Spec.DCGMExporter.Resources,
		}
		dsLabel = "nvidia-dcgm-exporter"
		mainCtrName = "nvidia-dcgm-exporter"
		manifestFile = filepath.Join(cfg.root, dcgmExporterAssetsPath)
		mainCtrImage, err = gpuv1.ImagePath(&cp.Spec.DCGMExporter)
		if err != nil {
			return nil, fmt.Errorf("unable to get mainCtrImage for dcgm-exporter: %v", err)
		}
	case "MIGManager":
		spec = commonDaemonsetSpec{
			repository:       cp.Spec.MIGManager.Repository,
			image:            cp.Spec.MIGManager.Image,
			version:          cp.Spec.MIGManager.Version,
			imagePullPolicy:  cp.Spec.MIGManager.ImagePullPolicy,
			imagePullSecrets: getImagePullSecrets(cp.Spec.MIGManager.ImagePullSecrets),
			args:             cp.Spec.MIGManager.Args,
			env:              cp.Spec.MIGManager.Env,
			resources:        cp.Spec.MIGManager.Resources,
		}
		dsLabel = "nvidia-mig-manager"
		mainCtrName = "nvidia-mig-manager"
		manifestFile = filepath.Join(cfg.root, migManagerAssetsPath)
		mainCtrImage, err = gpuv1.ImagePath(&cp.Spec.MIGManager)
		if err != nil {
			return nil, fmt.Errorf("unable to get mainCtrImage for mig-manager: %v", err)
		}
	default:
		return nil, fmt.Errorf("invalid component for testDaemonsetCommon(): %s", component)
	}

	// update cluster policy
	err = updateClusterPolicy(&clusterPolicyController, cp)
	if err != nil {
		t.Fatalf("error in test setup: %v", err)
	}

	// add manifests
	addState(&clusterPolicyController, manifestFile)

	// create resources
	_, err = clusterPolicyController.step()
	if err != nil {
		t.Errorf("error creating resources: %v", err)
	}
	// get daemonset
	opts := []client.ListOption{
		client.MatchingLabels{"app": dsLabel},
	}
	list := &appsv1.DaemonSetList{}
	err = clusterPolicyController.client.List(ctx, list, opts...)
	if err != nil {
		t.Fatalf("could not get DaemonSetList from client: %v", err)
	}

	// compare daemonset with expected output
	require.Equal(t, numDaemonsets, len(list.Items), "unexpected # of daemonsets")
	if numDaemonsets == 0 || len(list.Items) == 0 {
		return nil, nil
	}
	ds := list.Items[0]
	// find main container
	mainCtrIdx := -1
	for i, container := range ds.Spec.Template.Spec.Containers {
		if strings.Contains(container.Name, mainCtrName) {
			mainCtrIdx = i
			break
		}
	}
	if mainCtrIdx == -1 {
		return nil, fmt.Errorf("could not find main container index")
	}
	mainCtr := ds.Spec.Template.Spec.Containers[mainCtrIdx]

	if component == "Driver" && cp.Spec.Driver.UsePrecompiledDrivers() {
		// for pre-compiled drivers, container image is kernel specific
		require.Contains(t, mainCtr.Image, "-generic-ubuntu22.04", "unexpected Image")
	} else {
		require.Equal(t, mainCtrImage, mainCtr.Image, "unexpected Image")
	}
	require.Equal(t, gpuv1.ImagePullPolicy(spec.imagePullPolicy), mainCtr.ImagePullPolicy, "unexpected ImagePullPolicy")
	require.Equal(t, spec.imagePullSecrets, ds.Spec.Template.Spec.ImagePullSecrets, "unexpected ImagePullSecrets")

	if spec.startupProbe != nil {
		require.Equal(t, spec.startupProbe.InitialDelaySeconds, mainCtr.StartupProbe.InitialDelaySeconds)
		require.Equal(t, spec.startupProbe.PeriodSeconds, mainCtr.StartupProbe.PeriodSeconds)
		require.Equal(t, spec.startupProbe.TimeoutSeconds, mainCtr.StartupProbe.TimeoutSeconds)
		require.Equal(t, spec.startupProbe.FailureThreshold, mainCtr.StartupProbe.FailureThreshold)
	}

	if spec.args != nil {
		require.Equal(t, spec.args, mainCtr.Args, "unexpected Args")
	}
	for _, env := range spec.env {
		require.Contains(t, mainCtr.Env, env, "env var not present")
	}
	// TODO: implement checks for other common fields (i.e. Resources, securityContext, Tolerations, etc.)

	return &ds, nil
}

// getDriverTestInput return a ClusterPolicy instance for a particular
// driver test case. This function will grow as new test cases are added
func getDriverTestInput(testCase string) *gpuv1.ClusterPolicy {
	cp := clusterPolicy.DeepCopy()
	// Until we create sample ClusterPolicies that have all fields
	// set, hardcode some default values:
	cp.Spec.Driver.Repository = "nvcr.io/nvidia"
	cp.Spec.Driver.Image = "driver"
	cp.Spec.Driver.Version = "470.57.02"
	cp.Spec.Driver.ImagePullSecrets = []string{"ngc-secret"}

	cp.Spec.Driver.Manager.Repository = "nvcr.io/nvidia/cloud-native"
	cp.Spec.Driver.Manager.Image = "k8s-driver-manager"
	cp.Spec.Driver.Manager.Version = "test"
	cp.Spec.Driver.Manager.ImagePullSecrets = []string{"ngc-secret"}

	cp.Spec.Driver.StartupProbe = &gpuv1.ContainerProbeSpec{InitialDelaySeconds: 20, PeriodSeconds: 5, FailureThreshold: 1, TimeoutSeconds: 60}

	switch testCase {
	case "default":
		// Do nothing
	case "precompiled":
		usePrecompiled := true
		cp.Spec.Driver.UsePrecompiled = &usePrecompiled
	default:
		return nil
	}

	return cp
}

// getDriverTestOutput returns a map containing expected output for
// driver test case. This function will grow as new test cases are added
func getDriverTestOutput(testCase string) map[string]interface{} {
	// default output
	output := map[string]interface{}{
		"numDaemonsets":      1,
		"nvPeerMemPresent":   false,
		"driverManagerImage": "nvcr.io/nvidia/cloud-native/k8s-driver-manager:test",
		"imagePullSecret":    "ngc-secret",
	}

	switch testCase {
	case "default":
		output["driverImage"] = "nvcr.io/nvidia/driver:470.57.02-ubuntu22.04"
	case "precompiled":
		output["driverImage"] = "nvcr.io/nvidia/driver:470.57.02-5.4.0-generic-ubuntu22.04"
		output["numDaemonsets"] = 2
	default:
		return nil
	}
	return output
}

// TestDriver tests that the GPU Operator correctly deploys the driver daemonset
// under various scenarios/config options
func TestDriver(t *testing.T) {
	testCases := []struct {
		description   string
		clusterPolicy *gpuv1.ClusterPolicy
		output        map[string]interface{}
	}{
		{
			"Default",
			getDriverTestInput("default"),
			getDriverTestOutput("default"),
		},
		{
			"Precompiled Drivers",
			getDriverTestInput("precompiled"),
			getDriverTestOutput("precompiled"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			ds, err := testDaemonsetCommon(t, tc.clusterPolicy, "Driver", tc.output["numDaemonsets"].(int))
			if err != nil {
				t.Fatalf("error in testDaemonsetCommon(): %v", err)
			}
			if ds == nil {
				return
			}

			nvPeerMemPresent := false
			driverImage := ""
			driverManagerImage := ""
			for _, initContainer := range ds.Spec.Template.Spec.InitContainers {
				if strings.Contains(initContainer.Name, "k8s-driver-manager") {
					driverManagerImage = initContainer.Image
				}
			}
			for _, container := range ds.Spec.Template.Spec.Containers {
				if strings.Contains(container.Name, "nvidia-driver") {
					driverImage = container.Image
					continue
				}
				if strings.Contains(container.Name, "nvidia-peermem") {
					nvPeerMemPresent = true
				}
			}

			require.Equal(t, tc.output["nvPeerMemPresent"], nvPeerMemPresent, "Unexpected configuration for nv-peermem container")
			require.Equal(t, tc.output["driverImage"], driverImage, "Unexpected configuration for nvidia-driver-ctr image")
			require.Equal(t, tc.output["driverManagerImage"], driverManagerImage, "Unexpected configuration for k8s-driver-manager image")
			require.Equal(t, len(ds.Spec.Template.Spec.ImagePullSecrets), 1, "Incorrect number of imagePullSecrets in the daemon set spec")
			require.Equal(t, tc.output["imagePullSecret"], ds.Spec.Template.Spec.ImagePullSecrets[0].Name, "Incorrect imagePullSecret in the daemon set spec")

			// cleanup by deleting all kubernetes objects
			err = removeState(&clusterPolicyController, clusterPolicyController.idx-1)
			if err != nil {
				t.Fatalf("error removing state %v:", err)
			}
			clusterPolicyController.idx--
		})
	}
}

// getDevicePluginTestInput return a ClusterPolicy instance for a particular
// device-plugin test case. This function will grow as new test cases are added
func getDevicePluginTestInput(testCase string) *gpuv1.ClusterPolicy {
	cp := clusterPolicy.DeepCopy()

	// Until we create sample ClusterPolicies that have all fields
	// set, hardcode some default values:
	cp.Spec.DevicePlugin.Repository = "nvcr.io/nvidia"
	cp.Spec.DevicePlugin.Image = "k8s-device-plugin"
	cp.Spec.DevicePlugin.Version = "v0.12.0-ubi8"
	cp.Spec.DevicePlugin.ImagePullSecrets = []string{"ngc-secret"}

	cp.Spec.Validator.Repository = "nvcr.io/nvidia/cloud-native"
	cp.Spec.Validator.Image = "gpu-operator-validator"
	cp.Spec.Validator.Version = "v1.11.0"
	cp.Spec.Validator.ImagePullSecrets = []string{"ngc-secret"}

	switch testCase {
	case "default":
		// Do nothing
	case "custom-config":
		cp.Spec.DevicePlugin.Config = &gpuv1.DevicePluginConfig{Name: "plugin-config", Default: "default"}
	default:
		return nil
	}

	return cp
}

// getDevicePluginTestOutput returns a map containing expected output for
// device-plugin test case. This function will grow as new test cases are added
func getDevicePluginTestOutput(testCase string) map[string]interface{} {
	// default output
	output := map[string]interface{}{
		"numDaemonsets":               1,
		"configManagerInitPresent":    false,
		"configManagerSidecarPresent": false,
		"devicePluginImage":           "nvcr.io/nvidia/k8s-device-plugin:v0.12.0-ubi8",
		"imagePullSecret":             "ngc-secret",
	}

	switch testCase {
	case "default":
		output["env"] = map[string]string{}
	case "custom-config":
		// Ensure config-manager containers are added
		output["configManagerInitPresent"] = true
		output["configManagerSidecarPresent"] = true
		output["env"] = map[string]string{
			"CONFIG_FILE": "/config/config.yaml",
		}
	default:
		return nil
	}

	return output
}

// TestDevicePlugin tests that the GPU Operator correctly deploys the device-plugin daemonset
// under various scenarios/config options
func TestDevicePlugin(t *testing.T) {
	testCases := []struct {
		description   string
		clusterPolicy *gpuv1.ClusterPolicy
		output        map[string]interface{}
	}{
		{
			"Default",
			getDevicePluginTestInput("default"),
			getDevicePluginTestOutput("default"),
		},
		{
			"CustomConfig",
			getDevicePluginTestInput("custom-config"),
			getDevicePluginTestOutput("custom-config"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			ds, err := testDaemonsetCommon(t, tc.clusterPolicy, "DevicePlugin", tc.output["numDaemonsets"].(int))
			if err != nil {
				t.Fatalf("error in testDaemonsetCommon(): %v", err)
			}
			if ds == nil {
				return
			}

			configManagerInitPresent := false
			configManagerSidecarPresent := false
			devicePluginImage := ""
			mainCtrIdx := 0
			for _, initContainer := range ds.Spec.Template.Spec.InitContainers {
				if initContainer.Name == "config-manager-init" {
					configManagerInitPresent = true
				}
			}
			for i, container := range ds.Spec.Template.Spec.Containers {
				if container.Name == "nvidia-device-plugin" {
					devicePluginImage = container.Image
					mainCtrIdx = i
					continue
				}
				if container.Name == "config-manager" {
					configManagerSidecarPresent = true
				}
			}

			require.Equal(t, tc.output["configManagerInitPresent"], configManagerInitPresent, "Unexpected configuration for config-manager init container")
			require.Equal(t, tc.output["configManagerSidecarPresent"], configManagerSidecarPresent, "Unexpected configuration for config-manager sidecar container")
			require.Equal(t, tc.output["devicePluginImage"], devicePluginImage, "Unexpected configuration for nvidia-device-plugin image")

			for key, value := range tc.output["env"].(map[string]string) {
				envFound := false
				for _, envVar := range ds.Spec.Template.Spec.Containers[mainCtrIdx].Env {
					if envVar.Name == key && envVar.Value == value {
						envFound = true
					}
				}
				if !envFound {
					t.Fatalf("Expected env is not set for daemonset nvidia-device-plugin %s->%s", key, value)
				}
			}

			// cleanup by deleting all kubernetes objects
			err = removeState(&clusterPolicyController, clusterPolicyController.idx-1)
			if err != nil {
				t.Fatalf("error removing state %v:", err)
			}
			clusterPolicyController.idx--
		})
	}
}

// getVGPUManagerTestInput return a ClusterPolicy instance for a particular
// driver test case. This function will grow as new test cases are added
func getVGPUManagerTestInput(testCase string) *gpuv1.ClusterPolicy {
	cp := clusterPolicy.DeepCopy()

	// Until we create sample ClusterPolicies that have all fields
	// set, hardcode some default values:
	cp.Spec.VGPUManager.Repository = "nvcr.io/nvidia"
	cp.Spec.VGPUManager.Image = "vgpu-manager"
	cp.Spec.VGPUManager.Version = "470.57.02"
	cp.Spec.VGPUManager.DriverManager.Repository = "nvcr.io/nvidia/cloud-native"
	cp.Spec.VGPUManager.DriverManager.Image = "k8s-driver-manager"
	cp.Spec.VGPUManager.DriverManager.Version = "v0.3.0"
	cp.Spec.VGPUManager.ImagePullSecrets = []string{"ngc-secret"}
	cp.Spec.VGPUManager.DriverManager.ImagePullSecrets = []string{"ngc-secret"}
	clusterPolicyController.sandboxEnabled = true
	cp.Spec.VGPUManager.KernelModuleConfig = &gpuv1.KernelModuleConfigSpec{Name: "vgpu-manager-kernel-module-config"}
	switch testCase {
	case "default":
		// Do nothing
	default:
		return nil
	}

	return cp
}

// getVGPUManagerTestOutput returns a map containing expected output for
// driver test case. This function will grow as new test cases are added
func getVGPUManagerTestOutput(testCase string) map[string]interface{} {
	// default output
	output := map[string]interface{}{
		"numDaemonsets":      1,
		"driverImage":        "nvcr.io/nvidia/vgpu-manager:470.57.02-ubuntu22.04",
		"driverManagerImage": "nvcr.io/nvidia/cloud-native/k8s-driver-manager:v0.3.0",
		"imagePullSecret":    "ngc-secret",
		"kernelModuleConfig": "vgpu-manager-kernel-module-config",
	}

	switch testCase {
	case "default":
		// Do nothing
	default:
		return nil
	}

	return output
}

// TestVGPUManager tests that the GPU Operator correctly deploys the driver daemonset
// under various scenarios/config options
func TestVGPUManager(t *testing.T) {
	testCases := []struct {
		description   string
		clusterPolicy *gpuv1.ClusterPolicy
		output        map[string]interface{}
	}{
		{
			"Default",
			getVGPUManagerTestInput("default"),
			getVGPUManagerTestOutput("default"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			// Create the kernel module ConfigMap
			if tc.clusterPolicy.Spec.VGPUManager.KernelModuleConfig != nil && tc.clusterPolicy.Spec.VGPUManager.KernelModuleConfig.Name != "" {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      tc.clusterPolicy.Spec.VGPUManager.KernelModuleConfig.Name,
						Namespace: clusterPolicyController.operatorNamespace,
					},
					Data: map[string]string{
						"nvidia.conf": "# Test vGPU manager kernel module configuration\n",
					},
				}
				err := clusterPolicyController.client.Create(clusterPolicyController.ctx, cm)
				if err != nil {
					t.Fatalf("error creating kernel module ConfigMap: %v", err)
				}
			}

			ds, err := testDaemonsetCommon(t, tc.clusterPolicy, "VGPUManager", tc.output["numDaemonsets"].(int))
			if err != nil {
				t.Fatalf("error in testDaemonsetCommon(): %v", err)
			}
			if ds == nil {
				return
			}
			driverImage := ""
			driverManagerImage := ""
			for _, initContainer := range ds.Spec.Template.Spec.InitContainers {
				if strings.Contains(initContainer.Name, "k8s-driver-manager") {
					driverManagerImage = initContainer.Image
					break
				}
			}
			for _, container := range ds.Spec.Template.Spec.Containers {
				if strings.Contains(container.Name, "nvidia-vgpu-manager-ctr") {
					driverImage = container.Image
					continue
				}
			}

			require.Equal(t, tc.output["driverImage"], driverImage, "Unexpected configuration for nvidia-vgpu-manager-ctr image")
			require.Equal(t, tc.output["driverManagerImage"], driverManagerImage, "Unexpected configuration for k8s-driver-manager image")

			// cleanup by deleting all kubernetes objects
			err = removeState(&clusterPolicyController, clusterPolicyController.idx-1)
			if err != nil {
				t.Fatalf("error removing state %v:", err)
			}
			clusterPolicyController.idx--
		})
	}
}

func TestVGPUManagerAssets(t *testing.T) {
	// Clear any KernelModuleConfig that might have been set by previous tests to avoid missing ConfigMap errors
	clusterPolicyController.singleton.Spec.VGPUManager.KernelModuleConfig = nil

	manifestPath := filepath.Join(cfg.root, vGPUManagerAssetsPath)
	// add manifests
	addState(&clusterPolicyController, manifestPath)

	// create resources
	_, err := clusterPolicyController.step()
	if err != nil {
		t.Errorf("error creating resources: %v", err)
	}
}

// getSandboxDevicePluginTestInput return a ClusterPolicy instance for a particular
// device plugin test case. This function will grow as new test cases are added
func getSandboxDevicePluginTestInput(testCase string) *gpuv1.ClusterPolicy {
	cp := clusterPolicy.DeepCopy()

	// Until we create sample ClusterPolicies that have all fields
	// set, hardcode some default values:
	cp.Spec.SandboxWorkloads.Mode = "kubevirt"
	cp.Spec.SandboxDevicePlugin.Repository = "nvcr.io/nvidia"
	cp.Spec.SandboxDevicePlugin.Image = "kubevirt-device-plugin"
	cp.Spec.SandboxDevicePlugin.Version = "v1.1.0"
	clusterPolicyController.sandboxEnabled = true
	cp.Spec.SandboxDevicePlugin.ImagePullSecrets = []string{"ngc-secret"}

	cp.Spec.Validator.Repository = "nvcr.io/nvidia/cloud-native"
	cp.Spec.Validator.Image = "gpu-operator-validator"
	cp.Spec.Validator.Version = "v1.11.0"
	cp.Spec.Validator.ImagePullSecrets = []string{"ngc-secret"}

	switch testCase {
	case "default":
		// Do nothing
	default:
		return nil
	}

	return cp
}

// getSandboxDevicePluginTestOutput returns a map containing expected output for
// driver test case. This function will grow as new test cases are added
func getSandboxDevicePluginTestOutput(testCase string) map[string]interface{} {
	// default output
	output := map[string]interface{}{
		"numDaemonsets":   1,
		"image":           "nvcr.io/nvidia/kubevirt-device-plugin:v1.1.0",
		"imagePullSecret": "ngc-secret",
	}

	switch testCase {
	case "default":
		// Do nothing
	default:
		return nil
	}

	return output
}

// TestSandboxDevicePlugin tests that the GPU Operator correctly deploys the sandbox-device-plugin
// daemonset under various scenarios/config options
func TestSandboxDevicePlugin(t *testing.T) {
	testCases := []struct {
		description   string
		clusterPolicy *gpuv1.ClusterPolicy
		output        map[string]interface{}
	}{
		{
			"Default",
			getSandboxDevicePluginTestInput("default"),
			getSandboxDevicePluginTestOutput("default"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			ds, err := testDaemonsetCommon(t, tc.clusterPolicy, "SandboxDevicePlugin", tc.output["numDaemonsets"].(int))
			if err != nil {
				t.Fatalf("error in testDaemonsetCommon(): %v", err)
			}
			if ds == nil {
				return
			}

			image := ""
			for _, container := range ds.Spec.Template.Spec.Containers {
				if strings.Contains(container.Name, "nvidia-sandbox-device-plugin-ctr") {
					image = container.Image
					continue
				}
			}

			require.Equal(t, tc.output["image"], image, "Unexpected configuration for nvidia-sandbox-device-plugin-ctr image")

			// cleanup by deleting all kubernetes objects
			err = removeState(&clusterPolicyController, clusterPolicyController.idx-1)
			if err != nil {
				t.Fatalf("error removing state %v:", err)
			}
			clusterPolicyController.idx--
		})
	}
}

func TestSandboxDevicePluginAssets(t *testing.T) {
	manifestPath := filepath.Join(cfg.root, sandboxDevicePluginAssetsPath)
	// add manifests
	addState(&clusterPolicyController, manifestPath)

	// create resources
	_, err := clusterPolicyController.step()
	if err != nil {
		t.Errorf("error creating resources: %v", err)
	}
}

// getKataDevicePluginTestInput returns a ClusterPolicy instance for a particular
// kata device plugin test case. Kata device plugin is implied when sandboxWorkloads.mode is "kata".
func getKataDevicePluginTestInput(testCase string) *gpuv1.ClusterPolicy {
	cp := clusterPolicy.DeepCopy()

	cp.Spec.KataSandboxDevicePlugin.Repository = "nvcr.io/nvidia"
	cp.Spec.KataSandboxDevicePlugin.Image = "kata-gpu-device-plugin"
	cp.Spec.KataSandboxDevicePlugin.Version = "v0.0.1"
	clusterPolicyController.sandboxEnabled = true
	cp.Spec.SandboxWorkloads.Enabled = boolTrue
	cp.Spec.SandboxWorkloads.Mode = "kata"
	cp.Spec.KataSandboxDevicePlugin.Enabled = boolTrue
	cp.Spec.KataSandboxDevicePlugin.ImagePullSecrets = []string{"ngc-secret"}

	cp.Spec.Validator.Repository = "nvcr.io/nvidia/cloud-native"
	cp.Spec.Validator.Image = "gpu-operator-validator"
	cp.Spec.Validator.Version = "v1.11.0"
	cp.Spec.Validator.ImagePullSecrets = []string{"ngc-secret"}

	switch testCase {
	case "default":
		// Do nothing
	default:
		return nil
	}

	return cp
}

// getKataDevicePluginTestOutput returns a map containing expected output for
// kata device plugin test case.
func getKataDevicePluginTestOutput(testCase string) map[string]interface{} {
	output := map[string]interface{}{
		"numDaemonsets":   1,
		"image":           "nvcr.io/nvidia/kata-gpu-device-plugin:v0.0.1",
		"imagePullSecret": "ngc-secret",
	}

	switch testCase {
	case "default":
		// Do nothing
	default:
		return nil
	}

	return output
}

// TestKataDevicePlugin tests that the GPU Operator correctly deploys the kata-device-plugin
// daemonset when sandboxWorkloads.mode is "kata".
func TestKataDevicePlugin(t *testing.T) {
	testCases := []struct {
		description   string
		clusterPolicy *gpuv1.ClusterPolicy
		output        map[string]interface{}
	}{
		{
			"Default",
			getKataDevicePluginTestInput("default"),
			getKataDevicePluginTestOutput("default"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			ds, err := testDaemonsetCommon(t, tc.clusterPolicy, "KataDevicePlugin", tc.output["numDaemonsets"].(int))
			if err != nil {
				t.Fatalf("error in testDaemonsetCommon(): %v", err)
			}
			if ds == nil {
				return
			}

			image := ""
			for _, container := range ds.Spec.Template.Spec.Containers {
				if strings.Contains(container.Name, "nvidia-kata-sandbox-device-plugin-ctr") {
					image = container.Image
					continue
				}
			}

			require.Equal(t, tc.output["image"], image, "Unexpected configuration for nvidia-kata-sandbox-device-plugin-ctr image")

			// cleanup by deleting all kubernetes objects
			err = removeState(&clusterPolicyController, clusterPolicyController.idx-1)
			if err != nil {
				t.Fatalf("error removing state %v:", err)
			}
			clusterPolicyController.idx--
		})
	}
}

func TestKataDevicePluginAssets(t *testing.T) {
	manifestPath := filepath.Join(cfg.root, kataDevicePluginAssetsPath)
	// add manifests
	addState(&clusterPolicyController, manifestPath)

	// create resources
	_, err := clusterPolicyController.step()
	if err != nil {
		t.Errorf("error creating resources: %v", err)
	}
}

// getDCGMExporterTestInput return a ClusterPolicy instance for a particular
// dcgm-exporter test case.
func getDCGMExporterTestInput(testCase string) *gpuv1.ClusterPolicy {
	cp := clusterPolicy.DeepCopy()

	// Set some default values
	cp.Spec.DCGMExporter.Repository = "nvcr.io/nvidia/k8s"
	cp.Spec.DCGMExporter.Image = "dcgm-exporter"
	cp.Spec.DCGMExporter.Version = "3.3.0-3.2.0-ubuntu22.04"
	cp.Spec.DCGMExporter.ImagePullSecrets = []string{"ngc-secret"}

	cp.Spec.Validator.Repository = "nvcr.io/nvidia/cloud-native"
	cp.Spec.Validator.Image = "gpu-operator-validator"
	cp.Spec.Validator.Version = "v23.9.2"
	cp.Spec.Validator.ImagePullSecrets = []string{"ngc-secret"}

	switch testCase {
	case "default":
		// Do nothing
	case "standalone-dcgm":
		dcgmEnabled := true
		cp.Spec.DCGM.Enabled = &dcgmEnabled
	default:
		return nil
	}

	return cp
}

// getDCGMExporterTestOutput returns a map containing expected output for
// dcgm-exporter test case.
func getDCGMExporterTestOutput(testCase string) map[string]interface{} {
	// default output
	output := map[string]interface{}{
		"numDaemonsets":     1,
		"dcgmExporterImage": "nvcr.io/nvidia/k8s/dcgm-exporter:3.3.0-3.2.0-ubuntu22.04",
		"imagePullSecret":   "ngc-secret",
	}

	switch testCase {
	case "default":
		output["env"] = map[string]string{}
	case "standalone-dcgm":
		output["env"] = map[string]string{
			"DCGM_REMOTE_HOSTENGINE_INFO": "nvidia-dcgm:5555",
		}
	default:
		return nil
	}

	return output
}

// TestDCGMExporter tests that the GPU Operator correctly deploys the dcgm-exporter daemonset
// under various scenarios/config options
func TestDCGMExporter(t *testing.T) {
	testCases := []struct {
		description   string
		clusterPolicy *gpuv1.ClusterPolicy
		output        map[string]interface{}
	}{
		{
			"Default",
			getDCGMExporterTestInput("default"),
			getDCGMExporterTestOutput("default"),
		},
		{
			"StandalongDCGM",
			getDCGMExporterTestInput("standalone-dcgm"),
			getDCGMExporterTestOutput("standalone-dcgm"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			ds, err := testDaemonsetCommon(t, tc.clusterPolicy, "DCGMExporter", tc.output["numDaemonsets"].(int))
			if err != nil {
				t.Fatalf("error in testDaemonsetCommon(): %v", err)
			}
			if ds == nil {
				return
			}

			dcgmExporterImage := ""
			for _, container := range ds.Spec.Template.Spec.Containers {
				if container.Name == "nvidia-dcgm-exporter" {
					dcgmExporterImage = container.Image
					break
				}
			}
			for key, value := range tc.output["env"].(map[string]string) {
				envFound := false
				for _, envVar := range ds.Spec.Template.Spec.Containers[0].Env {
					if envVar.Name == key && envVar.Value == value {
						envFound = true
					}
				}
				if !envFound {
					t.Fatalf("Expected env is not set for daemonset nvidia-dcgm-exporter %s->%s", key, value)
				}
			}

			require.Equal(t, tc.output["dcgmExporterImage"], dcgmExporterImage, "Unexpected configuration for dcgm-exporter image")

			// cleanup by deleting all kubernetes objects
			err = removeState(&clusterPolicyController, clusterPolicyController.idx-1)
			if err != nil {
				t.Fatalf("error removing state %v:", err)
			}
			clusterPolicyController.idx--
		})
	}
}

func TestGetSanitizedKernelVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"5.14.0-427.37.1.el9_4.aarch64_64k", "5.14.0-427.37.1.el9.4"},
		{"5.14.0-427.37.1.el9_4.aarch64", "5.14.0-427.37.1.el9.4"},
		{"5.14.0-427.37.1.el9_4.x86_64_64k", "5.14.0-427.37.1.el9.4"},
		{"5.14.0-427.37.1.el9_4.x86_64", "5.14.0-427.37.1.el9.4"},
	}

	for _, test := range tests {
		result := getSanitizedKernelVersion(test.input)
		require.NotEmpty(t, result)
		require.Equal(t, test.expected, result)
	}
}

func TestServiceMonitor(t *testing.T) {
	const (
		testNamespace      = "test-namespace"
		testServiceMonitor = "test-service-monitor"
		filledNamespace    = "FILLED BY THE OPERATOR"
	)

	// Create scheme with required types
	scheme := runtime.NewScheme()
	require.NoError(t, promv1.AddToScheme(scheme))
	require.NoError(t, apiextensionsv1.AddToScheme(scheme))
	require.NoError(t, gpuv1.AddToScheme(scheme))

	serviceMonitor := promv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{Name: testServiceMonitor, Labels: map[string]string{}},
		Spec: promv1.ServiceMonitorSpec{
			NamespaceSelector: promv1.NamespaceSelector{MatchNames: []string{filledNamespace}},
			Endpoints:         []promv1.Endpoint{{}},
		},
	}

	// Create controller with given spec and state
	newController := func(k8s client.Client, scheme *runtime.Scheme, spec gpuv1.ClusterPolicySpec, state string) ClusterPolicyController {
		clusterPolicy := &gpuv1.ClusterPolicy{Spec: spec}
		resources := []Resources{{ServiceMonitor: serviceMonitor}}

		return ClusterPolicyController{
			client:            k8s,
			ctx:               context.Background(),
			singleton:         clusterPolicy,
			scheme:            scheme,
			operatorNamespace: testNamespace,
			resources:         resources,
			stateNames:        []string{state},
			idx:               0,
			logger:            ctrl.Log.WithName("test"),
		}
	}

	// CRD object for tests that need ServiceMonitor CRD present
	serviceMonitorCRD := &apiextensionsv1.CustomResourceDefinition{
		TypeMeta:   metav1.TypeMeta{Kind: "CustomResourceDefinition"},
		ObjectMeta: metav1.ObjectMeta{Name: ServiceMonitorCRDName},
	}

	tests := []struct {
		description            string
		stateName              string
		k8sObjects             []client.Object
		clusterPolicySpec      gpuv1.ClusterPolicySpec
		expectedState          gpuv1.State
		expectedServiceMonitor *promv1.ServiceMonitor
	}{
		{
			description: "dcgm-exporter disabled, CRD missing -> Ready",
			stateName:   "state-dcgm-exporter",
			k8sObjects:  nil,
			clusterPolicySpec: gpuv1.ClusterPolicySpec{
				DCGMExporter: gpuv1.DCGMExporterSpec{Enabled: ptr.To(false)},
			},
			expectedState:          gpuv1.Ready,
			expectedServiceMonitor: nil,
		},
		{
			description: "dcgm-exporter SM enabled, CRD missing -> NotReady",
			stateName:   "state-dcgm-exporter",
			k8sObjects:  nil,
			clusterPolicySpec: gpuv1.ClusterPolicySpec{
				DCGMExporter: gpuv1.DCGMExporterSpec{
					Enabled:        ptr.To(true),
					ServiceMonitor: &gpuv1.DCGMExporterServiceMonitorConfig{Enabled: ptr.To(true)},
				},
			},
			expectedState:          gpuv1.NotReady,
			expectedServiceMonitor: nil,
		},
		{
			description: "dcgm-exporter SM disabled, CRD present -> Disabled (delete if exists)",
			stateName:   "state-dcgm-exporter",
			k8sObjects:  []client.Object{serviceMonitorCRD},
			clusterPolicySpec: gpuv1.ClusterPolicySpec{
				DCGMExporter: gpuv1.DCGMExporterSpec{
					Enabled:        ptr.To(true),
					ServiceMonitor: &gpuv1.DCGMExporterServiceMonitorConfig{Enabled: ptr.To(false)},
				},
			},
			expectedState:          gpuv1.Disabled,
			expectedServiceMonitor: nil,
		},
		{
			description:            "operator-metrics, CRD missing -> Ready (ignore create)",
			stateName:              "state-operator-metrics",
			k8sObjects:             nil,
			clusterPolicySpec:      gpuv1.ClusterPolicySpec{},
			expectedState:          gpuv1.Ready,
			expectedServiceMonitor: nil,
		},
		{
			description: "node-status-exporter disabled, CRD present -> Disabled",
			stateName:   "state-node-status-exporter",
			k8sObjects:  []client.Object{serviceMonitorCRD},
			clusterPolicySpec: gpuv1.ClusterPolicySpec{
				NodeStatusExporter: gpuv1.NodeStatusExporterSpec{Enabled: ptr.To(false)},
			},
			expectedState:          gpuv1.Disabled,
			expectedServiceMonitor: nil,
		},
		{
			description: "dcgm-exporter SM enabled, CRD present -> Ready and applies edits",
			stateName:   "state-dcgm-exporter",
			k8sObjects:  []client.Object{serviceMonitorCRD},
			clusterPolicySpec: gpuv1.ClusterPolicySpec{
				DCGMExporter: gpuv1.DCGMExporterSpec{
					Enabled: ptr.To(true),
					ServiceMonitor: &gpuv1.DCGMExporterServiceMonitorConfig{
						Enabled:          ptr.To(true),
						Interval:         promv1.Duration("15s"),
						HonorLabels:      ptr.To(true),
						AdditionalLabels: map[string]string{"a": "b"},
						Relabelings:      []*promv1.RelabelConfig{{Action: "keep"}},
					},
				},
			},
			expectedState: gpuv1.Ready,
			expectedServiceMonitor: &promv1.ServiceMonitor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service-monitor",
					Namespace: "test-namespace",
					Labels:    map[string]string{"a": "b"},
				},
				Spec: promv1.ServiceMonitorSpec{
					NamespaceSelector: promv1.NamespaceSelector{MatchNames: []string{"test-namespace"}},
					Endpoints: []promv1.Endpoint{{
						Interval:    promv1.Duration("15s"),
						HonorLabels: true,
						RelabelConfigs: []promv1.RelabelConfig{{
							Action: "keep",
						}},
					}},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.k8sObjects...).
				Build()

			controller := newController(k8sClient, scheme, tc.clusterPolicySpec, tc.stateName)

			// Calls the actual ServiceMonitor function under test and validates the state
			state, err := ServiceMonitor(controller)

			require.NoError(t, err)
			require.Equal(t, tc.expectedState, state)

			found := &promv1.ServiceMonitor{}
			err = k8sClient.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: testServiceMonitor}, found)
			if tc.expectedServiceMonitor == nil {
				require.True(t, apierrors.IsNotFound(err))
				return
			}
			require.NoError(t, err)

			require.Equal(t, tc.expectedServiceMonitor.Name, found.Name)
			require.Equal(t, tc.expectedServiceMonitor.Namespace, found.Namespace)
			require.Equal(t, tc.expectedServiceMonitor.Labels, found.Labels)
			require.Equal(t, tc.expectedServiceMonitor.Spec, found.Spec)
		})
	}
}

func TestService(t *testing.T) {
	const (
		testNamespace = "test-namespace"
		testService   = "nvidia-dcgm-exporter"
	)

	// Helper to create scheme with required types
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, gpuv1.AddToScheme(scheme))

	// Template Service
	service := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: testService},
		Spec:       corev1.ServiceSpec{},
	}

	// Helper to create controller with given spec
	newController := func(k8s client.Client, scheme *runtime.Scheme, spec gpuv1.ClusterPolicySpec) ClusterPolicyController {
		clusterPolicy := &gpuv1.ClusterPolicy{Spec: spec}
		resources := []Resources{{Service: service}}
		return ClusterPolicyController{
			client:            k8s,
			ctx:               context.Background(),
			singleton:         clusterPolicy,
			scheme:            scheme,
			operatorNamespace: testNamespace,
			resources:         resources,
			stateNames:        []string{"state-dcgm-exporter"},
			idx:               0,
			logger:            ctrl.Log.WithName("test"),
		}
	}

	localPolicy := corev1.ServiceInternalTrafficPolicyLocal

	tests := []struct {
		description       string
		k8sObjects        []client.Object
		clusterPolicySpec gpuv1.ClusterPolicySpec
		expectedState     gpuv1.State
		expectService     bool
		expectedType      corev1.ServiceType
		expectedPolicy    *corev1.ServiceInternalTrafficPolicy
		expectedIP        string // For ClusterIP preservation test
	}{
		{
			description: "create and preprocess",
			k8sObjects:  nil,
			clusterPolicySpec: gpuv1.ClusterPolicySpec{
				DCGMExporter: gpuv1.DCGMExporterSpec{
					Enabled: ptr.To(true),
					ServiceSpec: &gpuv1.DCGMExporterServiceConfig{
						Type:                  corev1.ServiceTypeNodePort,
						InternalTrafficPolicy: &localPolicy,
					},
				},
			},
			expectedState:  gpuv1.Ready,
			expectService:  true,
			expectedType:   corev1.ServiceTypeNodePort,
			expectedPolicy: &localPolicy,
		},
		{
			description: "update preserves ClusterIP",
			k8sObjects: []client.Object{&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: testService, Namespace: testNamespace},
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "10.0.0.42",
				},
			}},
			clusterPolicySpec: gpuv1.ClusterPolicySpec{
				DCGMExporter: gpuv1.DCGMExporterSpec{
					Enabled: ptr.To(true),
					ServiceSpec: &gpuv1.DCGMExporterServiceConfig{
						Type:                  corev1.ServiceTypeNodePort,
						InternalTrafficPolicy: &localPolicy,
					},
				},
			},
			expectedState:  gpuv1.Ready,
			expectService:  true,
			expectedType:   corev1.ServiceTypeNodePort,
			expectedPolicy: &localPolicy,
			expectedIP:     "10.0.0.42",
		},
		{
			description: "disabled deletes and returns Disabled",
			k8sObjects: []client.Object{&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: testService, Namespace: testNamespace},
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "10.0.0.42",
				},
			}},
			clusterPolicySpec: gpuv1.ClusterPolicySpec{
				DCGMExporter: gpuv1.DCGMExporterSpec{Enabled: ptr.To(false)},
			},
			expectedState: gpuv1.Disabled,
			expectService: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.k8sObjects...).
				Build()

			controller := newController(k8sClient, scheme, tc.clusterPolicySpec)

			state, err := Service(controller)
			require.NoError(t, err)
			require.Equal(t, tc.expectedState, state)

			found := &corev1.Service{}
			err = k8sClient.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: testService}, found)
			if !tc.expectService {
				require.True(t, apierrors.IsNotFound(err))
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expectedType, found.Spec.Type)
			if tc.expectedPolicy != nil {
				require.NotNil(t, found.Spec.InternalTrafficPolicy)
				require.Equal(t, *tc.expectedPolicy, *found.Spec.InternalTrafficPolicy)
			}
			if tc.expectedIP != "" {
				require.Equal(t, tc.expectedIP, found.Spec.ClusterIP)
			}
		})
	}
}

func TestCertConfigPathMap(t *testing.T) {
	expectedPaths := map[string]string{
		"centos":   "/etc/pki/ca-trust/extracted/pem",
		"debian":   "/usr/local/share/ca-certificates",
		"ubuntu":   "/usr/local/share/ca-certificates",
		"rhcos":    "/etc/pki/ca-trust/extracted/pem",
		"rhel":     "/etc/pki/ca-trust/extracted/pem",
		"sles":     "/etc/pki/trust/anchors",
		"sl-micro": "/etc/pki/trust/anchors",
	}

	for os, expectedPath := range expectedPaths {
		path, ok := CertConfigPathMap[os]
		require.True(t, ok, "OS %s not found in CertConfigPathMap", os)
		require.Equal(t, expectedPath, path, "Incorrect path for OS %s", os)
	}
}

func TestRepoConfigPathMap(t *testing.T) {
	expected := map[string]string{
		"ubuntu":   "/etc/apt/sources.list.d",
		"rhcos":    "/etc/yum.repos.d",
		"rhel":     "/etc/yum.repos.d",
		"sles":     "/etc/zypp/repos.d",
		"sl-micro": "/etc/zypp/repos.d",
	}

	for os, path := range expected {
		val, ok := RepoConfigPathMap[os]
		require.True(t, ok, "Expected %s to be in RepoConfigPathMap", os)
		require.Equal(t, path, val, "Expected path for %s to be %s", os, path)
	}
}

func TestRuntimeClasses(t *testing.T) {
	const (
		testNamespace = "test-namespace"
	)

	// Create scheme with required types
	scheme := runtime.NewScheme()
	require.NoError(t, nodev1.AddToScheme(scheme))
	require.NoError(t, nodev1beta1.AddToScheme(scheme))
	require.NoError(t, apiextensionsv1.AddToScheme(scheme))
	require.NoError(t, gpuv1.AddToScheme(scheme))

	// Create controller with given spec and state
	newController := func(k8s client.Client, scheme *runtime.Scheme, spec gpuv1.ClusterPolicySpec, state string) ClusterPolicyController {
		clusterPolicy := &gpuv1.ClusterPolicy{Spec: spec}
		resources := []Resources{
			{
				RuntimeClasses: []nodev1.RuntimeClass{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "nvidia",
						},
						TypeMeta: metav1.TypeMeta{
							Kind:       "RuntimeClass",
							APIVersion: "node.k8s.io/v1",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "nvidia-cdi",
						},
						TypeMeta: metav1.TypeMeta{
							Kind:       "RuntimeClass",
							APIVersion: "node.k8s.io/v1",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "nvidia-legacy",
						},
						TypeMeta: metav1.TypeMeta{
							Kind:       "RuntimeClass",
							APIVersion: "node.k8s.io/v1",
						},
					},
				},
			},
		}

		return ClusterPolicyController{
			client:            k8s,
			ctx:               context.Background(),
			singleton:         clusterPolicy,
			scheme:            scheme,
			operatorNamespace: testNamespace,
			resources:         resources,
			stateNames:        []string{state},
			idx:               0,
			logger:            ctrl.Log.WithName("test"),
		}
	}

	tests := []struct {
		description            string
		stateName              string
		k8sVersion             string
		k8sObjects             []client.Object
		clusterPolicySpec      gpuv1.ClusterPolicySpec
		expectedState          gpuv1.State
		expectedRuntimeClasses []string
	}{
		{
			description: "CDI enabled",
			stateName:   "pre-requisites",
			k8sVersion:  "v1.33.0",
			k8sObjects:  nil,
			clusterPolicySpec: gpuv1.ClusterPolicySpec{
				CDI: gpuv1.CDIConfigSpec{Enabled: ptr.To(true)},
			},
			expectedState:          gpuv1.Ready,
			expectedRuntimeClasses: []string{"nvidia", "nvidia-legacy", "nvidia-cdi"},
		},
		{
			description: "CDI and NRI Plugin Enabled",
			stateName:   "pre-requisites",
			k8sVersion:  "v1.33.0",
			k8sObjects:  nil,
			clusterPolicySpec: gpuv1.ClusterPolicySpec{
				CDI: gpuv1.CDIConfigSpec{
					Enabled:          ptr.To(true),
					NRIPluginEnabled: ptr.To(true),
				},
			},
			expectedState:          gpuv1.Ready,
			expectedRuntimeClasses: []string{},
		},
		{
			description: "CDI and NRI Plugin Enabled with pre-existing runtime class",
			stateName:   "pre-requisites",
			k8sVersion:  "v1.33.0",
			k8sObjects: []client.Object{
				&nodev1.RuntimeClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "nvidia",
					},
				},
				&nodev1.RuntimeClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "nvidia-legacy",
					},
				},
				&nodev1.RuntimeClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "nvidia-cdi",
					},
				},
			},
			clusterPolicySpec: gpuv1.ClusterPolicySpec{
				CDI: gpuv1.CDIConfigSpec{
					Enabled:          ptr.To(true),
					NRIPluginEnabled: ptr.To(true),
				},
			},
			expectedState:          gpuv1.Ready,
			expectedRuntimeClasses: []string{},
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(test.k8sObjects...).
				Build()

			controller := newController(k8sClient, scheme, test.clusterPolicySpec, test.stateName)
			controller.k8sVersion = test.k8sVersion

			state, err := RuntimeClasses(controller)
			require.NoError(t, err)
			require.Equal(t, test.expectedState, state)

			for _, expectedRuntimeClass := range test.expectedRuntimeClasses {
				rcObject := &nodev1.RuntimeClass{}
				err := k8sClient.Get(t.Context(), client.ObjectKey{Name: expectedRuntimeClass}, rcObject)
				require.NoError(t, err)
				require.Equal(t, expectedRuntimeClass, rcObject.Name)
			}

		})
	}
}

// getMIGManagerTestInput returns a ClusterPolicy instance for a given MIG Manager test case
func getMIGManagerTestInput(testCase string) *gpuv1.ClusterPolicy {
	cp := clusterPolicy.DeepCopy()

	// Set default values for MIG Manager
	cp.Spec.MIGManager.Repository = "nvcr.io/nvidia/cloud-native"
	cp.Spec.MIGManager.Image = "k8s-mig-manager"
	cp.Spec.MIGManager.Version = "v0.5.0"
	cp.Spec.MIGManager.ImagePullSecrets = []string{"ngc-secret"}

	// Validator is required for all daemonset tests
	cp.Spec.Validator.Repository = "nvcr.io/nvidia/cloud-native"
	cp.Spec.Validator.Image = "gpu-operator-validator"
	cp.Spec.Validator.Version = "v1.11.0"
	cp.Spec.Validator.ImagePullSecrets = []string{"ngc-secret"}

	switch testCase {
	case "default":
		// No custom config
	case "custom-config":
		cp.Spec.MIGManager.Config = &gpuv1.MIGPartedConfigSpec{Name: "custom-mig-config"}
	default:
		return nil
	}

	return cp
}

// getMIGManagerTestOutput returns expected output for a MIG Manager test case
func getMIGManagerTestOutput(testCase string) map[string]interface{} {
	output := map[string]interface{}{
		"numDaemonsets":          1,
		"migManagerImage":        "nvcr.io/nvidia/cloud-native/k8s-mig-manager:v0.5.0",
		"imagePullSecret":        "ngc-secret",
		"migConfigVolumePresent": true,
		"env": map[string]string{
			"DEFAULT_CONFIG_FILE": "/mig-parted-config/config-default.yaml",
		},
	}

	switch testCase {
	case "default":
	case "custom-config":
		output["env"] = map[string]string{
			"CONFIG_FILE": "/mig-parted-config/config.yaml",
		}
	default:
		return nil
	}

	return output
}

// TestMIGManager tests that the GPU Operator correctly deploys the mig-manager daemonset
// under various scenarios/config options
func TestMIGManager(t *testing.T) {
	testCases := []struct {
		description   string
		clusterPolicy *gpuv1.ClusterPolicy
		output        map[string]interface{}
	}{
		{
			"Default",
			getMIGManagerTestInput("default"),
			getMIGManagerTestOutput("default"),
		},
		{
			"CustomConfig",
			getMIGManagerTestInput("custom-config"),
			getMIGManagerTestOutput("custom-config"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			ds, err := testDaemonsetCommon(t, tc.clusterPolicy, "MIGManager", tc.output["numDaemonsets"].(int))
			if err != nil {
				t.Fatalf("error in testDaemonsetCommon(): %v", err)
			}
			if ds == nil {
				return
			}

			migManagerImage := ""
			mainCtrIdx := 0
			migConfigVolumePresent := false

			// Find nvidia-mig-manager container and check image
			for i, container := range ds.Spec.Template.Spec.Containers {
				if container.Name == "nvidia-mig-manager" {
					migManagerImage = container.Image
					mainCtrIdx = i
					break
				}
			}

			// Check for mig-parted-config volume
			for _, vol := range ds.Spec.Template.Spec.Volumes {
				if vol.Name == "mig-parted-config" {
					migConfigVolumePresent = true
					break
				}
			}

			require.Equal(t, tc.output["migManagerImage"], migManagerImage, "Unexpected configuration for mig-manager image")
			require.Equal(t, tc.output["migConfigVolumePresent"], migConfigVolumePresent, "Unexpected configuration for mig-parted-config volume")

			// Check expected env vars
			for key, value := range tc.output["env"].(map[string]string) {
				envFound := false
				for _, envVar := range ds.Spec.Template.Spec.Containers[mainCtrIdx].Env {
					if envVar.Name == key && envVar.Value == value {
						envFound = true
					}
				}
				if !envFound {
					t.Fatalf("Expected env is not set for daemonset mig-manager %s->%s", key, value)
				}
			}

			// cleanup by deleting all kubernetes objects
			err = removeState(&clusterPolicyController, clusterPolicyController.idx-1)
			if err != nil {
				t.Fatalf("error removing state %v:", err)
			}
			clusterPolicyController.idx--
		})
	}
}
