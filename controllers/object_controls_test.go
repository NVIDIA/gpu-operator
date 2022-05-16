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

	gpuv1 "github.com/NVIDIA/gpu-operator/api/v1"
	secv1 "github.com/openshift/api/security/v1"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1beta1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedv1 "k8s.io/api/scheduling/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	clusterPolicyPath      = "config/samples/v1_clusterpolicy.yaml"
	clusterPolicyName      = "gpu-cluster-policy"
	driverAssetsPath       = "assets/state-driver/"
	vGPUManagerAssetsPath  = "assets/state-vgpu-manager/"
	driverDaemonsetName    = "nvidia-driver-daemonset"
	devicePluginAssetsPath = "assets/state-device-plugin/"
	nfdNvidiaPCILabelKey   = "feature.node.kubernetes.io/pci-10de.present"
	nfdOsNameLabelKey      = "feature.node.kubernetes.io/system-os_release.ID"
	nfdOsVersionLabelKey   = "feature.node.kubernetes.io/system-os_release.VERSION_ID"
)

type testConfig struct {
	root  string
	nodes int
}

var (
	cfg                     *testConfig
	clusterPolicyController ClusterPolicyController
	clusterPolicyReconciler ClusterPolicyReconciler
	clusterPolicy           gpuv1.ClusterPolicy
	boolTrue                *bool
	boolFalse               *bool
)

var nfdLabels = map[string]string{
	nfdNvidiaPCILabelKey: "true",
	nfdKernelLabelKey:    "5.4.0",
	nfdOsNameLabelKey:    "ubuntu",
	nfdOsVersionLabelKey: "18.04",
}

var kubernetesResources = []client.Object{
	&corev1.ServiceAccount{},
	&rbacv1.Role{},
	&rbacv1.RoleBinding{},
	&rbacv1.ClusterRole{},
	&rbacv1.ClusterRoleBinding{},
	&corev1.ConfigMap{},
	&appsv1.DaemonSet{},
	&appsv1.Deployment{},
	&corev1.Pod{},
	&corev1.Service{},
	&promv1.ServiceMonitor{},
	&schedv1.PriorityClass{},
	//&corev1.Taint{},
	&secv1.SecurityContextConstraints{},
	&policyv1beta1.PodSecurityPolicy{},
	&corev1.Namespace{},
	&nodev1.RuntimeClass{},
	&promv1.PrometheusRule{},
}

type commonDaemonsetSpec struct {
	repository       string
	image            string
	version          string
	imagePullPolicy  string
	imagePullSecrets []corev1.LocalObjectReference
	args             []string
	env              []corev1.EnvVar
	resources        *corev1.ResourceRequirements
}

func TestMain(m *testing.M) {
	_, filename, _, _ := goruntime.Caller(0)
	moduleRoot, err := getModuleRoot(filename)
	if err != nil {
		log.Fatalf("error in test setup: could not get module root: %v", err)
	}
	cfg = &testConfig{root: moduleRoot, nodes: 1}

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

// setup creates a mock kubernetes cluster and client. Nodes are labeled with the minumum
// required NFD labels to be detected as GPU nodes by the GPU Operator. A sample
// ClusterPolicy resource is applied to the cluster. The ClusterPolicyController
// object is initialized with the mock kubernetes client as well as other steps
// mimicking init() in state_manager.go
func setup() error {
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
	if err := secv1.AddToScheme(s); err != nil {
		return fmt.Errorf("unable to add secv1 schema: %v", err)
	}

	client, err := newCluster(cfg.nodes, s)
	if err != nil {
		return fmt.Errorf("unable to create cluster: %v", err)
	}

	// Get a sample ClusterPolicy manifest
	manifests := getAssetsFrom(&clusterPolicyController, filepath.Join(cfg.root, clusterPolicyPath), "")
	clusterPolicyManifest := manifests[0]
	ser := json.NewYAMLSerializer(json.DefaultMetaFactory, scheme.Scheme, scheme.Scheme)
	_, _, err = ser.Decode(clusterPolicyManifest, nil, &clusterPolicy)
	if err != nil {
		return fmt.Errorf("failed to decode sample ClusterPolicy manifest: %v", err)
	}

	err = client.Create(context.TODO(), &clusterPolicy)
	if err != nil {
		return fmt.Errorf("failed to create ClusterPolicy resource: %v", err)
	}

	// Confirm ClusterPolicy is deployed in mock cluster
	cp := &gpuv1.ClusterPolicy{}
	err = client.Get(context.TODO(), types.NamespacedName{Namespace: "", Name: clusterPolicyName}, cp)
	if err != nil {
		return fmt.Errorf("unable to get ClusterPolicy from client: %v", err)
	}

	opts := zap.Options{
		Development: true,
	}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	clusterPolicyReconciler = ClusterPolicyReconciler{
		Client: client,
		Log:    ctrl.Log.WithName("controller").WithName("ClusterPolicy"),
		Scheme: s,
	}

	clusterPolicyController = ClusterPolicyController{
		singleton: cp,
		rec:       &clusterPolicyReconciler,
	}

	clusterPolicyController.operatorMetrics = initOperatorMetrics(&clusterPolicyController)

	hasNFDLabels, gpuNodeCount, err := clusterPolicyController.labelGPUNodes()
	if err != nil {
		return fmt.Errorf("unable to label nodes in cluster: %v", err)
	}
	if gpuNodeCount == 0 {
		return fmt.Errorf("no gpu nodes in mock cluster")
	}

	clusterPolicyController.hasGPUNodes = gpuNodeCount != 0
	clusterPolicyController.hasNFDLabels = hasNFDLabels

	return nil
}

// newCluster creates a mock kubernetes cluster and returns the corresponding client object
func newCluster(nodes int, s *runtime.Scheme) (client.Client, error) {
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
		err := cl.Create(context.TODO(), n)
		if err != nil {
			return nil, fmt.Errorf("unable to create node in cluster: %v", err)
		}
	}

	return cl, nil
}

// updateClusterPolicy updates an existing ClusterPolicy instance
func updateClusterPolicy(n *ClusterPolicyController, cp *gpuv1.ClusterPolicy) error {
	n.singleton = cp
	err := n.rec.Client.Update(context.TODO(), cp)
	if err != nil && !errors.IsConflict(err) {
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
		err = n.rec.Client.DeleteAllOf(context.TODO(), res)
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
		}
		dsLabel = "nvidia-driver-daemonset"
		mainCtrName = "nvidia-driver"
		manifestFile = filepath.Join(cfg.root, driverAssetsPath+"0500_daemonset.yaml")
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
		manifestFile = filepath.Join(cfg.root, devicePluginAssetsPath+"0400_daemonset.yaml")
		mainCtrImage, err = gpuv1.ImagePath(&cp.Spec.DevicePlugin)
		if err != nil {
			return nil, fmt.Errorf("unable to get mainCtrImage for device-plugin: %v", err)
		}
	case "VGPUManager":
		spec = commonDaemonsetSpec{
			repository:       cp.Spec.VGPUManager.Repository,
			image:            cp.Spec.VGPUManager.Image,
			version:          cp.Spec.VGPUManager.Version,
			imagePullPolicy:  cp.Spec.VGPUManager.ImagePullPolicy,
			imagePullSecrets: getImagePullSecrets(cp.Spec.VGPUManager.ImagePullSecrets),
			args:             cp.Spec.VGPUManager.Args,
			env:              cp.Spec.VGPUManager.Env,
			resources:        cp.Spec.VGPUManager.Resources,
		}
		dsLabel = "nvidia-vgpu-manager-daemonset"
		mainCtrName = "nvidia-vgpu-manager-ctr"
		manifestFile = filepath.Join(cfg.root, vGPUManagerAssetsPath+"0500_daemonset.yaml")
		mainCtrImage, err = resolveDriverTag(clusterPolicyController, &cp.Spec.VGPUManager)
		if err != nil {
			return nil, fmt.Errorf("unable to get mainCtrImage for driver: %v", err)
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
	err = addState(&clusterPolicyController, manifestFile)
	if err != nil {
		t.Fatalf("unable to add state: %v", err)
	}
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
	err = clusterPolicyController.rec.Client.List(context.TODO(), list, opts...)
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

	require.Equal(t, mainCtrImage, mainCtr.Image, "unexpected Image")
	require.Equal(t, gpuv1.ImagePullPolicy(spec.imagePullPolicy), mainCtr.ImagePullPolicy, "unexpected ImagePullPolicy")
	require.Equal(t, spec.imagePullSecrets, ds.Spec.Template.Spec.ImagePullSecrets, "unexpected ImagePullSecrets")
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

	switch testCase {
	case "default":
		// Do nothing
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
		"numDaemonsets":          1,
		"mofedValidationPresent": false,
		"nvPeerMemPresent":       false,
		"driverImage":            "nvcr.io/nvidia/driver:470.57.02-ubuntu18.04",
	}

	switch testCase {
	case "default":
		// Do nothing
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

			mofedValidationPresent := false
			nvPeerMemPresent := false
			driverImage := ""
			for _, initContainer := range ds.Spec.Template.Spec.InitContainers {
				if strings.Contains(initContainer.Name, "mofed-validation") {
					mofedValidationPresent = true
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

			require.Equal(t, tc.output["mofedValidationPresent"], mofedValidationPresent, "Unexpected configuration for mofed-validation init container")
			require.Equal(t, tc.output["nvPeerMemPresent"], nvPeerMemPresent, "Unexpected configuration for nv-peermem container")
			require.Equal(t, tc.output["driverImage"], driverImage, "Unexpected configuration for nvidia-driver-ctr image")

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
		"numDaemonsets": 1,
		"driverImage":   "nvcr.io/nvidia/vgpu-manager:470.57.02-ubuntu18.04",
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
			ds, err := testDaemonsetCommon(t, tc.clusterPolicy, "VGPUManager", tc.output["numDaemonsets"].(int))
			if err != nil {
				t.Fatalf("error in testDaemonsetCommon(): %v", err)
			}
			if ds == nil {
				return
			}
			driverImage := ""
			for _, container := range ds.Spec.Template.Spec.Containers {
				if strings.Contains(container.Name, "nvidia-vgpu-manager-ctr") {
					driverImage = container.Image
					continue
				}
			}

			require.Equal(t, tc.output["driverImage"], driverImage, "Unexpected configuration for nvidia-vgpu-manager-ctr image")

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
	manifestPath := filepath.Join(cfg.root, vGPUManagerAssetsPath)
	// add manifests
	err := addState(&clusterPolicyController, manifestPath)
	if err != nil {
		t.Fatalf("unable to add state: %v", err)
	}
	// create resources
	_, err = clusterPolicyController.step()
	if err != nil {
		t.Errorf("error creating resources: %v", err)
	}
}
