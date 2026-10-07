package environment

import (
	"context"

	"github.com/NVIDIA/gpu-operator-self-certification/internal/certification/k8s"
)

type operatorCollector struct{}

func (c *operatorCollector) Name() string { return "operator" }

func (c *operatorCollector) Collect(ctx context.Context, clients *k8s.Clients, namespace string, s *EnvironmentSnapshot) error {
	s.Operator.Namespace = namespace

	// Get operator pod
	pods, err := clients.ListPods(ctx, namespace, "app=gpu-operator")
	if err != nil {
		return err
	}

	if len(pods.Items) == 0 {
		return nil
	}

	pod := pods.Items[0]
	s.Operator.PodName = pod.Name
	s.Operator.PodPhase = string(pod.Status.Phase)
	s.Operator.PodReady = isPodReady(&pod)

	// Get image and version from pod
	if len(pod.Spec.Containers) > 0 {
		container := pod.Spec.Containers[0]
		s.Operator.Image = container.Image
		s.Operator.Version = parseImageTag(container.Image)

		// Collect args
		s.Operator.Args = container.Args

		// Collect env vars
		s.Operator.Env = make(map[string]string)
		for _, env := range container.Env {
			if env.Value != "" {
				s.Operator.Env[env.Name] = env.Value
			}
		}
	}

	// Try to get version from labels
	if v, ok := pod.Labels["app.kubernetes.io/version"]; ok && s.Operator.Version == "" {
		s.Operator.Version = v
	}

	// Check for Helm release
	if release, ok := pod.Annotations["meta.helm.sh/release-name"]; ok {
		s.Operator.HelmRelease = release
	}

	// Get deployment for replica info
	deploy, err := clients.GetDeployment(ctx, namespace, "gpu-operator")
	if err == nil && deploy != nil {
		if deploy.Spec.Replicas != nil {
			s.Operator.Replicas = *deploy.Spec.Replicas
		}
		s.Operator.ReadyReplicas = deploy.Status.ReadyReplicas
	}

	return nil
}
