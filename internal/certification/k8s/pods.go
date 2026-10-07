package k8s

import (
	"context"
	"fmt"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func (c *Clients) CreatePod(ctx context.Context, namespace string, pod *corev1.Pod) error {
	_, err := c.k8s.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	return err
}

func (c *Clients) DeletePod(ctx context.Context, namespace, name string) error {
	return c.k8s.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

func (c *Clients) GetPod(ctx context.Context, namespace, name string) (*corev1.Pod, error) {
	return c.k8s.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
}

func (c *Clients) ListPods(ctx context.Context, namespace, labelSelector string) (*corev1.PodList, error) {
	return c.k8s.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
}

func (c *Clients) DeletePodsByLabel(ctx context.Context, namespace, labelSelector string) error {
	return c.k8s.CoreV1().Pods(namespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
}

func (c *Clients) IsPodReady(ctx context.Context, namespace, appLabel string) (bool, error) {
	pods, err := c.k8s.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", appLabel),
	})
	if err != nil {
		return false, nil
	}
	return arePodsReady(pods.Items), nil
}

func arePodsReady(pods []corev1.Pod) bool {
	if len(pods) == 0 {
		return false
	}
	for _, pod := range pods {
		if pod.DeletionTimestamp != nil {
			return false
		}
		ready := false
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}
		if !ready {
			return false
		}
	}
	return true
}

func (c *Clients) IsPodDeleted(ctx context.Context, namespace, appLabel string) (bool, error) {
	pods, err := c.k8s.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", appLabel),
	})
	if err != nil {
		return false, nil
	}

	return len(pods.Items) == 0, nil
}

func (c *Clients) WaitForPodReady(ctx context.Context, namespace, appLabel string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		return c.IsPodReady(ctx, namespace, appLabel)
	})
}

func (c *Clients) WaitForPodDeleted(ctx context.Context, namespace, appLabel string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		return c.IsPodDeleted(ctx, namespace, appLabel)
	})
}

func (c *Clients) WaitForPodComplete(ctx context.Context, namespace, name string, timeout time.Duration) (bool, error) {
	var succeeded bool
	err := wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		pod, err := c.k8s.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			succeeded = true
			return true, nil
		case corev1.PodFailed:
			return true, nil
		}
		return false, nil
	})
	return succeeded, err
}

func (c *Clients) WaitForPodsReady(ctx context.Context, namespace, labelSelector string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		pods, err := c.k8s.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			return false, nil
		}
		return arePodsReady(pods.Items), nil
	})
}

func (c *Clients) GetPodLogs(ctx context.Context, namespace, podName, containerName string) (string, error) {
	req := c.k8s.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = stream.Close() }()
	data, err := io.ReadAll(stream)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c *Clients) GetAllPodLabels(ctx context.Context, namespace, appLabel string) (map[string]map[string]string, error) {
	pods, err := c.k8s.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", appLabel),
	})
	if err != nil {
		return nil, err
	}

	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pod found with label app=%s", appLabel)
	}

	result := make(map[string]map[string]string, len(pods.Items))
	for _, pod := range pods.Items {
		result[pod.Name] = pod.Labels
	}

	return result, nil
}
