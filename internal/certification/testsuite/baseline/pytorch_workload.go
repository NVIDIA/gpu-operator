package baseline

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/NVIDIA/gpu-operator/internal/certification"
	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

const (
	pytorchTestPodName  = "pytorch-certification-test"
	defaultPyTorchImage = "nvcr.io/nvidia/pytorch:26.02-py3"
)

type PyTorchWorkload struct{}

func (p *PyTorchWorkload) Name() string {
	return "pytorch-workload"
}

func (p *PyTorchWorkload) Run(ctx context.Context, clients *k8s.Clients, cfg certification.Config) *certification.TestResult {
	return runPodTest(ctx, clients, cfg, podTestOpts{
		testName:       p.Name(),
		podName:        pytorchTestPodName,
		imageDesc:      defaultPyTorchImage,
		successMessage: "PyTorch GPU workload completed successfully",
		pod:            p.buildPod(cfg),
	})
}

func (p *PyTorchWorkload) buildPod(cfg certification.Config) *corev1.Pod {
	pythonScript := `
import torch
import torch.nn as nn
import torch.optim as optim

# Verify CUDA is available
assert torch.cuda.is_available(), "CUDA is not available"
print(f"CUDA device: {torch.cuda.get_device_name(0)}")

# Create random tensors on GPU
device = torch.device("cuda")
x = torch.randn(64, 10, device=device)
y = torch.randn(64, 1, device=device)
assert x.device.type == "cuda", "Input tensor not on GPU"
print("Tensors created on GPU")

# Define a simple 2-layer neural network
model = nn.Sequential(
    nn.Linear(10, 32),
    nn.ReLU(),
    nn.Linear(32, 1),
).to(device)
assert next(model.parameters()).device.type == "cuda", "Model not on GPU"
print("Model loaded on GPU")

# Train for 5 epochs
criterion = nn.MSELoss()
optimizer = optim.SGD(model.parameters(), lr=0.01)

for epoch in range(5):
    optimizer.zero_grad()
    output = model(x)
    loss = criterion(output, y)
    loss.backward()
    optimizer.step()
    print(f"Epoch {epoch+1}/5, Loss: {loss.item():.4f}")

print("PyTorch GPU training completed successfully")
`

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pytorchTestPodName,
			Namespace: cfg.Namespace,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    "pytorch-test",
					Image:   defaultPyTorchImage,
					Command: []string{"python3", "-c", pythonScript},
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
