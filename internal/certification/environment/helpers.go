package environment

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// parseImageTag extracts the tag from an image reference.
func parseImageTag(image string) string {
	// Handle digest-based images
	if idx := strings.LastIndex(image, "@"); idx != -1 {
		return image[idx+1:]
	}
	// Handle tag-based images - but need to avoid matching registry port
	if idx := strings.LastIndex(image, ":"); idx != -1 {
		candidate := image[idx+1:]
		// If candidate contains a slash, it's a registry port (e.g., registry:5000/image), not a tag
		if strings.Contains(candidate, "/") {
			return ""
		}
		return candidate
	}
	return ""
}

// isPodReady checks if a pod has the Ready condition set to True.
func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

// getPodNotReadyReason returns the reason why a pod is not ready.
func getPodNotReadyReason(pod *corev1.Pod) string {
	// Check Ready condition reason
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status != corev1.ConditionTrue {
			if cond.Reason != "" {
				return cond.Reason
			}
		}
	}

	// Check container statuses for waiting reason
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			return cs.State.Waiting.Reason
		}
	}

	return ""
}
