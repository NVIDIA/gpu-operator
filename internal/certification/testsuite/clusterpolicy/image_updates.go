package clusterpolicy

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/NVIDIA/gpu-operator-self-certification/internal/certification"
	"github.com/NVIDIA/gpu-operator-self-certification/internal/certification/k8s"
)

type ImageUpdates struct{}

func (t *ImageUpdates) Name() string {
	return "driver-image-updates"
}

func (t *ImageUpdates) Run(ctx context.Context, clients *k8s.Clients, cfg certification.Config) *certification.TestResult {
	result := certification.NewTestResult(t.Name())
	defer result.Finalize()

	if cfg.TargetDriverRepository == "" || cfg.TargetDriverImage == "" || cfg.TargetDriverVersion == "" {
		result.Skip("targetDriver not fully specified; set repository, image, and version to enable this test")
		return result
	}

	version := cfg.TargetDriverVersion
	fullImage := fmt.Sprintf("%s/%s:%s", cfg.TargetDriverRepository, cfg.TargetDriverImage, version)

	// Skip if no driver pods are running (driver disabled or pre-installed on host)
	if !clients.HasDriverPods(ctx, cfg.Namespace) {
		result.Skip("no operator-managed driver pods found (driver may be disabled or pre-installed on host)")
		return result
	}

	timeout := cfg.ParseTimeout()

	// Step 0: Check if all driver daemonsets already have the target image
	allAtTarget, _ := t.allDriverDaemonSetsHaveImage(ctx, clients, cfg.Namespace, version)
	if allAtTarget {
		result.Skip("driver already at target image %s, nothing to upgrade", fullImage)
		return result
	}

	// Step 1: Patch driver image (auto-detects ClusterPolicy vs NVIDIADriver mode)
	result.AddDetail("patching driver image to %s", fullImage)
	if err := clients.UpdateDriverImage(ctx, cfg.TargetDriverRepository, cfg.TargetDriverImage, version); err != nil {
		result.Fail("failed to patch driver image: %v", err)
		return result
	}

	// Step 2: Wait for all driver daemonsets to update with new image
	result.AddDetail("waiting for driver daemonset images to update")
	err := clients.WaitForCondition(ctx, timeout, func(ctx context.Context) (bool, error) {
		return t.allDriverDaemonSetsHaveImage(ctx, clients, cfg.Namespace, version)
	})
	if err != nil {
		result.Fail("driver daemonset image did not update to %s: %v", fullImage, err)
		return result
	}
	result.AddDetail("driver daemonset images updated successfully")

	// Step 3: Delete driver pods to trigger OnDelete update
	result.AddDetail("deleting driver pods to trigger OnDelete rollout")
	if err := clients.DeletePodsByLabel(ctx, cfg.Namespace, k8s.DriverLabelSelector); err != nil {
		result.Fail("failed to delete driver pods: %v", err)
		return result
	}

	// Step 4: Wait for driver pods to come back ready with the new image.
	// We check pod state first (before the node-label check in Step 5)
	// to avoid a race where nvidia.com/gpu-driver-upgrade-state is still
	// absent or stale immediately after pod deletion.
	result.AddDetail("waiting for driver pods to be ready with new image")
	err = clients.WaitForCondition(ctx, timeout, func(ctx context.Context) (bool, error) {
		pods, err := clients.ListPods(ctx, cfg.Namespace, k8s.DriverLabelSelector)
		if err != nil || len(pods.Items) == 0 {
			return false, nil
		}

		// Fail fast if any pod is stuck pulling the image.
		for _, pod := range pods.Items {
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting != nil {
					reason := cs.State.Waiting.Reason
					if reason == "ImagePullBackOff" || reason == "ErrImagePull" {
						return false, fmt.Errorf("image pull failed for %s: %s",
							cs.Image, cs.State.Waiting.Message)
					}
				}
			}
		}

		// Ensure all expected pods exist across all driver daemonsets.
		dsList, dsErr := clients.ListDaemonSets(ctx, cfg.Namespace, k8s.DriverLabelSelector)
		if dsErr == nil {
			totalDesired := 0
			for _, ds := range dsList.Items {
				totalDesired += int(ds.Status.DesiredNumberScheduled)
			}
			if totalDesired > 0 && len(pods.Items) < totalDesired {
				return false, nil
			}
		}

		for _, pod := range pods.Items {
			ready := false
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
					ready = true
					break
				}
			}
			if !ready {
				return false, nil
			}
			hasImage := false
			for _, container := range pod.Spec.Containers {
				if container.Name == k8s.DriverContainerName && strings.Contains(container.Image, version) {
					hasImage = true
					break
				}
			}
			if !hasImage {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		result.Fail("driver pod not ready with expected image: %v", err)
		return result
	}

	// Step 5: Confirm the GPU operator considers the upgrade done.
	// Now that pods are ready this should resolve quickly; checking the
	// node label ensures the operator's own state machine has settled.
	result.AddDetail("waiting for gpu-driver-upgrade-state to confirm upgrade done")
	if err := clients.WaitForDriverUpgradeDone(ctx, timeout); err != nil {
		result.Fail("driver upgrade state did not reach done: %v", err)
		return result
	}

	result.AddDetail("driver pod ready with image %s", fullImage)
	return result
}

// allDriverDaemonSetsHaveImage checks whether all driver daemonsets have the
// given version in their driver container image spec.
func (t *ImageUpdates) allDriverDaemonSetsHaveImage(ctx context.Context, clients *k8s.Clients, namespace, version string) (bool, error) {
	dsList, err := clients.ListDaemonSets(ctx, namespace, k8s.DriverLabelSelector)
	if err != nil {
		return false, nil
	}
	if len(dsList.Items) == 0 {
		return false, nil
	}
	for _, ds := range dsList.Items {
		found := false
		for _, container := range ds.Spec.Template.Spec.Containers {
			if container.Name == k8s.DriverContainerName && strings.Contains(container.Image, version) {
				found = true
				break
			}
		}
		if !found {
			return false, nil
		}
	}
	return true, nil
}
