// RDMA bandwidth test per NVIDIA GPUDirect RDMA verification procedure:
// https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/gpu-operator-rdma.html
package clusterpolicy

import (
	"context"
	"fmt"
	"log"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/NVIDIA/gpu-operator-self-certification/internal/certification"
	"github.com/NVIDIA/gpu-operator-self-certification/internal/certification/k8s"
)

const (
	rdmaServerPodName = "rdma-bw-server"
	rdmaClientPodName = "rdma-bw-client"
	rdmaContainerName = "rdma-test"

	// CUDA base image for RDMA test pods. Host MOFED libs are mounted via init container
	// since mellanox/cuda-perftest ships libs incompatible with MOFED 25.x+.
	// Requires glibc >= host MOFED. Override via Config.RDMABaseImage.
	defaultRDMABaseImage = "nvcr.io/nvidia/cuda:13.1.1-base-ubuntu24.04"

	// Init container image for copying host MOFED libs/binaries into a shared volume.
	rdmaInitImage = "busybox"

	maxLogTailBytes = 4096
)

// rdmaDeviceDetectScript auto-detects an active RDMA device, sets RDMA_DEV and RDMA_GID.
// For RoCE (Ethernet) devices, selects a RoCE v2 GID when available.
const rdmaDeviceDetectScript = `
RDMA_DEV=""
RDMA_GID=""
for path in /sys/class/infiniband/*; do
  [ -d "$path" ] || continue
  state=$(cat "$path/ports/1/state" 2>/dev/null)
  case "$state" in *ACTIVE*) ;; *) continue ;; esac
  RDMA_DEV=${path##*/}
  link=$(cat "$path/ports/1/link_layer" 2>/dev/null)
  if [ "$link" = "Ethernet" ]; then
    RDMA_GID=0
    for i in 0 1 2 3 4 5 6 7; do
      gtype=$(cat "$path/ports/1/gid_attrs/types/$i" 2>/dev/null)
      gid=$(cat "$path/ports/1/gids/$i" 2>/dev/null)
      if [ "$gtype" = "RoCE v2" ] && [ -n "$gid" ] && \
         [ "$gid" != "0000:0000:0000:0000:0000:0000:0000:0000" ]; then
        RDMA_GID=$i
        break
      fi
    done
  fi
  break
done

if [ -z "$RDMA_DEV" ]; then
  echo "ERROR: No active RDMA device found"
  echo "Available devices:"
  for path in /sys/class/infiniband/*; do
    [ -d "$path" ] || continue
    echo "  ${path##*/}: state=$(cat "$path/ports/1/state" 2>/dev/null) link=$(cat "$path/ports/1/link_layer" 2>/dev/null)"
  done
  exit 1
fi
echo "Using RDMA device: $RDMA_DEV (GID index: ${RDMA_GID:-none})"
`

// rdmaScript builds the ib_write_bw command for server (serverIP="") or client mode.
func rdmaScript(serverIP string, dmaBuf bool) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("export LD_LIBRARY_PATH=/mofed/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}\n")
	b.WriteString("export IBV_DRIVERS_DIR=/mofed/lib\n")
	b.WriteString("nvidia-smi\n")
	b.WriteString(rdmaDeviceDetectScript)

	if serverIP != "" {
		fmt.Fprintf(&b, "sleep 5\necho 'Running RDMA write bandwidth test to %s...'\n", serverIP)
	}

	b.WriteString("/mofed/bin/ib_write_bw --use_cuda=0")
	if dmaBuf {
		b.WriteString(" --use_cuda_dmabuf")
	}
	b.WriteString(` -d "$RDMA_DEV" ${RDMA_GID:+-x "$RDMA_GID"} -a -F --report_gbits -q 1`)
	if serverIP != "" {
		b.WriteString(" " + serverIP)
	}
	b.WriteString("\n")

	return b.String()
}

// getHostLibPathForOS returns the userspace library path based on OS family
// and CPU architecture. Debian/Ubuntu use multiarch paths; everything else
// (RHEL, SLES, Oracle Linux, Flatcar, CoreOS, …) uses /usr/lib64.
func getHostLibPathForOS(osID, arch string) string {
	switch osID {
	case "ubuntu", "debian":
		if arch == "arm64" {
			return "/usr/lib/aarch64-linux-gnu"
		}
		return "/usr/lib/x86_64-linux-gnu"
	default:
		return "/usr/lib64"
	}
}

// RDMA runs a GPUDirect RDMA bandwidth test between two GPU nodes.
type RDMA struct {
	DMABuf bool
}

func (t *RDMA) Name() string {
	if t.DMABuf {
		return "gpudirect-rdma-dmabuf"
	}
	return "gpudirect-rdma"
}

func (t *RDMA) Run(ctx context.Context, clients *k8s.Clients, cfg certification.Config) *certification.TestResult {
	result := certification.NewTestResult(t.Name())
	defer result.Finalize()

	if !clients.HasDriverPods(ctx, cfg.Namespace) {
		result.Skip("no operator-managed driver pods found (driver may be disabled or pre-installed on host)")
		return result
	}

	timeout := cfg.ParseTimeout()

	gpuNodes, err := clients.GetGPUNodes(ctx)
	if err != nil {
		result.Fail("failed to get GPU nodes: %v", err)
		return result
	}
	if len(gpuNodes) < 2 {
		result.Skip("RDMA test requires at least 2 GPU nodes, found %d", len(gpuNodes))
		return result
	}
	result.AddDetail("found %d GPU nodes for RDMA test", len(gpuNodes))

	// Step 1: Enable RDMA if not using DMA-BUF. The rdma.enabled field controls
	// nvidia_peermem, which is unnecessary with DMA-BUF.
	if !t.DMABuf {
		result.AddDetail("enabling GPUDirectRDMA via rdma.enabled=true (auto-detecting useHostMofed)")
		if err := clients.EnableRDMA(ctx); err != nil {
			result.Fail("failed to enable RDMA: %v", err)
			return result
		}

		// Step 2: Restart driver pods to pick up RDMA config (OnDelete update strategy)
		result.AddDetail("deleting driver pods to trigger RDMA enablement")
		if err := clients.DeletePodsByLabel(ctx, cfg.Namespace, k8s.DriverLabelSelector); err != nil {
			result.Fail("failed to delete driver pods: %v", err)
			return result
		}

		// Step 3: Wait for driver and device-plugin pods to be ready
		result.AddDetail("waiting for driver pods to restart with RDMA enabled")
		if err := clients.WaitForPodsReady(ctx, cfg.Namespace, k8s.DriverLabelSelector, timeout); err != nil {
			result.Fail("driver pods not ready after RDMA enablement: %v", err)
			return result
		}
		result.AddDetail("driver pods ready with RDMA enabled")

		result.AddDetail("waiting for device-plugin pods to be ready")
		if err := clients.WaitForPodReady(ctx, cfg.Namespace, DevicePluginAppLabel, timeout); err != nil {
			result.Fail("device-plugin pods not ready: %v", err)
			return result
		}
	} else {
		result.AddDetail("DMA-BUF mode: skipping rdma.enabled (nvidia_peermem not needed)")
	}

	// Step 4: Deploy server-client ib_write_bw bandwidth test
	result.AddDetail("deploying RDMA bandwidth test: server on %s, client on %s", gpuNodes[0], gpuNodes[1])

	baseImage := cfg.RDMABaseImage
	if baseImage == "" {
		baseImage = defaultRDMABaseImage
	}

	// Auto-detect host library path per node — OS families differ (e.g. Ubuntu
	// uses /usr/lib/x86_64-linux-gnu while RHEL uses /usr/lib64).
	serverLabels, err := clients.GetNodeLabels(ctx, gpuNodes[0])
	if err != nil {
		result.Fail("failed to get labels for node %s: %v", gpuNodes[0], err)
		return result
	}
	serverOS, serverArch := serverLabels["feature.node.kubernetes.io/system-os_release.ID"], serverLabels["kubernetes.io/arch"]
	serverLibPath := getHostLibPathForOS(serverOS, serverArch)
	result.AddDetail("node %s: OS %q arch %q, lib path %s", gpuNodes[0], serverOS, serverArch, serverLibPath)

	clientLabels, err := clients.GetNodeLabels(ctx, gpuNodes[1])
	if err != nil {
		result.Fail("failed to get labels for node %s: %v", gpuNodes[1], err)
		return result
	}
	clientOS, clientArch := clientLabels["feature.node.kubernetes.io/system-os_release.ID"], clientLabels["kubernetes.io/arch"]
	clientLibPath := getHostLibPathForOS(clientOS, clientArch)
	result.AddDetail("node %s: OS %q arch %q, lib path %s", gpuNodes[1], clientOS, clientArch, clientLibPath)

	t.cleanupRDMAPods(ctx, clients, cfg.Namespace)

	serverPod := t.buildRDMAPod(cfg.Namespace, rdmaServerPodName, gpuNodes[0], rdmaScript("", t.DMABuf), baseImage, serverLibPath)
	if err := clients.CreatePod(ctx, cfg.Namespace, serverPod); err != nil {
		result.Fail("failed to create RDMA server pod: %v", err)
		return result
	}
	defer t.cleanupRDMAPods(ctx, clients, cfg.Namespace)

	// Step 5: Wait for server to be running and get its IP
	var serverIP string
	err = clients.WaitForCondition(ctx, timeout, func(ctx context.Context) (bool, error) {
		pod, err := clients.GetPod(ctx, cfg.Namespace, rdmaServerPodName)
		if err != nil {
			return false, nil
		}
		if pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" {
			serverIP = pod.Status.PodIP
			return true, nil
		}
		if pod.Status.Phase == corev1.PodFailed {
			return false, fmt.Errorf("server pod failed")
		}
		return false, nil
	})
	if err != nil {
		t.attachPodLogs(ctx, clients, cfg.Namespace, rdmaServerPodName, result)
		result.Fail("RDMA server pod not running: %v", err)
		return result
	}
	result.AddDetail("RDMA server running on %s at %s", gpuNodes[0], serverIP)

	// Step 6: Deploy client on a different node
	clientPod := t.buildRDMAPod(cfg.Namespace, rdmaClientPodName, gpuNodes[1], rdmaScript(serverIP, t.DMABuf), baseImage, clientLibPath)
	if err := clients.CreatePod(ctx, cfg.Namespace, clientPod); err != nil {
		result.Fail("failed to create RDMA client pod: %v", err)
		return result
	}

	// Step 7: Wait for bandwidth test to complete
	result.AddDetail("running RDMA write bandwidth test across nodes")
	succeeded, err := clients.WaitForPodComplete(ctx, cfg.Namespace, rdmaClientPodName, timeout)
	if err != nil {
		result.Fail("RDMA bandwidth test did not complete: %v", err)
		return result
	}
	if !succeeded {
		t.attachPodLogs(ctx, clients, cfg.Namespace, rdmaClientPodName, result)
		result.Fail("RDMA bandwidth test failed")
		return result
	}

	result.AddDetail("RDMA write bandwidth test completed successfully across 2 GPU nodes")
	return result
}

// attachPodLogs attaches the tail of a pod's logs to the result for debugging.
func (t *RDMA) attachPodLogs(ctx context.Context, clients *k8s.Clients, namespace, podName string, result *certification.TestResult) {
	logs, err := clients.GetPodLogs(ctx, namespace, podName, rdmaContainerName)
	if err != nil || logs == "" {
		return
	}
	if len(logs) > maxLogTailBytes {
		logs = logs[len(logs)-maxLogTailBytes:]
	}
	result.AddDetail("%s logs: %s", podName, strings.TrimSpace(logs))
}

func (t *RDMA) cleanupRDMAPods(ctx context.Context, clients *k8s.Clients, namespace string) {
	for _, name := range []string{rdmaServerPodName, rdmaClientPodName} {
		if err := clients.DeletePod(ctx, namespace, name); err != nil {
			log.Printf("WARNING: failed to delete RDMA pod %s: %v", name, err)
		}
	}
}

// mofedInitContainer copies host MOFED userspace libs and perftest binaries
// into a shared emptyDir (needed because mellanox/cuda-perftest is ABI-incompatible
// with MOFED 25.x+).
func mofedInitContainer() corev1.Container {
	copyScript := `mkdir -p /mofed/lib /mofed/bin
for lib in libibverbs libmlx5 librdmacm libibumad libnl-3 libnl-route-3 libefa libpci; do
  cp /host-lib/${lib}.so* /mofed/lib/ 2>/dev/null || true
done
cp /host-lib/libibverbs/libmlx5-rdmav*.so /mofed/lib/ 2>/dev/null || true
for bin in ib_write_bw ib_read_bw; do
  cp /host-bin/$bin /mofed/bin/ 2>/dev/null || true
done`

	return corev1.Container{
		Name:    "copy-mofed",
		Image:   rdmaInitImage,
		Command: []string{"/bin/sh", "-c", copyScript},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "host-lib", MountPath: "/host-lib", ReadOnly: true},
			{Name: "host-bin", MountPath: "/host-bin", ReadOnly: true},
			{Name: "mofed", MountPath: "/mofed"},
		},
	}
}

func (t *RDMA) buildRDMAPod(namespace, name, nodeName, script, baseImage, libPath string) *corev1.Pod {
	privileged := true
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			RestartPolicy:  corev1.RestartPolicyNever,
			HostNetwork:    true,
			NodeSelector:   map[string]string{"kubernetes.io/hostname": nodeName},
			InitContainers: []corev1.Container{mofedInitContainer()},
			Containers: []corev1.Container{{
				Name:    rdmaContainerName,
				Image:   baseImage,
				Command: []string{"/bin/bash", "-c", script},
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("1"),
					},
				},
				// Privileged is required for RDMA verbs to open uverbs devices.
				SecurityContext: &corev1.SecurityContext{
					Privileged: &privileged,
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "infiniband", MountPath: "/dev/infiniband"},
					{Name: "mofed", MountPath: "/mofed", ReadOnly: true},
				},
			}},
			Volumes: []corev1.Volume{
				{
					Name: "infiniband",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{Path: "/dev/infiniband"},
					},
				},
				{
					Name: "host-lib",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{Path: libPath},
					},
				},
				{
					Name: "host-bin",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{Path: "/usr/bin"},
					},
				},
				{
					Name:         "mofed",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				},
			},
		},
	}
}
