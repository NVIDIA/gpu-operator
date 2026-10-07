package k8s

const (
	// GPUNodeSelector selects nodes with NVIDIA GPUs present.
	GPUNodeSelector = "nvidia.com/gpu.present=true"

	// DriverLabelSelector selects all driver pods/daemonsets in both ClusterPolicy
	// and NVIDIADriver modes via the standard Kubernetes component label.
	DriverLabelSelector = "app.kubernetes.io/component=nvidia-driver"

	// DriverContainerName is the container name for the NVIDIA driver in driver pods.
	DriverContainerName = "nvidia-driver-ctr"
)
