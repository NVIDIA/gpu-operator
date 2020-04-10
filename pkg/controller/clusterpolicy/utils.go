package clusterpolicy

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var OperatorLabels map[string]string = nil

func SetObjectLabels(c client.Client, labels map[string]string) error {
	if OperatorLabels == nil {
		if err := PopulateOperatorLabels(c); err != nil {
			return err
		}
	}

	for k, v := range OperatorLabels {
		labels[k] = v
	}

	return nil
}

func PopulateOperatorLabels(c client.Client) error {
	p := &corev1.Pod{}
	podName := getEnv("POD_NAME")
	podNamespace := getEnv("POD_NAMESPACE")

	err := c.Get(context.TODO(), types.NamespacedName{Namespace: podNamespace, Name: podName}, p)
	if err != nil {
		return err
	}

	OperatorLabels = make(map[string]string)
	labels := []string{"app.kubernetes.io/name", "app.kubernetes.io/instance", "app.kubernetes.io/version"}
	for _, l := range labels {
		val, ok := p.Labels[l]
		if !ok {
			continue
		}

		OperatorLabels[l] = val
	}

	OperatorLabels["app.kubernetes.io/managed-by"] = "gpu-operator"
	return nil
}
