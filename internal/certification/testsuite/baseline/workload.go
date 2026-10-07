package baseline

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/NVIDIA/gpu-operator-self-certification/internal/certification"
	"github.com/NVIDIA/gpu-operator-self-certification/internal/certification/k8s"
)

const gpuTestPodName = "gpu-certification-test"

type Workload struct{}

func (w *Workload) Name() string {
	return "gpu-workload"
}

func (w *Workload) Run(ctx context.Context, clients *k8s.Clients, cfg certification.Config) *certification.TestResult {
	return runPodTest(ctx, clients, cfg, podTestOpts{
		testName:       w.Name(),
		podName:        gpuTestPodName,
		imageDesc:      cfg.TestWorkloadImage,
		successMessage: "default GPU workload completed successfully",
		pod:            w.buildPod(cfg),
	})
}

func (w *Workload) buildPod(cfg certification.Config) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gpuTestPodName,
			Namespace: cfg.Namespace,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:  "cuda-test",
					Image: cfg.TestWorkloadImage,
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}
}
