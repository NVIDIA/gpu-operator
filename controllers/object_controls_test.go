package controllers

import (
	"context"
	"fmt"
	gpuv1 "github.com/NVIDIA/gpu-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"log"
	"os"
	"path/filepath"
	goruntime "runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"testing"
)

const (
	clusterPolicyPath   = "config/samples/v1_clusterpolicy.yaml"
	clusterPolicyName   = "gpu-cluster-policy"
	driverAssetsPath    = "assets/state-driver/"
	driverDaemonsetName = "nvidia-driver-daemonset"
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
)

var nfdLabels = map[string]string{
	"feature.node.kubernetes.io/pci-10de.present":             "true",
	"feature.node.kubernetes.io/kernel-version.full":          "5.4.0",
	"feature.node.kubernetes.io/system-os_release.ID":         "ubuntu",
	"feature.node.kubernetes.io/system-os_release.VERSION_ID": "18.04",
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
	s := scheme.Scheme
	if err := gpuv1.AddToScheme(s); err != nil {
		return fmt.Errorf("unable to add ClusterPolicy v1 schema: %v", err)
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

// TestDriverAssets tests that valid spec is generated for all driver assets
func TestDriverAssets(t *testing.T) {
	err := addState(&clusterPolicyController, filepath.Join(cfg.root, driverAssetsPath))
	if err != nil {
		t.Fatalf("unable to add state: %v", err)
	}

	_, err = clusterPolicyController.step()
	if err != nil {
		t.Errorf("error creating resources: %v", err)
	}

	// Verify that driver DaemonSet is deployed properly
	opts := []client.ListOption{
		client.MatchingLabels{"app": "nvidia-driver-daemonset"},
	}
	list := &appsv1.DaemonSetList{}
	err = clusterPolicyController.rec.Client.List(context.TODO(), list, opts...)
	if err != nil {
		t.Fatalf("could not get DaemonSetList from client: %v", err)
	}
	if len(list.Items) == 0 {
		t.Fatalf("no daemonsets returned from client")
	}

	ds := list.Items[0]
	t.Logf("%v created with valid spec", ds.Name)
	t.Logf("driver image: %v", ds.Spec.Template.Spec.Containers[0].Image)
}
