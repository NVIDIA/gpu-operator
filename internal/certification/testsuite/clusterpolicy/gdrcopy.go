package clusterpolicy

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/NVIDIA/gpu-operator/internal/certification"
	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

const (
	gdrcopyContainerName = "nvidia-gdrcopy-ctr"
	gdrcopyTestPodName   = "gdrcopy-test"
)

type GDRCopy struct{}

func (t *GDRCopy) Name() string {
	return "gdrcopy-enabled"
}

func (t *GDRCopy) Run(ctx context.Context, clients *k8s.Clients, cfg certification.Config) *certification.TestResult {
	result := certification.NewTestResult(t.Name())
	defer result.Finalize()

	// Skip if no driver pods are running (driver disabled or pre-installed on host)
	if !clients.HasDriverPods(ctx, cfg.Namespace) {
		result.Skip("no operator-managed driver pods found (driver may be disabled or pre-installed on host)")
		return result
	}

	// Determine which nodes support GDRCopy (non-precompiled drivers only)
	eligibleNodes, err := clients.GDRCopyEligibleNodes(ctx)
	if err != nil {
		result.Fail("failed to determine GDRCopy-eligible nodes: %v", err)
		return result
	}
	if len(eligibleNodes) == 0 {
		result.Skip("no GDRCopy-eligible nodes (all drivers use pre-compiled modules)")
		return result
	}
	result.AddDetail("found %d GDRCopy-eligible nodes", len(eligibleNodes))

	timeout := cfg.ParseTimeout()

	// Step 1: Enable GDRCopy (mode-aware: patches all NVIDIADriver CRs or ClusterPolicy)
	result.AddDetail("enabling GDRCopy via gdrcopy.enabled=true")
	if err := clients.EnableGDRCopy(ctx); err != nil {
		result.Fail("failed to enable GDRCopy: %v", err)
		return result
	}

	// Step 2: Delete driver pods to trigger update (OnDelete update strategy)
	result.AddDetail("deleting driver pods to trigger GDRCopy enablement")
	if err := clients.DeletePodsByLabel(ctx, cfg.Namespace, k8s.DriverLabelSelector); err != nil {
		result.Fail("failed to delete driver pods: %v", err)
		return result
	}

	// Step 3: Wait for driver pods to be ready.
	// The GDRCopy sidecar has a startup probe (lsmod | grep gdrdrv) so pods
	// only become Ready after the gdrdrv kernel module is successfully loaded.
	result.AddDetail("waiting for driver pods to restart with GDRCopy sidecar")
	if err := clients.WaitForPodsReady(ctx, cfg.Namespace, k8s.DriverLabelSelector, timeout); err != nil {
		result.Fail("driver pods not ready: %v", err)
		return result
	}

	// Step 4: Verify gdrcopy sidecar container exists in driver pods on eligible nodes
	result.AddDetail("verifying gdrcopy sidecar container in driver pods on eligible nodes")
	hasGDRCopy, err := t.verifyGDRCopyContainer(ctx, clients, cfg.Namespace, eligibleNodes)
	if err != nil {
		result.Fail("failed to check driver pods: %v", err)
		return result
	}
	if !hasGDRCopy {
		result.Fail("gdrcopy container %q not found in eligible driver pods", gdrcopyContainerName)
		return result
	}
	result.AddDetail("gdrcopy sidecar container found in all eligible driver pods")

	// Step 5: Deploy sample gdrcopy workload on an eligible node
	// Pick any eligible node to ensure the workload lands where GDRCopy is enabled.
	var targetNode string
	for n := range eligibleNodes {
		targetNode = n
		break
	}
	result.AddDetail("deploying sample gdrcopy workload on node %s", targetNode)
	pod := t.buildGDRCopyTestPod(cfg, targetNode)

	// Clean up any existing pod
	_ = clients.DeletePod(ctx, cfg.Namespace, gdrcopyTestPodName)

	if err := clients.CreatePod(ctx, cfg.Namespace, pod); err != nil {
		result.Fail("failed to create gdrcopy test pod: %v", err)
		return result
	}

	defer func() {
		_ = clients.DeletePod(ctx, cfg.Namespace, gdrcopyTestPodName)
	}()

	// Wait for pod to complete
	succeeded, err := clients.WaitForPodComplete(ctx, cfg.Namespace, gdrcopyTestPodName, timeout)
	if err != nil {
		result.Fail("gdrcopy test pod did not complete: %v", err)
		return result
	}
	if !succeeded {
		result.Fail("gdrcopy test pod failed")
		return result
	}

	result.AddDetail("GDRCopy configuration and workload test successful")
	return result
}

func (t *GDRCopy) verifyGDRCopyContainer(ctx context.Context, clients *k8s.Clients, namespace string, eligibleNodes map[string]bool) (bool, error) {
	pods, err := clients.ListPods(ctx, namespace, k8s.DriverLabelSelector)
	if err != nil {
		return false, err
	}

	checked := 0
	for _, pod := range pods.Items {
		if !eligibleNodes[pod.Spec.NodeName] {
			continue
		}
		checked++
		found := false
		for _, container := range pod.Spec.Containers {
			if container.Name == gdrcopyContainerName {
				found = true
				break
			}
		}
		if !found {
			return false, nil
		}
	}
	return checked > 0, nil
}

// gdrcopyWorkloadScript verifies GDRCopy is functional inside a container.
// It checks GPU access, validates the /dev/gdrdrv character device is present,
// and runs gdrcopy_sanity if available in the image for an end-to-end data copy test.
var gdrcopyWorkloadScript = `set -e
nvidia-smi
echo "Checking GDRCopy device..."
test -c /dev/gdrdrv
echo "GDRCopy device /dev/gdrdrv present"
if command -v gdrcopy_sanity >/dev/null 2>&1; then
    echo "Running gdrcopy_sanity..."
    gdrcopy_sanity
    echo "GDRCopy sanity test passed"
elif command -v copybw >/dev/null 2>&1; then
    echo "Running copybw..."
    copybw
    echo "GDRCopy bandwidth test passed"
else
    echo "GDRCopy sanity tools not available in image; device accessibility confirmed"
fi
`

func (t *GDRCopy) buildGDRCopyTestPod(cfg certification.Config, nodeName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gdrcopyTestPodName,
			Namespace: cfg.Namespace,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			NodeName:      nodeName,
			Containers: []corev1.Container{{
				Name:    "gdrcopy-test",
				Image:   cfg.TestWorkloadImage,
				Command: []string{"sh", "-c", gdrcopyWorkloadScript},
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("1"),
					},
				},
				Env: []corev1.EnvVar{
					{Name: "NVIDIA_GDRCOPY", Value: "enabled"},
				},
			}},
		},
	}
}
